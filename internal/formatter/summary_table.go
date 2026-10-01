package formatter

import (
	"fmt"
	"image/color"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/mattn/go-runewidth"
)

// FormatSummaryTableWithDetails renders the summary table with per-session
// breakdown. results carries each session's pre-computed analysis (from
// AnalyzeMultipleSessions), so the formatter never re-parses. storageDir is
// the Claude project directory, printed under the footer when it's set
// (with -v). Color and glyph choices are driven by noColor. width is the
// terminal's width, or 0 when stdout isn't a terminal (see reportWidth).
func FormatSummaryTableWithDetails(analysis *models.SessionAnalysis, results []models.SessionResult, storageDir string, noColor bool, expandAgents bool, width int) string {
	if analysis.Window != nil && analysis.MessageCount == 0 {
		return emptyWindow(*analysis.Window)
	}
	var sb strings.Builder
	sectionWidth := reportWidth(width)

	// Header panel
	sb.WriteString(renderHeaderPanel(analysis, sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Hero total cost as section header
	sb.WriteString(renderHeroCost(analysis.TotalCost.TotalCost, sectionWidth, noColor))
	sb.WriteString("\n\n")

	sb.WriteString(renderCostRows(analysis.TotalCost, analysis.TotalUsage, sectionWidth, noColor))

	// Cost by model section, left out for the same reason as in show
	if len(analysis.CostByModel) > 0 {
		sb.WriteString("\n")
		sb.WriteString(render.SectionHeader("COST BY MODEL", sectionWidth, noColor))
		sb.WriteString("\n\n")
		sb.WriteString(formatCostByModelContent(analysis, noColor))
	}

	// Get session breakdown data (needed for both chart and table)
	breakdownResult := renderSessionBreakdown(results, sectionWidth, noColor, expandAgents)

	// Session breakdown section (includes chart and table)
	sb.WriteString("\n")
	sb.WriteString(render.SectionHeader("SESSION BREAKDOWN", sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Cost chart at the top of the section, for sessions that cost anything
	if chart := renderCostChart(breakdownResult.costs, sectionWidth, noColor); chart != "" {
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
			line = styles.DimStyle.Render(line)
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
func renderSessionBreakdown(results []models.SessionResult, width int, noColor bool, expandAgents bool) sessionBreakdownResult {
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
	// MODIFIED is a date and time, or just the date when the table would
	// otherwise be too wide.
	timeWidth, dateWidth := len("MODIFIED"), len("MODIFIED")
	for _, r := range sorted {
		timeWidth = max(timeWidth, len(render.DateTime(r.Entry.Modified, now())))
		dateWidth = max(dateWidth, len(render.Date(r.Entry.Modified, now())))
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
	}

	// deepestTree is the widest agent tree before its costs, with a 10-column
	// model, or 0 without one.
	deepestTree := 0
	if expandAgents {
		for _, r := range sorted {
			if r.Analysis != nil && len(r.Analysis.Agents) > 0 {
				deepestTree = max(deepestTree, agentTreeWidth(r.Analysis, 10, true))
			}
		}
	}

	dateOnly := false
	modifiedWidth := func() int {
		if dateOnly {
			return dateWidth
		}
		return timeWidth
	}
	// lead is the room before MODEL: the indent, #, SESSION and MODIFIED,
	// with their gaps.
	lead := func() int { return 2 + 4 + 1 + 8 + 2 + modifiedWidth() + 2 }
	modelWidth := 10
	costCol := func() int { return lead() + modelWidth + 2 }
	showAgents := !expandAgents
	rowWidth := func() int {
		if showAgents {
			return costCol() + len("AGENTS") + 2 + costWidth
		}
		return costCol() + costWidth
	}
	// When the rows or the agent trees under them don't fit width, MODIFIED
	// loses its time, then AGENTS (a count the expanded tree replaces) goes,
	// with the time back if that makes room for it, and then MODEL narrows,
	// down to a stub.
	fits := func() bool { return rowWidth() <= width && deepestTree+2+costWidth <= width }
	layouts := [][2]bool{{false, true}, {true, true}, {false, false}, {true, false}}
	for i, l := range layouts {
		dateOnly = l[0]
		showAgents = !expandAgents && l[1]
		if fits() || i == len(layouts)-1 {
			break
		}
	}
	modelWidth = max(modelWidth-max(rowWidth()-width, 0), minModelWidth)

	// treeMsgs is whether agent rows show their message count, which goes
	// from every row before any model would be cut to fit before COST: the
	// model tells agents apart, the count doesn't.
	treeMsgs := true
	if expandAgents {
		// MODEL widens when the deepest agent row or a workflow label would
		// run into COST, as far as the report's width allows; past that,
		// agents' models and workflow labels are cut.
		maxModelWidth := modelWidth + max(width-rowWidth(), 0)
		maxCostCol := costCol() + maxModelWidth - modelWidth
		for _, r := range sorted {
			if r.Analysis != nil && len(r.Analysis.Agents) > 0 && agentTreeWidth(r.Analysis, 10, true)+2 > maxCostCol {
				treeMsgs = false
			}
		}
		for _, r := range sorted {
			if r.Analysis == nil {
				continue
			}
			if len(r.Analysis.Agents) > 0 {
				full := agentTreeWidth(r.Analysis, 10, treeMsgs) + 2 - costCol() + modelWidth
				cut := agentTreeWidth(r.Analysis, minModelWidth, treeMsgs) + 2 - costCol() + modelWidth
				modelWidth = max(modelWidth, min(full, max(maxModelWidth, cut)))
			}
			for _, wf := range r.Analysis.Workflows {
				label := treeIndent + treeStep + runewidth.StringWidth(workflowLabel(wf)) + 2
				modelWidth = max(modelWidth, min(label-costCol()+modelWidth, maxModelWidth))
			}
		}
	}
	var analyses []*models.SessionAnalysis
	for _, r := range sorted {
		analyses = append(analyses, r.Analysis)
	}
	treeStatus := workflowStatuses("", costCol()-treeIndent-treeStep-2, analyses...)

	row := func(num, id, modified, model, agents, cost string) string {
		line := fmt.Sprintf("  %s %s  %s  %s", num, id, modified, model)
		if showAgents {
			line += "  " + agents
		}
		return line + "  " + cost
	}
	pad := func(s string, w int) string { return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0)) }
	headerRow := row(
		fmt.Sprintf("%4s", "#"),
		fmt.Sprintf("%-8s", "SESSION"),
		fmt.Sprintf("%-*s", modifiedWidth(), "MODIFIED"),
		fmt.Sprintf("%-*s", modelWidth, "MODEL"),
		fmt.Sprintf("%*s", len("AGENTS"), "AGENTS"),
		fmt.Sprintf("%*s", costWidth, "COST"),
	)
	contentWidth := max(width-4, lipgloss.Width(headerRow)-2)
	rule := "  " + strings.Repeat(styles.LineHorizontal, contentWidth)
	if noColor {
		sb.WriteString(headerRow + "\n" + rule + "\n")
	} else {
		sb.WriteString(styles.HeaderStyle.Render(headerRow) + "\n" + "  " + styles.DimStyle.Render(strings.Repeat(styles.LineHorizontal, contentWidth)) + "\n")
	}

	dim := func(s string) string {
		if noColor {
			return s
		}
		return styles.DimStyle.Render(s)
	}
	for i := len(sorted) - 1; i >= 0; i-- {
		r := sorted[i]
		num := dim(fmt.Sprintf("%4d", len(sorted)-i))
		id := fmt.Sprintf("%-8s", render.TruncateID(r.Entry.SessionID, 8))
		modifiedAt := render.DateTime(r.Entry.Modified, now())
		if dateOnly {
			modifiedAt = render.Date(r.Entry.Modified, now())
		}
		modified := dim(pad(modifiedAt, modifiedWidth()))

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
			agents = styles.DimStyle.Render(agents)
			costStr = render.CostColored(cost, styles.GetCostGradientColor(cost, minCost, maxCost), costWidth)
		}
		sb.WriteString(row(num, id, modified, model, agents, costStr) + "\n")

		if expandAgents && a.HasAgents && len(a.Agents) > 0 {
			sb.WriteString(renderAgentTreeRows(a, noColor, treeLayout{
				costCol:   costCol(),
				costWidth: costWidth,
				msgs:      treeMsgs,
				status:    treeStatus,
			}))
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
		sb.WriteString("  " + styles.DimStyle.Render(strings.Repeat(styles.LineHorizontal, contentWidth)) + "\n")
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

// minModelWidth is as narrow as a MODEL column or an agent row's model gets
// on a narrow terminal: enough to tell Opus from Sonnet.
const minModelWidth = 6

// agentTreeWidth is how wide an analysis's agent rows get before their cost:
// connectors, marker, a modelWidth model and, with msgs, message count.
// Workflow agents sit a level deeper than the rest.
func agentTreeWidth(a *models.SessionAnalysis, modelWidth int, msgs bool) int {
	depth := 1
	for _, agent := range a.Agents {
		if agent.WorkflowID != "" {
			depth = 2
		}
	}
	width := treeIndent + depth*treeStep + 10 + 1 + modelWidth
	if msgs {
		width += 1 + agentMsgsWidth(a.Agents)
	}
	return width
}

// treeLayout places an agent tree's rows: each cost in a costWidth cell at
// costCol, message counts shown with msgs, and workflow statuses with status.
type treeLayout struct {
	costCol, costWidth int
	msgs, status       bool
}

// renderAgentTreeRows renders a session's agents as a tree under its row,
// each cost in a costWidth cell starting at costCol, under the session's
// COST, and each model cut when it would run into it. A workflow run is a
// node carrying its subtotal, dimmed since the agents under it repeat it,
// with its agents one level deeper:
//
//	├─ [Aa1b2c3d] Haiku 4.5   12 msgs   $0.4200
//	└─ workflow: audit (completed)      $1.65
//	   ├─ [Aw1a2b3c] Opus 4.8 21 msgs   $1.10
//	   └─ [Aw6e5d4c] Sonnet 5  1 msg    $0.5500
func renderAgentTreeRows(analysis *models.SessionAnalysis, noColor bool, layout treeLayout) string {
	var sb strings.Builder
	costCol, costWidth := layout.costCol, layout.costWidth
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
	line := func(left string, cost float64, costColor color.Color) string {
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
		return styles.DimStyle.Render(s)
	}
	agentRow := func(prefix string, agent *models.AgentAnalysis) string {
		modelName := render.PrimaryModel(agent.CostByModel)
		marker := fmt.Sprintf("%-10s", "[A"+render.ShortAgentID(agent.AgentID)+"]")
		// Up to 10 columns, as many as leave the 2-column gap before COST.
		room := costCol - 2 - lipgloss.Width(prefix) - 10 - 1
		if layout.msgs {
			room -= 1 + msgsWidth
		}
		modelWidth := min(10, max(room, minModelWidth))
		model := fmt.Sprintf("%-*s", modelWidth, render.ClampModel(modelName, modelWidth))
		msgs := fmt.Sprintf("%*s", msgsWidth, agentMsgs(agent.MessageCount))
		if !noColor {
			marker = lipgloss.NewStyle().Foreground(styles.GetAgentColor(agent.AgentID)).Render(marker)
			model = lipgloss.NewStyle().Foreground(styles.GetModelColor(modelName)).Render(model)
			msgs = styles.DimStyle.Render(msgs)
		}
		left := prefix + marker + " " + model
		if layout.msgs {
			left += " " + msgs
		}
		return line(left, agent.TotalCost.TotalCost, styles.SecondaryColor)
	}

	for i, n := range nodes {
		last := i == len(nodes)-1
		prefix := indent + dim(connector(last))
		if n.agent != nil {
			sb.WriteString(agentRow(prefix, n.agent))
			continue
		}
		room := costCol - treeIndent - treeStep - 2
		label := fitWorkflowLabel("", analysis.WorkflowByID(n.workflow), room, layout.status)
		sb.WriteString(line(prefix+dim(label), workflowCost(agents, n.workflow), styles.SecondaryColor))
		for j, child := range n.children {
			sb.WriteString(agentRow(indent+dim(rail(last)+connector(j == len(n.children)-1)), child))
		}
	}

	return sb.String()
}

// workflowLabel is render.WorkflowLabel, except that a label too long for
// that function's cap loses the end of its name rather than its status. Cut
// off by the cap, a status would go from one heading while the others keep
// theirs, and that heading would read as a run that had none.
func workflowLabel(meta models.WorkflowMeta) string {
	label := render.WorkflowLabel(meta)
	suffix := " (" + meta.Status + ")"
	if meta.Status == "" || strings.HasSuffix(label, suffix) {
		return label
	}
	// Cut, the label is as wide as the cap allows.
	room := runewidth.StringWidth(label) - runewidth.StringWidth(suffix)
	meta.Status = ""
	name := render.WorkflowLabel(meta)
	if room <= runewidth.StringWidth("workflow: ") {
		return name
	}
	cut := strings.TrimSuffix(truncateRight(name, room), styles.Ellipsis)
	return strings.TrimRight(cut, " ") + styles.Ellipsis + suffix
}

// workflowStatuses reports whether the workflow labels of analyses, after
// prefix, keep their statuses in width: all do when each fits with its own,
// else none does, so a label without one can't read as a run that had none.
func workflowStatuses(prefix string, width int, analyses ...*models.SessionAnalysis) bool {
	for _, a := range analyses {
		if a == nil {
			continue
		}
		for _, agent := range a.Agents {
			if agent.WorkflowID == "" {
				continue
			}
			// A status too long for the label's cap never shows, and can't
			// sit beside headings that show theirs.
			meta := a.WorkflowByID(agent.WorkflowID)
			label := workflowLabel(meta)
			statusCut := meta.Status != "" && !strings.HasSuffix(label, " ("+meta.Status+")")
			if statusCut || runewidth.StringWidth(prefix+label) > width {
				return false
			}
		}
	}
	return true
}

// fitWorkflowLabel fits a workflow run's label, after prefix, into width. The
// label loses its status unless status is set (see workflowStatuses) before
// the name is cut, and a cut never leaves a space before its ellipsis.
func fitWorkflowLabel(prefix string, meta models.WorkflowMeta, width int, status bool) string {
	if !status {
		meta.Status = ""
	}
	label := prefix + workflowLabel(meta)
	if runewidth.StringWidth(label) <= width {
		return label
	}
	cut := strings.TrimSuffix(truncateRight(label, width), styles.Ellipsis)
	return strings.TrimRight(cut, " ") + styles.Ellipsis
}
