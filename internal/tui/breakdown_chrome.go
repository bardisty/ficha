package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// Breakdown's chrome, in rows. The full header is the 3-row panel, the
// insights row, the notify row, the column header and its rule. The compact
// header, for short or narrow terminals, is one unboxed status line, the
// insights row and the column header. The footer is the rule carrying the
// scroll position, the totals line and the key hints.
const (
	breakdownHeaderRows        = 7
	breakdownCompactHeaderRows = 3
	breakdownFooterRows        = 3
)

// Below compactBelowHeight rows the boxed panel and the spacer rows cost more
// than the data rows they'd push out. Below compactBelowWidth columns the
// box gives way to the compact header. That width is breakdown's own, not
// minPanelWidth's: watch keeps its box down to its 40-column minimum.
const (
	compactBelowHeight = 16
	compactBelowWidth  = 44
)

// Below this height no layout leaves a data row, so View says the terminal
// is too small instead of drawing a broken frame. The width limit is the
// narrowest layout's (see tooSmall).
const tooSmallHeight = breakdownCompactHeaderRows + breakdownFooterRows + 1

// compact reports whether the frame uses the compact header.
func (m BreakdownModel) compact() bool {
	return (m.height > 0 && m.height < compactBelowHeight) || (m.width > 0 && m.width < compactBelowWidth)
}

// tooSmall reports whether the terminal can't fit even the compact frame:
// fewer rows than leave one for data, or fewer columns than #, TIME, MODEL
// and COST need. Cut any narrower, the frame's right edge would clip the
// digits off every cost.
func (m BreakdownModel) tooSmall() bool {
	if m.height > 0 && m.height < tooSmallHeight {
		return true
	}
	return m.width > 0 && m.width < m.minWidth()
}

// minWidth is the narrowest layout's width, for tooSmall. It never counts
// the weekday: layout leaves that out rather than need the column.
func (m BreakdownModel) minWidth() int {
	return breakdownLayout{indexWidth: m.table.indexWidth}.width()
}

// headerRows is the number of rows View draws above the viewport.
func (m BreakdownModel) headerRows() int {
	if m.compact() {
		return breakdownCompactHeaderRows
	}
	return breakdownHeaderRows
}

// panelWidth is the width of breakdown's rules and panel: watch's width, or
// the table's when the table is wider, so the frame never falls short of the
// rows it frames. Unlike panelWidthFor it has no floor: the compact
// frame has no box, so its rules can shrink to the terminal.
func (m BreakdownModel) panelWidth() int { return m.frameWidth(m.table) }

// frameWidth is panelWidth for a layout already worked out.
func (m BreakdownModel) frameWidth(layout breakdownLayout) int {
	want := max(defaultPanelWidth, layout.width()-bdIndent)
	if m.width <= 0 {
		return want
	}
	return max(min(m.width-2, want), 1)
}

// clock returns the model's notion of now; tests pin it.
func (m BreakdownModel) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// clockInterval paces clockMsg; see the function of the same name.
func (m BreakdownModel) clockInterval() time.Duration {
	return clockInterval(m.lastActivity, m.clock())
}

// spinnerCmd restarts the spinner when a load will show "loading...". A
// second chain is harmless: the spinner drops ticks with a stale tag.
func (m BreakdownModel) spinnerCmd() tea.Cmd {
	if m.showLoading() {
		return m.spinner.Tick
	}
	return nil
}

// showLoading reports whether the header shows "loading...": only until the
// session's first load lands, as in watch. A reload keeps the last status up
// instead of flickering on every write, even for a session with no rows.
func (m BreakdownModel) showLoading() bool {
	return m.loading && !m.loaded
}

// breakdownLastActivity is the newest message timestamp, else the session
// file's mtime for a session with no timestamped message yet. fromFile
// reports the fallback.
func breakdownLastActivity(msgs []models.BreakdownMessage, modTime time.Time) (t time.Time, fromFile bool) {
	for _, msg := range msgs {
		if msg.Timestamp.After(t) {
			t = msg.Timestamp
		}
	}
	if t.IsZero() {
		return modTime, !modTime.IsZero()
	}
	return t, false
}

// setFollow turns auto-scroll on or off. Turning it off records the message
// count, so the footer can count the rows that arrive while scrolled up.
// Before the first load lands there's no count to record, so the load that
// lands records it (see pausePending). Sorted by cost, the newest row isn't
// at the bottom, so nothing turns following on.
func (m *BreakdownModel) setFollow(on bool) {
	if on && m.sortByCost {
		return
	}
	if m.autoScroll && !on {
		m.pausedAt = m.scopeLen()
		m.pausePending = m.loading && len(m.messages) == 0
	}
	m.autoScroll = on
}

// refreshViewport re-renders the viewport content: the table, or the empty
// state once a load has found no messages. Auto-scroll keeps it at the
// bottom; otherwise it shows the offset the reader chose, clamped to the
// content. The clamp isn't kept: the viewport clamps on every SetContent,
// and a window that grows and shrinks back, as a zoomed tmux pane does,
// would otherwise come back rows away from the reader's line.
func (m *BreakdownModel) refreshViewport() {
	if !m.ready {
		return
	}
	var content string
	m.lineRows, m.linePos = nil, nil
	switch {
	case len(m.messages) > 0:
		// One blank line per table line; tableView draws the ones on screen.
		m.lineRows, m.linePos = m.tableLines()
		content = strings.Repeat("\n", len(m.lineRows)-1)
	case m.waiting():
		content = clipToWidth(m.renderWaiting(), m.width)
	case !m.showLoading() && m.err == nil:
		content = clipToWidth(m.renderEmptyState(), m.width)
	}
	m.viewport.SetContent(content)
	if m.autoScroll {
		m.viewport.GotoBottom()
		return
	}
	m.viewport.SetYOffset(m.chosenY)
}

// scrollTo moves the view to line y as the reader's choice, which
// refreshViewport keeps through reloads and resizes. Only the reader's own
// actions go through it: scroll keys, p, s and a switch.
func (m *BreakdownModel) scrollTo(y int) {
	m.viewport.SetYOffset(y)
	m.chosenY = m.viewport.YOffset()
}

// returnTo shows line y, a line the reader chose before: the sort anchor
// after a reload, or the time-order line s goes back to. Unlike scrollTo it
// keeps y as the choice even where this size clamps it, so a window grown
// meanwhile still gives the line back when it shrinks.
func (m *BreakdownModel) returnTo(y int) {
	m.chosenY = y
	m.viewport.SetYOffset(y)
}

// emptyStateText stands in for the table while the session has no replies.
const emptyStateText = "No replies yet. Rows appear as Claude responds."

// renderWaiting stands in for the table until the project has a session:
// "Waiting for a Claude Code session in ~/work/webapp…", as in watch.
func (m BreakdownModel) renderWaiting() string {
	lines := waitingLines(m.waitingIn, m.waitingHint, m.panelWidth())
	for i, line := range lines {
		if !m.noColor {
			line = styles.DimStyle.Render(line)
		}
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}

func (m BreakdownModel) renderEmptyState() string {
	if m.noColor {
		return "  " + emptyStateText
	}
	return "  " + styles.DimStyle.Render(emptyStateText)
}

// visibleRows returns the display Index of the first and last message rows
// in the viewport, skipping day dividers, or 0, 0 when none is visible.
func (m BreakdownModel) visibleRows() (first, last int) {
	top := m.viewport.YOffset()
	bottom := min(top+m.viewport.Height(), len(m.lineRows)) - 1
	for i := top; i <= bottom; i++ {
		if m.lineRows[i] > 0 {
			first = m.lineRows[i]
			break
		}
	}
	for i := bottom; i >= top; i-- {
		if m.lineRows[i] > 0 {
			last = m.lineRows[i]
			break
		}
	}
	return first, last
}

// positionText says which rows are on screen and, when the reader has
// scrolled up, how many have arrived below since:
//
//	rows 13-24 of 460 • 60 new ↓ (G)
//
// A table that fits the viewport has no position to report. It's the
// footer rule's fullest text; footerMarks has the shorter ones.
func (m BreakdownModel) positionText() string {
	marks := m.footerMarks()
	if len(marks) == 0 {
		return ""
	}
	return marks[0]
}

// footerMarks lists the footer rule's texts from fullest to barest, and the
// rule shows the first that fits. Each step drops one label, in this order:
// the key that steps the scope on, the day of the first row on screen, the
// row range, and last the scope, which names whose rows these are where
// the AGENT column has no room.
func (m BreakdownModel) footerMarks() []string {
	var marks []string
	if m.sortByCost {
		marks = []string{m.sortedPositionText(), sortedLabel}
	} else if position := m.rowsText(); position != "" {
		if day := m.firstVisibleDay(); day != "" {
			marks = append(marks, day+" "+styles.Bullet+" "+position)
		}
		marks = append(marks, position)
	}
	scope := m.ruleScope()
	if scope == "" {
		return marks
	}
	sep := " " + styles.Bullet + " "
	full := scope + sep + scopeKeyHint
	if len(marks) > 0 {
		full = scope + sep + marks[0] + sep + scopeKeyHint
	}
	scoped := []string{full}
	for _, mark := range marks {
		scoped = append(scoped, scope+sep+mark)
	}
	return append(scoped, scope)
}

// rowsText is the row range and new-row count of positionText, with no day.
// Scoped, it counts the scope's rows: the range is where they are among
// those, not the session's row numbers, which skip.
func (m BreakdownModel) rowsText() string {
	if !m.ready || m.viewport.TotalLineCount() <= m.viewport.Height() {
		return ""
	}
	first, last := m.visibleRows()
	if first == 0 {
		return ""
	}
	if m.scope.on {
		first, last = m.scopeRank(first), m.scopeRank(last)
	}
	text := fmt.Sprintf("rows %s-%s of %s", render.Count(first), render.Count(last), render.Count(m.scopeLen()))
	if n := m.scopeLen() - m.pausedAt; !m.autoScroll && n > 0 {
		text += fmt.Sprintf(" %s %s new %s (G)", styles.Bullet, render.Count(n), styles.MoreBelow)
	}
	return text
}

// scopeRank is where the row with display Index index falls among the
// scope's rows in time order, from 1.
func (m BreakdownModel) scopeRank(index int) int {
	rank := 0
	for i := range min(index, len(m.messages)) {
		if m.inScope(m.messages[i]) {
			rank++
		}
	}
	return rank
}

// firstVisibleDay names the day of the first row on screen, as its divider
// does, so a reader deep in a long day still sees which one. It reads the
// day from the nearest divider at or above that row, since a row whose
// timestamp didn't parse has no day of its own. Only a dated session in
// time order has days to name: in one that's all today the day goes
// without saying, and in cost order TIME carries it.
func (m BreakdownModel) firstVisibleDay() string {
	if !m.dated || m.sortByCost {
		return ""
	}
	top := m.viewport.YOffset()
	bottom := min(top+m.viewport.Height(), len(m.lineRows))
	first := top
	for first < bottom && m.lineRows[first] == 0 {
		first++
	}
	if first == bottom {
		return ""
	}
	for i := first; i >= 0; i-- {
		if m.lineRows[i] == 0 {
			return render.DayMarker(m.messages[m.linePos[i]].Timestamp)
		}
	}
	return ""
}

// sortedPositionText is positionText while sorted by cost. It leads with the
// mode and the key that leaves it, then counts ranks in cost order rather
// than row numbers, which no longer run in sequence:
//
//	sorted by cost (s) • rank 1-14 of 460
//
// It has no count of new rows: they land anywhere, and no key takes the
// reader to them.
func (m BreakdownModel) sortedPositionText() string {
	text := sortedLabel
	if m.ready && m.viewport.TotalLineCount() > m.viewport.Height() {
		first := m.viewport.YOffset() + 1
		last := min(m.viewport.YOffset()+m.viewport.Height(), m.viewport.TotalLineCount())
		text += fmt.Sprintf(" %s rank %s-%s of %s", styles.Bullet, render.Count(first), render.Count(last), render.Count(m.scopeLen()))
	}
	return text
}

// sortedLabel names the cost-order mode on the footer rule, with the key
// that leaves it: on a narrow terminal the help line has no room for s.
const sortedLabel = "sorted by cost (s)"

// renderFooterRule draws the heavy rule above the footer, carrying the
// scroll position in brackets like watch's rule does. It takes the fullest
// of footerMarks that fits, and the plain rule when none does.
func (m BreakdownModel) renderFooterRule(panelWidth int) string {
	const lead = 2 // rule cells kept left of the marker
	mark := ""
	for _, text := range m.footerMarks() {
		if lead+lipgloss.Width("[ "+text+" ]") <= panelWidth {
			mark = text
			break
		}
	}
	if mark == "" {
		rule := strings.Repeat(styles.BoxHorizontal, panelWidth)
		if m.noColor {
			return "  " + rule
		}
		return "  " + styles.PanelBorderStyle.Render(rule)
	}
	bracketed := "[ " + mark + " ]"
	left := strings.Repeat(styles.BoxHorizontal, lead)
	right := strings.Repeat(styles.BoxHorizontal, panelWidth-lead-lipgloss.Width(bracketed))
	if m.noColor {
		return "  " + left + bracketed + right
	}
	return "  " + styles.PanelBorderStyle.Render(left) + "[ " + styles.FooterStyle.Render(mark) + " ]" + styles.PanelBorderStyle.Render(right)
}
