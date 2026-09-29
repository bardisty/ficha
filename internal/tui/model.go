package tui

import (
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
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
	verbose     bool
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

	// Viewport for scrolling
	viewport   viewport.Model
	autoScroll bool
	ready      bool // viewport initialized

	spinner   spinner.Model
	watcher   *fsnotify.Watcher
	done      chan struct{}   // Channel to signal watcher goroutine to stop
	closing   *atomic.Bool    // Atomic flag for shutdown coordination
	closeOnce *sync.Once      // Ensure shutdown happens exactly once
	wg        *sync.WaitGroup // Waits for goroutines to drain on shutdown

	// A file-change waiter is blocked on the watcher; gates armFileWaiter so
	// poll-triggered reloads can't stack extra waiters
	fileWaiterActive bool

	// Auto-follow mode for tracking new sessions
	projectDir     string          // Project directory to watch for new sessions
	followMode     bool            // Whether to auto-follow new sessions
	prevSessionID  string          // Previous session ID (shown after switch)
	sessionWatcher *SessionWatcher // Watches for new session files
	switchNotifyAt time.Time       // When session switch notification started

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
	tickMsg            time.Time
	sessionSwitchedMsg struct {
		newSessionPath string
		newSessionID   string
	}
)

// NewModel creates a new TUI model
func NewModel(sessionPath, sessionID string, verbose, noColor bool, projectDir string, followMode bool) Model {
	// Initialize cost trend chart with default dimensions
	// Will be resized when we receive the first WindowSizeMsg
	chart := newCostChart(chartWidth, noColor)

	closing := &atomic.Bool{}
	closeOnce := &sync.Once{}
	return Model{
		sessionPath: sessionPath,
		sessionID:   sessionID,
		verbose:     verbose,
		noColor:     noColor,
		loading:     true,
		autoScroll:  true,
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
		subagentSig: subagentTreeSignature(filepath.Dir(sessionPath), sessionID),
	}
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			shutdownWatchers(m.closeOnce, m.closing, m.done, m.watcher, m.sessionWatcher, m.wg)
			return m, tea.Quit
		case "r":
			m.loading = true
			return m, m.loadAnalysis

		case "up", "k":
			m.autoScroll = false
			m.viewport.ScrollUp(1)

		case "down", "j":
			m.viewport.ScrollDown(1)
			if m.viewport.AtBottom() {
				m.autoScroll = true
			}

		case "pgup":
			m.autoScroll = false
			m.viewport.HalfPageUp()

		case "pgdown":
			m.viewport.HalfPageDown()
			if m.viewport.AtBottom() {
				m.autoScroll = true
			}

		case "g", "home":
			m.autoScroll = false
			m.viewport.GotoTop()

		case "G", "end":
			m.viewport.GotoBottom()
			m.autoScroll = true
		}

	case tea.WindowSizeMsg:
		// Header: panel(3 lines) + blank/notify(1) = 4 lines fixed
		// Footer: separator(1) + stats(1) + separator(1) + help(1) = 4 lines fixed
		headerHeight := 4
		footerHeight := 4

		if !m.ready {
			m.viewport = viewport.New(msg.Width, viewportHeight(msg.Height, headerHeight, footerHeight))
			m.viewport.YPosition = headerHeight
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = viewportHeight(msg.Height, headerHeight, footerHeight)
		}
		m.width = msg.Width
		m.height = msg.Height

		// Re-window the cost chart to the new width. A plain Resize would keep the
		// old width's pushed points (and its scale), so the drawn window and the
		// min/max/count labels would disagree with the new width — rebuild from
		// costHistory so the visible tail, scale, and labels all track the resize.
		m.rebuildCostChart()

		// Re-render content with new dimensions
		if m.analysis != nil {
			m.viewport.SetContent(clipToWidth(m.renderAnalysis(), m.width))
			if m.autoScroll {
				m.viewport.GotoBottom()
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

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
		if m.analysis != nil && msg.analysis != nil {
			m.detectChanges(m.analysis, msg.analysis)
		}
		m.analysis = msg.analysis
		m.loading = false
		m.lastUpdated = time.Now()
		m.err = nil

		// Update cost chart with new message costs
		m.updateCostChart()

		// Update viewport content
		if m.ready {
			m.viewport.SetContent(clipToWidth(m.renderAnalysis(), m.width))
			if m.autoScroll {
				m.viewport.GotoBottom()
			}
		}
		// Re-arm the file watcher only when no waiter is in flight: this reload
		// may have been poll-triggered, in which case the file-change waiter is
		// still blocked on the watcher
		// Call before return: the arm must mutate the m the caller receives
		armCmd := m.armFileWaiter()
		return m, armCmd

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
		m.loading = true
		armCmd := m.armFileWaiter()
		return m, tea.Batch(m.loadAnalysis, armCmd)

	case watcherStartedMsg:
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
		return m, armCmd

	case sessionWatcherStartedMsg:
		// Store the session watcher and start waiting for new sessions
		m.sessionWatcher = msg.watcher
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
		m.loading = true
		return m, m.loadAnalysis

	case sessionSwitchedMsg:
		// Store previous session ID and switch to new session
		m.prevSessionID = m.sessionID
		m.sessionID = msg.newSessionID
		m.sessionPath = msg.newSessionPath
		m.switchNotifyAt = time.Now()

		// Reset analysis state for clean switch. Clear err too so a stale header
		// error from the old session doesn't persist under the new one during the
		// load window (parity with breakdown's session-switch reset).
		m.analysis = nil
		m.err = nil
		m.loading = true
		m.changedAt = make(map[string]time.Time)
		m.deltaTokens = make(map[string]int64)
		m.deltaCount = 0
		// Drop the previous session's cached agent parses
		m.agentCache = analyzer.NewAgentParseCache()
		// Fingerprint the new session's subagent tree; the pending reload
		// covers anything already on disk
		m.subagentSig = subagentTreeSignature(filepath.Dir(m.sessionPath), m.sessionID)

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

		// Update session watcher's current session
		if m.sessionWatcher != nil {
			m.sessionWatcher.SetCurrentSession(m.sessionID)
		}

		// Reload analysis, restart file watcher, and restart session watcher
		// We must explicitly restart waitForNewSession because the goroutine that
		// detected this switch has already exited after returning sessionSwitchedMsg
		cmds := []tea.Cmd{m.loadAnalysis, func() tea.Msg { return m.watchFile() }}
		if m.sessionWatcher != nil {
			cmds = append(cmds, m.waitForNewSession())
		}
		return m, tea.Batch(cmds...)

	case tickMsg:
		// Clean up stale change tracking entries to prevent unbounded map growth
		m.cleanupStaleChanges()
		// Re-render to update highlight fading (only if there are active highlights)
		if m.ready && m.analysis != nil && len(m.changedAt) > 0 {
			m.viewport.SetContent(clipToWidth(m.renderAnalysis(), m.width))
		}
		// Continue the animation tick for highlight fade
		return m, tickCmd()

	case subagentPollMsg:
		sig := subagentTreeSignature(filepath.Dir(m.sessionPath), m.sessionID)
		if sig != m.subagentSig {
			m.subagentSig = sig
			m.loading = true
			return m, tea.Batch(m.loadAnalysis, subagentPollCmd())
		}
		return m, subagentPollCmd()
	}

	return m, nil
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
			m.changedAt["insights_first"] = now
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
			if old.Insights.LateAvgCost != new.Insights.LateAvgCost ||
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
	m.costChart.PushAll(m.visibleCostHistory())

	// Braille has four times the vertical resolution but no ASCII stand-in;
	// the ASCII glyph set draws columns, which renderCostChart maps to ASCII.
	if styles.ASCII() {
		m.costChart.Draw()
	} else {
		m.costChart.DrawBraille()
	}
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
