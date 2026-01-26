package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bardisty/ccusage/internal/analyzer"
	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/pricing"
	"github.com/bardisty/ccusage/internal/styles"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	analysisMsg    *models.SessionAnalysis
	errorMsg       error
	fileChangedMsg struct{}
	tickMsg        time.Time
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

	// Compare context window values
	oldContextSize := old.LastMessageUsage.ContextWindowSize()
	newContextSize := new.LastMessageUsage.ContextWindowSize()
	if oldContextSize != newContextSize {
		m.changedAt["context_window"] = now
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
	changed := m.recentlyChanged("messages")   // Use for delta display (works in noColor mode)
	highlighted := m.isHighlighted("messages") // Use for styling (false in noColor mode)
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

	title := "Token Breakdown (Cumulative)"
	if !m.noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString(title + "\n")

	u := m.analysis.TotalUsage
	msgCount := m.analysis.MessageCount

	// Token type colors for labels
	type tokenRow struct {
		label string
		field string
		value int
		color lipgloss.Color
	}

	rows := []tokenRow{
		{"Input tokens:", "input_tokens", u.InputTokens, lipgloss.Color("")},                // Default (no special color)
		{"Output tokens:", "output_tokens", u.OutputTokens, styles.OutputTokenColor},        // Light blue
		{"Cache write tokens:", "cache_write_tokens", u.CacheCreationInputTokens, styles.CacheWriteTokenColor}, // Orange
		{"Cache read tokens:", "cache_read_tokens", u.CacheReadInputTokens, styles.CacheReadTokenColor},        // Cyan
	}

	for _, row := range rows {
		changed := m.recentlyChanged(row.field)   // Use for delta display (works in noColor mode)
		highlighted := m.isHighlighted(row.field) // Use for styling (false in noColor mode)
		delta := m.deltaTokens[row.field]

		// Format the label with optional color
		var labelStr string
		if !m.noColor && row.color != "" {
			labelStyle := lipgloss.NewStyle().Foreground(row.color)
			labelStr = labelStyle.Render(fmt.Sprintf("%-20s", row.label))
		} else {
			labelStr = fmt.Sprintf("%-20s", row.label)
		}

		// Per-message average (dimmed in color mode)
		avgStr := formatAvg(row.value, msgCount)
		if !m.noColor && avgStr != "" {
			avgStr = dimStyle.Render(avgStr)
		}

		if changed {
			valStr := formatNumberWithDelta(row.value, delta, true)
			if highlighted {
				sb.WriteString(fmt.Sprintf("  %s %s  %s\n", labelStr, highlightStyle.Render(valStr), avgStr))
			} else {
				// noColor mode: show delta but without highlight styling
				sb.WriteString(fmt.Sprintf("  %s %s  %s\n", labelStr, valStr, avgStr))
			}
		} else {
			sb.WriteString(fmt.Sprintf("  %s %12s  %s\n", labelStr, formatNumber(row.value), avgStr))
		}
	}

	// Context window: last message's total input tokens (matches /context)
	contextSize := m.analysis.LastMessageUsage.ContextWindowSize()
	if contextSize > 0 {
		modelPricing := pricing.GetModelPricing(m.analysis.LastMessageModel)
		maxContext := modelPricing.MaxContextTokens
		contextPct := pricing.GetContextPercentage(modelPricing, contextSize)
		freeSpace := pricing.GetFreeSpace(modelPricing, contextSize)
		buffer := pricing.GetAutocompactBuffer(modelPricing)
		freePct := float64(freeSpace) / float64(maxContext) * 100

		highlighted := m.isHighlighted("context_window")

		// Get usage color based on context percentage
		usageColor := styles.GetContextUsageColor(contextPct)

		// Format context window line with dynamic color
		contextVal := formatNumber(contextSize)
		contextMeta := fmt.Sprintf("(%.0f%% of %s)", contextPct, formatNumber(maxContext))
		if highlighted {
			sb.WriteString(fmt.Sprintf("\n  Context Window:    %s  %s\n",
				highlightStyle.Render(fmt.Sprintf("%12s", contextVal)),
				dimStyle.Render(contextMeta)))
		} else if !m.noColor {
			// Apply usage-level color to the percentage
			coloredMeta := lipgloss.NewStyle().Foreground(usageColor).Render(
				fmt.Sprintf("(%.0f%% of %s)", contextPct, formatNumber(maxContext)))
			sb.WriteString(fmt.Sprintf("\n  Context Window:    %12s  %s\n",
				contextVal, coloredMeta))
		} else {
			sb.WriteString(fmt.Sprintf("\n  Context Window:    %12s  %s\n",
				contextVal, contextMeta))
		}

		// Progress bar
		sb.WriteString(fmt.Sprintf("    %s\n", formatContextProgressBar(contextSize, freeSpace, buffer, maxContext, m.noColor)))

		// Single line with Free space and Buffer (text colors hint at bar sections but stay readable)
		freeVal := formatNumber(freeSpace)
		bufferVal := formatNumber(buffer)
		if !m.noColor {
			// Free text highlights yellow when context changes, otherwise slightly brighter
			var freeStyled string
			if highlighted {
				freeStyled = highlightStyle.Render(fmt.Sprintf("Free: %s (%.1f%%)", freeVal, freePct))
			} else {
				freeStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(fmt.Sprintf("Free: %s (%.1f%%)", freeVal, freePct))
			}
			bufferStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(fmt.Sprintf("Buffer: %s (reserved)", bufferVal))
			sepStyled := dimStyle.Render("│")
			sb.WriteString(fmt.Sprintf("    %s  %s  %s\n", freeStyled, sepStyled, bufferStyled))
		} else {
			sb.WriteString(fmt.Sprintf("    Free: %s (%.1f%%)  │  Buffer: %s (reserved)\n", freeVal, freePct, bufferVal))
		}
	}

	return sb.String()
}

// formatAvg formats a per-message average
func formatAvg(total int, msgCount int) string {
	if msgCount == 0 {
		return ""
	}
	avg := total / msgCount
	return fmt.Sprintf("(avg: %s/msg)", formatNumber(avg))
}

func (m Model) renderCostByModel() string {
	var sb strings.Builder

	title := "Cost by Model"
	if !m.noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString(title + "\n")

	// Extract and sort by cost (highest first)
	type modelCost struct {
		id   string
		cost float64
	}
	models := make([]modelCost, 0, len(m.analysis.CostByModel))
	for id, breakdown := range m.analysis.CostByModel {
		models = append(models, modelCost{id, breakdown.TotalCost})
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].cost > models[j].cost
	})

	totalCost := m.analysis.TotalCost.TotalCost
	for _, mc := range models {
		cost := m.analysis.CostByModel[mc.id]
		modelName := pricing.GetModelDisplayName(mc.id)
		highlighted := m.isHighlighted("model_" + mc.id)

		// Apply model color to the label
		var labelStr string
		if !m.noColor {
			modelColor := getModelColor(mc.id)
			labelStyle := lipgloss.NewStyle().Foreground(modelColor)
			labelStr = labelStyle.Render(fmt.Sprintf("%-20s", modelName+":"))
		} else {
			labelStr = fmt.Sprintf("%-20s", modelName+":")
		}

		// Apply cost magnitude shading
		costStr := formatCostStyledWithMagnitude(cost.TotalCost, 14, highlighted, m.noColor, totalCost)
		sb.WriteString(fmt.Sprintf("  %s %s\n", labelStr, costStr))
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

	totalCost := a.TotalCost.TotalCost

	// Show parent session cost with highlight and magnitude shading
	parentHighlighted := m.isHighlighted("parent_cost")
	parentCostStr := formatCostStyledWithMagnitude(a.ParentCost.TotalCost, 14, parentHighlighted, m.noColor, totalCost)
	sb.WriteString(fmt.Sprintf("  %-20s %s\n", "Parent session:", parentCostStr))

	// Show each agent with highlight and magnitude shading
	for _, agent := range a.Agents {
		label := fmt.Sprintf("Agent %s:", agent.AgentID)
		agentHighlighted := m.isHighlighted("agent_" + agent.AgentID)
		costStr := formatCostStyledWithMagnitude(agent.TotalCost.TotalCost, 14, agentHighlighted, m.noColor, totalCost)
		sb.WriteString(fmt.Sprintf("  %-20s %s  (%d msgs)\n", label, costStr, agent.MessageCount))
	}

	// Show agents subtotal with highlight and magnitude shading
	subtotalHighlighted := m.isHighlighted("agents_subtotal")
	subtotalStr := formatCostStyledWithMagnitude(a.AgentsCost.TotalCost, 14, subtotalHighlighted, m.noColor, totalCost)
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

// formatCostStyledWithMagnitude returns a cost string with magnitude-based coloring
// Higher costs relative to total are brighter, lower costs are dimmer
func formatCostStyledWithMagnitude(cost float64, width int, highlighted bool, noColor bool, total float64) string {
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
		if highlighted {
			return padding + highlightStyle.Render(full)
		}
		magnitudeColor := getCostMagnitudeColor(cost, total)
		return padding + lipgloss.NewStyle().Foreground(magnitudeColor).Render(full)
	}

	main := full[:dotIdx+3]  // "$123.45"
	extra := full[dotIdx+3:] // "6789"

	if highlighted {
		return padding + highlightStyle.Render(main+extra)
	}

	// Apply magnitude coloring to main part, dim the extra
	magnitudeColor := getCostMagnitudeColor(cost, total)
	mainStyled := lipgloss.NewStyle().Foreground(magnitudeColor).Render(main)
	return padding + mainStyled + dimStyle.Render(extra)
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

// getModelColor returns the appropriate color for a model based on its tier
func getModelColor(modelID string) lipgloss.Color {
	modelLower := strings.ToLower(modelID)
	switch {
	case strings.Contains(modelLower, "opus"):
		return styles.OpusColor
	case strings.Contains(modelLower, "sonnet"):
		return styles.SonnetColor
	case strings.Contains(modelLower, "haiku"):
		return styles.HaikuColor
	default:
		return styles.SecondaryColor
	}
}

// getCostMagnitudeColor returns a color based on the cost's proportion of total
func getCostMagnitudeColor(cost, total float64) lipgloss.Color {
	if total == 0 {
		return styles.CostMediumColor
	}
	proportion := cost / total
	switch {
	case proportion > 0.5:
		return styles.CostHighColor
	case proportion >= 0.1:
		return styles.CostMediumColor
	default:
		return styles.CostLowColor
	}
}

// formatContextProgressBar creates a visual progress bar showing context usage
// Bar segments: used (█), free (░), buffer (▒)
// Total width: 50 characters
func formatContextProgressBar(contextSize, freeSpace, buffer, maxContext int, noColor bool) string {
	const barWidth = 50

	if maxContext == 0 {
		return strings.Repeat("░", barWidth)
	}

	// Calculate proportions
	usedRatio := float64(contextSize) / float64(maxContext)
	freeRatio := float64(freeSpace) / float64(maxContext)

	// Convert to bar segments
	usedChars := int(usedRatio * float64(barWidth))
	freeChars := int(freeRatio * float64(barWidth))
	bufferChars := barWidth - usedChars - freeChars

	// Ensure we don't go negative due to rounding
	if bufferChars < 0 {
		bufferChars = 0
	}
	// Adjust for rounding to hit exactly barWidth
	total := usedChars + freeChars + bufferChars
	if total < barWidth {
		freeChars += barWidth - total
	} else if total > barWidth {
		if freeChars > 0 {
			freeChars -= total - barWidth
		} else if usedChars > 0 {
			usedChars -= total - barWidth
		}
	}

	usedStr := strings.Repeat("█", usedChars)
	freeStr := strings.Repeat("░", freeChars)
	bufferStr := strings.Repeat("▒", bufferChars)

	if noColor {
		return "[" + usedStr + freeStr + bufferStr + "]"
	}

	// Get usage color based on percentage - only the used portion is colored
	usagePct := usedRatio * 100
	usageColor := styles.GetContextUsageColor(usagePct)

	// Free space is neutral light gray for contrast, buffer is darker gray
	usedStyled := lipgloss.NewStyle().Foreground(usageColor).Render(usedStr)
	freeStyled := lipgloss.NewStyle().Foreground(styles.ContextFreeColor).Render(freeStr)
	bufferStyled := lipgloss.NewStyle().Foreground(styles.ContextBufferColor).Render(bufferStr)

	return "[" + usedStyled + freeStyled + bufferStyled + "]"
}
