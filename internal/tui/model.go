package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/canvas"
	"github.com/NimbleMarkets/ntcharts/v2/canvas/graph"
	"github.com/NimbleMarkets/ntcharts/v2/sparkline"
	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/fsnotify/fsnotify"
)

// Highlight duration for changed values
const highlightDuration = 2 * time.Second

// watchDebounce coalesces rapid fsnotify write events into a single reload.
const watchDebounce = 50 * time.Millisecond

// Chart display constants
const (
	chartHeight = 6           // Height of the sparkline chart in characters
	chartWidth  = 60          // Default width of the chart (adjusted on resize)
	chartBucket = time.Minute // The span of one chart column
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
	// staleSince is when, by clock, the data on screen fell behind the file:
	// a load failed, or the file went away. Zero while it's current. The
	// spend rate is worked out against it, since against the clock it would
	// fall as if the session were slowing down.
	staleSince time.Time

	// agentCache memoizes agent sub-session parses so a reload triggered by a
	// parent-file write doesn't re-parse every unchanged agent.
	agentCache *analyzer.AgentParseCache

	// copies is what the session's file shares with other sessions, found
	// once per session: a fork's copies are its file's head, which doesn't
	// change as it grows. Who keeps them can, so family is the fork family
	// they were found in, rechecked when a member writes. copiesFor is the
	// session file they were found for, "" until a load has looked.
	copies    *models.CopiedReplies
	family    *analyzer.ForkFamily
	copiesFor string
	// rechecking is set while a recheck runs, and recheckAgain when a
	// member wrote during it. copiesGen counts session switches, so a
	// recheck for a session left behind is dropped.
	rechecking, recheckAgain bool
	copiesGen                int

	// Change tracking for highlight animation
	changedAt   map[string]time.Time
	deltaTokens map[string]int64 // Delta values for token counts
	deltaCount  int              // Delta for message count

	// Viewport for scrolling. watch is a dashboard, not a log: it opens at the
	// top and keeps the reader's position across reloads and resizes.
	// chosenY is the offset the reader last scrolled to, which only a scroll
	// key sets, and which moves with their line when a section above it
	// changes length; see refreshContent.
	viewport viewport.Model
	chosenY  int
	ready    bool // viewport initialized
	// bodySections is where each of the body's sections starts as last
	// rendered; refreshContent keeps the reader's line by them.
	bodySections []bodySection

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
	// sessionFallback is the session watcher's, polling the project
	// directory for want of fsnotify
	sessionFallback watchFallback

	// fileSig is the session file's sessionFileSig when the last load
	// started; the poll reloads when the file no longer matches it
	fileSig string

	// loadGen counts the loads started, so a poll result can tell that one
	// started after it was sent; see pollResultMsg.stale
	loadGen int

	// Session following. The session watcher runs in every mode: following
	// switches to a newly created session, and pinned shows it as a hint.
	projectDir      string          // Project directory to watch for new sessions
	project         string          // Project name for the header and title; "" when unknown
	followMode      bool            // Whether to auto-follow new sessions
	prevSessionID   string          // Session watched before the last switch
	prevSessionPath string          // Its file, for the go-back key (-)
	prevFollow      bool            // followMode as that session was left
	agentsOpen      bool            // every agent on its own row; a toggles it
	lastCreated     string          // The last session seen start, or the first; see isNewestSession
	sessionWatcher  *SessionWatcher // Watches for other sessions' files
	switched        *switchNotice   // Shown until a key or a newer hint
	seen            sessionsSeen    // Other sessions' last writes, for what's news
	keysOpen        bool            // The ? key list covers the frame
	hint            *sessionHint    // Another session's activity, not followed
	windowTitle     string          // Terminal title, once a session has loaded

	// waitingIn is the directory a waiting model (no session yet) waits for
	// Claude Code to start in, and waitingHint the line under it; see
	// NewWaitingModel.
	waitingIn   string
	waitingHint []string

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
	fileWrite        time.Time // the session file's mtime at the last load; see sessionsSeen.left

	// runningAgents is runningAgentsKey as of the last content render; a
	// clock tick that sees it change re-renders the agent list's dots and
	// folds.
	runningAgents string

	// Cost trend chart
	costChart sparkline.Model // Sparkline chart for cost trend
	// costHistory is the chart's columns: spend per clock minute, parent
	// and agents, oldest first, ending with the minute chartMinute.
	costHistory []float64
	chartMinute time.Time

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
		// copiesFound is set when the load looked for copies, which it
		// does until one finds the session's replies to go by.
		copiesFound bool
		family      *analyzer.ForkFamily
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
		lastCreated: sessionPath,
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
	m.sessionFallback.sessions = true
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
// the header and the waiting message. hint is a line for under that message
// in its forms from longest to shortest, or nil; see waitingLines.
func NewWaitingModel(projectDir, projectPath string, hint []string, noColor, followMode bool) Model {
	m := NewModel("", "", noColor, projectDir, followMode)
	m.loading = false
	m.waitingIn = projectPath
	m.waitingHint = hint
	m.project = paths.BasenameCrossOS(projectPath)
	return m
}

// waiting reports whether watch has no session yet.
func (m Model) waiting() bool { return m.sessionPath == "" }

// waitingLines is a waiting view's message and the hint under it, fitted to
// width columns. The message drops the directory when it doesn't fit, and
// the hint takes its first form that fits or is left out: wrapped, either
// would push the frame out of shape.
func waitingLines(in string, hint []string, width int) []string {
	msg := "Waiting for a Claude Code session in " + in + styles.Ellipsis
	if in == "" || lipgloss.Width(msg) > width {
		msg = "Waiting for a Claude Code session" + styles.Ellipsis
	}
	for _, h := range hint {
		if lipgloss.Width(h) <= width {
			return []string{msg, h}
		}
	}
	return []string{msg}
}

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
	case tea.KeyPressMsg:
		var done bool
		if m.keysOpen, done = toggleKeyList(m.keysOpen, msg); done {
			return m, nil
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
				return m, m.retrySessionWatch()
			}
			load := m.startLoad()
			return m, tea.Batch(load, m.spinnerCmd(), m.retryWatchNow(), m.retrySessionWatch())

		case "ctrl+z":
			if canSuspend() {
				return m, tea.Suspend
			}
			return m, nil

		case "-":
			// Back to the previous session, like cd -. breakdown has the same
			// key, and p there means peak, so neither view uses p for this.
			// Going back to an older session is a deliberate choice of it,
			// so it pins. Back on the newest, which following would be on,
			// the mode is what it was when the reader left.
			if m.prevSessionPath != "" {
				follow := m.prevFollow && isNewestSession(m.lastCreated, m.prevSessionPath)
				next, cmd := m.switchTo(m.prevSessionPath, m.prevSessionID, false)
				back := next.(Model)
				back.followMode = follow
				return back, cmd
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

		case "a":
			// The rows the list gains or loses are above a reader past it,
			// so refreshContent keeps their line. A list short enough never
			// folds, and a setting it can't show would leave a later, longer
			// list unfolded with nothing to say why.
			if m.analysis != nil && len(m.analysis.Agents) > agentListCap {
				m.agentsOpen = !m.agentsOpen
				if m.ready {
					m.refreshContent()
				}
			}

		case "g", "home":
			m.viewport.GotoTop()
			m.chosenY = m.viewport.YOffset()

		case "G", "end":
			m.viewport.GotoBottom()
			m.chosenY = m.viewport.YOffset()

		default:
			// Everything else goes to the viewport's pager keymap: j/k and
			// arrows, space/f/pgdown and b/pgup by page, d/u and ctrl+d/u by
			// half page. f is taken above for follow. A key that moves
			// nothing leaves the choice alone: after a resize clamped the
			// view, it would otherwise take the clamp as the reader's.
			before := m.viewport.YOffset()
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			if m.viewport.YOffset() != before {
				m.chosenY = m.viewport.YOffset()
			}
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
			m.viewport = newViewport(msg.Width, 1)
			m.ready = true
		}
		m.viewport.SetWidth(msg.Width)
		m.layoutViewport()

		// Re-window the cost chart to the new width. A plain Resize would keep
		// the old width's pushed points and scale, so the drawn window and the
		// label would disagree with the new width.
		m.rebuildCostChart()

		// Re-render content with new dimensions
		if m.analysis != nil || m.waiting() {
			m.relayoutContent()
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
		// so it's redone only when an agent went quiet or the chart's minute
		// rolled over. The chart moves on while the session is idle, as the
		// rate does, so a pause shows as empty columns.
		if msg.gen != m.clockGen {
			return m, nil // superseded by a faster chain; see analysisMsg
		}
		rolled := m.analysis != nil && !m.clock().Truncate(chartBucket).Equal(m.chartMinute)
		if rolled {
			m.rebuildCostChart()
		}
		if m.ready && (rolled || runningAgentsKey(m.analysis, m.clock()) != m.runningAgents) {
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
		var recheck tea.Cmd
		if msg.copiesFound {
			m.setFamily(msg.family)
			m.copiesFor = m.sessionPath
		} else {
			recheck = m.recheckCopies()
		}
		m.loading = false
		m.lastUpdated = time.Now()
		m.staleSince = time.Time{}
		m.err = nil
		slowClock := m.clockInterval() > time.Second
		m.lastActivity, m.activityFromFile = lastActivity(msg.analysis, msg.modTime)
		m.fileWrite = msg.modTime
		// A message after an idle stretch: the pending clock tick is up to
		// 15s out, so start a 1s chain now for the seconds count and let the
		// slow one lapse.
		var clock tea.Cmd
		if slowClock && m.clockInterval() == time.Second {
			m.clockGen++
			clock = clockCmd(time.Second, m.clockGen)
		}

		m.rebuildCostChart()

		// Update viewport content
		if m.ready {
			m.refreshContent()
		}
		// Re-arm the file watcher only when no waiter is in flight: this reload
		// may have been poll-triggered, in which case the file-change waiter is
		// still blocked on the watcher
		// Call before return: the arm must mutate the m the caller receives
		armCmd := m.armFileWaiter()
		m.setTitle()
		return m, tea.Batch(armCmd, tick, clock, recheck)

	case errorMsg:
		if msg.sessionPath != "" && msg.sessionPath != m.sessionPath {
			// Stale load/watcher error for a previous session; see analysisMsg.
			return m, m.armFileWaiter()
		}
		m.err = msg.err
		m.loading = false
		m.markStale()
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
			m.markStale()
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
		if m.sessionFallback.retryDue(msg) {
			return m, m.retrySessionWatch()
		}
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
		m.sessionWatcher = msg.watcher
		retry := m.sessionFallback.sessionWatchResult(msg)
		// A session created between the decision to wait and the watcher's
		// start is already on disk; take it rather than wait for its next
		// write.
		if m.waiting() {
			if path, id := msg.watcher.NewestSession(); path != "" {
				next, cmd := m.switchTo(path, id, true)
				return next, tea.Batch(cmd, retry)
			}
		}
		return m, tea.Batch(m.waitForNewSession(), retry)

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
		if msg.created {
			m.lastCreated = msg.path
		}
		outcome, hint := onSessionActivity(msg, m.sessionID, m.waiting(), m.followMode, m.hint, m.clock())
		if outcome == activitySwitch {
			return m.switchTo(msg.path, msg.id, true)
		}
		if outcome == activityHint && m.seen.news(msg, m.clock()) {
			m.switched = nil
		}
		m.hint = hint
		if m.family != nil && m.family.Includes(msg.id) {
			// Call before return: the recheck must mark the m the caller
			// receives
			recheck := m.recheckCopies()
			return m, tea.Batch(m.waitForNewSession(), recheck)
		}
		return m, m.waitForNewSession()

	case copiesMsg:
		if msg.gen != m.copiesGen {
			return m, nil
		}
		m.rechecking = false
		if msg.family != nil {
			m.setFamily(msg.family)
			if m.ready {
				m.refreshContent()
			}
		}
		if m.recheckAgain {
			m.recheckAgain = false
			recheck := m.recheckCopies()
			return m, recheck
		}
		return m, nil

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
		return m, pollSessionCmd(m.sessionPath, m.sessionID, m.loadGen)

	case pollResultMsg:
		if msg.stale(m.sessionPath, m.loadGen) {
			return m, subagentPollCmd()
		}
		treeChanged := msg.treeSig != m.subagentSig
		m.subagentSig = msg.treeSig
		if treeChanged || msg.fileSig != m.fileSig {
			load := m.startLoadFrom(msg.fileSig)
			return m, tea.Batch(load, subagentPollCmd(), m.spinnerCmd())
		}
		return m, subagentPollCmd()
	}

	return m, nil
}

// setFamily takes f as the session's fork family, nil for none, and its
// copies for the note.
func (m *Model) setFamily(f *analyzer.ForkFamily) {
	m.family, m.copies = f, nil
	if f != nil {
		m.copies = f.Copies
	}
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
	m.seen.left(m.sessionID, m.lastActivity, m.fileWrite, m.clock())
	// A waiting view's first session is the one following is on.
	if m.waiting() {
		m.lastCreated = path
	}
	m.prevSessionID, m.prevSessionPath = m.sessionID, m.sessionPath
	m.prevFollow = m.followMode
	m.sessionID, m.sessionPath = id, path
	if m.hint != nil && m.hint.id == id {
		m.hint = nil
	}

	// Reset analysis state for clean switch. Clear err too so a stale header
	// error from the old session doesn't persist under the new one during the
	// load window (parity with breakdown's session-switch reset).
	m.analysis = nil
	m.err, m.staleSince = nil, time.Time{}
	m.lastActivity, m.fileWrite = time.Time{}, time.Time{}
	m.changedAt = make(map[string]time.Time)
	m.deltaTokens = make(map[string]int64)
	m.deltaCount = 0
	// Drop the previous session's cached agent parses
	m.agentCache = analyzer.NewAgentParseCache()
	m.copies, m.family, m.copiesFor = nil, nil, ""
	m.rechecking, m.recheckAgain = false, false
	m.copiesGen++
	// Fingerprint the new session's subagent tree; the pending reload
	// covers anything already on disk
	m.subagentSig = subagentTreeSignature(filepath.Dir(m.sessionPath), m.sessionID)

	// A new session opens at the top, like the first one, and drops the old
	// session's warning rows from the layout.
	m.chosenY = 0
	m.bodySections = nil
	// The new session's list opens folded, as the first one did.
	m.agentsOpen = false
	if m.ready {
		m.viewport.SetContent("")
		m.viewport.GotoTop()
		m.layoutViewport()
	}

	// Reset cost chart for new session
	m.costHistory = nil
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

// markStale notes that the data on screen fell behind the file, keeping the
// time it first did.
func (m *Model) markStale() {
	if m.staleSince.IsZero() {
		m.staleSince = m.clock()
	}
}

// startLoad marks a reload as started and returns it. The poll's baseline
// moves here rather than when the load lands, so a load already reading a
// write keeps the next poll from reloading for it again.
func (m *Model) startLoad() tea.Cmd {
	return m.startLoadFrom(sessionFileSig(m.sessionPath))
}

// startLoadFrom is startLoad with the baseline already measured, as the
// poll's result has it.
func (m *Model) startLoadFrom(fileSig string) tea.Cmd {
	m.loading = true
	m.loadGen++
	m.fileSig = fileSig
	return m.loadAnalysis
}

// hintVisible reports whether the hint is still on screen: it fades once
// the other session has been quiet for idleAfter, and n stops acting on it.
func (m Model) hintVisible() bool { return hintShowing(m.hint, m.clock()) }

// spinnerCmd restarts the spinner when a reload will show "loading...". A
// second chain is harmless: the spinner drops ticks with a stale tag.
func (m Model) spinnerCmd() tea.Cmd {
	if m.showLoading() {
		return m.spinner.Tick
	}
	return nil
}

// showLoading reports whether the header shows "loading...": only while
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

// setTitle makes the terminal title "ficha · webapp · $30.05", so a tab bar
// tells several watch panes apart. View carries it to Bubble Tea.
func (m *Model) setTitle() {
	if m.analysis == nil {
		return
	}
	name := m.project
	if name == "" {
		name = render.TruncateID(m.sessionID, sessionIDDisplayLen)
	}
	m.windowTitle = "ficha " + styles.Bullet + " " + name + " " + styles.Bullet + " " + render.Cost(m.analysis.TotalCost.TotalCost)
}

// layoutViewport sizes the viewport to the rows the header and footer leave.
// The footer grows with its warning rows, so this runs whenever the width or
// the analysis changes, not only on resize.
func (m *Model) layoutViewport() {
	if !m.ready {
		return
	}
	m.viewport.SetHeight(viewportHeight(m.height, m.headerHeight(), m.footerHeight()))
}

// refreshContent re-renders the body into the viewport at the offset the
// reader chose, clamped so a shorter body or a taller window never leaves
// the view scrolled past its last line. The clamp isn't kept: the viewport
// clamps on every SetContent, and a window that grows and shrinks back, as
// a zoomed tmux pane does, would otherwise come back off the reader's line.
//
// When a section above the reader's line changed length, as the agent list
// does when agents start or fold, the choice moves with it, so the line at
// the top stays at the top. A reader at the top, or inside the section
// that changed, stays where they were.
func (m *Model) refreshContent() {
	before := m.bodySections
	m.setBody()
	m.chosenY += sectionShift(before, m.bodySections, m.chosenY)
	m.viewport.SetYOffset(m.chosenY)
}

// relayoutContent is refreshContent for a resize. The sections move with
// the new size, but that's the window changing, not the content above the
// reader, and the reader's choice stays as it is.
func (m *Model) relayoutContent() {
	m.setBody()
	m.viewport.SetYOffset(m.chosenY)
}

// setBody renders the body into the viewport and notes where its sections
// start, without moving the viewport's offset from what SetContent leaves.
func (m *Model) setBody() {
	m.layoutViewport()
	m.runningAgents = runningAgentsKey(m.analysis, m.clock())
	// The too-small screen replaces the frame, so the body behind it is never
	// drawn. Building it there would only risk a panic in a layout no one sees.
	if m.tooSmall() {
		m.bodySections = nil
		m.viewport.SetContent("")
		return
	}
	raw, sections := m.renderAnalysisSections()
	body, at := fitBody(clipToWidth(raw, m.width), m.compact())
	for i := range sections {
		sections[i].line = at(sections[i].line)
	}
	m.bodySections = sections
	m.viewport.SetContent(body)
}

// sectionShift is how far the reader's line at y moved between two
// layouts of the body: how far the start of the section it's in moved.
// A section gone from the new layout gives no shift.
func sectionShift(before, after []bodySection, y int) int {
	in := -1
	for i, s := range before {
		if s.line <= y {
			in = i
		}
	}
	if in < 0 {
		return 0
	}
	for _, s := range after {
		if s.name == before[in].name {
			return s.line - before[in].line
		}
	}
	return 0
}

// fitBody drops whitespace-only lines from both ends of body, and in a
// compact layout, where every row goes to data, from everywhere. The body's
// edge blank lines would count as hidden lines in the footer rule's overflow
// marker, and a leading one would open the view on an empty row. Rendered
// lines are space-padded to a common width, so a blank one isn't empty. at
// maps a line of body to the line it lands on.
func fitBody(body string, compact bool) (fitted string, at func(int) int) {
	lines := strings.Split(body, "\n")
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	// landed[i] is how many kept lines come before line i.
	landed := make([]int, len(lines)+1)
	kept := make([]string, 0, end-start)
	for i, line := range lines {
		landed[i] = len(kept)
		if i >= start && i < end && (!compact || strings.TrimSpace(line) != "") {
			kept = append(kept, line)
		}
	}
	landed[len(lines)] = len(kept)
	return strings.Join(kept, "\n"), func(i int) int { return landed[min(max(i, 0), len(lines))] }
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

// getChartWidth returns the chart's width in columns, one minute each: the
// panel less 8, starting 2 columns in from the rule and ending 6 short of
// it, so 68 minutes at the design width and 110 at the widest. A terminal
// too narrow for the design width cuts the chart to its own width less the
// same 8, keeping the chart to 68 there.
func (m Model) getChartWidth() int {
	if m.width <= 0 {
		return defaultPanelWidth - 8
	}
	// The sparkline canvas does make() with this length; non-positive panics.
	return max(min(m.width-8, max(panelWidthFor(m.width), defaultPanelWidth)-8), 1)
}

// costBuckets sums spend, parent and agents alike, into one bucket per clock
// minute, oldest first, ending with the minute now is in. That last bucket
// fills as the minute goes on, and the rest shift only when it rolls over;
// buckets that ended at now would shift every column on every tick. It
// returns n buckets, or fewer when the first message is more recent, and
// none without a timestamped message.
//
// Messages without a timestamp are skipped, as rollingRate skips them, so
// the chart and the footer's rate count the same spend. One stamped after
// now, by a clock running ahead, counts in the newest bucket.
func costBuckets(msgs []models.MessageAnalysis, now time.Time, n int) []float64 {
	var first time.Time
	for _, msg := range msgs {
		if !msg.Timestamp.IsZero() && (first.IsZero() || msg.Timestamp.Before(first)) {
			first = msg.Timestamp
		}
	}
	if first.IsZero() || n < 1 {
		return nil
	}
	end := now.Truncate(chartBucket)
	span := int(end.Sub(first.Truncate(chartBucket))/chartBucket) + 1
	n = max(1, min(n, span))
	start := end.Add(-time.Duration(n-1) * chartBucket)
	buckets := make([]float64, n)
	for _, msg := range msgs {
		if msg.Timestamp.IsZero() || msg.Timestamp.Before(start) {
			continue
		}
		buckets[min(int(msg.Timestamp.Sub(start)/chartBucket), n-1)] += msg.Cost.TotalCost
	}
	return buckets
}

// rebuildCostChart buckets the analysis at the current width and clock and
// redraws the sparkline from scratch. It runs on every reload, on resize and
// when the clock enters a new minute; the analyzer lists agent messages after
// the parent's, so appending to the last chart wouldn't place them, and
// bucketing is far cheaper than the re-parse that precedes a reload.
func (m *Model) rebuildCostChart() {
	now := m.clock()
	m.chartMinute = now.Truncate(chartBucket)
	m.costHistory = nil
	if m.analysis != nil {
		m.costHistory = costBuckets(m.analysis.Messages, now, m.getChartWidth())
	}
	m.costChart = newCostChart(m.getChartWidth(), m.noColor)
	m.costChart.PushAll(m.costHistory)
	// The sparkline's max starts at 1 and only grows, so sub-dollar minutes
	// would draw in the bottom rows. Scale to the window's own peak so its
	// shape uses the full height; the floor stays at zero.
	peak := maxOf(m.costHistory)
	if peak > 0 {
		m.costChart.SetMax(peak)
	}

	// Braille has four times the vertical resolution but no ASCII stand-in;
	// the ASCII glyph set draws columns, which renderCostChart maps to ASCII.
	if styles.ASCII() {
		m.costChart.Draw()
		floorEmptyMinutes(&m.costChart, len(m.costHistory))
	} else {
		drawMinuteLine(&m.costChart, m.costHistory, peak)
	}
}

// floorEmptyMinutes marks the bottom row of each of the last n columns that
// drew no block. A minute too cheap for one, or idle, would otherwise leave
// its column blank, and an idle stretch would read as margin rather than
// time; the braille line runs along the floor there instead.
func floorEmptyMinutes(chart *sparkline.Model, n int) {
	w, h := chart.Width(), chart.Height()
	for x := w - n; x < w; x++ {
		p := canvas.Point{X: x, Y: h - 1}
		if r := chart.Canvas.Cell(p).Rune; r == 0 || r == ' ' {
			chart.Canvas.SetRuneWithStyle(p, '_', chart.Style)
		}
	}
}

// drawMinuteLine draws vals on chart as a braille line, one minute to a cell
// on its left dot column, so the newest minute is in the last cell and a
// rollover moves the line exactly one cell. The sparkline's own DrawBraille
// spreads a full chart's points unevenly over one cell fewer, which would
// break the label's one-minute-per-column count.
func drawMinuteLine(chart *sparkline.Model, vals []float64, peak float64) {
	w, h := chart.Width(), chart.Height()
	grid := graph.NewBrailleGrid(w, h, 0, float64(2*w-1), 0, float64(h))
	at := func(i int) canvas.Point {
		var y float64
		if peak > 0 {
			y = vals[i] / peak * float64(h)
		}
		return grid.GridPoint(canvas.Float64Point{X: float64(2 * (w - len(vals) + i)), Y: y})
	}
	for i := range vals {
		for _, p := range graph.GetLinePoints(at(i), at(min(i+1, len(vals)-1))) {
			grid.Set(p)
		}
	}
	chart.Canvas.Clear()
	graph.DrawBraillePatterns(&chart.Canvas, canvas.Point{}, grid.BraillePatterns(), chart.Style)
	chart.Canvas.SetStyle(chart.Style)
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
