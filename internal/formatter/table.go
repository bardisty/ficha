package formatter

import (
	"fmt"
	"strings"
	"time"

	"github.com/bah/ccusage/internal/models"
	"github.com/bah/ccusage/internal/pricing"
	"github.com/bah/ccusage/internal/styles"
)

// Local aliases for frequently used styles
var (
	headerStyle       = styles.HeaderStyle
	labelStyle        = styles.LabelStyle
	valueStyle        = styles.ValueStyle
	totalLabelStyle   = styles.TotalLabelStyle
	totalValueStyle   = styles.TotalValueStyle
	savingsLabelStyle = styles.SavingsLabelStyle
	savingsValueStyle = styles.SavingsValueStyle
	borderStyle       = styles.BorderStyle
	sessionIDStyle    = styles.SessionIDStyle
	footerStyle       = styles.FooterStyle
)

// FormatSessionTable formats a session analysis as a styled table
func FormatSessionTable(analysis *models.SessionAnalysis, noColor bool) string {
	if noColor {
		return formatSessionTablePlain(analysis)
	}

	var sb strings.Builder

	// Session header
	sb.WriteString(fmt.Sprintf("Session: %s\n\n", sessionIDStyle.Render(truncateID(analysis.SessionID))))

	// Table header (16-char value column for 6 decimal costs with padding)
	sb.WriteString(borderStyle.Render("┌───────────────────────────┬────────────────┐") + "\n")
	sb.WriteString(borderStyle.Render("│") +
		headerStyle.Render(fmt.Sprintf(" %-25s ", "Category")) +
		borderStyle.Render("│") +
		headerStyle.Render(fmt.Sprintf(" %14s ", "Amount")) +
		borderStyle.Render("│") + "\n")
	sb.WriteString(borderStyle.Render("├───────────────────────────┼────────────────┤") + "\n")

	// Cost rows
	rows := []struct {
		label string
		value float64
	}{
		{"Input tokens", analysis.TotalCost.InputCost},
		{"Output tokens", analysis.TotalCost.OutputCost},
	}

	if analysis.TotalCost.CacheWrite5mCost > 0 {
		rows = append(rows, struct {
			label string
			value float64
		}{"Cache write (5m TTL)", analysis.TotalCost.CacheWrite5mCost})
	}

	if analysis.TotalCost.CacheWrite1hCost > 0 {
		rows = append(rows, struct {
			label string
			value float64
		}{"Cache write (1h TTL)", analysis.TotalCost.CacheWrite1hCost})
	}

	if analysis.TotalCost.CacheReadCost > 0 {
		rows = append(rows, struct {
			label string
			value float64
		}{"Cache read", analysis.TotalCost.CacheReadCost})
	}

	for _, row := range rows {
		sb.WriteString(borderStyle.Render("│") +
			labelStyle.Render(fmt.Sprintf(" %-25s ", row.label)) +
			borderStyle.Render("│") +
			" " + formatCostStyled(row.value, 14, noColor) + " " +
			borderStyle.Render("│") + "\n")
	}

	// Separator and total
	sb.WriteString(borderStyle.Render("├───────────────────────────┼────────────────┤") + "\n")
	sb.WriteString(borderStyle.Render("│") +
		totalLabelStyle.Render(fmt.Sprintf(" %-25s ", "TOTAL")) +
		borderStyle.Render("│") +
		totalValueStyle.Render(" "+formatCostStyled(analysis.TotalCost.TotalCost, 14, noColor)+" ") +
		borderStyle.Render("│") + "\n")

	// Cache savings
	if analysis.TotalCost.CacheSavings > 0 {
		sb.WriteString(borderStyle.Render("│") +
			savingsLabelStyle.Render(fmt.Sprintf(" %-25s ", "Cache Savings")) +
			borderStyle.Render("│") +
			savingsValueStyle.Render(" "+formatCostStyled(analysis.TotalCost.CacheSavings, 14, noColor)+" ") +
			borderStyle.Render("│") + "\n")
	}

	sb.WriteString(borderStyle.Render("└───────────────────────────┴────────────────┘") + "\n")

	// Footer
	sb.WriteString("\n")
	sb.WriteString(footerStyle.Render(fmt.Sprintf("Messages: %d │ Duration: %s",
		analysis.MessageCount,
		formatDuration(analysis.Duration.Duration()))))

	// Token breakdown (always shown)
	sb.WriteString("\n\n")
	sb.WriteString(formatTokenBreakdown(analysis, noColor))

	// Cost by model (always shown)
	sb.WriteString("\n\n")
	sb.WriteString(formatCostByModel(analysis, noColor))

	// Agent breakdown (shown when agents exist)
	if analysis.HasAgents {
		sb.WriteString("\n\n")
		sb.WriteString(formatAgentBreakdown(analysis, noColor))
	}

	return sb.String()
}

// formatSessionTablePlain formats a session analysis as a plain text table
func formatSessionTablePlain(analysis *models.SessionAnalysis) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Session: %s\n\n", truncateID(analysis.SessionID)))

	sb.WriteString("+---------------------------+----------------+\n")
	sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Category", "Amount"))
	sb.WriteString("+---------------------------+----------------+\n")

	if analysis.TotalCost.InputCost > 0 {
		sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Input tokens", formatCost(analysis.TotalCost.InputCost)))
	}
	if analysis.TotalCost.OutputCost > 0 {
		sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Output tokens", formatCost(analysis.TotalCost.OutputCost)))
	}
	if analysis.TotalCost.CacheWrite5mCost > 0 {
		sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Cache write (5m TTL)", formatCost(analysis.TotalCost.CacheWrite5mCost)))
	}
	if analysis.TotalCost.CacheWrite1hCost > 0 {
		sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Cache write (1h TTL)", formatCost(analysis.TotalCost.CacheWrite1hCost)))
	}
	if analysis.TotalCost.CacheReadCost > 0 {
		sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Cache read", formatCost(analysis.TotalCost.CacheReadCost)))
	}

	sb.WriteString("+---------------------------+----------------+\n")
	sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "TOTAL", formatCost(analysis.TotalCost.TotalCost)))

	if analysis.TotalCost.CacheSavings > 0 {
		sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Cache Savings", formatCost(analysis.TotalCost.CacheSavings)))
	}

	sb.WriteString("+---------------------------+----------------+\n")

	sb.WriteString(fmt.Sprintf("\nMessages: %d | Duration: %s",
		analysis.MessageCount,
		formatDuration(analysis.Duration.Duration())))

	// Token breakdown (always shown)
	sb.WriteString("\n\n")
	sb.WriteString(formatTokenBreakdown(analysis, true))

	// Cost by model (always shown)
	sb.WriteString("\n\n")
	sb.WriteString(formatCostByModel(analysis, true))

	// Agent breakdown (shown when agents exist)
	if analysis.HasAgents {
		sb.WriteString("\n\n")
		sb.WriteString(formatAgentBreakdown(analysis, true))
	}

	return sb.String()
}

// formatTokenBreakdown formats detailed token breakdown
func formatTokenBreakdown(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	title := "Token Breakdown"
	if !noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString(title + "\n")

	usage := analysis.TotalUsage
	sb.WriteString(fmt.Sprintf("  Input tokens:       %12s\n", formatNumber(usage.InputTokens)))
	sb.WriteString(fmt.Sprintf("  Output tokens:      %12s\n", formatNumber(usage.OutputTokens)))
	sb.WriteString(fmt.Sprintf("  Cache write tokens: %12s\n", formatNumber(usage.CacheCreationInputTokens)))
	sb.WriteString(fmt.Sprintf("  Cache read tokens:  %12s\n", formatNumber(usage.CacheReadInputTokens)))

	if usage.CacheCreation != nil {
		sb.WriteString(fmt.Sprintf("    - 5m TTL:         %12s\n", formatNumber(usage.CacheCreation.Ephemeral5mInputTokens)))
		sb.WriteString(fmt.Sprintf("    - 1h TTL:         %12s\n", formatNumber(usage.CacheCreation.Ephemeral1hInputTokens)))
	}

	return sb.String()
}

// formatCostByModel formats cost breakdown by model
func formatCostByModel(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	title := "Cost by Model"
	if !noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString(title + "\n")

	for modelID, cost := range analysis.CostByModel {
		modelName := pricing.GetModelDisplayName(modelID)
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", modelName+":", formatCostStyled(cost.TotalCost, 14, noColor)))
	}

	return sb.String()
}

// formatAgentBreakdown formats the agent sub-sessions cost breakdown
func formatAgentBreakdown(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	title := fmt.Sprintf("Agent Sub-Sessions (%d)", analysis.AgentCount)
	if !noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString(title + "\n")

	// Show parent session cost
	sb.WriteString(fmt.Sprintf("  %-20s %s\n", "Parent session:", formatCostStyled(analysis.ParentCost.TotalCost, 14, noColor)))

	// Show each agent
	for _, agent := range analysis.Agents {
		label := fmt.Sprintf("Agent %s:", agent.AgentID)
		sb.WriteString(fmt.Sprintf("  %-20s %s  (%d msgs)\n", label, formatCostStyled(agent.TotalCost.TotalCost, 14, noColor), agent.MessageCount))
	}

	// Show agents subtotal
	sb.WriteString(fmt.Sprintf("  %-20s %s\n", "Agents subtotal:", formatCostStyled(analysis.AgentsCost.TotalCost, 14, noColor)))

	return sb.String()
}

// FormatSessionListTable formats a list of sessions as a table
func FormatSessionListTable(entries []models.SessionEntry, noColor bool) string {
	var sb strings.Builder

	if noColor {
		sb.WriteString(fmt.Sprintf("%-40s  %8s  %6s  %s\n", "Session ID", "Messages", "Agents", "Modified"))
		sb.WriteString(strings.Repeat("-", 85) + "\n")
	} else {
		sb.WriteString(headerStyle.Render(fmt.Sprintf("%-40s  %8s  %6s  %s", "Session ID", "Messages", "Agents", "Modified")) + "\n")
		sb.WriteString(borderStyle.Render(strings.Repeat("─", 85)) + "\n")
	}

	for _, entry := range entries {
		modified := entry.Modified.Format("2006-01-02 15:04")
		agentStr := "-"
		if entry.AgentCount > 0 {
			agentStr = fmt.Sprintf("%d", entry.AgentCount)
		}
		sb.WriteString(fmt.Sprintf("%-40s  %8d  %6s  %s\n",
			truncateID(entry.SessionID),
			entry.MessageCount,
			agentStr,
			modified))
	}

	return sb.String()
}

// Helper functions

func formatCost(cost float64) string {
	// Plain format with 6 decimal places
	return fmt.Sprintf("$%.6f", cost)
}

// formatCostStyled returns a cost string with extra precision (after 2 decimals) dimmed
func formatCostStyled(cost float64, width int, noColor bool) string {
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
		return padding + full
	}

	main := full[:dotIdx+3]  // "$123.45"
	extra := full[dotIdx+3:] // "6789"

	return padding + main + styles.DimStyle.Render(extra)
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
