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
var trailingPad = regexp.MustCompile(`(?:\x1b\[[0-9;]*m| )+$`)

var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// trimLineEnds drops the padding at the end of each line of a report, keeping
// any escape codes among it, which may close a style opened earlier.
func trimLineEnds(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = trailingPad.ReplaceAllStringFunc(l, func(tail string) string {
			return strings.Join(sgr.FindAllString(tail, -1), "")
		})
	}
	return strings.Join(lines, "\n")
}

// now is the clock reports read for relative times and for whether a date
// needs its year. Tests pin it.
var now = time.Now

// Widths of show and summary. Piped, they draw at staticReportWidth so
// redirected output doesn't depend on the terminal it came from. On a
// narrower terminal they fit it, and from 50 columns up nothing in them runs
// past the frame. Below that the frame follows the terminal down to
// minReportWidth, which most reports fit, though a year in MODIFIED, a wide
// cost or a four-digit message count can still run past it. Narrower than
// that, the frame stays put rather than draw narrower than what's in it. A
// wider terminal keeps staticReportWidth, since nothing here needs the room.
const (
	staticReportWidth = 76
	minReportWidth    = 45
)

// reportWidth is the width show and summary draw at, given the terminal's
// width, or 0 when stdout isn't a terminal.
func reportWidth(terminal int) int {
	if terminal <= 0 {
		return staticReportWidth
	}
	return min(max(terminal, minReportWidth), staticReportWidth)
}

// renderHeaderPanel renders the boxed header of show and summary:
//
//	╔══════════════════════════════════════════════════════════════════════════╗
//	║  ~/source/webapp  │  Session: 68994c84  │  Duration: 3h 12m              ║
//	╚══════════════════════════════════════════════════════════════════════════╝
//
// A summary spans many sessions, so it gives their count and the span from
// the first message to the last as dates, rather than a duration that reads
// like time spent.
//
// When the box is too narrow, a show's duration gives way before its
// project, and so does a summary's span on a narrowed report; at the piped
// width a summary's project goes first, as it always has. A windowed
// summary loses its project, then its session count, but never the window:
// without it the figures read as all-time.
func renderHeaderPanel(analysis *models.SessionAnalysis, width int, noColor bool) string {
	if analysis.IsSummary {
		giveWay := []int{panelLead, 1}
		switch {
		case analysis.Window != nil && !analysis.Window.IsZero():
			giveWay = []int{panelLead, 0}
		case width < staticReportWidth:
			giveWay = []int{1, panelLead}
		}
		when := spanOrWindow(analysis.Window, analysis.StartTime, analysis.EndTime)
		// A window with years and times on both ends can outgrow a narrow
		// box on its own; its range still reads as one without the label.
		if width < staticReportWidth && lipgloss.Width(when) > width-6 {
			when = strings.TrimPrefix(when, "Window: ")
		}
		return renderPanel(analysis.Project, []string{
			fmt.Sprintf("%d %s", analysis.SessionCount, sessionsWord(analysis.SessionCount)),
			when,
		}, giveWay, width, noColor)
	}
	return renderPanel(analysis.Project, []string{
		"Session: " + render.TruncateID(analysis.SessionID, 8),
		"Duration: " + render.Duration(analysis.Duration.Duration()),
	}, []int{1, panelLead}, width, noColor)
}

// spanOrWindow is the header's time field: the --since/--until window when
// one was given, since that's what the figures cover, else the span of the
// messages.
func spanOrWindow(w *models.TimeWindow, first, last time.Time) string {
	if w == nil || w.IsZero() {
		return "Span: " + span(first, last)
	}
	return "Window: " + windowLabel(*w)
}

// windowLabel renders a --since/--until window: "Sep 21 → now",
// "Sep 01 → Sep 30". An end at midnight is the exclusive bound of a whole
// day given as a date, so it shows as the day before, the one typed.
func windowLabel(w models.TimeWindow) string {
	point := func(t time.Time) string {
		l := t.Local()
		if l.Hour() == 0 && l.Minute() == 0 && l.Second() == 0 {
			return render.Date(t, now())
		}
		return render.DateTime(t, now())
	}
	from, to := "start", "now"
	if !w.Since.IsZero() {
		from = point(w.Since)
	}
	if !w.Until.IsZero() {
		until := w.Until
		l := until.Local()
		if l.Hour() == 0 && l.Minute() == 0 && l.Second() == 0 {
			until = until.AddDate(0, 0, -1)
		}
		to = point(until)
	}
	return from + " " + styles.Arrow + " " + to
}

// emptyWindow is the one line a windowed report prints when nothing falls
// inside the window, instead of a report of empty sections.
func emptyWindow(w models.TimeWindow) string {
	return "No messages in " + windowLabel(w) + "."
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

// panelLead stands for the lead in a renderPanel give-way order.
const panelLead = -1

// renderPanel draws the boxed header every static report opens with: the
// lead (the project, highlighted) and the other fields, separated by rules.
// The lead gives way first when the fields don't fit, losing its head like
// a project path in the global table. Past that, fields go, and the lead,
// in giveWay's order: field indexes, or panelLead. Whatever giveWay leaves
// out stays, and so does the last field standing. An empty lead is left out.
func renderPanel(lead string, fields []string, giveWay []int, width int, noColor bool) string {
	if width <= 0 {
		width = 76
	}
	inner := width - 6 // 2 for borders, 2 for padding on each side
	sep := "  " + styles.BoxVerticalSep + "  "
	lead = stripControl(lead)
	shown := make([]bool, len(fields))
	for i := range shown {
		shown[i] = true
	}
	// need is the room the shown fields take, with the lead cut to its
	// narrowest.
	need := func() int {
		var parts []string
		for i, f := range fields {
			if shown[i] {
				parts = append(parts, f)
			}
		}
		if lead != "" {
			parts = append(parts, strings.Repeat(" ", minProjectWidth))
		}
		return lipgloss.Width(strings.Join(parts, sep))
	}
	left := len(fields)
	for _, i := range giveWay {
		if need() <= inner {
			break
		}
		switch {
		case i == panelLead:
			lead = ""
		case left > 1:
			shown[i] = false
			left--
		}
	}
	var kept []string
	for i, f := range fields {
		if shown[i] {
			kept = append(kept, f)
		}
	}
	fields = kept
	rest := strings.Join(fields, sep)
	if lead != "" {
		lead = truncateLeft(lead, inner-lipgloss.Width(rest)-lipgloss.Width(sep))
	}
	content := rest
	styled := rest
	if lead != "" {
		content = lead + sep + rest
		styled = styles.SectionHeaderStyle.Render(lead) + styles.PanelBorderStyle.Render(sep) + rest
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
	side := styles.PanelBorderStyle.Render(styles.BoxVertical)
	return styles.PanelBorderStyle.Render(top) + "\n" + side + "  " + styled + pad + "  " + side + "\n" + styles.PanelBorderStyle.Render(bottom)
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

	return styles.DimStyle.Render(leftLine) + "[ " + styles.HeroCostStyle.Render(costStr) + " ]" + styles.DimStyle.Render(rightLine)
}

// renderCostRows renders the cost and token rows every static report opens
// with, fitted to width. The rows' trailing notes go first, a cache write's
// TTL moving into its label so the two rows stay apart, then the "tokens"
// after each count. The block sheds each together, so its rows read alike.
func renderCostRows(cost models.CostBreakdown, usage models.TokenUsage, width int, noColor bool) string {
	out := costRows(cost, usage, true, true, costRowWidths{cost: 11, tokens: 12}, noColor)
	if lipgloss.Width(out) > width {
		out = costRows(cost, usage, false, true, costRowWidths{cost: 11, tokens: 12}, noColor)
	}
	if lipgloss.Width(out) > width {
		out = costRows(cost, usage, false, false, costRowWidths{cost: 11, tokens: 12}, noColor)
	}
	if lipgloss.Width(out) > width {
		// Give up only the columns the rows don't fit in: the cost column
		// takes back spare room first, so its $ stays where it usually is.
		w := fittedCostRowWidths(cost, usage)
		spare := width - lipgloss.Width(costRows(cost, usage, false, false, w, true))
		grow := max(0, min(spare, 11-w.cost))
		w.cost += grow
		w.tokens += max(0, min(spare-grow, 12-w.tokens))
		out = costRows(cost, usage, false, false, w, noColor)
	}
	return out
}

// costRowWidths sizes the cost rows' cost and token-count columns.
type costRowWidths struct{ cost, tokens int }

// fittedCostRowWidths sizes both columns to their widest value rather than
// the usual 11 and 12, for global's narrowest layouts. show and summary never
// draw narrow enough to need it.
func fittedCostRowWidths(cost models.CostBreakdown, usage models.TokenUsage) costRowWidths {
	cache5m, cache1h := render.CacheTokensByTTL(usage)
	var w costRowWidths
	for _, n := range []int64{usage.InputTokens, usage.OutputTokens, cache5m, cache1h, usage.CacheReadInputTokens} {
		w.tokens = max(w.tokens, len(render.Number(n)))
	}
	w.cost = render.CostCellWidth(0, cost.InputCost, cost.OutputCost, cost.CacheWrite5mCost, cost.CacheWrite1hCost, cost.CacheReadCost, cost.CacheSavings)
	return w
}

// costRows renders the cost rows with or without their notes and the
// "tokens" unit, in columns w wide.
func costRows(cost models.CostBreakdown, usage models.TokenUsage, notes, unit bool, w costRowWidths, noColor bool) string {
	var sb strings.Builder
	sb.WriteString(renderUnifiedCostRow("Input", cost.InputCost, usage.InputTokens, nil, unit, "", w, noColor))
	sb.WriteString(renderUnifiedCostRow("Output", cost.OutputCost, usage.OutputTokens, styles.OutputTokenColor, unit, "", w, noColor))
	cacheWrite := func(ttl string, c float64, tokens int64) string {
		if notes {
			return renderUnifiedCostRow("Cache write", c, tokens, styles.CacheWriteTokenColor, unit, ttl+" TTL", w, noColor)
		}
		return renderUnifiedCostRow("Cache write "+ttl, c, tokens, styles.CacheWriteTokenColor, unit, "", w, noColor)
	}
	cache5mTokens, cache1hTokens := render.CacheTokensByTTL(usage)
	if cost.CacheWrite5mCost > 0 {
		sb.WriteString(cacheWrite("5m", cost.CacheWrite5mCost, cache5mTokens))
	}
	if cost.CacheWrite1hCost > 0 {
		sb.WriteString(cacheWrite("1h", cost.CacheWrite1hCost, cache1hTokens))
	}
	if cost.CacheReadCost > 0 || usage.CacheReadInputTokens > 0 {
		sb.WriteString(renderUnifiedCostRow("Cache read", cost.CacheReadCost, usage.CacheReadInputTokens, styles.CacheReadTokenColor, unit, "", w, noColor))
	}
	if cost.CacheSavings > 0 {
		sb.WriteString(renderSavingsRow(cost.CacheSavings, notes, w.cost, noColor))
	}
	return sb.String()
}

// renderUnifiedCostRow renders a single row with cost and token info
// combined, the count followed by "tokens" when unit is set:
// "  Label            $0.3710      53.9K tokens"
func renderUnifiedCostRow(label string, cost float64, tokens int64, labelColor lipgloss.TerminalColor, unit bool, extra string, w costRowWidths, noColor bool) string {
	// Format label with optional color
	var labelStr string
	if !noColor && labelColor != nil {
		labelStyled := lipgloss.NewStyle().Foreground(labelColor)
		labelStr = labelStyled.Render(fmt.Sprintf("%-14s", label))
	} else {
		labelStr = fmt.Sprintf("%-14s", label)
	}

	costStr := formatCostStyled(cost, w.cost, noColor)

	// Format tokens
	tokenStr := fmt.Sprintf("%*s", w.tokens, render.Number(tokens))
	if unit {
		tokenStr += " tokens"
	}

	// Add extra info (like TTL)
	extraStr := ""
	if extra != "" {
		if !noColor {
			extraStr = "  " + styles.DimStyle.Render(extra)
		} else {
			extraStr = "  " + extra
		}
	}

	return fmt.Sprintf("  %s %s  %s%s\n", labelStr, costStr, tokenStr, extraStr)
}

// renderSavingsRow renders the cache-savings line (2-space indent, 11-wide
// cost), with its "(from cache reads)" note when note is set. In no-color
// mode the label and note are plain; otherwise the label is cyan and the
// value green.
func renderSavingsRow(savings float64, note bool, costWidth int, noColor bool) string {
	if noColor {
		row := fmt.Sprintf("  %-14s %s", "Savings", render.CostCell(savings, costWidth))
		if note {
			row += "  (from cache reads)"
		}
		return row + "\n"
	}
	row := fmt.Sprintf("  %s %s",
		styles.SavingsLabelStyle.Render(fmt.Sprintf("%-14s", "Savings")),
		formatCostStyledGreen(savings, costWidth, noColor))
	if note {
		row += "  " + styles.DimStyle.Render("(from cache reads)")
	}
	return row + "\n"
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

// footerStats joins footer fields with footerSep on as few lines as fit in
// width. Callers put the total first, so it's on screen whenever the footer
// is.
func footerStats(fields []string, width int, noColor bool) string {
	lines := splitFooterFields(fields, "  "+footerSep()+"  ", width)
	if !noColor {
		for i, l := range lines {
			lines[i] = styles.FooterStyle.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

// splitFooterFields breaks fields, in order, into the fewest lines that fit
// in width, and of those splits takes the one with the shortest longest line,
// so a footer that needs two lines reads as two even halves rather than a
// full line and a lone field. Ties go to the fuller first line. A field wider
// than width gets a line to itself. Footers hold a handful of fields, so
// trying every split is cheap.
func splitFooterFields(fields []string, sep string, width int) []string {
	if len(fields) == 0 {
		return []string{""}
	}
	var best []string
	bestWidest, bestFirst := 0, 0
	// Bit i of breaks set means a new line starts after fields[i].
	for breaks := 0; breaks < 1<<(len(fields)-1); breaks++ {
		lines := []string{fields[0]}
		fits := true
		for i, f := range fields[1:] {
			if breaks&(1<<i) != 0 {
				lines = append(lines, f)
			} else {
				lines[len(lines)-1] += sep + f
				fits = fits && lipgloss.Width(lines[len(lines)-1]) <= width
			}
		}
		if !fits {
			continue
		}
		widest := 0
		for _, l := range lines {
			widest = max(widest, lipgloss.Width(l))
		}
		first := lipgloss.Width(lines[0])
		better := best == nil || len(lines) < len(best) ||
			(len(lines) == len(best) && (widest < bestWidest || (widest == bestWidest && first > bestFirst)))
		if better {
			best, bestWidest, bestFirst = lines, widest, first
		}
	}
	return best
}

// renderFooterDoubleRule renders the heavy separator that closes a table body.
func renderFooterDoubleRule(width int, noColor bool) string {
	if noColor {
		return strings.Repeat(styles.BoxHorizontal, width)
	}
	return styles.PanelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width))
}

// renderFooterSingleRule renders the light rule drawn under the footer stats.
func renderFooterSingleRule(width int, noColor bool) string {
	if noColor {
		return strings.Repeat(styles.LineHorizontal, width)
	}
	return styles.DimStyle.Render(strings.Repeat(styles.LineHorizontal, width))
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
