package formatter

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
)

// FormatSummaryTableWithDetails renders the summary table with per-session
// breakdown. results carries each session's pre-computed analysis (from
// AnalyzeMultipleSessions), so the formatter never re-parses. Color and glyph
// choices are driven by noColor.
func FormatSummaryTableWithDetails(analysis *models.SessionAnalysis, results []models.SessionResult, projectDir string, noColor bool, expandAgents bool) string {
	var sb strings.Builder
	const sectionWidth = 76

	// Header panel
	sb.WriteString(renderHeaderPanel(analysis, sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Hero total cost as section header
	sb.WriteString(renderHeroCost(analysis.TotalCost.TotalCost, sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Unified cost+token rows (same as FormatSessionTable)
	sb.WriteString(renderUnifiedCostRow("Input", analysis.TotalCost.InputCost, analysis.TotalUsage.InputTokens, lipgloss.Color(""), "", noColor))
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

	// Cache read
	if analysis.TotalCost.CacheReadCost > 0 || analysis.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(renderUnifiedCostRow("Cache read", analysis.TotalCost.CacheReadCost, analysis.TotalUsage.CacheReadInputTokens, styles.CacheReadTokenColor, "", noColor))
	}

	// Savings row
	if analysis.TotalCost.CacheSavings > 0 {
		sb.WriteString(renderSavingsRow(analysis.TotalCost.CacheSavings, noColor))
	}

	// Cost by model section
	sb.WriteString("\n")
	sb.WriteString(render.SectionHeader("COST BY MODEL", sectionWidth, noColor))
	sb.WriteString("\n\n")
	sb.WriteString(formatCostByModelContent(analysis, noColor))

	// Get session breakdown data (needed for both chart and table)
	breakdownResult := renderSessionBreakdown(results, noColor, expandAgents)

	// Session breakdown section (includes chart and table)
	sb.WriteString("\n")
	sb.WriteString(render.SectionHeader("SESSION BREAKDOWN", sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Cost chart at top of section (only show if we have 2+ data points)
	if len(breakdownResult.costs) > 1 {
		sb.WriteString(renderCostChart(breakdownResult.costs, breakdownResult.dates, sectionWidth, noColor))
		sb.WriteString("\n")
	}

	sb.WriteString(breakdownResult.table)

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

	footerText := fmt.Sprintf("Messages: %s  %s  Sessions: %d", msgStr, footerSep(noColor), analysis.SessionCount)
	if noColor {
		sb.WriteString(footerText)
	} else {
		sb.WriteString(footerStyle.Render(footerText))
	}
	sb.WriteString("\n")

	// Single-line separator
	sb.WriteString(renderFooterSingleRule(sectionWidth, noColor))
	sb.WriteString("\n")

	// Project path
	projectLine := fmt.Sprintf("Project: %s", projectDir)
	if noColor {
		sb.WriteString(projectLine)
	} else {
		sb.WriteString(dimStyle.Render(projectLine))
	}

	return sb.String()
}

// sessionBreakdownResult contains the rendered breakdown table plus data for the chart
type sessionBreakdownResult struct {
	table string
	costs []float64
	dates []time.Time
}

// renderSessionBreakdown renders the session breakdown table with cumulative column
// When expandAgents is false, shows AGENTS column with count + cost
// When expandAgents is true, shows agent sub-sessions as indented tree rows
//
// Each result carries its session's pre-computed analysis (nil when the session
// failed to parse, rendered as an "(error)" row); the formatter does no parsing.
func renderSessionBreakdown(results []models.SessionResult, noColor bool, expandAgents bool) sessionBreakdownResult {
	var sb strings.Builder

	// Sort by modified time
	sorted := make([]models.SessionResult, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Entry.Modified.Before(sorted[j].Entry.Modified)
	})

	// Pair each session with its analysis and collect costs
	type sessionWithAnalysis struct {
		entry    models.SessionEntry
		analysis *models.SessionAnalysis
		cost     float64
		err      bool
	}

	sessionData := make([]sessionWithAnalysis, 0, len(sorted))
	var costs []float64
	var dates []time.Time
	var totalCost float64

	for _, r := range sorted {
		if r.Analysis == nil {
			sessionData = append(sessionData, sessionWithAnalysis{entry: r.Entry, analysis: nil, cost: 0, err: true})
			continue
		}
		cost := r.Analysis.TotalCost.TotalCost
		sessionData = append(sessionData, sessionWithAnalysis{entry: r.Entry, analysis: r.Analysis, cost: cost, err: false})
		costs = append(costs, cost)
		dates = append(dates, r.Entry.Modified)
		totalCost += cost
	}

	// Calculate min/max for gradient
	var minCost, maxCost float64
	if len(costs) > 0 {
		minCost = costs[0]
		maxCost = costs[0]
		for _, c := range costs[1:] {
			if c < minCost {
				minCost = c
			}
			if c > maxCost {
				maxCost = c
			}
		}
	}

	// Table separator width matches section content width (76 - 2 padding on each side = 72)
	// This keeps separators aligned with section boundaries regardless of table column widths
	contentWidth := 72

	var headerRow string
	if expandAgents {
		// Expanded view: simple 5-column layout (72 chars max for RFC brutalist compliance)
		headerRow = fmt.Sprintf("  %4s %-8s  %-12s  %-10s  %8s  %10s", "#", "SESSION", "MODIFIED", "MODEL", "COST", "CUMULATIVE")
	} else {
		// Default view: 7 columns, 72 chars max - AGENTS before COST groups metadata together
		headerRow = fmt.Sprintf("  %4s %-8s  %-12s  %-9s %10s  %8s %10s", "#", "SESSION", "MODIFIED", "MODEL", "AGENTS", "COST", "CUMULATIVE")
	}

	// Render header row (bold white, all caps - matches breakdown command style)
	if noColor {
		sb.WriteString(headerRow + "\n")
		sb.WriteString("  " + strings.Repeat("-", contentWidth) + "\n")
	} else {
		sb.WriteString(headerStyle.Render(headerRow) + "\n")
		sb.WriteString("  " + dimStyle.Render(strings.Repeat(styles.LineHorizontal, contentWidth)) + "\n")
	}

	// Render session rows
	skipped := 0
	var cumulativeSum float64
	for i, sd := range sessionData {
		num := i + 1

		// Truncate session ID to first 8 chars
		shortID := sd.entry.SessionID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}

		// Format modified date with time (mainframe style: DD MMM HH:MM with uppercase month)
		modifiedStr := strings.ToUpper(sd.entry.Modified.Format("02 Jan 15:04"))

		if sd.err {
			skipped++
			if expandAgents {
				if noColor {
					sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-10s  %8s  %10s\n", num, shortID, modifiedStr, "-", "(error)", "-"))
				} else {
					sb.WriteString(fmt.Sprintf("  %s %s  %s  %s  %s  %s\n",
						dimStyle.Render(fmt.Sprintf("%4d", num)),
						fmt.Sprintf("%-8s", shortID),
						dimStyle.Render(fmt.Sprintf("%-12s", modifiedStr)),
						dimStyle.Render(fmt.Sprintf("%-10s", "-")),
						dimStyle.Render(fmt.Sprintf("%8s", "(error)")),
						dimStyle.Render(fmt.Sprintf("%10s", "-"))))
				}
			} else {
				if noColor {
					sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-9s %10s  %8s %10s\n", num, shortID, modifiedStr, "-", "-", "(error)", "-"))
				} else {
					sb.WriteString(fmt.Sprintf("  %s %s  %s  %s %s  %s %s\n",
						dimStyle.Render(fmt.Sprintf("%4d", num)),
						fmt.Sprintf("%-8s", shortID),
						dimStyle.Render(fmt.Sprintf("%-12s", modifiedStr)),
						dimStyle.Render(fmt.Sprintf("%-9s", "-")),
						dimStyle.Render(fmt.Sprintf("%10s", "-")),
						dimStyle.Render(fmt.Sprintf("%8s", "(error)")),
						dimStyle.Render(fmt.Sprintf("%10s", "-"))))
				}
			}
			continue
		}

		cumulativeSum += sd.cost

		// Format agents column (only used when not expanding)
		agentsStr := formatAgentsColumn(sd.analysis, noColor)

		// Get primary model for the PARENT session (excludes agent models)
		modelName := "-"
		if sd.analysis != nil {
			// Use ParentCostByModel to get parent-only model, fall back to CostByModel for backwards compatibility
			if len(sd.analysis.ParentCostByModel) > 0 {
				modelName = render.PrimaryModel(sd.analysis.ParentCostByModel)
			} else {
				modelName = render.PrimaryModel(sd.analysis.CostByModel)
			}
		}

		if expandAgents {
			// Expanded view: 5 columns, 2-decimal costs (RFC brutalist: under 72 chars)
			if noColor {
				costStr := fmt.Sprintf("$%.2f", sd.cost)
				cumStr := fmt.Sprintf("$%.2f", cumulativeSum)
				sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-10s  %8s  %10s\n", num, shortID, modifiedStr, modelName, costStr, cumStr))
			} else {
				costColor := styles.GetCostGradientColor(sd.cost, minCost, maxCost)
				costStr := fmt.Sprintf("$%.2f", sd.cost)
				costStyled := lipgloss.NewStyle().Foreground(costColor).Render(fmt.Sprintf("%8s", costStr))
				cumStr := fmt.Sprintf("$%.2f", cumulativeSum)
				cumStyled := lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(fmt.Sprintf("%10s", cumStr))

				// Color model name by tier
				modelColor := styles.GetModelColor(modelName)
				modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-10s", modelName))

				sb.WriteString(fmt.Sprintf("  %s %s  %s  %s  %s  %s\n",
					dimStyle.Render(fmt.Sprintf("%4d", num)),
					fmt.Sprintf("%-8s", shortID),
					dimStyle.Render(fmt.Sprintf("%-12s", modifiedStr)),
					modelStyled,
					costStyled,
					cumStyled))
			}

			// Render agent tree rows if this session has agents
			if sd.analysis != nil && sd.analysis.HasAgents && len(sd.analysis.Agents) > 0 {
				sb.WriteString(renderAgentTreeRows(sd.analysis, noColor))
			}
		} else {
			// Default view: AGENTS before COST (groups metadata, then costs)
			if noColor {
				costStr := fmt.Sprintf("$%.2f", sd.cost)
				cumStr := fmt.Sprintf("$%.2f", cumulativeSum)
				sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-9s %10s  %8s %10s\n", num, shortID, modifiedStr, modelName, agentsStr, costStr, cumStr))
			} else {
				costColor := styles.GetCostGradientColor(sd.cost, minCost, maxCost)
				costStr := fmt.Sprintf("$%.2f", sd.cost)
				costStyled := lipgloss.NewStyle().Foreground(costColor).Render(fmt.Sprintf("%8s", costStr))
				cumStr := fmt.Sprintf("$%.2f", cumulativeSum)
				cumStyled := lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(fmt.Sprintf("%10s", cumStr))

				// Color model name by tier (9 chars to fit 72-char width)
				modelColor := styles.GetModelColor(modelName)
				modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-9s", modelName))

				sb.WriteString(fmt.Sprintf("  %s %s  %s  %s %s  %s %s\n",
					dimStyle.Render(fmt.Sprintf("%4d", num)),
					fmt.Sprintf("%-8s", shortID),
					dimStyle.Render(fmt.Sprintf("%-12s", modifiedStr)),
					modelStyled,
					agentsStr,
					costStyled,
					cumStyled))
			}
		}
	}

	// Footer separator and sum
	validSessions := len(sorted) - skipped
	sumLabel := fmt.Sprintf("Sum (%d sessions):", validSessions)

	if noColor {
		sumCostStr := fmt.Sprintf("$%.2f", totalCost)
		sb.WriteString("  " + strings.Repeat("-", contentWidth) + "\n")
		if expandAgents {
			// Expanded: label spans left columns, sum right-aligned under CUMULATIVE
			sb.WriteString(fmt.Sprintf("  %-50s  %10s\n", sumLabel, sumCostStr))
		} else {
			// Default: label aligns under SESSION through AGENTS columns, cost under CUMULATIVE
			sb.WriteString(fmt.Sprintf("  %-52s  %10s\n", sumLabel, sumCostStr))
		}
	} else {
		sb.WriteString("  " + dimStyle.Render(strings.Repeat(styles.LineHorizontal, contentWidth)) + "\n")
		sumCostStr := fmt.Sprintf("$%.2f", totalCost)
		sumStyled := lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(fmt.Sprintf("%10s", sumCostStr))
		if expandAgents {
			sb.WriteString(fmt.Sprintf("  %-50s  %s\n", sumLabel, sumStyled))
		} else {
			sb.WriteString(fmt.Sprintf("  %-52s  %s\n", sumLabel, sumStyled))
		}
	}

	return sessionBreakdownResult{
		table: sb.String(),
		costs: costs,
		dates: dates,
	}
}

// formatAgentsColumn formats the AGENTS column value for a session
// Format: "N [$X.XX]" where N = agent count, $X.XX = agent subtotal (2 decimals)
// Returns "-" (dimmed) if no agents
// Column width: 10 chars
func formatAgentsColumn(analysis *models.SessionAnalysis, noColor bool) string {
	const colWidth = 10

	if analysis == nil || !analysis.HasAgents || analysis.AgentCount == 0 {
		if noColor {
			return fmt.Sprintf("%*s", colWidth, "-")
		}
		// For styled output, add padding BEFORE the styled content
		// (can't use %Ns because ANSI codes count as bytes)
		return strings.Repeat(" ", colWidth-1) + dimStyle.Render("-")
	}

	// Format: "N [$X.XX]" - right-aligned in colWidth chars
	agentCost := analysis.AgentsCost.TotalCost
	costStr := fmt.Sprintf("[$%.2f]", agentCost)
	fullStr := fmt.Sprintf("%d %s", analysis.AgentCount, costStr)

	if noColor {
		return fmt.Sprintf("%*s", colWidth, fullStr)
	}

	// Dim the bracketed cost (it's already included in COST column)
	// Calculate padding to right-align the display to colWidth chars
	displayWidth := len(fullStr)
	padding := colWidth - displayWidth
	if padding < 0 {
		padding = 0
	}
	countStr := fmt.Sprintf("%d ", analysis.AgentCount)
	return strings.Repeat(" ", padding) + countStr + dimStyle.Render(costStr)
}

// renderAgentTreeRows renders indented agent sub-session rows with tree connectors
// Tree connectors: ├─ for all but last, └─ for final agent
// Compact format: "    ├─ [A1] Haiku 4.5  45 msgs  $0.18"
// Follows RFC brutalist principle: remove the unnecessary (no agent ID - it's noise)
// Workflow agents get a dim group-header line before each run's first agent.
func renderAgentTreeRows(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder
	agents := analysis.Agents

	// Tree connector characters
	var branchChar, lastBranchChar string
	if noColor {
		branchChar = "+-"
		lastBranchChar = "`-"
	} else {
		branchChar = "├─"
		lastBranchChar = "└─"
	}

	prevWorkflow := ""
	for i, agent := range agents {
		if agent.WorkflowID != prevWorkflow {
			prevWorkflow = agent.WorkflowID
			if agent.WorkflowID != "" {
				label := render.WorkflowLabel(analysis.WorkflowByID(agent.WorkflowID))
				if noColor {
					sb.WriteString(fmt.Sprintf("       -- %s\n", label))
				} else {
					sb.WriteString("       " + dimStyle.Render("── "+label) + "\n")
				}
			}
		}
		agentNum := i + 1
		isLast := i == len(agents)-1

		// Tree connector
		var connector string
		if isLast {
			connector = lastBranchChar
		} else {
			connector = branchChar
		}

		// Get primary model for this agent
		modelName := render.PrimaryModel(agent.CostByModel)

		// Format message count
		msgStr := fmt.Sprintf("%d msgs", agent.MessageCount)
		if agent.MessageCount == 1 {
			msgStr = "1 msg"
		}

		agentLabel := fmt.Sprintf("[A%d]", agentNum)

		// Format: "       ├─ [A1] Haiku 4.5  45 msgs  $0.18"
		// 7-space indent aligns tree connector under SESSION column
		if noColor {
			costStr := fmt.Sprintf("$%.2f", agent.TotalCost.TotalCost)
			sb.WriteString(fmt.Sprintf("       %s %s %-10s %8s  %s\n",
				connector, agentLabel, modelName, msgStr, costStr))
		} else {
			// Color agent marker using GetAgentColor
			agentColor := styles.GetAgentColor(fmt.Sprintf("%d", agentNum))
			labelStyled := lipgloss.NewStyle().Foreground(agentColor).Render(agentLabel)

			// Color model name by tier
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-10s", modelName))

			// Dim the message count and cost
			msgStyled := dimStyle.Render(fmt.Sprintf("%8s", msgStr))
			costStyled := dimStyle.Render(fmt.Sprintf("$%.2f", agent.TotalCost.TotalCost))

			sb.WriteString(fmt.Sprintf("       %s %s %s %s  %s\n",
				dimStyle.Render(connector),
				labelStyled,
				modelStyled,
				msgStyled,
				costStyled))
		}
	}

	return sb.String()
}
