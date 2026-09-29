package tui

import (
	"fmt"
	"sort"

	keybind "github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// isDownKey reports whether msg is one of the viewport keymap's downward
// scroll keys.
func isDownKey(km viewport.KeyMap, msg tea.KeyMsg) bool {
	return keybind.Matches(msg, km.Down, km.PageDown, km.HalfPageDown)
}

// topCount is how many of the most expensive rows p cycles through.
const topCount = 5

// repeatedRune returns how many times a key chunk repeats one rune, or 0
// when it isn't a repeat: a single key, a paste, or mixed text.
func repeatedRune(msg tea.KeyMsg) int {
	if msg.Type != tea.KeyRunes || msg.Paste || len(msg.Runes) < 2 {
		return 0
	}
	for _, r := range msg.Runes[1:] {
		if r != msg.Runes[0] {
			return 0
		}
	}
	return len(msg.Runes)
}

// topByCost returns the positions in m.messages of the most expensive rows,
// most expensive first; ties go to the earlier row.
func (m BreakdownModel) topByCost() []int {
	pos := make([]int, len(m.messages))
	for i := range pos {
		pos[i] = i
	}
	sort.SliceStable(pos, func(a, b int) bool {
		return m.messages[pos[a]].Cost.TotalCost > m.messages[pos[b]].Cost.TotalCost
	})
	return pos[:min(topCount, len(pos))]
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
			m.viewport.SetYOffset(line - m.viewport.Height/2)
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
