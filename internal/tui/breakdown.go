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

	messages    []models.BreakdownMessage
	totalCost   float64
	minCost     float64 // For cost gradient coloring
	maxCost     float64 // For cost gradient coloring
	insights    *models.MessageInsights
	err         error
	loading     bool
	lastUpdated time.Time

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
	closeOnce sync.Once
	watching  *atomic.Bool

	width  int
	height int
}

// Breakdown-specific messages
type (
	breakdownMsgsMsg struct {
		messages  []models.BreakdownMessage
		totalCost float64
		minCost   float64
		maxCost   float64
		insights  *models.MessageInsights
	}
	breakdownErrorMsg error
)

// NewBreakdownModel creates a new breakdown TUI model
func NewBreakdownModel(sessionPath, sessionID string, noColor bool) BreakdownModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	closing := &atomic.Bool{}
	watching := &atomic.Bool{}

	return BreakdownModel{
		sessionPath:   sessionPath,
		sessionID:     sessionID,
		noColor:       noColor,
		loading:       true,
		autoScroll:    true,
		spinner:       s,
		done:          make(chan struct{}),
		closing:       closing,
		watching:      watching,
		newMsgIndices: make(map[int]time.Time),
	}
}

// Init initializes the breakdown TUI
func (m BreakdownModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.loadBreakdown,
		func() tea.Msg { return m.watchFile() },
		tickCmd(),
	)
}

// Update handles messages
func (m BreakdownModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.closeOnce.Do(func() {
				m.closing.Store(true)
				if m.done != nil {
					close(m.done)
				}
				if m.watcher != nil {
					m.watcher.Close()
				}
			})
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
		// Header: Session(1) + LIVE/Updated(1) + blank(1) + insights(1) + blank(1) + table header(1) + separator(1)
		// Always use 7 to avoid layout shift when insights load after initial render
		headerHeight := 7
		footerHeight := 2 // Footer stats, help

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
			m.viewport.SetContent(m.renderTableContent())
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
		m.loading = false
		m.lastUpdated = time.Now()
		m.err = nil

		if m.ready {
			m.viewport.SetContent(m.renderTableContent())
			if m.autoScroll {
				m.viewport.GotoBottom()
			}
		}

	case breakdownErrorMsg:
		m.err = msg
		m.loading = false

	case watcherStartedMsg:
		m.watcher = msg.watcher
		return m, m.waitForFileChangeBreakdown()

	case fileChangedMsg:
		m.loading = true
		return m, tea.Batch(m.loadBreakdownCmd(), m.waitForFileChangeBreakdown())

	case tickMsg:
		// Clean up expired highlights
		m.cleanupExpiredHighlights()
		// Re-render to update highlight fading
		if m.ready && len(m.messages) > 0 {
			m.viewport.SetContent(m.renderTableContent())
		}
		return m, tickCmd()
	}

	return m, tea.Batch(cmds...)
}

// detectNewMessages compares old and new messages to find newly added ones
func (m *BreakdownModel) detectNewMessages(newMessages []models.BreakdownMessage) {
	now := time.Now()
	oldCount := len(m.messages)

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

	// Session ID (full, for matching with watch view)
	if !m.noColor {
		labelStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.AccentColor)
		idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		sb.WriteString(labelStyle.Render("Session:"))
		sb.WriteString(" ")
		sb.WriteString(idStyle.Render(m.sessionID))
	} else {
		sb.WriteString("Session: ")
		sb.WriteString(m.sessionID)
	}
	sb.WriteString("\n")

	// Live indicator
	sb.WriteString(liveIndicatorStyle.Render(" LIVE "))
	sb.WriteString(" ")

	if m.loading {
		sb.WriteString(m.spinner.View())
		sb.WriteString(" Loading...")
	} else if m.err != nil {
		sb.WriteString(fmt.Sprintf("Error: %v", m.err))
	} else {
		sb.WriteString(fmt.Sprintf("Updated: %s", m.lastUpdated.Format("15:04:05")))
	}
	sb.WriteString("\n\n")

	// Compact insights line
	if m.insights != nil && len(m.messages) > 0 {
		sb.WriteString(m.renderCompactInsights())
		sb.WriteString("\n\n")
	}

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

	// Footer with colored cost
	scrollMode := "AUTO"
	if !m.autoScroll {
		scrollMode = "MANUAL"
	}
	if !m.noColor {
		// Build footer with highlighted cost (6 decimals, trailing dimmed)
		msgPart := fmt.Sprintf("Messages: %d", len(m.messages))
		costStyled := formatCostWithDimDecimals(m.totalCost, styles.SuccessColor, 0)
		scrollPart := fmt.Sprintf("Scroll: %s", scrollMode)

		// Use lighter gray (250) for text
		lightGray := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
		sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

		sb.WriteString(lightGray.Render(msgPart))
		sb.WriteString(sepStyle.Render(" │ "))
		sb.WriteString(lightGray.Render("Total: "))
		sb.WriteString(costStyled)
		sb.WriteString(sepStyle.Render(" │ "))
		sb.WriteString(lightGray.Render(scrollPart))
	} else {
		sb.WriteString(fmt.Sprintf("Messages: %d │ Total: $%.6f │ Scroll: %s",
			len(m.messages), m.totalCost, scrollMode))
	}
	sb.WriteString("\n")

	// Help - use lighter gray
	if !m.noColor {
		helpText := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("q: quit • g/G: top/bottom • ↑↓: scroll")
		sb.WriteString(helpText)
	} else {
		sb.WriteString("q: quit • g/G: top/bottom • ↑↓: scroll")
	}

	return sb.String()
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

	return strings.Join(parts, " │ ")
}

// renderTableHeader renders the table header row
func (m BreakdownModel) renderTableHeader() string {
	// Width: cost(10) + space(1) + trend(1) = 12 for COST column
	// " COST" shifts header 1 char right to align with $ in values (assumes <$10 per message)
	header := fmt.Sprintf("%-5s  %-8s  %-10s  %-12s  %6s  %5s  %6s  %6s",
		"#", "TIME", "MODEL", " COST", "IN", "OUT", "C_WR", "C_RD")
	if !m.noColor {
		return headerStyle.Render(header)
	}
	return header
}

// renderTableSeparator renders the separator line
func (m BreakdownModel) renderTableSeparator() string {
	sep := strings.Repeat("-", 72)
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
	inStr := fmt.Sprintf("%6s", formatCompactNumber(msg.Usage.InputTokens))
	outStr := fmt.Sprintf("%5s", formatCompactNumber(msg.Usage.OutputTokens))
	cacheWriteStr := fmt.Sprintf("%6s", formatCompactNumber(msg.Usage.CacheCreationInputTokens))
	cacheReadStr := fmt.Sprintf("%6s", formatCompactNumber(msg.Usage.CacheReadInputTokens))

	// Get trend indicator
	trendSymbol, trendDirection := getRowTrendIndicator(msg.Cost.TotalCost, prevCost, isFirst)

	// Agent marker at the end
	agentMarker := ""
	if msg.AgentID != "" {
		agentMarker = fmt.Sprintf("[A%s]", msg.AgentID)
	}

	if m.noColor {
		return fmt.Sprintf("%s  %s  %s  %s %s  %s  %s  %s  %s  %s",
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
	costStyled := formatCostWithDimDecimals(msg.Cost.TotalCost, costColor, 10)

	// For new messages, override with highlight style
	if isNew {
		highlightStyle := styles.HighlightStyle
		return fmt.Sprintf("%s  %s  %s  %s %s  %s  %s  %s  %s  %s",
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

	return fmt.Sprintf("%s  %s  %s  %s %s  %s  %s  %s  %s  %s",
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
	messages, err := analyzer.GetBreakdownMessages(m.sessionPath, m.sessionID)
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
		messages:  messages,
		totalCost: totalCost,
		minCost:   minCost,
		maxCost:   maxCost,
		insights:  insights,
	}
}

func (m BreakdownModel) loadBreakdownCmd() tea.Cmd {
	return func() tea.Msg {
		return m.loadBreakdown()
	}
}

func (m BreakdownModel) watchFile() tea.Msg {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return breakdownErrorMsg(err)
	}

	err = watcher.Add(m.sessionPath)
	if err != nil {
		watcher.Close()
		return breakdownErrorMsg(err)
	}

	return watcherStartedMsg{watcher: watcher}
}

// waitForFileChangeBreakdown returns a command that waits for file changes
func (m BreakdownModel) waitForFileChangeBreakdown() tea.Cmd {
	return func() tea.Msg {
		if m.closing != nil && m.closing.Load() {
			return nil
		}
		if m.watcher == nil || m.done == nil {
			return nil
		}

		if m.watching != nil && !m.watching.CompareAndSwap(false, true) {
			return nil
		}
		defer func() {
			if m.watching != nil {
				m.watching.Store(false)
			}
		}()

		for {
			select {
			case <-m.done:
				return nil
			case event, ok := <-m.watcher.Events:
				if !ok {
					return nil
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					return fileChangedMsg{}
				}
				continue
			case err, ok := <-m.watcher.Errors:
				if !ok {
					return nil
				}
				return breakdownErrorMsg(err)
			}
		}
	}
}

// Per-message change symbols (distinct from session trend ▲/▼/═)
const (
	changeUp     = "↑"
	changeDown   = "↓"
	changeStable = "·"
)

// getRowTrendIndicator returns the change indicator for a message based on cost change
// from previous message. Uses 10% threshold for significance.
func getRowTrendIndicator(currentCost, previousCost float64, isFirst bool) (symbol string, direction models.TrendDirection) {
	if isFirst || previousCost == 0 {
		return changeStable, models.TrendStable
	}

	change := (currentCost - previousCost) / previousCost

	if change > 0.10 {
		return changeUp, models.TrendIncreasing
	} else if change < -0.10 {
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

// formatCostWithDimDecimals formats cost to 6 decimal places with trailing decimals dimmed.
// The main part ($X.XX) uses the provided color, extra decimals (XXXX) are dimmed gray.
func formatCostWithDimDecimals(cost float64, color lipgloss.Color, width int) string {
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

	// Calculate padding needed
	padding := ""
	if width > plainLen {
		padding = strings.Repeat(" ", width-plainLen)
	}

	// Split into main ($X.XX) and extra (XXXX) parts
	dotIdx := strings.Index(full, ".")
	if dotIdx == -1 || len(full) <= dotIdx+3 {
		return padding + lipgloss.NewStyle().Foreground(color).Render(full)
	}

	main := full[:dotIdx+3]  // "$0.09"
	extra := full[dotIdx+3:] // "3528"

	mainStyle := lipgloss.NewStyle().Foreground(color)
	dimStyle := lipgloss.NewStyle().Foreground(styles.SecondaryColor)

	return padding + mainStyle.Render(main) + dimStyle.Render(extra)
}

// formatCompactNumber formats a number compactly (e.g., "89.3K")
func formatCompactNumber(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
