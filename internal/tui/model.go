package tui

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/bardisty/ccusage/internal/analyzer"
	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/styles"
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
	// parent-file write doesn't re-parse every unchanged agent (see TUI-3).
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

	// Auto-follow mode for tracking new sessions
	projectDir     string          // Project directory to watch for new sessions
	followMode     bool            // Whether to auto-follow new sessions
	prevSessionID  string          // Previous session ID (shown after switch)
	sessionWatcher *SessionWatcher // Watches for new session files
	switchNotifyAt time.Time       // When session switch notification started

	// Cost trend chart
	costChart        sparkline.Model // Sparkline chart for cost trend
	costHistory      []float64       // Rolling window of per-message costs
	lastMessageCount int             // Track message count to detect new messages

	width  int
	height int
}

// Messages
type (
	analysisMsg        *models.SessionAnalysis
	errorMsg           error
	fileChangedMsg     struct{}
	tickMsg            time.Time
	sessionSwitchedMsg struct {
		newSessionPath string
		newSessionID   string
	}
)

// NewModel creates a new TUI model
func NewModel(sessionPath, sessionID string, verbose, noColor bool, projectDir string, followMode bool) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	// Initialize cost trend chart with default dimensions
	// Will be resized when we receive the first WindowSizeMsg
	chartStyle := lipgloss.NewStyle().Foreground(styles.SuccessColor)
	chart := sparkline.New(chartWidth, chartHeight, sparkline.WithStyle(chartStyle))

	closing := &atomic.Bool{}
	closeOnce := &sync.Once{}
	return Model{
		sessionPath: sessionPath,
		sessionID:   sessionID,
		verbose:     verbose,
		noColor:     noColor,
		loading:     true,
		autoScroll:  true,
		spinner:     s,
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
			m.viewport = viewport.New(msg.Width, msg.Height-headerHeight-footerHeight)
			m.viewport.YPosition = headerHeight
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - headerHeight - footerHeight
		}
		m.width = msg.Width
		m.height = msg.Height

		// Resize cost chart to match new width
		newChartWidth := m.getChartWidth()
		m.costChart.Resize(newChartWidth, chartHeight)
		// Resize keeps the data but clears the canvas; redraw so the chart
		// isn't blank until the next message arrives
		if len(m.costHistory) > 0 {
			if !m.noColor {
				m.costChart.DrawBraille()
			} else {
				m.costChart.Draw()
			}
		}

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
		// Detect changes and mark them for highlighting
		// Compare current analysis (before update) with new analysis
		if m.analysis != nil && msg != nil {
			m.detectChanges(m.analysis, msg)
		}
		m.analysis = msg
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
		// Restart file watcher after analysis completes to avoid race condition
		// where the old watcher's defer hasn't executed yet when we start a new one
		return m, m.waitForFileChange()

	case errorMsg:
		m.err = msg
		m.loading = false
		// Restart file watcher even after error to continue monitoring
		return m, m.waitForFileChange()

	case watcherStartedMsg:
		// Store the watcher and start listening for file changes
		m.watcher = msg.watcher
		return m, m.waitForFileChange()

	case sessionWatcherStartedMsg:
		// Store the session watcher and start waiting for new sessions
		m.sessionWatcher = msg.watcher
		return m, m.waitForNewSession()

	case sessionWatcherRestartMsg:
		// Session was changed externally, restart waiting
		return m, m.waitForNewSession()

	case fileChangedMsg:
		m.loading = true
		// Watcher restart moved to analysisMsg handler to avoid race condition
		return m, m.loadAnalysis

	case sessionSwitchedMsg:
		// Store previous session ID and switch to new session
		m.prevSessionID = m.sessionID
		m.sessionID = msg.newSessionID
		m.sessionPath = msg.newSessionPath
		m.switchNotifyAt = time.Now()

		// Reset analysis state for clean switch
		m.analysis = nil
		m.loading = true
		m.changedAt = make(map[string]time.Time)
		m.deltaTokens = make(map[string]int64)
		m.deltaCount = 0
		// Drop the previous session's cached agent parses
		m.agentCache = analyzer.NewAgentParseCache()

		// Reset cost chart for new session
		m.costHistory = make([]float64, 0)
		m.lastMessageCount = 0
		chartStyle := lipgloss.NewStyle().Foreground(styles.SuccessColor)
		m.costChart = sparkline.New(m.getChartWidth(), chartHeight, sparkline.WithStyle(chartStyle))

		// Stop old file watcher, will be restarted by watchFile
		if m.watcher != nil {
			m.watcher.Close()
			m.watcher = nil
		}

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
	if old.TotalUsage.CacheCreationInputTokens != new.TotalUsage.CacheCreationInputTokens {
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

// updateCostChart updates the sparkline chart with cost data from analysis
func (m *Model) updateCostChart() {
	if m.analysis == nil || m.analysis.Messages == nil {
		return
	}

	// Check if we have new messages
	currentMessageCount := len(m.analysis.Messages)
	if currentMessageCount <= m.lastMessageCount {
		return // No new messages
	}

	// Extract costs from new messages
	for i := m.lastMessageCount; i < currentMessageCount; i++ {
		cost := m.analysis.Messages[i].Cost.TotalCost
		m.costHistory = append(m.costHistory, cost)
	}

	// Cap history size to prevent unbounded growth
	trimmed := false
	if len(m.costHistory) > maxCostHistorySize {
		m.costHistory = m.costHistory[len(m.costHistory)-maxCostHistorySize:]
		trimmed = true
	}

	// If trimmed, rebuild chart from scratch; otherwise push incrementally
	if trimmed {
		// Recreate chart and repopulate with trimmed window
		chartStyle := lipgloss.NewStyle().Foreground(styles.SuccessColor)
		m.costChart = sparkline.New(m.getChartWidth(), chartHeight, sparkline.WithStyle(chartStyle))
		m.costChart.PushAll(m.costHistory)
	} else {
		// Push only new values for efficiency
		for i := m.lastMessageCount; i < currentMessageCount; i++ {
			m.costChart.Push(m.analysis.Messages[i].Cost.TotalCost)
		}
	}

	// Redraw the chart with updated data
	if !m.noColor {
		m.costChart.DrawBraille()
	} else {
		m.costChart.Draw()
	}

	m.lastMessageCount = currentMessageCount
}
