package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// Panel sizing: panels, separators, and section headers are designed for
// defaultPanelWidth columns; on narrower terminals they shrink toward
// minPanelWidth so box-drawing lines don't wrap and desynchronize the fixed
// header/footer height math. minPanelWidth plus the 2-column indent is
// minTermWidth, so the whole frame fits at every width watch draws.
const (
	defaultPanelWidth = 76
	minPanelWidth     = minTermWidth - 2
)

// panelWidthFor returns the panel width for a terminal width: the design width
// when it fits (accounting for the 2-column left indent), otherwise clamped.
// A zero/negative termWidth means no WindowSizeMsg yet — use the design width.
func panelWidthFor(termWidth int) int {
	if termWidth <= 0 {
		return defaultPanelWidth
	}
	return max(min(termWidth-2, defaultPanelWidth), minPanelWidth)
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
		lines := append(keyList("watch", WatchKeys(), m.width, m.height-1, m.noColor), helpLine(WatchKeys(), m.width, true, !m.followMode, m.noColor))
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
	// Handle empty session state
	if m.isEmptySession() {
		return m.renderEmptyState()
	}

	var sb strings.Builder
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
		if m.width > 0 && costRowLead+len(note) > m.width {
			note = ""
		}
		if m.noColor {
			sb.WriteString(strings.TrimRight(fmt.Sprintf("    %-14s %s  %s",
				"Savings", render.CostCell(a.TotalCost.CacheSavings, 11), note), " ") + "\n")
		} else {
			savingsHighlighted := m.isHighlighted("savings")
			savingsStr := render.CostStyledGreen(a.TotalCost.CacheSavings, 11, savingsHighlighted, m.noColor)
			line := "    " + styles.SavingsLabelStyle.Render(fmt.Sprintf("%-14s", "Savings")) + " " + savingsStr
			if note != "" {
				line += "  " + styles.DimStyle.Render(note)
			}
			sb.WriteString(line + "\n")
		}
	}

	// Context window section
	sb.WriteString("\n")
	sb.WriteString(m.renderContextSection())

	// Cost trend chart (only show if we have 2+ data points). A short
	// terminal can't spare its nine rows.
	if len(m.costHistory) >= 2 && !m.compact() {
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("COST TREND", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderCostChart())
	}

	// Section: COST BY MODEL. A session of only synthetic lines has no model
	// with a cost, and a heading over nothing reads as a rendering fault.
	if len(a.CostByModel) > 0 {
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("COST BY MODEL", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderCostByModelContent())
	}

	// Agent breakdown (shown when agents exist)
	if a.HasAgents {
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("AGENT SUB-SESSIONS", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderAgentBreakdownContent())
	}

	// Message insights (shown when insights are available)
	if a.Insights != nil {
		sb.WriteString("\n")
		sb.WriteString("  " + render.SectionHeader("MESSAGE INSIGHTS", sectionWidth, m.noColor))
		sb.WriteString("\n\n")
		sb.WriteString(m.renderInsightsContent())
	}

	// Note: Footer is rendered separately in View() after the separators

	return sb.String()
}

// renderContextSection renders the context gauge sized to the panel, with
// its scope and headroom note under the bar.
func (m Model) renderContextSection() string {
	contextSize := m.analysis.LastMessageUsage.ContextWindowSize()
	if contextSize == 0 {
		return ""
	}
	maxContext := pricing.GetModelPricing(m.analysis.LastMessageModel).MaxContextTokens

	// The panel's 2-column inner indent and the "Context " label come first;
	// the gauge right-aligns its percentage, so "Context  95%" and
	// "Context 100%" align.
	// The panel keeps a minimum width and clips below it; size the gauge to
	// the columns actually on screen so it drops fields instead.
	avail := panelWidthFor(m.width)
	if m.width > 0 {
		avail = min(avail, m.width-2)
	}
	width := avail - 2 - 8
	line, note := render.ContextGauge(contextSize, maxContext, width, m.noColor, m.isHighlighted("context_window"))
	if !m.noColor {
		note = lipgloss.NewStyle().Foreground(styles.NoteColor).Render(note)
	}
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
		costStr := render.CostStyled(cost.TotalCost, 12, highlighted, m.noColor)
		sb.WriteString(fmt.Sprintf("    %s %s\n", labelStr, costStr))
	}

	return sb.String()
}

// renderAgentBreakdownContent renders just the agent breakdown content (no header)
// Layout: [AN] Model (ID) msgs cost
// All rows align costs at column 47 (4 indent + 43 content)
// Example:
//
//	Parent session                              $1.14
//	[Aa0b184d] Opus 4.5      14 msgs            $0.3588
//	Agents subtotal                             $5.11
func (m Model) renderAgentBreakdownContent() string {
	var sb strings.Builder
	a := m.analysis

	// Show parent session cost - right-aligned cost at column 47
	// Format: 4(indent) + 40(label) + 3(spaces) + cost = 47 chars before cost
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	// Uses plain white (not magnitude gradient) - model tier colors provide cost hierarchy
	// A terminal narrower than the rows takes the columns it lacks out of
	// the space before the cost, so the cost stays on screen.
	labelWidth := 40 - m.agentShrink()
	parentHighlighted := m.isHighlighted("parent_cost")
	parentCostStr := render.CostStyled(a.ParentCost.TotalCost, 11, parentHighlighted, m.noColor)
	if !m.noColor {
		paddedLabel := fmt.Sprintf("%-*s", labelWidth, "Parent session")
		sb.WriteString(fmt.Sprintf("    %s   %s\n", styles.DimStyle.Render(paddedLabel), parentCostStr))
	} else {
		sb.WriteString(fmt.Sprintf("    %-*s   %s\n", labelWidth, "Parent session", parentCostStr))
	}

	// Show each agent with [AN] Model (ID) msgs cost format
	// Format: 4(indent) + 5(marker) + 1 + 11(model) + 1 + 10(id) + 1 + 8(msgs) + 6(spaces) + cost
	//       = 4 + 37 + 6 = 47 chars before cost (aligned with parent)
	// Uses plain white costs - model tier colors already provide cost hierarchy
	// Workflow agents are grouped after regular agents; a dim header line marks
	// each run's start. Markers carry the real agent ID (abbreviated), matching
	// the breakdown TUI's [A<id>] scheme. A running agent's live dot sits in
	// the indent, so the rows' columns don't move as agents start and stop.
	// When a heading doesn't fit, every heading drops its status before a
	// name is cut, all or none, as show does, so a heading without one can't
	// read as a run that had none.
	rows := agentRows(a, m.clock())
	statuses := true
	for _, row := range rows {
		if row.kind == agentRowRun &&
			lipgloss.Width(styles.GroupRule+" "+render.WorkflowLabel(a.WorkflowByID(row.runID))) > labelWidth {
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
			pad := strings.Repeat(" ", labelWidth-lipgloss.Width(heading)+3)
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
			label := fmt.Sprintf("%-22s", fmt.Sprintf("%d finished agents", row.count))
			costStr := render.CostStyled(row.cost, 11, false, m.noColor)
			msgs, gap := m.agentMsgsColumn(row.msgs)
			if m.noColor {
				sb.WriteString("    " + label + msgs + gap + costStr + "\n")
			} else {
				sb.WriteString("    " + styles.DimStyle.Render(label) + dimMsgs(msgs) + gap + costStr + "\n")
			}
		case agentRowAgent:
			sb.WriteString(m.renderAgentRow(row.agent, row.running))
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

// agentShrink is how many columns the agent section's rows, 58 wide, lack
// on the terminal.
func (m Model) agentShrink() int {
	if m.width <= 0 {
		return 0
	}
	return max(4+40+3+11-m.width, 0)
}

// dimMsgs dims agentMsgsColumn's count, leaving its leading space plain.
func dimMsgs(msgs string) string {
	if msgs == "" {
		return ""
	}
	return " " + styles.DimStyle.Render(msgs[1:])
}

// agentMsgsColumn is an agent row's message count, " 22 msgs" right-aligned
// in 9 columns, and the gap before its cost: 12 spaces, less what a narrow
// terminal lacks. Where no gap is left the count goes, so the cost stays.
func (m Model) agentMsgsColumn(n int) (msgs, gap string) {
	width := 12 - m.agentShrink()
	msgs = fmt.Sprintf(" %8s", msgCount(n))
	if width < 1 {
		msgs, width = "", width+len(msgs)
	}
	return msgs, strings.Repeat(" ", max(width, 1))
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

// renderAgentRow renders one agent's row: live dot, [A<id>] marker, model,
// message count and cost.
func (m Model) renderAgentRow(agent models.AgentAnalysis, running bool) string {
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

	msgs, gap := m.agentMsgsColumn(agent.MessageCount)
	if m.noColor {
		return fmt.Sprintf("  %s %-10s %-11s%s%s%s\n",
			dot, marker, modelLabel, msgs, gap, costStr)
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

	return fmt.Sprintf("  %s %s %s%s%s%s\n",
		dot, markerStyled, modelStyled, dimMsgs(msgs), gap, costStr)
}

// msgCount is "1 msg" or "N msgs".
func msgCount(n int) string {
	if n == 1 {
		return "1 msg"
	}
	return fmt.Sprintf("%d msgs", n)
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
			timestampStr := styles.DimStyle.Render(fmt.Sprintf("(%s)", render.Clock(last.Timestamp)))
			componentCostStr := formatCostStyledDim(last.MainCostValue)
			componentStr := styles.DimStyle.Render(componentLabel+":") + " " + componentCostStr
			sb.WriteString(m.fitInsight("    "+labelStr+" "+costStr, timestampStr, componentStr))
		} else {
			sb.WriteString(m.fitInsight(fmt.Sprintf("    %-10s %s", "Last", render.CostCell(last.Cost, 10)),
				"("+render.Clock(last.Timestamp)+")",
				componentLabel+": "+render.Cost(last.MainCostValue)))
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
			timestampStr := styles.DimStyle.Render(fmt.Sprintf("(%s)", render.Clock(highest.Timestamp)))
			sb.WriteString(m.fitInsight("    "+labelStr+" "+costStr, timestampStr, styles.DimStyle.Render(multiplierStr)))
		} else {
			sb.WriteString(m.fitInsight(fmt.Sprintf("    %-10s %s", "Peak", render.CostCell(highest.Cost, 10)),
				"("+render.Clock(highest.Timestamp)+")",
				multiplierStr))
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

// fitInsight is an insight row: lead, then the rest two spaces apart.
func (m Model) fitInsight(lead string, rest ...string) string {
	for i := range rest {
		rest[i] = "  " + rest[i]
	}
	return m.fitRow(lead, rest...)
}

// fitRow ends a body row after the last of its pieces that fits the
// terminal, so a narrow one loses whole pieces from the right, never half a
// word. Each piece carries its own leading space.
func (m Model) fitRow(lead string, rest ...string) string {
	return joinSegments(append([]string{lead}, rest...), "", m.width) + "\n"
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
	return liveHeaderParams{
		sessionID:    m.sessionID,
		loading:      m.showLoading(),
		err:          m.err,
		spinnerView:  m.spinner.View(),
		noColor:      m.noColor,
		width:        width,
		project:      m.project,
		mode:         mode,
		lastActivity: m.lastActivity,
		noMessages:   m.activityFromFile,
		waiting:      m.waiting(),
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
	costRowLead     = 4 + 14 + 1 + 11 + 2
	tokenCountWidth = 12
	tokenWord       = " tokens"
)

// deltaColumnEnd is where the delta column starts, plus its two-space gap:
// indent, label, cost, count, " tokens", then the TTL column.
const deltaColumnEnd = costRowLead + tokenCountWidth + len(tokenWord) + ttlColumnWidth + 2

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
		if costRowLead+countWidth+len(word)+ttlWidth > m.width {
			word = ""
		}
		if ttl != "" && costRowLead+countWidth+ttlWidth > m.width {
			label += " " + ttl
			ttl, ttlWidth = "", 0
		}
		countWidth = max(min(countWidth, m.width-costRowLead), len(count))
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
	inline := deltaStr != "" && m.width > 0 && deltaColumnEnd+lipgloss.Width(deltaStr) > m.width
	if inline {
		end := costRowLead + countWidth + 1 + lipgloss.Width(deltaStr) + len(word)
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

	costStr := render.CostStyled(cost, 11, costHighlighted, m.noColor)

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
	msg := "Awaiting first message..."
	if m.waiting() {
		msg = "Waiting for a Claude Code session in " + m.waitingIn + styles.Ellipsis
		if lipgloss.Width(msg) > sectionWidth {
			msg = "Waiting for a Claude Code session" + styles.Ellipsis
		}
	}
	pad := strings.Repeat(" ", 2+max((sectionWidth-lipgloss.Width(msg))/2, 0))

	if m.noColor {
		sb.WriteString(pad + msg + "\n")
	} else {
		sb.WriteString(pad + styles.DimStyle.Render(msg) + "\n")
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

// renderCostChart renders the cost trend sparkline with labels. The min/max and
// count describe exactly the window the sparkline draws — its last chart-width
// points — so the labels can never advertise a peak that is off-screen. When
// the whole history fits the chart the count is a plain "(N msgs)"; once it
// overflows the label discloses the shown/total split as "(last N of M msgs)".
func (m Model) renderCostChart() string {
	var sb strings.Builder

	// Find min and max over the drawn window only
	visible := m.visibleCostHistory()
	var minCost, maxCost float64
	if len(visible) > 0 {
		minCost = visible[0]
		maxCost = visible[0]
		for _, cost := range visible {
			if cost < minCost {
				minCost = cost
			}
			if cost > maxCost {
				maxCost = cost
			}
		}
	}

	// Render the chart with proper indentation
	chartView := m.costChart.View()
	if styles.ASCII() {
		chartView = styles.ASCIIChart(chartView)
	}
	chartLines := strings.Split(chartView, "\n")
	for _, line := range chartLines {
		if line != "" {
			sb.WriteString("    " + line + "\n")
		}
	}

	// Count label: plain when nothing is truncated, shown/total when it is.
	// A narrow terminal gets the shorter forms, then no count, then no max:
	// whole pieces, never half a word.
	counts := []string{fmt.Sprintf("(%d msgs)", len(visible))}
	if len(visible) < len(m.costHistory) {
		counts = []string{
			fmt.Sprintf("(last %d of %d msgs)", len(visible), len(m.costHistory)),
			fmt.Sprintf("(last %d msgs)", len(visible)),
		}
	}
	const indent = "    "
	scale := indent + "min: " + render.Cost(minCost)
	var forms []string
	for _, c := range counts {
		forms = append(forms, scale+"  max: "+render.Cost(maxCost)+"  "+c)
	}
	forms = append(forms, scale+"  max: "+render.Cost(maxCost), scale)
	scaleInfo := strings.TrimPrefix(m.firstFit(forms...), indent)
	if !m.noColor {
		sb.WriteString("    " + styles.DimStyle.Render(scaleInfo) + "\n")
	} else {
		sb.WriteString("    " + scaleInfo + "\n")
	}

	return sb.String()
}
