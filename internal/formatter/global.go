package formatter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// FormatGlobalTable renders global analysis as a table. Color and glyph choices
// are driven by noColor.
func FormatGlobalTable(analysis *models.GlobalAnalysis, noColor bool, topN int, showDetails bool) string {
	if topN < 0 {
		topN = 0
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
		sb.WriteString(renderSavingsRow(analysis.TotalCost.CacheSavings, noColor))
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
	sb.WriteString(renderFooterDoubleRule(sectionWidth, noColor))
	sb.WriteString("\n")

	sb.WriteString(footerStats([]string{
		"Total: " + render.Cost(analysis.TotalCost.TotalCost),
		"Messages: " + render.Number(int64(analysis.MessageCount)),
		fmt.Sprintf("Sessions: %d", analysis.SessionCount),
		fmt.Sprintf("Projects: %d", analysis.ProjectCount),
	}, sectionWidth, noColor))
	sb.WriteString("\n")
	sb.WriteString(renderFooterSingleRule(sectionWidth, noColor))

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
	if noColor {
		sep = styles.AsciiVertical
	}
	content := fmt.Sprintf("%s  %s  %s  %s  %s", titlePart, sep, sessionPart, sep, durationPart)
	contentLen := len(titlePart) + 2 + 1 + 2 + len(sessionPart) + 2 + 1 + 2 + len(durationPart)
	padding := innerWidth - contentLen
	if padding < 0 {
		padding = 0
	}

	if noColor {
		sb.WriteString(styles.AsciiCorner)
		sb.WriteString(strings.Repeat(styles.AsciiHorizontal, width-2))
		sb.WriteString(styles.AsciiCorner)
		sb.WriteString("\n")

		sb.WriteString(styles.AsciiVertical)
		sb.WriteString("  ")
		sb.WriteString(content)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString("  ")
		sb.WriteString(styles.AsciiVertical)
		sb.WriteString("\n")

		sb.WriteString(styles.AsciiCorner)
		sb.WriteString(strings.Repeat(styles.AsciiHorizontal, width-2))
		sb.WriteString(styles.AsciiCorner)
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
		modelLabel := render.ClampModel(modelName, 12)
		if noColor {
			sb.WriteString(fmt.Sprintf("    %-12s %s\n", modelLabel, render.Cost(cost.TotalCost)))
		} else {
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-12s", modelLabel))
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

	projectWidth := 45

	// Cost columns grow to fit their widest value, so a large total widens the
	// column for every row instead of pushing one row out of line.
	var displayed, cumulatives []float64
	var running float64
	for i := 0; i < displayCount && i < len(projects); i++ {
		c := projects[i].TotalCost.TotalCost
		running += c
		displayed = append(displayed, c)
		cumulatives = append(cumulatives, running)
	}
	costWidth := render.CostCellWidth(10, displayed...)
	cumWidth := render.CostCellWidth(10, cumulatives...)

	// Header row
	var headerRow string
	if showDetails {
		headerRow = fmt.Sprintf("  %3s   %-45s  %8s  %*s  %7s  %*s", "#", "PROJECT", "SESSIONS", costWidth, "COST", "% TOTAL", cumWidth, "CUMULATIVE")
	} else {
		headerRow = fmt.Sprintf("  %3s   %-45s  %8s  %*s  %7s", "#", "PROJECT", "SESSIONS", costWidth, "COST", "% TOTAL")
	}
	contentWidth := max(92, len(headerRow)-2)

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

		// Truncate project name with middle ellipsis if needed, then pad by
		// display width: fmt's %-Ns counts runes, so a wide (CJK/emoji) name
		// would under-pad and shift every column to its right.
		name := truncateMiddle(p.DisplayName, projectWidth)
		name += strings.Repeat(" ", projectWidth-runewidth.StringWidth(name))

		if noColor {
			costStr := render.CostCell(p.TotalCost.TotalCost, costWidth)
			pctStr := fmt.Sprintf("%.1f%%", pctTotal)
			if showDetails {
				cumStr := render.CostCell(cumulative, cumWidth)
				sb.WriteString(fmt.Sprintf("  %3d   %s  %8d  %s  %7s  %s\n",
					i+1, name, p.SessionCount, costStr, pctStr, cumStr))
			} else {
				sb.WriteString(fmt.Sprintf("  %3d   %s  %8d  %s  %7s\n",
					i+1, name, p.SessionCount, costStr, pctStr))
			}
		} else {
			costColor := styles.GetCostGradientColor(p.TotalCost.TotalCost, minCost, maxCost)
			costStyled := render.CostColored(p.TotalCost.TotalCost, costColor, costWidth)

			pctStr := fmt.Sprintf("%.1f%%", pctTotal)
			pctStyled := dimStyle.Render(fmt.Sprintf("%7s", pctStr))

			numStyled := dimStyle.Render(fmt.Sprintf("%3d", i+1))
			sessionsStyled := dimStyle.Render(fmt.Sprintf("%8d", p.SessionCount))

			if showDetails {
				cumStyled := render.CostColored(cumulative, styles.SuccessColor, cumWidth)
				sb.WriteString(fmt.Sprintf("  %s   %s  %s  %s  %s  %s\n",
					numStyled, name, sessionsStyled, costStyled, pctStyled, cumStyled))
			} else {
				sb.WriteString(fmt.Sprintf("  %s   %s  %s  %s  %s\n",
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

		summaryText := fmt.Sprintf("(%d more projects totaling %s)", remaining, render.Cost(remainingCost))
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
		"first_active",
		"last_active",
		"skipped_sessions",
		"skipped_agents",
		"skipped_lines",
		"estimated_cost_messages",
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing CSV header: %w", err)
	}

	// Write project rows
	for _, p := range analysis.Projects {
		row := []string{
			csvCell(p.DisplayName),
			fmt.Sprintf("%d", p.SessionCount),
			fmt.Sprintf("%d", p.MessageCount),
			fmt.Sprintf("%.6f", p.TotalCost.InputCost),
			fmt.Sprintf("%.6f", p.TotalCost.OutputCost),
			fmt.Sprintf("%.6f", p.TotalCost.CacheWrite5mCost),
			fmt.Sprintf("%.6f", p.TotalCost.CacheWrite1hCost),
			fmt.Sprintf("%.6f", p.TotalCost.CacheReadCost),
			fmt.Sprintf("%.6f", p.TotalCost.TotalCost),
			fmt.Sprintf("%.6f", p.TotalCost.CacheSavings),
			p.FirstActive.Format("2006-01-02T15:04:05Z07:00"),
			p.LastActive.Format("2006-01-02T15:04:05Z07:00"),
			fmt.Sprintf("%d", p.SkippedSessions),
			fmt.Sprintf("%d", p.SkippedAgents),
			fmt.Sprintf("%d", p.SkippedLines),
			fmt.Sprintf("%d", p.EstimatedCostMessages),
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
