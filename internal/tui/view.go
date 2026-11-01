package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// Panel sizing: panels, separators, and section headers are designed for
// defaultPanelWidth columns; on narrower terminals they shrink toward
// minPanelWidth so box-drawing lines don't wrap and desynchronize the fixed
// header/footer height math. minPanelWidth plus the 2-column indent is
// minTermWidth, so the whole frame fits at every width watch draws. Past 80
// columns the panel grows with the terminal, keeping the 2-column margin it
// has at 80 on each side, up to maxPanelWidth: that gives the cost chart 110
// minutes, and wider rules would only stretch the sections whose figures
// sit at their left.
const (
	defaultPanelWidth = 76
	maxPanelWidth     = 118
	minPanelWidth     = minTermWidth - 2
)

// panelWidthFor returns the panel width for a terminal width: the design
// width when it fits beside the 2-column left indent, the terminal less
// that indent when it doesn't, down to minPanelWidth, and the terminal less
// both margins when it's wider than 80, up to maxPanelWidth. A
// zero/negative termWidth means no WindowSizeMsg yet — use the design width.
func panelWidthFor(termWidth int) int {
	if termWidth <= 0 {
		return defaultPanelWidth
	}
	return max(min(termWidth-2, defaultPanelWidth), min(termWidth-4, maxPanelWidth), minPanelWidth)
}

// columnsWidthFor is panelWidthFor held at the design width, for the
// sections laid out in columns: the context gauge and the agent rows keep
// their layout on a wide terminal rather than spread it across the panel,
// so their costs stay in the cost rows' column.
func columnsWidthFor(termWidth int) int {
	return min(panelWidthFor(termWidth), defaultPanelWidth)
}

// viewportHeight returns the rows left for the scrollable viewport after the
// fixed header and footer, clamped to at least 1: a terminal shorter than the
// chrome (or one reporting 0 rows, as bare ptys do) must degrade to a
// squeezed layout, not hand the viewport a negative height — its line math
// panics on that.
func viewportHeight(termHeight, headerHeight, footerHeight int) int {
	return max(termHeight-headerHeight-footerHeight, 1)
}

// newViewport returns a live view's scrolling body. Its sideways step is
// zero: the body is clipped to the terminal before it gets here, so the
// keymap's h, l and arrow bindings have nothing to scroll to.
//
// Space pages down with shift held too. Bubble Tea asks the terminal to
// report modified keys apart from plain ones, and a terminal that does
// sends shift+space as a key of its own, which the keymap doesn't bind.
func newViewport(width, height int) viewport.Model {
	vp := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
	vp.SetHorizontalStep(0)
	vp.KeyMap.PageDown.SetKeys(append(vp.KeyMap.PageDown.Keys(), "shift+space")...)
	return vp
}

// clipToWidth truncates every line of rendered output to the terminal width
// (ANSI-aware) so overlong lines degrade by clipping instead of wrapping.
// Applied to viewport content, so a row ends where the terminal does and
// the viewport has nothing to cut, and to the final frame, where terminal
// hard-wrap would desynchronize the fixed header/footer layout.
func clipToWidth(frame string, termWidth int) string {
	if termWidth <= 0 {
		return frame
	}
	return lipgloss.NewStyle().MaxWidth(termWidth).Render(frame)
}

// View hands Bubble Tea the frame to draw on the alternate screen, and
// watch's terminal title, which it writes when the title changes.
func (m Model) View() tea.View {
	v := tea.NewView(m.frame())
	v.AltScreen = true
	v.WindowTitle = m.windowTitle
	return v
}

// frame renders the TUI
func (m Model) frame() string {
	if m.tooSmall() {
		return renderTooSmall(m.width, m.height, minTermWidth, minTermHeight)
	}
	if m.keysOpen {
		lines := append(keyList("watch", WatchKeys(), m.width, m.height-1, m.noColor, ""), helpLine(WatchKeys(), m.width, true, !m.followMode, m.noColor))
		return clipToWidth(strings.Join(lines, "\n"), m.width)
	}

	var sb strings.Builder
	panelWidth := panelWidthFor(m.width)

	// FIXED HEADER: the boxed panel, or one plain line when compact
	if m.compact() {
		sb.WriteString("  " + fitStatusHeader(m.headerParams(panelWidth), panelWidth-2))
	} else {
		sb.WriteString(m.renderHeaderPanel(panelWidth))
	}

	// Notify row, always reserved: errors, the last session switch, other
	// sessions' activity
	sb.WriteString("\n")
	if row := m.renderNotifyRow(panelWidth); row != "" {
		sb.WriteString("  " + row)
	}
	sb.WriteString("\n")

	// SCROLLABLE CONTENT (viewport)
	if m.ready {
		sb.WriteString(m.viewport.View())
	}
	sb.WriteString("\n")

	// FIXED FOOTER: rule, pinned stats, warning rows, help
	sb.WriteString(strings.Join(m.renderFooterLines(panelWidth), "\n"))

	return clipToWidth(sb.String(), m.width)
}

// renderTooSmall replaces a view's frame when no layout fits, naming the
// minimum the terminal misses. The lines stay short, and each leads with what
// matters, so the message survives clipping at 20 columns. A terminal
// shorter than the message keeps its top lines, so a frame is never taller
// than the terminal, here as in every other layout.
func renderTooSmall(width, height, minWidth, minHeight int) string {
	lines := []string{"terminal too small"}
	// A zero size isn't known yet; it isn't the one that's short.
	if width > 0 && width < minWidth {
		lines = append(lines, fmt.Sprintf("need %d cols", minWidth))
	}
	if height > 0 && height < minHeight {
		lines = append(lines, fmt.Sprintf("need %d rows", minHeight))
	}
	lines = append(lines, "q to quit")
	if height > 0 {
		lines = lines[:min(len(lines), height)]
	}
	return clipToWidth("  "+strings.Join(lines, "\n  "), width)
}

// renderAnalysis renders the live analysis body. Cache-write tokens are split by
// TTL in both modes; only the per-fragment styling varies with m.noColor.
func (m Model) renderAnalysis() string {
	body, _ := m.renderAnalysisSections()
	return body
}

// bodySection is where one of the body's sections starts: the blank line
// above its heading, which moves with it, or 0 for the totals at the top.
type bodySection struct {
	name string
	line int
}

// renderAnalysisSections is renderAnalysis, along with where each section
// starts in what it returns, so refreshContent can keep the reader's line
// when a section above it changes length.
func (m Model) renderAnalysisSections() (string, []bodySection) {
	// Handle empty session state
	if m.isEmptySession() {
		return m.renderEmptyState(), nil
	}

	var sb strings.Builder
	sections := []bodySection{{name: "totals"}}
	// begin marks a section starting at the next line written.
	begin := func(name string) {
		sections = append(sections, bodySection{name: name, line: strings.Count(sb.String(), "\n")})
	}
	a := m.analysis
	sectionWidth := panelWidthFor(m.width)

	// Hero total cost as section header
	sb.WriteString("\n")
	totalHighlighted := m.isHighlighted("total")
	sb.WriteString("  " + m.renderHeroCost(a.TotalCost.TotalCost, totalHighlighted, sectionWidth))
	sb.WriteString("\n")

	// Unified cost+token rows
	// Input tokens
	sb.WriteString(m.renderUnifiedCostRow(
		"Input", a.TotalCost.InputCost, a.TotalUsage.InputTokens,
		"input_cost", "input_tokens", nil, ""))

	// Output tokens
	sb.WriteString(m.renderUnifiedCostRow(
		"Output", a.TotalCost.OutputCost, a.TotalUsage.OutputTokens,
		"output_cost", "output_tokens", styles.OutputTokenColor, ""))

	// Cache write rows - split tokens by TTL when detailed breakdown available.
	// Each row reads its own per-TTL token delta so a write to one bucket can't
	// flash a delta on the other; tokenKey5m/tokenKey1h stay in
	// lockstep with detectChanges' keying via cacheWriteTokenKeys.
	cache5mTokens, cache1hTokens := render.CacheTokensByTTL(a.TotalUsage)
	tokenKey5m, tokenKey1h := cacheWriteTokenKeys(a.TotalUsage)
	has5mCost := a.TotalCost.CacheWrite5mCost > 0
	has1hCost := a.TotalCost.CacheWrite1hCost > 0

	if has5mCost {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache write", a.TotalCost.CacheWrite5mCost, cache5mTokens,
			"cache_write_5m", tokenKey5m, styles.CacheWriteTokenColor, "5m"))
	}

	if has1hCost {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache write", a.TotalCost.CacheWrite1hCost, cache1hTokens,
			"cache_write_1h", tokenKey1h, styles.CacheWriteTokenColor, "1h"))
	}

	// Cache read - only show if present
	if a.TotalCost.CacheReadCost > 0 || a.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache read", a.TotalCost.CacheReadCost, a.TotalUsage.CacheReadInputTokens,
			"cache_read", "cache_read_tokens", styles.CacheReadTokenColor, ""))
	}

	// Savings row (no separator - the rows above sum to hero TOTAL, not savings).
	// The plain form never highlights, but pads the value like the colored one.
	// A narrow terminal drops the note whole.
	if a.TotalCost.CacheSavings > 0 {
		note := "(from cache reads)"
		if m.width > 0 && m.costRowLead()+len(note) > m.width {
			note = ""
		}
		if m.noColor {
			sb.WriteString(strings.TrimRight(fmt.Sprintf("    %-14s %s  %s",
				"Savings", render.CostCell(a.TotalCost.CacheSavings, m.costCellWidth()), note), " ") + "\n")
		} else {
			savingsHighlighted := m.isHighlighted("savings")
			savingsStr := render.CostStyledGreen(a.TotalCost.CacheSavings, m.costCellWidth(), savingsHighlighted, m.noColor)
			line := "    " + styles.SavingsLabelStyle.Render(fmt.Sprintf("%-14s", "Savings")) + " " + savingsStr
			if note != "" {
				line += "  " + styles.DimStyle.Render(note)
			}
			sb.WriteString(line + "\n")
		}
	}

	if m.copies != nil {
		sb.WriteString(m.renderCopiesNote())
	}

	// Context window section
	begin("context")
	sb.WriteString("\n")
	sb.WriteString(m.renderContextSection())

	// Agent breakdown (shown when agents exist)
	if a.HasAgents {
		begin("agents")
		sb.WriteString("\n")
		sb.WriteString("  " + m.renderAgentHeader(sectionWidth))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderAgentBreakdownContent())
	}

	// Section: COST BY MODEL. A session of only synthetic lines has no model
	// with a cost, and a heading over nothing reads as a rendering fault.
	if len(a.CostByModel) > 0 {
		begin("models")
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("COST BY MODEL", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderCostByModelContent())
	}

	// Cost trend chart, from the first timestamped message on. A short
	// terminal can't spare its nine rows.
	if len(m.costHistory) > 0 && !m.compact() {
		begin("trend")
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("COST TREND", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderCostChart())
	}

	// Message insights (shown when insights are available)
	if a.Insights != nil {
		begin("insights")
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("MESSAGE INSIGHTS", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderInsightsContent())
	}

	// Note: Footer is rendered separately in View() after the separators

	return sb.String(), sections
}

// renderCopiesNote is the note under the cost rows on the messages the
// session's file shares with other sessions. It sits with the totals it
// qualifies, indented like their rows and wrapped to the columns on screen.
func (m Model) renderCopiesNote() string {
	width := columnsWidthFor(m.width)
	if m.width > 0 {
		width = min(width, m.width-2)
	}
	text := render.WrapNoRunt(render.CopiesNote(*m.copies), width-2)
	var sb strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if !m.noColor {
			line = styles.DimStyle.Render(line)
		}
		sb.WriteString("    " + line + "\n")
	}
	return sb.String()
}

// renderContextSection renders the context gauge sized to the panel, with
// its scope and headroom note under the bar.
func (m Model) renderContextSection() string {
	c := analyzer.ContextUsage(m.analysis.LastMessageUsage, m.analysis.LastMessageModel)
	if c == nil {
		return ""
	}

	// The panel's 2-column inner indent and the "Context " label come first;
	// the gauge right-aligns its percentage, so "Context  95%" and
	// "Context 100%" align.
	// The panel keeps a minimum width and clips below it; size the gauge to
	// the columns actually on screen so it drops fields instead.
	avail := columnsWidthFor(m.width)
	if m.width > 0 {
		avail = min(avail, m.width-2)
	}
	width := avail - 2 - 8
	var agent *render.ContextAgent
	if near := nearestAgent(m.analysis, c, m.clock()); near != nil {
		agent = &render.ContextAgent{ID: near.AgentID, Reading: *near.Context}
	}
	// Without agents there's no Main conversation row to name the model, so
	// the note does.
	line, note := render.ContextGauge(*c, m.analysis.LastMessageModel, !m.analysis.HasAgents, agent, width, m.noColor, m.isHighlighted("context_window"))
	return "    Context " + line + "\n             " + note + "\n"
}

// cacheWriteTokenKeys returns the highlight/delta map keys for the 5m and 1h
// cache-write rows. With the detailed bucket breakdown present each row gets its
// own per-TTL key so its delta describes only its own column; a detail-less
// legacy usage (all writes priced as 5m) has a single flat count and shares the
// flat key. Must mirror detectChanges' cache-write delta keying.
func cacheWriteTokenKeys(usage models.TokenUsage) (string, string) {
	if usage.CacheCreation != nil {
		return "cache_write_5m_tokens", "cache_write_1h_tokens"
	}
	return "cache_write_tokens", "cache_write_tokens"
}

// hasUnknownModel reports whether any model in a cost-by-model map was priced
// from the fallback table.
func hasUnknownModel(costByModel map[string]models.CostBreakdown) bool {
	for modelID := range costByModel {
		if !pricing.IsKnownModel(modelID) {
			return true
		}
	}
	return false
}

// renderCostByModelContent renders just the cost by model content (no header)
func (m Model) renderCostByModelContent() string {
	var sb strings.Builder

	// Highest-cost model first.
	costWidth := render.ModelCostWidth(m.analysis.CostByModel)
	for _, modelID := range render.OrderModelsByCost(m.analysis.CostByModel) {
		cost := m.analysis.CostByModel[modelID]
		modelName := pricing.GetModelDisplayName(modelID)
		highlighted := m.isHighlighted("model_" + modelID)

		// The TUI has no stderr to warn on (it owns the screen), so an unpriced
		// model is flagged inline; renderFooter explains the marker. Clamp
		// before appending so the marker survives a long raw ID.
		modelLabel := render.ClampModel(modelName, 12)
		if !pricing.IsKnownModel(modelID) {
			modelLabel = render.ClampModel(modelName, 11) + unknownModelMarker
		}

		// Apply model color to the label (reduced padding from 18 to 12)
		var labelStr string
		if !m.noColor {
			modelColor := styles.GetModelColor(modelName)
			labelStyle := lipgloss.NewStyle().Foreground(modelColor)
			labelStr = labelStyle.Render(fmt.Sprintf("%-12s", modelLabel))
		} else {
			labelStr = fmt.Sprintf("%-12s", modelLabel)
		}

		// Plain white costs - model tier colors already provide cost hierarchy
		costStr := render.CostStyled(cost.TotalCost, costWidth, highlighted, m.noColor)
		sb.WriteString(fmt.Sprintf("    %s %s\n", labelStr, costStr))
	}

	return sb.String()
}

// renderAgentBreakdownContent renders just the agent breakdown content (no header)
// Layout: [AN] task Model msgs ctx cost
// All rows align costs at column 47 (4 indent + 43 content), further right
// by agentTasks.extra when the rows carry tasks
// Example:
//
//	Main conversation                           $1.14
//	[Aa0b184d] Opus 4.5      14 msgs            $0.3588
//	Agents subtotal                             $5.11
func (m Model) renderAgentBreakdownContent() string {
	var sb strings.Builder
	a := m.analysis

	// Show parent session cost - right-aligned cost at column 47, or later
	// with tasks
	// Format: 4(indent) + 40(label) + 3(spaces) + cost = 47 chars before cost
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	// Uses plain white (not magnitude gradient) - model tier colors provide cost hierarchy
	// A terminal narrower than the rows takes the columns it lacks out of
	// the space before the cost, so the cost stays on screen.
	rows := agentRows(a, m.clock(), m.agentsOpen)
	tasks := m.agentTaskColumn(rows)
	labelWidth := 40 - m.agentShrink(tasks) + tasks.extra()
	parentHighlighted := m.isHighlighted("parent_cost")
	parentCostStr := render.CostStyled(a.ParentCost.TotalCost, 11, parentHighlighted, m.noColor)
	// The agent rows' model column is 11 columns past the label's start and
	// any task cell; MainRowLabel puts the model there when it can.
	label := render.MainRowLabel(a.LastMessageModel, 11+tasks.cell(), labelWidth, true, m.noColor)
	sb.WriteString("    " + label + "   " + parentCostStr + "\n")

	// Show each agent as [A<id>] model msgs ctx cost
	// Format: 2(indent) + 1(dot) + 1 + 10(marker) + 1 + 11(model) + 9(msgs) + 9(ctx) + 3(gap) + cost
	//       = 47 chars before cost (aligned with parent), plus any task cell
	// Uses plain white costs - model tier colors already provide cost hierarchy
	// Workflow agents are grouped after regular agents; a dim header line marks
	// each run's start. Markers carry the real agent ID (abbreviated), matching
	// the breakdown TUI's [A<id>] scheme. A running agent's live dot sits in
	// the indent, so the rows' columns don't move as agents start and stop.
	// When a heading doesn't fit, or its status is too long to show, every
	// heading drops its status before a name is cut, all or none, as show
	// does, so a heading without one can't read as a run that had none.
	statuses := true
	for _, row := range rows {
		if row.kind != agentRowRun {
			continue
		}
		meta := a.WorkflowByID(row.runID)
		if render.WorkflowStatusDropped(meta) || lipgloss.Width(styles.GroupRule+" "+render.WorkflowLabel(meta)) > labelWidth {
			statuses = false
		}
	}
	for _, row := range rows {
		switch row.kind {
		case agentRowRun:
			// The run's subtotal sits in the cost column, like show's, so a
			// workflow compares with the parent session at a glance. It's
			// dim like its heading while it repeats the rows below and isn't
			// part of the column's sum; a folded run's rows are gone, so
			// then it is.
			meta := a.WorkflowByID(row.runID)
			if !statuses {
				meta.Status = ""
			}
			heading := styles.GroupRule + " " + render.WorkflowLabel(meta)
			if lipgloss.Width(heading) > labelWidth {
				heading = withEllipsis(heading, labelWidth)
			}
			pad := strings.Repeat(" ", max(labelWidth-lipgloss.Width(heading)+3, 0))
			cost := workflowCost(a.Agents, row.runID)
			switch {
			case m.noColor:
				sb.WriteString("    " + heading + pad + render.CostCell(cost, 11) + "\n")
			case row.folded:
				sb.WriteString("    " + styles.DimStyle.Render(heading) + pad + render.CostStyled(cost, 11, false, false) + "\n")
			default:
				sb.WriteString("    " + styles.DimStyle.Render(heading) + pad + render.CostColored(cost, styles.SecondaryColor, 11) + "\n")
			}
		case agentRowFinished:
			// The fold has no task, so where the agents' count makes way for
			// theirs it keeps its own, in the room its task cell leaves. A
			// task cell too narrow for the label and the count leaves the
			// label alone, as the agents' rows are.
			text := render.Count(row.count) + " finished agents"
			labelWidth, noMsgs := 22+tasks.cell(), tasks.noMsgs
			if noMsgs && lipgloss.Width(text) <= labelWidth-agentMsgsWidth {
				labelWidth, noMsgs = labelWidth-agentMsgsWidth, false
			}
			label := fmt.Sprintf("%-*s", labelWidth, text)
			costStr := render.CostStyled(row.cost, 11, false, m.noColor)
			msgs, ctx, gap := m.agentColumns(row.msgs, nil, noMsgs, m.agentShrink(tasks))
			if m.noColor {
				sb.WriteString("    " + label + msgs + ctx + gap + costStr + "\n")
			} else {
				sb.WriteString("    " + styles.DimStyle.Render(label) + dimMsgs(msgs) + ctx + gap + costStr + "\n")
			}
		case agentRowAgent:
			sb.WriteString(m.renderAgentRow(row.agent, row.running, tasks))
		}
	}

	// Show agents subtotal in bold green (matches TotalValueStyle for visual hierarchy)
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	subtotalHighlighted := m.isHighlighted("agents_subtotal")
	subtotalStr := render.CostStyledBoldGreen(a.AgentsCost.TotalCost, 11, subtotalHighlighted, m.noColor)
	if !m.noColor {
		paddedSubtotal := fmt.Sprintf("%-*s", labelWidth, "Agents subtotal")
		sb.WriteString(fmt.Sprintf("    %s   %s\n", styles.DimStyle.Render(paddedSubtotal), subtotalStr))
	} else {
		sb.WriteString(fmt.Sprintf("    %-*s   %s\n", labelWidth, "Agents subtotal", subtotalStr))
	}

	return sb.String()
}

// agentShrink is how many columns the agent section's rows, 58 wide with no
// task, lack on the terminal.
func (m Model) agentShrink(tasks agentTasks) int {
	if m.width <= 0 {
		return 0
	}
	return max(4+40+3+11+tasks.extra()-m.width, 0)
}

// dimMsgs dims agentColumns' count, leaving its leading space plain.
func dimMsgs(msgs string) string {
	if msgs == "" {
		return ""
	}
	return " " + styles.DimStyle.Render(msgs[1:])
}

// agentTasks is the task column of watch's agent rows, as show draws it
// after the marker: width columns wide, 0 when no row has one, and taking
// the message count's place when noMsgs.
type agentTasks struct {
	width  int
	noMsgs bool
}

// cell is the columns the task takes in a row: its width and the space
// before it.
func (t agentTasks) cell() int {
	if t.width == 0 {
		return 0
	}
	return 1 + t.width
}

// extra is how far the task moves the cost column right: its cell, less the
// count it replaces.
func (t agentTasks) extra() int {
	if t.noMsgs {
		return t.cell() - agentMsgsWidth
	}
	return t.cell()
}

// agentMsgsWidth is the width of agentColumns' message count, " 22 msgs".
const agentMsgsWidth = 9

// agentTaskColumn fits the drawn agent rows' tasks into the room the panel
// has past the 58-column rows, as show does: beside the count when the
// widest fits whole, then in the count's place, cut to fit, and gone when
// that would cut it below render.MinTaskWidth, or below the count that an
// agent without a task shows there instead. So the task gives way after the
// count and before the reading and the cost. The rows stay inside the
// panel, as every row of watch does, so a long task is cut shorter than
// show cuts it on a wide terminal. The column takes all the room it has
// rather than the widest task's width, so the cost column moves once when
// the first task appears, not again with each new agent.
func (m Model) agentTaskColumn(rows []agentRow) agentTasks {
	want, countWant := 0, 0
	for _, row := range rows {
		if row.kind == agentRowAgent {
			want = max(want, lipgloss.Width(row.agent.TaskLabel()))
			if row.agent.TaskLabel() == "" {
				countWant = max(countWant, lipgloss.Width(render.MsgCount(row.agent.MessageCount)))
			}
		}
	}
	room := columnsWidthFor(m.width) - (4 + 40 + 3 + 11)
	switch {
	case want == 0:
		return agentTasks{}
	case 1+want <= room:
		return agentTasks{width: room - 1}
	case room+agentMsgsWidth-1 >= min(max(want, countWant), render.MinTaskWidth):
		return agentTasks{width: room + agentMsgsWidth - 1, noMsgs: true}
	default:
		return agentTasks{}
	}
}

// agentColumns are an agent row's message count, " 22 msgs" right-aligned in
// 9 columns, its context reading, " 38% ctx" in 9 more (blank for a nil c),
// and the gap before its cost: 3 spaces, less what a narrow terminal lacks.
// Where no gap is left the count goes, then the reading, so the cost stays.
// The count goes first because the reading says more about a running agent.
// The count also goes where noMsgs, for a task's room. shrink is
// agentShrink's for the section's rows.
func (m Model) agentColumns(n int, c *models.ContextUsage, noMsgs bool, shrink int) (msgs, ctx, gap string) {
	width := 3 - shrink
	msgs = fmt.Sprintf(" %8s", render.MsgCount(n))
	if noMsgs {
		msgs = ""
	}
	ctx = " " + render.AgentContextCell(c, m.noColor)
	if width < 1 && msgs != "" {
		msgs, width = "", width+len(msgs)
	}
	if width < 1 {
		ctx, width = "", width+1+render.AgentContextWidth
	}
	return msgs, ctx, strings.Repeat(" ", max(width, 1))
}

// workflowCost sums the cost of a workflow run's agents.
func workflowCost(agents []models.AgentAnalysis, runID string) float64 {
	var total float64
	for _, agent := range agents {
		if agent.WorkflowID == runID {
			total += agent.TotalCost.TotalCost
		}
	}
	return total
}

// renderAgentRow renders one agent's row: live dot, [A<id>] marker, task,
// model, message count, context reading and cost.
func (m Model) renderAgentRow(agent models.AgentAnalysis, running bool, tasks agentTasks) string {
	agentHighlighted := m.isHighlighted("agent_" + agent.AgentID)
	costStr := render.CostStyled(agent.TotalCost.TotalCost, 11, agentHighlighted, m.noColor)

	// [A<id>] carries the abbreviated real agent ID (%-10s fits [A1234567]),
	// so no separate ID column is needed
	marker := "[A" + render.ShortAgentID(agent.AgentID) + "]"

	// Get primary model for this agent
	modelName := render.PrimaryModel(agent.CostByModel)
	modelLabel := render.ClampModel(modelName, 11)

	dot := " "
	if running {
		dot = styles.LiveDot
	}

	msgs, ctx, gap := m.agentColumns(agent.MessageCount, agent.Context, tasks.noMsgs, m.agentShrink(tasks))
	task := ""
	if tasks.width > 0 {
		count := ""
		if tasks.noMsgs {
			count = render.MsgCount(agent.MessageCount)
		}
		task = render.TaskCell(agent.TaskLabel(), count, tasks.width, m.noColor) + " "
	}
	if m.noColor {
		return fmt.Sprintf("  %s %-10s %s%-11s%s%s%s%s\n",
			dot, marker, task, modelLabel, msgs, ctx, gap, costStr)
	}
	if running {
		dot = styles.LiveIndicatorStyle.Render(dot)
	}
	// Color the marker by hashing the full agent ID (matches breakdown)
	agentColor := styles.GetAgentColor(agent.AgentID)
	markerStyled := lipgloss.NewStyle().Foreground(agentColor).Render(fmt.Sprintf("%-10s", marker))

	// Color model name by tier
	modelColor := styles.GetModelColor(modelName)
	modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-11s", modelLabel))

	return fmt.Sprintf("  %s %s %s%s%s%s%s%s\n",
		dot, markerStyled, task, modelStyled, dimMsgs(msgs), ctx, gap, costStr)
}

// renderInsightsContent renders just the insights content (no header)
func (m Model) renderInsightsContent() string {
	var sb strings.Builder
	insights := m.analysis.Insights

	// When agents ran, these insights cover the main conversation alone (agents
	// appear in AGENT SUB-SESSIONS above); label the scope so it can't be
	// silently compared against the breakdown view's parent+agent insights. No
	// label without agents: parent-only and all-messages are then identical.
	if m.analysis.HasAgents {
		scope := "main conversation only, agents excluded"
		if m.width > 0 && 4+len(scope) > m.width {
			scope = "main conversation only"
		}
		if m.noColor {
			sb.WriteString("    " + scope + "\n")
		} else {
			sb.WriteString("    " + styles.DimStyle.Render(scope) + "\n")
		}
	}

	// Last message
	if insights.LastMessage != nil {
		last := insights.LastMessage
		componentLabel := render.CostComponentLabel(last.MainCostComponent)
		highlighted := m.isHighlighted("insights_last")

		if !m.noColor {
			labelStr := styles.DimStyle.Render(fmt.Sprintf("%-10s", "Last"))
			costStr := render.CostStyled(last.Cost, 10, highlighted, m.noColor)
			componentCostStr := formatCostStyledDim(last.MainCostValue)
			componentStr := styles.DimStyle.Render(componentLabel+":") + " " + componentCostStr
			sb.WriteString(m.fitTimedInsight("    "+labelStr+" "+costStr, last.Timestamp, componentStr))
		} else {
			sb.WriteString(m.fitTimedInsight(fmt.Sprintf("    %-10s %s", "Last", render.CostCell(last.Cost, 10)),
				last.Timestamp, componentLabel+": "+render.Cost(last.MainCostValue)))
		}
	}

	// Highest cost (only if notably above average)
	if insights.HighestCost != nil {
		highest := insights.HighestCost
		// The multiplier is information, not a warning: nearly every session
		// has a message well above its average.
		multiplierStr := fmt.Sprintf("%.1fx avg cost", insights.CostMultiplier())
		highlighted := m.isHighlighted("insights_highest")

		if !m.noColor {
			labelStr := styles.DimStyle.Render(fmt.Sprintf("%-10s", "Peak"))
			costStr := render.CostStyled(highest.Cost, 10, highlighted, m.noColor)
			sb.WriteString(m.fitTimedInsight("    "+labelStr+" "+costStr, highest.Timestamp, styles.DimStyle.Render(multiplierStr)))
		} else {
			sb.WriteString(m.fitTimedInsight(fmt.Sprintf("    %-10s %s", "Peak", render.CostCell(highest.Cost, 10)),
				highest.Timestamp, multiplierStr))
		}
	}

	// Trend (only once the analyzer actually computed one — see HasTrend)
	if insights.HasTrend() {
		trendDesc := insights.TrendDescription()
		trendSymbol := render.TrendSymbol(insights.CostTrend)
		highlighted := m.isHighlighted("insights_trend")

		recentStr := render.Cost(insights.RecentAvgCost) + "/msg"
		avgStr := render.Cost(insights.AverageCost) + "/msg"
		window := fmt.Sprintf("last %d", insights.TrendWindow)

		if !m.noColor {
			labelStr := styles.DimStyle.Render(fmt.Sprintf("%-10s", "Trend"))
			// Color the trend symbol and description based on direction
			var symbolStyled, descStyled string
			switch insights.CostTrend {
			case models.TrendIncreasing:
				symbolStyled = lipgloss.NewStyle().Foreground(styles.WarningColor).Render(trendSymbol)
				descStyled = lipgloss.NewStyle().Foreground(styles.WarningColor).Render(trendDesc)
			case models.TrendDecreasing:
				symbolStyled = lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(trendSymbol)
				descStyled = lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(trendDesc)
			default:
				symbolStyled = styles.DimStyle.Render(trendSymbol)
				descStyled = styles.DimStyle.Render(trendDesc)
			}
			if highlighted {
				recentStr = styles.HighlightStyle.Render(recentStr)
			}
			sb.WriteString(m.fitTrend("    "+labelStr+" "+window+" "+recentStr,
				" vs "+avgStr+" avg", "  "+symbolStyled, " "+descStyled))
		} else {
			sb.WriteString(m.fitTrend(fmt.Sprintf("    %-10s %s %s", "Trend", window, recentStr),
				" vs "+avgStr+" avg", "  "+trendSymbol, " "+trendDesc))
		}
	}

	return sb.String()
}

// fitTimedInsight is an insight row with a time, in the longest form that
// fits: lead, the time, then detail. The detail goes first, then a dated
// time takes its short form, so it shortens before it goes. The time is
// decided against the clock on every render, so a time from before
// midnight gains its weekday once midnight passes.
func (m Model) fitTimedInsight(lead string, t time.Time, detail string) string {
	style := func(clock string) string {
		clock = "(" + clock + ")"
		if !m.noColor {
			clock = styles.DimStyle.Render(clock)
		}
		return "  " + clock
	}
	long, short := style(render.ClockOn(t, m.clock())), style(render.ClockOnShort(t, m.clock()))
	return m.firstFit(lead+long+"  "+detail, lead+long, lead+short, lead) + "\n"
}

// fitTrend is the trend row in the longest form that fits. The direction
// is the row's point, so the comparison with the average goes before it.
func (m Model) fitTrend(lead, vs, symbol, desc string) string {
	return m.firstFit(lead+vs+symbol+desc, lead+vs+symbol, lead+symbol+desc, lead+symbol, lead) + "\n"
}

// firstFit is the first of forms, longest first, that fits the terminal,
// or the last.
func (m Model) firstFit(forms ...string) string {
	for _, f := range forms {
		if m.width <= 0 || lipgloss.Width(f) <= m.width {
			return f
		}
	}
	return forms[len(forms)-1]
}

// Panel and section rendering helpers

// renderHeaderPanel renders the mainframe-style header panel with session info
// Format:
// ╔══════════════════════════════════════════════════════════════════════════╗
// ║  Session: xxx (prev: yyy)  │  ● LIVE  │  Updated: HH:MM:SS              ║
// ╚══════════════════════════════════════════════════════════════════════════╝
func (m Model) renderHeaderPanel(width int) string {
	return renderLiveHeaderPanel(m.headerParams(width))
}

// headerParams fills the live header for the current frame.
func (m Model) headerParams(width int) liveHeaderParams {
	mode := "PINNED"
	if m.followMode {
		mode = "FOLLOWING"
	}
	var title string
	if m.analysis != nil {
		title = models.OneLine(m.analysis.Title)
	}
	return liveHeaderParams{
		sessionID:    m.sessionID,
		loading:      m.showLoading(),
		err:          m.err,
		spinnerView:  m.spinner.View(),
		noColor:      m.noColor,
		width:        width,
		project:      m.project,
		title:        title,
		mode:         mode,
		lastActivity: m.lastActivity,
		noMessages:   m.activityFromFile,
		waiting:      m.waiting(),
		blind:        m.followMode && m.sessionFallback.blind != nil,
		now:          m.clock(),
	}
}

// TUI-only formatting helpers, layered over the shared render.* helpers with
// live-view behavior (a fully dimmed cost, a value with a change delta).

// formatCostStyledDim returns a cost string entirely in dim style
// Used for secondary cost displays like component breakdowns in insights
func formatCostStyledDim(cost float64) string {
	return styles.DimStyle.Render(render.Cost(cost))
}

// formatDelta formats a token-count change as "(+2.7K)" or "(-1.0K)", or ""
// for no change.
func formatDelta(delta int64) string {
	switch {
	case delta > 0:
		return "(+" + render.Number(delta) + ")"
	case delta < 0:
		return "(-" + render.Number(-delta) + ")"
	}
	return ""
}

// renderHeroCost renders the total cost integrated into a section header
// Format: ─────────────────────[ $12.67 TOTAL ]─────────────────────
func (m Model) renderHeroCost(cost float64, highlighted bool, width int) string {
	costFull := render.Cost(cost)
	bracketedCost := "[ " + costFull + " TOTAL ]"
	costLen := len(bracketedCost)
	sideLen := (width - costLen) / 2
	if sideLen < 0 {
		sideLen = 0
	}
	rightLen := width - sideLen - costLen
	if rightLen < 0 {
		rightLen = 0
	}

	rule := styles.LineHorizontal
	leftLine := strings.Repeat(rule, sideLen)
	rightLine := strings.Repeat(rule, rightLen)

	if m.noColor {
		return leftLine + bracketedCost + rightLine
	}

	// A highlight covers the value only, not " TOTAL"
	costStyled := styles.HeroCostStyle.Render(costFull + " TOTAL")
	if highlighted {
		costStyled = styles.HighlightStyle.Render(costFull) + styles.HeroCostStyle.Render(" TOTAL")
	}

	return styles.DimStyle.Render(leftLine) + "[ " + costStyled + " ]" + styles.DimStyle.Render(rightLine)
}

// ttlColumnWidth is the width of the "  5m TTL" column after "tokens".
const ttlColumnWidth = 8

// Cost row columns: indent, label, cost, then the count and " tokens".
const (
	tokenCountWidth = 12
	tokenWord       = " tokens"
)

// costCellWidth is the cost rows' cost column: 11, or as wide as the widest
// amount, so a five-figure savings keeps its decimal point under the rest.
func (m Model) costCellWidth() int {
	if m.analysis == nil {
		return 11
	}
	c := m.analysis.TotalCost
	return render.CostCellWidth(11, c.InputCost, c.OutputCost, c.CacheWrite5mCost, c.CacheWrite1hCost, c.CacheReadCost, c.CacheSavings)
}

// costRowLead is the cost rows' width up to the token count: indent,
// label, cost and the gap after it.
func (m Model) costRowLead() int { return 4 + 14 + 1 + m.costCellWidth() + 2 }

// deltaColumnEnd is where the delta column starts, plus its two-space gap:
// indent, label, cost, count, " tokens", then the TTL column.
func (m Model) deltaColumnEnd() int {
	return m.costRowLead() + tokenCountWidth + len(tokenWord) + ttlColumnWidth + 2
}

// renderUnifiedCostRow renders a single row with cost and token info combined
// Format: "  Label          $0.371042     53.9K tokens"
// ttl ("5m", "1h") marks a cache-write row. A terminal too narrow for the
// whole row sheds whole pieces: the word "tokens", then the TTL column,
// whose TTL moves into the label ("Cache write 5m") so the two cache-write
// rows stay apart, then the count's padding.
func (m Model) renderUnifiedCostRow(label string, cost float64, tokens int64, costField, tokenField string, labelColor color.Color, ttl string) string {
	costHighlighted := m.isHighlighted(costField)
	tokenChanged := m.recentlyChanged(tokenField)
	tokenHighlighted := m.isHighlighted(tokenField)
	delta := m.deltaTokens[tokenField]

	count := render.Number(tokens)
	word, countWidth := tokenWord, tokenCountWidth
	ttlWidth := 0
	if ttl != "" {
		ttlWidth = ttlColumnWidth
	}
	if m.width > 0 {
		if m.costRowLead()+countWidth+len(word)+ttlWidth > m.width {
			word = ""
		}
		if ttl != "" && m.costRowLead()+countWidth+ttlWidth > m.width {
			label += " " + ttl
			ttl, ttlWidth = "", 0
		}
		countWidth = max(min(countWidth, m.width-m.costRowLead()), len(count))
	}

	// Tokens keep a fixed width; a recent change lights them up, and its
	// delta goes in a column of its own after the TTL so nothing to its
	// left moves while it shows. Where that column would fall off a narrow
	// terminal, the delta sits right after the count instead: a shift
	// beats a delta nobody can see. While it shows there, the TTL moves into
	// the label to make room, and where even that isn't enough the delta
	// goes: the count still lights up.
	var deltaStr string
	if tokenChanged && delta != 0 {
		deltaStr = formatDelta(delta)
		if tokenHighlighted {
			deltaStr = styles.HighlightStyle.Render(deltaStr)
		}
	}
	inline := deltaStr != "" && m.width > 0 && m.deltaColumnEnd()+lipgloss.Width(deltaStr) > m.width
	if inline {
		end := m.costRowLead() + countWidth + 1 + lipgloss.Width(deltaStr) + len(word)
		if ttl != "" && end+ttlWidth > m.width {
			label += " " + ttl
			ttl = ""
		}
		if end > m.width {
			deltaStr, inline = "", false
		}
	}
	// Format label with optional color
	var labelStr string
	if !m.noColor && labelColor != nil {
		labelStyle := lipgloss.NewStyle().Foreground(labelColor)
		labelStr = labelStyle.Render(fmt.Sprintf("%-14s", label))
	} else {
		labelStr = fmt.Sprintf("%-14s", label)
	}

	costStr := render.CostStyled(cost, m.costCellWidth(), costHighlighted, m.noColor)

	tokenStr := fmt.Sprintf("%*s", countWidth, count)
	if tokenHighlighted {
		tokenStr = styles.HighlightStyle.Render(tokenStr)
	}
	if inline {
		tokenStr += " " + deltaStr
	}

	// Add the TTL, padded so the delta column lines up
	extraStr := ""
	if ttl != "" {
		if !m.noColor {
			extraStr = "  " + styles.DimStyle.Render(ttl+" TTL")
		} else {
			extraStr = "  " + ttl + " TTL"
		}
	}
	if deltaStr != "" && !inline {
		extraStr += strings.Repeat(" ", max(ttlColumnWidth-lipgloss.Width(extraStr), 0))
		extraStr += "  " + deltaStr
	}

	return fmt.Sprintf("    %s %s  %s%s%s\n", labelStr, costStr, tokenStr, word, extraStr)
}

// renderEmptyState renders a clean empty state for new sessions
func (m Model) renderEmptyState() string {
	var sb strings.Builder
	sectionWidth := panelWidthFor(m.width)

	// Hero cost (even $0.00 to establish visual anchor), indented like the
	// full view's
	sb.WriteString("\n")
	sb.WriteString("  " + m.renderHeroCost(0, false, sectionWidth))
	sb.WriteString("\n\n")

	// Awaiting message, centered under the rule
	lines := []string{"Awaiting first message..."}
	if m.waiting() {
		lines = waitingLines(m.waitingIn, m.waitingHint, sectionWidth)
	}
	for _, line := range lines {
		pad := strings.Repeat(" ", 2+max((sectionWidth-lipgloss.Width(line))/2, 0))
		if !m.noColor {
			line = styles.DimStyle.Render(line)
		}
		sb.WriteString(pad + line + "\n")
	}

	return sb.String()
}

// isEmptySession checks if this is an empty/new session with no real data
func (m Model) isEmptySession() bool {
	if m.analysis == nil {
		return true
	}
	// Session is empty if total cost is 0 and no messages
	return m.analysis.TotalCost.TotalCost == 0 && m.analysis.MessageCount == 0
}

// renderAgentHeader is the AGENT SUB-SESSIONS rule with how many of the
// session's agents are running, since most of the list sits below the fold
// and a new agent's row appears there unseen. Running means what the list's
// dot means. A narrow rule drops the total and keeps the running count:
// whether anything is running at all is the question the header is there to
// answer, and the list gives the total. A session whose agent files were
// all unreadable has nothing to count, and the footer says why.
func (m Model) renderAgentHeader(width int) string {
	const name = "AGENT SUB-SESSIONS"
	total := len(m.analysis.Agents)
	if total == 0 {
		return render.SectionHeaderFit(width, m.noColor, name)
	}
	running := render.Count(runningAgentCount(m.analysis, m.clock()))
	sep := " " + styles.FieldSep + " "
	return render.SectionHeaderFit(width, m.noColor,
		name+sep+running+" of "+render.Count(total)+" running",
		name+sep+running+" running",
		name)
}

// renderCostChart renders the cost trend sparkline and its label: what a
// column is, how long the chart spans, the drawn window's peak minute, and
// that agents count when the session has any. The scale runs from zero, so
// a minimum would read $0 on any chart with an idle minute, and the label
// leaves it out.
func (m Model) renderCostChart() string {
	var sb strings.Builder

	chartView := m.costChart.View()
	if styles.ASCII() {
		chartView = styles.ASCIIChart(chartView)
	}
	for _, line := range strings.Split(chartView, "\n") {
		if line != "" {
			sb.WriteString("    " + line + "\n")
		}
	}

	// A narrow terminal shortens the scope, then drops the unit, then the
	// scope, then the span: whole pieces, never half a word. The scope goes
	// late because MESSAGE INSIGHTS below says it counts the main
	// conversation only, and without it the chart would seem to as well.
	// A session without agents has no scope to give: insights drops its
	// label then too, keyed on the same flag.
	sep := " " + styles.FieldSep + " "
	span := fmt.Sprintf("last %dm", len(m.costHistory))
	peak := "max " + render.Cost(maxOf(m.costHistory))
	const indent = "    "
	forms := []string{
		indent + "$ per minute, " + span + sep + peak,
		indent + span + sep + peak,
		indent + peak,
	}
	if m.analysis != nil && m.analysis.HasAgents {
		forms = []string{
			indent + "$ per minute, " + span + sep + peak + sep + "main conversation + agents",
			indent + "$ per minute, " + span + sep + peak + sep + "with agents",
			indent + span + sep + peak + sep + "with agents",
			indent + span + sep + peak,
			indent + peak,
		}
	}
	scaleInfo := strings.TrimPrefix(m.firstFit(forms...), indent)
	if !m.noColor {
		sb.WriteString("    " + styles.DimStyle.Render(scaleInfo) + "\n")
	} else {
		sb.WriteString("    " + scaleInfo + "\n")
	}

	return sb.String()
}
