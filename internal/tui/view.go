package tui

import (
	"fmt"
	"strings"
	"time"

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
	var sb strings.Builder
	panelWidth := panelWidthFor(m.width)

	// FIXED HEADER (4 lines)
	sb.WriteString(m.renderHeaderPanel(panelWidth))

	// Show switch notification on next line if applicable
	showSwitchNotify := !m.switchNotifyAt.IsZero() && time.Since(m.switchNotifyAt) < switchNotifyDuration
	if showSwitchNotify {
		sb.WriteString("\n")
		if m.noColor {
			sb.WriteString("  [Switched to new session]")
		} else {
			sb.WriteString("  " + lipgloss.NewStyle().Foreground(styles.HighlightColor).Bold(true).Render("Switched to new session"))
		}
	}
	sb.WriteString("\n")

	// SCROLLABLE CONTENT (viewport)
	if m.ready {
		sb.WriteString(m.viewport.View())
	} else if m.loading {
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// FIXED FOOTER: rule, pinned stats, warning rows, help
	sb.WriteString(strings.Join(m.renderFooterLines(panelWidth), "\n"))

	return clipToWidth(sb.String(), m.width)
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
	sb.WriteString("\n\n")

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

	// Cost trend chart (only show if we have 2+ data points)
	if len(m.costHistory) >= 2 {
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

// renderContextSection renders the context window progress bar and stats
func (m Model) renderContextSection() string {
	var sb strings.Builder

	contextSize := m.analysis.LastMessageUsage.ContextWindowSize()
	if contextSize == 0 {
		return ""
	}

	modelPricing := pricing.GetModelPricing(m.analysis.LastMessageModel)
	maxContext := modelPricing.MaxContextTokens
	contextPct := pricing.GetContextPercentage(modelPricing, contextSize)
	freeSpace := pricing.GetFreeSpace(modelPricing, contextSize)
	freePct := float64(freeSpace) / float64(maxContext) * 100

	highlighted := m.isHighlighted("context_window")
	usageColor := styles.GetContextUsageColor(contextPct)

	// Context label with value
	contextVal := render.Number(contextSize)
	contextMeta := fmt.Sprintf("(%.0f%% of %s)", contextPct, render.Number(int64(maxContext)))

	if m.noColor {
		sb.WriteString(fmt.Sprintf("    Context  %s  %s %s\n", render.ContextBar(contextSize, freeSpace, maxContext, true), contextVal, contextMeta))
		sb.WriteString(fmt.Sprintf("             Free: %s (%.1f%%)\n", render.Number(freeSpace), freePct))
	} else {
		// Progress bar with context info
		if highlighted {
			sb.WriteString(fmt.Sprintf("    Context  %s  %s %s\n",
				render.ContextBar(contextSize, freeSpace, maxContext, false),
				highlightStyle.Render(contextVal),
				dimStyle.Render(contextMeta)))
		} else {
			coloredMeta := lipgloss.NewStyle().Foreground(usageColor).Render(contextMeta)
			sb.WriteString(fmt.Sprintf("    Context  %s  %s %s\n",
				render.ContextBar(contextSize, freeSpace, maxContext, false),
				contextVal, coloredMeta))
		}

		// Free space info
		freeVal := render.Number(freeSpace)
		freeValWithPct := fmt.Sprintf("%s (%.1f%%)", freeVal, freePct)
		var freeStyled string
		if highlighted {
			freeStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render("Free: ") + highlightStyle.Render(freeValWithPct)
		} else {
			freeStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(fmt.Sprintf("Free: %s", freeValWithPct))
		}
		sb.WriteString(fmt.Sprintf("             %s\n", freeStyled))
	}

	return sb.String()
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
				label := render.WorkflowLabel(a.WorkflowByID(agent.WorkflowID))
				if m.noColor {
					sb.WriteString("    " + styles.GroupRule + " " + label + "\n")
				} else {
					sb.WriteString("    " + dimStyle.Render(styles.GroupRule+" "+label) + "\n")
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

// renderInsightsContent renders just the insights content (no header)
func (m Model) renderInsightsContent() string {
	var sb strings.Builder
	insights := m.analysis.Insights

	// When agents ran, these insights cover the parent transcript alone (agents
	// appear in AGENT SUB-SESSIONS above); label the scope so it can't be
	// silently compared against the breakdown view's parent+agent insights. No
	// label without agents: parent-only and all-messages are then identical.
	if m.analysis.HasAgents {
		const scope = "scope: parent transcript"
		if m.noColor {
			sb.WriteString("    " + scope + "\n")
		} else {
			sb.WriteString("    " + dimStyle.Render(scope) + "\n")
		}
	}

	// First message
	if insights.FirstMessage != nil {
		first := insights.FirstMessage
		componentLabel := render.CostComponentLabel(first.MainCostComponent)
		highlighted := m.isHighlighted("insights_first")

		if !m.noColor {
			labelStr := dimStyle.Render(fmt.Sprintf("%-10s", "First"))
			costStr := render.CostStyled(first.Cost, 10, highlighted, m.noColor)
			timestampStr := dimStyle.Render(fmt.Sprintf("(%s)", render.Clock(first.Timestamp)))
			componentCostStr := formatCostStyledDim(first.MainCostValue)
			componentStr := dimStyle.Render(componentLabel+":") + " " + componentCostStr
			sb.WriteString(fmt.Sprintf("    %s %s  %s  %s\n",
				labelStr,
				costStr,
				timestampStr,
				componentStr))
		} else {
			sb.WriteString(fmt.Sprintf("    %-10s %s  (%s)  %s: %s\n",
				"First",
				render.CostCell(first.Cost, 10),
				render.Clock(first.Timestamp),
				componentLabel,
				render.Cost(first.MainCostValue)))
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
		multiplier := insights.CostMultiplier()
		warningStr := fmt.Sprintf("%.1fx avg cost", multiplier)
		highlighted := m.isHighlighted("insights_highest")

		if !m.noColor {
			labelStr := dimStyle.Render(fmt.Sprintf("%-10s", "Peak"))
			costStr := render.CostStyled(highest.Cost, 10, highlighted, m.noColor)
			timestampStr := dimStyle.Render(fmt.Sprintf("(%s)", render.Clock(highest.Timestamp)))
			warningStyled := lipgloss.NewStyle().Foreground(styles.WarningColor).Render(styles.Warning + " " + warningStr)
			sb.WriteString(fmt.Sprintf("    %s %s  %s  %s\n",
				labelStr,
				costStr,
				timestampStr,
				warningStyled))
		} else {
			sb.WriteString(fmt.Sprintf("    %-10s %s  (%s)  %s %s\n",
				"Peak",
				render.CostCell(highest.Cost, 10),
				render.Clock(highest.Timestamp),
				styles.Warning,
				warningStr))
		}
	}

	// Trend (only once the analyzer actually computed one — see HasTrend)
	if insights.HasTrend() {
		trendDesc := insights.TrendDescription()
		trendSymbol := render.TrendSymbol(insights.CostTrend)
		highlighted := m.isHighlighted("insights_trend")

		earlyStr := render.Cost(insights.EarlyAvgCost) + "/msg"
		lateStr := render.Cost(insights.LateAvgCost) + "/msg"

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
			var trendLine string
			if highlighted {
				// Highlight only the cost values, not the arrow
				trendLine = highlightStyle.Render(earlyStr) + " " + styles.Arrow + " " + highlightStyle.Render(lateStr)
			} else {
				trendLine = earlyStr + " " + styles.Arrow + " " + lateStr
			}
			sb.WriteString(fmt.Sprintf("    %s %s  %s %s\n",
				labelStr,
				trendLine,
				symbolStyled,
				descStyled))
		} else {
			sb.WriteString(fmt.Sprintf("    %-10s %s %s %s  %s %s\n",
				"Trend",
				earlyStr,
				styles.Arrow,
				lateStr,
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
	return renderLiveHeaderPanel(liveHeaderParams{
		sessionID:     m.sessionID,
		prevSessionID: m.prevSessionID,
		loading:       m.loading,
		err:           m.err,
		lastUpdated:   m.lastUpdated,
		spinnerView:   m.spinner.View(),
		noColor:       m.noColor,
		width:         width,
	})
}

// TUI-only formatting helpers, layered over the shared render.* helpers with
// live-view behavior (a fully dimmed cost, a value with a change delta).

// formatCostStyledDim returns a cost string entirely in dim style
// Used for secondary cost displays like component breakdowns in insights
func formatCostStyledDim(cost float64) string {
	return dimStyle.Render(render.Cost(cost))
}

// formatNumberWithDelta formats a number with optional delta during highlight
func formatNumberWithDelta(n int64, delta int64, showDelta bool) string {
	valStr := render.Number(n)
	if !showDelta || delta == 0 {
		return fmt.Sprintf("%12s", valStr)
	}

	// Format delta
	var deltaStr string
	if delta > 0 {
		deltaStr = fmt.Sprintf("(+%s)", render.Number(delta))
	} else {
		deltaStr = fmt.Sprintf("(-%s)", render.Number(-delta))
	}

	return fmt.Sprintf("%12s %s", valStr, deltaStr)
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

	// Format tokens
	var tokenStr string
	if tokenChanged {
		tokenStr = formatNumberWithDelta(tokens, delta, true)
		if tokenHighlighted {
			tokenStr = highlightStyle.Render(tokenStr)
		}
	} else {
		tokenStr = fmt.Sprintf("%12s", render.Number(tokens))
	}

	// Add extra info (like TTL)
	extraStr := ""
	if extra != "" {
		if !m.noColor {
			extraStr = "  " + dimStyle.Render(extra)
		} else {
			extraStr = "  " + extra
		}
	}

	return fmt.Sprintf("    %s %s  %s tokens%s\n", labelStr, costStr, tokenStr, extraStr)
}

// renderEmptyState renders a clean empty state for new sessions
func (m Model) renderEmptyState() string {
	var sb strings.Builder
	sectionWidth := panelWidthFor(m.width)

	// Hero cost (even $0.00 to establish visual anchor)
	sb.WriteString("\n")
	sb.WriteString(m.renderHeroCost(0, false, sectionWidth))
	sb.WriteString("\n\n")

	// Simple awaiting message centered
	msg := "Awaiting first message..."
	padding := (sectionWidth - len(msg)) / 2
	if padding < 0 {
		padding = 0
	}
	pad := strings.Repeat(" ", padding)

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
