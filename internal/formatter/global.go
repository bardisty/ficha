package formatter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/pricing"
	"github.com/bardisty/ccusage/internal/render"
	"github.com/bardisty/ccusage/internal/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// FormatGlobalTable formats global analysis as a styled table
func FormatGlobalTable(analysis *models.GlobalAnalysis, noColor bool, topN int, showDetails bool) string {
	if topN < 0 {
		topN = 0
	}
	if noColor {
		return formatGlobalTablePlain(analysis, topN, showDetails)
	}

	var sb strings.Builder
	const sectionWidth = 96

	// Header panel
	sb.WriteString(renderGlobalHeaderPanel(analysis, sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Hero total cost
	sb.WriteString(renderHeroCost(analysis.TotalCost.TotalCost, sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Token breakdown rows
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
		savingsStr := formatCostStyledGreen(analysis.TotalCost.CacheSavings, 11, noColor)
		sb.WriteString(fmt.Sprintf("  %s %s  %s\n",
			savingsLabelStyle.Render(fmt.Sprintf("%-14s", "Savings")),
			savingsStr,
			dimStyle.Render("(from cache reads)")))
	}

	// Cost by model section
	sb.WriteString("\n")
	sb.WriteString(render.SectionHeader("COST BY MODEL", sectionWidth, noColor))
	sb.WriteString("\n\n")
	sb.WriteString(formatGlobalCostByModel(analysis.CostByModel, noColor))

	// Top projects section
	sb.WriteString("\n")
	projectsShown := topN
	if showDetails || topN >= len(analysis.Projects) {
		projectsShown = len(analysis.Projects)
	}
	headerText := fmt.Sprintf("TOP PROJECTS (%d)", projectsShown)
	if showDetails {
		headerText = fmt.Sprintf("ALL PROJECTS (%d)", len(analysis.Projects))
	}
	sb.WriteString(render.SectionHeader(headerText, sectionWidth, noColor))
	sb.WriteString("\n\n")
	sb.WriteString(renderProjectsTable(analysis, noColor, topN, showDetails))

	// Footer
	sb.WriteString("\n")
	sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, sectionWidth)))
	sb.WriteString("\n")

	footerText := fmt.Sprintf("Messages: %s  │  Sessions: %d  │  Projects: %d",
		render.Number(int64(analysis.MessageCount)),
		analysis.SessionCount,
		analysis.ProjectCount)
	sb.WriteString(footerStyle.Render(footerText))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render(strings.Repeat(styles.LineHorizontal, sectionWidth)))

	return sb.String()
}

// formatGlobalTablePlain formats global analysis as plain text
func formatGlobalTablePlain(analysis *models.GlobalAnalysis, topN int, showDetails bool) string {
	var sb strings.Builder
	const sectionWidth = 96

	// Header panel
	sb.WriteString(renderGlobalHeaderPanel(analysis, sectionWidth, true))
	sb.WriteString("\n\n")

	// Hero total cost
	sb.WriteString(renderHeroCost(analysis.TotalCost.TotalCost, sectionWidth, true))
	sb.WriteString("\n\n")

	// Token breakdown rows
	sb.WriteString(renderUnifiedCostRowPlain("Input", analysis.TotalCost.InputCost, analysis.TotalUsage.InputTokens, ""))
	sb.WriteString(renderUnifiedCostRowPlain("Output", analysis.TotalCost.OutputCost, analysis.TotalUsage.OutputTokens, ""))

	cache5mTokens, cache1hTokens := render.CacheTokensByTTL(analysis.TotalUsage)
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

	if analysis.TotalCost.CacheSavings > 0 {
		sb.WriteString(fmt.Sprintf("  %-14s %11s  (from cache reads)\n",
			"Savings", render.Cost(analysis.TotalCost.CacheSavings)))
	}

	// Cost by model section
	sb.WriteString("\n")
	sb.WriteString(render.SectionHeader("COST BY MODEL", sectionWidth, true))
	sb.WriteString("\n\n")
	sb.WriteString(formatGlobalCostByModel(analysis.CostByModel, true))

	// Top projects section
	sb.WriteString("\n")
	projectsShown := topN
	if showDetails || topN >= len(analysis.Projects) {
		projectsShown = len(analysis.Projects)
	}
	headerText := fmt.Sprintf("TOP PROJECTS (%d)", projectsShown)
	if showDetails {
		headerText = fmt.Sprintf("ALL PROJECTS (%d)", len(analysis.Projects))
	}
	sb.WriteString(render.SectionHeader(headerText, sectionWidth, true))
	sb.WriteString("\n\n")
	sb.WriteString(renderProjectsTable(analysis, true, topN, showDetails))

	// Footer
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("=", sectionWidth))
	sb.WriteString("\n")

	footerText := fmt.Sprintf("Messages: %s  |  Sessions: %d  |  Projects: %d",
		render.Number(int64(analysis.MessageCount)),
		analysis.SessionCount,
		analysis.ProjectCount)
	sb.WriteString(footerText)
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", sectionWidth))

	return sb.String()
}

// renderGlobalHeaderPanel renders the header panel for global stats
func renderGlobalHeaderPanel(analysis *models.GlobalAnalysis, width int, noColor bool) string {
	var sb strings.Builder

	if width < 40 {
		width = 76
	}

	innerWidth := width - 6

	// Build content
	titlePart := fmt.Sprintf("Global: %d projects", analysis.ProjectCount)
	sessionPart := fmt.Sprintf("%d sessions", analysis.SessionCount)
	durationPart := fmt.Sprintf("Duration: %s", render.DurationLong(analysis.Duration.Duration()))

	sep := styles.BoxVerticalSep
	content := fmt.Sprintf("%s  %s  %s  %s  %s", titlePart, sep, sessionPart, sep, durationPart)
	contentLen := len(titlePart) + 2 + 1 + 2 + len(sessionPart) + 2 + 1 + 2 + len(durationPart)
	padding := innerWidth - contentLen
	if padding < 0 {
		padding = 0
	}

	if noColor {
		sb.WriteString(styles.BoxTopLeft)
		sb.WriteString(strings.Repeat(styles.BoxHorizontal, width-2))
		sb.WriteString(styles.BoxTopRight)
		sb.WriteString("\n")

		sb.WriteString(styles.BoxVertical)
		sb.WriteString("  ")
		sb.WriteString(content)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString("  ")
		sb.WriteString(styles.BoxVertical)
		sb.WriteString("\n")

		sb.WriteString(styles.BoxBottomLeft)
		sb.WriteString(strings.Repeat(styles.BoxHorizontal, width-2))
		sb.WriteString(styles.BoxBottomRight)
	} else {
		titleStyled := fmt.Sprintf("%s %d projects",
			sectionHeaderStyle.Render("Global:"),
			analysis.ProjectCount)
		sepStyled := panelBorderStyle.Render(sep)

		sb.WriteString(panelBorderStyle.Render(styles.BoxTopLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopRight))
		sb.WriteString("\n")

		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("  ")
		sb.WriteString(titleStyled)
		sb.WriteString("  ")
		sb.WriteString(sepStyled)
		sb.WriteString(fmt.Sprintf("  %s  ", sessionPart))
		sb.WriteString(sepStyled)
		sb.WriteString(fmt.Sprintf("  %s", durationPart))
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString("  ")
		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("\n")

		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomRight))
	}

	return sb.String()
}

// formatGlobalCostByModel renders cost by model for global stats
func formatGlobalCostByModel(costByModel map[string]models.CostBreakdown, noColor bool) string {
	var sb strings.Builder

	// Highest-cost model first.
	for _, modelID := range render.OrderModelsByCost(costByModel) {
		cost := costByModel[modelID]
		modelName := pricing.GetModelDisplayName(modelID)
		if noColor {
			sb.WriteString(fmt.Sprintf("    %-12s %s\n", modelName, render.Cost(cost.TotalCost)))
		} else {
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-12s", modelName))
			sb.WriteString(fmt.Sprintf("    %s %s\n", modelStyled, formatCostStyled(cost.TotalCost, 12, noColor)))
		}
	}

	return sb.String()
}

// renderProjectsTable renders the projects breakdown table
func renderProjectsTable(analysis *models.GlobalAnalysis, noColor bool, topN int, showDetails bool) string {
	var sb strings.Builder

	projects := analysis.Projects
	displayCount := topN
	if displayCount < 0 {
		displayCount = 0
	}
	if showDetails || displayCount >= len(projects) {
		displayCount = len(projects)
	}

	// Calculate min/max for gradient by scanning — projects may be re-sorted
	// by any key (--sort-by), so positional first/last are not cost extremes
	var minCost, maxCost float64
	if len(projects) > 0 {
		minCost = projects[0].TotalCost.TotalCost
		maxCost = minCost
		for _, p := range projects[1:] {
			c := p.TotalCost.TotalCost
			if c < minCost {
				minCost = c
			}
			if c > maxCost {
				maxCost = c
			}
		}
	}

	contentWidth := 92
	projectWidth := 45

	// Header row
	var headerRow string
	if showDetails {
		headerRow = fmt.Sprintf("  %3s   %-45s  %8s  %10s  %7s  %10s", "#", "PROJECT", "SESSIONS", "COST", "% TOTAL", "CUMULATIVE")
	} else {
		headerRow = fmt.Sprintf("  %3s   %-45s  %8s  %10s  %7s", "#", "PROJECT", "SESSIONS", "COST", "% TOTAL")
	}

	if noColor {
		sb.WriteString(headerRow + "\n")
		sb.WriteString("  " + strings.Repeat("-", contentWidth) + "\n")
	} else {
		sb.WriteString(headerStyle.Render(headerRow) + "\n")
		sb.WriteString("  " + dimStyle.Render(strings.Repeat(styles.LineHorizontal, contentWidth)) + "\n")
	}

	// Project rows
	var cumulative float64
	for i := 0; i < displayCount && i < len(projects); i++ {
		p := projects[i]
		cumulative += p.TotalCost.TotalCost
		pctTotal := 0.0
		if analysis.TotalCost.TotalCost > 0 {
			pctTotal = (p.TotalCost.TotalCost / analysis.TotalCost.TotalCost) * 100
		}

		// Truncate project name with middle ellipsis if needed
		name := truncateMiddle(p.DisplayName, projectWidth)

		if noColor {
			costStr := fmt.Sprintf("$%.2f", p.TotalCost.TotalCost)
			pctStr := fmt.Sprintf("%.1f%%", pctTotal)
			if showDetails {
				cumStr := fmt.Sprintf("$%.2f", cumulative)
				sb.WriteString(fmt.Sprintf("  %3d   %-45s  %8d  %10s  %7s  %10s\n",
					i+1, name, p.SessionCount, costStr, pctStr, cumStr))
			} else {
				sb.WriteString(fmt.Sprintf("  %3d   %-45s  %8d  %10s  %7s\n",
					i+1, name, p.SessionCount, costStr, pctStr))
			}
		} else {
			costColor := styles.GetCostGradientColor(p.TotalCost.TotalCost, minCost, maxCost)
			costStr := fmt.Sprintf("$%.2f", p.TotalCost.TotalCost)
			costStyled := lipgloss.NewStyle().Foreground(costColor).Render(fmt.Sprintf("%10s", costStr))

			pctStr := fmt.Sprintf("%.1f%%", pctTotal)
			pctStyled := dimStyle.Render(fmt.Sprintf("%7s", pctStr))

			numStyled := dimStyle.Render(fmt.Sprintf("%3d", i+1))
			sessionsStyled := dimStyle.Render(fmt.Sprintf("%8d", p.SessionCount))

			if showDetails {
				cumStr := fmt.Sprintf("$%.2f", cumulative)
				cumStyled := lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(fmt.Sprintf("%10s", cumStr))
				sb.WriteString(fmt.Sprintf("  %s   %-45s  %s  %s  %s  %s\n",
					numStyled, name, sessionsStyled, costStyled, pctStyled, cumStyled))
			} else {
				sb.WriteString(fmt.Sprintf("  %s   %-45s  %s  %s  %s\n",
					numStyled, name, sessionsStyled, costStyled, pctStyled))
			}
		}
	}

	// Footer separator and summary of remaining projects
	if noColor {
		sb.WriteString("  " + strings.Repeat("-", contentWidth) + "\n")
	} else {
		sb.WriteString("  " + dimStyle.Render(strings.Repeat(styles.LineHorizontal, contentWidth)) + "\n")
	}

	// Show remaining projects summary if not showing all
	if !showDetails && displayCount < len(projects) {
		remaining := len(projects) - displayCount
		var remainingCost float64
		for i := displayCount; i < len(projects); i++ {
			remainingCost += projects[i].TotalCost.TotalCost
		}

		summaryText := fmt.Sprintf("(%d more projects totaling $%.2f)", remaining, remainingCost)
		if noColor {
			sb.WriteString(fmt.Sprintf("  %s\n", summaryText))
		} else {
			sb.WriteString(fmt.Sprintf("  %s\n", dimStyle.Render(summaryText)))
		}
	}

	return sb.String()
}

// truncateMiddle truncates a string in the middle, showing start...end.
// Operates on runes and display width so multi-byte and wide (CJK) characters
// are never split mid-character.
func truncateMiddle(s string, maxWidth int) string {
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	// Reserve 3 cells for "..."
	available := maxWidth - 3
	if available < 1 {
		// No room for start...end; hard-truncate to width
		return runewidth.Truncate(s, maxWidth, "")
	}
	// Split roughly 60/40 favoring the end (project name is usually more distinctive)
	startWidth := available * 2 / 5
	endWidth := available - startWidth

	runes := []rune(s)
	start, w := 0, 0
	for _, r := range runes {
		rw := runewidth.RuneWidth(r)
		if w+rw > startWidth {
			break
		}
		w += rw
		start++
	}
	end, w := len(runes), 0
	for i := len(runes) - 1; i >= 0; i-- {
		rw := runewidth.RuneWidth(runes[i])
		if w+rw > endWidth {
			break
		}
		w += rw
		end--
	}
	return string(runes[:start]) + "..." + string(runes[end:])
}

// FormatGlobalJSON formats global analysis as JSON
func FormatGlobalJSON(analysis *models.GlobalAnalysis, pretty bool) (string, error) {
	var data []byte
	var err error

	if pretty {
		data, err = json.MarshalIndent(analysis, "", "  ")
	} else {
		data, err = json.Marshal(analysis)
	}

	if err != nil {
		return "", err
	}

	return string(data), nil
}

// FormatGlobalCSV formats global analysis as CSV
func FormatGlobalCSV(analysis *models.GlobalAnalysis) (string, error) {
	var sb strings.Builder
	w := csv.NewWriter(&sb)

	// Write header
	header := []string{
		"project",
		"sessions",
		"messages",
		"input_cost",
		"output_cost",
		"cache_write_5m_cost",
		"cache_write_1h_cost",
		"cache_read_cost",
		"total_cost",
		"cache_savings",
		"last_active",
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing CSV header: %w", err)
	}

	// Write project rows
	for _, p := range analysis.Projects {
		row := []string{
			p.DisplayName,
			fmt.Sprintf("%d", p.SessionCount),
			fmt.Sprintf("%d", p.MessageCount),
			fmt.Sprintf("%.6f", p.TotalCost.InputCost),
			fmt.Sprintf("%.6f", p.TotalCost.OutputCost),
			fmt.Sprintf("%.6f", p.TotalCost.CacheWrite5mCost),
			fmt.Sprintf("%.6f", p.TotalCost.CacheWrite1hCost),
			fmt.Sprintf("%.6f", p.TotalCost.CacheReadCost),
			fmt.Sprintf("%.6f", p.TotalCost.TotalCost),
			fmt.Sprintf("%.6f", p.TotalCost.CacheSavings),
			p.LastActive.Format("2006-01-02T15:04:05Z07:00"),
		}
		if err := w.Write(row); err != nil {
			return "", fmt.Errorf("writing CSV row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("flushing CSV: %w", err)
	}

	return sb.String(), nil
}
