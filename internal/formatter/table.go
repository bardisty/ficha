package formatter

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/bardisty/ccusage/internal/analyzer"
	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/pricing"
	"github.com/bardisty/ccusage/internal/styles"
	"github.com/charmbracelet/lipgloss"
)

// Local aliases for frequently used styles
var (
	savingsLabelStyle  = styles.SavingsLabelStyle
	savingsValueStyle  = styles.SavingsValueStyle
	footerStyle        = styles.FooterStyle
	heroCostStyle      = styles.HeroCostStyle
	sectionHeaderStyle = styles.SectionHeaderStyle
	panelBorderStyle   = styles.PanelBorderStyle
	dimStyle           = styles.DimStyle
	headerStyle        = styles.HeaderStyle
)

// getCacheTokensByTTL returns separate token counts for 5m and 1h TTL cache writes.
// Falls back to aggregate (all 5m) when detailed breakdown unavailable.
func getCacheTokensByTTL(usage models.TokenUsage) (int64, int64) {
	if usage.CacheCreation != nil {
		return usage.CacheCreation.Ephemeral5mInputTokens, usage.CacheCreation.Ephemeral1hInputTokens
	}
	return usage.CacheCreationInputTokens, 0
}

// FormatSessionTable formats a session analysis as a styled table
func FormatSessionTable(analysis *models.SessionAnalysis, noColor bool) string {
	if noColor {
		return formatSessionTablePlain(analysis)
	}

	var sb strings.Builder
	const sectionWidth = 76

	// Detect summary vs show mode
	isSummary := strings.HasPrefix(analysis.SessionID, "Summary")

	// Header panel
	sb.WriteString(renderHeaderPanel(analysis, sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Hero total cost as section header
	sb.WriteString(renderHeroCost(analysis.TotalCost.TotalCost, sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Unified cost+token rows
	// Input tokens
	sb.WriteString(renderUnifiedCostRow("Input", analysis.TotalCost.InputCost, analysis.TotalUsage.InputTokens, lipgloss.Color(""), "", noColor))

	// Output tokens
	sb.WriteString(renderUnifiedCostRow("Output", analysis.TotalCost.OutputCost, analysis.TotalUsage.OutputTokens, styles.OutputTokenColor, "", noColor))

	// Cache write rows
	cache5mTokens, cache1hTokens := getCacheTokensByTTL(analysis.TotalUsage)
	has5mCost := analysis.TotalCost.CacheWrite5mCost > 0
	has1hCost := analysis.TotalCost.CacheWrite1hCost > 0

	if has5mCost {
		sb.WriteString(renderUnifiedCostRow("Cache write", analysis.TotalCost.CacheWrite5mCost, cache5mTokens, styles.CacheWriteTokenColor, "5m TTL", noColor))
	}

	if has1hCost {
		sb.WriteString(renderUnifiedCostRow("Cache write", analysis.TotalCost.CacheWrite1hCost, cache1hTokens, styles.CacheWriteTokenColor, "1h TTL", noColor))
	}

	// Cache read - only show if present
	if analysis.TotalCost.CacheReadCost > 0 || analysis.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(renderUnifiedCostRow("Cache read", analysis.TotalCost.CacheReadCost, analysis.TotalUsage.CacheReadInputTokens, styles.CacheReadTokenColor, "", noColor))
	}

	// Savings row
	if analysis.TotalCost.CacheSavings > 0 {
		savingsStr := formatCostStyledGreen(analysis.TotalCost.CacheSavings, 11, noColor)
		sb.WriteString(fmt.Sprintf("  %s %s  %s\n",
			savingsLabelStyle.Render(fmt.Sprintf("%-14s", "Savings")),
			savingsStr,
			dimStyle.Render("(from cache reads)")))
	}

	// Context window section - only for single sessions, not summaries
	if !isSummary {
		contextSize := analysis.LastMessageUsage.ContextWindowSize()
		if contextSize > 0 {
			sb.WriteString("\n")
			sb.WriteString(renderContextSection(analysis, noColor))
		}
	}

	// Cost by model section
	sb.WriteString("\n")
	sb.WriteString(renderSectionHeader("COST BY MODEL", sectionWidth, noColor))
	sb.WriteString("\n\n")
	sb.WriteString(formatCostByModelContent(analysis, noColor))

	// Agent breakdown (shown when agents exist)
	if analysis.HasAgents {
		sb.WriteString("\n")
		sb.WriteString(renderSectionHeader("AGENT SUB-SESSIONS", sectionWidth, noColor))
		sb.WriteString("\n\n")
		sb.WriteString(formatAgentBreakdownContent(analysis, noColor))
	}

	// Message insights (shown when insights are available)
	if analysis.Insights != nil {
		sb.WriteString("\n")
		sb.WriteString(renderSectionHeader("MESSAGE INSIGHTS", sectionWidth, noColor))
		sb.WriteString("\n\n")
		sb.WriteString(formatInsightsSectionContent(analysis.Insights, noColor))
	}

	// Footer with double-line separator
	sb.WriteString("\n")
	sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, sectionWidth)))
	sb.WriteString("\n")

	// Footer stats
	msgStr := fmt.Sprintf("%d", analysis.MessageCount)
	if analysis.AgentMessageCount > 0 {
		msgStr = fmt.Sprintf("%d (%d parent, %d agents)",
			analysis.MessageCount, analysis.ParentMessageCount, analysis.AgentMessageCount)
	}

	footerText := fmt.Sprintf("Messages: %s", msgStr)
	if isSummary {
		// Extract session count from the ID (format: "Summary (N sessions)")
		var sessionCount int
		_, _ = fmt.Sscanf(analysis.SessionID, "Summary (%d sessions)", &sessionCount)
		if sessionCount > 0 {
			footerText += fmt.Sprintf("  │  Sessions: %d", sessionCount)
		}
	} else {
		// Show last active time for single sessions
		if !analysis.EndTime.IsZero() {
			footerText += fmt.Sprintf("  │  Last active: %s", analysis.EndTime.Local().Format("2006-01-02 15:04"))
		}
	}

	sb.WriteString(footerStyle.Render(footerText))
	sb.WriteString("\n")

	// Single-line help separator
	sb.WriteString(dimStyle.Render(strings.Repeat(styles.LineHorizontal, sectionWidth)))

	return sb.String()
}

// formatSessionTablePlain formats a session analysis as a plain text table
func formatSessionTablePlain(analysis *models.SessionAnalysis) string {
	var sb strings.Builder
	const sectionWidth = 76

	// Detect summary vs show mode
	isSummary := strings.HasPrefix(analysis.SessionID, "Summary")

	// Header panel
	sb.WriteString(renderHeaderPanel(analysis, sectionWidth, true))
	sb.WriteString("\n\n")

	// Hero total cost as section header
	sb.WriteString(renderHeroCost(analysis.TotalCost.TotalCost, sectionWidth, true))
	sb.WriteString("\n\n")

	// Unified cost+token rows
	sb.WriteString(renderUnifiedCostRowPlain("Input", analysis.TotalCost.InputCost, analysis.TotalUsage.InputTokens, ""))
	sb.WriteString(renderUnifiedCostRowPlain("Output", analysis.TotalCost.OutputCost, analysis.TotalUsage.OutputTokens, ""))

	// Cache write rows
	cache5mTokens, cache1hTokens := getCacheTokensByTTL(analysis.TotalUsage)
	has5mCost := analysis.TotalCost.CacheWrite5mCost > 0
	has1hCost := analysis.TotalCost.CacheWrite1hCost > 0

	if has5mCost {
		sb.WriteString(renderUnifiedCostRowPlain("Cache write", analysis.TotalCost.CacheWrite5mCost, cache5mTokens, "5m TTL"))
	}

	if has1hCost {
		sb.WriteString(renderUnifiedCostRowPlain("Cache write", analysis.TotalCost.CacheWrite1hCost, cache1hTokens, "1h TTL"))
	}

	if analysis.TotalCost.CacheReadCost > 0 || analysis.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(renderUnifiedCostRowPlain("Cache read", analysis.TotalCost.CacheReadCost, analysis.TotalUsage.CacheReadInputTokens, ""))
	}

	// Savings row
	if analysis.TotalCost.CacheSavings > 0 {
		sb.WriteString(fmt.Sprintf("  %-14s %11s  (from cache reads)\n",
			"Savings", formatCost(analysis.TotalCost.CacheSavings)))
	}

	// Context window section - only for single sessions, not summaries
	if !isSummary {
		contextSize := analysis.LastMessageUsage.ContextWindowSize()
		if contextSize > 0 {
			sb.WriteString("\n")
			sb.WriteString(renderContextSection(analysis, true))
		}
	}

	// Cost by model section
	sb.WriteString("\n")
	sb.WriteString(renderSectionHeader("COST BY MODEL", sectionWidth, true))
	sb.WriteString("\n\n")
	sb.WriteString(formatCostByModelContent(analysis, true))

	// Agent breakdown (shown when agents exist)
	if analysis.HasAgents {
		sb.WriteString("\n")
		sb.WriteString(renderSectionHeader("AGENT SUB-SESSIONS", sectionWidth, true))
		sb.WriteString("\n\n")
		sb.WriteString(formatAgentBreakdownContent(analysis, true))
	}

	// Message insights (shown when insights are available)
	if analysis.Insights != nil {
		sb.WriteString("\n")
		sb.WriteString(renderSectionHeader("MESSAGE INSIGHTS", sectionWidth, true))
		sb.WriteString("\n\n")
		sb.WriteString(formatInsightsSectionContent(analysis.Insights, true))
	}

	// Footer with double-line separator
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("=", sectionWidth))
	sb.WriteString("\n")

	// Footer stats
	msgStr := fmt.Sprintf("%d", analysis.MessageCount)
	if analysis.AgentMessageCount > 0 {
		msgStr = fmt.Sprintf("%d (%d parent, %d agents)",
			analysis.MessageCount, analysis.ParentMessageCount, analysis.AgentMessageCount)
	}

	footerText := fmt.Sprintf("Messages: %s", msgStr)
	if isSummary {
		var sessionCount int
		_, _ = fmt.Sscanf(analysis.SessionID, "Summary (%d sessions)", &sessionCount)
		if sessionCount > 0 {
			footerText += fmt.Sprintf("  |  Sessions: %d", sessionCount)
		}
	} else {
		// Show last active time for single sessions
		if !analysis.EndTime.IsZero() {
			footerText += fmt.Sprintf("  |  Last active: %s", analysis.EndTime.Local().Format("2006-01-02 15:04"))
		}
	}

	sb.WriteString(footerText)
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", sectionWidth))

	return sb.String()
}

// FormatSummaryTableWithDetails formats the summary table with session breakdown
func FormatSummaryTableWithDetails(analysis *models.SessionAnalysis, sessions []models.SessionEntry, projectDir string, noColor bool, expandAgents bool) string {
	if noColor {
		return formatSummaryTableWithDetailsPlain(analysis, sessions, projectDir, expandAgents)
	}

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
	cache5mTokens, cache1hTokens := getCacheTokensByTTL(analysis.TotalUsage)
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
		savingsStr := formatCostStyledGreen(analysis.TotalCost.CacheSavings, 11, noColor)
		sb.WriteString(fmt.Sprintf("  %s %s  %s\n",
			savingsLabelStyle.Render(fmt.Sprintf("%-14s", "Savings")),
			savingsStr,
			dimStyle.Render("(from cache reads)")))
	}

	// Cost by model section
	sb.WriteString("\n")
	sb.WriteString(renderSectionHeader("COST BY MODEL", sectionWidth, noColor))
	sb.WriteString("\n\n")
	sb.WriteString(formatCostByModelContent(analysis, noColor))

	// Get session breakdown data (needed for both chart and table)
	breakdownResult := renderSessionBreakdown(sessions, noColor, expandAgents)

	// Session breakdown section (includes chart and table)
	sb.WriteString("\n")
	sb.WriteString(renderSectionHeader("SESSION BREAKDOWN", sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Cost chart at top of section (only show if we have 2+ data points)
	if len(breakdownResult.costs) > 1 {
		sb.WriteString(renderCostChart(breakdownResult.costs, breakdownResult.dates, sectionWidth, noColor))
		sb.WriteString("\n")
	}

	sb.WriteString(breakdownResult.table)

	// Footer with double-line separator
	sb.WriteString("\n")
	sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, sectionWidth)))
	sb.WriteString("\n")

	// Footer stats
	msgStr := fmt.Sprintf("%d", analysis.MessageCount)
	if analysis.AgentMessageCount > 0 {
		msgStr = fmt.Sprintf("%d (%d parent, %d agents)",
			analysis.MessageCount, analysis.ParentMessageCount, analysis.AgentMessageCount)
	}

	var sessionCount int
	_, _ = fmt.Sscanf(analysis.SessionID, "Summary (%d sessions)", &sessionCount)

	footerText := fmt.Sprintf("Messages: %s  │  Sessions: %d", msgStr, sessionCount)
	sb.WriteString(footerStyle.Render(footerText))
	sb.WriteString("\n")

	// Single-line separator
	sb.WriteString(dimStyle.Render(strings.Repeat(styles.LineHorizontal, sectionWidth)))
	sb.WriteString("\n")

	// Project path
	sb.WriteString(dimStyle.Render(fmt.Sprintf("Project: %s", projectDir)))

	return sb.String()
}

// formatSummaryTableWithDetailsPlain formats the summary with details in plain text
func formatSummaryTableWithDetailsPlain(analysis *models.SessionAnalysis, sessions []models.SessionEntry, projectDir string, expandAgents bool) string {
	var sb strings.Builder
	const sectionWidth = 76

	// Header panel
	sb.WriteString(renderHeaderPanel(analysis, sectionWidth, true))
	sb.WriteString("\n\n")

	// Hero total cost as section header
	sb.WriteString(renderHeroCost(analysis.TotalCost.TotalCost, sectionWidth, true))
	sb.WriteString("\n\n")

	// Unified cost+token rows
	sb.WriteString(renderUnifiedCostRowPlain("Input", analysis.TotalCost.InputCost, analysis.TotalUsage.InputTokens, ""))
	sb.WriteString(renderUnifiedCostRowPlain("Output", analysis.TotalCost.OutputCost, analysis.TotalUsage.OutputTokens, ""))

	// Cache write rows
	cache5mTokens, cache1hTokens := getCacheTokensByTTL(analysis.TotalUsage)
	has5mCost := analysis.TotalCost.CacheWrite5mCost > 0
	has1hCost := analysis.TotalCost.CacheWrite1hCost > 0

	if has5mCost {
		sb.WriteString(renderUnifiedCostRowPlain("Cache write", analysis.TotalCost.CacheWrite5mCost, cache5mTokens, "5m TTL"))
	}

	if has1hCost {
		sb.WriteString(renderUnifiedCostRowPlain("Cache write", analysis.TotalCost.CacheWrite1hCost, cache1hTokens, "1h TTL"))
	}

	if analysis.TotalCost.CacheReadCost > 0 || analysis.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(renderUnifiedCostRowPlain("Cache read", analysis.TotalCost.CacheReadCost, analysis.TotalUsage.CacheReadInputTokens, ""))
	}

	// Savings row
	if analysis.TotalCost.CacheSavings > 0 {
		sb.WriteString(fmt.Sprintf("  %-14s %11s  (from cache reads)\n",
			"Savings", formatCost(analysis.TotalCost.CacheSavings)))
	}

	// Cost by model section
	sb.WriteString("\n")
	sb.WriteString(renderSectionHeader("COST BY MODEL", sectionWidth, true))
	sb.WriteString("\n\n")
	sb.WriteString(formatCostByModelContent(analysis, true))

	// Get session breakdown data (needed for both chart and table)
	breakdownResult := renderSessionBreakdown(sessions, true, expandAgents)

	// Session breakdown section (includes chart and table)
	sb.WriteString("\n")
	sb.WriteString(renderSectionHeader("SESSION BREAKDOWN", sectionWidth, true))
	sb.WriteString("\n\n")

	// Cost chart at top of section (only show if we have 2+ data points)
	if len(breakdownResult.costs) > 1 {
		sb.WriteString(renderCostChart(breakdownResult.costs, breakdownResult.dates, sectionWidth, true))
		sb.WriteString("\n")
	}

	sb.WriteString(breakdownResult.table)

	// Footer with double-line separator
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("=", sectionWidth))
	sb.WriteString("\n")

	// Footer stats
	msgStr := fmt.Sprintf("%d", analysis.MessageCount)
	if analysis.AgentMessageCount > 0 {
		msgStr = fmt.Sprintf("%d (%d parent, %d agents)",
			analysis.MessageCount, analysis.ParentMessageCount, analysis.AgentMessageCount)
	}

	var sessionCount int
	_, _ = fmt.Sscanf(analysis.SessionID, "Summary (%d sessions)", &sessionCount)

	footerText := fmt.Sprintf("Messages: %s  |  Sessions: %d", msgStr, sessionCount)
	sb.WriteString(footerText)
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", sectionWidth))
	sb.WriteString("\n")

	// Project path
	sb.WriteString(fmt.Sprintf("Project: %s", projectDir))

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
func renderSessionBreakdown(sessions []models.SessionEntry, noColor bool, expandAgents bool) sessionBreakdownResult {
	var sb strings.Builder

	// Sort by modified time
	sorted := make([]models.SessionEntry, len(sessions))
	copy(sorted, sessions)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Modified.Before(sorted[j].Modified)
	})

	// Analyze each session and collect costs
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

	for _, s := range sorted {
		analysis, err := analyzer.AnalyzeSession(s.FullPath, s.SessionID, false)
		if err != nil {
			sessionData = append(sessionData, sessionWithAnalysis{entry: s, analysis: nil, cost: 0, err: true})
			continue
		}
		cost := analysis.TotalCost.TotalCost
		sessionData = append(sessionData, sessionWithAnalysis{entry: s, analysis: analysis, cost: cost, err: false})
		costs = append(costs, cost)
		dates = append(dates, s.Modified)
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
	contentWidth := 72

	var headerRow string
	if expandAgents {
		// Expanded view: simple 5-column layout (72 chars max for RFC brutalist compliance)
		headerRow = fmt.Sprintf("  %4s %-8s  %-12s  %-10s  %8s  %10s", "#", "SESSION", "MODIFIED", "MODEL", "COST", "CUMULATIVE")
	} else {
		// Default view: 7 columns, 72 chars max - AGENTS before COST groups metadata together
		headerRow = fmt.Sprintf("  %4s %-8s  %-12s  %-9s %10s  %8s %10s", "#", "SESSION", "MODIFIED", "MODEL", "AGENTS", "COST", "CUMULATIVE")
	}

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
					sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-10s  %8s  %10s\n", num, shortID, modifiedStr, "-", "(error)", "-"))
				} else {
					sb.WriteString(fmt.Sprintf("  %s %s  %s  %s  %s  %s\n",
						dimStyle.Render(fmt.Sprintf("%4d", num)),
						fmt.Sprintf("%-8s", shortID),
						dimStyle.Render(fmt.Sprintf("%-12s", modifiedStr)),
						dimStyle.Render(fmt.Sprintf("%-10s", "-")),
						dimStyle.Render(fmt.Sprintf("%8s", "(error)")),
						dimStyle.Render(fmt.Sprintf("%10s", "-"))))
				}
			} else {
				if noColor {
					sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-9s %10s  %8s %10s\n", num, shortID, modifiedStr, "-", "-", "(error)", "-"))
				} else {
					sb.WriteString(fmt.Sprintf("  %s %s  %s  %s %s  %s %s\n",
						dimStyle.Render(fmt.Sprintf("%4d", num)),
						fmt.Sprintf("%-8s", shortID),
						dimStyle.Render(fmt.Sprintf("%-12s", modifiedStr)),
						dimStyle.Render(fmt.Sprintf("%-9s", "-")),
						dimStyle.Render(fmt.Sprintf("%10s", "-")),
						dimStyle.Render(fmt.Sprintf("%8s", "(error)")),
						dimStyle.Render(fmt.Sprintf("%10s", "-"))))
				}
			}
			continue
		}

		cumulativeSum += sd.cost

		// Format agents column (only used when not expanding)
		agentsStr := formatAgentsColumn(sd.analysis, noColor)

		// Get primary model for the PARENT session (excludes agent models)
		modelName := "-"
		if sd.analysis != nil {
			// Use ParentCostByModel to get parent-only model, fall back to CostByModel for backwards compatibility
			if len(sd.analysis.ParentCostByModel) > 0 {
				modelName = getPrimaryModel(sd.analysis.ParentCostByModel)
			} else {
				modelName = getPrimaryModel(sd.analysis.CostByModel)
			}
		}

		if expandAgents {
			// Expanded view: 5 columns, 2-decimal costs (RFC brutalist: under 72 chars)
			if noColor {
				costStr := fmt.Sprintf("$%.2f", sd.cost)
				cumStr := fmt.Sprintf("$%.2f", cumulativeSum)
				sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-10s  %8s  %10s\n", num, shortID, modifiedStr, modelName, costStr, cumStr))
			} else {
				costColor := styles.GetCostGradientColor(sd.cost, minCost, maxCost)
				costStr := fmt.Sprintf("$%.2f", sd.cost)
				costStyled := lipgloss.NewStyle().Foreground(costColor).Render(fmt.Sprintf("%8s", costStr))
				cumStr := fmt.Sprintf("$%.2f", cumulativeSum)
				cumStyled := lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(fmt.Sprintf("%10s", cumStr))

				// Color model name by tier
				modelColor := styles.GetModelColor(modelName)
				modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-10s", modelName))

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
				sb.WriteString(renderAgentTreeRows(sd.analysis.Agents, noColor))
			}
		} else {
			// Default view: AGENTS before COST (groups metadata, then costs)
			if noColor {
				costStr := fmt.Sprintf("$%.2f", sd.cost)
				cumStr := fmt.Sprintf("$%.2f", cumulativeSum)
				sb.WriteString(fmt.Sprintf("  %4d %-8s  %-12s  %-9s %10s  %8s %10s\n", num, shortID, modifiedStr, modelName, agentsStr, costStr, cumStr))
			} else {
				costColor := styles.GetCostGradientColor(sd.cost, minCost, maxCost)
				costStr := fmt.Sprintf("$%.2f", sd.cost)
				costStyled := lipgloss.NewStyle().Foreground(costColor).Render(fmt.Sprintf("%8s", costStr))
				cumStr := fmt.Sprintf("$%.2f", cumulativeSum)
				cumStyled := lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(fmt.Sprintf("%10s", cumStr))

				// Color model name by tier (9 chars to fit 72-char width)
				modelColor := styles.GetModelColor(modelName)
				modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-9s", modelName))

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
	sumLabel := fmt.Sprintf("Sum (%d sessions):", validSessions)

	if noColor {
		sumCostStr := fmt.Sprintf("$%.2f", totalCost)
		sb.WriteString("  " + strings.Repeat("-", contentWidth) + "\n")
		if expandAgents {
			// Expanded: label spans left columns, sum right-aligned under CUMULATIVE
			sb.WriteString(fmt.Sprintf("  %-50s  %10s\n", sumLabel, sumCostStr))
		} else {
			// Default: label aligns under SESSION through AGENTS columns, cost under CUMULATIVE
			sb.WriteString(fmt.Sprintf("  %-52s  %10s\n", sumLabel, sumCostStr))
		}
	} else {
		sb.WriteString("  " + dimStyle.Render(strings.Repeat(styles.LineHorizontal, contentWidth)) + "\n")
		sumCostStr := fmt.Sprintf("$%.2f", totalCost)
		sumStyled := lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(fmt.Sprintf("%10s", sumCostStr))
		if expandAgents {
			sb.WriteString(fmt.Sprintf("  %-50s  %s\n", sumLabel, sumStyled))
		} else {
			sb.WriteString(fmt.Sprintf("  %-52s  %s\n", sumLabel, sumStyled))
		}
	}

	return sessionBreakdownResult{
		table: sb.String(),
		costs: costs,
		dates: dates,
	}
}

// formatAgentsColumn formats the AGENTS column value for a session
// Format: "N [$X.XX]" where N = agent count, $X.XX = agent subtotal (2 decimals)
// Returns "-" (dimmed) if no agents
// Column width: 10 chars
func formatAgentsColumn(analysis *models.SessionAnalysis, noColor bool) string {
	const colWidth = 10

	if analysis == nil || !analysis.HasAgents || analysis.AgentCount == 0 {
		if noColor {
			return fmt.Sprintf("%*s", colWidth, "-")
		}
		// For styled output, add padding BEFORE the styled content
		// (can't use %Ns because ANSI codes count as bytes)
		return strings.Repeat(" ", colWidth-1) + dimStyle.Render("-")
	}

	// Format: "N [$X.XX]" - right-aligned in colWidth chars
	agentCost := analysis.AgentsCost.TotalCost
	costStr := fmt.Sprintf("[$%.2f]", agentCost)
	fullStr := fmt.Sprintf("%d %s", analysis.AgentCount, costStr)

	if noColor {
		return fmt.Sprintf("%*s", colWidth, fullStr)
	}

	// Dim the bracketed cost (it's already included in COST column)
	// Calculate padding to right-align the display to colWidth chars
	displayWidth := len(fullStr)
	padding := colWidth - displayWidth
	if padding < 0 {
		padding = 0
	}
	countStr := fmt.Sprintf("%d ", analysis.AgentCount)
	return strings.Repeat(" ", padding) + countStr + dimStyle.Render(costStr)
}

// renderAgentTreeRows renders indented agent sub-session rows with tree connectors
// Tree connectors: ├─ for all but last, └─ for final agent
// Compact format: "    ├─ [A1] Haiku 4.5  45 msgs  $0.18"
// Follows RFC brutalist principle: remove the unnecessary (no agent ID - it's noise)
func renderAgentTreeRows(agents []models.AgentAnalysis, noColor bool) string {
	var sb strings.Builder

	// Tree connector characters
	var branchChar, lastBranchChar string
	if noColor {
		branchChar = "+-"
		lastBranchChar = "`-"
	} else {
		branchChar = "├─"
		lastBranchChar = "└─"
	}

	for i, agent := range agents {
		agentNum := i + 1
		isLast := i == len(agents)-1

		// Tree connector
		var connector string
		if isLast {
			connector = lastBranchChar
		} else {
			connector = branchChar
		}

		// Get primary model for this agent
		modelName := getPrimaryModel(agent.CostByModel)

		// Format message count
		msgStr := fmt.Sprintf("%d msgs", agent.MessageCount)
		if agent.MessageCount == 1 {
			msgStr = "1 msg"
		}

		agentLabel := fmt.Sprintf("[A%d]", agentNum)

		// Format: "       ├─ [A1] Haiku 4.5  45 msgs  $0.18"
		// 7-space indent aligns tree connector under SESSION column
		if noColor {
			costStr := fmt.Sprintf("$%.2f", agent.TotalCost.TotalCost)
			sb.WriteString(fmt.Sprintf("       %s %s %-10s %8s  %s\n",
				connector, agentLabel, modelName, msgStr, costStr))
		} else {
			// Color agent marker using GetAgentColor
			agentColor := styles.GetAgentColor(fmt.Sprintf("%d", agentNum))
			labelStyled := lipgloss.NewStyle().Foreground(agentColor).Render(agentLabel)

			// Color model name by tier
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-10s", modelName))

			// Dim the message count and cost
			msgStyled := dimStyle.Render(fmt.Sprintf("%8s", msgStr))
			costStyled := dimStyle.Render(fmt.Sprintf("$%.2f", agent.TotalCost.TotalCost))

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

// Chart display constants for summary sparkline
const (
	summaryChartHeight = 6 // 6 rows = 24 braille levels (matching watch command)
)

// renderCostChart renders a sparkline chart showing session costs over time
// Uses ntcharts braille rendering for high-resolution visualization (24 vertical levels)
func renderCostChart(costs []float64, dates []time.Time, width int, noColor bool) string {
	if len(costs) == 0 {
		return ""
	}

	var sb strings.Builder

	// Find min/max for labels
	var minCost, maxCost float64
	minCost = costs[0]
	maxCost = costs[0]
	for _, c := range costs {
		if c < minCost {
			minCost = c
		}
		if c > maxCost {
			maxCost = c
		}
	}

	// Calculate chart width (leave room for indent and some padding)
	indent := "    " // 4-space indent to match section content
	chartWidth := width - 8
	if chartWidth < 20 {
		chartWidth = 20
	}
	if chartWidth > 68 {
		chartWidth = 68 // Cap at reasonable width
	}

	// Create sparkline chart with appropriate styling
	var chart sparkline.Model
	if !noColor {
		chartStyle := lipgloss.NewStyle().Foreground(styles.SuccessColor)
		chart = sparkline.New(chartWidth, summaryChartHeight, sparkline.WithStyle(chartStyle))
	} else {
		chart = sparkline.New(chartWidth, summaryChartHeight)
	}

	// Push all cost data
	chart.PushAll(costs)

	// Render with appropriate mode
	if !noColor {
		chart.DrawBraille()
	} else {
		chart.Draw()
	}

	// Render the chart with proper indentation
	chartLines := strings.Split(chart.View(), "\n")
	for _, line := range chartLines {
		if line != "" {
			sb.WriteString(indent + line + "\n")
		}
	}

	// Add scale labels below the chart
	scaleInfo := fmt.Sprintf("min: %s  max: %s  (%d sessions)",
		formatChartCost(minCost), formatChartCost(maxCost), len(costs))
	if noColor {
		sb.WriteString(indent + scaleInfo + "\n")
	} else {
		sb.WriteString(indent + dimStyle.Render(scaleInfo) + "\n")
	}

	// Add date labels
	if len(dates) >= 2 {
		startDate := dates[0].Format("02 JAN")
		endDate := dates[len(dates)-1].Format("02 JAN")

		// Position dates at start and end of chart area
		padding := chartWidth - len(startDate) - len(endDate)
		if padding < 1 {
			padding = 1
		}
		dateLine := fmt.Sprintf("%s%s%s", startDate, strings.Repeat(" ", padding), endDate)
		if noColor {
			sb.WriteString(indent + dateLine + "\n")
		} else {
			sb.WriteString(indent + dimStyle.Render(dateLine) + "\n")
		}
	}

	return sb.String()
}

// formatChartCost formats a cost value compactly for chart Y-axis labels
func formatChartCost(cost float64) string {
	if cost >= 100 {
		return fmt.Sprintf("$%.0f", cost)
	} else if cost >= 10 {
		return fmt.Sprintf("$%.1f", cost)
	} else if cost >= 1 {
		return fmt.Sprintf("$%.2f", cost)
	}
	return fmt.Sprintf("$%.3f", cost)
}

// Rendering helpers (matching TUI style)

// renderHeaderPanel renders the mainframe-style header panel
// Format:
// ╔══════════════════════════════════════════════════════════════════════════╗
// ║  Session: xxx  │  Duration: Xh Ym                                        ║
// ╚══════════════════════════════════════════════════════════════════════════╝
func renderHeaderPanel(analysis *models.SessionAnalysis, width int, noColor bool) string {
	var sb strings.Builder

	if width < 40 {
		width = 76
	}

	innerWidth := width - 6 // 2 for borders, 2 for left padding, 2 for right padding

	// Detect summary vs show mode
	isSummary := strings.HasPrefix(analysis.SessionID, "Summary")

	// Build content parts - use consistent format for both plain and styled
	var titlePart string
	var sessionCount int
	if isSummary {
		// Extract session count from ID (format: "Summary (N sessions)")
		_, _ = fmt.Sscanf(analysis.SessionID, "Summary (%d sessions)", &sessionCount)
		titlePart = fmt.Sprintf("Summary: %d sessions", sessionCount)
	} else {
		titlePart = fmt.Sprintf("Session: %s", truncateID(analysis.SessionID))
	}

	durationPart := fmt.Sprintf("Duration: %s", formatDuration(analysis.Duration.Duration()))

	// Calculate content length
	sep := styles.BoxVerticalSep
	content := fmt.Sprintf("%s  %s  %s", titlePart, sep, durationPart)
	contentLen := len(titlePart) + 2 + 1 + 2 + len(durationPart)
	padding := innerWidth - contentLen
	if padding < 0 {
		padding = 0
	}

	if noColor {
		// Top border
		sb.WriteString(styles.BoxTopLeft)
		sb.WriteString(strings.Repeat(styles.BoxHorizontal, width-2))
		sb.WriteString(styles.BoxTopRight)
		sb.WriteString("\n")

		// Content line
		sb.WriteString(styles.BoxVertical)
		sb.WriteString("  ")
		sb.WriteString(content)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString("  ")
		sb.WriteString(styles.BoxVertical)
		sb.WriteString("\n")

		// Bottom border
		sb.WriteString(styles.BoxBottomLeft)
		sb.WriteString(strings.Repeat(styles.BoxHorizontal, width-2))
		sb.WriteString(styles.BoxBottomRight)
	} else {
		// Build styled content - matches plain text format
		var titleStyled string
		if isSummary {
			titleStyled = fmt.Sprintf("%s %d sessions",
				sectionHeaderStyle.Render("Summary:"),
				sessionCount)
		} else {
			titleStyled = fmt.Sprintf("%s %s",
				sectionHeaderStyle.Render("Session:"),
				truncateID(analysis.SessionID))
		}

		durationStyled := fmt.Sprintf("Duration: %s", formatDuration(analysis.Duration.Duration()))
		sepStyled := panelBorderStyle.Render(sep)

		// Top border
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopRight))
		sb.WriteString("\n")

		// Content line
		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("  ")
		sb.WriteString(titleStyled)
		sb.WriteString("  ")
		sb.WriteString(sepStyled)
		sb.WriteString("  ")
		sb.WriteString(durationStyled)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString("  ")
		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("\n")

		// Bottom border
		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomRight))
	}

	return sb.String()
}

// renderSectionHeader renders a bracketed section header
// Format: ─────────────────────────────[ SECTION NAME ]─────────────────────────────
func renderSectionHeader(name string, width int, noColor bool) string {
	if width < 20 {
		width = 76
	}

	bracketedName := "[ " + name + " ]"
	nameLen := len(bracketedName)
	sideLen := (width - nameLen) / 2
	if sideLen < 0 {
		sideLen = 0
	}
	rightLen := width - sideLen - nameLen
	if rightLen < 0 {
		rightLen = 0
	}

	leftLine := strings.Repeat(styles.LineHorizontal, sideLen)
	rightLine := strings.Repeat(styles.LineHorizontal, rightLen)

	if noColor {
		return leftLine + bracketedName + rightLine
	}

	// Section name in cyan, lines in dim
	return dimStyle.Render(leftLine) + "[ " + sectionHeaderStyle.Render(name) + " ]" + dimStyle.Render(rightLine)
}

// renderHeroCost renders the total cost integrated into a section header
// Format: ─────────────────────[ $12.665834 TOTAL ]─────────────────────
func renderHeroCost(cost float64, width int, noColor bool) string {
	costFull := fmt.Sprintf("$%.6f", cost)
	costStr := costFull + " TOTAL"
	bracketedCost := "[ " + costStr + " ]"
	costLen := len(bracketedCost)
	sideLen := (width - costLen) / 2
	if sideLen < 0 {
		sideLen = 0
	}
	rightLen := width - sideLen - costLen
	if rightLen < 0 {
		rightLen = 0
	}

	leftLine := strings.Repeat(styles.LineHorizontal, sideLen)
	rightLine := strings.Repeat(styles.LineHorizontal, rightLen)

	if noColor {
		return leftLine + bracketedCost + rightLine
	}

	// Cost with dimmed trailing decimals
	var costStyled string
	dotIdx := strings.Index(costFull, ".")
	if dotIdx != -1 && len(costFull) > dotIdx+3 {
		mainPart := costFull[:dotIdx+3]  // "$12.66"
		extraPart := costFull[dotIdx+3:] // "5834"
		costStyled = heroCostStyle.Render(mainPart) + dimStyle.Render(extraPart) + heroCostStyle.Render(" TOTAL")
	} else {
		costStyled = heroCostStyle.Render(costStr)
	}

	return dimStyle.Render(leftLine) + "[ " + costStyled + " ]" + dimStyle.Render(rightLine)
}

// renderUnifiedCostRow renders a single row with cost and token info combined
// Format: "  Label          $0.371042     53.9K tokens"
func renderUnifiedCostRow(label string, cost float64, tokens int64, labelColor lipgloss.Color, extra string, noColor bool) string {
	// Format label with optional color
	var labelStr string
	if !noColor && labelColor != "" {
		labelStyled := lipgloss.NewStyle().Foreground(labelColor)
		labelStr = labelStyled.Render(fmt.Sprintf("%-14s", label))
	} else {
		labelStr = fmt.Sprintf("%-14s", label)
	}

	// Format cost (11 chars width)
	costStr := formatCostStyled(cost, 11, noColor)

	// Format tokens
	tokenStr := fmt.Sprintf("%12s", formatNumber(tokens))

	// Add extra info (like TTL)
	extraStr := ""
	if extra != "" {
		if !noColor {
			extraStr = "  " + dimStyle.Render(extra)
		} else {
			extraStr = "  " + extra
		}
	}

	return fmt.Sprintf("  %s %s  %s tokens%s\n", labelStr, costStr, tokenStr, extraStr)
}

// renderUnifiedCostRowPlain renders a cost row in plain text mode
func renderUnifiedCostRowPlain(label string, cost float64, tokens int64, extra string) string {
	extraStr := ""
	if extra != "" {
		extraStr = "  " + extra
	}
	return fmt.Sprintf("  %-14s %11s  %12s tokens%s\n", label, formatCost(cost), formatNumber(tokens), extraStr)
}

// renderContextSection renders the context window section
func renderContextSection(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	contextSize := analysis.LastMessageUsage.ContextWindowSize()
	if contextSize == 0 {
		return ""
	}

	modelPricing := pricing.GetModelPricing(analysis.LastMessageModel)
	maxContext := modelPricing.MaxContextTokens
	contextPct := pricing.GetContextPercentage(modelPricing, contextSize)
	freeSpace := pricing.GetFreeSpace(modelPricing, contextSize)
	buffer := pricing.GetAutocompactBuffer(modelPricing)
	freePct := float64(freeSpace) / float64(maxContext) * 100

	usageColor := styles.GetContextUsageColor(contextPct)

	// Context label with value
	contextVal := formatNumber(contextSize)
	contextMeta := fmt.Sprintf("(%.0f%% of %s)", contextPct, formatNumber(int64(maxContext)))

	if noColor {
		sb.WriteString(fmt.Sprintf("  Context  %s  %s %s\n", formatContextProgressBar(contextSize, freeSpace, buffer, maxContext, true), contextVal, contextMeta))
		sb.WriteString(fmt.Sprintf("           Free: %s (%.1f%%)  |  Buffer: %s\n", formatNumber(freeSpace), freePct, formatNumber(buffer)))
	} else {
		// Progress bar with context info
		coloredMeta := lipgloss.NewStyle().Foreground(usageColor).Render(contextMeta)
		sb.WriteString(fmt.Sprintf("  Context  %s  %s %s\n",
			formatContextProgressBar(contextSize, freeSpace, buffer, maxContext, false),
			contextVal, coloredMeta))

		// Free space and buffer info
		freeVal := formatNumber(freeSpace)
		bufferVal := formatNumber(buffer)
		freeValWithPct := fmt.Sprintf("%s (%.1f%%)", freeVal, freePct)
		freeStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(fmt.Sprintf("Free: %s", freeValWithPct))
		bufferStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(fmt.Sprintf("Buffer: %s", bufferVal))
		sb.WriteString(fmt.Sprintf("           %s  %s  %s\n", freeStyled, dimStyle.Render("│"), bufferStyled))
	}

	return sb.String()
}

// getPrimaryModel returns the dominant model for an agent (by highest cost)
// Returns the display name (e.g., "Opus 4.5") or "-" if no model data
func getPrimaryModel(costByModel map[string]models.CostBreakdown) string {
	var maxModel string
	var maxCost float64
	for model, cost := range costByModel {
		if cost.TotalCost > maxCost {
			maxCost = cost.TotalCost
			maxModel = model
		}
	}
	if maxModel == "" {
		return "-"
	}
	return pricing.GetModelDisplayName(maxModel)
}

// formatCostByModelContent renders cost by model rows (content only, no header)
func formatCostByModelContent(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	// Sort model IDs for deterministic output
	modelIDs := make([]string, 0, len(analysis.CostByModel))
	for modelID := range analysis.CostByModel {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)

	for _, modelID := range modelIDs {
		cost := analysis.CostByModel[modelID]
		modelName := pricing.GetModelDisplayName(modelID)
		if noColor {
			sb.WriteString(fmt.Sprintf("    %-12s %s\n", modelName, formatCost(cost.TotalCost)))
		} else {
			// Color by model tier
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-12s", modelName))
			sb.WriteString(fmt.Sprintf("    %s %s\n", modelStyled, formatCostStyled(cost.TotalCost, 12, noColor)))
		}
	}

	return sb.String()
}

// formatAgentBreakdownContent renders agent breakdown rows (content only, no header)
// Layout: [AN] Model (ID) msgs cost
// All rows align costs at column 45 (2 indent + 43 content)
// Example:
//
//	Parent session                            $1.135371
//	[A1] Opus 4.5    (a0b184d)     14 msgs    $1.358774
//	Agents subtotal                           $5.112218
func formatAgentBreakdownContent(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	// Parent session cost - right-aligned cost at column 45
	// Format: 2(indent) + 40(label) + 3(spaces) + cost = 45 chars before cost
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	if noColor {
		sb.WriteString(fmt.Sprintf("  %-40s   %s\n", "Parent session", formatCost(analysis.ParentCost.TotalCost)))
	} else {
		paddedLabel := fmt.Sprintf("%-40s", "Parent session")
		sb.WriteString(fmt.Sprintf("  %s   %s\n", paddedLabel, formatCostStyled(analysis.ParentCost.TotalCost, 11, noColor)))
	}

	// Each agent with [AN] Model (ID) msgs cost format
	// Format: 2(indent) + 5(marker) + 1 + 11(model) + 1 + 10(id) + 1 + 8(msgs) + 6(spaces) + cost
	//       = 2 + 37 + 6 = 45 chars before cost (aligned with parent)
	for i, agent := range analysis.Agents {
		agentNum := i + 1
		shortID := agent.AgentID
		if len(shortID) > 7 {
			shortID = shortID[:7]
		}

		// Get primary model for this agent
		modelName := getPrimaryModel(agent.CostByModel)

		// Format message count with singular/plural
		msgStr := fmt.Sprintf("%d msgs", agent.MessageCount)
		if agent.MessageCount == 1 {
			msgStr = "1 msg"
		}

		if noColor {
			marker := fmt.Sprintf("[A%d]", agentNum)
			idStr := fmt.Sprintf("(%s)", shortID)
			sb.WriteString(fmt.Sprintf("  %-5s %-11s %-10s %8s      %s\n",
				marker, modelName, idStr, msgStr, formatCost(agent.TotalCost.TotalCost)))
		} else {
			// Color agent marker (use %-5s to handle [A10] etc)
			agentColor := styles.GetAgentColor(fmt.Sprintf("%d", agentNum))
			markerStyled := lipgloss.NewStyle().Foreground(agentColor).Render(fmt.Sprintf("%-5s", fmt.Sprintf("[A%d]", agentNum)))

			// Color model name by tier
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-11s", modelName))

			// Dim the ID and message count
			idStyled := dimStyle.Render(fmt.Sprintf("%-10s", fmt.Sprintf("(%s)", shortID)))
			msgStyled := dimStyle.Render(fmt.Sprintf("%8s", msgStr))

			costStr := formatCostStyled(agent.TotalCost.TotalCost, 11, noColor)

			sb.WriteString(fmt.Sprintf("  %s %s %s %s      %s\n",
				markerStyled, modelStyled, idStyled, msgStyled, costStr))
		}
	}

	// Agents subtotal in bold green (matches TotalValueStyle for visual hierarchy)
	// Note: Must pad BEFORE styling to avoid ANSI escape codes breaking width calculation
	if noColor {
		sb.WriteString(fmt.Sprintf("  %-40s   %s\n", "Agents subtotal", formatCost(analysis.AgentsCost.TotalCost)))
	} else {
		paddedSubtotal := fmt.Sprintf("%-40s", "Agents subtotal")
		sb.WriteString(fmt.Sprintf("  %s   %s\n", paddedSubtotal, formatCostStyledBoldGreen(analysis.AgentsCost.TotalCost, 11, noColor)))
	}

	return sb.String()
}

// formatInsightsSectionContent renders message insights rows (content only, no header)
func formatInsightsSectionContent(insights *models.MessageInsights, noColor bool) string {
	var sb strings.Builder

	// First message
	if insights.FirstMessage != nil {
		first := insights.FirstMessage
		componentLabel := formatCostComponentLabel(first.MainCostComponent)
		timestamp := dimStyle.Render(fmt.Sprintf("(%s)", first.Timestamp.Format("15:04:05")))
		componentInfo := dimStyle.Render(fmt.Sprintf("%s: %s", componentLabel, formatCost(first.MainCostValue)))

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s: %s\n",
				"First",
				formatCostStyled(first.Cost, 10, noColor),
				first.Timestamp.Format("15:04:05"),
				componentLabel,
				formatCost(first.MainCostValue)))
		} else {
			sb.WriteString(fmt.Sprintf("  %-10s %s  %s  %s\n",
				"First",
				formatCostStyled(first.Cost, 10, noColor),
				timestamp,
				componentInfo))
		}
	}

	// Last message
	if insights.LastMessage != nil {
		last := insights.LastMessage
		componentLabel := formatCostComponentLabel(last.MainCostComponent)
		timestamp := dimStyle.Render(fmt.Sprintf("(%s)", last.Timestamp.Format("15:04:05")))
		componentInfo := dimStyle.Render(fmt.Sprintf("%s: %s", componentLabel, formatCost(last.MainCostValue)))

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  %s: %s\n",
				"Last",
				formatCostStyled(last.Cost, 10, noColor),
				last.Timestamp.Format("15:04:05"),
				componentLabel,
				formatCost(last.MainCostValue)))
		} else {
			sb.WriteString(fmt.Sprintf("  %-10s %s  %s  %s\n",
				"Last",
				formatCostStyled(last.Cost, 10, noColor),
				timestamp,
				componentInfo))
		}
	}

	// Highest cost (only if notably above average)
	if insights.HighestCost != nil {
		highest := insights.HighestCost
		multiplier := insights.CostMultiplier()
		warningStr := fmt.Sprintf("%.1fx avg cost", multiplier)

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s  (%s)  ! %s\n",
				"Peak",
				formatCostStyled(highest.Cost, 10, noColor),
				highest.Timestamp.Format("15:04:05"),
				warningStr))
		} else {
			timestamp := dimStyle.Render(fmt.Sprintf("(%s)", highest.Timestamp.Format("15:04:05")))
			warningStyled := lipgloss.NewStyle().Foreground(styles.WarningColor).Render("⚠ " + warningStr)
			sb.WriteString(fmt.Sprintf("  %-10s %s  %s  %s\n",
				"Peak",
				formatCostStyled(highest.Cost, 10, noColor),
				timestamp,
				warningStyled))
		}
	}

	// Trend (only for sessions with 5+ messages)
	if insights.MessageCount >= 5 {
		trendDesc := insights.TrendDescription()
		trendSymbol := insights.CostTrend.Symbol()

		earlyStr := fmt.Sprintf("$%.2f/msg", insights.EarlyAvgCost)
		lateStr := fmt.Sprintf("$%.2f/msg", insights.LateAvgCost)

		if noColor {
			sb.WriteString(fmt.Sprintf("  %-10s %s -> %s  %s %s\n",
				"Trend",
				earlyStr,
				lateStr,
				trendSymbol,
				trendDesc))
		} else {
			// Color the trend symbol based on direction
			var symbolStyled string
			switch insights.CostTrend {
			case models.TrendIncreasing:
				symbolStyled = lipgloss.NewStyle().Foreground(styles.WarningColor).Render(trendSymbol)
			case models.TrendDecreasing:
				symbolStyled = lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(trendSymbol)
			default:
				symbolStyled = dimStyle.Render(trendSymbol)
			}
			sb.WriteString(fmt.Sprintf("  %-10s %s → %s  %s %s\n",
				"Trend",
				earlyStr,
				lateStr,
				symbolStyled,
				trendDesc))
		}
	}

	return sb.String()
}

// formatCostStyledGreen returns a cost string with green styling for savings
func formatCostStyledGreen(cost float64, width int, noColor bool) string {
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

	padding := ""
	if width > plainLen {
		padding = strings.Repeat(" ", width-plainLen)
	}

	if noColor {
		return padding + full
	}

	// Split into main ($X.XX) and extra (XXXX) parts
	dotIdx := strings.Index(full, ".")
	if dotIdx == -1 || len(full) <= dotIdx+3 {
		return padding + savingsValueStyle.Render(full)
	}

	main := full[:dotIdx+3]
	extra := full[dotIdx+3:]

	return padding + savingsValueStyle.Render(main) + dimStyle.Render(extra)
}

// formatCostStyledBoldGreen returns a cost string in bold green (for totals/subtotals)
func formatCostStyledBoldGreen(cost float64, width int, noColor bool) string {
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

	padding := ""
	if width > plainLen {
		padding = strings.Repeat(" ", width-plainLen)
	}

	if noColor {
		return padding + full
	}

	// Split into main ($X.XX) and extra (XXXX) parts
	dotIdx := strings.Index(full, ".")
	if dotIdx == -1 || len(full) <= dotIdx+3 {
		return padding + styles.TotalValueStyle.Render(full)
	}

	main := full[:dotIdx+3]
	extra := full[dotIdx+3:]

	return padding + styles.TotalValueStyle.Render(main) + dimStyle.Render(extra)
}

// FormatSessionListTable formats a list of sessions as a table
func FormatSessionListTable(entries []models.SessionEntry, noColor bool) string {
	var sb strings.Builder
	const width = 76

	// Header panel
	sessionWord := "sessions"
	if len(entries) == 1 {
		sessionWord = "session"
	}
	headerText := fmt.Sprintf("%d %s", len(entries), sessionWord)

	if noColor {
		// Plain header panel
		sb.WriteString(styles.BoxTopLeft)
		sb.WriteString(strings.Repeat(styles.BoxHorizontal, width-2))
		sb.WriteString(styles.BoxTopRight)
		sb.WriteString("\n")

		sb.WriteString(styles.BoxVertical)
		sb.WriteString("  ")
		sb.WriteString(headerText)
		sb.WriteString(strings.Repeat(" ", width-6-len(headerText)))
		sb.WriteString("  ")
		sb.WriteString(styles.BoxVertical)
		sb.WriteString("\n")

		sb.WriteString(styles.BoxBottomLeft)
		sb.WriteString(strings.Repeat(styles.BoxHorizontal, width-2))
		sb.WriteString(styles.BoxBottomRight)
		sb.WriteString("\n\n")
	} else {
		// Styled header panel
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopRight))
		sb.WriteString("\n")

		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("  ")
		sb.WriteString(sectionHeaderStyle.Render(fmt.Sprintf("%d", len(entries))))
		sb.WriteString(fmt.Sprintf(" %s", sessionWord))
		sb.WriteString(strings.Repeat(" ", width-6-len(headerText)))
		sb.WriteString("  ")
		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("\n")

		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomRight))
		sb.WriteString("\n\n")
	}

	// Column headers
	colHeader := fmt.Sprintf("%-36s  %8s  %7s  %s", "Session ID", "Messages", "Agents", "Modified")
	if noColor {
		sb.WriteString(colHeader + "\n")
		sb.WriteString(strings.Repeat("-", width) + "\n")
	} else {
		sb.WriteString(dimStyle.Render(colHeader) + "\n")
		sb.WriteString(dimStyle.Render(strings.Repeat(styles.LineHorizontal, width)) + "\n")
	}

	// Session rows
	for _, entry := range entries {
		modified := entry.Modified.Format("2006-01-02 15:04")
		agentStr := "-"
		if entry.AgentCount > 0 {
			agentStr = fmt.Sprintf("%d (%d)", entry.AgentCount, entry.AgentMessageCount)
		}

		// Truncate session ID to fit (first 8 chars is usually enough to identify)
		shortID := entry.SessionID
		if len(shortID) > 36 {
			shortID = shortID[:33] + "..."
		}

		if noColor {
			sb.WriteString(fmt.Sprintf("%-36s  %8d  %7s  %s\n",
				shortID,
				entry.MessageCount,
				agentStr,
				modified))
		} else {
			// Dim the session ID, highlight message count if > 0
			var msgStr string
			if entry.MessageCount > 0 {
				msgStr = fmt.Sprintf("%8d", entry.MessageCount)
			} else {
				msgStr = dimStyle.Render(fmt.Sprintf("%8d", entry.MessageCount))
			}
			sb.WriteString(fmt.Sprintf("%s  %s  %7s  %s\n",
				dimStyle.Render(fmt.Sprintf("%-36s", shortID)),
				msgStr,
				agentStr,
				dimStyle.Render(modified)))
		}
	}

	// Footer separator
	sb.WriteString("\n")
	if noColor {
		sb.WriteString(strings.Repeat("-", width))
	} else {
		sb.WriteString(dimStyle.Render(strings.Repeat(styles.LineHorizontal, width)))
	}

	return sb.String()
}

// Helper functions

func formatCost(cost float64) string {
	// Plain format with 6 decimal places
	return fmt.Sprintf("$%.6f", cost)
}

// formatCostStyled returns a cost string with extra precision (after 2 decimals) dimmed
func formatCostStyled(cost float64, width int, noColor bool) string {
	// Format to 6 decimal places: "$123.456789"
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

	// Calculate padding needed
	padding := ""
	if width > plainLen {
		padding = strings.Repeat(" ", width-plainLen)
	}

	if noColor {
		return padding + full
	}

	// Split into main ($X.XX) and extra (XXXX) parts
	dotIdx := strings.Index(full, ".")
	if dotIdx == -1 || len(full) <= dotIdx+3 {
		return padding + full
	}

	main := full[:dotIdx+3]  // "$123.45"
	extra := full[dotIdx+3:] // "6789"

	return padding + main + styles.DimStyle.Render(extra)
}

func formatNumber(n int64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.2fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

func truncateID(id string) string {
	if len(id) <= 40 {
		return id
	}
	return id[:37] + "..."
}

// formatCostComponentLabel returns a human-readable label for a cost component
func formatCostComponentLabel(component string) string {
	switch component {
	case "input":
		return "input"
	case "output":
		return "output"
	case "cache_write_5m":
		return "cache_write"
	case "cache_write_1h":
		return "cache_write"
	case "cache_read":
		return "cache_read"
	default:
		return component
	}
}

// formatContextProgressBar creates a visual progress bar showing context usage
// Bar segments: used (█), free (░), buffer (▒)
// Total width: 40 characters (TUI uses barWidth=38 for narrower panel)
func formatContextProgressBar(contextSize, freeSpace, buffer int64, maxContext int, noColor bool) string {
	const barWidth = 40

	if maxContext == 0 {
		return strings.Repeat("░", barWidth)
	}

	// Calculate proportions
	usedRatio := float64(contextSize) / float64(maxContext)
	freeRatio := float64(freeSpace) / float64(maxContext)

	// Convert to bar segments
	usedChars := int(usedRatio * float64(barWidth))
	freeChars := int(freeRatio * float64(barWidth))
	bufferChars := barWidth - usedChars - freeChars

	// Ensure we don't go negative due to rounding
	if bufferChars < 0 {
		bufferChars = 0
	}
	// Adjust for rounding to hit exactly barWidth
	total := usedChars + freeChars + bufferChars
	if total < barWidth {
		freeChars += barWidth - total
	} else if total > barWidth {
		if freeChars > 0 {
			freeChars -= total - barWidth
		} else if usedChars > 0 {
			usedChars -= total - barWidth
		}
	}

	// Guard against negative values from rounding adjustments and ensure total == barWidth
	usedChars = max(0, usedChars)
	freeChars = max(0, freeChars)
	bufferChars = max(0, bufferChars)
	if total := usedChars + freeChars + bufferChars; total < barWidth {
		freeChars += barWidth - total
	}

	usedStr := strings.Repeat("█", usedChars)
	freeStr := strings.Repeat("░", freeChars)
	bufferStr := strings.Repeat("▒", bufferChars)

	if noColor {
		return "[" + usedStr + freeStr + bufferStr + "]"
	}

	// Get usage color based on percentage - only the used portion is colored
	usagePct := usedRatio * 100
	usageColor := styles.GetContextUsageColor(usagePct)

	// Free space is neutral light gray for contrast, buffer is darker gray
	usedStyled := lipgloss.NewStyle().Foreground(usageColor).Render(usedStr)
	freeStyled := lipgloss.NewStyle().Foreground(styles.ContextFreeColor).Render(freeStr)
	bufferStyled := lipgloss.NewStyle().Foreground(styles.ContextBufferColor).Render(bufferStr)

	return "[" + usedStyled + freeStyled + bufferStyled + "]"
}
