package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/bah/ccusage/internal/analyzer"
	"github.com/bah/ccusage/internal/models"
	"github.com/bah/ccusage/internal/pricing"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// Highlight duration for changed values
const highlightDuration = 2 * time.Second

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
	deltaTokens map[string]int // Delta values for token counts
	deltaCount  int            // Delta for message count

	spinner spinner.Model
	watcher *fsnotify.Watcher
	done    chan struct{} // Channel to signal watcher goroutine to stop

	width  int
	height int
}

// Messages
type (
	analysisMsg   *models.SessionAnalysis
	errorMsg      error
	fileChangedMsg struct{}
	tickMsg       time.Time
)

// NewModel creates a new TUI model
func NewModel(sessionPath, sessionID string, verbose, noColor bool) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	return Model{
		sessionPath: sessionPath,
		sessionID:   sessionID,
		verbose:     verbose,
		noColor:     noColor,
		loading:     true,
		spinner:     s,
		done:        make(chan struct{}),
		changedAt:   make(map[string]time.Time),
		deltaTokens: make(map[string]int),
	}
}

// Init initializes the TUI
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.loadAnalysis,
		func() tea.Msg { return m.watchFile() },
		tickCmd(),
	)
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
			// Signal the watcher goroutine to stop
			if m.done != nil {
				close(m.done)
				m.done = nil // Prevent double close
			}
			if m.watcher != nil {
				m.watcher.Close()
				m.watcher = nil
			}
			return m, tea.Quit
		case "r":
			m.loading = true
			return m, m.loadAnalysis
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

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

	case errorMsg:
		m.err = msg
		m.loading = false

	case watcherStartedMsg:
		// Store the watcher and start listening for file changes
		m.watcher = msg.watcher
		return m, m.waitForFileChange()

	case fileChangedMsg:
		m.loading = true
		// After handling the change, continue waiting for more changes
		return m, tea.Batch(m.loadAnalysis, m.waitForFileChange())

	case tickMsg:
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

// recentlyChanged checks if a field was recently changed (within highlight duration)
// This is used to determine whether to show delta values, regardless of color mode
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

// View renders the TUI
func (m Model) View() string {
	var sb strings.Builder

	// Title
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Session: %s", truncateID(m.sessionID))))
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

	// Analysis content
	if m.analysis != nil {
		sb.WriteString(m.renderAnalysis())
	}

	// Help
	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("q: quit • r: refresh"))

	return sb.String()
}

func (m Model) renderAnalysis() string {
	if m.noColor {
		return m.renderAnalysisPlain()
	}

	var sb strings.Builder
	a := m.analysis

	// Table (14-char value column for 6 decimal costs like "$123.456789")
	sb.WriteString(tableBorderStyle.Render("┌───────────────────────────┬────────────────┐") + "\n")
	sb.WriteString(tableBorderStyle.Render("│") +
		headerStyle.Render(fmt.Sprintf(" %-25s ", "Category")) +
		tableBorderStyle.Render("│") +
		headerStyle.Render(fmt.Sprintf(" %14s ", "Amount")) +
		tableBorderStyle.Render("│") + "\n")
	sb.WriteString(tableBorderStyle.Render("├───────────────────────────┼────────────────┤") + "\n")

	// Cost rows with field tracking for highlights
	type costRow struct {
		label string
		value float64
		field string // field name for highlight tracking
	}

	rows := []costRow{
		{"Input tokens", a.TotalCost.InputCost, "input_cost"},
		{"Output tokens", a.TotalCost.OutputCost, "output_cost"},
	}

	if a.TotalCost.CacheWrite5mCost > 0 {
		rows = append(rows, costRow{"Cache write (5m TTL)", a.TotalCost.CacheWrite5mCost, "cache_write_5m"})
	}

	if a.TotalCost.CacheWrite1hCost > 0 {
		rows = append(rows, costRow{"Cache write (1h TTL)", a.TotalCost.CacheWrite1hCost, "cache_write_1h"})
	}

	if a.TotalCost.CacheReadCost > 0 {
		rows = append(rows, costRow{"Cache read", a.TotalCost.CacheReadCost, "cache_read"})
	}

	for _, row := range rows {
		highlighted := m.isHighlighted(row.field)
		sb.WriteString(tableBorderStyle.Render("│") +
			labelStyle.Render(fmt.Sprintf(" %-25s ", row.label)) +
			tableBorderStyle.Render("│") +
			" " + formatCostStyled(row.value, 14, highlighted, m.noColor) + " " +
			tableBorderStyle.Render("│") + "\n")
	}

	// Total row
	sb.WriteString(tableBorderStyle.Render("├───────────────────────────┼────────────────┤") + "\n")
	totalHighlighted := m.isHighlighted("total")
	var totalCostStr string
	if totalHighlighted {
		totalCostStr = " " + formatCostStyled(a.TotalCost.TotalCost, 14, true, m.noColor) + " "
	} else {
		totalCostStr = totalValueStyle.Render(" " + formatCostStyled(a.TotalCost.TotalCost, 14, false, m.noColor) + " ")
	}
	sb.WriteString(tableBorderStyle.Render("│") +
		totalLabelStyle.Render(fmt.Sprintf(" %-25s ", "TOTAL")) +
		tableBorderStyle.Render("│") +
		totalCostStr +
		tableBorderStyle.Render("│") + "\n")

	// Savings row with highlight
	if a.TotalCost.CacheSavings > 0 {
		savingsHighlighted := m.isHighlighted("savings")
		var savingsCostStr string
		if savingsHighlighted {
			savingsCostStr = " " + formatCostStyled(a.TotalCost.CacheSavings, 14, true, m.noColor) + " "
		} else {
			savingsCostStr = savingsValueStyle.Render(" " + formatCostStyled(a.TotalCost.CacheSavings, 14, false, m.noColor) + " ")
		}
		sb.WriteString(tableBorderStyle.Render("│") +
			savingsLabelStyle.Render(fmt.Sprintf(" %-25s ", "Cache Savings")) +
			tableBorderStyle.Render("│") +
			savingsCostStr +
			tableBorderStyle.Render("│") + "\n")
	}

	sb.WriteString(tableBorderStyle.Render("└───────────────────────────┴────────────────┘") + "\n")

	// Footer with message count highlight and delta
	sb.WriteString("\n")
	var footerLine string
	changed := m.recentlyChanged("messages")     // Use for delta display (works in noColor mode)
	highlighted := m.isHighlighted("messages")   // Use for styling (false in noColor mode)
	if changed {
		// Show delta when recently changed
		var msgStr string
		if m.deltaCount > 0 {
			msgStr = fmt.Sprintf("Messages: %d (+%d)", a.MessageCount, m.deltaCount)
		} else if m.deltaCount < 0 {
			msgStr = fmt.Sprintf("Messages: %d (%d)", a.MessageCount, m.deltaCount)
		} else {
			msgStr = fmt.Sprintf("Messages: %d", a.MessageCount)
		}
		if highlighted {
			footerLine = highlightStyle.Render(msgStr) +
				footerStyle.Render(fmt.Sprintf(" │ Duration: %s", formatDuration(a.Duration.Duration())))
		} else {
			// noColor mode: show delta but without highlight styling
			footerLine = msgStr + fmt.Sprintf(" │ Duration: %s", formatDuration(a.Duration.Duration()))
		}
	} else {
		footerLine = footerStyle.Render(fmt.Sprintf("Messages: %d │ Duration: %s",
			a.MessageCount, formatDuration(a.Duration.Duration())))
	}
	sb.WriteString(footerLine)

	// Token breakdown (always shown)
	sb.WriteString("\n\n")
	sb.WriteString(m.renderTokenBreakdown())

	// Cost by model (always shown)
	sb.WriteString("\n\n")
	sb.WriteString(m.renderCostByModel())

	// Agent breakdown (shown when agents exist)
	if a.HasAgents {
		sb.WriteString("\n\n")
		sb.WriteString(m.renderAgentBreakdown())
	}

	return sb.String()
}

func (m Model) renderAnalysisPlain() string {
	var sb strings.Builder
	a := m.analysis

	sb.WriteString("+---------------------------+----------------+\n")
	sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Category", "Amount"))
	sb.WriteString("+---------------------------+----------------+\n")

	if a.TotalCost.InputCost > 0 {
		sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Input tokens", formatCost(a.TotalCost.InputCost)))
	}
	if a.TotalCost.OutputCost > 0 {
		sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Output tokens", formatCost(a.TotalCost.OutputCost)))
	}
	if a.TotalCost.CacheWrite5mCost > 0 {
		sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Cache write (5m TTL)", formatCost(a.TotalCost.CacheWrite5mCost)))
	}
	if a.TotalCost.CacheWrite1hCost > 0 {
		sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Cache write (1h TTL)", formatCost(a.TotalCost.CacheWrite1hCost)))
	}
	if a.TotalCost.CacheReadCost > 0 {
		sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Cache read", formatCost(a.TotalCost.CacheReadCost)))
	}

	sb.WriteString("+---------------------------+----------------+\n")
	sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "TOTAL", formatCost(a.TotalCost.TotalCost)))

	if a.TotalCost.CacheSavings > 0 {
		sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Cache Savings", formatCost(a.TotalCost.CacheSavings)))
	}

	sb.WriteString("+---------------------------+----------------+\n")

	// Show message count with delta if recently changed
	if m.recentlyChanged("messages") && m.deltaCount != 0 {
		var deltaStr string
		if m.deltaCount > 0 {
			deltaStr = fmt.Sprintf(" (+%d)", m.deltaCount)
		} else {
			deltaStr = fmt.Sprintf(" (%d)", m.deltaCount)
		}
		sb.WriteString(fmt.Sprintf("\nMessages: %d%s | Duration: %s",
			a.MessageCount, deltaStr,
			formatDuration(a.Duration.Duration())))
	} else {
		sb.WriteString(fmt.Sprintf("\nMessages: %d | Duration: %s",
			a.MessageCount,
			formatDuration(a.Duration.Duration())))
	}

	// Token breakdown (always shown)
	sb.WriteString("\n\n")
	sb.WriteString(m.renderTokenBreakdown())

	// Cost by model (always shown)
	sb.WriteString("\n\n")
	sb.WriteString(m.renderCostByModel())

	// Agent breakdown (shown when agents exist)
	if a.HasAgents {
		sb.WriteString("\n\n")
		sb.WriteString(m.renderAgentBreakdown())
	}

	return sb.String()
}

func (m Model) renderTokenBreakdown() string {
	var sb strings.Builder

	title := "Token Breakdown"
	if !m.noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString(title + "\n")

	u := m.analysis.TotalUsage

	// Helper to format a token line with optional highlight and delta
	formatLine := func(label, field string, value int) string {
		changed := m.recentlyChanged(field) // Use for delta display (works in noColor mode)
		highlighted := m.isHighlighted(field) // Use for styling (false in noColor mode)
		delta := m.deltaTokens[field]

		if changed {
			valStr := formatNumberWithDelta(value, delta, true)
			if highlighted {
				return fmt.Sprintf("  %-20s %s\n", label, highlightStyle.Render(valStr))
			}
			// noColor mode: show delta but without highlight styling
			return fmt.Sprintf("  %-20s %s\n", label, valStr)
		}
		return fmt.Sprintf("  %-20s %12s\n", label, formatNumber(value))
	}

	sb.WriteString(formatLine("Input tokens:", "input_tokens", u.InputTokens))
	sb.WriteString(formatLine("Output tokens:", "output_tokens", u.OutputTokens))
	sb.WriteString(formatLine("Cache write tokens:", "cache_write_tokens", u.CacheCreationInputTokens))
	sb.WriteString(formatLine("Cache read tokens:", "cache_read_tokens", u.CacheReadInputTokens))

	return sb.String()
}

func (m Model) renderCostByModel() string {
	var sb strings.Builder

	title := "Cost by Model"
	if !m.noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString(title + "\n")

	for modelID, cost := range m.analysis.CostByModel {
		modelName := pricing.GetModelDisplayName(modelID)
		highlighted := m.isHighlighted("model_" + modelID)
		costStr := formatCostStyled(cost.TotalCost, 14, highlighted, m.noColor)
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", modelName+":", costStr))
	}

	return sb.String()
}

func (m Model) renderAgentBreakdown() string {
	var sb strings.Builder
	a := m.analysis

	title := fmt.Sprintf("Agent Sub-Sessions (%d)", a.AgentCount)
	if !m.noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString(title + "\n")

	// Show parent session cost with highlight
	parentHighlighted := m.isHighlighted("parent_cost")
	parentCostStr := formatCostStyled(a.ParentCost.TotalCost, 14, parentHighlighted, m.noColor)
	sb.WriteString(fmt.Sprintf("  %-20s %s\n", "Parent session:", parentCostStr))

	// Show each agent with highlight
	for _, agent := range a.Agents {
		label := fmt.Sprintf("Agent %s:", agent.AgentID)
		agentHighlighted := m.isHighlighted("agent_" + agent.AgentID)
		costStr := formatCostStyled(agent.TotalCost.TotalCost, 14, agentHighlighted, m.noColor)
		sb.WriteString(fmt.Sprintf("  %-20s %s  (%d msgs)\n", label, costStr, agent.MessageCount))
	}

	// Show agents subtotal with highlight
	subtotalHighlighted := m.isHighlighted("agents_subtotal")
	subtotalStr := formatCostStyled(a.AgentsCost.TotalCost, 14, subtotalHighlighted, m.noColor)
	sb.WriteString(fmt.Sprintf("  %-20s %s\n", "Agents subtotal:", subtotalStr))

	return sb.String()
}

// Commands

func (m Model) loadAnalysis() tea.Msg {
	analysis, err := analyzer.AnalyzeSession(m.sessionPath, m.sessionID, m.verbose)
	if err != nil {
		return errorMsg(err)
	}
	return analysisMsg(analysis)
}

// loadAnalysisCmd returns a command that loads the analysis
func (m Model) loadAnalysisCmd() tea.Cmd {
	return func() tea.Msg {
		return m.loadAnalysis()
	}
}

// watcherStartedMsg is sent when the watcher is successfully created
type watcherStartedMsg struct {
	watcher *fsnotify.Watcher
}

func (m Model) watchFile() tea.Msg {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errorMsg(err)
	}

	err = watcher.Add(m.sessionPath)
	if err != nil {
		watcher.Close()
		return errorMsg(err)
	}

	// Return the watcher to be stored in the model
	return watcherStartedMsg{watcher: watcher}
}

// waitForFileChange returns a command that waits for file changes or shutdown
func (m Model) waitForFileChange() tea.Cmd {
	return func() tea.Msg {
		if m.watcher == nil || m.done == nil {
			return nil
		}

		select {
		case <-m.done:
			// Shutdown signal received
			return nil
		case event, ok := <-m.watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				return fileChangedMsg{}
			}
			// For other events, keep waiting
			return m.waitForFileChange()()
		case err, ok := <-m.watcher.Errors:
			if !ok {
				return nil
			}
			return errorMsg(err)
		}
	}
}

// Helper functions

func formatCost(cost float64) string {
	// Plain format with 6 decimal places
	return fmt.Sprintf("$%.6f", cost)
}

// formatCostStyled returns a cost string with extra precision (after 2 decimals) dimmed
// When highlighted, the entire cost is shown in highlight color
// The width parameter pads the result to a fixed visual width (ignoring ANSI codes)
func formatCostStyled(cost float64, width int, highlighted bool, noColor bool) string {
	// Format to 6 decimal places: "$123.456789"
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

	// Calculate padding needed
	padding := ""
	if width > plainLen {
		padding = strings.Repeat(" ", width-plainLen)
	}

	if noColor {
		return padding + full
	}

	// Split into main ($X.XX) and extra (XXXX) parts
	dotIdx := strings.Index(full, ".")
	if dotIdx == -1 || len(full) <= dotIdx+3 {
		// No decimal or not enough digits
		if highlighted {
			return padding + highlightStyle.Render(full)
		}
		return padding + full
	}

	main := full[:dotIdx+3]  // "$123.45"
	extra := full[dotIdx+3:] // "6789"

	if highlighted {
		return padding + highlightStyle.Render(main+extra)
	}
	return padding + main + dimStyle.Render(extra)
}

func formatNumber(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.2fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// formatNumberWithDelta formats a number with optional delta during highlight
func formatNumberWithDelta(n int, delta int, showDelta bool) string {
	valStr := formatNumber(n)
	if !showDelta || delta == 0 {
		return fmt.Sprintf("%12s", valStr)
	}

	// Format delta
	var deltaStr string
	if delta > 0 {
		deltaStr = fmt.Sprintf("(+%s)", formatNumber(delta))
	} else {
		deltaStr = fmt.Sprintf("(-%s)", formatNumber(-delta))
	}

	return fmt.Sprintf("%12s %s", valStr, deltaStr)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

func truncateID(id string) string {
	if len(id) <= 40 {
		return id
	}
	return id[:37] + "..."
}
