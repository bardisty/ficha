package tui

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
)

// Highlight duration for changed values
const highlightDuration = 2 * time.Second

// watchDebounce coalesces rapid fsnotify write events into a single reload.
const watchDebounce = 50 * time.Millisecond

// Duration to show "Switched to new session" notification
const switchNotifyDuration = 5 * time.Second

// Chart display constants
const (
	chartHeight        = 6    // Height of the sparkline chart in characters
	chartWidth         = 60   // Default width of the chart (adjusted on resize)
	maxCostHistorySize = 1000 // Max entries in cost history to prevent unbounded growth
)

// Model is the Bubbletea model for the TUI
type Model struct {
	sessionPath string
	sessionID   string
	noColor     bool

	analysis    *models.SessionAnalysis
	err         error
	loading     bool
	lastUpdated time.Time

	// agentCache memoizes agent sub-session parses so a reload triggered by a
	// parent-file write doesn't re-parse every unchanged agent.
	agentCache *analyzer.AgentParseCache

	// Change tracking for highlight animation
	changedAt   map[string]time.Time
	deltaTokens map[string]int64 // Delta values for token counts
	deltaCount  int              // Delta for message count

	// Viewport for scrolling. watch is a dashboard, not a log: it opens at the
	// top and keeps the reader's position across reloads and resizes.
	viewport viewport.Model
	ready    bool // viewport initialized

	// now stands in for time.Now where the view reads the wall clock (the
	// rolling rate); nil means time.Now. Tests pin it.
	now func() time.Time

	spinner   spinner.Model
	watcher   *fsnotify.Watcher
	done      chan struct{}   // Channel to signal watcher goroutine to stop
	closing   *atomic.Bool    // Atomic flag for shutdown coordination
	closeOnce *sync.Once      // Ensure shutdown happens exactly once
	wg        *sync.WaitGroup // Waits for goroutines to drain on shutdown

	// A file-change waiter is blocked on the watcher; gates armFileWaiter so
	// poll-triggered reloads can't stack extra waiters
	fileWaiterActive bool

	// fallback polls the session file while its watcher can't be created
	fallback watchFallback

	// fileSig is the session file's sessionFileSig when the last load
	// started; the poll reloads when the file no longer matches it
	fileSig string

	// Session following. The session watcher runs in every mode: following
	// switches to a newly created session, and pinned shows it as a hint.
	projectDir      string          // Project directory to watch for new sessions
	project         string          // Project name for the header and title; "" when unknown
	followMode      bool            // Whether to auto-follow new sessions
	prevSessionID   string          // Session watched before the last switch
	prevSessionPath string          // Its file, for the go-back key (-)
	sessionWatcher  *SessionWatcher // Watches for other sessions' files
	switched        *switchNotice   // Shown until the next keypress
	hint            *sessionHint    // Another session's activity, not followed
	windowTitle     string          // Last title sent, to send only changes

	// waitingIn is the directory a waiting model (no session yet) waits for
	// Claude Code to start in; see NewWaitingModel.
	waitingIn string

	// ticking is set while the 100ms highlight tick is scheduled; it runs
	// only while a highlight is fading, so an idle view doesn't redraw ten
	// times a second.
	ticking bool

	// clockGen identifies the live clockMsg chain; a tick from an older
	// chain ends it.
	clockGen int

	// lastActivity is the newest message timestamp (parent or agent), else
	// the session file's mtime (activityFromFile); the header ages it.
	lastActivity     time.Time
	activityFromFile bool

	// runningAgents is runningAgentsKey as of the last content render; a
	// clock tick that sees it change re-renders the agent list's dots and
	// folds.
	runningAgents string

	// Cost trend chart
	costChart   sparkline.Model // Sparkline chart for cost trend
	costHistory []float64       // Rolling window of per-message costs (parent + agents, chronological)

	// Last subagent-tree fingerprint; the poll reloads when it changes
	// (fsnotify never sees subagent/workflow writes — see subagentPollCmd)
	subagentSig string

	width  int
	height int
}

// Messages
type (
	// analysisMsg carries a completed reload plus the session it was loaded for.
	// In follow mode a slow in-flight load for the previous session can land
	// after a switch; the handler drops it when sessionPath doesn't match the
	// current session, so it can't overwrite the new session's data.
	analysisMsg struct {
		analysis    *models.SessionAnalysis
		sessionPath string
		modTime     time.Time // session file mtime; zero when unknown
	}
	// errorMsg carries a reload/watcher failure and the session it belongs to;
	// a stale error for the old session must not stamp over the new one.
	errorMsg struct {
		err         error
		sessionPath string
	}
	// fileChangedMsg reports a session-file change seen by a specific watcher.
	// The watcher identifies the message's origin: handlers drop messages from
	// a superseded (closed) watcher so stale ones can't corrupt the
	// waiter-in-flight accounting
	fileChangedMsg struct {
		watcher *fsnotify.Watcher
	}
	tickMsg time.Time
)

// NewModel creates a new TUI model
func NewModel(sessionPath, sessionID string, noColor bool, projectDir string, followMode bool) Model {
	// Initialize cost trend chart with default dimensions
	// Will be resized when we receive the first WindowSizeMsg
	chart := newCostChart(chartWidth, noColor)

	closing := &atomic.Bool{}
	closeOnce := &sync.Once{}
	m := Model{
		sessionPath: sessionPath,
		sessionID:   sessionID,
		noColor:     noColor,
		loading:     true,
		spinner:     newSpinner(noColor),
		done:        make(chan struct{}),
		closing:     closing,
		closeOnce:   closeOnce,
		wg:          &sync.WaitGroup{},
		changedAt:   make(map[string]time.Time),
		deltaTokens: make(map[string]int64),
		projectDir:  projectDir,
		followMode:  followMode,
		costChart:   chart,
		costHistory: make([]float64, 0),
		agentCache:  analyzer.NewAgentParseCache(),
		project:     projectName(projectDir),
	}
	if sessionPath != "" {
		m.subagentSig = subagentTreeSignature(filepath.Dir(sessionPath), sessionID)
		// Init's load starts after this and reads at least as much.
		m.fileSig = sessionFileSig(sessionPath)
	}
	return m
}

// NewWaitingModel opens watch before its project has any session. It waits
// on projectDir, which may not exist yet, and follows the first session
// created there. projectPath is the directory Claude Code will run in, for
// the header and the waiting message.
func NewWaitingModel(projectDir, projectPath string, noColor, followMode bool) Model {
	m := NewModel("", "", noColor, projectDir, followMode)
	m.loading = false
	m.waitingIn = projectPath
	m.project = paths.BasenameCrossOS(projectPath)
	return m
}

// waiting reports whether watch has no session yet.
func (m Model) waiting() bool { return m.sessionPath == "" }

// projectName is the last element of the project's real directory, or ""
// when the storage directory doesn't record it.
func projectName(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	p := parser.ProjectOriginalPath(projectDir)
	if p == "" {
		return ""
	}
	return paths.BasenameCrossOS(p)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Repeats that arrive in one input chunk (a held key over a slow
		// link) come as one message, "jjjj"; take them one at a time.
		if runes := msg.Runes; msg.Type == tea.KeyRunes && !msg.Paste && len(runes) > 1 && strings.Count(string(runes), string(runes[0])) == len(runes) {
			var cmds []tea.Cmd
			var model tea.Model = m
			for range runes {
				var cmd tea.Cmd
				model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: runes[:1]})
				cmds = append(cmds, cmd)
			}
			return model, tea.Batch(cmds...)
		}

		// The switch notice explains a total that changed under the reader;
		// any key acknowledges it.
		m.switched = nil
		switch msg.String() {
		case "q", "ctrl+c":
			shutdownWatchers(m.closeOnce, m.closing, m.done, m.watcher, m.sessionWatcher, m.wg)
			return m, tea.Quit
		case "r":
			// The view is already live; r is the retry the notify row offers
			// after an error, and a harmless re-read otherwise.
			if m.waiting() {
				// The session watcher is all a waiting view has; if it
				// failed to start, r is the retry the notify row offers.
				if m.sessionWatcher == nil && m.projectDir != "" {
					m.err = nil
					return m, m.startSessionWatcher()
				}
				return m, nil
			}
			load := m.startLoad()
			return m, tea.Batch(load, m.spinnerCmd(), m.retryWatchNow())

		case "ctrl+z":
			if canSuspend() {
				return m, tea.Suspend
			}
			return m, nil

		case "-":
			// Back to the previous session, like cd -. breakdown has the same
			// key, and p there means peak, so neither view uses p for this.
			// Going back is a deliberate choice of session, so it pins.
			if m.prevSessionPath != "" {
				m.followMode = false
				return m.switchTo(m.prevSessionPath, m.prevSessionID, false)
			}

		case "n":
			if m.hintVisible() {
				return m.switchTo(m.hint.path, m.hint.id, false)
			}

		case "f":
			m.followMode = !m.followMode
			if h := followTarget(m.hint, m.clock()); m.followMode && h != nil {
				return m.switchTo(h.path, h.id, false)
			}

		case "g", "home":
			m.viewport.GotoTop()

		case "G", "end":
			m.viewport.GotoBottom()

		default:
			// Everything else goes to the viewport's pager keymap: j/k and
			// arrows, space/f/pgdown and b/pgup by page, d/u and ctrl+d/u by
			// half page. f is taken above for follow.
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

	case tea.ResumeMsg:
		// Back from ctrl+z: the watchers kept their events, but reload in
		// case the session moved on while the process was stopped.
		if m.waiting() {
			return m, nil
		}
		load := m.startLoad()
		return m, tea.Batch(load, m.spinnerCmd())

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			m.viewport = viewport.New(msg.Width, 1)
			m.viewport.YPosition = watchHeaderHeight
			m.ready = true
		}
		m.viewport.Width = msg.Width
		m.layoutViewport()

		// Re-window the cost chart to the new width. A plain Resize would keep the
		// old width's pushed points (and its scale), so the drawn window and the
		// min/max/count labels would disagree with the new width — rebuild from
		// costHistory so the visible tail, scale, and labels all track the resize.
		m.rebuildCostChart()

		// Re-render content with new dimensions
		if m.analysis != nil || m.waiting() {
			m.refreshContent()
		}

	case spinner.TickMsg:
		// Let the spinner stop once nothing shows it; spinnerCmd restarts it.
		if !m.showLoading() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case clockMsg:
		// The redraw after every message re-reads the clock for the header's
		// age, the rate and the hint's expiry. The body is rendered content,
		// so it's redone only when an agent went quiet.
		if msg.gen != m.clockGen {
			return m, nil // superseded by a faster chain; see analysisMsg
		}
		if m.ready && runningAgentsKey(m.analysis, m.clock()) != m.runningAgents {
			m.refreshContent()
		}
		return m, clockCmd(m.clockInterval(), m.clockGen)

	case analysisMsg:
		if msg.sessionPath != "" && msg.sessionPath != m.sessionPath {
			// A load for a previous session finished after a follow-mode switch;
			// dropping it keeps it from overwriting the new session's data under
			// the new header. Re-arm (idempotent) so the waiter isn't lost. An
			// unstamped (empty) message applies — only hand-built test messages
			// omit the path; real loads always stamp it.
			return m, m.armFileWaiter()
		}
		// Detect changes and mark them for highlighting
		// Compare current analysis (before update) with new analysis
		var tick tea.Cmd
		if m.analysis != nil && msg.analysis != nil {
			m.detectChanges(m.analysis, msg.analysis)
			if len(m.changedAt) > 0 && !m.ticking {
				m.ticking = true
				tick = tickCmd()
			}
		}
		m.analysis = msg.analysis
		m.loading = false
		m.lastUpdated = time.Now()
		m.err = nil
		slowClock := m.clockInterval() > time.Second
		m.lastActivity, m.activityFromFile = lastActivity(msg.analysis, msg.modTime)
		// A message after an idle stretch: the pending clock tick is up to
		// 15s out, so start a 1s chain now for the seconds count and let the
		// slow one lapse.
		var clock tea.Cmd
		if slowClock && m.clockInterval() == time.Second {
			m.clockGen++
			clock = clockCmd(time.Second, m.clockGen)
		}

		// Update cost chart with new message costs
		m.updateCostChart()

		// Update viewport content
		if m.ready {
			m.refreshContent()
		}
		// Re-arm the file watcher only when no waiter is in flight: this reload
		// may have been poll-triggered, in which case the file-change waiter is
		// still blocked on the watcher
		// Call before return: the arm must mutate the m the caller receives
		armCmd := m.armFileWaiter()
		return m, tea.Batch(armCmd, m.titleCmd(), tick, clock)

	case errorMsg:
		if msg.sessionPath != "" && msg.sessionPath != m.sessionPath {
			// Stale load/watcher error for a previous session; see analysisMsg.
			return m, m.armFileWaiter()
		}
		m.err = msg.err
		m.loading = false
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
		armCmd := m.armFileWaiter()
		if errors.Is(msg.err, errSessionFileGone) {
			// Nothing to reload: keep the last data up, marked stale, until
			// the file is re-created (the waiter reports that) or r retries.
			return m, armCmd
		}
		load := m.startLoad()
		return m, tea.Batch(load, armCmd, m.spinnerCmd())

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
			catchUp = m.startLoad()
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
		// start is already on disk; take it rather than wait for its next
		// write.
		if m.waiting() {
			if path, id := msg.watcher.NewestSession(); path != "" {
				return m.switchTo(path, id, true)
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
		// analysisMsg/errorMsg arms its replacement
		m.fileWaiterActive = false
		load := m.startLoad()
		return m, tea.Batch(load, m.spinnerCmd())

	case sessionActivityMsg:
		outcome, hint := onSessionActivity(msg, m.sessionID, m.waiting(), m.followMode, m.hint, m.clock())
		if outcome == activitySwitch {
			return m.switchTo(msg.path, msg.id, true)
		}
		m.hint = hint
		return m, m.waitForNewSession()

	case tickMsg:
		// Clean up stale change tracking entries to prevent unbounded map growth
		m.cleanupStaleChanges()
		// Re-render so highlights fade, and once more after the last one
		// expires so it doesn't stay lit
		if m.ready && m.analysis != nil {
			m.refreshContent()
		}
		if !m.anyHighlight() {
			m.ticking = false
			return m, nil
		}
		return m, tickCmd()

	case subagentPollMsg:
		if m.waiting() {
			return m, subagentPollCmd()
		}
		sig := subagentTreeSignature(filepath.Dir(m.sessionPath), m.sessionID)
		treeChanged := sig != m.subagentSig
		m.subagentSig = sig
		if treeChanged || sessionFileSig(m.sessionPath) != m.fileSig {
			load := m.startLoad()
			return m, tea.Batch(load, subagentPollCmd(), m.spinnerCmd())
		}
		return m, subagentPollCmd()
	}

	return m, nil
}

// switchTo moves the view to another session. auto marks a follow-mode
// switch to a new session, as opposed to a key press. The previous session
// is remembered for the go-back key.
func (m Model) switchTo(path, id string, auto bool) (tea.Model, tea.Cmd) {
	notice := &switchNotice{auto: auto}
	if m.analysis != nil {
		notice.prevTotal = m.analysis.TotalCost.TotalCost
		notice.hadTotal = true
	}
	m.switched = notice
	m.prevSessionID, m.prevSessionPath = m.sessionID, m.sessionPath
	m.sessionID, m.sessionPath = id, path
	if m.hint != nil && m.hint.id == id {
		m.hint = nil
	}

	// Reset analysis state for clean switch. Clear err too so a stale header
	// error from the old session doesn't persist under the new one during the
	// load window (parity with breakdown's session-switch reset).
	m.analysis = nil
	m.err = nil
	m.lastActivity = time.Time{}
	m.changedAt = make(map[string]time.Time)
	m.deltaTokens = make(map[string]int64)
	m.deltaCount = 0
	// Drop the previous session's cached agent parses
	m.agentCache = analyzer.NewAgentParseCache()
	// Fingerprint the new session's subagent tree; the pending reload
	// covers anything already on disk
	m.subagentSig = subagentTreeSignature(filepath.Dir(m.sessionPath), m.sessionID)

	// A new session opens at the top, like the first one, and drops the old
	// session's warning rows from the layout.
	if m.ready {
		m.viewport.SetContent("")
		m.viewport.GotoTop()
		m.layoutViewport()
	}

	// Reset cost chart for new session
	m.costHistory = make([]float64, 0)
	m.costChart = newCostChart(m.getChartWidth(), m.noColor)

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

	// After every reset above: the load copies the model as it is now.
	cmds := []tea.Cmd{m.startLoad(), func() tea.Msg { return m.watchFile() }}
	if m.sessionWatcher != nil {
		m.sessionWatcher.SetCurrentSession(m.sessionID)
		// An automatic switch was reported by the session waiter, which has
		// exited, so start another. A key press leaves that waiter blocked;
		// SetCurrentSession wakes it and sessionWatcherRestartMsg re-arms it.
		if auto {
			cmds = append(cmds, m.waitForNewSession())
		}
	}
	cmds = append(cmds, m.spinnerCmd())
	return m, tea.Batch(cmds...)
}

// startLoad marks a reload as started and returns it. The poll's baseline
// moves here rather than when the load lands, so a load already reading a
// write keeps the next poll from reloading for it again.
func (m *Model) startLoad() tea.Cmd {
	m.loading = true
	m.fileSig = sessionFileSig(m.sessionPath)
	return m.loadAnalysis
}

// hintVisible reports whether the hint is still on screen: it fades once
// the other session has been quiet for idleAfter, and n stops acting on it.
func (m Model) hintVisible() bool { return hintShowing(m.hint, m.clock()) }

// spinnerCmd restarts the spinner when a reload will show "Loading...". A
// second chain is harmless: the spinner drops ticks with a stale tag.
func (m Model) spinnerCmd() tea.Cmd {
	if m.showLoading() {
		return m.spinner.Tick
	}
	return nil
}

// showLoading reports whether the header shows "Loading...": only while
// there is no data yet, on the first load and after a switch. A background
// reload keeps the last status up instead of flickering on every write.
func (m Model) showLoading() bool {
	return m.loading && m.analysis == nil
}

// clockInterval paces clockMsg; see the function of the same name.
func (m Model) clockInterval() time.Duration {
	return clockInterval(m.lastActivity, m.clock())
}

// lastActivity is the newest message timestamp in the analysis (agents
// included), falling back to the file's mtime for a session with no
// timestamped message yet, so a long-dead empty session still reads idle.
// fromFile reports the fallback; comparing the result with modTime can't,
// because a message stamped at the file's mtime is equal to it.
func lastActivity(a *models.SessionAnalysis, modTime time.Time) (t time.Time, fromFile bool) {
	if a != nil {
		t = a.EndTime
		for _, msg := range a.Messages {
			if msg.Timestamp.After(t) {
				t = msg.Timestamp
			}
		}
	}
	if t.IsZero() {
		return modTime, !modTime.IsZero()
	}
	return t, false
}

// titleCmd sets the terminal title to "ficha · webapp · $30.05" when it
// changed, so a tab bar tells several watch panes apart.
func (m *Model) titleCmd() tea.Cmd {
	if m.analysis == nil {
		return nil
	}
	name := m.project
	if name == "" {
		name = render.TruncateID(m.sessionID, sessionIDDisplayLen)
	}
	title := "ficha " + styles.Bullet + " " + name + " " + styles.Bullet + " " + render.Cost(m.analysis.TotalCost.TotalCost)
	if title == m.windowTitle {
		return nil
	}
	m.windowTitle = title
	return tea.SetWindowTitle(title)
}

// layoutViewport sizes the viewport to the rows the header and footer leave.
// The footer grows with its warning rows, so this runs whenever the width or
// the analysis changes, not only on resize.
func (m *Model) layoutViewport() {
	if !m.ready {
		return
	}
	m.viewport.Height = viewportHeight(m.height, m.headerHeight(), m.footerHeight())
}

// refreshContent re-renders the body into the viewport and keeps the scroll
// position, clamped so a shorter body or a taller window never leaves the
// view scrolled past its last line.
func (m *Model) refreshContent() {
	m.layoutViewport()
	m.runningAgents = runningAgentsKey(m.analysis, m.clock())
	// The body's edge blank lines would count as hidden lines in the footer
	// rule's overflow marker, and a leading one would open the view on an
	// empty row.
	body := trimBlankEdges(clipToWidth(m.renderAnalysis(), m.width))
	if m.compact() {
		body = dropBlankLines(body)
	}
	m.viewport.SetContent(body)
	m.viewport.SetYOffset(m.viewport.YOffset)
}

// trimBlankEdges drops whitespace-only lines from both ends of s. Rendered
// lines are space-padded to a common width, so a blank one isn't empty.
func trimBlankEdges(s string) string {
	lines := strings.Split(s, "\n")
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// dropBlankLines removes whitespace-only lines, for compact layouts where
// every row goes to data.
func dropBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}

// detectChanges compares old and new analysis and marks changed fields
func (m *Model) detectChanges(old, new *models.SessionAnalysis) {
	now := time.Now()

	// Clean up stale entries (older than 2x highlight duration)
	for field, changedTime := range m.changedAt {
		if time.Since(changedTime) > highlightDuration*2 {
			delete(m.changedAt, field)
			delete(m.deltaTokens, field)
		}
	}

	// Compare cost fields
	if old.TotalCost.InputCost != new.TotalCost.InputCost {
		m.changedAt["input_cost"] = now
	}
	if old.TotalCost.OutputCost != new.TotalCost.OutputCost {
		m.changedAt["output_cost"] = now
	}
	if old.TotalCost.CacheWrite5mCost != new.TotalCost.CacheWrite5mCost {
		m.changedAt["cache_write_5m"] = now
	}
	if old.TotalCost.CacheWrite1hCost != new.TotalCost.CacheWrite1hCost {
		m.changedAt["cache_write_1h"] = now
	}
	if old.TotalCost.CacheReadCost != new.TotalCost.CacheReadCost {
		m.changedAt["cache_read"] = now
	}
	if old.TotalCost.TotalCost != new.TotalCost.TotalCost {
		m.changedAt["total"] = now
	}
	if old.TotalCost.CacheSavings != new.TotalCost.CacheSavings {
		m.changedAt["savings"] = now
	}

	// Compare token counts with delta tracking (show only most recent change)
	if old.TotalUsage.InputTokens != new.TotalUsage.InputTokens {
		m.deltaTokens["input_tokens"] = new.TotalUsage.InputTokens - old.TotalUsage.InputTokens
		m.changedAt["input_tokens"] = now
	}
	if old.TotalUsage.OutputTokens != new.TotalUsage.OutputTokens {
		m.deltaTokens["output_tokens"] = new.TotalUsage.OutputTokens - old.TotalUsage.OutputTokens
		m.changedAt["output_tokens"] = now
	}
	// Cache-write tokens: when the per-TTL breakdown is present, track a delta
	// per bucket under its own key so each row's highlight matches its own
	// column — a 5m-only write must not flash a delta on the 1h row. Detail-less
	// legacy usages (priced entirely as 5m) keep the single flat-keyed delta.
	// Keying mirrors cacheWriteTokenKeys, which the view reads these back with.
	if new.TotalUsage.CacheCreation != nil {
		old5m, old1h := render.CacheTokensByTTL(old.TotalUsage)
		new5m, new1h := render.CacheTokensByTTL(new.TotalUsage)
		if old5m != new5m {
			m.deltaTokens["cache_write_5m_tokens"] = new5m - old5m
			m.changedAt["cache_write_5m_tokens"] = now
		}
		if old1h != new1h {
			m.deltaTokens["cache_write_1h_tokens"] = new1h - old1h
			m.changedAt["cache_write_1h_tokens"] = now
		}
	} else if old.TotalUsage.CacheCreationInputTokens != new.TotalUsage.CacheCreationInputTokens {
		m.deltaTokens["cache_write_tokens"] = new.TotalUsage.CacheCreationInputTokens - old.TotalUsage.CacheCreationInputTokens
		m.changedAt["cache_write_tokens"] = now
	}
	if old.TotalUsage.CacheReadInputTokens != new.TotalUsage.CacheReadInputTokens {
		m.deltaTokens["cache_read_tokens"] = new.TotalUsage.CacheReadInputTokens - old.TotalUsage.CacheReadInputTokens
		m.changedAt["cache_read_tokens"] = now
	}

	// Compare message count with delta tracking (show only most recent change)
	if old.MessageCount != new.MessageCount {
		m.deltaCount = new.MessageCount - old.MessageCount
		m.changedAt["messages"] = now
	}

	// Compare cost by model
	for modelID, newCost := range new.CostByModel {
		if oldCost, exists := old.CostByModel[modelID]; !exists || oldCost.TotalCost != newCost.TotalCost {
			m.changedAt["model_"+modelID] = now
		}
	}

	// Compare context window values
	oldContextSize := old.LastMessageUsage.ContextWindowSize()
	newContextSize := new.LastMessageUsage.ContextWindowSize()
	if oldContextSize != newContextSize {
		m.changedAt["context_window"] = now
	}

	// Compare insights
	if new.Insights != nil {
		if old.Insights == nil {
			// Insights newly appeared
			m.changedAt["insights_last"] = now
			if new.Insights.HighestCost != nil {
				m.changedAt["insights_highest"] = now
			}
			if new.Insights.MessageCount >= 5 {
				m.changedAt["insights_trend"] = now
			}
		} else {
			// Compare individual insight fields
			// Handle nil LastMessage on either side to avoid nil pointer dereference
			if (old.Insights.LastMessage == nil) != (new.Insights.LastMessage == nil) ||
				(old.Insights.LastMessage != nil && new.Insights.LastMessage != nil &&
					old.Insights.LastMessage.Cost != new.Insights.LastMessage.Cost) {
				m.changedAt["insights_last"] = now
			}
			if (old.Insights.HighestCost == nil) != (new.Insights.HighestCost == nil) ||
				(new.Insights.HighestCost != nil && old.Insights.HighestCost != nil &&
					new.Insights.HighestCost.Cost != old.Insights.HighestCost.Cost) {
				m.changedAt["insights_highest"] = now
			}
			if old.Insights.RecentAvgCost != new.Insights.RecentAvgCost ||
				old.Insights.CostTrend != new.Insights.CostTrend {
				m.changedAt["insights_trend"] = now
			}
		}
	}

	// Compare agent breakdown
	if old.ParentCost.TotalCost != new.ParentCost.TotalCost {
		m.changedAt["parent_cost"] = now
	}
	if old.AgentsCost.TotalCost != new.AgentsCost.TotalCost {
		m.changedAt["agents_subtotal"] = now
	}
	// Track individual agent changes
	for _, newAgent := range new.Agents {
		found := false
		for _, oldAgent := range old.Agents {
			if oldAgent.AgentID == newAgent.AgentID {
				found = true
				if oldAgent.TotalCost.TotalCost != newAgent.TotalCost.TotalCost {
					m.changedAt["agent_"+newAgent.AgentID] = now
				}
				break
			}
		}
		if !found {
			// New agent appeared
			m.changedAt["agent_"+newAgent.AgentID] = now
		}
	}
}

// cleanupStaleChanges removes entries from changedAt/deltaTokens that are past their highlight window.
// Called periodically from tickMsg to prevent unbounded map growth during idle sessions.
// Note: Uses value receiver to match bubbletea Update() convention; map mutations work because maps
// are reference types. Non-map fields (e.g., deltaCount) are NOT cleaned up here — they are only
// displayed when recentlyChanged("messages") is true, which depends on changedAt (a map).
func (m Model) cleanupStaleChanges() {
	for field, changedTime := range m.changedAt {
		if time.Since(changedTime) > highlightDuration*2 {
			delete(m.changedAt, field)
			delete(m.deltaTokens, field)
		}
	}
}

// anyHighlight reports whether any field is still inside its highlight
// window. The 100ms tick stops once none is, and changedAt entries past it
// are pruned on the next change.
func (m Model) anyHighlight() bool {
	for field := range m.changedAt {
		if m.recentlyChanged(field) {
			return true
		}
	}
	return false
}

// recentlyChanged checks if a field was recently changed (within highlight duration)
// This is used to determine whether to show delta values, regardless of color mode.
// Read-only: it is reached from View(), which must not mutate model state; stale
// entries are pruned by cleanupStaleChanges on tick instead.
func (m Model) recentlyChanged(field string) bool {
	changedTime, exists := m.changedAt[field]
	if !exists {
		return false
	}

	return time.Since(changedTime) < highlightDuration
}

// isHighlighted checks if a field should be highlighted with color (recently changed AND color enabled)
func (m Model) isHighlighted(field string) bool {
	if m.noColor {
		return false
	}
	return m.recentlyChanged(field)
}

// getChartWidth returns the appropriate chart width based on terminal width
func (m Model) getChartWidth() int {
	// Chart fits within the 76-char panel with 4-char indent on each side
	// Leave room for y-axis labels (if we add them later)
	maxWidth := 68
	if m.width > 0 && m.width-8 < maxWidth {
		w := m.width - 8
		if w < 1 {
			w = 1 // sparkline canvas does make() with this length; non-positive panics
		}
		return w
	}
	return maxWidth
}

// mergedCostHistory extracts per-message costs in chronological order, keeping
// at most max entries from the end. The analyzer emits messages as parent
// block then agent blocks (its documented order — never sorted in place here),
// so a stable sort both interleaves agent spend where it happened and keeps
// that order deterministic when timestamps tie.
//
// A missing timestamp unmarshals to the zero time.Time (real session data does
// this — see timeRange in analyzer/session.go). Left as-is it sorts before every
// real timestamp, so a mid-session point without a timestamp would jump to chart
// position 0 and be the first dropped by the tail-keep truncation even when it is
// the newest spend. Instead each zero-ts point inherits the previous message's
// effective timestamp (in file order), so the stable sort leaves it adjacent to
// its file neighbors. Leading zero-ts points (nothing precedes them) keep the
// zero time, and an all-zero list falls back to pure file order as before.
func mergedCostHistory(msgs []models.MessageAnalysis, max int) []float64 {
	type point struct {
		ts   time.Time
		cost float64
	}
	points := make([]point, len(msgs))
	var lastTS time.Time
	for i, msg := range msgs {
		ts := msg.Timestamp
		if ts.IsZero() {
			ts = lastTS
		} else {
			lastTS = ts
		}
		points[i] = point{ts: ts, cost: msg.Cost.TotalCost}
	}
	sort.SliceStable(points, func(i, j int) bool {
		return points[i].ts.Before(points[j].ts)
	})
	if len(points) > max {
		points = points[len(points)-max:]
	}
	history := make([]float64, len(points))
	for i, p := range points {
		history[i] = p.cost
	}
	return history
}

// visibleCostHistory returns the tail of costHistory the chart can actually draw
// at the current width. The sparkline's ring buffer holds only chart-width
// points, so this is the exact window the bars, scale, and labels must all agree
// on (see rebuildCostChart / renderCostChart).
func (m Model) visibleCostHistory() []float64 {
	w := m.getChartWidth()
	if len(m.costHistory) <= w {
		return m.costHistory
	}
	return m.costHistory[len(m.costHistory)-w:]
}

// rebuildCostChart recreates the sparkline at the current width and pushes only
// the visible window. Pushing the full history would let ntcharts' AutoMaxValue
// ratchet the scale to a peak that has already rotated out of the ring buffer,
// flat-lining the visible bars against an off-screen max; pushing only
// what is drawn keeps the scale honest to the visible bars.
func (m *Model) rebuildCostChart() {
	m.costChart = newCostChart(m.getChartWidth(), m.noColor)
	visible := m.visibleCostHistory()
	m.costChart.PushAll(visible)
	// The sparkline's max starts at 1 and only grows, so sub-dollar messages
	// (nearly all of them) would draw in the bottom rows. Scale to the
	// window's own peak so its shape uses the full height.
	if peak := maxOf(visible); peak > 0 {
		m.costChart.SetMax(peak)
	}

	// Braille has four times the vertical resolution but no ASCII stand-in;
	// the ASCII glyph set draws columns, which renderCostChart maps to ASCII.
	if styles.ASCII() {
		m.costChart.Draw()
	} else {
		m.costChart.DrawBraille()
	}
}

// maxOf returns the largest value, or 0 for none.
func maxOf(vals []float64) float64 {
	var top float64
	for _, v := range vals {
		top = max(top, v)
	}
	return top
}

// newCostChart builds the cost-trend sparkline, green unless color is off.
func newCostChart(width int, noColor bool) sparkline.Model {
	if noColor {
		return sparkline.New(width, chartHeight)
	}
	chartStyle := lipgloss.NewStyle().Foreground(styles.SuccessColor)
	return sparkline.New(width, chartHeight, sparkline.WithStyle(chartStyle))
}

// updateCostChart rebuilds the sparkline from the analysis on every reload.
// A full rebuild (rather than pushing the new tail) is what lets the list
// carry agent messages, whose timestamps land mid-list; reloads only happen
// on real file changes, and the rebuild is far cheaper than the re-parse
// that precedes it.
func (m *Model) updateCostChart() {
	if m.analysis == nil {
		return
	}

	m.costHistory = mergedCostHistory(m.analysis.Messages, maxCostHistorySize)
	m.rebuildCostChart()
}
