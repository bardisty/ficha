package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/bardisty/ficha/internal/models"
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
// panel can't be drawn at its minimum width plus indent.
const (
	compactBelowHeight = 16
	compactBelowWidth  = minPanelWidth + 4
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
	return m.width > 0 && m.width < breakdownLayout{indexWidth: m.table.indexWidth}.width()
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
// rows it frames. Unlike panelWidthFor it has no 40-column floor: the compact
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

// spinnerCmd restarts the spinner when a load begins. The header shows it
// for every load, background reloads included. A second chain is harmless:
// the spinner drops ticks with a stale tag.
func (m BreakdownModel) spinnerCmd() tea.Cmd {
	if m.loading {
		return m.spinner.Tick
	}
	return nil
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
		m.pausedAt = len(m.messages)
		m.pausePending = m.loading && len(m.messages) == 0
	}
	m.autoScroll = on
}

// refreshViewport re-renders the viewport content: the table, or the empty
// state once a load has found no messages. Auto-scroll keeps it at the bottom.
func (m *BreakdownModel) refreshViewport() {
	if !m.ready {
		return
	}
	var content string
	switch {
	case len(m.messages) > 0:
		content, m.lineRows = m.renderTableContent()
	case m.waiting():
		content, m.lineRows = m.renderWaiting(), nil
	case !m.loading && m.err == nil:
		content, m.lineRows = m.renderEmptyState(), nil
	default:
		content, m.lineRows = "", nil
	}
	m.viewport.SetContent(clipToWidth(content, m.width))
	if m.autoScroll {
		m.viewport.GotoBottom()
	}
}

// emptyStateText stands in for the table while the session has no replies.
const emptyStateText = "No replies yet. Rows appear as Claude responds."

// renderWaiting stands in for the table until the project has a session:
// "Waiting for a Claude Code session in ~/work/webapp…", as in watch.
func (m BreakdownModel) renderWaiting() string {
	text := "Waiting for a Claude Code session" + styles.Ellipsis
	if m.waitingIn != "" {
		text = "Waiting for a Claude Code session in " + m.waitingIn + styles.Ellipsis
	}
	if m.noColor {
		return "  " + text
	}
	return "  " + dimStyle.Render(text)
}

func (m BreakdownModel) renderEmptyState() string {
	if m.noColor {
		return "  " + emptyStateText
	}
	return "  " + dimStyle.Render(emptyStateText)
}

// visibleRows returns the display Index of the first and last message rows
// in the viewport, skipping day dividers, or 0, 0 when none is visible.
func (m BreakdownModel) visibleRows() (first, last int) {
	top := m.viewport.YOffset
	bottom := min(top+m.viewport.Height, len(m.lineRows)) - 1
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
// A table that fits the viewport has no position to report.
func (m BreakdownModel) positionText() string {
	if m.sortByCost {
		return m.sortedPositionText()
	}
	if !m.ready || m.viewport.TotalLineCount() <= m.viewport.Height {
		return ""
	}
	first, last := m.visibleRows()
	if first == 0 {
		return ""
	}
	text := fmt.Sprintf("rows %d-%d of %d", first, last, len(m.messages))
	if n := len(m.messages) - m.pausedAt; !m.autoScroll && n > 0 {
		text += fmt.Sprintf(" %s %d new %s (G)", styles.Bullet, n, styles.MoreBelow)
	}
	return text
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
	if m.ready && m.viewport.TotalLineCount() > m.viewport.Height {
		first := m.viewport.YOffset + 1
		last := min(m.viewport.YOffset+m.viewport.Height, m.viewport.TotalLineCount())
		text += fmt.Sprintf(" %s rank %d-%d of %d", styles.Bullet, first, last, len(m.messages))
	}
	return text
}

// sortedLabel names the cost-order mode on the footer rule, with the key
// that leaves it: on a narrow terminal the help line has no room for s.
const sortedLabel = "sorted by cost (s)"

// renderFooterRule draws the heavy rule above the footer, carrying the
// scroll position in brackets like watch's rule does. A position that
// doesn't fit leaves the plain rule, or while sorted, the mode alone.
func (m BreakdownModel) renderFooterRule(panelWidth int) string {
	mark := m.positionText()
	const lead = 2 // rule cells kept left of the marker
	if m.sortByCost && lead+lipgloss.Width("[ "+mark+" ]") > panelWidth {
		mark = sortedLabel
	}
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
