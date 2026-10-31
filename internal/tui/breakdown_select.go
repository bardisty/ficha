package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	keybind "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// isDownKey reports whether msg is one of the viewport keymap's downward
// scroll keys.
func isDownKey(km viewport.KeyMap, msg tea.KeyPressMsg) bool {
	return keybind.Matches(msg, km.Down, km.PageDown, km.HalfPageDown)
}

// topCount is how many of the most expensive rows p cycles through.
const topCount = 5

// byCost returns the positions in m.messages of the scope's rows, most
// expensive first; ties go to the earlier row.
func (m BreakdownModel) byCost() []int {
	pos := m.timeOrder()
	sort.SliceStable(pos, func(a, b int) bool {
		return m.messages[pos[a]].Cost.TotalCost > m.messages[pos[b]].Cost.TotalCost
	})
	return pos
}

// topByCost returns the positions in m.messages of the most expensive rows,
// most expensive first.
func (m BreakdownModel) topByCost() []int {
	pos := m.byCost()
	return pos[:min(topCount, len(pos))]
}

// rowOrder returns the positions in m.messages of the scope's rows in the
// order the table draws them: by time, or by cost while sorted.
func (m BreakdownModel) rowOrder() []int {
	if m.sortByCost {
		return m.byCost()
	}
	return m.timeOrder()
}

// timeOrder returns the positions in m.messages of the scope's rows, by
// time.
func (m BreakdownModel) timeOrder() []int {
	pos := make([]int, 0, len(m.messages))
	for i, msg := range m.messages {
		if m.inScope(msg) {
			pos = append(pos, i)
		}
	}
	return pos
}

// sortAnchor returns the identity of the row at the top of a sorted view
// scrolled off the top, for keepSortAnchor, or "" when there's nothing to
// hold: in time order new rows land below, and at the top of a sorted view
// a new most expensive row should show where the reader is looking.
func (m BreakdownModel) sortAnchor() string {
	if !m.sortByCost || !m.ready || m.chosenY == 0 || m.chosenY >= len(m.lineRows) {
		return ""
	}
	index := m.lineRows[m.chosenY]
	if index < 1 || index > len(m.messages) {
		return ""
	}
	return breakdownMsgKey(m.messages[index-1])
}

// keepSortAnchor scrolls the anchor row back to the top after a reload. A
// row that sorts in above it would otherwise push every row on screen down
// one line under the reader's eye.
func (m *BreakdownModel) keepSortAnchor(anchor string) {
	if anchor == "" {
		return
	}
	for line, index := range m.lineRows {
		if index > 0 && breakdownMsgKey(m.messages[index-1]) == anchor {
			m.returnTo(line)
			return
		}
	}
}

// toggleSort switches the table between time order and cost order. Sorted,
// the view opens on the most expensive row and stops following: new rows
// still load and count, but land where their cost puts them rather than
// moving the view. Back in time order, the view returns to where it was, or
// to following if it was following.
func (m *BreakdownModel) toggleSort() {
	if !m.ready {
		return
	}
	if !m.sortByCost {
		// Nothing to sort. Leaving the mode needs no rows, though: a reload
		// can empty the table while it's sorted.
		if len(m.messages) == 0 {
			return
		}
		m.timeYOffset, m.timeFollow = m.chosenY, m.autoScroll
		m.setFollow(false)
		m.sortByCost = true
		m.relayout()
		m.refreshViewport()
		m.scrollTo(0)
		return
	}
	// The time-order line is the one to keep if this relayout finds the
	// session newly dated.
	m.chosenY = m.timeYOffset
	m.sortByCost = false
	m.relayout()
	m.refreshViewport()
	if m.timeFollow {
		m.viewport.GotoBottom()
		m.scrollTo(m.viewport.YOffset())
		m.setFollow(true)
		return
	}
	m.returnTo(m.chosenY)
}

// selectedRank returns the selected row's position in top and its rank, or
// -1 when nothing is selected or the selection has left the top rows.
func (m BreakdownModel) selectedRank(top []int) int {
	if m.selectedKey == "" {
		return -1
	}
	for rank, pos := range top {
		if breakdownMsgKey(m.messages[pos]) == m.selectedKey {
			return rank
		}
	}
	return -1
}

// selectNextTop jumps to the next of the most expensive rows: the peak
// first, then down the top rows by cost, wrapping. The row is centered and
// highlighted, and auto-scroll stops so it stays put.
func (m *BreakdownModel) selectNextTop() {
	top := m.topByCost()
	if len(top) == 0 || !m.ready {
		return
	}
	rank := (m.selectedRank(top) + 1) % len(top)
	msg := m.messages[top[rank]]
	m.selectedKey = breakdownMsgKey(msg)
	m.selectionNotice = true
	m.setFollow(false)
	m.refreshViewport()
	for line, index := range m.lineRows {
		if index == msg.Index {
			m.scrollTo(line - m.viewport.Height()/2)
			break
		}
	}
}

// selectedLine is the table line of p's selected row, or -1 when nothing is
// selected or its row isn't in the table.
func (m BreakdownModel) selectedLine() int {
	if m.selectedKey == "" {
		return -1
	}
	for line, index := range m.lineRows {
		if index > 0 && breakdownMsgKey(m.messages[m.linePos[line]]) == m.selectedKey {
			return line
		}
	}
	return -1
}

// selectedOnScreen reports whether p's selected row is in the viewport.
func (m BreakdownModel) selectedOnScreen() bool {
	line, top := m.selectedLine(), m.viewport.YOffset()
	return line >= 0 && line >= top && line < top+m.viewport.Height()
}

// showSelected scrolls the least that brings p's selected row on screen,
// after a resize. A row still on screen leaves the view alone, so the
// first row holds through zoom and unzoom as it does with no selection.
func (m *BreakdownModel) showSelected() {
	line, top, height := m.selectedLine(), m.viewport.YOffset(), m.viewport.Height()
	switch {
	case line < 0 || height <= 0:
	case line < top:
		m.scrollTo(line)
	case line >= top+height:
		m.scrollTo(line - height + 1)
	}
}

// clearSelection drops p's selection.
func (m *BreakdownModel) clearSelection() {
	m.selectedKey = ""
	m.selectionNotice = false
}

// minSplitShare is the least share of a row's cost that selectionText names
// a component for. Below it a component can't be why the row was expensive,
// and "Cache read $0.0001" would only take room from the ones that are.
const minSplitShare = 0.05

// selectionText describes the row p selected for the notify row while
// selectionNotice holds, with its cost split by component in dollars,
// largest first:
//
//	top 4 of 5: #547 $0.3479 • Cache write 1h $0.1487, Cache read $0.1222, Output $0.0757 • p next
//
// The table's columns are token counts, priced up to 200 times apart, so
// they can't say which part made the row expensive; this can. The labels
// are the ones watch and show use. When the line doesn't fit, components go
// from the smallest end, then the split, before p next: without the hint,
// p's next press is a guess. Sorted, where no divider shows, the first
// reply after a compaction says so in place of the split, since that's why
// it cost what it did.
func (m BreakdownModel) selectionText() string {
	// Runs on every frame; don't sort the rows when there's nothing to say
	if !m.selectionNotice || m.selectedKey == "" {
		return ""
	}
	top := m.topByCost()
	rank := m.selectedRank(top)
	if rank < 0 {
		return ""
	}
	msg := m.messages[top[rank]]
	prefix := fmt.Sprintf("top %s of %s: #%d %s", render.Count(rank+1), render.Count(len(top)), msg.Index, render.Cost(msg.Cost.TotalCost))
	var split []string
	for _, c := range render.CostComponents(msg.Cost) {
		if c.Cost < minSplitShare*msg.Cost.TotalCost {
			break
		}
		split = append(split, render.CostComponentLabel(c.Name)+" "+render.Cost(c.Cost))
	}
	if m.sortByCost && msg.Compacted != nil {
		split = []string{"first reply after compaction"}
	}
	sep := " " + styles.Bullet + " "
	width := m.width - 2
	for n := len(split); n > 0; n-- {
		line := prefix + sep + strings.Join(split[:n], ", ") + sep + "p next"
		if m.width <= 0 || lipgloss.Width(line) <= width {
			return line
		}
	}
	return joinSegments([]string{prefix, "p next"}, sep, width)
}

// leftView is how the reader left a session: enough for - to bring it back
// as it was, by row rather than by line, since the session may have grown
// and its day dividers moved while the reader was away.
type leftView struct {
	path       string
	follow     bool // following new sessions
	autoScroll bool // on the newest row
	top        rowMark
	sorted     bool
	timeTop    rowMark // while sorted, the time-order line s goes back to
	timeFollow bool
	selected   string // p's selection, as selectedKey
	pausedAt   int    // rows when following stopped: any past it are new
	scope      rowScope
}

// rowMark is a table line held by the row on it. above counts the lines
// the mark sits above the row, on the dividers that head it.
type rowMark struct {
	key   string // breakdownMsgKey
	index int    // the row's number, for when its key is gone
	above int
}

// leaving is the view as the reader leaves it.
func (m BreakdownModel) leaving() leftView {
	v := leftView{
		path: m.sessionPath, follow: m.followMode, autoScroll: m.autoScroll,
		sorted: m.sortByCost, timeFollow: m.timeFollow, selected: m.selectedKey, pausedAt: m.pausedAt,
		scope: m.scope,
	}
	if m.autoScroll {
		v.pausedAt = m.scopeLen()
	}
	v.top = m.markLine(m.lineRows, m.linePos, m.chosenY)
	if m.sortByCost {
		rows, pos := m.timeLines()
		v.timeTop = m.markLine(rows, pos, m.timeYOffset)
	}
	return v
}

// markLine marks line y of a layout from tableLines, or the zero mark when
// y is past it.
func (m BreakdownModel) markLine(lineRows, linePos []int, y int) rowMark {
	if y < 0 || y >= len(lineRows) {
		return rowMark{}
	}
	msg := m.messages[linePos[y]]
	above := 0
	for line := y; line < len(lineRows) && lineRows[line] <= 0; line++ {
		above++
	}
	return rowMark{key: breakdownMsgKey(msg), index: msg.Index, above: above}
}

// lineOf finds mark in a layout from tableLines. A row whose key is gone,
// the transcript having been rewritten, gives way to the row now under its
// number, or the last row.
func (m BreakdownModel) lineOf(lineRows, linePos []int, mark rowMark) int {
	if mark.key == "" || len(lineRows) == 0 {
		return 0
	}
	byIndex := -1
	for line, index := range lineRows {
		if index <= 0 {
			continue
		}
		if breakdownMsgKey(m.messages[linePos[line]]) == mark.key {
			// As far up its dividers as the mark was, of those it has now.
			back := 0
			for back < mark.above && line-back > 0 && lineRows[line-back-1] <= 0 {
				back++
			}
			return line - back
		}
		if index == mark.index {
			byIndex = line
		}
	}
	if byIndex < 0 {
		return len(lineRows) - 1
	}
	return byIndex
}

// bringBack applies a view - went back to, once its session's first load
// has landed: the sort, p's selection if its row is still there, and the
// first row on screen, or the newest row if the reader was on it. Rows
// that arrived meanwhile count as new, with those already new when the
// reader left, as they would had the reader stayed scrolled up.
func (m *BreakdownModel) bringBack(v leftView) {
	if !m.ready || len(m.messages) == 0 {
		return
	}
	if v.scope.on {
		m.scope = v.scope
		if m.countScope(); m.scopeLen() == 0 {
			m.scope = rowScope{}
			m.countScope()
		}
		m.scopeInsights()
		m.refreshViewport()
	}
	if slices.ContainsFunc(m.messages, func(msg models.BreakdownMessage) bool { return breakdownMsgKey(msg) == v.selected }) {
		m.selectedKey = v.selected
	}
	if v.autoScroll && !v.sorted {
		return
	}
	m.setFollow(false)
	m.pausedAt = min(v.pausedAt, m.scopeLen())
	if v.sorted {
		rows, pos := m.timeLines()
		m.timeYOffset, m.timeFollow = m.lineOf(rows, pos, v.timeTop), v.timeFollow
		m.sortByCost = true
		m.relayout()
		m.refreshViewport()
	}
	m.returnTo(m.lineOf(m.lineRows, m.linePos, v.top))
}
