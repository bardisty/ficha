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
func renderContextSection(analysis *models.SessionAnalysis, width int, noColor bool) string {
	contextSize := analysis.LastMessageUsage.ContextWindowSize()
	if contextSize == 0 {
		return ""
	}
	maxContext := pricing.GetModelPricing(analysis.LastMessageModel).MaxContextTokens

	// Fill the section after the "  Context " label; the gauge right-aligns
	// its percentage, so "Context  95%" and "Context 100%" align.
	gaugeWidth := width - 10
	line, note := render.ContextGauge(contextSize, maxContext, gaugeWidth, noColor, false)
	if !noColor {
		note = lipgloss.NewStyle().Foreground(styles.NoteColor).Render(note)
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
// All rows align costs at column 45 (2 indent + 43 content), further right
// when a workflow heading needs the room and width has it, or further left
// when the rows would be wider than width
// Example:
//
//	Parent session                              $1.14
//	[Aa0b184d] Opus 4.5      14 msgs            $0.3588
//	Agents subtotal                             $5.11
func formatAgentBreakdownContent(analysis *models.SessionAnalysis, width int, noColor bool) string {
	var sb strings.Builder

	// labelWidth is the room before the cost column, less the indent and a
	// 3-space gap: 40, or as wide as the widest workflow heading with its
	// status. It narrows to fit width, but never so far that an agent row's
	// marker, model and message count (10+1+11+1+msgs, then a space) would
	// reach the cost. Past width, headings are cut as workflowStatuses says.
	msgsWidth := agentMsgsWidth(analysis.Agents)
	labelWidth := 40
	for _, agent := range analysis.Agents {
		if agent.WorkflowID != "" {
			heading := styles.GroupRule + " " + workflowLabel(analysis.WorkflowByID(agent.WorkflowID))
			labelWidth = max(labelWidth, runewidth.StringWidth(heading))
		}
	}
	labelWidth = max(min(labelWidth, width-2-3-11), msgsWidth+21)

	// Parent session cost, after 2(indent) + labelWidth + 3(spaces): column 45
	// unless a workflow heading or the width moved it
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	if noColor {
		sb.WriteString(fmt.Sprintf("  %-*s   %s\n", labelWidth, "Parent session", render.CostCell(analysis.ParentCost.TotalCost, 11)))
	} else {
		paddedLabel := fmt.Sprintf("%-*s", labelWidth, "Parent session")
		sb.WriteString(fmt.Sprintf("  %s   %s\n", paddedLabel, formatCostStyled(analysis.ParentCost.TotalCost, 11, noColor)))
	}

	// Each agent with [A<id>] Model msgs cost format — the marker carries the
	// abbreviated real agent ID, matching the breakdown TUI's scheme.
	// Format: 2(indent) + 10(marker) + 1 + 11(model) + 1 + msgs + gap + cost
	// where msgs + gap = labelWidth - 20 (normally 8 + 12; the gap shrinks as
	// the msgs column grows to fit an oversized count), keeping cost at 45
	// chars (aligned with parent and subtotal).
	// Workflow agents are grouped after regular agents; a dim header line marks
	// each run's start.
	msgsGap := strings.Repeat(" ", max(1, labelWidth-20-msgsWidth))
	status := workflowStatuses(styles.GroupRule+" ", labelWidth, analysis)
	prevWorkflow := ""
	for _, agent := range analysis.Agents {
		if agent.WorkflowID != prevWorkflow {
			prevWorkflow = agent.WorkflowID
			if agent.WorkflowID != "" {
				// The run's subtotal sits in the cost column, so a workflow
				// compares with the parent session at a glance.
				heading := fitWorkflowLabel(styles.GroupRule+" ", analysis.WorkflowByID(agent.WorkflowID), labelWidth, status)
				pad := strings.Repeat(" ", labelWidth-runewidth.StringWidth(heading)+3)
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
			msgStyled := styles.DimStyle.Render(fmt.Sprintf("%*s", msgsWidth, msgStr))

			costStr := formatCostStyled(agent.TotalCost.TotalCost, 11, noColor)

			sb.WriteString(fmt.Sprintf("  %s %s %s%s%s\n",
				markerStyled, modelStyled, msgStyled, msgsGap, costStr))
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

	// First and Last lose their detail together, so neither reads as a
	// message that had none.
	type messageRow struct {
		label  string
		fields []string
	}
	var messages []messageRow
	detail := true
	labels := []string{"First", "Last"}
	for i, msg := range []*models.MessageSnapshot{insights.FirstMessage, insights.LastMessage} {
		if msg == nil {
			continue
		}
		m := messageRow{labels[i], []string{
			formatCostStyled(msg.Cost, 10, noColor),
			dim("(" + render.Clock(msg.Timestamp) + ")"),
			dim(render.CostComponentLabel(msg.MainCostComponent) + ": " + render.Cost(msg.MainCostValue)),
		}}
		if lipgloss.Width(fmt.Sprintf("  %-10s %s", m.label, strings.Join(m.fields, "  "))) > width {
			detail = false
		}
		messages = append(messages, m)
	}
	for _, m := range messages {
		if !detail {
			m.fields = m.fields[:2]
		}
		row(m.label, m.fields...)
	}

	// Highest cost (only if notably above average)
	if insights.HighestCost != nil {
		highest := insights.HighestCost
		// The multiplier is information, not a warning: nearly every session
		// has a message well above its average.
		row("Peak",
			formatCostStyled(highest.Cost, 10, noColor),
			dim("("+render.Clock(highest.Timestamp)+")"),
			dim(fmt.Sprintf("%.1fx avg cost", insights.CostMultiplier())))
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
