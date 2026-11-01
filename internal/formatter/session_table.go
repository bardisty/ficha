package formatter

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// FormatSessionTable renders a session analysis as a table (single session or
// summary). Color and glyph choices are driven by noColor. width is the
// terminal's width, or 0 when stdout isn't a terminal (see reportWidth).
func FormatSessionTable(analysis *models.SessionAnalysis, noColor bool, width int) string {
	var sb strings.Builder
	sectionWidth := reportWidth(width)

	// Detect summary vs show mode
	isSummary := analysis.IsSummary

	// A session nobody has replied to yet has nothing to price, and a report
	// of empty sections reads like a broken tool or the wrong session.
	if isSummary && analysis.Window != nil && analysis.MessageCount == 0 {
		return emptyWindow(*analysis.Window, analysis.Project)
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

	sb.WriteString(renderCostRows(analysis.TotalCost, analysis.TotalUsage, sectionWidth, noColor))

	// Context window section - only for single sessions, not summaries
	if !isSummary {
		contextSize := analysis.LastMessageUsage.ContextWindowSize()
		if contextSize > 0 {
			sb.WriteString("\n")
			sb.WriteString(renderContextSection(analysis, sectionWidth, noColor))
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
	if days := renderDaysSection(analysis.Days, sectionWidth, noColor); days != "" {
		sb.WriteString("\n" + days)
	}

	// Agent breakdown (shown when agents exist). The summary aggregate sets
	// HasAgents/AgentsCost without collecting the per-agent records, and every
	// line of this section is per-agent, so require them (same guard as the
	// summary detail table).
	if analysis.HasAgents && len(analysis.Agents) > 0 {
		sb.WriteString("\n")
		sb.WriteString(render.SectionHeader("AGENT SUB-SESSIONS", sectionWidth, noColor))
		sb.WriteString("\n\n")
		sb.WriteString(formatAgentBreakdownContent(analysis, sectionWidth, noColor))
	}

	// Message insights (shown when insights are available)
	if analysis.Insights != nil {
		sb.WriteString("\n")
		sb.WriteString(render.SectionHeader("MESSAGE INSIGHTS", sectionWidth, noColor))
		sb.WriteString("\n\n")
		sb.WriteString(formatInsightsSectionContent(analysis.Insights, analysis.HasAgents, sectionWidth, noColor))
	}

	// Footer with double-line separator
	sb.WriteString("\n")
	sb.WriteString(renderFooterDoubleRule(sectionWidth, noColor))
	sb.WriteString("\n")

	// Footer stats. summary's total names the agents' share, which show
	// already gives as its own subtotal row.
	total := "Total: " + render.Cost(analysis.TotalCost.TotalCost)
	messages := messagesField(analysis.MessageCount, analysis.ParentMessageCount, analysis.AgentMessageCount)
	if isSummary {
		total = totalField(analysis.TotalCost.TotalCost, analysis.AgentsCost.TotalCost, sectionWidth)
		messages = splitMessagesField(analysis.MessageCount, analysis.ParentMessageCount, analysis.AgentMessageCount, sectionWidth)
	}
	fields := []string{total, messages}
	if isSummary {
		if sessionCount := analysis.SessionCount; sessionCount > 0 {
			fields = append(fields, "Sessions: "+render.Count(sessionCount))
		}
	} else if !analysis.EndTime.IsZero() {
		// Show last active time for single sessions
		fields = append(fields, "Last active: "+render.DateTime(analysis.EndTime, now()))
	}
	sb.WriteString(footerStats(fields, sectionWidth, noColor))
	sb.WriteString("\n")
	if !isSummary && analysis.Copies != nil {
		sb.WriteString(copiesNote(analysis, sectionWidth, noColor))
	}

	// Single-line help separator
	sb.WriteString(renderFooterSingleRule(sectionWidth, noColor))

	return trimLineEnds(sb.String())
}

// copiesNote is show's note under the footer on the messages its file
// shares with other sessions, wrapped to width.
func copiesNote(a *models.SessionAnalysis, width int, noColor bool) string {
	note := render.CopiesNoteFor(*a.Copies, a.MessageCount, a.TotalCost.TotalCost)
	lines := strings.Split(render.WrapNoRunt(note, width), "\n")
	if !noColor {
		for i, l := range lines {
			lines[i] = styles.FooterStyle.Render(l)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// renderContextSection renders the context window section: the gauge line,
// then its scope and headroom note under the bar.
func renderContextSection(analysis *models.SessionAnalysis, width int, noColor bool) string {
	c := analyzer.ContextUsage(analysis.LastMessageUsage, analysis.LastMessageModel)
	if c == nil {
		return ""
	}

	// Fill the section after the "  Context " label; the gauge right-aligns
	// its percentage, so "Context  95%" and "Context 100%" align.
	gaugeWidth := width - 10
	line, note := render.ContextGauge(*c, analysis.LastMessageModel, false, nil, gaugeWidth, noColor, false)
	return "  Context " + line + "\n           " + note + "\n"
}

// agentMsgsWidth returns the msgs column width for a set of agent rows: at
// least 8 (the historical fixed width, fitting "999 msgs"), grown to the
// longest count so an oversized count widens the column for every row instead
// of pushing a single row's cost out of alignment.
func agentMsgsWidth(agents []models.AgentAnalysis) int {
	width := 8
	for _, a := range agents {
		if l := len(render.MsgCount(a.MessageCount)); l > width {
			width = l
		}
	}
	return width
}

// formatAgentBreakdownContent renders agent breakdown rows (content only, no header)
// Layout: [A<id>] task model msgs ctx cost
// All rows align costs at column 45 (2 indent + 43 content), further right
// when a task, or a workflow heading, needs the room and width has it, or
// further left when the rows would be wider than width
// Example:
//
//	Main conversation                                      $1.14
//	[Aa0b184d] Find the auth middleware Opus 4.5   14 msgs  38% ctx   $0.3588
//	Agents subtotal                                        $5.11
func formatAgentBreakdownContent(analysis *models.SessionAnalysis, width int, noColor bool) string {
	var sb strings.Builder

	// labelWidth is the room before the cost column, less the indent and a
	// 3-space gap: 40, or as wide as the widest workflow heading with its
	// status, or the agent rows. It narrows to fit width, but never so far
	// that an agent row's marker, model and message count (10+1+11+1+msgs,
	// then a space) would reach the cost. Past width, headings are cut as
	// workflowStatuses says.
	msgsWidth := agentMsgsWidth(analysis.Agents)
	taskWant := agentTaskWidth(analysis.Agents)
	cellWant := taskWant
	if taskWant > 0 {
		cellWant = max(cellWant, tasklessCountWidth(analysis.Agents))
	}
	headingWidth := 40
	for _, agent := range analysis.Agents {
		if agent.WorkflowID != "" {
			heading := styles.GroupRule + " " + render.WorkflowLabel(analysis.WorkflowByID(agent.WorkflowID))
			headingWidth = max(headingWidth, lipgloss.Width(heading))
		}
	}
	// labelFor is labelWidth for agent rows with a rowMsgs-wide count (none
	// when negative) and a rowTask-wide task. Rows without a task never
	// widen it, so their count gives way as it would with no task at all.
	labelFor := func(rowMsgs, rowTask int) int {
		want := headingWidth
		if rowTask > 0 {
			want = max(want, agentRowWidth(rowMsgs, rowTask)-2)
		}
		return max(min(want, width-2-3-11), msgsWidth+21)
	}
	labelWidth := labelFor(msgsWidth, taskWant)

	// Each agent with [A<id>] task Model msgs ctx cost format — the marker
	// carries the abbreviated real agent ID, matching the breakdown TUI's
	// scheme. The task, the flexible part, is in a column as wide as the
	// widest one, and there only when some agent has one.
	// Format: 2(indent) + 10(marker) + 1 + task + 1 + 11(model) + msgs + ctx + gap + cost
	// where everything from the marker to the gap fills labelWidth + 3,
	// keeping cost aligned with parent and subtotal. Where a task wouldn't
	// fit whole, the message count goes first, then the task is cut, and
	// cut below render.MinTaskWidth it goes too, leaving the row as it is
	// without one. A task shorter than that is never cut. Once the count
	// has made way, an agent without a task shows its count in the task's
	// cell, which is never cut either: a cell without room for it goes, and
	// the short tasks with it.
	// Where the context reading, " 38% ctx", would leave no gap, it takes
	// the count's place, as in watch; labelWidth's floor always leaves it
	// room there.
	// Workflow agents are grouped after regular agents; a dim header line marks
	// each run's start.
	showMsgs := agentRowWidth(msgsWidth, taskWant) < labelWidth+3
	taskWidth := taskWant
	if !showMsgs {
		taskWidth = min(cellWant, labelWidth+3-agentRowWidth(-1, 0)-2)
		if taskWidth < min(cellWant, render.MinTaskWidth) {
			taskWidth = 0
			labelWidth = labelFor(msgsWidth, 0)
			showMsgs = agentRowWidth(msgsWidth, 0) < labelWidth+3
		}
	}
	rowMsgs := -1
	if showMsgs {
		rowMsgs = msgsWidth
	}
	// Whatever went from the rows hands its room back, so the cost column
	// sits no further right than the rows need.
	labelWidth = labelFor(rowMsgs, taskWidth)

	// Parent session cost, after 2(indent) + labelWidth + 3(spaces): column 45
	// unless a workflow heading, a task or the width moved it
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	// It names the main conversation's model. The agent rows' model column
	// is 11 columns past the label's start and any task cell; MainRowLabel
	// puts the model there when it can.
	taskCell := 0
	if taskWidth > 0 {
		taskCell = 1 + taskWidth
	}
	label := render.MainRowLabel(analysis.LastMessageModel, 11+taskCell, labelWidth, false, noColor)
	sb.WriteString("  " + label + "   " + formatCostStyled(analysis.ParentCost.TotalCost, 11, noColor) + "\n")
	msgsGap := strings.Repeat(" ", max(1, labelWidth+3-agentRowWidth(rowMsgs, taskWidth)))
	status := workflowStatuses(styles.GroupRule+" ", labelWidth, analysis)
	prevWorkflow := ""
	for _, agent := range analysis.Agents {
		if agent.WorkflowID != prevWorkflow {
			prevWorkflow = agent.WorkflowID
			if agent.WorkflowID != "" {
				// The run's subtotal sits in the cost column, so a workflow
				// compares with the parent session at a glance.
				heading := fitWorkflowLabel(styles.GroupRule+" ", analysis.WorkflowByID(agent.WorkflowID), labelWidth, status)
				pad := strings.Repeat(" ", labelWidth-lipgloss.Width(heading)+3)
				cost := workflowCost(analysis.Agents, agent.WorkflowID)
				if noColor {
					sb.WriteString("  " + heading + pad + render.CostCell(cost, 11) + "\n")
				} else {
					// Dim like its heading: the subtotal repeats the rows
					// below it and isn't part of the column's sum.
					sb.WriteString("  " + styles.DimStyle.Render(heading) + pad + render.CostColored(cost, styles.SecondaryColor, 11) + "\n")
				}
			}
		}
		// [A<id>] carries the abbreviated real agent ID (%-10s fits [A1234567])
		marker := "[A" + render.ShortAgentID(agent.AgentID) + "]"

		// Get primary model for this agent
		modelName := render.PrimaryModel(agent.CostByModel)
		modelLabel := render.ClampModel(modelName, 11)

		// Format message count with singular/plural, right-aligned after a
		// space, or nothing where the context reading took its place
		msgStr := ""
		if showMsgs {
			msgStr = fmt.Sprintf(" %*s", msgsWidth, render.MsgCount(agent.MessageCount))
		}
		ctx := " " + render.AgentContextCell(agent.Context, noColor)
		task := ""
		if taskWidth > 0 {
			count := ""
			if !showMsgs {
				count = render.MsgCount(agent.MessageCount)
			}
			task = " " + render.TaskCell(agent.TaskLabel(), count, taskWidth, noColor)
		}

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s%s %-11s%s%s%s%s\n",
				marker, task, modelLabel, msgStr, ctx, msgsGap, render.CostCell(agent.TotalCost.TotalCost, 11)))
		} else {
			// Color the marker by hashing the full agent ID (matches breakdown)
			agentColor := styles.GetAgentColor(agent.AgentID)
			markerStyled := lipgloss.NewStyle().Foreground(agentColor).Render(fmt.Sprintf("%-10s", marker))

			// Color model name by tier
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-11s", modelLabel))

			// Dim the message count, leaving its leading space plain
			msgStyled := ""
			if msgStr != "" {
				msgStyled = " " + styles.DimStyle.Render(msgStr[1:])
			}

			costStr := formatCostStyled(agent.TotalCost.TotalCost, 11, noColor)

			sb.WriteString(fmt.Sprintf("  %s%s %s%s%s%s%s\n",
				markerStyled, task, modelStyled, msgStyled, ctx, msgsGap, costStr))
		}
	}

	// Agents subtotal in bold green (matches TotalValueStyle for visual hierarchy)
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	if noColor {
		sb.WriteString(fmt.Sprintf("  %-*s   %s\n", labelWidth, "Agents subtotal", render.CostCell(analysis.AgentsCost.TotalCost, 11)))
	} else {
		paddedSubtotal := fmt.Sprintf("%-*s", labelWidth, "Agents subtotal")
		sb.WriteString(fmt.Sprintf("  %s   %s\n", paddedSubtotal, formatCostStyledBoldGreen(analysis.AgentsCost.TotalCost, 11, noColor)))
	}

	return sb.String()
}

// agentTaskWidth is the width of the widest task among agents, or 0 when
// none has one.
func agentTaskWidth(agents []models.AgentAnalysis) int {
	width := 0
	for _, a := range agents {
		width = max(width, lipgloss.Width(a.TaskLabel()))
	}
	return width
}

// tasklessCountWidth is the width of the widest message count among agents
// with no task, which their task cell shows once the count column has made
// way, or 0 when every agent has a task. It counts only when some row has a
// task cell, which is the caller's to know.
func tasklessCountWidth(agents []models.AgentAnalysis) int {
	width := 0
	for _, a := range agents {
		if a.TaskLabel() == "" {
			width = max(width, len(render.MsgCount(a.MessageCount)))
		}
	}
	return width
}

// agentRowWidth is how wide show's agent row is from the marker through its
// context reading: with a msgsWidth message count (none when negative), and
// with a taskWidth task (none when 0).
func agentRowWidth(msgsWidth, taskWidth int) int {
	width := 10 + 1 + 11 + 1 + render.AgentContextWidth
	if msgsWidth >= 0 {
		width += 1 + msgsWidth
	}
	if taskWidth > 0 {
		width += 1 + taskWidth
	}
	return width
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

// formatInsightsSectionContent renders message insights rows (content only,
// no header). A row that would be wider than width loses its last field,
// the detail, rather than wrap.
func formatInsightsSectionContent(insights *models.MessageInsights, hasAgents bool, width int, noColor bool) string {
	var sb strings.Builder
	dim := func(s string) string {
		if noColor {
			return s
		}
		return styles.DimStyle.Render(s)
	}
	// row writes a labelled row of fields, dropping trailing fields past the
	// first until it fits.
	row := func(label string, fields ...string) {
		line := fmt.Sprintf("  %-10s %s", label, strings.Join(fields, "  "))
		for len(fields) > 1 && lipgloss.Width(line) > width {
			fields = fields[:len(fields)-1]
			line = fmt.Sprintf("  %-10s %s", label, strings.Join(fields, "  "))
		}
		sb.WriteString(line + "\n")
	}

	// When agents ran, these insights cover the main conversation alone (the
	// agent rows live in AGENT SUB-SESSIONS above and are excluded here). Label
	// the scope so it can't be silently mistaken for the breakdown view, which
	// computes Peak/trend over the merged parent+agent messages. No label when
	// there are no agents: parent-only and all-messages are then identical.
	if hasAgents {
		sb.WriteString("  " + dim("main conversation only, agents excluded") + "\n")
	}

	fits := func(label string, fields ...string) bool {
		return lipgloss.Width(fmt.Sprintf("  %-10s %s", label, strings.Join(fields, "  "))) <= width
	}
	// timed is a row's cost and time without its detail: the time in the
	// longest of its forms that fits, so a dated time shortens before it goes
	// and the row doesn't read as a message with no time.
	timed := func(label, cost string, t time.Time) []string {
		for _, clock := range []string{render.ClockOn(t, now()), render.ClockOnShort(t, now())} {
			if fits(label, cost, dim("("+clock+")")) {
				return []string{cost, dim("(" + clock + ")")}
			}
		}
		return []string{cost}
	}

	// First and Last lose their detail together, so neither reads as a
	// message that had none. Each then fits its own time.
	type messageRow struct {
		label  string
		msg    *models.MessageSnapshot
		fields []string
	}
	var messages []messageRow
	detail := true
	labels := []string{"First", "Last"}
	for i, msg := range []*models.MessageSnapshot{insights.FirstMessage, insights.LastMessage} {
		if msg == nil {
			continue
		}
		m := messageRow{labels[i], msg, []string{
			formatCostStyled(msg.Cost, 10, noColor),
			dim("(" + render.ClockOn(msg.Timestamp, now()) + ")"),
			dim(render.CostComponentLabel(msg.MainCostComponent) + ": " + render.Cost(msg.MainCostValue)),
		}}
		if !fits(m.label, m.fields...) {
			detail = false
		}
		messages = append(messages, m)
	}
	for _, m := range messages {
		if !detail {
			m.fields = timed(m.label, m.fields[0], m.msg.Timestamp)
		}
		row(m.label, m.fields...)
	}

	// Highest cost (only if notably above average). The multiplier goes
	// before the time shortens.
	if insights.HighestCost != nil {
		highest := insights.HighestCost
		// The multiplier is information, not a warning: nearly every session
		// has a message well above its average.
		fields := []string{
			formatCostStyled(highest.Cost, 10, noColor),
			dim("(" + render.ClockOn(highest.Timestamp, now()) + ")"),
			dim(fmt.Sprintf("%.1fx avg cost", insights.CostMultiplier())),
		}
		if !fits("Peak", fields...) {
			fields = timed("Peak", fields[0], highest.Timestamp)
		}
		row("Peak", fields...)
	}

	// Trend (only once the analyzer actually computed one — see HasTrend)
	if insights.HasTrend() {
		trendSymbol := render.TrendSymbol(insights.CostTrend)
		if !noColor {
			// Color the trend symbol based on direction
			switch insights.CostTrend {
			case models.TrendIncreasing:
				trendSymbol = lipgloss.NewStyle().Foreground(styles.WarningColor).Render(trendSymbol)
			case models.TrendDecreasing:
				trendSymbol = lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(trendSymbol)
			default:
				trendSymbol = styles.DimStyle.Render(trendSymbol)
			}
		}
		direction := trendSymbol + " " + insights.TrendDescription()
		recent, average := render.Cost(insights.RecentAvgCost), render.Cost(insights.AverageCost)
		// The direction is the reading; the comparison behind it shortens,
		// then goes. Both averages are per message, so the short form drops
		// "/msg" and keeps what tells them apart.
		lines := []string{
			fmt.Sprintf("  %-10s last %d %s/msg vs %s/msg avg  %s", "Trend", insights.TrendWindow, recent, average, direction),
			fmt.Sprintf("  %-10s last %d %s vs %s avg  %s", "Trend", insights.TrendWindow, recent, average, direction),
			fmt.Sprintf("  %-10s %s", "Trend", direction),
		}
		line := lines[len(lines)-1]
		for _, l := range lines {
			if lipgloss.Width(l) <= width {
				line = l
				break
			}
		}
		sb.WriteString(line + "\n")
	}

	return sb.String()
}
