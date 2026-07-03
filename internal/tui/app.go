package tui

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/bardisty/ccusage/internal/analyzer"
	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/pricing"
	"github.com/bardisty/ccusage/internal/render"
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
	}
}

// Init initializes the TUI
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.spinner.Tick,
		m.loadAnalysis,
		func() tea.Msg { return m.watchFile() },
		tickCmd(),
	}

	// Start session watcher if follow mode is enabled
	if m.followMode && m.projectDir != "" {
		cmds = append(cmds, m.startSessionWatcher())
	}

	return tea.Batch(cmds...)
}

// tickCmd returns a command that sends a tick every 100ms for animation
func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
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
			m.viewport.LineUp(1)

		case "down", "j":
			m.viewport.LineDown(1)
			if m.viewport.AtBottom() {
				m.autoScroll = true
			}

		case "pgup":
			m.autoScroll = false
			m.viewport.HalfViewUp()

		case "pgdown":
			m.viewport.HalfViewDown()
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

// Panel sizing: panels, separators, and section headers are designed for
// defaultPanelWidth columns; on narrower terminals they shrink toward
// minPanelWidth so box-drawing lines don't wrap and desynchronize the fixed
// header/footer height math. Below minPanelWidth (plus indent) the frame is
// clipped by View's MaxWidth instead.
const (
	defaultPanelWidth = 76
	minPanelWidth     = 40
)

// panelWidthFor returns the panel width for a terminal width: the design width
// when it fits (accounting for the 2-column left indent), otherwise clamped.
// A zero/negative termWidth means no WindowSizeMsg yet — use the design width.
func panelWidthFor(termWidth int) int {
	if termWidth <= 0 {
		return defaultPanelWidth
	}
	return max(min(termWidth-2, defaultPanelWidth), minPanelWidth)
}

// clipToWidth truncates every line of rendered output to the terminal width
// (ANSI-aware) so overlong lines degrade by clipping instead of wrapping.
// Applied to viewport content — the viewport soft-wraps overlong lines, which
// inflates line counts — and to the final frame, where terminal hard-wrap
// would desynchronize the fixed header/footer layout.
func clipToWidth(frame string, termWidth int) string {
	if termWidth <= 0 {
		return frame
	}
	return lipgloss.NewStyle().MaxWidth(termWidth).Render(frame)
}

// View renders the TUI
func (m Model) View() string {
	var sb strings.Builder
	panelWidth := panelWidthFor(m.width)

	// FIXED HEADER (4 lines)
	sb.WriteString(m.renderHeaderPanel(panelWidth))

	// Show switch notification on next line if applicable
	showSwitchNotify := !m.switchNotifyAt.IsZero() && time.Since(m.switchNotifyAt) < switchNotifyDuration
	if showSwitchNotify {
		sb.WriteString("\n")
		if m.noColor {
			sb.WriteString("  [Switched to new session]")
		} else {
			sb.WriteString("  " + lipgloss.NewStyle().Foreground(styles.HighlightColor).Bold(true).Render("Switched to new session"))
		}
	}
	sb.WriteString("\n")

	// SCROLLABLE CONTENT (viewport)
	if m.ready {
		sb.WriteString(m.viewport.View())
	} else if m.loading {
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// FIXED FOOTER (4 lines) - with 2-space padding
	footerSep := strings.Repeat(styles.BoxHorizontal, panelWidth)
	if !m.noColor {
		footerSep = panelBorderStyle.Render(footerSep)
	}
	sb.WriteString("  " + footerSep + "\n")

	// Footer stats - only show if we have data
	if m.analysis != nil && !m.isEmptySession() {
		sb.WriteString("  " + m.renderFooter() + "\n")
	} else {
		sb.WriteString("\n")
	}

	// Single-line help separator
	helpSep := strings.Repeat(styles.LineHorizontal, panelWidth)
	if !m.noColor {
		helpSep = dimStyle.Render(helpSep)
	}
	sb.WriteString("  " + helpSep + "\n")

	// Help text - expanded keybinds to match breakdown
	if !m.noColor {
		helpText := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("q: quit • r: refresh • g/G: top/bottom • ↑↓: scroll")
		sb.WriteString("  " + helpText)
	} else {
		sb.WriteString("  q: quit • r: refresh • g/G: top/bottom • ↑↓: scroll")
	}

	return clipToWidth(sb.String(), m.width)
}

func (m Model) renderAnalysis() string {
	if m.noColor {
		return m.renderAnalysisPlain()
	}

	// Handle empty session state
	if m.isEmptySession() {
		return m.renderEmptyState()
	}

	var sb strings.Builder
	a := m.analysis
	sectionWidth := panelWidthFor(m.width)

	// Hero total cost as section header
	sb.WriteString("\n")
	totalHighlighted := m.isHighlighted("total")
	sb.WriteString("  " + m.renderHeroCost(a.TotalCost.TotalCost, totalHighlighted, sectionWidth))
	sb.WriteString("\n\n")

	// Unified cost+token rows
	// Input tokens
	sb.WriteString(m.renderUnifiedCostRow(
		"Input", a.TotalCost.InputCost, a.TotalUsage.InputTokens,
		"input_cost", "input_tokens", lipgloss.Color(""), ""))

	// Output tokens
	sb.WriteString(m.renderUnifiedCostRow(
		"Output", a.TotalCost.OutputCost, a.TotalUsage.OutputTokens,
		"output_cost", "output_tokens", styles.OutputTokenColor, ""))

	// Cache write rows - split tokens by TTL when detailed breakdown available
	cache5mTokens, cache1hTokens := render.CacheTokensByTTL(a.TotalUsage)
	has5mCost := a.TotalCost.CacheWrite5mCost > 0
	has1hCost := a.TotalCost.CacheWrite1hCost > 0

	if has5mCost {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache write", a.TotalCost.CacheWrite5mCost, cache5mTokens,
			"cache_write_5m", "cache_write_tokens", styles.CacheWriteTokenColor, "5m TTL"))
	}

	if has1hCost {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache write", a.TotalCost.CacheWrite1hCost, cache1hTokens,
			"cache_write_1h", "cache_write_tokens", styles.CacheWriteTokenColor, "1h TTL"))
	}

	// Cache read - only show if present
	if a.TotalCost.CacheReadCost > 0 || a.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache read", a.TotalCost.CacheReadCost, a.TotalUsage.CacheReadInputTokens,
			"cache_read", "cache_read_tokens", styles.CacheReadTokenColor, ""))
	}

	// Savings row (no separator - the rows above sum to hero TOTAL, not savings)
	if a.TotalCost.CacheSavings > 0 {
		savingsHighlighted := m.isHighlighted("savings")
		savingsStr := render.CostStyledGreen(a.TotalCost.CacheSavings, 11, savingsHighlighted, m.noColor)
		sb.WriteString(fmt.Sprintf("    %s %s  %s\n",
			savingsLabelStyle.Render(fmt.Sprintf("%-14s", "Savings")),
			savingsStr,
			dimStyle.Render("(from cache reads)")))
	}

	// Context window section
	sb.WriteString("\n")
	sb.WriteString(m.renderContextSection())

	// Cost trend chart (only show if we have 2+ data points)
	if len(m.costHistory) >= 2 {
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("COST TREND", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderCostChart())
	}

	// Section: COST BY MODEL
	sb.WriteString("\n")
	sb.WriteString("  " + render.SectionHeader("COST BY MODEL", sectionWidth, m.noColor))
	sb.WriteString("\n\n")
	sb.WriteString(m.renderCostByModelContent())

	// Agent breakdown (shown when agents exist)
	if a.HasAgents {
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("AGENT SUB-SESSIONS", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderAgentBreakdownContent())
	}

	// Message insights (shown when insights are available)
	if a.Insights != nil {
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("MESSAGE INSIGHTS", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderInsightsContent())
	}

	// Note: Footer is rendered separately in View() after the separators

	return sb.String()
}

func (m Model) renderAnalysisPlain() string {
	// Handle empty session state
	if m.isEmptySession() {
		return m.renderEmptyState()
	}

	var sb strings.Builder
	a := m.analysis
	sectionWidth := panelWidthFor(m.width)

	// Hero total cost as section header
	sb.WriteString("\n")
	sb.WriteString("  " + m.renderHeroCost(a.TotalCost.TotalCost, false, sectionWidth))
	sb.WriteString("\n\n")

	// Unified cost+token rows
	sb.WriteString(m.renderUnifiedCostRow(
		"Input", a.TotalCost.InputCost, a.TotalUsage.InputTokens,
		"input_cost", "input_tokens", lipgloss.Color(""), ""))

	// Output tokens
	sb.WriteString(m.renderUnifiedCostRow(
		"Output", a.TotalCost.OutputCost, a.TotalUsage.OutputTokens,
		"output_cost", "output_tokens", lipgloss.Color(""), ""))

	// Cache write rows - tokens aren't split by TTL, so show them on whichever row has cost
	cacheWriteTokens := a.TotalUsage.CacheCreationInputTokens
	has5mCost := a.TotalCost.CacheWrite5mCost > 0
	has1hCost := a.TotalCost.CacheWrite1hCost > 0

	if has5mCost {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache write", a.TotalCost.CacheWrite5mCost, cacheWriteTokens,
			"cache_write_5m", "cache_write_tokens", lipgloss.Color(""), "5m TTL"))
	}

	if has1hCost {
		var tokens int64
		if !has5mCost {
			tokens = cacheWriteTokens
		}
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache write", a.TotalCost.CacheWrite1hCost, tokens,
			"cache_write_1h", "cache_write_tokens", lipgloss.Color(""), "1h TTL"))
	}

	// Cache read
	if a.TotalCost.CacheReadCost > 0 || a.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache read", a.TotalCost.CacheReadCost, a.TotalUsage.CacheReadInputTokens,
			"cache_read", "cache_read_tokens", lipgloss.Color(""), ""))
	}

	// Savings row (no separator - rows above sum to hero TOTAL, not savings)
	if a.TotalCost.CacheSavings > 0 {
		sb.WriteString(fmt.Sprintf("    %-14s %s  (from cache reads)\n",
			"Savings", render.Cost(a.TotalCost.CacheSavings)))
	}

	// Context window section
	sb.WriteString("\n")
	sb.WriteString(m.renderContextSection())

	// Cost trend chart (only show if we have 2+ data points)
	if len(m.costHistory) >= 2 {
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("COST TREND", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderCostChart())
	}

	// Section: COST BY MODEL
	sb.WriteString("\n")
	sb.WriteString("  " + render.SectionHeader("COST BY MODEL", sectionWidth, m.noColor))
	sb.WriteString("\n\n")
	sb.WriteString(m.renderCostByModelContent())

	// Agent breakdown (shown when agents exist)
	if a.HasAgents {
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("AGENT SUB-SESSIONS", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderAgentBreakdownContent())
	}

	// Message insights (shown when insights are available)
	if a.Insights != nil {
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("MESSAGE INSIGHTS", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderInsightsContent())
	}

	// Note: Footer is rendered separately in View() after the separators

	return sb.String()
}

// renderContextSection renders the context window progress bar and stats
func (m Model) renderContextSection() string {
	var sb strings.Builder

	contextSize := m.analysis.LastMessageUsage.ContextWindowSize()
	if contextSize == 0 {
		return ""
	}

	modelPricing := pricing.GetModelPricing(m.analysis.LastMessageModel)
	maxContext := modelPricing.MaxContextTokens
	contextPct := pricing.GetContextPercentage(modelPricing, contextSize)
	freeSpace := pricing.GetFreeSpace(modelPricing, contextSize)
	freePct := float64(freeSpace) / float64(maxContext) * 100

	highlighted := m.isHighlighted("context_window")
	usageColor := styles.GetContextUsageColor(contextPct)

	// Context label with value
	contextVal := render.Number(contextSize)
	contextMeta := fmt.Sprintf("(%.0f%% of %s)", contextPct, render.Number(int64(maxContext)))

	if m.noColor {
		sb.WriteString(fmt.Sprintf("    Context  %s  %s %s\n", render.ContextBar(contextSize, freeSpace, maxContext, true), contextVal, contextMeta))
		sb.WriteString(fmt.Sprintf("             Free: %s (%.1f%%)\n", render.Number(freeSpace), freePct))
	} else {
		// Progress bar with context info
		if highlighted {
			sb.WriteString(fmt.Sprintf("    Context  %s  %s %s\n",
				render.ContextBar(contextSize, freeSpace, maxContext, false),
				highlightStyle.Render(contextVal),
				dimStyle.Render(contextMeta)))
		} else {
			coloredMeta := lipgloss.NewStyle().Foreground(usageColor).Render(contextMeta)
			sb.WriteString(fmt.Sprintf("    Context  %s  %s %s\n",
				render.ContextBar(contextSize, freeSpace, maxContext, false),
				contextVal, coloredMeta))
		}

		// Free space info
		freeVal := render.Number(freeSpace)
		freeValWithPct := fmt.Sprintf("%s (%.1f%%)", freeVal, freePct)
		var freeStyled string
		if highlighted {
			freeStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render("Free: ") + highlightStyle.Render(freeValWithPct)
		} else {
			freeStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(fmt.Sprintf("Free: %s", freeValWithPct))
		}
		sb.WriteString(fmt.Sprintf("             %s\n", freeStyled))
	}

	return sb.String()
}

// renderFooter renders the message count and duration footer
func (m Model) renderFooter() string {
	a := m.analysis
	changed := m.recentlyChanged("messages")
	highlighted := m.isHighlighted("messages")

	var footerLine string
	if changed {
		var valStr string
		if m.deltaCount > 0 {
			valStr = fmt.Sprintf("%d (+%d)", a.MessageCount, m.deltaCount)
		} else if m.deltaCount < 0 {
			valStr = fmt.Sprintf("%d (%d)", a.MessageCount, m.deltaCount)
		} else {
			valStr = fmt.Sprintf("%d", a.MessageCount)
		}
		if highlighted {
			footerLine = footerStyle.Render("Messages: ") + highlightStyle.Render(valStr) +
				footerStyle.Render(fmt.Sprintf("  │  Duration: %s", render.Duration(a.Duration.Duration())))
		} else {
			footerLine = fmt.Sprintf("Messages: %s  │  Duration: %s", valStr, render.Duration(a.Duration.Duration()))
		}
	} else {
		footerLine = footerStyle.Render(fmt.Sprintf("Messages: %d  │  Duration: %s",
			a.MessageCount, render.Duration(a.Duration.Duration())))
	}

	// Surface parse warnings so undercounted totals don't look authoritative
	if a.SkippedLines > 0 {
		if m.noColor {
			footerLine += fmt.Sprintf("  │  ! %d skipped line(s)", a.SkippedLines)
		} else {
			warnStyle := lipgloss.NewStyle().Foreground(styles.WarningColor)
			footerLine += footerStyle.Render("  │  ") + warnStyle.Render(fmt.Sprintf("⚠ %d skipped line(s)", a.SkippedLines))
		}
	}
	return footerLine
}

// renderCostByModelContent renders just the cost by model content (no header)
func (m Model) renderCostByModelContent() string {
	var sb strings.Builder

	// Order by cost descending (canonical COST BY MODEL ordering, audit DUP-2)
	for _, modelID := range render.OrderModelsByCost(m.analysis.CostByModel) {
		cost := m.analysis.CostByModel[modelID]
		modelName := pricing.GetModelDisplayName(modelID)
		highlighted := m.isHighlighted("model_" + modelID)

		// Apply model color to the label (reduced padding from 18 to 12)
		var labelStr string
		if !m.noColor {
			modelColor := styles.GetModelColor(modelName)
			labelStyle := lipgloss.NewStyle().Foreground(modelColor)
			labelStr = labelStyle.Render(fmt.Sprintf("%-12s", modelName))
		} else {
			labelStr = fmt.Sprintf("%-12s", modelName)
		}

		// Plain white costs - model tier colors already provide cost hierarchy
		costStr := render.CostStyled(cost.TotalCost, 12, highlighted, m.noColor)
		sb.WriteString(fmt.Sprintf("    %s %s\n", labelStr, costStr))
	}

	return sb.String()
}

// renderAgentBreakdownContent renders just the agent breakdown content (no header)
// Layout: [AN] Model (ID) msgs cost
// All rows align costs at column 47 (4 indent + 43 content)
// Example:
//
//	Parent session                            $1.135371
//	[A1] Opus 4.5    (a0b184d)     14 msgs    $1.358774
//	Agents subtotal                           $5.112218
func (m Model) renderAgentBreakdownContent() string {
	var sb strings.Builder
	a := m.analysis

	// Show parent session cost - right-aligned cost at column 47
	// Format: 4(indent) + 40(label) + 3(spaces) + cost = 47 chars before cost
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	// Uses plain white (not magnitude gradient) - model tier colors provide cost hierarchy
	parentHighlighted := m.isHighlighted("parent_cost")
	parentCostStr := render.CostStyled(a.ParentCost.TotalCost, 11, parentHighlighted, m.noColor)
	if !m.noColor {
		paddedLabel := fmt.Sprintf("%-40s", "Parent session")
		sb.WriteString(fmt.Sprintf("    %s   %s\n", dimStyle.Render(paddedLabel), parentCostStr))
	} else {
		sb.WriteString(fmt.Sprintf("    %-40s   %s\n", "Parent session", parentCostStr))
	}

	// Show each agent with [AN] Model (ID) msgs cost format
	// Format: 4(indent) + 5(marker) + 1 + 11(model) + 1 + 10(id) + 1 + 8(msgs) + 6(spaces) + cost
	//       = 4 + 37 + 6 = 47 chars before cost (aligned with parent)
	// Uses plain white costs - model tier colors already provide cost hierarchy
	for i, agent := range a.Agents {
		agentNum := fmt.Sprintf("%d", i+1)
		agentHighlighted := m.isHighlighted("agent_" + agent.AgentID)
		costStr := render.CostStyled(agent.TotalCost.TotalCost, 11, agentHighlighted, m.noColor)

		shortID := agent.AgentID
		if len(shortID) > 7 {
			shortID = shortID[:7]
		}

		// Get primary model for this agent
		modelName := render.PrimaryModel(agent.CostByModel)

		// Format message count with singular/plural
		msgStr := fmt.Sprintf("%d msgs", agent.MessageCount)
		if agent.MessageCount == 1 {
			msgStr = "1 msg"
		}

		if !m.noColor {
			// Color the [An] marker (use %-5s to handle [A10] etc)
			agentColor := styles.GetAgentColor(agentNum)
			markerStyled := lipgloss.NewStyle().Foreground(agentColor).Render(fmt.Sprintf("%-5s", fmt.Sprintf("[A%s]", agentNum)))

			// Color model name by tier
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-11s", modelName))

			// Dim the ID and message count
			idStyled := dimStyle.Render(fmt.Sprintf("%-10s", fmt.Sprintf("(%s)", shortID)))
			msgStyled := dimStyle.Render(fmt.Sprintf("%8s", msgStr))

			sb.WriteString(fmt.Sprintf("    %s %s %s %s      %s\n",
				markerStyled, modelStyled, idStyled, msgStyled, costStr))
		} else {
			marker := fmt.Sprintf("[A%d]", i+1)
			idStr := fmt.Sprintf("(%s)", shortID)
			sb.WriteString(fmt.Sprintf("    %-5s %-11s %-10s %8s      %s\n",
				marker, modelName, idStr, msgStr, costStr))
		}
	}

	// Show agents subtotal in bold green (matches TotalValueStyle for visual hierarchy)
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	subtotalHighlighted := m.isHighlighted("agents_subtotal")
	subtotalStr := render.CostStyledBoldGreen(a.AgentsCost.TotalCost, 11, subtotalHighlighted, m.noColor)
	if !m.noColor {
		paddedSubtotal := fmt.Sprintf("%-40s", "Agents subtotal")
		sb.WriteString(fmt.Sprintf("    %s   %s\n", dimStyle.Render(paddedSubtotal), subtotalStr))
	} else {
		sb.WriteString(fmt.Sprintf("    %-40s   %s\n", "Agents subtotal", subtotalStr))
	}

	return sb.String()
}

// renderInsightsContent renders just the insights content (no header)
func (m Model) renderInsightsContent() string {
	var sb strings.Builder
	insights := m.analysis.Insights

	// First message
	if insights.FirstMessage != nil {
		first := insights.FirstMessage
		componentLabel := render.CostComponentLabel(first.MainCostComponent)
		highlighted := m.isHighlighted("insights_first")

		if !m.noColor {
			labelStr := dimStyle.Render(fmt.Sprintf("%-10s", "First"))
			costStr := render.CostStyled(first.Cost, 10, highlighted, m.noColor)
			timestampStr := dimStyle.Render(fmt.Sprintf("(%s)", first.Timestamp.Format("15:04:05")))
			componentCostStr := formatCostStyledDim(first.MainCostValue)
			componentStr := dimStyle.Render(componentLabel+":") + " " + componentCostStr
			sb.WriteString(fmt.Sprintf("    %s %s  %s  %s\n",
				labelStr,
				costStr,
				timestampStr,
				componentStr))
		} else {
			sb.WriteString(fmt.Sprintf("    %-10s %s  (%s)  %s: %s\n",
				"First",
				render.Cost(first.Cost),
				first.Timestamp.Format("15:04:05"),
				componentLabel,
				render.Cost(first.MainCostValue)))
		}
	}

	// Last message
	if insights.LastMessage != nil {
		last := insights.LastMessage
		componentLabel := render.CostComponentLabel(last.MainCostComponent)
		highlighted := m.isHighlighted("insights_last")

		if !m.noColor {
			labelStr := dimStyle.Render(fmt.Sprintf("%-10s", "Last"))
			costStr := render.CostStyled(last.Cost, 10, highlighted, m.noColor)
			timestampStr := dimStyle.Render(fmt.Sprintf("(%s)", last.Timestamp.Format("15:04:05")))
			componentCostStr := formatCostStyledDim(last.MainCostValue)
			componentStr := dimStyle.Render(componentLabel+":") + " " + componentCostStr
			sb.WriteString(fmt.Sprintf("    %s %s  %s  %s\n",
				labelStr,
				costStr,
				timestampStr,
				componentStr))
		} else {
			sb.WriteString(fmt.Sprintf("    %-10s %s  (%s)  %s: %s\n",
				"Last",
				render.Cost(last.Cost),
				last.Timestamp.Format("15:04:05"),
				componentLabel,
				render.Cost(last.MainCostValue)))
		}
	}

	// Highest cost (only if notably above average)
	if insights.HighestCost != nil {
		highest := insights.HighestCost
		multiplier := insights.CostMultiplier()
		warningStr := fmt.Sprintf("%.1fx avg cost", multiplier)
		highlighted := m.isHighlighted("insights_highest")

		if !m.noColor {
			labelStr := dimStyle.Render(fmt.Sprintf("%-10s", "Peak"))
			costStr := render.CostStyled(highest.Cost, 10, highlighted, m.noColor)
			timestampStr := dimStyle.Render(fmt.Sprintf("(%s)", highest.Timestamp.Format("15:04:05")))
			warningStyled := lipgloss.NewStyle().Foreground(styles.WarningColor).Render("⚠ " + warningStr)
			sb.WriteString(fmt.Sprintf("    %s %s  %s  %s\n",
				labelStr,
				costStr,
				timestampStr,
				warningStyled))
		} else {
			sb.WriteString(fmt.Sprintf("    %-10s %s  (%s)  ! %s\n",
				"Peak",
				render.Cost(highest.Cost),
				highest.Timestamp.Format("15:04:05"),
				warningStr))
		}
	}

	// Trend (only for sessions with 5+ messages)
	if insights.MessageCount >= 5 {
		trendDesc := insights.TrendDescription()
		trendSymbol := insights.CostTrend.Symbol()
		highlighted := m.isHighlighted("insights_trend")

		earlyStr := fmt.Sprintf("$%.2f/msg", insights.EarlyAvgCost)
		lateStr := fmt.Sprintf("$%.2f/msg", insights.LateAvgCost)

		if !m.noColor {
			labelStr := dimStyle.Render(fmt.Sprintf("%-10s", "Trend"))
			// Color the trend symbol and description based on direction
			var symbolStyled, descStyled string
			switch insights.CostTrend {
			case models.TrendIncreasing:
				symbolStyled = lipgloss.NewStyle().Foreground(styles.WarningColor).Render(trendSymbol)
				descStyled = lipgloss.NewStyle().Foreground(styles.WarningColor).Render(trendDesc)
			case models.TrendDecreasing:
				symbolStyled = lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(trendSymbol)
				descStyled = lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(trendDesc)
			default:
				symbolStyled = dimStyle.Render(trendSymbol)
				descStyled = dimStyle.Render(trendDesc)
			}
			var trendLine string
			if highlighted {
				// Highlight only the cost values, not the arrow
				trendLine = fmt.Sprintf("%s → %s", highlightStyle.Render(earlyStr), highlightStyle.Render(lateStr))
			} else {
				trendLine = fmt.Sprintf("%s → %s", earlyStr, lateStr)
			}
			sb.WriteString(fmt.Sprintf("    %s %s  %s %s\n",
				labelStr,
				trendLine,
				symbolStyled,
				descStyled))
		} else {
			sb.WriteString(fmt.Sprintf("    %-10s %s -> %s  %s %s\n",
				"Trend",
				earlyStr,
				lateStr,
				trendSymbol,
				trendDesc))
		}
	}

	return sb.String()
}

// Commands

func (m Model) loadAnalysis() tea.Msg {
	if m.closing != nil && m.closing.Load() {
		return nil
	}
	// Always include messages for the cost trend chart
	// The verbose flag controls additional output details, but we need messages
	// for the live chart regardless
	analysis, err := analyzer.AnalyzeSession(m.sessionPath, m.sessionID, true)
	if err != nil {
		return errorMsg(err)
	}
	return analysisMsg(analysis)
}

// watcherStartedMsg is sent when the watcher is successfully created
type watcherStartedMsg struct {
	watcher *fsnotify.Watcher
}

// sessionWatcherStartedMsg is sent when the session watcher is ready
type sessionWatcherStartedMsg struct {
	watcher *SessionWatcher
}

// sessionWatcherRestartMsg signals that the watcher should restart waiting
type sessionWatcherRestartMsg struct{}

// wrapErr adapts this model's error message type for the shared watcher
// commands in file_watcher.go (audit TUI-5).
func (m Model) wrapErr(err error) tea.Msg { return errorMsg(err) }

// The watcher lifecycle lives in file_watcher.go, shared with breakdown.go.
func (m Model) watchFile() tea.Msg { return watchFileCmd(m.sessionPath, m.wrapErr) }

func (m Model) waitForFileChange() tea.Cmd {
	return waitForFileChangeCmd(m.wg, m.closing, m.watcher, m.done, m.sessionPath, m.wrapErr)
}

func (m Model) startSessionWatcher() tea.Cmd {
	return startSessionWatcherCmd(m.projectDir, m.sessionID, m.wrapErr)
}

func (m Model) waitForNewSession() tea.Cmd {
	return waitForNewSessionCmd(m.wg, m.sessionWatcher)
}

// Panel and section rendering helpers

// renderHeaderPanel renders the mainframe-style header panel with session info
// Format:
// ╔══════════════════════════════════════════════════════════════════════════╗
// ║  Session: xxx (prev: yyy)  │  ● LIVE  │  Updated: HH:MM:SS              ║
// ╚══════════════════════════════════════════════════════════════════════════╝
func (m Model) renderHeaderPanel(width int) string {
	return renderLiveHeaderPanel(liveHeaderParams{
		sessionID:     m.sessionID,
		prevSessionID: m.prevSessionID,
		loading:       m.loading,
		err:           m.err,
		lastUpdated:   m.lastUpdated,
		spinnerView:   m.spinner.View(),
		noColor:       m.noColor,
		width:         width,
	})
}

// Helper functions
//
// Pure formatting/number/duration/section/progress-bar helpers now live in
// internal/render (audit DUP-1). The TUI-only helpers below (dim cost,
// number-with-delta) wrap the shared render helpers with live-view behavior.

// formatCostStyledDim returns a cost string entirely in dim style
// Used for secondary cost displays like component breakdowns in insights
func formatCostStyledDim(cost float64) string {
	return dimStyle.Render(fmt.Sprintf("$%.6f", cost))
}

// formatNumberWithDelta formats a number with optional delta during highlight
func formatNumberWithDelta(n int64, delta int64, showDelta bool) string {
	valStr := render.Number(n)
	if !showDelta || delta == 0 {
		return fmt.Sprintf("%12s", valStr)
	}

	// Format delta
	var deltaStr string
	if delta > 0 {
		deltaStr = fmt.Sprintf("(+%s)", render.Number(delta))
	} else {
		deltaStr = fmt.Sprintf("(-%s)", render.Number(-delta))
	}

	return fmt.Sprintf("%12s %s", valStr, deltaStr)
}

// renderHeroCost renders the total cost integrated into a section header
// Format: ─────────────────────[ $12.665834 TOTAL ]─────────────────────
func (m Model) renderHeroCost(cost float64, highlighted bool, width int) string {
	// Format the cost with 6 decimal places
	costFull := fmt.Sprintf("$%.6f", cost)
	costStr := costFull + " TOTAL"
	bracketedCost := "[ " + costStr + " ]"
	costLen := len(bracketedCost)
	sideLen := (width - costLen) / 2
	if sideLen < 0 {
		sideLen = 0
	}
	rightLen := width - sideLen - costLen
	if rightLen < 0 {
		rightLen = 0
	}

	leftLine := strings.Repeat(styles.LineHorizontal, sideLen)
	rightLine := strings.Repeat(styles.LineHorizontal, rightLen)

	if m.noColor {
		return leftLine + bracketedCost + rightLine
	}

	// Cost with dimmed trailing decimals (like other cost displays)
	// Split into main ($X.XX) and extra (XXXX) parts
	var costStyled string
	dotIdx := strings.Index(costFull, ".")
	if highlighted {
		// Highlight only the cost value, not " TOTAL"
		if dotIdx != -1 && len(costFull) > dotIdx+3 {
			mainPart := costFull[:dotIdx+3]
			extraPart := costFull[dotIdx+3:]
			costStyled = highlightStyle.Render(mainPart+extraPart) + heroCostStyle.Render(" TOTAL")
		} else {
			costStyled = highlightStyle.Render(costFull) + heroCostStyle.Render(" TOTAL")
		}
	} else if dotIdx != -1 && len(costFull) > dotIdx+3 {
		mainPart := costFull[:dotIdx+3]  // "$12.66"
		extraPart := costFull[dotIdx+3:] // "5834"
		costStyled = heroCostStyle.Render(mainPart) + dimStyle.Render(extraPart) + heroCostStyle.Render(" TOTAL")
	} else {
		costStyled = heroCostStyle.Render(costStr)
	}

	return dimStyle.Render(leftLine) + "[ " + costStyled + " ]" + dimStyle.Render(rightLine)
}

// renderUnifiedCostRow renders a single row with cost and token info combined
// Format: "  Label          $0.371042     53.9K tokens"
func (m Model) renderUnifiedCostRow(label string, cost float64, tokens int64, costField, tokenField string, labelColor lipgloss.Color, extra string) string {
	costHighlighted := m.isHighlighted(costField)
	tokenChanged := m.recentlyChanged(tokenField)
	tokenHighlighted := m.isHighlighted(tokenField)
	delta := m.deltaTokens[tokenField]

	// Format label with optional color
	var labelStr string
	if !m.noColor && labelColor != "" {
		labelStyle := lipgloss.NewStyle().Foreground(labelColor)
		labelStr = labelStyle.Render(fmt.Sprintf("%-14s", label))
	} else {
		labelStr = fmt.Sprintf("%-14s", label)
	}

	// Format cost (10 chars for "$123.456789")
	costStr := render.CostStyled(cost, 11, costHighlighted, m.noColor)

	// Format tokens
	var tokenStr string
	if tokenChanged {
		tokenStr = formatNumberWithDelta(tokens, delta, true)
		if tokenHighlighted {
			tokenStr = highlightStyle.Render(tokenStr)
		}
	} else {
		tokenStr = fmt.Sprintf("%12s", render.Number(tokens))
	}

	// Add extra info (like TTL)
	extraStr := ""
	if extra != "" {
		if !m.noColor {
			extraStr = "  " + dimStyle.Render(extra)
		} else {
			extraStr = "  " + extra
		}
	}

	return fmt.Sprintf("    %s %s  %s tokens%s\n", labelStr, costStr, tokenStr, extraStr)
}

// renderEmptyState renders a clean empty state for new sessions
func (m Model) renderEmptyState() string {
	var sb strings.Builder
	sectionWidth := panelWidthFor(m.width)

	// Hero cost (even $0.00 to establish visual anchor)
	sb.WriteString("\n")
	sb.WriteString(m.renderHeroCost(0, false, sectionWidth))
	sb.WriteString("\n\n")

	// Simple awaiting message centered
	msg := "Awaiting first message..."
	padding := (sectionWidth - len(msg)) / 2
	if padding < 0 {
		padding = 0
	}
	pad := strings.Repeat(" ", padding)

	if m.noColor {
		sb.WriteString(pad + msg + "\n")
	} else {
		sb.WriteString(pad + dimStyle.Render(msg) + "\n")
	}

	return sb.String()
}

// isEmptySession checks if this is an empty/new session with no real data
func (m Model) isEmptySession() bool {
	if m.analysis == nil {
		return true
	}
	// Session is empty if total cost is 0 and no messages
	return m.analysis.TotalCost.TotalCost == 0 && m.analysis.MessageCount == 0
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

// renderCostChart renders the cost trend sparkline with labels
func (m Model) renderCostChart() string {
	var sb strings.Builder

	// Find min and max for display
	var minCost, maxCost float64
	if len(m.costHistory) > 0 {
		minCost = m.costHistory[0]
		maxCost = m.costHistory[0]
		for _, cost := range m.costHistory {
			if cost < minCost {
				minCost = cost
			}
			if cost > maxCost {
				maxCost = cost
			}
		}
	}

	// Render the chart with proper indentation
	chartLines := strings.Split(m.costChart.View(), "\n")
	for _, line := range chartLines {
		if line != "" {
			sb.WriteString("    " + line + "\n")
		}
	}

	// Add scale labels below the chart
	if !m.noColor {
		scaleInfo := fmt.Sprintf("min: $%.4f  max: $%.4f  (%d msgs)",
			minCost, maxCost, len(m.costHistory))
		sb.WriteString("    " + dimStyle.Render(scaleInfo) + "\n")
	} else {
		sb.WriteString(fmt.Sprintf("    min: $%.4f  max: $%.4f  (%d msgs)\n",
			minCost, maxCost, len(m.costHistory)))
	}

	return sb.String()
}
