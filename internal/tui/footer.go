package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// Watch's fixed chrome. The header is the 3-line panel plus the notify row,
// which is always reserved so a notice never shifts the body; the footer is
// the rule, the pinned stats line, any warning rows, and help. Compact mode
// trades the box for a one-line header and drops the help row.
const (
	watchHeaderHeight   = 4
	compactHeaderHeight = 2
	watchFooterBase     = 3
	compactFooterBase   = 2
	maxWarningRows      = 2
)

// Terminal size thresholds: below compactBelowRows the chrome goes compact,
// and below minTermWidth x minTermHeight no frame fits at all.
const (
	compactBelowRows = 16
	minTermWidth     = 40
	minTermHeight    = 8
)

// compact reports whether the terminal is short enough for compact chrome.
func (m Model) compact() bool {
	return m.height > 0 && m.height < compactBelowRows
}

// tooSmall reports whether the terminal can't hold a usable frame.
func (m Model) tooSmall() bool {
	return m.width > 0 && m.height > 0 && (m.width < minTermWidth || m.height < minTermHeight)
}

// headerHeight is the header's row count, notify row included.
func (m Model) headerHeight() int {
	if m.compact() {
		return compactHeaderHeight
	}
	return watchHeaderHeight
}

// rateWindow is how far back the footer's rolling spend rate looks.
const rateWindow = 10 * time.Minute

// rollingRate returns spend per hour over the window ending at now, from
// message timestamps (parent and agents alike). ok is false until the session
// spans the whole window: a rate from a 2-minute-old session averaged over 10
// minutes would read low, and scaling it up would read a single expensive
// first message as a sustained burn. Messages without a timestamp can't be
// placed in the window and are skipped.
func rollingRate(msgs []models.MessageAnalysis, now time.Time, window time.Duration) (perHour float64, ok bool) {
	start := now.Add(-window)
	var earliest time.Time
	var sum float64
	for _, msg := range msgs {
		if msg.Timestamp.IsZero() {
			continue
		}
		if earliest.IsZero() || msg.Timestamp.Before(earliest) {
			earliest = msg.Timestamp
		}
		if msg.Timestamp.After(start) {
			sum += msg.Cost.TotalCost
		}
	}
	if earliest.IsZero() || earliest.After(start) {
		return 0, false
	}
	return sum * float64(time.Hour) / float64(window), true
}

// windowLabel formats a rate window compactly: "10m", "1h".
func windowLabel(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return fmt.Sprintf("%dm", d/time.Minute)
}

// clock returns the model's notion of now; tests pin it.
func (m Model) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// footerHeight is the footer's row count at the current width.
func (m Model) footerHeight() int {
	base := watchFooterBase
	if m.compact() {
		base = compactFooterBase
	}
	return base + len(m.warningRows(panelWidthFor(m.width)))
}

// renderFooterLines renders the fixed footer, one string per row.
func (m Model) renderFooterLines(panelWidth int) []string {
	lines := []string{m.renderFooterRule(panelWidth)}

	// The stats line shows for any non-empty session; an empty one keeps the
	// row blank so the layout doesn't jump when the first message lands.
	if m.analysis != nil && !m.isEmptySession() {
		lines = append(lines, "  "+m.renderStatsLine(panelWidth))
	} else {
		lines = append(lines, "")
	}

	for _, row := range m.warningRows(panelWidth) {
		if m.noColor {
			lines = append(lines, "  "+row)
		} else {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(styles.WarningColor).Render(row))
		}
	}

	if m.compact() {
		return lines
	}
	// r isn't listed: the view is already live, and the notify row offers
	// it as a retry when something fails.
	helpText := helpLine("q quit", "j/k scroll", "space/b page", "g/G top/bottom", "f follow")
	if !m.noColor {
		helpText = lipgloss.NewStyle().Foreground(styles.SecondaryColor).Render(helpText)
	}
	return append(lines, "  "+helpText)
}

// renderFooterRule draws the heavy rule above the footer. When the body
// overflows the viewport, the rule carries how many lines sit above and
// below it, bracketed like the body's section rules, so a total scrolled
// off-screen is never out of mind.
func (m Model) renderFooterRule(panelWidth int) string {
	var marks []string
	if m.ready {
		above := m.viewport.YOffset
		below := m.viewport.TotalLineCount() - m.viewport.YOffset - m.viewport.Height
		if above > 0 {
			marks = append(marks, fmt.Sprintf("%s %d more", styles.MoreAbove, above))
		}
		if below > 0 {
			marks = append(marks, fmt.Sprintf("%s %d more", styles.MoreBelow, below))
		}
	}
	mark := strings.Join(marks, "  ")

	const lead = 2 // rule cells kept left of the marker
	bracketed := "[ " + mark + " ]"
	if mark == "" || lead+lipgloss.Width(bracketed) > panelWidth {
		rule := strings.Repeat(styles.BoxHorizontal, panelWidth)
		if m.noColor {
			return "  " + rule
		}
		return "  " + panelBorderStyle.Render(rule)
	}

	left := strings.Repeat(styles.BoxHorizontal, lead)
	right := strings.Repeat(styles.BoxHorizontal, panelWidth-lead-lipgloss.Width(bracketed))
	if m.noColor {
		return "  " + left + bracketed + right
	}
	return "  " + panelBorderStyle.Render(left) + "[ " + footerStyle.Render(mark) + " ]" + panelBorderStyle.Render(right)
}

// renderStatsLine renders the pinned one-line summary:
//
//	$30.05 API est. │ ctx 95% │ $7.20/h (10m) │ 400 msgs │ 3h 12m
//
// It survives any scroll position, so it carries the two readings people
// glance at a side terminal for. To fit a narrow terminal the "API est."
// label goes first, then whole readings from the right; the total always
// stays.
func (m Model) renderStatsLine(width int) string {
	line := m.statsLine(true)
	if lipgloss.Width(line) <= width {
		return line
	}
	segs := m.statsSegments(false)
	sep := " " + styles.BoxVerticalSep + " "
	if !m.noColor {
		sep = footerStyle.Render(sep)
	}
	line = strings.Join(segs, sep)
	for len(segs) > 1 && lipgloss.Width(line) > width {
		segs = segs[:len(segs)-1]
		line = strings.Join(segs, sep)
	}
	return line
}

// statsLine joins every stats segment.
func (m Model) statsLine(label bool) string {
	sep := " " + styles.BoxVerticalSep + " "
	if !m.noColor {
		sep = footerStyle.Render(sep)
	}
	return strings.Join(m.statsSegments(label), sep)
}

// statsSegments renders the stats line's readings, most important first.
// label adds "API est." after the total. The message count highlights on
// change but carries no delta: a width that changes for two seconds would
// make the line jump, or drop a reading at a narrow width.
func (m Model) statsSegments(label bool) []string {
	a := m.analysis
	style := func(st lipgloss.Style, s string) string {
		if m.noColor {
			return s
		}
		return st.Render(s)
	}

	var segs []string

	total := render.Cost(a.TotalCost.TotalCost)
	totalStyle := heroCostStyle
	if m.isHighlighted("total") {
		totalStyle = highlightStyle
	}
	totalSeg := style(totalStyle, total)
	if label {
		totalSeg += style(footerStyle, " API est.")
	}
	segs = append(segs, totalSeg)

	if contextSize := a.LastMessageUsage.ContextWindowSize(); contextSize > 0 {
		modelPricing := pricing.GetModelPricing(a.LastMessageModel)
		pct := pricing.GetContextPercentage(modelPricing, contextSize)
		ctxStyle := lipgloss.NewStyle().Foreground(styles.GetContextUsageColor(pct))
		segs = append(segs, style(footerStyle, "ctx ")+style(ctxStyle, fmt.Sprintf("%.0f%%", pct)))
	}

	rateStr := "-/h"
	if perHour, ok := rollingRate(a.Messages, m.clock(), rateWindow); ok {
		rateStr = render.Cost(perHour) + "/h"
	}
	segs = append(segs, style(footerStyle, fmt.Sprintf("%s (%s)", rateStr, windowLabel(rateWindow))))

	msgs := fmt.Sprintf("%d msgs", a.MessageCount)
	if a.MessageCount == 1 {
		msgs = "1 msg"
	}
	msgStyle := footerStyle
	if m.isHighlighted("messages") {
		msgStyle = highlightStyle
	}
	segs = append(segs, style(msgStyle, msgs))

	return append(segs, style(footerStyle, render.Duration(a.Duration.Duration())))
}

// warningRows returns the accounting warnings fitted to width in at most
// maxWarningRows rows. Notes share a row when they fit side by side and
// otherwise start their own, so a note never splits across rows unless it is
// wider than the terminal by itself. Empty when there is nothing to report,
// so the row costs nothing then.
func (m Model) warningRows(width int) []string {
	a := m.analysis
	if a == nil {
		return nil
	}
	var notes []string
	if note := accountingFootnote(a.SkippedAgents, a.SkippedLines, a.EstimatedCostMessages); note != "" {
		notes = append(notes, note)
	}
	if hasUnknownModel(a.CostByModel) {
		notes = append(notes, unknownModelFootnote(unknownModelIDs(a.CostByModel)))
	}
	return packNotes(notes, " "+styles.BoxVerticalSep+" ", width, maxWarningRows)
}

// packNotes lays notes out in rows of at most width columns, joining
// neighbors with sep while they fit, and keeps at most maxRows rows, the last
// ending in an ellipsis when anything was cut.
func packNotes(notes []string, sep string, width, maxRows int) []string {
	var rows []string
	for _, note := range notes {
		if n := len(rows); n > 0 && lipgloss.Width(rows[n-1]+sep+note) <= width {
			rows[n-1] += sep + note
			continue
		}
		rows = append(rows, wrapWords(note, width, maxRows)...)
	}
	if len(rows) > maxRows {
		rows = rows[:maxRows]
		rows[maxRows-1] = withEllipsis(rows[maxRows-1], width)
	}
	return rows
}

// wrapWords greedily wraps plain text to width, keeping at most maxRows rows.
// When text is left over, the last row ends in an ellipsis. A single word
// wider than width is hard-clipped.
func wrapWords(text string, width, maxRows int) []string {
	width = max(width, 1)
	var rows []string
	var cur string
	for _, word := range strings.Fields(text) {
		switch {
		case cur == "":
			cur = word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= width:
			cur += " " + word
		default:
			rows = append(rows, cur)
			cur = word
		}
		if len(rows) == maxRows {
			// A word is waiting for a row we can't show: cut the last kept row.
			rows[maxRows-1] = withEllipsis(rows[maxRows-1], width)
			return clipRows(rows, width)
		}
	}
	if cur != "" {
		rows = append(rows, cur)
	}
	return clipRows(rows, width)
}

// withEllipsis appends an ellipsis to row, trimming it to stay within width.
func withEllipsis(row string, width int) string {
	ell := styles.Ellipsis
	r := []rune(row)
	for len(r) > 0 && lipgloss.Width(string(r))+lipgloss.Width(ell) > width {
		r = r[:len(r)-1]
	}
	return strings.TrimRight(string(r), " ") + ell
}

// clipRows hard-clips each row to width.
func clipRows(rows []string, width int) []string {
	for i, r := range rows {
		if lipgloss.Width(r) > width {
			rows[i] = lipgloss.NewStyle().MaxWidth(width).Render(r)
		}
	}
	return rows
}

// renderNotifyRow renders the row under the header, or "" when there is
// nothing to say. An error wins over the switch notice, then a missing file
// watcher, then a hint about another session. The switch notice outranks the
// watcher because it lasts only until the next key, and answers one.
func (m Model) renderNotifyRow(width int) string {
	style := func(c lipgloss.TerminalColor, s string) string {
		if m.noColor {
			return s
		}
		return lipgloss.NewStyle().Foreground(c).Bold(true).Render(s)
	}

	var text string
	color := styles.HighlightColor
	switch {
	case m.err != nil:
		color = styles.ErrorColor
		text = styles.Warning + " " + describeErr(m.err)
		if m.analysis != nil {
			text += ", showing last data"
		}
		text += " " + styles.Bullet + " r to retry"
	case m.switched != nil:
		lead := "switched to " + render.TruncateID(m.sessionID, sessionIDDisplayLen)
		if m.switched.auto {
			lead = "new session " + render.TruncateID(m.sessionID, sessionIDDisplayLen)
		}
		text = styles.Arrow + " " + lead
		if m.switched.hadTotal {
			text += fmt.Sprintf(" (previous %s: %s)",
				render.TruncateID(m.prevSessionID, sessionIDDisplayLen), render.Cost(m.switched.prevTotal))
		}
		if m.prevSessionPath != "" {
			text += " " + styles.Bullet + " - to go back"
		}
	case m.fallback.active():
		color = styles.WarningColor
		text = m.fallback.notice(width)
	case m.hintVisible():
		id := render.TruncateID(m.hint.id, sessionIDDisplayLen)
		if m.hint.created {
			text = "new session " + id + " started"
		} else {
			text = "newer activity in " + id
		}
		text += " " + styles.Bullet + " n to switch"
	default:
		return ""
	}
	if lipgloss.Width(text) > width {
		text = withEllipsis(text, width)
	}
	return style(color, text)
}

// describeErr turns a load or watch error into notify-row text. A missing
// file gets words a user can act on instead of an "open …: no such file".
func describeErr(err error) string {
	var unavailable watchUnavailableError
	switch {
	case errors.As(err, &unavailable):
		return describeWatchUnavailable(unavailable.err)
	case errors.Is(err, errSessionFileGone), errors.Is(err, fs.ErrNotExist):
		return "session file removed"
	case errors.Is(err, fs.ErrPermission):
		return "session file unreadable (permission denied)"
	}
	return err.Error()
}
