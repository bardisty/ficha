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

	sb.WriteString(footerStats([]string{
		"Total: " + render.Cost(analysis.TotalCost.TotalCost),
		"Messages: " + msgStr,
		fmt.Sprintf("Sessions: %d", analysis.SessionCount),
	}, sectionWidth, noColor))
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

	// Sort by modified time. Stable, matching sortedSuccessfulResults: the
	// machine detail formats order rows with the same comparator, and the
	// CUMULATIVE column accumulates over row order, so an unstable sort could
	// let tie-mtime sessions order (and accumulate) differently across surfaces.
	sorted := make([]models.SessionResult, len(results))
	copy(sorted, results)
	sort.SliceStable(sorted, func(i, j int) bool {
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
	// Cost columns grow to fit their widest value (the sum included, since it
	// prints under CUMULATIVE), so a large total widens the column for every
	// row instead of pushing one row out of line.
	cumulatives := make([]float64, 0, len(costs)+1)
	var running float64
	for _, c := range costs {
		running += c
		cumulatives = append(cumulatives, running)
	}
	cumulatives = append(cumulatives, totalCost)
	costWidth := render.CostCellWidth(8, costs...)
	cumWidth := render.CostCellWidth(10, cumulatives...)
	analyses := make([]*models.SessionAnalysis, 0, len(sessionData))
	for _, sd := range sessionData {
		analyses = append(analyses, sd.analysis)
	}
	agentsWidth := agentsColumnWidth(analyses)

	var headerRow string
	if expandAgents {
		// Expanded view: simple 5-column layout (72 chars max for RFC brutalist compliance)
		headerRow = fmt.Sprintf("  %4s %-8s  %-12s  %-10s  %*s  %*s", "#", "SESSION", "MODIFIED", "MODEL", costWidth, "COST", cumWidth, "CUMULATIVE")
	} else {
		// Default view: 7 columns, 73 chars - AGENTS before COST groups metadata together
		headerRow = fmt.Sprintf("  %4s %-8s  %-12s  %-10s %*s  %*s %*s", "#", "SESSION", "MODIFIED", "MODEL", agentsWidth, "AGENTS", costWidth, "COST", cumWidth, "CUMULATIVE")
	}
	contentWidth := max(72, len(headerRow)-2)

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
					sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-10s  %*s  %*s\n", num, shortID, modifiedStr, "-", costWidth, "(error)", cumWidth, "-"))
				} else {
					sb.WriteString(fmt.Sprintf("  %s %s  %s  %s  %s  %s\n",
						dimStyle.Render(fmt.Sprintf("%4d", num)),
						fmt.Sprintf("%-8s", shortID),
						dimStyle.Render(fmt.Sprintf("%-12s", modifiedStr)),
						dimStyle.Render(fmt.Sprintf("%-10s", "-")),
						dimStyle.Render(fmt.Sprintf("%*s", costWidth, "(error)")),
						dimStyle.Render(fmt.Sprintf("%*s", cumWidth, "-"))))
				}
			} else {
				if noColor {
					sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-10s %*s  %*s %*s\n", num, shortID, modifiedStr, "-", agentsWidth, "-", costWidth, "(error)", cumWidth, "-"))
				} else {
					sb.WriteString(fmt.Sprintf("  %s %s  %s  %s %s  %s %s\n",
						dimStyle.Render(fmt.Sprintf("%4d", num)),
						fmt.Sprintf("%-8s", shortID),
						dimStyle.Render(fmt.Sprintf("%-12s", modifiedStr)),
						dimStyle.Render(fmt.Sprintf("%-10s", "-")),
						dimStyle.Render(fmt.Sprintf("%*s", agentsWidth, "-")),
						dimStyle.Render(fmt.Sprintf("%*s", costWidth, "(error)")),
						dimStyle.Render(fmt.Sprintf("%*s", cumWidth, "-"))))
				}
			}
			continue
		}

		cumulativeSum += sd.cost

		// Format agents column (only used when not expanding)
		agentsStr := formatAgentsColumn(sd.analysis, agentsWidth, noColor)

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
		// Colors match on the full name ("Sonnet", "opus"); only the printed
		// label is clamped to the column.
		modelLabel := render.ClampModel(modelName, 10)

		if expandAgents {
			// Expanded view: 5 columns (RFC brutalist: under 72 chars)
			if noColor {
				costStr := render.CostCell(sd.cost, costWidth)
				cumStr := render.CostCell(cumulativeSum, cumWidth)
				sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-10s  %s  %s\n", num, shortID, modifiedStr, modelLabel, costStr, cumStr))
			} else {
				costColor := styles.GetCostGradientColor(sd.cost, minCost, maxCost)
				costStyled := render.CostColored(sd.cost, costColor, costWidth)
				cumStyled := render.CostColored(cumulativeSum, styles.SuccessColor, cumWidth)

				// Color model name by tier
				modelColor := styles.GetModelColor(modelName)
				modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-10s", modelLabel))

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
				costStr := render.CostCell(sd.cost, costWidth)
				cumStr := render.CostCell(cumulativeSum, cumWidth)
				sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-10s %s  %s %s\n", num, shortID, modifiedStr, modelLabel, agentsStr, costStr, cumStr))
			} else {
				costColor := styles.GetCostGradientColor(sd.cost, minCost, maxCost)
				costStyled := render.CostColored(sd.cost, costColor, costWidth)
				cumStyled := render.CostColored(cumulativeSum, styles.SuccessColor, cumWidth)

				// Color model name by tier (10 chars: the longest display name,
				// "Sonnet 4.6"; the row still fits the 72-char separator)
				modelColor := styles.GetModelColor(modelName)
				modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-10s", modelLabel))

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
	sumLabel := fmt.Sprintf("Sum (%d %s):", validSessions, sessionsWord(validSessions))

	// The sum right-aligns exactly under CUMULATIVE: the label spans the header
	// width minus the 2-space indent, the 2-space gap before the cost, and the
	// CUMULATIVE field. Derived from headerRow (plain ASCII in both
	// views) so the two lines cannot drift apart.
	sumLabelWidth := len(headerRow) - 2 - 2 - cumWidth

	if noColor {
		sb.WriteString("  " + strings.Repeat("-", contentWidth) + "\n")
		sb.WriteString(fmt.Sprintf("  %-*s  %s\n", sumLabelWidth, sumLabel, render.CostCell(totalCost, cumWidth)))
	} else {
		sb.WriteString("  " + dimStyle.Render(strings.Repeat(styles.LineHorizontal, contentWidth)) + "\n")
		sumStyled := render.CostColored(totalCost, styles.SuccessColor, cumWidth)
		sb.WriteString(fmt.Sprintf("  %-*s  %s\n", sumLabelWidth, sumLabel, sumStyled))
	}

	return sessionBreakdownResult{
		table: sb.String(),
		costs: costs,
		dates: dates,
	}
}

// agentsCell is the plain AGENTS value for a session, "N [$X]" with the
// subtotal at human precision, or "-" without agents. A two-decimal subtotal
// gets the same two-space pad a CostCell does, after the bracket, so the
// decimal points line up down the column.
func agentsCell(analysis *models.SessionAnalysis) string {
	if analysis == nil || !analysis.HasAgents || analysis.AgentCount == 0 {
		return "-"
	}
	cell := render.CostCell(analysis.AgentsCost.TotalCost, 0)
	value := strings.TrimRight(cell, " ")
	return fmt.Sprintf("%d [%s]%s", analysis.AgentCount, value, cell[len(value):])
}

// agentsColumnWidth is the AGENTS column width: at least the header's 10,
// grown to the widest cell so a large count or subtotal widens the column
// instead of shifting its row.
func agentsColumnWidth(analyses []*models.SessionAnalysis) int {
	width := 10
	for _, a := range analyses {
		width = max(width, len(agentsCell(a)))
	}
	return width
}

// formatAgentsColumn right-aligns agentsCell in width columns, dimming the
// bracketed subtotal (COST already includes it) and a lone "-".
func formatAgentsColumn(analysis *models.SessionAnalysis, width int, noColor bool) string {
	cell := agentsCell(analysis)
	padding := strings.Repeat(" ", max(0, width-len(cell)))
	if noColor {
		return padding + cell
	}
	if idx := strings.Index(cell, "["); idx >= 0 {
		return padding + cell[:idx] + dimStyle.Render(cell[idx:])
	}
	return padding + dimStyle.Render(cell)
}

// renderAgentTreeRows renders indented agent sub-session rows with tree connectors
// Tree connectors: ├─ for all but last, └─ for final agent
// Compact format: "    ├─ [Aa1b2c3d] Haiku 4.5  45 msgs  $0.1800"
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

	// Shared msgs column width: grows past 8 only when a count overflows, so
	// every sibling row's trailing cost stays mutually aligned.
	msgsWidth := agentMsgsWidth(agents)
	agentCosts := make([]float64, len(agents))
	for i, a := range agents {
		agentCosts[i] = a.TotalCost.TotalCost
	}
	costWidth := render.CostCellWidth(0, agentCosts...)

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
		modelLabel := render.ClampModel(modelName, 10)

		// Format message count
		msgStr := agentMsgs(agent.MessageCount)

		// [A<id>] carries the abbreviated real agent ID (%-10s fits [A1234567]),
		// matching the breakdown TUI's scheme
		agentLabel := "[A" + render.ShortAgentID(agent.AgentID) + "]"

		// Format: "       ├─ [Aa1b2c3d] Haiku 4.5  45 msgs  $0.1800"
		// 7-space indent aligns tree connector under SESSION column
		if noColor {
			costStr := render.CostCell(agent.TotalCost.TotalCost, costWidth)
			sb.WriteString(fmt.Sprintf("       %s %-10s %-10s %*s  %s\n",
				connector, agentLabel, modelLabel, msgsWidth, msgStr, costStr))
		} else {
			// Color the marker by hashing the full agent ID (matches breakdown)
			agentColor := styles.GetAgentColor(agent.AgentID)
			labelStyled := lipgloss.NewStyle().Foreground(agentColor).Render(fmt.Sprintf("%-10s", agentLabel))

			// Color model name by tier
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-10s", modelLabel))

			// Dim the message count and cost
			msgStyled := dimStyle.Render(fmt.Sprintf("%*s", msgsWidth, msgStr))
			costStyled := render.CostColored(agent.TotalCost.TotalCost, styles.SecondaryColor, costWidth)

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
