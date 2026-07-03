package tui

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

// BreakdownModel is the Bubbletea model for the breakdown TUI
type BreakdownModel struct {
	sessionPath string
	sessionID   string
	noColor     bool

	messages     []models.BreakdownMessage
	totalCost    float64
	minCost      float64 // For cost gradient coloring
	maxCost      float64 // For cost gradient coloring
	insights     *models.MessageInsights
	skippedLines int // JSONL lines skipped during parsing (malformed or oversized)
	err          error
	loading      bool
	lastUpdated  time.Time

	// Viewport for scrolling
	viewport   viewport.Model
	autoScroll bool
	ready      bool // viewport initialized

	// Change tracking for highlight animation
	newMsgIndices map[int]time.Time // message index -> when it was added

	spinner   spinner.Model
	watcher   *fsnotify.Watcher
	done      chan struct{}
	closing   *atomic.Bool
	closeOnce *sync.Once
	wg        *sync.WaitGroup

	// Auto-follow mode for tracking new sessions
	projectDir     string          // Project directory to watch for new sessions
	followMode     bool            // Whether to auto-follow new sessions
	prevSessionID  string          // Previous session ID (shown after switch)
	sessionWatcher *SessionWatcher // Watches for new session files
	switchNotifyAt time.Time       // When session switch notification started

	width  int
	height int
}

// Breakdown-specific messages
type (
	breakdownMsgsMsg struct {
		messages     []models.BreakdownMessage
		totalCost    float64
		minCost      float64
		maxCost      float64
		insights     *models.MessageInsights
		skippedLines int
	}
	breakdownErrorMsg error
)

// NewBreakdownModel creates a new breakdown TUI model
func NewBreakdownModel(sessionPath, sessionID string, noColor bool, projectDir string, followMode bool) BreakdownModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	closing := &atomic.Bool{}
	closeOnce := &sync.Once{}

	return BreakdownModel{
		sessionPath:   sessionPath,
		sessionID:     sessionID,
		noColor:       noColor,
		loading:       true,
		autoScroll:    true,
		spinner:       s,
		done:          make(chan struct{}),
		closing:       closing,
		closeOnce:     closeOnce,
		wg:            &sync.WaitGroup{},
		newMsgIndices: make(map[int]time.Time),
		projectDir:    projectDir,
		followMode:    followMode,
	}
}

// Init initializes the breakdown TUI
func (m BreakdownModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.spinner.Tick,
		m.loadBreakdown,
		func() tea.Msg { return m.watchFile() },
		tickCmd(),
	}

	// Start session watcher if follow mode is enabled
	if m.followMode && m.projectDir != "" {
		cmds = append(cmds, m.startSessionWatcher())
	}

	return tea.Batch(cmds...)
}

// Update handles messages
func (m BreakdownModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			shutdownWatchers(m.closeOnce, m.closing, m.done, m.watcher, m.sessionWatcher, m.wg)
			return m, tea.Quit

		case "up", "k":
			m.autoScroll = false
			m.viewport.LineUp(1)

		case "down", "j":
			m.viewport.LineDown(1)
			// Re-enable auto-scroll if at bottom
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
		// Header: panel(3 lines) + blank/notify(1) + insights(1) + blank(1) + table header(1) + separator(1)
		// Always use 8 to avoid layout shift when insights load after initial render
		headerHeight := 8
		footerHeight := 4 // Double-line separator(1) + stats(1) + single-line(1) + help(1)

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

		// Re-render content with new dimensions
		if len(m.messages) > 0 {
			m.viewport.SetContent(clipToWidth(m.renderTableContent(), m.width))
			if m.autoScroll {
				m.viewport.GotoBottom()
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case breakdownMsgsMsg:
		// Track new messages for highlighting
		m.detectNewMessages(msg.messages)

		m.messages = msg.messages
		m.totalCost = msg.totalCost
		m.minCost = msg.minCost
		m.maxCost = msg.maxCost
		m.insights = msg.insights
		m.skippedLines = msg.skippedLines
		m.loading = false
		m.lastUpdated = time.Now()
		m.err = nil

		if m.ready {
			m.viewport.SetContent(clipToWidth(m.renderTableContent(), m.width))
			if m.autoScroll {
				m.viewport.GotoBottom()
			}
		}
		// Restart file watcher after data loads to avoid race condition
		// where the old watcher's defer hasn't executed yet when we start a new one
		return m, m.waitForFileChangeBreakdown()

	case breakdownErrorMsg:
		m.err = msg
		m.loading = false
		// Restart file watcher even after error to continue monitoring
		return m, m.waitForFileChangeBreakdown()

	case watcherStartedMsg:
		m.watcher = msg.watcher
		return m, m.waitForFileChangeBreakdown()

	case sessionWatcherStartedMsg:
		// Store the session watcher and start waiting for new sessions
		m.sessionWatcher = msg.watcher
		return m, m.waitForNewSession()

	case sessionWatcherRestartMsg:
		// Session was changed externally, restart waiting
		return m, m.waitForNewSession()

	case fileChangedMsg:
		m.loading = true
		// Watcher restart moved to breakdownMsgsMsg handler to avoid race condition
		return m, m.loadBreakdownCmd()

	case sessionSwitchedMsg:
		// Store previous session ID and switch to new session
		m.prevSessionID = m.sessionID
		m.sessionID = msg.newSessionID
		m.sessionPath = msg.newSessionPath
		m.switchNotifyAt = time.Now()

		// Reset state for clean switch
		m.messages = nil
		m.insights = nil
		m.totalCost = 0
		m.minCost = 0
		m.maxCost = 0
		m.skippedLines = 0
		m.loading = true
		m.newMsgIndices = make(map[int]time.Time)

		// Stop old file watcher, will be restarted by watchFile
		if m.watcher != nil {
			m.watcher.Close()
			m.watcher = nil
		}

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

	case tickMsg:
		// Re-render only while highlights are active — an idle table would
		// otherwise be fully re-rendered every tick. Capture before cleanup:
		// the tick that expires the last highlight still needs one final
		// re-render to un-highlight its rows.
		hadHighlights := len(m.newMsgIndices) > 0
		m.cleanupExpiredHighlights()
		if hadHighlights && m.ready && len(m.messages) > 0 {
			m.viewport.SetContent(clipToWidth(m.renderTableContent(), m.width))
		}
		return m, tickCmd()
	}

	return m, tea.Batch(cmds...)
}

// detectNewMessages compares old and new messages to find newly added ones
func (m *BreakdownModel) detectNewMessages(newMessages []models.BreakdownMessage) {
	oldCount := len(m.messages)

	// Nothing to diff against on first load (or right after a session switch):
	// flagging every row would flash the whole table as "new"
	if oldCount == 0 {
		return
	}

	now := time.Now()

	// Any message with index > oldCount is new
	for _, msg := range newMessages {
		if msg.Index > oldCount {
			m.newMsgIndices[msg.Index] = now
		}
	}
}

// cleanupExpiredHighlights removes highlight entries older than highlightDuration
func (m *BreakdownModel) cleanupExpiredHighlights() {
	for idx, addedAt := range m.newMsgIndices {
		if time.Since(addedAt) > highlightDuration {
			delete(m.newMsgIndices, idx)
		}
	}
}

// isNewMessage checks if a message should be highlighted as new
func (m *BreakdownModel) isNewMessage(index int) bool {
	if m.noColor {
		return false
	}
	addedAt, exists := m.newMsgIndices[index]
	if !exists {
		return false
	}
	return time.Since(addedAt) < highlightDuration
}

// View renders the breakdown TUI
func (m BreakdownModel) View() string {
	var sb strings.Builder
	panelWidth := panelWidthFor(m.width)

	// Header panel
	sb.WriteString(m.renderHeaderPanel(panelWidth))

	// Show switch notification after session switch
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

	// Compact insights line
	if m.insights != nil && len(m.messages) > 0 {
		sb.WriteString(m.renderCompactInsights())
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// Table header
	sb.WriteString(m.renderTableHeader())
	sb.WriteString("\n")

	// Separator
	sb.WriteString(m.renderTableSeparator())
	sb.WriteString("\n")

	// Viewport with scrollable content
	if m.ready {
		sb.WriteString(m.viewport.View())
	}
	sb.WriteString("\n")

	// Double-line footer separator
	footerSep := strings.Repeat(styles.BoxHorizontal, panelWidth)
	if !m.noColor {
		footerSep = panelBorderStyle.Render(footerSep)
	}
	sb.WriteString("  " + footerSep + "\n")

	// Footer with colored cost
	scrollMode := "AUTO"
	if !m.autoScroll {
		scrollMode = "MANUAL"
	}
	if !m.noColor {
		// Build footer with highlighted cost (6 decimals, trailing dimmed)
		msgPart := fmt.Sprintf("Messages: %d", len(m.messages))
		costStyled := render.CostWithDimDecimals(m.totalCost, styles.SuccessColor, 0)
		scrollPart := fmt.Sprintf("Scroll: %s", scrollMode)

		// Use lighter gray (250) for text
		lightGray := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
		sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

		sb.WriteString("  ")
		sb.WriteString(lightGray.Render(msgPart))
		sb.WriteString(sepStyle.Render(" │ "))
		sb.WriteString(lightGray.Render("Total: "))
		sb.WriteString(costStyled)
		sb.WriteString(sepStyle.Render(" │ "))
		sb.WriteString(lightGray.Render(scrollPart))
		// Surface parse warnings so an incomplete breakdown doesn't look complete
		if m.skippedLines > 0 {
			warnStyle := lipgloss.NewStyle().Foreground(styles.WarningColor)
			sb.WriteString(sepStyle.Render(" │ "))
			sb.WriteString(warnStyle.Render(fmt.Sprintf("⚠ %d skipped line(s)", m.skippedLines)))
		}
	} else {
		sb.WriteString(fmt.Sprintf("  Messages: %d │ Total: $%.6f │ Scroll: %s",
			len(m.messages), m.totalCost, scrollMode))
		if m.skippedLines > 0 {
			sb.WriteString(fmt.Sprintf(" │ ! %d skipped line(s)", m.skippedLines))
		}
	}
	sb.WriteString("\n")

	// Single-line separator before help
	helpSep := strings.Repeat(styles.LineHorizontal, panelWidth)
	if !m.noColor {
		helpSep = dimStyle.Render(helpSep)
	}
	sb.WriteString("  " + helpSep + "\n")

	// Help - use lighter gray
	if !m.noColor {
		helpText := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("q: quit • g/G: top/bottom • ↑↓: scroll")
		sb.WriteString("  " + helpText)
	} else {
		sb.WriteString("  q: quit • g/G: top/bottom • ↑↓: scroll")
	}

	return clipToWidth(sb.String(), m.width)
}

// renderHeaderPanel renders the mainframe-style header panel with session info
func (m BreakdownModel) renderHeaderPanel(width int) string {
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

// renderCompactInsights renders a single line of insights
func (m BreakdownModel) renderCompactInsights() string {
	if m.insights == nil {
		return ""
	}

	var parts []string

	// Peak cost with message number for easy lookup
	if m.insights.HighestCost != nil {
		mult := m.insights.CostMultiplier()
		peakStr := fmt.Sprintf("Peak: #%d %s @ %s (%.1fx avg)",
			m.insights.HighestCost.Index,
			formatCompactCost(m.insights.HighestCost.Cost),
			m.insights.HighestCost.Timestamp.Format("15:04"),
			mult)
		if !m.noColor {
			parts = append(parts, lipgloss.NewStyle().Foreground(styles.WarningColor).Render(peakStr))
		} else {
			parts = append(parts, peakStr)
		}
	}

	// Trend
	if m.insights.MessageCount >= 5 {
		trendDesc := m.insights.TrendDescription()
		trendSymbol := m.insights.CostTrend.Symbol()
		trendStr := fmt.Sprintf("Trend: %s %s", trendSymbol, trendDesc)
		if !m.noColor {
			var color lipgloss.Color
			switch m.insights.CostTrend {
			case models.TrendIncreasing:
				color = styles.WarningColor
			case models.TrendDecreasing:
				color = styles.SuccessColor
			default:
				color = styles.SecondaryColor
			}
			parts = append(parts, lipgloss.NewStyle().Foreground(color).Render(trendStr))
		} else {
			parts = append(parts, trendStr)
		}
	}

	return "  " + strings.Join(parts, " │ ")
}

// renderTableHeader renders the table header row
func (m BreakdownModel) renderTableHeader() string {
	// Width: cost(10) + space(1) + trend(1) = 12 for COST column
	// " COST" shifts header 1 char right to align with $ in values (assumes <$10 per message)
	header := fmt.Sprintf("  %-5s  %-8s  %-10s  %-12s  %6s  %5s  %6s  %6s",
		"#", "TIME", "MODEL", " COST", "IN", "OUT", "C_WR", "C_RD")
	if !m.noColor {
		return headerStyle.Render(header)
	}
	return header
}

// renderTableSeparator renders the separator line using Unicode box-drawing characters
func (m BreakdownModel) renderTableSeparator() string {
	sep := "  " + strings.Repeat(styles.LineHorizontal, panelWidthFor(m.width))
	if !m.noColor {
		return tableBorderStyle.Render(sep)
	}
	return sep
}

// renderTableContent renders all message rows for the viewport
func (m BreakdownModel) renderTableContent() string {
	var sb strings.Builder
	var prevCost float64

	for i, msg := range m.messages {
		isFirst := i == 0
		sb.WriteString(m.renderRow(msg, m.isNewMessage(msg.Index), prevCost, isFirst))
		prevCost = msg.Cost.TotalCost
		if i < len(m.messages)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// renderRow renders a single message row
func (m BreakdownModel) renderRow(msg models.BreakdownMessage, isNew bool, prevCost float64, isFirst bool) string {
	// Format column values
	indexStr := fmt.Sprintf("%-5d", msg.Index)
	timeStr := fmt.Sprintf("%-8s", msg.Timestamp.Format("15:04:05"))
	modelName := pricing.GetModelDisplayName(msg.Model)
	modelStr := fmt.Sprintf("%-10s", modelName)
	// Cost: 6 decimal places, 10 char width (e.g., "$0.093528" = 9 chars)
	costStr := fmt.Sprintf("%-10s", fmt.Sprintf("$%.6f", msg.Cost.TotalCost))
	inStr := fmt.Sprintf("%6s", render.Number(msg.Usage.InputTokens))
	outStr := fmt.Sprintf("%5s", render.Number(msg.Usage.OutputTokens))
	cacheWriteStr := fmt.Sprintf("%6s", render.Number(msg.Usage.CacheCreationInputTokens))
	cacheReadStr := fmt.Sprintf("%6s", render.Number(msg.Usage.CacheReadInputTokens))

	// Get trend indicator
	trendSymbol, trendDirection := getRowTrendIndicator(msg.Cost.TotalCost, prevCost, isFirst)

	// Agent marker at the end
	agentMarker := ""
	if msg.AgentID != "" {
		agentMarker = fmt.Sprintf("[A%s]", msg.AgentID)
	}

	if m.noColor {
		return fmt.Sprintf("  %s  %s  %s  %s %s  %s  %s  %s  %s  %s",
			indexStr, timeStr, modelStr, costStr, trendSymbol, inStr, outStr, cacheWriteStr, cacheReadStr, agentMarker)
	}

	// Apply per-column styling
	dimStyle := lipgloss.NewStyle().Foreground(styles.SecondaryColor)
	modelStyle := lipgloss.NewStyle().Foreground(styles.GetModelColor(modelName))
	costColor := styles.GetCostGradientColor(msg.Cost.TotalCost, m.minCost, m.maxCost)
	outStyle := lipgloss.NewStyle().Foreground(styles.OutputTokenColor)
	cacheWriteStyle := lipgloss.NewStyle().Foreground(styles.CacheWriteTokenColor)
	cacheReadStyle := lipgloss.NewStyle().Foreground(styles.CacheReadTokenColor)

	// Color the trend indicator based on direction
	var trendStyled string
	switch trendDirection {
	case models.TrendIncreasing:
		trendStyled = lipgloss.NewStyle().Foreground(styles.WarningColor).Render(trendSymbol)
	case models.TrendDecreasing:
		trendStyled = lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(trendSymbol)
	default:
		trendStyled = dimStyle.Render(trendSymbol)
	}

	// Format cost with dimmed trailing decimals (main $X.XX colored, XXXX dimmed)
	costStyled := render.CostWithDimDecimals(msg.Cost.TotalCost, costColor, 10)

	// For new messages, override with highlight style
	if isNew {
		highlightStyle := styles.HighlightStyle
		return fmt.Sprintf("  %s  %s  %s  %s %s  %s  %s  %s  %s  %s",
			highlightStyle.Render(indexStr),
			highlightStyle.Render(timeStr),
			highlightStyle.Render(modelStr),
			highlightStyle.Render(costStr),
			highlightStyle.Render(trendSymbol),
			highlightStyle.Render(inStr),
			highlightStyle.Render(outStr),
			highlightStyle.Render(cacheWriteStr),
			highlightStyle.Render(cacheReadStr),
			highlightStyle.Render(agentMarker))
	}

	// Agent marker with its own color
	agentRendered := ""
	if agentMarker != "" {
		agentStyle := lipgloss.NewStyle().Foreground(styles.GetAgentColor(msg.AgentID))
		agentRendered = agentStyle.Render(agentMarker)
	}

	return fmt.Sprintf("  %s  %s  %s  %s %s  %s  %s  %s  %s  %s",
		dimStyle.Render(indexStr),
		dimStyle.Render(timeStr),
		modelStyle.Render(modelStr),
		costStyled,
		trendStyled,
		inStr, // Input stays white/default
		outStyle.Render(outStr),
		cacheWriteStyle.Render(cacheWriteStr),
		cacheReadStyle.Render(cacheReadStr),
		agentRendered)
}

// Commands

func (m BreakdownModel) loadBreakdown() tea.Msg {
	if m.closing != nil && m.closing.Load() {
		return nil
	}
	messages, skippedLines, err := analyzer.GetBreakdownMessages(m.sessionPath, m.sessionID)
	if err != nil {
		return breakdownErrorMsg(err)
	}

	// Calculate total cost, min/max cost, and get insights
	var totalCost float64
	var minCost, maxCost float64
	var messageAnalyses []models.MessageAnalysis

	for i, msg := range messages {
		totalCost += msg.Cost.TotalCost

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

		messageAnalyses = append(messageAnalyses, models.MessageAnalysis{
			Timestamp: msg.Timestamp,
			Model:     msg.Model,
			Usage:     msg.Usage,
			Cost:      msg.Cost,
		})
	}

	insights := analyzer.CalculateInsights(messageAnalyses)

	return breakdownMsgsMsg{
		messages:     messages,
		totalCost:    totalCost,
		minCost:      minCost,
		maxCost:      maxCost,
		insights:     insights,
		skippedLines: skippedLines,
	}
}

func (m BreakdownModel) loadBreakdownCmd() tea.Cmd {
	return func() tea.Msg {
		return m.loadBreakdown()
	}
}

// wrapErr adapts this model's error message type for the shared watcher
// commands in file_watcher.go (audit TUI-5).
func (m BreakdownModel) wrapErr(err error) tea.Msg { return breakdownErrorMsg(err) }

// The watcher lifecycle lives in file_watcher.go, shared with app.go.
func (m BreakdownModel) watchFile() tea.Msg { return watchFileCmd(m.sessionPath, m.wrapErr) }

func (m BreakdownModel) waitForFileChangeBreakdown() tea.Cmd {
	return waitForFileChangeCmd(m.wg, m.closing, m.watcher, m.done, m.sessionPath, m.wrapErr)
}

// Per-message change symbols (distinct from session trend ▲/▼/═)
const (
	changeUp     = "↑"
	changeDown   = "↓"
	changeStable = "·"
)

// getRowTrendIndicator returns the change indicator for a message based on cost change
// from previous message. Uses 5% threshold for significance.
func getRowTrendIndicator(currentCost, previousCost float64, isFirst bool) (symbol string, direction models.TrendDirection) {
	if isFirst || previousCost == 0 {
		return changeStable, models.TrendStable
	}

	change := (currentCost - previousCost) / previousCost

	if change > 0.05 {
		return changeUp, models.TrendIncreasing
	} else if change < -0.05 {
		return changeDown, models.TrendDecreasing
	}
	return changeStable, models.TrendStable
}

// Compact formatting helpers

// formatCompactCost formats a cost value compactly (e.g., "$0.0512")
func formatCompactCost(cost float64) string {
	if cost >= 100 {
		return fmt.Sprintf("$%.2f", cost)
	}
	if cost >= 10 {
		return fmt.Sprintf("$%.3f", cost)
	}
	return fmt.Sprintf("$%.4f", cost)
}

// Cost-with-dim-decimals and compact-number formatting now live in
// internal/render (audit DUP-1); formatCompactNumber was byte-identical to
// render.Number.

func (m BreakdownModel) startSessionWatcher() tea.Cmd {
	return startSessionWatcherCmd(m.projectDir, m.sessionID, m.wrapErr)
}

func (m BreakdownModel) waitForNewSession() tea.Cmd {
	return waitForNewSessionCmd(m.wg, m.sessionWatcher)
}
