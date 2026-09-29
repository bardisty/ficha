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
	"github.com/mattn/go-runewidth"
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
	cumWidth := render.CostCellWidth(10, cumulatives...)
	analyses := make([]*models.SessionAnalysis, 0, len(sessionData))
	for _, sd := range sessionData {
		analyses = append(analyses, sd.analysis)
	}
	agentsWidth := agentsColumnWidth(analyses)

	// The expanded view prints agent costs under COST, so the column holds
	// them too, and MODEL widens when the deepest agent row would otherwise
	// run into it.
	modelWidth := 10
	costWidth := render.CostCellWidth(8, costs...)
	if expandAgents {
		for _, a := range analyses {
			if a == nil {
				continue
			}
			for _, agent := range a.Agents {
				costWidth = max(costWidth, render.CostCellWidth(0, agent.TotalCost.TotalCost))
				if agent.WorkflowID != "" {
					costWidth = max(costWidth, render.CostCellWidth(0, workflowCost(a.Agents, agent.WorkflowID)))
				}
			}
			modelWidth = max(modelWidth, agentTreeWidth(a)+2-expandedCostColumn+10)
		}
		// Workflow labels widen it too, as far as the 76-column report allows;
		// past that they're cut.
		maxModelWidth := 10 + 76 - (expandedCostColumn + costWidth + 2 + cumWidth)
		for _, a := range analyses {
			if a == nil {
				continue
			}
			for _, wf := range a.Workflows {
				label := treeIndent + treeStep + runewidth.StringWidth(render.WorkflowLabel(wf)) + 2
				modelWidth = max(modelWidth, min(label-expandedCostColumn+10, maxModelWidth))
			}
		}
	}

	var headerRow string
	if expandAgents {
		headerRow = fmt.Sprintf("  %4s %-8s  %-12s  %-*s  %*s  %*s", "#", "SESSION", "MODIFIED", modelWidth, "MODEL", costWidth, "COST", cumWidth, "CUMULATIVE")
	} else {
		// Default view: 7 columns, 73 chars - AGENTS before COST groups metadata together
		headerRow = fmt.Sprintf("  %4s %-8s  %-12s  %-10s %*s  %*s %*s", "#", "SESSION", "MODIFIED", "MODEL", agentsWidth, "AGENTS", costWidth, "COST", cumWidth, "CUMULATIVE")
	}
	contentWidth := max(72, len(headerRow)-2)

	// Render header row (bold white, all caps - matches breakdown command style)
	if noColor {
		sb.WriteString(headerRow + "\n")
		sb.WriteString("  " + strings.Repeat(styles.LineHorizontal, contentWidth) + "\n")
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
					sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-*s  %*s  %*s\n", num, shortID, modifiedStr, modelWidth, "-", costWidth, "(error)", cumWidth, "-"))
				} else {
					sb.WriteString(fmt.Sprintf("  %s %s  %s  %s  %s  %s\n",
						dimStyle.Render(fmt.Sprintf("%4d", num)),
						fmt.Sprintf("%-8s", shortID),
						dimStyle.Render(fmt.Sprintf("%-12s", modifiedStr)),
						dimStyle.Render(fmt.Sprintf("%-*s", modelWidth, "-")),
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
				sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-*s  %s  %s\n", num, shortID, modifiedStr, modelWidth, modelLabel, costStr, cumStr))
			} else {
				costColor := styles.GetCostGradientColor(sd.cost, minCost, maxCost)
				costStyled := render.CostColored(sd.cost, costColor, costWidth)
				cumStyled := render.CostColored(cumulativeSum, styles.SuccessColor, cumWidth)

				// Color model name by tier
				modelColor := styles.GetModelColor(modelName)
				modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-*s", modelWidth, modelLabel))

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
				sb.WriteString(renderAgentTreeRows(sd.analysis, noColor, expandedCostColumn+modelWidth-10, costWidth))
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
		sb.WriteString("  " + strings.Repeat(styles.LineHorizontal, contentWidth) + "\n")
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

// expandedCostColumn is where COST starts in the expanded view with the
// MODEL column at its base width of 10.
const expandedCostColumn = 2 + 4 + 1 + 8 + 2 + 12 + 2 + 10 + 2

// Agent tree rows: a 7-space indent puts the connectors under SESSION, and
// each level adds a 3-column connector ("├─ ") or rail ("│  ").
const (
	treeIndent = 7
	treeStep   = 3
)

// agentTreeWidth is how wide an analysis's agent rows get before their cost:
// connectors, marker, model and message count. Workflow agents sit a level
// deeper than the rest.
func agentTreeWidth(a *models.SessionAnalysis) int {
	depth := 1
	for _, agent := range a.Agents {
		if agent.WorkflowID != "" {
			depth = 2
		}
	}
	return treeIndent + depth*treeStep + 10 + 1 + 10 + 1 + agentMsgsWidth(a.Agents)
}

// renderAgentTreeRows renders a session's agents as a tree under its row,
// each cost in a costWidth cell starting at costCol, under the session's
// COST. A workflow run is a node carrying its subtotal, dimmed since the
// agents under it repeat it, with its agents one level deeper:
//
//	├─ [Aa1b2c3d] Haiku 4.5   12 msgs   $0.4200
//	└─ workflow: audit (completed)      $1.65
//	   ├─ [Aw1a2b3c] Opus 4.8 21 msgs   $1.10
//	   └─ [Aw6e5d4c] Sonnet 5  1 msg    $0.5500
func renderAgentTreeRows(analysis *models.SessionAnalysis, noColor bool, costCol, costWidth int) string {
	var sb strings.Builder
	agents := analysis.Agents
	msgsWidth := agentMsgsWidth(agents)
	indent := strings.Repeat(" ", treeIndent)

	// Top-level nodes: each regular agent, and each workflow run in the
	// order its first agent appears.
	type node struct {
		agent    *models.AgentAnalysis
		workflow string
		children []*models.AgentAnalysis
	}
	var nodes []*node
	runs := map[string]*node{}
	for i := range agents {
		a := &agents[i]
		if a.WorkflowID == "" {
			nodes = append(nodes, &node{agent: a})
			continue
		}
		n, ok := runs[a.WorkflowID]
		if !ok {
			n = &node{workflow: a.WorkflowID}
			runs[a.WorkflowID] = n
			nodes = append(nodes, n)
		}
		n.children = append(n.children, a)
	}

	connector := func(last bool) string {
		if last {
			return styles.TreeLast + " "
		}
		return styles.TreeBranch + " "
	}
	rail := func(last bool) string {
		if last {
			return strings.Repeat(" ", treeStep)
		}
		return styles.TreeRail + strings.Repeat(" ", treeStep-runewidth.StringWidth(styles.TreeRail))
	}
	// line pads left out to costCol and appends the cost cell.
	line := func(left string, cost float64, costColor lipgloss.Color) string {
		pad := strings.Repeat(" ", max(costCol-lipgloss.Width(left), 1))
		if noColor {
			return left + pad + render.CostCell(cost, costWidth) + "\n"
		}
		return left + pad + render.CostColored(cost, costColor, costWidth) + "\n"
	}
	dim := func(s string) string {
		if noColor {
			return s
		}
		return dimStyle.Render(s)
	}
	agentRow := func(prefix string, agent *models.AgentAnalysis) string {
		modelName := render.PrimaryModel(agent.CostByModel)
		marker := fmt.Sprintf("%-10s", "[A"+render.ShortAgentID(agent.AgentID)+"]")
		model := fmt.Sprintf("%-10s", render.ClampModel(modelName, 10))
		msgs := fmt.Sprintf("%*s", msgsWidth, agentMsgs(agent.MessageCount))
		if !noColor {
			marker = lipgloss.NewStyle().Foreground(styles.GetAgentColor(agent.AgentID)).Render(marker)
			model = lipgloss.NewStyle().Foreground(styles.GetModelColor(modelName)).Render(model)
			msgs = dimStyle.Render(msgs)
		}
		return line(prefix+marker+" "+model+" "+msgs, agent.TotalCost.TotalCost, styles.SecondaryColor)
	}

	for i, n := range nodes {
		last := i == len(nodes)-1
		prefix := indent + dim(connector(last))
		if n.agent != nil {
			sb.WriteString(agentRow(prefix, n.agent))
			continue
		}
		room := costCol - treeIndent - treeStep - 2
		label := truncateRight(render.WorkflowLabel(analysis.WorkflowByID(n.workflow)), room)
		sb.WriteString(line(prefix+dim(label), workflowCost(agents, n.workflow), styles.SecondaryColor))
		for j, child := range n.children {
			sb.WriteString(agentRow(indent+dim(rail(last)+connector(j == len(n.children)-1)), child))
		}
	}

	return sb.String()
}
