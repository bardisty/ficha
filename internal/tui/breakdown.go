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
		// Build footer with highlighted cost
		msgPart := fmt.Sprintf("Messages: %d", len(m.messages))
		costPart := formatCompactCost(m.totalCost)
		scrollPart := fmt.Sprintf("Scroll: %s", scrollMode)

		// Use lighter gray (250) for text, green for cost
		lightGray := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
		costStyle := lipgloss.NewStyle().Foreground(styles.SuccessColor).Bold(true)
		sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

		sb.WriteString(lightGray.Render(msgPart))
		sb.WriteString(sepStyle.Render(" │ "))
		sb.WriteString(lightGray.Render("Total: "))
		sb.WriteString(costStyle.Render(costPart))
		sb.WriteString(sepStyle.Render(" │ "))
		sb.WriteString(lightGray.Render(scrollPart))
	} else {
		sb.WriteString(fmt.Sprintf("Messages: %d │ Total: %s │ Scroll: %s",
			len(m.messages), formatCompactCost(m.totalCost), scrollMode))
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
	header := fmt.Sprintf("%-5s  %-8s  %-10s  %-10s  %6s  %5s  %7s",
		"#", "TIME", "MODEL", "COST", "IN", "OUT", "CACHE")
	if !m.noColor {
		return headerStyle.Render(header)
	}
	return header
}

// renderTableSeparator renders the separator line
func (m BreakdownModel) renderTableSeparator() string {
	sep := strings.Repeat("-", 62)
	if !m.noColor {
		return tableBorderStyle.Render(sep)
	}
	return sep
}

// renderTableContent renders all message rows for the viewport
func (m BreakdownModel) renderTableContent() string {
	var sb strings.Builder

	for i, msg := range m.messages {
		sb.WriteString(m.renderRow(msg, m.isNewMessage(msg.Index)))
		if i < len(m.messages)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// renderRow renders a single message row
func (m BreakdownModel) renderRow(msg models.BreakdownMessage, isNew bool) string {
	// Index
	indexStr := fmt.Sprintf("%d", msg.Index)

	// Model display name (short form like "Sonnet 4", "Haiku 4.5")
	modelName := pricing.GetModelDisplayName(msg.Model)

	// Agent marker at the end
	agentMarker := ""
	if msg.AgentID != "" {
		agentMarker = fmt.Sprintf("[A%s]", msg.AgentID)
	}

	// Format row content
	row := fmt.Sprintf("%-5s  %-8s  %-10s  %-10s  %6s  %5s  %7s  %s",
		indexStr,
		msg.Timestamp.Format("15:04:05"),
		modelName,
		formatCompactCost(msg.Cost.TotalCost),
		formatCompactNumber(msg.Usage.InputTokens),
		formatCompactNumber(msg.Usage.OutputTokens),
		formatCompactNumber(msg.Usage.CacheReadInputTokens),
		agentMarker)

	if m.noColor {
		return row
	}

	if isNew {
		return styles.HighlightStyle.Render(row)
	}
	return row
}

// Commands

func (m BreakdownModel) loadBreakdown() tea.Msg {
	messages, err := analyzer.GetBreakdownMessages(m.sessionPath, m.sessionID)
	if err != nil {
		return breakdownErrorMsg(err)
	}

	// Calculate total cost and get insights
	var totalCost float64
	var messageAnalyses []models.MessageAnalysis
	for _, msg := range messages {
		totalCost += msg.Cost.TotalCost
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
