package formatter

import (
	"fmt"
	"strings"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
)

// Local aliases for frequently used styles
var (
	savingsLabelStyle  = styles.SavingsLabelStyle
	footerStyle        = styles.FooterStyle
	heroCostStyle      = styles.HeroCostStyle
	sectionHeaderStyle = styles.SectionHeaderStyle
	panelBorderStyle   = styles.PanelBorderStyle
	dimStyle           = styles.DimStyle
	headerStyle        = styles.HeaderStyle
)

// sessionsWord returns "session" or "sessions" for a count, keeping every
// surface that prints one grammatically consistent.
func sessionsWord(n int) string {
	if n == 1 {
		return "session"
	}
	return "sessions"
}

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
	isSummary := analysis.IsSummary

	// Build content parts - use consistent format for both plain and styled
	var titlePart string
	var sessionCount int
	var sessionWord string
	if isSummary {
		sessionCount = analysis.SessionCount
		sessionWord = sessionsWord(sessionCount)
		titlePart = fmt.Sprintf("Summary: %d %s", sessionCount, sessionWord)
	} else {
		titlePart = fmt.Sprintf("Session: %s", render.TruncateID(analysis.SessionID, 40))
	}

	// Summaries aggregate many sessions and can span days, so they use the
	// day-aware format; a single session stays in hours.
	durationValue := render.Duration(analysis.Duration.Duration())
	if isSummary {
		durationValue = render.DurationLong(analysis.Duration.Duration())
	}
	durationPart := fmt.Sprintf("Duration: %s", durationValue)

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
			titleStyled = fmt.Sprintf("%s %d %s",
				sectionHeaderStyle.Render("Summary:"),
				sessionCount, sessionWord)
		} else {
			titleStyled = fmt.Sprintf("%s %s",
				sectionHeaderStyle.Render("Session:"),
				render.TruncateID(analysis.SessionID, 40))
		}

		durationStyled := fmt.Sprintf("Duration: %s", durationValue)
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

// renderHeroCost renders the total cost integrated into a section header
// Format: ─────────────────────[ $12.67 TOTAL ]─────────────────────
func renderHeroCost(cost float64, width int, noColor bool) string {
	costStr := render.Cost(cost) + " TOTAL"
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

	rule := styles.LineHorizontal
	leftLine := strings.Repeat(rule, sideLen)
	rightLine := strings.Repeat(rule, rightLen)

	if noColor {
		return leftLine + bracketedCost + rightLine
	}

	return dimStyle.Render(leftLine) + "[ " + heroCostStyle.Render(costStr) + " ]" + dimStyle.Render(rightLine)
}

// renderUnifiedCostRow renders a single row with cost and token info combined
// Format: "  Label            $0.3710      53.9K tokens"
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
	tokenStr := fmt.Sprintf("%12s", render.Number(tokens))

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

// renderSavingsRow renders the cache-savings line shared by the session,
// summary, and global tables (2-space indent, 11-wide cost). In no-color mode
// the label and parenthetical are plain; otherwise the label is cyan and the
// value green.
func renderSavingsRow(savings float64, noColor bool) string {
	if noColor {
		return fmt.Sprintf("  %-14s %s  (from cache reads)\n", "Savings", render.CostCell(savings, 11))
	}
	return fmt.Sprintf("  %s %s  %s\n",
		savingsLabelStyle.Render(fmt.Sprintf("%-14s", "Savings")),
		formatCostStyledGreen(savings, 11, noColor),
		dimStyle.Render("(from cache reads)"))
}

// footerSep is the field separator used inside footer stat lines.
func footerSep() string {
	return styles.BoxVerticalSep
}

// footerStats joins footer fields with footerSep, starting a new line when the
// next field would run past width. Callers put the total first, so it's on
// screen whenever the footer is.
func footerStats(fields []string, width int, noColor bool) string {
	sep := "  " + footerSep() + "  "
	var lines []string
	line := ""
	for _, f := range fields {
		switch {
		case line == "":
			line = f
		case lipgloss.Width(line+sep+f) > width:
			lines = append(lines, line)
			line = f
		default:
			line += sep + f
		}
	}
	lines = append(lines, line)
	if !noColor {
		for i, l := range lines {
			lines[i] = footerStyle.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

// renderFooterDoubleRule renders the heavy separator that closes a table body.
func renderFooterDoubleRule(width int, noColor bool) string {
	if noColor {
		return strings.Repeat(styles.BoxHorizontal, width)
	}
	return panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width))
}

// renderFooterSingleRule renders the light rule drawn under the footer stats.
func renderFooterSingleRule(width int, noColor bool) string {
	if noColor {
		return strings.Repeat(styles.LineHorizontal, width)
	}
	return dimStyle.Render(strings.Repeat(styles.LineHorizontal, width))
}

// formatCostByModelContent renders cost by model rows (content only, no header)
func formatCostByModelContent(analysis *models.SessionAnalysis, noColor bool) string {
	var sb strings.Builder

	// Highest-cost model first.
	for _, modelID := range render.OrderModelsByCost(analysis.CostByModel) {
		cost := analysis.CostByModel[modelID]
		modelName := pricing.GetModelDisplayName(modelID)
		modelLabel := render.ClampModel(modelName, 12)
		if noColor {
			sb.WriteString(fmt.Sprintf("    %-12s %s\n", modelLabel, render.CostCell(cost.TotalCost, 12)))
		} else {
			// Color by model tier
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-12s", modelLabel))
			sb.WriteString(fmt.Sprintf("    %s %s\n", modelStyled, formatCostStyled(cost.TotalCost, 12, noColor)))
		}
	}

	return sb.String()
}

// The static formatter never highlights changed values, so these wrappers pin
// render's highlighted parameter to false.
func formatCostStyled(cost float64, width int, noColor bool) string {
	return render.CostStyled(cost, width, false, noColor)
}

func formatCostStyledGreen(cost float64, width int, noColor bool) string {
	return render.CostStyledGreen(cost, width, false, noColor)
}

func formatCostStyledBoldGreen(cost float64, width int, noColor bool) string {
	return render.CostStyledBoldGreen(cost, width, false, noColor)
}
