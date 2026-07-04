package formatter

import (
	"fmt"
	"strings"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
)

// FormatSessionTable renders a session analysis as a table (single session or
// summary). Color and glyph choices are driven by noColor.
func FormatSessionTable(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder
	const sectionWidth = 76

	// Detect summary vs show mode
	isSummary := analysis.IsSummary

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

	// Cache write rows
	cache5mTokens, cache1hTokens := render.CacheTokensByTTL(analysis.TotalUsage)
	has5mCost := analysis.TotalCost.CacheWrite5mCost > 0
	has1hCost := analysis.TotalCost.CacheWrite1hCost > 0

	if has5mCost {
		sb.WriteString(renderUnifiedCostRow("Cache write", analysis.TotalCost.CacheWrite5mCost, cache5mTokens, styles.CacheWriteTokenColor, "5m TTL", noColor))
	}

	if has1hCost {
		sb.WriteString(renderUnifiedCostRow("Cache write", analysis.TotalCost.CacheWrite1hCost, cache1hTokens, styles.CacheWriteTokenColor, "1h TTL", noColor))
	}

	// Cache read - only show if present
	if analysis.TotalCost.CacheReadCost > 0 || analysis.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(renderUnifiedCostRow("Cache read", analysis.TotalCost.CacheReadCost, analysis.TotalUsage.CacheReadInputTokens, styles.CacheReadTokenColor, "", noColor))
	}

	// Savings row
	if analysis.TotalCost.CacheSavings > 0 {
		sb.WriteString(renderSavingsRow(analysis.TotalCost.CacheSavings, noColor))
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
	sb.WriteString(render.SectionHeader("COST BY MODEL", sectionWidth, noColor))
	sb.WriteString("\n\n")
	sb.WriteString(formatCostByModelContent(analysis, noColor))

	// Agent breakdown (shown when agents exist)
	if analysis.HasAgents {
		sb.WriteString("\n")
		sb.WriteString(render.SectionHeader("AGENT SUB-SESSIONS", sectionWidth, noColor))
		sb.WriteString("\n\n")
		sb.WriteString(formatAgentBreakdownContent(analysis, noColor))
	}

	// Message insights (shown when insights are available)
	if analysis.Insights != nil {
		sb.WriteString("\n")
		sb.WriteString(render.SectionHeader("MESSAGE INSIGHTS", sectionWidth, noColor))
		sb.WriteString("\n\n")
		sb.WriteString(formatInsightsSectionContent(analysis.Insights, noColor))
	}

	// Footer with double-line separator
	sb.WriteString("\n")
	sb.WriteString(renderFooterDoubleRule(sectionWidth, noColor))
	sb.WriteString("\n")

	// Footer stats
	msgStr := fmt.Sprintf("%d", analysis.MessageCount)
	if analysis.AgentMessageCount > 0 {
		msgStr = fmt.Sprintf("%d (%d parent, %d agents)",
			analysis.MessageCount, analysis.ParentMessageCount, analysis.AgentMessageCount)
	}

	footerText := fmt.Sprintf("Messages: %s", msgStr)
	sep := footerSep(noColor)
	if isSummary {
		if sessionCount := analysis.SessionCount; sessionCount > 0 {
			footerText += fmt.Sprintf("  %s  Sessions: %d", sep, sessionCount)
		}
	} else if !analysis.EndTime.IsZero() {
		// Show last active time for single sessions
		footerText += fmt.Sprintf("  %s  Last active: %s", sep, analysis.EndTime.Local().Format("2006-01-02 15:04"))
	}

	if noColor {
		sb.WriteString(footerText)
	} else {
		sb.WriteString(footerStyle.Render(footerText))
	}
	sb.WriteString("\n")

	// Single-line help separator
	sb.WriteString(renderFooterSingleRule(sectionWidth, noColor))

	return sb.String()
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
	freePct := float64(freeSpace) / float64(maxContext) * 100

	usageColor := styles.GetContextUsageColor(contextPct)

	// Context label with value
	contextVal := render.Number(contextSize)
	contextMeta := fmt.Sprintf("(%.0f%% of %s)", contextPct, render.Number(int64(maxContext)))

	if noColor {
		sb.WriteString(fmt.Sprintf("  Context  %s  %s %s\n", render.ContextBar(contextSize, freeSpace, maxContext, true), contextVal, contextMeta))
		sb.WriteString(fmt.Sprintf("           Free: %s (%.1f%%)\n", render.Number(freeSpace), freePct))
	} else {
		// Progress bar with context info
		coloredMeta := lipgloss.NewStyle().Foreground(usageColor).Render(contextMeta)
		sb.WriteString(fmt.Sprintf("  Context  %s  %s %s\n",
			render.ContextBar(contextSize, freeSpace, maxContext, false),
			contextVal, coloredMeta))

		// Free space info
		freeVal := render.Number(freeSpace)
		freeValWithPct := fmt.Sprintf("%s (%.1f%%)", freeVal, freePct)
		freeStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(fmt.Sprintf("Free: %s", freeValWithPct))
		sb.WriteString(fmt.Sprintf("           %s\n", freeStyled))
	}

	return sb.String()
}

// formatAgentBreakdownContent renders agent breakdown rows (content only, no header)
// Layout: [AN] Model (ID) msgs cost
// All rows align costs at column 45 (2 indent + 43 content)
// Example:
//
//	Parent session                            $1.135371
//	[A1] Opus 4.5    (a0b184d)     14 msgs    $1.358774
//	Agents subtotal                           $5.112218
func formatAgentBreakdownContent(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	// Parent session cost - right-aligned cost at column 45
	// Format: 2(indent) + 40(label) + 3(spaces) + cost = 45 chars before cost
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	if noColor {
		sb.WriteString(fmt.Sprintf("  %-40s   %s\n", "Parent session", render.Cost(analysis.ParentCost.TotalCost)))
	} else {
		paddedLabel := fmt.Sprintf("%-40s", "Parent session")
		sb.WriteString(fmt.Sprintf("  %s   %s\n", paddedLabel, formatCostStyled(analysis.ParentCost.TotalCost, 11, noColor)))
	}

	// Each agent with [AN] Model (ID) msgs cost format
	// Format: 2(indent) + 5(marker) + 1 + 11(model) + 1 + 10(id) + 1 + 8(msgs) + 6(spaces) + cost
	//       = 2 + 37 + 6 = 45 chars before cost (aligned with parent)
	for i, agent := range analysis.Agents {
		agentNum := i + 1
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

		if noColor {
			marker := fmt.Sprintf("[A%d]", agentNum)
			idStr := fmt.Sprintf("(%s)", shortID)
			sb.WriteString(fmt.Sprintf("  %-5s %-11s %-10s %8s      %s\n",
				marker, modelName, idStr, msgStr, render.Cost(agent.TotalCost.TotalCost)))
		} else {
			// Color agent marker (use %-5s to handle [A10] etc)
			agentColor := styles.GetAgentColor(fmt.Sprintf("%d", agentNum))
			markerStyled := lipgloss.NewStyle().Foreground(agentColor).Render(fmt.Sprintf("%-5s", fmt.Sprintf("[A%d]", agentNum)))

			// Color model name by tier
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-11s", modelName))

			// Dim the ID and message count
			idStyled := dimStyle.Render(fmt.Sprintf("%-10s", fmt.Sprintf("(%s)", shortID)))
			msgStyled := dimStyle.Render(fmt.Sprintf("%8s", msgStr))

			costStr := formatCostStyled(agent.TotalCost.TotalCost, 11, noColor)

			sb.WriteString(fmt.Sprintf("  %s %s %s %s      %s\n",
				markerStyled, modelStyled, idStyled, msgStyled, costStr))
		}
	}

	// Agents subtotal in bold green (matches TotalValueStyle for visual hierarchy)
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	if noColor {
		sb.WriteString(fmt.Sprintf("  %-40s   %s\n", "Agents subtotal", render.Cost(analysis.AgentsCost.TotalCost)))
	} else {
		paddedSubtotal := fmt.Sprintf("%-40s", "Agents subtotal")
		sb.WriteString(fmt.Sprintf("  %s   %s\n", paddedSubtotal, formatCostStyledBoldGreen(analysis.AgentsCost.TotalCost, 11, noColor)))
	}

	return sb.String()
}

// formatInsightsSectionContent renders message insights rows (content only, no header)
func formatInsightsSectionContent(insights *models.MessageInsights, noColor bool) string {
	var sb strings.Builder

	// First message
	if insights.FirstMessage != nil {
		first := insights.FirstMessage
		componentLabel := render.CostComponentLabel(first.MainCostComponent)
		timestamp := dimStyle.Render(fmt.Sprintf("(%s)", first.Timestamp.Format("15:04:05")))
		componentInfo := dimStyle.Render(fmt.Sprintf("%s: %s", componentLabel, render.Cost(first.MainCostValue)))

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s: %s\n",
				"First",
				formatCostStyled(first.Cost, 10, noColor),
				first.Timestamp.Format("15:04:05"),
				componentLabel,
				render.Cost(first.MainCostValue)))
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
		componentLabel := render.CostComponentLabel(last.MainCostComponent)
		timestamp := dimStyle.Render(fmt.Sprintf("(%s)", last.Timestamp.Format("15:04:05")))
		componentInfo := dimStyle.Render(fmt.Sprintf("%s: %s", componentLabel, render.Cost(last.MainCostValue)))

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s: %s\n",
				"Last",
				formatCostStyled(last.Cost, 10, noColor),
				last.Timestamp.Format("15:04:05"),
				componentLabel,
				render.Cost(last.MainCostValue)))
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
