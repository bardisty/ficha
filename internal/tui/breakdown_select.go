package tui

import (
	"fmt"
	"sort"

	keybind "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

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

// byCost returns the positions in m.messages, most expensive first; ties go
// to the earlier row.
func (m BreakdownModel) byCost() []int {
	pos := make([]int, len(m.messages))
	for i := range pos {
		pos[i] = i
	}
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

// rowOrder returns the positions in m.messages in the order the table draws
// them: by time, or by cost while sorted.
func (m BreakdownModel) rowOrder() []int {
	if m.sortByCost {
		return m.byCost()
	}
	pos := make([]int, len(m.messages))
	for i := range pos {
		pos[i] = i
	}
	return pos
}

// sortAnchor returns the identity of the row at the top of a sorted view
// scrolled off the top, for keepSortAnchor, or "" when there's nothing to
// hold: in time order new rows land below, and at the top of a sorted view
// a new most expensive row should show where the reader is looking.
func (m BreakdownModel) sortAnchor() string {
	if !m.sortByCost || !m.ready || m.viewport.YOffset() == 0 || m.viewport.YOffset() >= len(m.lineRows) {
		return ""
	}
	index := m.lineRows[m.viewport.YOffset()]
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
			m.viewport.SetYOffset(line)
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
		m.timeYOffset, m.timeFollow = m.viewport.YOffset(), m.autoScroll
		m.setFollow(false)
		m.sortByCost = true
		m.refreshViewport()
		m.viewport.GotoTop()
		return
	}
	m.sortByCost = false
	m.refreshViewport()
	if m.timeFollow {
		m.viewport.GotoBottom()
		m.setFollow(true)
		return
	}
	m.viewport.SetYOffset(m.timeYOffset)
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
	m.setFollow(false)
	m.refreshViewport()
	for line, index := range m.lineRows {
		if index == msg.Index {
			m.viewport.SetYOffset(line - m.viewport.Height()/2)
			break
		}
	}
}

// clearSelection drops the p highlight, re-rendering only when there was one.
func (m *BreakdownModel) clearSelection() {
	if m.selectedKey == "" {
		return
	}
	m.selectedKey = ""
	m.refreshViewport()
}

// selectionText describes the row p selected for the notify row:
//
//	top 1 of 5: #231 $0.4419 • cache write 12.0K 5m + 3.3K 1h • p next
//
// The cache-write split is what C_WR merges, and often why the row was
// expensive; it's left off when it doesn't fit, or there was no write.
func (m BreakdownModel) selectionText() string {
	// Runs on every frame; don't sort the rows when there's no selection
	if m.selectedKey == "" {
		return ""
	}
	top := m.topByCost()
	rank := m.selectedRank(top)
	if rank < 0 {
		return ""
	}
	msg := m.messages[top[rank]]
	parts := []string{fmt.Sprintf("top %d of %d: #%d %s", rank+1, len(top), msg.Index, render.Cost(msg.Cost.TotalCost))}
	switch w5m, w1h := render.CacheTokensByTTL(msg.Usage); {
	case w5m > 0 && w1h > 0:
		parts = append(parts, fmt.Sprintf("cache write %s 5m + %s 1h", render.Number(w5m), render.Number(w1h)))
	case w5m > 0:
		parts = append(parts, "cache write "+render.Number(w5m)+" 5m")
	case w1h > 0:
		parts = append(parts, "cache write "+render.Number(w1h)+" 1h")
	}
	parts = append(parts, "p next")
	sep := " " + styles.Bullet + " "
	// The key hint outranks the split: without it, p's next press is a guess.
	if m.width > 0 && len(parts) == 3 {
		if full := joinSegments(parts, sep, m.width-2); full != parts[0]+sep+parts[1]+sep+parts[2] {
			parts = []string{parts[0], parts[2]}
		}
	}
	return joinSegments(parts, sep, m.width-2)
}
