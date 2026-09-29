package formatter

import (
	"fmt"
	"strings"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// FormatSessionTable renders a session analysis as a table (single session or
// summary). Color and glyph choices are driven by noColor.
func FormatSessionTable(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder
	const sectionWidth = 76

	// Detect summary vs show mode
	isSummary := analysis.IsSummary

	// A session nobody has replied to yet has nothing to price, and a report
	// of empty sections reads like a broken tool or the wrong session.
	if isSummary && analysis.Window != nil && analysis.MessageCount == 0 {
		return emptyWindow(*analysis.Window)
	}
	if !isSummary && analysis.MessageCount == 0 {
		return fmt.Sprintf("No assistant messages in session %s yet.", render.TruncateID(analysis.SessionID, 8))
	}

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
	// A scope made only of synthetic lines has messages but no model with a
	// cost, and a heading over nothing reads as a rendering fault.
	if len(analysis.CostByModel) > 0 {
		sb.WriteString("\n")
		sb.WriteString(render.SectionHeader("COST BY MODEL", sectionWidth, noColor))
		sb.WriteString("\n\n")
		sb.WriteString(formatCostByModelContent(analysis, noColor))
	}

	// Agent breakdown (shown when agents exist). The summary aggregate sets
	// HasAgents/AgentsCost without collecting the per-agent records, and every
	// line of this section is per-agent, so require them (same guard as the
	// summary detail table).
	if analysis.HasAgents && len(analysis.Agents) > 0 {
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
		sb.WriteString(formatInsightsSectionContent(analysis.Insights, analysis.HasAgents, noColor))
	}

	// Footer with double-line separator
	sb.WriteString("\n")
	sb.WriteString(renderFooterDoubleRule(sectionWidth, noColor))
	sb.WriteString("\n")

	// Footer stats
	fields := []string{
		"Total: " + render.Cost(analysis.TotalCost.TotalCost),
		messagesField(analysis.MessageCount, analysis.ParentMessageCount, analysis.AgentMessageCount),
	}
	if isSummary {
		if sessionCount := analysis.SessionCount; sessionCount > 0 {
			fields = append(fields, fmt.Sprintf("Sessions: %d", sessionCount))
		}
	} else if !analysis.EndTime.IsZero() {
		// Show last active time for single sessions
		fields = append(fields, "Last active: "+render.DateTime(analysis.EndTime, now()))
	}
	sb.WriteString(footerStats(fields, sectionWidth, noColor))
	sb.WriteString("\n")

	// Single-line help separator
	sb.WriteString(renderFooterSingleRule(sectionWidth, noColor))

	return trimLineEnds(sb.String())
}

// renderContextSection renders the context window section: the gauge line,
// then its scope and headroom note under the bar.
func renderContextSection(analysis *models.SessionAnalysis, noColor bool) string {
	contextSize := analysis.LastMessageUsage.ContextWindowSize()
	if contextSize == 0 {
		return ""
	}
	maxContext := pricing.GetModelPricing(analysis.LastMessageModel).MaxContextTokens

	// Fill the 76-column section after the "  Context " label; the gauge
	// right-aligns its percentage, so "Context  95%" and "Context 100%" align.
	const gaugeWidth = 76 - 10
	line, note := render.ContextGauge(contextSize, maxContext, gaugeWidth, noColor, false)
	if !noColor {
		note = lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(note)
	}
	return "  Context " + line + "\n           " + note + "\n"
}

// agentMsgs formats an agent's message count ("45 msgs", "1 msg").
func agentMsgs(count int) string {
	if count == 1 {
		return "1 msg"
	}
	return fmt.Sprintf("%d msgs", count)
}

// agentMsgsWidth returns the msgs column width for a set of agent rows: at
// least 8 (the historical fixed width, fitting "999 msgs"), grown to the
// longest count so an oversized count widens the column for every row instead
// of pushing a single row's cost out of alignment.
func agentMsgsWidth(agents []models.AgentAnalysis) int {
	width := 8
	for _, a := range agents {
		if l := len(agentMsgs(a.MessageCount)); l > width {
			width = l
		}
	}
	return width
}

// formatAgentBreakdownContent renders agent breakdown rows (content only, no header)
// Layout: [AN] Model (ID) msgs cost
// All rows align costs at column 45 (2 indent + 43 content)
// Example:
//
//	Parent session                              $1.14
//	[Aa0b184d] Opus 4.5      14 msgs            $0.3588
//	Agents subtotal                             $5.11
func formatAgentBreakdownContent(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	// Parent session cost - right-aligned cost at column 45
	// Format: 2(indent) + 40(label) + 3(spaces) + cost = 45 chars before cost
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	if noColor {
		sb.WriteString(fmt.Sprintf("  %-40s   %s\n", "Parent session", render.CostCell(analysis.ParentCost.TotalCost, 11)))
	} else {
		paddedLabel := fmt.Sprintf("%-40s", "Parent session")
		sb.WriteString(fmt.Sprintf("  %s   %s\n", paddedLabel, formatCostStyled(analysis.ParentCost.TotalCost, 11, noColor)))
	}

	// Each agent with [A<id>] Model msgs cost format — the marker carries the
	// abbreviated real agent ID, matching the breakdown TUI's scheme.
	// Format: 2(indent) + 10(marker) + 1 + 11(model) + 1 + msgs + gap + cost
	// where msgs + gap = 20 (normally 8 + 12; the gap shrinks as the msgs
	// column grows to fit an oversized count), keeping cost at 45 chars
	// (aligned with parent and subtotal).
	// Workflow agents are grouped after regular agents; a dim header line marks
	// each run's start.
	msgsWidth := agentMsgsWidth(analysis.Agents)
	msgsGap := strings.Repeat(" ", max(1, 20-msgsWidth))
	prevWorkflow := ""
	for _, agent := range analysis.Agents {
		if agent.WorkflowID != prevWorkflow {
			prevWorkflow = agent.WorkflowID
			if agent.WorkflowID != "" {
				// The run's subtotal sits in the cost column, so a workflow
				// compares with the parent session at a glance.
				heading := truncateRight(styles.GroupRule+" "+render.WorkflowLabel(analysis.WorkflowByID(agent.WorkflowID)), 40)
				pad := strings.Repeat(" ", 40-runewidth.StringWidth(heading)+3)
				cost := workflowCost(analysis.Agents, agent.WorkflowID)
				if noColor {
					sb.WriteString("  " + heading + pad + render.CostCell(cost, 11) + "\n")
				} else {
					// Dim like its heading: the subtotal repeats the rows
					// below it and isn't part of the column's sum.
					sb.WriteString("  " + dimStyle.Render(heading) + pad + render.CostColored(cost, styles.SecondaryColor, 11) + "\n")
				}
			}
		}
		// [A<id>] carries the abbreviated real agent ID (%-10s fits [A1234567])
		marker := "[A" + render.ShortAgentID(agent.AgentID) + "]"

		// Get primary model for this agent
		modelName := render.PrimaryModel(agent.CostByModel)
		modelLabel := render.ClampModel(modelName, 11)

		// Format message count with singular/plural
		msgStr := agentMsgs(agent.MessageCount)

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %-11s %*s%s%s\n",
				marker, modelLabel, msgsWidth, msgStr, msgsGap, render.CostCell(agent.TotalCost.TotalCost, 11)))
		} else {
			// Color the marker by hashing the full agent ID (matches breakdown)
			agentColor := styles.GetAgentColor(agent.AgentID)
			markerStyled := lipgloss.NewStyle().Foreground(agentColor).Render(fmt.Sprintf("%-10s", marker))

			// Color model name by tier
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-11s", modelLabel))

			// Dim the message count
			msgStyled := dimStyle.Render(fmt.Sprintf("%*s", msgsWidth, msgStr))

			costStr := formatCostStyled(agent.TotalCost.TotalCost, 11, noColor)

			sb.WriteString(fmt.Sprintf("  %s %s %s%s%s\n",
				markerStyled, modelStyled, msgStyled, msgsGap, costStr))
		}
	}

	// Agents subtotal in bold green (matches TotalValueStyle for visual hierarchy)
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	if noColor {
		sb.WriteString(fmt.Sprintf("  %-40s   %s\n", "Agents subtotal", render.CostCell(analysis.AgentsCost.TotalCost, 11)))
	} else {
		paddedSubtotal := fmt.Sprintf("%-40s", "Agents subtotal")
		sb.WriteString(fmt.Sprintf("  %s   %s\n", paddedSubtotal, formatCostStyledBoldGreen(analysis.AgentsCost.TotalCost, 11, noColor)))
	}

	return sb.String()
}

// workflowCost sums the cost of a workflow run's agents.
func workflowCost(agents []models.AgentAnalysis, runID string) float64 {
	var sum float64
	for _, a := range agents {
		if a.WorkflowID == runID {
			sum += a.TotalCost.TotalCost
		}
	}
	return sum
}

// formatInsightsSectionContent renders message insights rows (content only, no header)
func formatInsightsSectionContent(insights *models.MessageInsights, hasAgents bool, noColor bool) string {
	var sb strings.Builder

	// When agents ran, these insights cover the main conversation alone (the
	// agent rows live in AGENT SUB-SESSIONS above and are excluded here). Label
	// the scope so it can't be silently mistaken for the breakdown view, which
	// computes Peak/trend over the merged parent+agent messages. No label when
	// there are no agents: parent-only and all-messages are then identical.
	if hasAgents {
		const scope = "main conversation only, agents excluded"
		if noColor {
			sb.WriteString("  " + scope + "\n")
		} else {
			sb.WriteString("  " + dimStyle.Render(scope) + "\n")
		}
	}

	// First message
	if insights.FirstMessage != nil {
		first := insights.FirstMessage
		componentLabel := render.CostComponentLabel(first.MainCostComponent)
		timestamp := dimStyle.Render(fmt.Sprintf("(%s)", render.Clock(first.Timestamp)))
		componentInfo := dimStyle.Render(fmt.Sprintf("%s: %s", componentLabel, render.Cost(first.MainCostValue)))

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s: %s\n",
				"First",
				formatCostStyled(first.Cost, 10, noColor),
				render.Clock(first.Timestamp),
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
		timestamp := dimStyle.Render(fmt.Sprintf("(%s)", render.Clock(last.Timestamp)))
		componentInfo := dimStyle.Render(fmt.Sprintf("%s: %s", componentLabel, render.Cost(last.MainCostValue)))

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s: %s\n",
				"Last",
				formatCostStyled(last.Cost, 10, noColor),
				render.Clock(last.Timestamp),
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
		// The multiplier is information, not a warning: nearly every session
		// has a message well above its average.
		multiplierStr := fmt.Sprintf("%.1fx avg cost", insights.CostMultiplier())

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s\n",
				"Peak",
				formatCostStyled(highest.Cost, 10, noColor),
				render.Clock(highest.Timestamp),
				multiplierStr))
		} else {
			timestamp := dimStyle.Render(fmt.Sprintf("(%s)", render.Clock(highest.Timestamp)))
			sb.WriteString(fmt.Sprintf("  %-10s %s  %s  %s\n",
				"Peak",
				formatCostStyled(highest.Cost, 10, noColor),
				timestamp,
				dimStyle.Render(multiplierStr)))
		}
	}

	// Trend (only once the analyzer actually computed one — see HasTrend)
	if insights.HasTrend() {
		trendDesc := insights.TrendDescription()
		trendSymbol := render.TrendSymbol(insights.CostTrend)

		comparison := fmt.Sprintf("last %d %s/msg vs %s/msg avg",
			insights.TrendWindow, render.Cost(insights.RecentAvgCost), render.Cost(insights.AverageCost))

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  %s %s\n",
				"Trend",
				comparison,
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
			sb.WriteString(fmt.Sprintf("  %-10s %s  %s %s\n",
				"Trend",
				comparison,
				symbolStyled,
				trendDesc))
		}
	}

	return sb.String()
}
