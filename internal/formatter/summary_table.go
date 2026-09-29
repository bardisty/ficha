package formatter

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// FormatSummaryTableWithDetails renders the summary table with per-session
// breakdown. results carries each session's pre-computed analysis (from
// AnalyzeMultipleSessions), so the formatter never re-parses. storageDir is
// the Claude project directory, printed under the footer when it's set
// (with -v). Color and glyph choices are driven by noColor.
func FormatSummaryTableWithDetails(analysis *models.SessionAnalysis, results []models.SessionResult, storageDir string, noColor bool, expandAgents bool) string {
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

	// Cost chart at the top of the section, for sessions that cost anything
	if chart := renderCostChart(breakdownResult.costs, noColor); chart != "" {
		sb.WriteString(chart)
		sb.WriteString("\n")
	}

	sb.WriteString(breakdownResult.table)

	// Footer with double-line separator
	sb.WriteString("\n")
	sb.WriteString(renderFooterDoubleRule(sectionWidth, noColor))
	sb.WriteString("\n")

	// Footer stats
	sb.WriteString(footerStats([]string{
		"Total: " + render.Cost(analysis.TotalCost.TotalCost),
		messagesField(analysis.MessageCount, analysis.ParentMessageCount, analysis.AgentMessageCount),
		fmt.Sprintf("Sessions: %d", analysis.SessionCount),
	}, sectionWidth, noColor))
	sb.WriteString("\n")

	// Single-line separator
	sb.WriteString(renderFooterSingleRule(sectionWidth, noColor))

	// The storage directory, for debugging: the header names the project.
	if storageDir != "" {
		line := "Storage: " + storageDir
		if !noColor {
			line = dimStyle.Render(line)
		}
		sb.WriteString("\n" + line)
	}

	return trimLineEnds(sb.String())
}

// sessionBreakdownResult is the rendered breakdown table plus the chart's
// points: the priced sessions' costs, oldest first.
type sessionBreakdownResult struct {
	table string
	costs []float64
}

// renderSessionBreakdown renders one row per session, newest first like
// list. With expandAgents each session's agents follow as a tree, costs
// under COST; without, AGENTS is a count, since COST already includes them.
//
// Each result carries its session's pre-computed analysis (nil when the session
// failed to parse, rendered as an "(error)" row); the formatter does no parsing.
func renderSessionBreakdown(results []models.SessionResult, noColor bool, expandAgents bool) sessionBreakdownResult {
	var sb strings.Builder

	// Oldest first, stable, as the machine detail formats order their rows
	// (sortedSuccessfulResults); the table then reads it backwards.
	sorted := make([]models.SessionResult, len(results))
	copy(sorted, results)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Entry.Modified.Before(sorted[j].Entry.Modified)
	})

	var chartCosts, costs []float64
	var totalCost float64
	valid := 0
	modifiedWidth := len("MODIFIED")
	for _, r := range sorted {
		modifiedWidth = max(modifiedWidth, len(render.DateTime(r.Entry.Modified, now())))
		if r.Analysis == nil {
			continue
		}
		c := r.Analysis.TotalCost.TotalCost
		valid++
		totalCost += c
		costs = append(costs, c)
		if c > 0 {
			chartCosts = append(chartCosts, c)
		}
	}
	var minCost, maxCost float64
	for i, c := range costs {
		if i == 0 || c < minCost {
			minCost = c
		}
		maxCost = max(maxCost, c)
	}

	// COST holds the sum too, and in the expanded view every agent and run
	// subtotal, so a large value widens the column for every row.
	costWidth := render.CostCellWidth(8, append(costs, totalCost)...)
	modelWidth := 10
	costCol := func() int { return 2 + 4 + 1 + 8 + 2 + modifiedWidth + 2 + modelWidth + 2 }
	if expandAgents {
		for _, r := range sorted {
			if r.Analysis == nil {
				continue
			}
			for _, agent := range r.Analysis.Agents {
				costWidth = max(costWidth, render.CostCellWidth(0, agent.TotalCost.TotalCost))
				if agent.WorkflowID != "" {
					costWidth = max(costWidth, render.CostCellWidth(0, workflowCost(r.Analysis.Agents, agent.WorkflowID)))
				}
			}
		}
		// MODEL widens when the deepest agent row or a workflow label would
		// run into COST, as far as the 76-column report allows; past that,
		// labels are cut.
		maxModelWidth := modelWidth + 76 - (costCol() + costWidth)
		for _, r := range sorted {
			if r.Analysis == nil {
				continue
			}
			modelWidth = max(modelWidth, agentTreeWidth(r.Analysis)+2-costCol()+modelWidth)
			for _, wf := range r.Analysis.Workflows {
				label := treeIndent + treeStep + runewidth.StringWidth(render.WorkflowLabel(wf)) + 2
				modelWidth = max(modelWidth, min(label-costCol()+modelWidth, maxModelWidth))
			}
		}
	}

	row := func(num, id, modified, model, agents, cost string) string {
		line := fmt.Sprintf("  %s %s  %s  %s", num, id, modified, model)
		if !expandAgents {
			line += "  " + agents
		}
		return line + "  " + cost
	}
	pad := func(s string, w int) string { return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0)) }
	headerRow := row(
		fmt.Sprintf("%4s", "#"),
		fmt.Sprintf("%-8s", "SESSION"),
		fmt.Sprintf("%-*s", modifiedWidth, "MODIFIED"),
		fmt.Sprintf("%-*s", modelWidth, "MODEL"),
		fmt.Sprintf("%*s", len("AGENTS"), "AGENTS"),
		fmt.Sprintf("%*s", costWidth, "COST"),
	)
	contentWidth := max(72, lipgloss.Width(headerRow)-2)
	rule := "  " + strings.Repeat(styles.LineHorizontal, contentWidth)
	if noColor {
		sb.WriteString(headerRow + "\n" + rule + "\n")
	} else {
		sb.WriteString(headerStyle.Render(headerRow) + "\n" + "  " + dimStyle.Render(strings.Repeat(styles.LineHorizontal, contentWidth)) + "\n")
	}

	dim := func(s string) string {
		if noColor {
			return s
		}
		return dimStyle.Render(s)
	}
	for i := len(sorted) - 1; i >= 0; i-- {
		r := sorted[i]
		num := dim(fmt.Sprintf("%4d", len(sorted)-i))
		id := fmt.Sprintf("%-8s", render.TruncateID(r.Entry.SessionID, 8))
		modified := dim(pad(render.DateTime(r.Entry.Modified, now()), modifiedWidth))

		if r.Analysis == nil {
			sb.WriteString(row(num, id, modified,
				dim(pad("-", modelWidth)),
				dim(fmt.Sprintf("%*s", len("AGENTS"), "-")),
				dim(fmt.Sprintf("%*s", costWidth, "(error)"))) + "\n")
			continue
		}
		a := r.Analysis
		cost := a.TotalCost.TotalCost

		// The parent's model: agents' models show in their own rows.
		modelName := parentPrimaryModel(a)
		model := pad(render.ClampModel(modelName, modelWidth), modelWidth)
		agents := fmt.Sprintf("%*s", len("AGENTS"), "-")
		if a.AgentCount > 0 {
			agents = fmt.Sprintf("%*d", len("AGENTS"), a.AgentCount)
		}
		costStr := render.CostCell(cost, costWidth)
		if !noColor {
			model = lipgloss.NewStyle().Foreground(styles.GetModelColor(modelName)).Render(model)
			agents = dimStyle.Render(agents)
			costStr = render.CostColored(cost, styles.GetCostGradientColor(cost, minCost, maxCost), costWidth)
		}
		sb.WriteString(row(num, id, modified, model, agents, costStr) + "\n")

		if expandAgents && a.HasAgents && len(a.Agents) > 0 {
			sb.WriteString(renderAgentTreeRows(a, noColor, costCol(), costWidth))
		}
	}

	// The sum sits under COST.
	sumLabel := fmt.Sprintf("Sum (%d %s):", valid, sessionsWord(valid))
	sumLabelWidth := lipgloss.Width(headerRow) - 2 - 2 - costWidth
	sumCost := render.CostCell(totalCost, costWidth)
	if !noColor {
		sumCost = render.CostColored(totalCost, styles.SuccessColor, costWidth)
	}
	if noColor {
		sb.WriteString(rule + "\n")
	} else {
		sb.WriteString("  " + dimStyle.Render(strings.Repeat(styles.LineHorizontal, contentWidth)) + "\n")
	}
	sb.WriteString(fmt.Sprintf("  %-*s  %s\n", sumLabelWidth, sumLabel, sumCost))

	return sessionBreakdownResult{table: sb.String(), costs: chartCosts}
}

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
