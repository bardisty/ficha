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
	savingsLabelStyle  = styles.SavingsLabelStyle
	savingsValueStyle  = styles.SavingsValueStyle
	footerStyle        = styles.FooterStyle
	heroCostStyle      = styles.HeroCostStyle
	sectionHeaderStyle = styles.SectionHeaderStyle
	panelBorderStyle   = styles.PanelBorderStyle
	dimStyle           = styles.DimStyle
)

// FormatSessionTable formats a session analysis as a styled table
func FormatSessionTable(analysis *models.SessionAnalysis, noColor bool) string {
	if noColor {
		return formatSessionTablePlain(analysis)
	}

	var sb strings.Builder
	const sectionWidth = 76

	// Detect summary vs show mode
	isSummary := strings.HasPrefix(analysis.SessionID, "Summary")

	// Header panel
	sb.WriteString(renderHeaderPanel(analysis, sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Hero total cost as section header
	sb.WriteString(renderHeroCost(analysis.TotalCost.TotalCost, sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Unified cost+token rows
	// Input tokens
	sb.WriteString(renderUnifiedCostRow("Input", analysis.TotalCost.InputCost, analysis.TotalUsage.InputTokens, lipgloss.Color(""), "", noColor))

	// Output tokens
	sb.WriteString(renderUnifiedCostRow("Output", analysis.TotalCost.OutputCost, analysis.TotalUsage.OutputTokens, styles.OutputTokenColor, "", noColor))

	// Cache write rows - show tokens on first row that has cost
	cacheWriteTokens := analysis.TotalUsage.CacheCreationInputTokens
	has5mCost := analysis.TotalCost.CacheWrite5mCost > 0
	has1hCost := analysis.TotalCost.CacheWrite1hCost > 0

	if has5mCost {
		sb.WriteString(renderUnifiedCostRow("Cache write", analysis.TotalCost.CacheWrite5mCost, cacheWriteTokens, styles.CacheWriteTokenColor, "5m TTL", noColor))
	}

	if has1hCost {
		tokens := 0
		if !has5mCost {
			tokens = cacheWriteTokens
		}
		sb.WriteString(renderUnifiedCostRow("Cache write", analysis.TotalCost.CacheWrite1hCost, tokens, styles.CacheWriteTokenColor, "1h TTL", noColor))
	}

	// Cache read - only show if present
	if analysis.TotalCost.CacheReadCost > 0 || analysis.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(renderUnifiedCostRow("Cache read", analysis.TotalCost.CacheReadCost, analysis.TotalUsage.CacheReadInputTokens, styles.CacheReadTokenColor, "", noColor))
	}

	// Savings row
	if analysis.TotalCost.CacheSavings > 0 {
		savingsStr := formatCostStyledGreen(analysis.TotalCost.CacheSavings, 11, noColor)
		sb.WriteString(fmt.Sprintf("  %s %s  %s\n",
			savingsLabelStyle.Render(fmt.Sprintf("%-14s", "Savings")),
			savingsStr,
			dimStyle.Render("(from cache reads)")))
	}

	// Context window section - only for single sessions, not summaries
	if !isSummary {
		contextSize := analysis.LastMessageUsage.ContextWindowSize()
		if contextSize > 0 {
			sb.WriteString("\n")
			sb.WriteString(renderContextSection(analysis, noColor))
		}
	}

	// Cost by model section
	sb.WriteString("\n")
	sb.WriteString(renderSectionHeader("COST BY MODEL", sectionWidth, noColor))
	sb.WriteString("\n\n")
	sb.WriteString(formatCostByModelContent(analysis, noColor))

	// Agent breakdown (shown when agents exist)
	if analysis.HasAgents {
		sb.WriteString("\n")
		sb.WriteString(renderSectionHeader("AGENT SUB-SESSIONS", sectionWidth, noColor))
		sb.WriteString("\n\n")
		sb.WriteString(formatAgentBreakdownContent(analysis, noColor))
	}

	// Message insights (shown when insights are available)
	if analysis.Insights != nil {
		sb.WriteString("\n")
		sb.WriteString(renderSectionHeader("MESSAGE INSIGHTS", sectionWidth, noColor))
		sb.WriteString("\n\n")
		sb.WriteString(formatInsightsSectionContent(analysis.Insights, noColor))
	}

	// Footer with double-line separator
	sb.WriteString("\n")
	sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, sectionWidth)))
	sb.WriteString("\n")

	// Footer stats
	msgStr := fmt.Sprintf("%d", analysis.MessageCount)
	if analysis.AgentMessageCount > 0 {
		msgStr = fmt.Sprintf("%d (%d parent, %d agents)",
			analysis.MessageCount, analysis.ParentMessageCount, analysis.AgentMessageCount)
	}

	footerText := fmt.Sprintf("Messages: %s", msgStr)
	if isSummary {
		// Extract session count from the ID (format: "Summary (N sessions)")
		var sessionCount int
		_, _ = fmt.Sscanf(analysis.SessionID, "Summary (%d sessions)", &sessionCount)
		if sessionCount > 0 {
			footerText += fmt.Sprintf("  │  Sessions: %d", sessionCount)
		}
	} else {
		// Show last active time for single sessions
		if !analysis.EndTime.IsZero() {
			footerText += fmt.Sprintf("  │  Last active: %s", analysis.EndTime.Local().Format("2006-01-02 15:04"))
		}
	}

	sb.WriteString(footerStyle.Render(footerText))
	sb.WriteString("\n")

	// Single-line help separator
	sb.WriteString(dimStyle.Render(strings.Repeat(styles.LineHorizontal, sectionWidth)))

	return sb.String()
}

// formatSessionTablePlain formats a session analysis as a plain text table
func formatSessionTablePlain(analysis *models.SessionAnalysis) string {
	var sb strings.Builder
	const sectionWidth = 76

	// Detect summary vs show mode
	isSummary := strings.HasPrefix(analysis.SessionID, "Summary")

	// Header panel
	sb.WriteString(renderHeaderPanel(analysis, sectionWidth, true))
	sb.WriteString("\n\n")

	// Hero total cost as section header
	sb.WriteString(renderHeroCost(analysis.TotalCost.TotalCost, sectionWidth, true))
	sb.WriteString("\n\n")

	// Unified cost+token rows
	sb.WriteString(renderUnifiedCostRowPlain("Input", analysis.TotalCost.InputCost, analysis.TotalUsage.InputTokens, ""))
	sb.WriteString(renderUnifiedCostRowPlain("Output", analysis.TotalCost.OutputCost, analysis.TotalUsage.OutputTokens, ""))

	// Cache write rows
	cacheWriteTokens := analysis.TotalUsage.CacheCreationInputTokens
	has5mCost := analysis.TotalCost.CacheWrite5mCost > 0
	has1hCost := analysis.TotalCost.CacheWrite1hCost > 0

	if has5mCost {
		sb.WriteString(renderUnifiedCostRowPlain("Cache write", analysis.TotalCost.CacheWrite5mCost, cacheWriteTokens, "5m TTL"))
	}

	if has1hCost {
		tokens := 0
		if !has5mCost {
			tokens = cacheWriteTokens
		}
		sb.WriteString(renderUnifiedCostRowPlain("Cache write", analysis.TotalCost.CacheWrite1hCost, tokens, "1h TTL"))
	}

	if analysis.TotalCost.CacheReadCost > 0 || analysis.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(renderUnifiedCostRowPlain("Cache read", analysis.TotalCost.CacheReadCost, analysis.TotalUsage.CacheReadInputTokens, ""))
	}

	// Savings row
	if analysis.TotalCost.CacheSavings > 0 {
		sb.WriteString(fmt.Sprintf("  %-14s %11s  (from cache reads)\n",
			"Savings", formatCost(analysis.TotalCost.CacheSavings)))
	}

	// Context window section - only for single sessions, not summaries
	if !isSummary {
		contextSize := analysis.LastMessageUsage.ContextWindowSize()
		if contextSize > 0 {
			sb.WriteString("\n")
			sb.WriteString(renderContextSection(analysis, true))
		}
	}

	// Cost by model section
	sb.WriteString("\n")
	sb.WriteString(renderSectionHeader("COST BY MODEL", sectionWidth, true))
	sb.WriteString("\n\n")
	sb.WriteString(formatCostByModelContent(analysis, true))

	// Agent breakdown (shown when agents exist)
	if analysis.HasAgents {
		sb.WriteString("\n")
		sb.WriteString(renderSectionHeader("AGENT SUB-SESSIONS", sectionWidth, true))
		sb.WriteString("\n\n")
		sb.WriteString(formatAgentBreakdownContent(analysis, true))
	}

	// Message insights (shown when insights are available)
	if analysis.Insights != nil {
		sb.WriteString("\n")
		sb.WriteString(renderSectionHeader("MESSAGE INSIGHTS", sectionWidth, true))
		sb.WriteString("\n\n")
		sb.WriteString(formatInsightsSectionContent(analysis.Insights, true))
	}

	// Footer with double-line separator
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("=", sectionWidth))
	sb.WriteString("\n")

	// Footer stats
	msgStr := fmt.Sprintf("%d", analysis.MessageCount)
	if analysis.AgentMessageCount > 0 {
		msgStr = fmt.Sprintf("%d (%d parent, %d agents)",
			analysis.MessageCount, analysis.ParentMessageCount, analysis.AgentMessageCount)
	}

	footerText := fmt.Sprintf("Messages: %s", msgStr)
	if isSummary {
		var sessionCount int
		_, _ = fmt.Sscanf(analysis.SessionID, "Summary (%d sessions)", &sessionCount)
		if sessionCount > 0 {
			footerText += fmt.Sprintf("  |  Sessions: %d", sessionCount)
		}
	} else {
		// Show last active time for single sessions
		if !analysis.EndTime.IsZero() {
			footerText += fmt.Sprintf("  |  Last active: %s", analysis.EndTime.Local().Format("2006-01-02 15:04"))
		}
	}

	sb.WriteString(footerText)
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", sectionWidth))

	return sb.String()
}

// Rendering helpers (matching TUI style)

// renderHeaderPanel renders the mainframe-style header panel
// Format:
// ╔══════════════════════════════════════════════════════════════════════════╗
// ║  Session: xxx  │  Duration: Xh Ym                                        ║
// ╚══════════════════════════════════════════════════════════════════════════╝
func renderHeaderPanel(analysis *models.SessionAnalysis, width int, noColor bool) string {
	var sb strings.Builder

	if width < 40 {
		width = 76
	}

	innerWidth := width - 6 // 2 for borders, 2 for left padding, 2 for right padding

	// Detect summary vs show mode
	isSummary := strings.HasPrefix(analysis.SessionID, "Summary")

	// Build content parts - use consistent format for both plain and styled
	var titlePart string
	var sessionCount int
	if isSummary {
		// Extract session count from ID (format: "Summary (N sessions)")
		_, _ = fmt.Sscanf(analysis.SessionID, "Summary (%d sessions)", &sessionCount)
		titlePart = fmt.Sprintf("Summary: %d sessions", sessionCount)
	} else {
		titlePart = fmt.Sprintf("Session: %s", truncateID(analysis.SessionID))
	}

	durationPart := fmt.Sprintf("Duration: %s", formatDuration(analysis.Duration.Duration()))

	// Calculate content length
	sep := styles.BoxVerticalSep
	content := fmt.Sprintf("%s  %s  %s", titlePart, sep, durationPart)
	contentLen := len(titlePart) + 2 + 1 + 2 + len(durationPart)
	padding := innerWidth - contentLen
	if padding < 0 {
		padding = 0
	}

	if noColor {
		// Top border
		sb.WriteString(styles.BoxTopLeft)
		sb.WriteString(strings.Repeat(styles.BoxHorizontal, width-2))
		sb.WriteString(styles.BoxTopRight)
		sb.WriteString("\n")

		// Content line
		sb.WriteString(styles.BoxVertical)
		sb.WriteString("  ")
		sb.WriteString(content)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString("  ")
		sb.WriteString(styles.BoxVertical)
		sb.WriteString("\n")

		// Bottom border
		sb.WriteString(styles.BoxBottomLeft)
		sb.WriteString(strings.Repeat(styles.BoxHorizontal, width-2))
		sb.WriteString(styles.BoxBottomRight)
	} else {
		// Build styled content - matches plain text format
		var titleStyled string
		if isSummary {
			titleStyled = fmt.Sprintf("%s %d sessions",
				sectionHeaderStyle.Render("Summary:"),
				sessionCount)
		} else {
			titleStyled = fmt.Sprintf("%s %s",
				sectionHeaderStyle.Render("Session:"),
				truncateID(analysis.SessionID))
		}

		durationStyled := fmt.Sprintf("Duration: %s", formatDuration(analysis.Duration.Duration()))
		sepStyled := panelBorderStyle.Render(sep)

		// Top border
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopRight))
		sb.WriteString("\n")

		// Content line
		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("  ")
		sb.WriteString(titleStyled)
		sb.WriteString("  ")
		sb.WriteString(sepStyled)
		sb.WriteString("  ")
		sb.WriteString(durationStyled)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString("  ")
		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("\n")

		// Bottom border
		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomRight))
	}

	return sb.String()
}

// renderSectionHeader renders a bracketed section header
// Format: ─────────────────────────────[ SECTION NAME ]─────────────────────────────
func renderSectionHeader(name string, width int, noColor bool) string {
	if width < 20 {
		width = 76
	}

	bracketedName := "[ " + name + " ]"
	nameLen := len(bracketedName)
	sideLen := (width - nameLen) / 2
	if sideLen < 0 {
		sideLen = 0
	}
	rightLen := width - sideLen - nameLen
	if rightLen < 0 {
		rightLen = 0
	}

	leftLine := strings.Repeat(styles.LineHorizontal, sideLen)
	rightLine := strings.Repeat(styles.LineHorizontal, rightLen)

	if noColor {
		return leftLine + bracketedName + rightLine
	}

	// Section name in cyan, lines in dim
	return dimStyle.Render(leftLine) + "[ " + sectionHeaderStyle.Render(name) + " ]" + dimStyle.Render(rightLine)
}

// renderHeroCost renders the total cost integrated into a section header
// Format: ─────────────────────[ $12.665834 TOTAL ]─────────────────────
func renderHeroCost(cost float64, width int, noColor bool) string {
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

	if noColor {
		return leftLine + bracketedCost + rightLine
	}

	// Cost with dimmed trailing decimals
	var costStyled string
	dotIdx := strings.Index(costFull, ".")
	if dotIdx != -1 && len(costFull) > dotIdx+3 {
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
func renderUnifiedCostRow(label string, cost float64, tokens int, labelColor lipgloss.Color, extra string, noColor bool) string {
	// Format label with optional color
	var labelStr string
	if !noColor && labelColor != "" {
		labelStyled := lipgloss.NewStyle().Foreground(labelColor)
		labelStr = labelStyled.Render(fmt.Sprintf("%-14s", label))
	} else {
		labelStr = fmt.Sprintf("%-14s", label)
	}

	// Format cost (11 chars width)
	costStr := formatCostStyled(cost, 11, noColor)

	// Format tokens
	tokenStr := fmt.Sprintf("%12s", formatNumber(tokens))

	// Add extra info (like TTL)
	extraStr := ""
	if extra != "" {
		if !noColor {
			extraStr = "  " + dimStyle.Render(extra)
		} else {
			extraStr = "  " + extra
		}
	}

	return fmt.Sprintf("  %s %s  %s tokens%s\n", labelStr, costStr, tokenStr, extraStr)
}

// renderUnifiedCostRowPlain renders a cost row in plain text mode
func renderUnifiedCostRowPlain(label string, cost float64, tokens int, extra string) string {
	extraStr := ""
	if extra != "" {
		extraStr = "  " + extra
	}
	return fmt.Sprintf("  %-14s %11s  %12s tokens%s\n", label, formatCost(cost), formatNumber(tokens), extraStr)
}

// renderContextSection renders the context window section
func renderContextSection(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	contextSize := analysis.LastMessageUsage.ContextWindowSize()
	if contextSize == 0 {
		return ""
	}

	modelPricing := pricing.GetModelPricing(analysis.LastMessageModel)
	maxContext := modelPricing.MaxContextTokens
	contextPct := pricing.GetContextPercentage(modelPricing, contextSize)
	freeSpace := pricing.GetFreeSpace(modelPricing, contextSize)
	buffer := pricing.GetAutocompactBuffer(modelPricing)
	freePct := float64(freeSpace) / float64(maxContext) * 100

	usageColor := styles.GetContextUsageColor(contextPct)

	// Context label with value
	contextVal := formatNumber(contextSize)
	contextMeta := fmt.Sprintf("(%.0f%% of %s)", contextPct, formatNumber(maxContext))

	if noColor {
		sb.WriteString(fmt.Sprintf("  Context  %s  %s %s\n", formatContextProgressBar(contextSize, freeSpace, buffer, maxContext, true), contextVal, contextMeta))
		sb.WriteString(fmt.Sprintf("           Free: %s (%.1f%%)  |  Buffer: %s\n", formatNumber(freeSpace), freePct, formatNumber(buffer)))
	} else {
		// Progress bar with context info
		coloredMeta := lipgloss.NewStyle().Foreground(usageColor).Render(contextMeta)
		sb.WriteString(fmt.Sprintf("  Context  %s  %s %s\n",
			formatContextProgressBar(contextSize, freeSpace, buffer, maxContext, false),
			contextVal, coloredMeta))

		// Free space and buffer info
		freeVal := formatNumber(freeSpace)
		bufferVal := formatNumber(buffer)
		freeValWithPct := fmt.Sprintf("%s (%.1f%%)", freeVal, freePct)
		freeStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(fmt.Sprintf("Free: %s", freeValWithPct))
		bufferStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(fmt.Sprintf("Buffer: %s", bufferVal))
		sb.WriteString(fmt.Sprintf("           %s  %s  %s\n", freeStyled, dimStyle.Render("│"), bufferStyled))
	}

	return sb.String()
}

// formatCostByModelContent renders cost by model rows (content only, no header)
func formatCostByModelContent(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	for modelID, cost := range analysis.CostByModel {
		modelName := pricing.GetModelDisplayName(modelID)
		if noColor {
			sb.WriteString(fmt.Sprintf("    %-12s %s\n", modelName, formatCost(cost.TotalCost)))
		} else {
			// Color by model tier
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-12s", modelName))
			sb.WriteString(fmt.Sprintf("    %s %s\n", modelStyled, formatCostStyled(cost.TotalCost, 12, noColor)))
		}
	}

	return sb.String()
}

// formatAgentBreakdownContent renders agent breakdown rows (content only, no header)
func formatAgentBreakdownContent(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	// Parent session cost
	if noColor {
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", "Parent session", formatCost(analysis.ParentCost.TotalCost)))
	} else {
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", "Parent session", formatCostStyled(analysis.ParentCost.TotalCost, 14, noColor)))
	}

	// Each agent with [AN] style markers
	for i, agent := range analysis.Agents {
		agentNum := i + 1
		shortID := agent.AgentID
		if len(shortID) > 7 {
			shortID = shortID[:7]
		}
		label := fmt.Sprintf("[A%d] (%s)", agentNum, shortID)
		msgs := fmt.Sprintf("%d msgs", agent.MessageCount)

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-20s %s   %s\n", label, formatCost(agent.TotalCost.TotalCost), msgs))
		} else {
			// Color agent marker
			agentColor := styles.GetAgentColor(fmt.Sprintf("%d", agentNum))
			labelStyled := lipgloss.NewStyle().Foreground(agentColor).Render(label)
			sb.WriteString(fmt.Sprintf("  %-20s %s   %s\n", labelStyled, formatCostStyled(agent.TotalCost.TotalCost, 14, noColor), dimStyle.Render(msgs)))
		}
	}

	// Agents subtotal
	if noColor {
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", "Agents subtotal", formatCost(analysis.AgentsCost.TotalCost)))
	} else {
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", dimStyle.Render("Agents subtotal"), formatCostStyled(analysis.AgentsCost.TotalCost, 14, noColor)))
	}

	return sb.String()
}

// formatInsightsSectionContent renders message insights rows (content only, no header)
func formatInsightsSectionContent(insights *models.MessageInsights, noColor bool) string {
	var sb strings.Builder

	// First message
	if insights.FirstMessage != nil {
		first := insights.FirstMessage
		componentLabel := formatCostComponentLabel(first.MainCostComponent)
		timestamp := dimStyle.Render(fmt.Sprintf("(%s)", first.Timestamp.Format("15:04:05")))
		componentInfo := dimStyle.Render(fmt.Sprintf("%s: %s", componentLabel, formatCost(first.MainCostValue)))

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s: %s\n",
				"First",
				formatCostStyled(first.Cost, 10, noColor),
				first.Timestamp.Format("15:04:05"),
				componentLabel,
				formatCost(first.MainCostValue)))
		} else {
			sb.WriteString(fmt.Sprintf("  %-10s %s  %s  %s\n",
				"First",
				formatCostStyled(first.Cost, 10, noColor),
				timestamp,
				componentInfo))
		}
	}

	// Last message
	if insights.LastMessage != nil {
		last := insights.LastMessage
		componentLabel := formatCostComponentLabel(last.MainCostComponent)
		timestamp := dimStyle.Render(fmt.Sprintf("(%s)", last.Timestamp.Format("15:04:05")))
		componentInfo := dimStyle.Render(fmt.Sprintf("%s: %s", componentLabel, formatCost(last.MainCostValue)))

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s: %s\n",
				"Last",
				formatCostStyled(last.Cost, 10, noColor),
				last.Timestamp.Format("15:04:05"),
				componentLabel,
				formatCost(last.MainCostValue)))
		} else {
			sb.WriteString(fmt.Sprintf("  %-10s %s  %s  %s\n",
				"Last",
				formatCostStyled(last.Cost, 10, noColor),
				timestamp,
				componentInfo))
		}
	}

	// Highest cost (only if notably above average)
	if insights.HighestCost != nil {
		highest := insights.HighestCost
		multiplier := insights.CostMultiplier()
		warningStr := fmt.Sprintf("%.1fx avg cost", multiplier)

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  ! %s\n",
				"Peak",
				formatCostStyled(highest.Cost, 10, noColor),
				highest.Timestamp.Format("15:04:05"),
				warningStr))
		} else {
			timestamp := dimStyle.Render(fmt.Sprintf("(%s)", highest.Timestamp.Format("15:04:05")))
			warningStyled := lipgloss.NewStyle().Foreground(styles.WarningColor).Render("⚠ " + warningStr)
			sb.WriteString(fmt.Sprintf("  %-10s %s  %s  %s\n",
				"Peak",
				formatCostStyled(highest.Cost, 10, noColor),
				timestamp,
				warningStyled))
		}
	}

	// Trend (only for sessions with 5+ messages)
	if insights.MessageCount >= 5 {
		trendDesc := insights.TrendDescription()
		trendSymbol := insights.CostTrend.Symbol()

		earlyStr := fmt.Sprintf("$%.2f/msg", insights.EarlyAvgCost)
		lateStr := fmt.Sprintf("$%.2f/msg", insights.LateAvgCost)

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s -> %s  %s %s\n",
				"Trend",
				earlyStr,
				lateStr,
				trendSymbol,
				trendDesc))
		} else {
			// Color the trend symbol based on direction
			var symbolStyled string
			switch insights.CostTrend {
			case models.TrendIncreasing:
				symbolStyled = lipgloss.NewStyle().Foreground(styles.WarningColor).Render(trendSymbol)
			case models.TrendDecreasing:
				symbolStyled = lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(trendSymbol)
			default:
				symbolStyled = dimStyle.Render(trendSymbol)
			}
			sb.WriteString(fmt.Sprintf("  %-10s %s → %s  %s %s\n",
				"Trend",
				earlyStr,
				lateStr,
				symbolStyled,
				trendDesc))
		}
	}

	return sb.String()
}

// formatCostStyledGreen returns a cost string with green styling for savings
func formatCostStyledGreen(cost float64, width int, noColor bool) string {
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

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
		return padding + savingsValueStyle.Render(full)
	}

	main := full[:dotIdx+3]
	extra := full[dotIdx+3:]

	return padding + savingsValueStyle.Render(main) + dimStyle.Render(extra)
}

// FormatSessionListTable formats a list of sessions as a table
func FormatSessionListTable(entries []models.SessionEntry, noColor bool) string {
	var sb strings.Builder
	const width = 76

	// Header panel
	sessionWord := "sessions"
	if len(entries) == 1 {
		sessionWord = "session"
	}
	headerText := fmt.Sprintf("%d %s", len(entries), sessionWord)

	if noColor {
		// Plain header panel
		sb.WriteString(styles.BoxTopLeft)
		sb.WriteString(strings.Repeat(styles.BoxHorizontal, width-2))
		sb.WriteString(styles.BoxTopRight)
		sb.WriteString("\n")

		sb.WriteString(styles.BoxVertical)
		sb.WriteString("  ")
		sb.WriteString(headerText)
		sb.WriteString(strings.Repeat(" ", width-6-len(headerText)))
		sb.WriteString("  ")
		sb.WriteString(styles.BoxVertical)
		sb.WriteString("\n")

		sb.WriteString(styles.BoxBottomLeft)
		sb.WriteString(strings.Repeat(styles.BoxHorizontal, width-2))
		sb.WriteString(styles.BoxBottomRight)
		sb.WriteString("\n\n")
	} else {
		// Styled header panel
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopRight))
		sb.WriteString("\n")

		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("  ")
		sb.WriteString(sectionHeaderStyle.Render(fmt.Sprintf("%d", len(entries))))
		sb.WriteString(fmt.Sprintf(" %s", sessionWord))
		sb.WriteString(strings.Repeat(" ", width-6-len(headerText)))
		sb.WriteString("  ")
		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("\n")

		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomRight))
		sb.WriteString("\n\n")
	}

	// Column headers
	colHeader := fmt.Sprintf("%-36s  %8s  %7s  %s", "Session ID", "Messages", "Agents", "Modified")
	if noColor {
		sb.WriteString(colHeader + "\n")
		sb.WriteString(strings.Repeat("-", width) + "\n")
	} else {
		sb.WriteString(dimStyle.Render(colHeader) + "\n")
		sb.WriteString(dimStyle.Render(strings.Repeat(styles.LineHorizontal, width)) + "\n")
	}

	// Session rows
	for _, entry := range entries {
		modified := entry.Modified.Format("2006-01-02 15:04")
		agentStr := "-"
		if entry.AgentCount > 0 {
			agentStr = fmt.Sprintf("%d (%d)", entry.AgentCount, entry.AgentMessageCount)
		}

		// Truncate session ID to fit (first 8 chars is usually enough to identify)
		shortID := entry.SessionID
		if len(shortID) > 36 {
			shortID = shortID[:33] + "..."
		}

		if noColor {
			sb.WriteString(fmt.Sprintf("%-36s  %8d  %7s  %s\n",
				shortID,
				entry.MessageCount,
				agentStr,
				modified))
		} else {
			// Dim the session ID, highlight message count if > 0
			var msgStr string
			if entry.MessageCount > 0 {
				msgStr = fmt.Sprintf("%8d", entry.MessageCount)
			} else {
				msgStr = dimStyle.Render(fmt.Sprintf("%8d", entry.MessageCount))
			}
			sb.WriteString(fmt.Sprintf("%s  %s  %7s  %s\n",
				dimStyle.Render(fmt.Sprintf("%-36s", shortID)),
				msgStr,
				agentStr,
				dimStyle.Render(modified)))
		}
	}

	// Footer separator
	sb.WriteString("\n")
	if noColor {
		sb.WriteString(strings.Repeat("-", width))
	} else {
		sb.WriteString(dimStyle.Render(strings.Repeat(styles.LineHorizontal, width)))
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
// Total width: 40 characters (matches TUI)
func formatContextProgressBar(contextSize, freeSpace, buffer, maxContext int, noColor bool) string {
	const barWidth = 40

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
