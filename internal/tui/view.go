package tui

import (
	"fmt"
	"strings"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
)

// Panel sizing: panels, separators, and section headers are designed for
// defaultPanelWidth columns; on narrower terminals they shrink toward
// minPanelWidth so box-drawing lines don't wrap and desynchronize the fixed
// header/footer height math. Below minPanelWidth (plus indent) the frame is
// clipped by View's MaxWidth instead.
const (
	defaultPanelWidth = 76
	minPanelWidth     = 40
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

// clipToWidth truncates every line of rendered output to the terminal width
// (ANSI-aware) so overlong lines degrade by clipping instead of wrapping.
// Applied to viewport content — the viewport soft-wraps overlong lines, which
// inflates line counts — and to the final frame, where terminal hard-wrap
// would desynchronize the fixed header/footer layout.
func clipToWidth(frame string, termWidth int) string {
	if termWidth <= 0 {
		return frame
	}
	return lipgloss.NewStyle().MaxWidth(termWidth).Render(frame)
}

// View renders the TUI
func (m Model) View() string {
	if m.tooSmall() {
		return m.renderTooSmall()
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

// renderTooSmall replaces the frame when no layout fits.
func (m Model) renderTooSmall() string {
	// Short lines, so the message itself survives a tiny terminal.
	var short []string
	if m.width < minTermWidth {
		short = append(short, fmt.Sprintf("%d columns, need %d", m.width, minTermWidth))
	}
	if m.height < minTermHeight {
		short = append(short, fmt.Sprintf("%d rows, need %d", m.height, minTermHeight))
	}
	lines := append([]string{"terminal too small"}, short...)
	lines = append(lines, "q to quit")
	return clipToWidth("  "+strings.Join(lines, "\n  "), m.width)
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
		"input_cost", "input_tokens", lipgloss.Color(""), ""))

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
			"cache_write_5m", tokenKey5m, styles.CacheWriteTokenColor, "5m TTL"))
	}

	if has1hCost {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache write", a.TotalCost.CacheWrite1hCost, cache1hTokens,
			"cache_write_1h", tokenKey1h, styles.CacheWriteTokenColor, "1h TTL"))
	}

	// Cache read - only show if present
	if a.TotalCost.CacheReadCost > 0 || a.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache read", a.TotalCost.CacheReadCost, a.TotalUsage.CacheReadInputTokens,
			"cache_read", "cache_read_tokens", styles.CacheReadTokenColor, ""))
	}

	// Savings row (no separator - the rows above sum to hero TOTAL, not savings).
	// The plain form never highlights, but pads the value like the colored one.
	if a.TotalCost.CacheSavings > 0 {
		if m.noColor {
			sb.WriteString(fmt.Sprintf("    %-14s %s  (from cache reads)\n",
				"Savings", render.CostCell(a.TotalCost.CacheSavings, 11)))
		} else {
			savingsHighlighted := m.isHighlighted("savings")
			savingsStr := render.CostStyledGreen(a.TotalCost.CacheSavings, 11, savingsHighlighted, m.noColor)
			sb.WriteString(fmt.Sprintf("    %s %s  %s\n",
				savingsLabelStyle.Render(fmt.Sprintf("%-14s", "Savings")),
				savingsStr,
				dimStyle.Render("(from cache reads)")))
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

	// Section: COST BY MODEL
	sb.WriteString("\n")
	sb.WriteString("  " + render.SectionHeader("COST BY MODEL", sectionWidth, m.noColor))
	sb.WriteString("\n\n")
	sb.WriteString(m.renderCostByModelContent())

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
	// The panel keeps a 40-column minimum and clips below it; size the gauge
	// to the columns actually on screen so it drops fields instead.
	avail := panelWidthFor(m.width)
	if m.width > 0 {
		avail = min(avail, m.width-2)
	}
	width := avail - 2 - 8
	line, note := render.ContextGauge(contextSize, maxContext, width, m.noColor, m.isHighlighted("context_window"))
	if !m.noColor {
		note = lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(note)
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
	parentHighlighted := m.isHighlighted("parent_cost")
	parentCostStr := render.CostStyled(a.ParentCost.TotalCost, 11, parentHighlighted, m.noColor)
	if !m.noColor {
		paddedLabel := fmt.Sprintf("%-40s", "Parent session")
		sb.WriteString(fmt.Sprintf("    %s   %s\n", dimStyle.Render(paddedLabel), parentCostStr))
	} else {
		sb.WriteString(fmt.Sprintf("    %-40s   %s\n", "Parent session", parentCostStr))
	}

	// Show each agent with [AN] Model (ID) msgs cost format
	// Format: 4(indent) + 5(marker) + 1 + 11(model) + 1 + 10(id) + 1 + 8(msgs) + 6(spaces) + cost
	//       = 4 + 37 + 6 = 47 chars before cost (aligned with parent)
	// Uses plain white costs - model tier colors already provide cost hierarchy
	// Workflow agents are grouped after regular agents; a dim header line marks
	// each run's start. Markers carry the real agent ID (abbreviated), matching
	// the breakdown TUI's [A<id>] scheme.
	prevWorkflow := ""
	for _, agent := range a.Agents {
		if agent.WorkflowID != prevWorkflow {
			prevWorkflow = agent.WorkflowID
			if agent.WorkflowID != "" {
				// The run's subtotal sits in the cost column, like show's, so a
				// workflow compares with the parent session at a glance. It's
				// dim like its heading: it repeats the rows below and isn't
				// part of the column's sum.
				heading := styles.GroupRule + " " + render.WorkflowLabel(a.WorkflowByID(agent.WorkflowID))
				if lipgloss.Width(heading) > 40 {
					heading = withEllipsis(heading, 40)
				}
				pad := strings.Repeat(" ", 40-lipgloss.Width(heading)+3)
				cost := workflowCost(a.Agents, agent.WorkflowID)
				if m.noColor {
					sb.WriteString("    " + heading + pad + render.CostCell(cost, 11) + "\n")
				} else {
					sb.WriteString("    " + dimStyle.Render(heading) + pad + render.CostColored(cost, styles.SecondaryColor, 11) + "\n")
				}
			}
		}
		agentHighlighted := m.isHighlighted("agent_" + agent.AgentID)
		costStr := render.CostStyled(agent.TotalCost.TotalCost, 11, agentHighlighted, m.noColor)

		// [A<id>] carries the abbreviated real agent ID (%-10s fits [A1234567]),
		// so no separate ID column is needed
		marker := "[A" + render.ShortAgentID(agent.AgentID) + "]"

		// Get primary model for this agent
		modelName := render.PrimaryModel(agent.CostByModel)
		modelLabel := render.ClampModel(modelName, 11)

		// Format message count with singular/plural
		msgStr := fmt.Sprintf("%d msgs", agent.MessageCount)
		if agent.MessageCount == 1 {
			msgStr = "1 msg"
		}

		if !m.noColor {
			// Color the marker by hashing the full agent ID (matches breakdown)
			agentColor := styles.GetAgentColor(agent.AgentID)
			markerStyled := lipgloss.NewStyle().Foreground(agentColor).Render(fmt.Sprintf("%-10s", marker))

			// Color model name by tier
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-11s", modelLabel))

			// Dim the message count
			msgStyled := dimStyle.Render(fmt.Sprintf("%8s", msgStr))

			sb.WriteString(fmt.Sprintf("    %s %s %s            %s\n",
				markerStyled, modelStyled, msgStyled, costStr))
		} else {
			sb.WriteString(fmt.Sprintf("    %-10s %-11s %8s            %s\n",
				marker, modelLabel, msgStr, costStr))
		}
	}

	// Show agents subtotal in bold green (matches TotalValueStyle for visual hierarchy)
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	subtotalHighlighted := m.isHighlighted("agents_subtotal")
	subtotalStr := render.CostStyledBoldGreen(a.AgentsCost.TotalCost, 11, subtotalHighlighted, m.noColor)
	if !m.noColor {
		paddedSubtotal := fmt.Sprintf("%-40s", "Agents subtotal")
		sb.WriteString(fmt.Sprintf("    %s   %s\n", dimStyle.Render(paddedSubtotal), subtotalStr))
	} else {
		sb.WriteString(fmt.Sprintf("    %-40s   %s\n", "Agents subtotal", subtotalStr))
	}

	return sb.String()
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

// renderInsightsContent renders just the insights content (no header)
func (m Model) renderInsightsContent() string {
	var sb strings.Builder
	insights := m.analysis.Insights

	// When agents ran, these insights cover the main conversation alone (agents
	// appear in AGENT SUB-SESSIONS above); label the scope so it can't be
	// silently compared against the breakdown view's parent+agent insights. No
	// label without agents: parent-only and all-messages are then identical.
	if m.analysis.HasAgents {
		const scope = "main conversation only, agents excluded"
		if m.noColor {
			sb.WriteString("    " + scope + "\n")
		} else {
			sb.WriteString("    " + dimStyle.Render(scope) + "\n")
		}
	}

	// Last message
	if insights.LastMessage != nil {
		last := insights.LastMessage
		componentLabel := render.CostComponentLabel(last.MainCostComponent)
		highlighted := m.isHighlighted("insights_last")

		if !m.noColor {
			labelStr := dimStyle.Render(fmt.Sprintf("%-10s", "Last"))
			costStr := render.CostStyled(last.Cost, 10, highlighted, m.noColor)
			timestampStr := dimStyle.Render(fmt.Sprintf("(%s)", render.Clock(last.Timestamp)))
			componentCostStr := formatCostStyledDim(last.MainCostValue)
			componentStr := dimStyle.Render(componentLabel+":") + " " + componentCostStr
			sb.WriteString(fmt.Sprintf("    %s %s  %s  %s\n",
				labelStr,
				costStr,
				timestampStr,
				componentStr))
		} else {
			sb.WriteString(fmt.Sprintf("    %-10s %s  (%s)  %s: %s\n",
				"Last",
				render.CostCell(last.Cost, 10),
				render.Clock(last.Timestamp),
				componentLabel,
				render.Cost(last.MainCostValue)))
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
			labelStr := dimStyle.Render(fmt.Sprintf("%-10s", "Peak"))
			costStr := render.CostStyled(highest.Cost, 10, highlighted, m.noColor)
			timestampStr := dimStyle.Render(fmt.Sprintf("(%s)", render.Clock(highest.Timestamp)))
			sb.WriteString(fmt.Sprintf("    %s %s  %s  %s\n",
				labelStr,
				costStr,
				timestampStr,
				dimStyle.Render(multiplierStr)))
		} else {
			sb.WriteString(fmt.Sprintf("    %-10s %s  (%s)  %s\n",
				"Peak",
				render.CostCell(highest.Cost, 10),
				render.Clock(highest.Timestamp),
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
			labelStr := dimStyle.Render(fmt.Sprintf("%-10s", "Trend"))
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
				symbolStyled = dimStyle.Render(trendSymbol)
				descStyled = dimStyle.Render(trendDesc)
			}
			if highlighted {
				recentStr = highlightStyle.Render(recentStr)
			}
			trendLine := window + " " + recentStr + " vs " + avgStr + " avg"
			sb.WriteString(fmt.Sprintf("    %s %s  %s %s\n",
				labelStr,
				trendLine,
				symbolStyled,
				descStyled))
		} else {
			sb.WriteString(fmt.Sprintf("    %-10s %s %s vs %s avg  %s %s\n",
				"Trend",
				window,
				recentStr,
				avgStr,
				trendSymbol,
				trendDesc))
		}
	}

	return sb.String()
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
	return dimStyle.Render(render.Cost(cost))
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
	costStyled := heroCostStyle.Render(costFull + " TOTAL")
	if highlighted {
		costStyled = highlightStyle.Render(costFull) + heroCostStyle.Render(" TOTAL")
	}

	return dimStyle.Render(leftLine) + "[ " + costStyled + " ]" + dimStyle.Render(rightLine)
}

// ttlColumnWidth is the width of the "  5m TTL" column after "tokens".
const ttlColumnWidth = 8

// deltaColumnEnd is where the delta column starts, plus its two-space gap:
// indent, label, cost, count, " tokens", then the TTL column.
const deltaColumnEnd = 4 + 14 + 1 + 11 + 2 + 12 + 7 + ttlColumnWidth + 2

// renderUnifiedCostRow renders a single row with cost and token info combined
// Format: "  Label          $0.371042     53.9K tokens"
func (m Model) renderUnifiedCostRow(label string, cost float64, tokens int64, costField, tokenField string, labelColor lipgloss.Color, extra string) string {
	costHighlighted := m.isHighlighted(costField)
	tokenChanged := m.recentlyChanged(tokenField)
	tokenHighlighted := m.isHighlighted(tokenField)
	delta := m.deltaTokens[tokenField]

	// Format label with optional color
	var labelStr string
	if !m.noColor && labelColor != "" {
		labelStyle := lipgloss.NewStyle().Foreground(labelColor)
		labelStr = labelStyle.Render(fmt.Sprintf("%-14s", label))
	} else {
		labelStr = fmt.Sprintf("%-14s", label)
	}

	costStr := render.CostStyled(cost, 11, costHighlighted, m.noColor)

	// Tokens keep a fixed width; a recent change lights them up, and its
	// delta goes in a column of its own after the TTL so nothing to its
	// left moves while it shows. Where that column would fall off a narrow
	// terminal, the delta sits right after the count instead: a shift
	// beats a delta nobody can see.
	var deltaStr string
	if tokenChanged && delta != 0 {
		deltaStr = formatDelta(delta)
		if tokenHighlighted {
			deltaStr = highlightStyle.Render(deltaStr)
		}
	}
	inline := deltaStr != "" && m.width > 0 && deltaColumnEnd+lipgloss.Width(deltaStr) > m.width
	tokenStr := fmt.Sprintf("%12s", render.Number(tokens))
	if tokenHighlighted {
		tokenStr = highlightStyle.Render(tokenStr)
	}
	if inline {
		tokenStr += " " + deltaStr
	}

	// Add extra info (like TTL), padded so the delta column lines up
	extraStr := ""
	if extra != "" {
		if !m.noColor {
			extraStr = "  " + dimStyle.Render(extra)
		} else {
			extraStr = "  " + extra
		}
	}
	if deltaStr != "" && !inline {
		extraStr += strings.Repeat(" ", max(ttlColumnWidth-lipgloss.Width(extraStr), 0))
		extraStr += "  " + deltaStr
	}

	return fmt.Sprintf("    %s %s  %s tokens%s\n", labelStr, costStr, tokenStr, extraStr)
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
		sb.WriteString(pad + dimStyle.Render(msg) + "\n")
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

	// Count label: plain when nothing is truncated, shown/total when it is
	var countInfo string
	if len(visible) < len(m.costHistory) {
		countInfo = fmt.Sprintf("(last %d of %d msgs)", len(visible), len(m.costHistory))
	} else {
		countInfo = fmt.Sprintf("(%d msgs)", len(visible))
	}

	// Add scale labels below the chart
	scaleInfo := fmt.Sprintf("min: %s  max: %s  %s", render.Cost(minCost), render.Cost(maxCost), countInfo)
	if !m.noColor {
		sb.WriteString("    " + dimStyle.Render(scaleInfo) + "\n")
	} else {
		sb.WriteString("    " + scaleInfo + "\n")
	}

	return sb.String()
}
