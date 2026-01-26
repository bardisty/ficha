package formatter

import (
	"fmt"
	"strings"
	"time"

	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/pricing"
	"github.com/bardisty/ccusage/internal/styles"
	"github.com/charmbracelet/lipgloss"
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

	// Message insights (shown when insights are available)
	if analysis.Insights != nil {
		sb.WriteString("\n\n")
		sb.WriteString(formatInsightsSection(analysis.Insights, noColor))
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

	// Always show input and output tokens (consistent with color formatter)
	sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Input tokens", formatCost(analysis.TotalCost.InputCost)))
	sb.WriteString(fmt.Sprintf("| %-25s | %14s |\n", "Output tokens", formatCost(analysis.TotalCost.OutputCost)))

	// Only show cache rows if they have values
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

	// Message insights (shown when insights are available)
	if analysis.Insights != nil {
		sb.WriteString("\n\n")
		sb.WriteString(formatInsightsSection(analysis.Insights, true))
	}

	return sb.String()
}

// formatTokenBreakdown formats detailed token breakdown
func formatTokenBreakdown(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	title := "Token Breakdown (Cumulative)"
	if !noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString(title + "\n")

	usage := analysis.TotalUsage
	msgCount := analysis.MessageCount

	// Format each row with per-message average (dimmed in color mode)
	formatAvgStyled := func(total, count int) string {
		avg := formatAvg(total, count)
		if !noColor && avg != "" {
			return styles.DimStyle.Render(avg)
		}
		return avg
	}

	sb.WriteString(fmt.Sprintf("  Input tokens:       %12s  %s\n",
		formatNumber(usage.InputTokens), formatAvgStyled(usage.InputTokens, msgCount)))
	sb.WriteString(fmt.Sprintf("  Output tokens:      %12s  %s\n",
		formatNumber(usage.OutputTokens), formatAvgStyled(usage.OutputTokens, msgCount)))
	sb.WriteString(fmt.Sprintf("  Cache write tokens: %12s  %s\n",
		formatNumber(usage.CacheCreationInputTokens), formatAvgStyled(usage.CacheCreationInputTokens, msgCount)))
	sb.WriteString(fmt.Sprintf("  Cache read tokens:  %12s  %s\n",
		formatNumber(usage.CacheReadInputTokens), formatAvgStyled(usage.CacheReadInputTokens, msgCount)))

	if usage.CacheCreation != nil {
		sb.WriteString(fmt.Sprintf("    - 5m TTL:         %12s\n", formatNumber(usage.CacheCreation.Ephemeral5mInputTokens)))
		sb.WriteString(fmt.Sprintf("    - 1h TTL:         %12s\n", formatNumber(usage.CacheCreation.Ephemeral1hInputTokens)))
	}

	// Context window: last message's total input tokens (matches /context)
	contextSize := analysis.LastMessageUsage.ContextWindowSize()
	if contextSize > 0 {
		modelPricing := pricing.GetModelPricing(analysis.LastMessageModel)
		maxContext := modelPricing.MaxContextTokens
		contextPct := pricing.GetContextPercentage(modelPricing, contextSize)
		freeSpace := pricing.GetFreeSpace(modelPricing, contextSize)
		buffer := pricing.GetAutocompactBuffer(modelPricing)
		freePct := float64(freeSpace) / float64(maxContext) * 100

		// Get usage color based on context percentage
		usageColor := styles.GetContextUsageColor(contextPct)

		// Context window header with dynamic color
		contextVal := formatNumber(contextSize)
		if !noColor {
			coloredMeta := lipgloss.NewStyle().Foreground(usageColor).Render(
				fmt.Sprintf("(%.0f%% of %s)", contextPct, formatNumber(maxContext)))
			sb.WriteString(fmt.Sprintf("\n  Context Window:     %12s  %s\n",
				contextVal, coloredMeta))
		} else {
			sb.WriteString(fmt.Sprintf("\n  Context Window:     %12s  (%.0f%% of %s)\n",
				contextVal, contextPct, formatNumber(maxContext)))
		}

		// Progress bar
		sb.WriteString(fmt.Sprintf("    %s\n", formatContextProgressBar(contextSize, freeSpace, buffer, maxContext, noColor)))

		// Single line with Free space and Buffer (text colors hint at bar sections but stay readable)
		freeVal := formatNumber(freeSpace)
		bufferVal := formatNumber(buffer)
		if !noColor {
			// Moderate colors: Free slightly brighter, Buffer slightly dimmer (both readable)
			freeStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(fmt.Sprintf("Free: %s (%.1f%%)", freeVal, freePct))
			bufferStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(fmt.Sprintf("Buffer: %s (reserved)", bufferVal))
			sepStyled := styles.DimStyle.Render("│")
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

// formatInsightsSection formats the message insights section
func formatInsightsSection(insights *models.MessageInsights, noColor bool) string {
	var sb strings.Builder

	title := "Message Insights"
	if !noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString(title + "\n")

	// First message
	if insights.FirstMessage != nil {
		first := insights.FirstMessage
		componentLabel := formatCostComponentLabel(first.MainCostComponent)
		if !noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s\n",
				"First:",
				formatCostStyled(first.Cost, 10, noColor),
				first.Timestamp.Format("15:04:05"),
				styles.DimStyle.Render(fmt.Sprintf("%s: %s", componentLabel, formatCostStyled(first.MainCostValue, 0, noColor)))))
		} else {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s: %s\n",
				"First:",
				formatCostStyled(first.Cost, 10, noColor),
				first.Timestamp.Format("15:04:05"),
				componentLabel,
				formatCostStyled(first.MainCostValue, 0, noColor)))
		}
	}

	// Last message
	if insights.LastMessage != nil {
		last := insights.LastMessage
		componentLabel := formatCostComponentLabel(last.MainCostComponent)
		if !noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s\n",
				"Last:",
				formatCostStyled(last.Cost, 10, noColor),
				last.Timestamp.Format("15:04:05"),
				styles.DimStyle.Render(fmt.Sprintf("%s: %s", componentLabel, formatCostStyled(last.MainCostValue, 0, noColor)))))
		} else {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s: %s\n",
				"Last:",
				formatCostStyled(last.Cost, 10, noColor),
				last.Timestamp.Format("15:04:05"),
				componentLabel,
				formatCostStyled(last.MainCostValue, 0, noColor)))
		}
	}

	// Highest cost (only if notably above average)
	if insights.HighestCost != nil {
		highest := insights.HighestCost
		multiplier := insights.CostMultiplier()
		warningStr := fmt.Sprintf("%.1fx avg cost", multiplier)
		if !noColor {
			warningStyled := lipgloss.NewStyle().Foreground(styles.WarningColor).Render("⚠ " + warningStr)
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s\n",
				"Highest:",
				formatCostStyled(highest.Cost, 10, noColor),
				highest.Timestamp.Format("15:04:05"),
				warningStyled))
		} else {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  ! %s\n",
				"Highest:",
				formatCostStyled(highest.Cost, 10, noColor),
				highest.Timestamp.Format("15:04:05"),
				warningStr))
		}
	}

	// Trend (only for sessions with 5+ messages)
	if insights.MessageCount >= 5 {
		trendDesc := insights.TrendDescription()
		trendSymbol := insights.CostTrend.Symbol()

		earlyStr := fmt.Sprintf("$%.2f/msg", insights.EarlyAvgCost)
		lateStr := fmt.Sprintf("$%.2f/msg", insights.LateAvgCost)

		if !noColor {
			// Color the trend symbol based on direction
			var symbolStyled string
			switch insights.CostTrend {
			case models.TrendIncreasing:
				symbolStyled = lipgloss.NewStyle().Foreground(styles.WarningColor).Render(trendSymbol)
			case models.TrendDecreasing:
				symbolStyled = lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(trendSymbol)
			default:
				symbolStyled = styles.DimStyle.Render(trendSymbol)
			}
			sb.WriteString(fmt.Sprintf("  %-10s %s → %s  %s %s\n",
				"Trend:",
				earlyStr,
				lateStr,
				symbolStyled,
				trendDesc))
		} else {
			sb.WriteString(fmt.Sprintf("  %-10s %s -> %s  %s %s\n",
				"Trend:",
				earlyStr,
				lateStr,
				trendSymbol,
				trendDesc))
		}
	}

	return sb.String()
}

// formatCostComponentLabel returns a human-readable label for a cost component
func formatCostComponentLabel(component string) string {
	switch component {
	case "input":
		return "input"
	case "output":
		return "output"
	case "cache_write_5m":
		return "cache_write"
	case "cache_write_1h":
		return "cache_write"
	case "cache_read":
		return "cache_read"
	default:
		return component
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
