package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/paths"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
)

// BreakdownModel is the Bubbletea model for the breakdown TUI
type BreakdownModel struct {
	sessionPath string
	sessionID   string
	noColor     bool

	messages       []models.BreakdownMessage
	totalCost      float64
	minCost        float64 // For cost gradient coloring
	maxCost        float64 // For cost gradient coloring
	insights       *models.MessageInsights
	skippedLines   int               // JSONL lines skipped during parsing (malformed or oversized)
	skippedAgents  int               // Agent sub-sessions that could not be read
	estimatedCosts int               // Messages whose cache-write cost is a 5m-rate estimate
	hasUnknown     bool              // Any message priced from the fallback table (marked in the rows)
	hasAgents      bool              // Any agent row in the merged list — drives the insight scope label
	runTags        map[string]string // Workflow run ID -> AGENT-column run tag
	err            error
	loading        bool
	lastUpdated    time.Time

	// agentCache memoizes agent sub-session parses so a reload triggered by a
	// parent-file write doesn't re-parse every unchanged agent.
	agentCache *analyzer.AgentParseCache

	// Viewport for scrolling
	viewport   viewport.Model
	autoScroll bool
	ready      bool // viewport initialized
	// pausedAt is the message count when auto-scroll last turned off; rows
	// past it arrived while the reader was scrolled up.
	pausedAt int
	// pausePending: auto-scroll went off before any load landed, so the next
	// load's rows are the baseline, not new arrivals.
	pausePending bool
	// selectedKey is the row p last jumped to (breakdownMsgKey), highlighted
	// until another key is pressed; "" when none.
	selectedKey string
	// lineRows maps each viewport content line to the display Index of the
	// message on it, or 0 for a day divider.
	lineRows []int

	// Header state: project name, and the newest message timestamp (else the
	// session file's mtime, with activityFromFile set) that the header ages.
	project          string
	waitingIn        string // the directory a waiting model waits for a session in
	lastActivity     time.Time
	activityFromFile bool
	now              func() time.Time // tests pin the clock; nil means time.Now

	// Change tracking for highlight animation. Keyed by message identity, not
	// position: on every reload the merged list is timestamp-sorted and reindexed
	// 1..N, so a positional key would flag old rows that merely shifted when a new
	// agent message inserts mid-list.
	newMsgKeys map[string]time.Time // message identity -> when it was added

	spinner   spinner.Model
	watcher   *fsnotify.Watcher
	done      chan struct{}
	closing   *atomic.Bool
	closeOnce *sync.Once
	wg        *sync.WaitGroup

	// A file-change waiter is blocked on the watcher; gates armFileWaiter so
	// poll-triggered reloads can't stack extra waiters
	fileWaiterActive bool

	// fallback polls the session file while its watcher can't be created
	fallback watchFallback

	// Auto-follow mode for tracking new sessions
	projectDir      string          // Project directory to watch for new sessions
	followMode      bool            // Whether to auto-follow new sessions
	prevSessionID   string          // Session open before the last switch
	prevSessionPath string          // Its file, for the go-back key (-)
	switchedBack    bool            // The last switch was the go-back key
	sessionWatcher  *SessionWatcher // Watches for new session files
	switchNotifyAt  time.Time       // When session switch notification started

	// Last subagent-tree fingerprint; the poll reloads when it changes
	// (fsnotify never sees subagent/workflow writes — see subagentPollCmd)
	subagentSig string

	width  int
	height int
}

// Breakdown-specific messages
type (
	breakdownMsgsMsg struct {
		messages       []models.BreakdownMessage
		totalCost      float64
		minCost        float64
		maxCost        float64
		insights       *models.MessageInsights
		skippedLines   int
		skippedAgents  int
		estimatedCosts int
		hasUnknown     bool
		hasAgents      bool
		workflows      []models.WorkflowMeta
		modTime        time.Time // session file mtime at load; zero if unknown
		// sessionPath identifies the session this load was started for. In
		// follow mode a slow in-flight load for the previous session can land
		// after a switch; the handler drops it when it doesn't match the
		// current session so it can't overwrite the new session's data.
		sessionPath string
	}
	breakdownErrorMsg struct {
		err error
		// sessionPath as in breakdownMsgsMsg: a stale load error for the old
		// session must not stamp itself over the new one after a switch.
		sessionPath string
	}
)

// NewBreakdownModel creates a new breakdown TUI model
func NewBreakdownModel(sessionPath, sessionID string, noColor bool, projectDir string, followMode bool) BreakdownModel {
	closing := &atomic.Bool{}
	closeOnce := &sync.Once{}

	m := BreakdownModel{
		sessionPath: sessionPath,
		sessionID:   sessionID,
		noColor:     noColor,
		loading:     true,
		autoScroll:  true,
		spinner:     newSpinner(noColor),
		done:        make(chan struct{}),
		closing:     closing,
		closeOnce:   closeOnce,
		wg:          &sync.WaitGroup{},
		newMsgKeys:  make(map[string]time.Time),
		projectDir:  projectDir,
		followMode:  followMode,
		agentCache:  analyzer.NewAgentParseCache(),
		project:     projectName(projectDir),
	}
	if sessionPath != "" {
		m.subagentSig = subagentTreeSignature(filepath.Dir(sessionPath), sessionID)
	}
	return m
}

// NewWaitingBreakdownModel opens breakdown before its project has any
// session, as watch's NewWaitingModel does. It waits on projectDir, which may
// not exist yet, and opens the first session created there. projectPath is
// the directory Claude Code will run in, for the header and the waiting line.
func NewWaitingBreakdownModel(projectDir, projectPath string, noColor, followMode bool) BreakdownModel {
	m := NewBreakdownModel("", "", noColor, projectDir, followMode)
	m.loading = false
	m.waitingIn = projectPath
	m.project = paths.BasenameCrossOS(projectPath)
	return m
}

// waiting reports whether breakdown has no session yet.
func (m BreakdownModel) waiting() bool { return m.sessionPath == "" }

// Init initializes the breakdown TUI
func (m BreakdownModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, tickCmd(), subagentPollCmd()}
	if !m.waiting() {
		cmds = append(cmds, m.loadBreakdown, func() tea.Msg { return m.watchFile() })
	}

	// The session watcher runs pinned too, so f can start following
	if m.projectDir != "" {
		cmds = append(cmds, m.startSessionWatcher())
	}

	return tea.Batch(cmds...)
}

// Update handles messages
func (m BreakdownModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// A slow link can deliver a held key as one chunk ("jjjj"), which
		// matches no binding; replay it one key at a time.
		if n := repeatedRune(msg); n > 1 {
			var model tea.Model = m
			var cmds []tea.Cmd
			for range n {
				var cmd tea.Cmd
				model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: msg.Runes[:1]})
				cmds = append(cmds, cmd)
			}
			return model, tea.Batch(cmds...)
		}
		if msg.String() != "p" {
			m.clearSelection()
		}
		switch msg.String() {
		case "q", "ctrl+c":
			shutdownWatchers(m.closeOnce, m.closing, m.done, m.watcher, m.sessionWatcher, m.wg)
			return m, tea.Quit

		case "ctrl+z":
			if canSuspend() {
				return m, tea.Suspend
			}
			return m, nil

		case "r":
			// The view is already live; r is the retry the notify row offers
			// after an error, and a harmless re-read otherwise.
			if m.waiting() {
				// The session watcher is all a waiting view has; if it
				// failed to start, r is the retry.
				if m.sessionWatcher == nil && m.projectDir != "" {
					m.err = nil
					return m, m.startSessionWatcher()
				}
				return m, nil
			}
			m.loading = true
			return m, tea.Batch(m.loadBreakdownCmd(), m.retryWatchNow())

		case "p":
			m.selectNextTop()

		case "-":
			// Back to the previous session, as in watch; it pins there too.
			if m.prevSessionPath != "" {
				m.followMode = false
				return m.switchTo(m.prevSessionPath, m.prevSessionID, true)
			}

		case "f":
			// Following new sessions, as in watch; the header shows the mode
			m.followMode = !m.followMode

		case "g", "home":
			m.setFollow(false)
			m.viewport.GotoTop()

		case "G", "end":
			m.viewport.GotoBottom()
			m.setFollow(true)

		default:
			// The viewport's own keymap: arrows, j/k, PgUp/PgDn, space/b,
			// d/u and ctrl+d/u. Scrolling off the bottom stops auto-scroll;
			// reaching it again resumes.
			before := m.viewport.YOffset
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			// A downward key at the bottom moves nothing but still means
			// "take me to the newest", so it resumes following too.
			if m.viewport.YOffset != before || (m.viewport.AtBottom() && isDownKey(m.viewport.KeyMap, msg)) {
				m.setFollow(m.viewport.AtBottom())
			}
			return m, cmd
		}

	case tea.ResumeMsg:
		// Anything written while suspended arrived without a redraw; reload
		// rather than trust that every change was seen.
		if m.waiting() {
			return m, nil
		}
		m.loading = true
		return m, m.loadBreakdownCmd()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// The chrome's row count depends on the size (see headerRows), so the
		// viewport gets exactly the rows View leaves it.
		vpHeight := viewportHeight(msg.Height, m.headerRows(), breakdownFooterRows)
		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpHeight)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpHeight
		}
		m.viewport.YPosition = m.headerRows()
		m.refreshViewport()
		// A taller viewport can leave the old offset past the end of the
		// content; re-setting it clamps it.
		m.viewport.SetYOffset(m.viewport.YOffset)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case breakdownMsgsMsg:
		if msg.sessionPath != "" && msg.sessionPath != m.sessionPath {
			// A load for a previous session finished after a follow-mode switch;
			// dropping it keeps it from overwriting the new session's data under
			// the new header. Re-arm (idempotent) so the waiter isn't lost. An
			// unstamped (empty) message applies — only hand-built test messages
			// omit the path; real loads always stamp it.
			return m, m.armFileWaiter()
		}
		// Track new messages for highlighting
		m.detectNewMessages(msg.messages)
		if m.pausePending {
			m.pausedAt = len(msg.messages)
			m.pausePending = false
		}

		m.messages = msg.messages
		m.totalCost = msg.totalCost
		m.minCost = msg.minCost
		m.maxCost = msg.maxCost
		m.insights = msg.insights
		m.skippedLines = msg.skippedLines
		m.skippedAgents = msg.skippedAgents
		m.estimatedCosts = msg.estimatedCosts
		m.hasUnknown = msg.hasUnknown
		m.hasAgents = msg.hasAgents
		m.runTags = workflowRunTags(msg.workflows)
		m.lastActivity, m.activityFromFile = breakdownLastActivity(msg.messages, msg.modTime)
		m.loading = false
		m.lastUpdated = time.Now()
		m.err = nil
		m.refreshViewport()
		// Re-arm the file watcher only when no waiter is in flight: this reload
		// may have been poll-triggered, in which case the file-change waiter is
		// still blocked on the watcher
		// Call before return: the arm must mutate the m the caller receives
		armCmd := m.armFileWaiter()
		return m, armCmd

	case breakdownErrorMsg:
		if msg.sessionPath != "" && msg.sessionPath != m.sessionPath {
			// Stale load error for a previous session; see breakdownMsgsMsg.
			return m, m.armFileWaiter()
		}
		m.err = msg.err
		m.loading = false
		m.refreshViewport()
		// Re-arm (if needed) even after an error to continue monitoring
		// Call before return: the arm must mutate the m the caller receives
		armCmd := m.armFileWaiter()
		return m, armCmd

	case fileWatchErrMsg:
		if msg.watcher != m.watcher {
			// Stale message from a superseded watcher; the current waiter
			// accounting doesn't cover it
			return m, nil
		}
		// The file-change waiter exited with a watcher error. Errors like an
		// event-queue overflow mean changes may have been dropped unseen, so
		// reload as well as arming a replacement waiter
		m.err = msg.err
		m.fileWaiterActive = false
		m.loading = true
		armCmd := m.armFileWaiter()
		return m, tea.Batch(m.loadBreakdownCmd(), armCmd)

	case watcherFailedMsg:
		// Another session's attempt, or one that lost a race to a watcher
		// that did start: neither says anything about the current file.
		if msg.sessionPath != m.sessionPath || m.watcher != nil {
			return m, nil
		}
		retry := m.fallback.fail(msg.err)
		return m, retry

	case watchRetryMsg:
		if !m.fallback.retryDue(msg) || m.watcher != nil || m.waiting() {
			return m, nil
		}
		return m, m.watchFile

	case watcherStartedMsg:
		if msg.sessionPath != "" && msg.sessionPath != m.sessionPath {
			// Started for a session the view has since left.
			msg.watcher.Close()
			return m, nil
		}
		// The new watcher sees only writes from now on; one that landed
		// since the last poll would otherwise wait for the next write.
		var catchUp tea.Cmd
		if m.fallback.active() {
			m.loading = true
			catchUp = m.loadBreakdownCmd()
		}
		m.fallback.reset()
		// A replacement watcher (rapid session switches can have two watchFile
		// calls in flight) supersedes the current one: close it so its waiter
		// exits, and account for that exit here since it carries no message
		if m.watcher != nil && m.watcher != msg.watcher {
			m.watcher.Close()
			m.fileWaiterActive = false
		}
		m.watcher = msg.watcher
		// Call before return: the arm must mutate the m the caller receives
		armCmd := m.armFileWaiter()
		return m, tea.Batch(armCmd, catchUp)

	case sessionWatcherStartedMsg:
		// Store the session watcher and start waiting for new sessions
		m.sessionWatcher = msg.watcher
		// A session created between the decision to wait and the watcher's
		// start is already on disk; take it rather than wait for its next write.
		if m.waiting() {
			if path, id := msg.watcher.NewestSession(); path != "" {
				return m.switchTo(path, id, false)
			}
		}
		return m, m.waitForNewSession()

	case sessionWatcherRestartMsg:
		// Session was changed externally, restart waiting
		return m, m.waitForNewSession()

	case fileChangedMsg:
		if msg.watcher != m.watcher {
			// Stale message from a superseded watcher; the current waiter
			// accounting doesn't cover it
			return m, nil
		}
		// The waiter that reported this change has exited; the reload's
		// breakdownMsgsMsg/breakdownErrorMsg arms its replacement
		m.fileWaiterActive = false
		m.loading = true
		return m, m.loadBreakdownCmd()

	case sessionSwitchedMsg:
		// Pinned (f toggles it): stay put and keep watching. Waiting, any
		// session beats none.
		if !m.followMode && !m.waiting() {
			return m, m.waitForNewSession()
		}
		return m.switchTo(msg.newSessionPath, msg.newSessionID, false)

	case tickMsg:
		// Re-render only while highlights are active — an idle table would
		// otherwise be fully re-rendered every tick. Capture before cleanup:
		// the tick that expires the last highlight still needs one final
		// re-render to un-highlight its rows.
		hadHighlights := len(m.newMsgKeys) > 0
		m.cleanupExpiredHighlights()
		if hadHighlights && len(m.messages) > 0 {
			m.refreshViewport()
		}
		return m, tickCmd()

	case subagentPollMsg:
		if m.waiting() {
			return m, subagentPollCmd()
		}
		sig := subagentTreeSignature(filepath.Dir(m.sessionPath), m.sessionID)
		treeChanged := sig != m.subagentSig
		m.subagentSig = sig
		fileChanged := m.fallback.pollChanged(m.sessionPath)
		if treeChanged || fileChanged {
			m.loading = true
			return m, tea.Batch(m.loadBreakdownCmd(), subagentPollCmd())
		}
		return m, subagentPollCmd()
	}

	return m, tea.Batch(cmds...)
}

// switchTo opens another session, dropping everything the old one showed.
// back marks the go-back key, as opposed to following a new session.
func (m BreakdownModel) switchTo(path, id string, back bool) (tea.Model, tea.Cmd) {
	m.prevSessionID, m.prevSessionPath = m.sessionID, m.sessionPath
	m.switchedBack = back
	m.sessionID = id
	m.sessionPath = path
	m.switchNotifyAt = time.Now()

	// Reset state for clean switch. hasUnknown and err must reset too, or the
	// old session's "* = fallback pricing" footnote (and a stale header
	// error) persist under the new session until its first load lands — or
	// indefinitely if that load errors.
	m.messages = nil
	m.insights = nil
	m.hasAgents = false
	m.runTags = nil
	m.totalCost = 0
	m.minCost = 0
	m.maxCost = 0
	m.skippedLines = 0
	m.skippedAgents = 0
	m.estimatedCosts = 0
	m.hasUnknown = false
	m.err = nil
	m.loading = true
	m.newMsgKeys = make(map[string]time.Time)
	m.lastActivity = time.Time{}
	m.activityFromFile = false
	// The new session opens following its newest row, whatever the
	// reader had scrolled to in the old one.
	m.autoScroll = true
	m.pausedAt = 0
	m.pausePending = false
	m.selectedKey = ""
	m.lineRows = nil
	if m.ready {
		m.viewport.SetContent("")
		m.viewport.GotoTop()
	}
	// Drop the previous session's cached agent parses
	m.agentCache = analyzer.NewAgentParseCache()
	// Fingerprint the new session's subagent tree; the pending reload
	// covers anything already on disk
	m.subagentSig = subagentTreeSignature(filepath.Dir(m.sessionPath), m.sessionID)

	// Stop old file watcher, will be restarted by watchFile. Closing it
	// unblocks the old waiter, which exits without a message, so the
	// in-flight flag resets here for the new watcher's waiter
	if m.watcher != nil {
		m.watcher.Close()
		m.watcher = nil
	}
	m.fileWaiterActive = false
	// The new file gets its own watcher attempt, and its own backoff
	m.fallback.reset()

	// Update session watcher's current session
	if m.sessionWatcher != nil {
		m.sessionWatcher.SetCurrentSession(m.sessionID)
	}

	// Reload data, restart file watcher, and restart session watcher
	// We must explicitly restart waitForNewSession because the goroutine that
	// detected this switch has already exited after returning sessionSwitchedMsg
	cmds := []tea.Cmd{m.loadBreakdownCmd(), func() tea.Msg { return m.watchFile() }}
	if m.sessionWatcher != nil {
		cmds = append(cmds, m.waitForNewSession())
	}
	return m, tea.Batch(cmds...)
}

// breakdownMsgKey identifies a message by content, not position, so highlight
// tracking survives the per-reload timestamp sort and 1..N reindex. Two rows
// with identical timestamp, agent, model, usage, and cost are indistinguishable
// anyway; collisions only affect a 2s cosmetic highlight.
func breakdownMsgKey(msg models.BreakdownMessage) string {
	u := msg.Usage
	var e5m, e1h int64
	if u.CacheCreation != nil {
		e5m = u.CacheCreation.Ephemeral5mInputTokens
		e1h = u.CacheCreation.Ephemeral1hInputTokens
	}
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d\x00%.10f",
		msg.Timestamp.UTC().Format(time.RFC3339Nano), msg.AgentID, msg.Model,
		u.InputTokens, u.OutputTokens, u.CacheCreationInputTokens, u.CacheReadInputTokens,
		e5m, e1h, msg.Cost.TotalCost)
}

// detectNewMessages flags messages whose identity is absent from the currently
// displayed set as new. Keying on identity (not index) means a mid-list agent
// insertion highlights only the inserted row, not the old tail rows the sort
// shifted past the old count.
func (m *BreakdownModel) detectNewMessages(newMessages []models.BreakdownMessage) {
	// Nothing to diff against on first load (or right after a session switch):
	// flagging every row would flash the whole table as "new"
	if len(m.messages) == 0 {
		return
	}

	seen := make(map[string]struct{}, len(m.messages))
	for _, msg := range m.messages {
		seen[breakdownMsgKey(msg)] = struct{}{}
	}

	now := time.Now()
	for _, msg := range newMessages {
		key := breakdownMsgKey(msg)
		if _, ok := seen[key]; !ok {
			m.newMsgKeys[key] = now
		}
	}
}

// cleanupExpiredHighlights removes highlight entries older than highlightDuration.
// This also bounds the map: keys for messages whose content changed (or that
// vanished) are pruned once their window lapses.
func (m *BreakdownModel) cleanupExpiredHighlights() {
	for key, addedAt := range m.newMsgKeys {
		if time.Since(addedAt) > highlightDuration {
			delete(m.newMsgKeys, key)
		}
	}
}

// isNewMessage checks if a message should be highlighted as new
func (m *BreakdownModel) isNewMessage(msg models.BreakdownMessage) bool {
	if m.noColor {
		return false
	}
	addedAt, exists := m.newMsgKeys[breakdownMsgKey(msg)]
	if !exists {
		return false
	}
	return time.Since(addedAt) < highlightDuration
}

// View renders the breakdown TUI. Its row count must match headerRows and
// breakdownFooterRows exactly, or the frame outgrows the terminal.
func (m BreakdownModel) View() string {
	if m.tooSmall() {
		return clipToWidth("  terminal too small", m.width)
	}
	// The layout walks every row; work it out once per frame.
	layout := m.layout()
	panelWidth := m.frameWidth(layout)
	compact := m.compact()

	var lines []string
	if compact {
		lines = append(lines, "  "+fitStatusHeader(m.headerParams(panelWidth), panelWidth))
	} else {
		lines = append(lines, strings.Split(m.renderHeaderPanel(panelWidth), "\n")...)
	}
	// The insights row is reserved even before insights load, and the notify
	// row even when there's nothing to say, so nothing below them moves.
	insights := ""
	if m.insights != nil && len(m.messages) > 0 {
		insights = m.renderCompactInsights()
	}
	notify := m.renderNotifyRow()
	switch {
	case !compact:
		lines = append(lines, insights, notify)
	case notify != "":
		// The compact frame has no notify row; an error or switch notice
		// outranks the insights for the moment it shows.
		lines = append(lines, notify)
	default:
		lines = append(lines, insights)
	}
	lines = append(lines, m.renderTableHeader(layout))
	if !compact {
		lines = append(lines, m.renderTableSeparator(panelWidth))
	}
	if m.ready {
		lines = append(lines, m.viewport.View())
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, m.renderFooterRule(panelWidth), m.renderStatsLine(), m.renderHelpLine())

	return clipToWidth(strings.Join(lines, "\n"), m.width)
}

// renderNotifyRow is a load error in words, then p's selection, the switch
// notice for a few seconds after following a new session or going back, or
// a missing file watcher, and blank otherwise. The watcher notice comes last
// because the others are brief and answer something the reader just did or
// saw.
func (m BreakdownModel) renderNotifyRow() string {
	if m.err != nil {
		text := styles.Warning + " " + describeErr(m.err)
		if len(m.messages) > 0 {
			text += ", showing last data"
		}
		text += " " + styles.Bullet + " r to retry"
		if m.noColor {
			return "  " + text
		}
		return "  " + lipgloss.NewStyle().Foreground(styles.ErrorColor).Render(text)
	}
	if text := m.selectionText(); text != "" {
		if m.noColor {
			return "  " + text
		}
		return "  " + lipgloss.NewStyle().Foreground(styles.HighlightColor).Render(text)
	}
	if !m.switchNotifyAt.IsZero() && time.Since(m.switchNotifyAt) < switchNotifyDuration {
		lead := "Switched to new session"
		if m.switchedBack {
			lead = "Switched back to " + render.TruncateID(m.sessionID, sessionIDDisplayLen)
		}
		tail := ""
		if m.prevSessionPath != "" {
			tail = " " + styles.Bullet + " - to go back"
		}
		if m.noColor {
			return "  [" + lead + "]" + tail
		}
		return "  " + lipgloss.NewStyle().Foreground(styles.HighlightColor).Bold(true).Render(lead) + lipgloss.NewStyle().Foreground(styles.HighlightColor).Render(tail)
	}
	if m.fallback.active() {
		text := m.fallback.notice(m.panelWidth())
		if m.noColor {
			return "  " + text
		}
		return "  " + lipgloss.NewStyle().Foreground(styles.WarningColor).Render(text)
	}
	return ""
}

// renderStatsLine is the footer's totals, plus anything the totals can't
// account for.
func (m BreakdownModel) renderStatsLine() string {
	sep := " " + styles.BoxVerticalSep + " "
	if m.noColor {
		line := fmt.Sprintf("  Messages: %d%sTotal: %s", len(m.messages), sep, render.Cost(m.totalCost))
		if note := accountingFootnote(m.skippedAgents, m.skippedLines, m.estimatedCosts); note != "" {
			line += sep + note
		}
		if m.hasUnknown {
			line += sep + unknownModelFootnote(m.unknownModelIDs())
		}
		return line
	}

	lightGray := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	sepStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(sep)
	warnStyle := lipgloss.NewStyle().Foreground(styles.WarningColor)

	var sb strings.Builder
	sb.WriteString("  ")
	sb.WriteString(lightGray.Render(fmt.Sprintf("Messages: %d", len(m.messages))))
	sb.WriteString(sepStyled)
	sb.WriteString(lightGray.Render("Total: "))
	sb.WriteString(lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(render.Cost(m.totalCost)))
	// Surface parse warnings so an incomplete breakdown doesn't look complete
	if note := accountingFootnote(m.skippedAgents, m.skippedLines, m.estimatedCosts); note != "" {
		sb.WriteString(sepStyled)
		sb.WriteString(warnStyle.Render(note))
	}
	// Explain the MODEL-column asterisk: those rows are fallback-priced
	if m.hasUnknown {
		sb.WriteString(sepStyled)
		sb.WriteString(warnStyle.Render(unknownModelFootnote(m.unknownModelIDs())))
	}
	return sb.String()
}

// renderHelpLine is the key-hint row.
func (m BreakdownModel) renderHelpLine() string {
	// watch's line, plus p, which only breakdown has. r isn't listed: the
	// view is already live, and the notify row offers it as a retry.
	helpText := helpLine("q quit", "j/k scroll", "space/b page", "g/G top/bottom", "f follow", "p peak")
	if !m.noColor {
		helpText = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(helpText)
	}
	return "  " + helpText
}

// renderHeaderPanel renders the boxed live header, in the same form as watch's.
func (m BreakdownModel) renderHeaderPanel(width int) string {
	return renderLiveHeaderPanel(m.headerParams(width))
}

// headerParams fills the shared live header the way watch does, so the two
// TUIs' headers read the same.
func (m BreakdownModel) headerParams(width int) liveHeaderParams {
	mode := "PINNED"
	if m.followMode {
		mode = "FOLLOWING"
	}
	return liveHeaderParams{
		sessionID:    m.sessionID,
		loading:      m.loading,
		err:          m.err,
		spinnerView:  m.spinner.View(),
		noColor:      m.noColor,
		width:        width,
		project:      m.project,
		mode:         mode,
		waiting:      m.waiting(),
		lastActivity: m.lastActivity,
		noMessages:   m.activityFromFile,
		now:          m.clock(),
	}
}

// Breakdown's insights cover the merged parent+agent messages, unlike
// show's and watch's parent-only insights, so the figures can legitimately
// differ; when agents ran, the line says so. The short form is for a line
// too narrow for the long one. It answers watch's "agents excluded" and
// can't be read as a count.
const (
	insightsScope      = "main conversation + agents"
	insightsScopeShort = "incl. agents"
)

// renderCompactInsights renders a single line of insights:
//
//	main conversation + agents │ Peak: #231 $0.4419 @ 20:44:29 [Aa499eb9] (5.9x avg) │ Trend: ═ flat
//
// When the line doesn't fit, detail goes in this order: the long scope label
// for the short one, the Peak's multiplier, its time and agent marker, then
// the Trend, then the scope. The Trend goes late because nothing else on
// screen says it, and it never shows without the scope, since the scope is
// why it can differ from watch's.
func (m BreakdownModel) renderCompactInsights() string {
	// The scope qualifies the figures; with neither figure it says nothing.
	if m.insights == nil || (m.insights.HighestCost == nil && !m.insights.HasTrend()) {
		return ""
	}
	style := func(text string, color lipgloss.Color) string {
		if m.noColor || text == "" {
			return text
		}
		return lipgloss.NewStyle().Foreground(color).Render(text)
	}

	// The Peak, from most detail to least. The row number and cost find the
	// row, so they are what always stays; the time to the second and the
	// agent marker match it by eye.
	peaks := []string{""}
	if peak := m.insights.HighestCost; peak != nil {
		short := fmt.Sprintf("Peak: #%d %s", peak.Index, render.Cost(peak.Cost))
		timed := short + " @ " + render.Clock(peak.Timestamp)
		if marker := m.peakAgentMarker(); marker != "" {
			timed += " " + marker
		}
		full := timed + fmt.Sprintf(" (%.1fx avg)", m.insights.CostMultiplier())
		peaks = []string{full, timed, short}
		for i := range peaks {
			peaks[i] = style(peaks[i], styles.WarningColor)
		}
	}

	// Trend (only once the analyzer actually computed one — see HasTrend)
	trends := []string{""}
	if m.insights.HasTrend() {
		color := styles.SecondaryColor
		switch m.insights.CostTrend {
		case models.TrendIncreasing:
			color = styles.WarningColor
		case models.TrendDecreasing:
			color = styles.SuccessColor
		}
		trendStr := fmt.Sprintf("Trend: %s %s", render.TrendSymbol(m.insights.CostTrend), m.insights.TrendDescription())
		trends = []string{style(trendStr, color), ""}
	}

	// Without agents the two scopes are the same messages: no label.
	scopes := []string{""}
	if m.hasAgents {
		scopes = []string{style(insightsScope, styles.SecondaryColor), style(insightsScopeShort, styles.SecondaryColor)}
	}

	sep := " " + styles.BoxVerticalSep + " "
	width := m.width - 2
	join := func(parts ...string) string {
		var kept []string
		for _, p := range parts {
			if p != "" {
				kept = append(kept, p)
			}
		}
		return strings.Join(kept, sep)
	}
	for _, trend := range trends {
		for i, scope := range scopes {
			variants := peaks
			// The long label is worth less than any of the Peak's detail.
			if i == 0 && len(scopes) > 1 {
				variants = peaks[:1]
			}
			for _, peak := range variants {
				if line := join(scope, peak, trend); width <= 0 || lipgloss.Width(line) <= width {
					return "  " + line
				}
			}
		}
	}
	// Narrower than the least a scoped line needs: the bare Peak, clipped by
	// the frame if it must be.
	return "  " + peaks[len(peaks)-1]
}

// joinSegments joins parts with sep, leaving off whole trailing parts that
// would run past width, so a narrow terminal loses a segment rather than
// cutting one mid-word. The first part always stays, to be clipped if it must.
// width <= 0 means unknown, and keeps every part.
func joinSegments(parts []string, sep string, width int) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		next := out + sep + p
		if width > 0 && lipgloss.Width(next) > width {
			break
		}
		out = next
	}
	return out
}

// peakAgentMarker returns the agent marker of the Peak row, or "" when a
// parent message is the peak. The snapshot's Index is the row's display
// Index; the timestamp check guards against a snapshot and a message list
// that came from different loads.
func (m BreakdownModel) peakAgentMarker() string {
	peak := m.insights.HighestCost
	if peak == nil || peak.Index < 1 || peak.Index > len(m.messages) {
		return ""
	}
	row := m.messages[peak.Index-1]
	if !row.Timestamp.Equal(peak.Timestamp) {
		return ""
	}
	return agentMarker(row.AgentID)
}

// renderTableHeader renders the table header row
func (m BreakdownModel) renderTableHeader(layout breakdownLayout) string {
	header := layout.header()
	if !m.noColor {
		return headerStyle.Render(header)
	}
	return header
}

// renderTableSeparator renders the table separator rule, dimmed unless color
// is off.
func (m BreakdownModel) renderTableSeparator(panelWidth int) string {
	sep := "  " + strings.Repeat(styles.LineHorizontal, panelWidth)
	if !m.noColor {
		return tableBorderStyle.Render(sep)
	}
	return sep
}

// renderTableContent renders all message rows for the viewport, and for each
// line the display Index of the message on it (0 for a day divider).
func (m BreakdownModel) renderTableContent() (string, []int) {
	var sb strings.Builder
	layout := m.layout()
	lineRows := make([]int, 0, len(m.messages))

	for i, msg := range m.messages {
		// Rows carry only a time, so mark where the local day changes. A
		// zero timestamp (unparseable in the transcript) has no day to mark.
		if i > 0 && !msg.Timestamp.IsZero() && !m.messages[i-1].Timestamp.IsZero() &&
			!render.SameLocalDay(m.messages[i-1].Timestamp, msg.Timestamp) {
			sb.WriteString(m.renderDayMarker(msg.Timestamp))
			sb.WriteString("\n")
			lineRows = append(lineRows, 0)
		}
		highlight := m.isNewMessage(msg) || (m.selectedKey != "" && !m.noColor && breakdownMsgKey(msg) == m.selectedKey)
		sb.WriteString(m.renderRow(msg, highlight, layout))
		lineRows = append(lineRows, msg.Index)
		if i < len(m.messages)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String(), lineRows
}

// renderDayMarker renders the divider row placed above the first message of a
// new local day.
func (m BreakdownModel) renderDayMarker(t time.Time) string {
	marker := styles.GroupRule + " " + render.DayMarker(t) + " " + styles.GroupRule
	if m.noColor {
		return "  " + marker
	}
	return "  " + dimStyle.Render(marker)
}

// agentMarker is the AGENT cell's ID part: the same [A<id>] marker watch,
// show and summary print, so a row can be matched to its agent there.
func agentMarker(agentID string) string {
	if agentID == "" {
		return ""
	}
	return "[A" + render.ShortAgentID(agentID) + "]"
}

// runTag returns the short workflow-run marker for a message, or "" for
// parent and regular subagent rows.
func (m BreakdownModel) runTag(msg models.BreakdownMessage) string {
	if msg.WorkflowID == "" {
		return ""
	}
	if tag, ok := m.runTags[msg.WorkflowID]; ok {
		return tag
	}
	return workflowRunTag("")
}

// agentCell is the AGENT cell text: the marker, then the run tag for a
// workflow agent when withTag is set.
func (m BreakdownModel) agentCell(msg models.BreakdownMessage, withTag bool) string {
	marker := agentMarker(msg.AgentID)
	if tag := m.runTag(msg); withTag && tag != "" && marker != "" {
		return marker + " " + tag
	}
	return marker
}

// layout picks the columns this frame draws. See breakdownLayout.
func (m BreakdownModel) layout() breakdownLayout {
	markerWidth, cellWidth, lastIndex, modelWant := 0, 0, 0, 0
	for _, msg := range m.messages {
		markerWidth = max(markerWidth, len(agentMarker(msg.AgentID)))
		cellWidth = max(cellWidth, len(m.agentCell(msg, true)))
		lastIndex = max(lastIndex, msg.Index)
		if !pricing.IsKnownModel(msg.Model) {
			modelWant = max(modelWant, utf8.RuneCountInString(pricing.GetModelDisplayName(msg.Model))+len(unknownModelMarker))
		}
	}
	return newBreakdownLayout(len(strconv.Itoa(lastIndex)), markerWidth, cellWidth, m.width).widenModel(modelWant, m.width)
}

// renderRow renders a single message row
func (m BreakdownModel) renderRow(msg models.BreakdownMessage, isNew bool, layout breakdownLayout) string {
	modelName := pricing.GetModelDisplayName(msg.Model)
	// Flag fallback-priced rows inline; the footer explains the marker. Clamp
	// first so a long raw ID can't push it out of the column (or off it).
	modelLabel := render.ClampModel(modelName, layout.modelWidth())
	if !pricing.IsKnownModel(msg.Model) {
		modelLabel = render.ClampModel(modelName, layout.modelWidth()-1) + unknownModelMarker
	}
	marker := agentMarker(msg.AgentID)

	c := breakdownCells{
		index: fmt.Sprintf("%-*d", layout.indexWidth, msg.Index),
		time:  fmt.Sprintf("%-*s", bdTimeWidth, render.Clock(msg.Timestamp)),
		agent: fmt.Sprintf("%-*s", layout.agentWidth, m.agentCell(msg, layout.runTags)),
		model: fmt.Sprintf("%-*s", layout.modelWidth(), modelLabel),
		cost:  render.CostCell(msg.Cost.TotalCost, bdCostWidth),
		in:    fmt.Sprintf("%*s", bdInWidth, render.Number(msg.Usage.InputTokens)),
		out:   fmt.Sprintf("%*s", bdOutWidth, render.Number(msg.Usage.OutputTokens)),
		cacheWrite: fmt.Sprintf("%*s", bdCacheWidth,
			render.Number(msg.Usage.CacheCreationInputTokens)),
		cacheRead: fmt.Sprintf("%*s", bdCacheWidth, render.Number(msg.Usage.CacheReadInputTokens)),
	}

	if m.noColor {
		return layout.join(c)
	}

	// Each cell is styled after padding, so a highlighted row keeps exactly
	// the column positions of a settled one.
	if isNew {
		h := styles.HighlightStyle
		return layout.join(breakdownCells{
			index: h.Render(c.index), time: h.Render(c.time), agent: h.Render(c.agent),
			model: h.Render(c.model), cost: h.Render(c.cost), in: h.Render(c.in),
			out: h.Render(c.out), cacheWrite: h.Render(c.cacheWrite), cacheRead: h.Render(c.cacheRead),
		})
	}

	// The agent marker takes the agent's hashed color (as in watch); the run
	// tag and the padding after it stay dim.
	agent := c.agent
	if marker != "" {
		pad := c.agent[len(marker):]
		agent = lipgloss.NewStyle().Foreground(styles.GetAgentColor(msg.AgentID)).Render(marker) + dimStyle.Render(pad)
	}
	costColor := styles.GetCostGradientColor(msg.Cost.TotalCost, m.minCost, m.maxCost)
	return layout.join(breakdownCells{
		index:      dimStyle.Render(c.index),
		time:       dimStyle.Render(c.time),
		agent:      agent,
		model:      lipgloss.NewStyle().Foreground(styles.GetModelColor(modelName)).Render(c.model),
		cost:       render.CostColored(msg.Cost.TotalCost, costColor, bdCostWidth),
		in:         c.in, // Input stays white/default
		out:        lipgloss.NewStyle().Foreground(styles.OutputTokenColor).Render(c.out),
		cacheWrite: lipgloss.NewStyle().Foreground(styles.CacheWriteTokenColor).Render(c.cacheWrite),
		cacheRead:  lipgloss.NewStyle().Foreground(styles.CacheReadTokenColor).Render(c.cacheRead),
	})
}

// Commands

func (m BreakdownModel) loadBreakdown() tea.Msg {
	if m.closing != nil && m.closing.Load() {
		return nil
	}
	result, err := analyzer.GetBreakdownMessagesWithCache(m.sessionPath, m.sessionID, m.agentCache)
	if err != nil {
		return breakdownErrorMsg{err: err, sessionPath: m.sessionPath}
	}
	messages := result.Messages
	var modTime time.Time
	if info, err := os.Stat(m.sessionPath); err == nil {
		modTime = info.ModTime()
	}

	// Calculate total cost and min/max cost for the gradient. Insights are
	// order-sensitive and come from the analyzer, computed over the file-order
	// merged list; these aggregates are order-insensitive so the
	// display-sorted list is fine.
	var totalCost float64
	var minCost, maxCost float64
	hasUnknown := false
	hasAgents := false

	for i, msg := range messages {
		totalCost += msg.Cost.TotalCost
		if !hasUnknown && !pricing.IsKnownModel(msg.Model) {
			hasUnknown = true
		}
		if msg.AgentID != "" {
			hasAgents = true
		}

		// Track min/max for cost gradient
		if i == 0 {
			minCost = msg.Cost.TotalCost
			maxCost = msg.Cost.TotalCost
		} else {
			if msg.Cost.TotalCost < minCost {
				minCost = msg.Cost.TotalCost
			}
			if msg.Cost.TotalCost > maxCost {
				maxCost = msg.Cost.TotalCost
			}
		}
	}

	return breakdownMsgsMsg{
		messages:       messages,
		totalCost:      totalCost,
		minCost:        minCost,
		maxCost:        maxCost,
		insights:       result.Insights,
		skippedLines:   result.SkippedLines,
		skippedAgents:  result.SkippedAgents,
		estimatedCosts: result.EstimatedCostMessages,
		hasUnknown:     hasUnknown,
		hasAgents:      hasAgents,
		workflows:      result.Workflows,
		modTime:        modTime,
		sessionPath:    m.sessionPath,
	}
}

func (m BreakdownModel) loadBreakdownCmd() tea.Cmd {
	return func() tea.Msg {
		return m.loadBreakdown()
	}
}

// wrapErr lets the shared watcher commands report failures as this model's
// error message.
func (m BreakdownModel) wrapErr(err error) tea.Msg {
	return breakdownErrorMsg{err: err, sessionPath: m.sessionPath}
}

func (m BreakdownModel) watchFile() tea.Msg { return watchFileCmd(m.sessionPath) }

// retryWatchNow is r's watcher retry; see Model.retryWatchNow.
func (m BreakdownModel) retryWatchNow() tea.Cmd {
	if !m.fallback.active() || m.watcher != nil {
		return nil
	}
	return m.watchFile
}

// armFileWaiter starts a file-change waiter unless one is already blocked on
// the watcher; see Model.armFileWaiter. Returns nil when a waiter is already
// in flight.
func (m *BreakdownModel) armFileWaiter() tea.Cmd {
	if m.watcher == nil || m.fileWaiterActive {
		return nil
	}
	m.fileWaiterActive = true
	return waitForFileChangeCmd(m.wg, m.closing, m.watcher, m.done, m.sessionPath)
}

func (m BreakdownModel) startSessionWatcher() tea.Cmd {
	return startSessionWatcherCmd(m.projectDir, m.sessionID, m.wrapErr)
}

func (m BreakdownModel) waitForNewSession() tea.Cmd {
	return waitForNewSessionCmd(m.wg, m.sessionWatcher)
}
