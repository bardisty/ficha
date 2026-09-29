package formatter

import (
	"fmt"
	"regexp"
	"strings"
	"time"

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

// trailingPad matches spaces at the end of a line, before any SGR codes
// that close it. A two-decimal cost pads two spaces after itself to keep
// decimal points aligned, which leaves them trailing when it ends a line.
var trailingPad = regexp.MustCompile(` +((?:\x1b\[[0-9;]*m)*)$`)

// trimLineEnds drops the padding at the end of each line of a report, keeping
// any escape codes that end it.
func trimLineEnds(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = trailingPad.ReplaceAllString(l, "$1")
	}
	return strings.Join(lines, "\n")
}

// now is the clock reports read for relative times and for whether a date
// needs its year. Tests pin it.
var now = time.Now

// renderHeaderPanel renders the boxed header of show and summary:
//
//	╔══════════════════════════════════════════════════════════════════════════╗
//	║  ~/source/webapp  │  Session: 68994c84  │  Duration: 3h 12m              ║
//	╚══════════════════════════════════════════════════════════════════════════╝
//
// A summary spans many sessions, so it gives their count and the span from
// the first message to the last as dates, rather than a duration that reads
// like time spent.
func renderHeaderPanel(analysis *models.SessionAnalysis, width int, noColor bool) string {
	if analysis.IsSummary {
		return renderPanel(analysis.Project, []string{
			fmt.Sprintf("%d %s", analysis.SessionCount, sessionsWord(analysis.SessionCount)),
			"Span: " + span(analysis.StartTime, analysis.EndTime),
		}, width, noColor)
	}
	return renderPanel(analysis.Project, []string{
		"Session: " + render.TruncateID(analysis.SessionID, 8),
		"Duration: " + render.Duration(analysis.Duration.Duration()),
	}, width, noColor)
}

// span renders a first-to-last range as dates, "Sep 08 → Sep 28", or one
// date when both fall on the same day.
func span(first, last time.Time) string {
	from, to := render.Date(first, now()), render.Date(last, now())
	if from == to {
		return from
	}
	return from + " " + styles.Arrow + " " + to
}

// renderPanel draws the boxed header every static report opens with: the
// lead (the project, highlighted) and the other fields, separated by rules.
// The lead gives way first when the fields don't fit, losing its head like
// a project path in the global table, then trailing fields go. An empty lead
// is left out.
func renderPanel(lead string, fields []string, width int, noColor bool) string {
	if width < 40 {
		width = 76
	}
	inner := width - 6 // 2 for borders, 2 for padding on each side
	sep := "  " + styles.BoxVerticalSep + "  "
	// On a narrow terminal the last fields give way before the box breaks.
	for len(fields) > 1 && lipgloss.Width(strings.Join(fields, sep)) > inner {
		fields = fields[:len(fields)-1]
	}
	rest := strings.Join(fields, sep)
	if lead != "" {
		room := inner - lipgloss.Width(rest) - lipgloss.Width(sep)
		if room < minProjectWidth {
			lead = ""
		} else {
			lead = truncateLeft(lead, room)
		}
	}
	content := rest
	styled := rest
	if lead != "" {
		content = lead + sep + rest
		styled = sectionHeaderStyle.Render(lead) + panelBorderStyle.Render(sep) + rest
		if noColor {
			styled = content
		}
	}
	pad := strings.Repeat(" ", max(inner-lipgloss.Width(content), 0))

	top := styles.BoxTopLeft + strings.Repeat(styles.BoxHorizontal, width-2) + styles.BoxTopRight
	bottom := styles.BoxBottomLeft + strings.Repeat(styles.BoxHorizontal, width-2) + styles.BoxBottomRight
	if noColor {
		return top + "\n" + styles.BoxVertical + "  " + content + pad + "  " + styles.BoxVertical + "\n" + bottom
	}
	side := panelBorderStyle.Render(styles.BoxVertical)
	return panelBorderStyle.Render(top) + "\n" + side + "  " + styled + pad + "  " + side + "\n" + panelBorderStyle.Render(bottom)
}

// renderHeroCost renders the total cost integrated into a section header.
// It's the report's most prominent figure, so it's where the output says the
// number is a list-price estimate and not a bill.
// Format: ──────────[ $12.67 API-equivalent estimate ]──────────
func renderHeroCost(cost float64, width int, noColor bool) string {
	costStr := render.Cost(cost) + " API-equivalent estimate"
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

// messagesField is the footer's message count, split into parent and agent
// messages when agents ran.
func messagesField(total, parent, agents int) string {
	if agents == 0 {
		return "Messages: " + render.Count(total)
	}
	return fmt.Sprintf("Messages: %s (%s parent, %s agents)", render.Count(total), render.Count(parent), render.Count(agents))
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
