package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// sortRows is n rows a minute apart, crossing midnight, each costing its
// row number mod 7 cents, so cost order differs from time order.
func sortRows(n int) []models.BreakdownMessage {
	msgs := chromeRows(goldenTime(23, 50, 0), n)
	for i := range msgs {
		msgs[i].Cost.TotalCost = float64((i+1)%7) / 100
	}
	return msgs
}

// Sorted, the table runs most expensive first under each row's own number,
// with no day dividers, and the footer rule names the mode.
func TestBreakdownSort_OrdersByCost(t *testing.T) {
	m := loadedBreakdown(t, 100, 24, sortRows(40))
	if !strings.Contains(tableContent(m), "── ") {
		t.Fatal("setup: time order should draw a day divider at midnight")
	}
	m = press(t, m, "s")
	lines := strings.Split(tableContent(m), "\n")
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "6 ") || !strings.HasPrefix(strings.TrimSpace(lines[1]), "13 ") {
		t.Errorf("cost order should open on #6 then #13 (ties by time):\n%s", strings.Join(lines[:3], "\n"))
	}
	for _, l := range lines {
		if strings.Contains(l, "── ") {
			t.Errorf("no day dividers in cost order: %q", l)
		}
	}
	if m.viewport.YOffset != 0 {
		t.Errorf("sorting should open at the top, got offset %d", m.viewport.YOffset)
	}
	if rule := m.renderFooterRule(m.panelWidth()); !strings.Contains(rule, "[ sorted by cost (s) • rank 1-14 of 40 ]") {
		t.Errorf("footer rule: %q", rule)
	}
}

// tableContent is the whole table, the rows and dividers, as the viewport
// would show it scrolled end to end.
func tableContent(m BreakdownModel) string {
	lines := make([]string, len(m.lineRows))
	for i := range m.lineRows {
		lines[i] = m.renderLine(i)
	}
	return strings.Join(lines, "\n")
}

// Sorted, new rows load and count, but the view holds the row the reader
// is looking at, even when a new row sorts in above it; neither G nor
// scrolling to the bottom resumes following. At the very top, a new most
// expensive row shows at the top.
func TestBreakdownSort_PausesFollowing(t *testing.T) {
	withPeak := append(sortRows(40), models.BreakdownMessage{
		Index: 41, Timestamp: goldenTime(23, 50, 0).Add(40 * time.Minute), Model: "claude-opus-4-8",
		Cost: models.CostBreakdown{TotalCost: 1},
	})
	m := press(t, loadedBreakdown(t, 100, 24, sortRows(40)), "s", "j", "j")
	top := m.lineRows[m.viewport.YOffset]
	updated, _ := m.Update(breakdownMsgsMsg{messages: withPeak, insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if got := m.lineRows[m.viewport.YOffset]; got != top || m.autoScroll {
		t.Errorf("a new row above moved the sorted view: top row #%d, want #%d; following %v", got, top, m.autoScroll)
	}
	if rule := m.renderFooterRule(m.panelWidth()); !strings.Contains(rule, "[ sorted by cost (s) • rank 4-17 of 41 ]") {
		t.Errorf("footer rule: %q", rule)
	}
	if !strings.Contains(m.renderStatsLine(m.layout()), "Messages: 41") {
		t.Errorf("stats line should count the new row: %q", m.renderStatsLine(m.layout()))
	}
	if m = press(t, m, "G"); m.autoScroll {
		t.Error("G in cost order should not resume following")
	}

	m = press(t, loadedBreakdown(t, 100, 24, sortRows(40)), "s")
	updated, _ = m.Update(breakdownMsgsMsg{messages: withPeak, insights: &models.MessageInsights{}})
	if m = updated.(BreakdownModel); m.viewport.YOffset != 0 || m.lineRows[0] != 41 {
		t.Errorf("at the top, the new peak should show first: offset %d, top row #%d", m.viewport.YOffset, m.lineRows[0])
	}
}

// s again restores time order where the reader left it: following if they
// were following, at their scroll position if they had scrolled up.
func TestBreakdownSort_RestoresTimeOrder(t *testing.T) {
	m := press(t, loadedBreakdown(t, 100, 24, sortRows(40)), "s")
	updated, _ := m.Update(breakdownMsgsMsg{messages: sortRows(45), insights: &models.MessageInsights{}})
	m = press(t, updated.(BreakdownModel), "s")
	if !m.autoScroll || !m.viewport.AtBottom() {
		t.Errorf("following before s should follow after it: following %v, at bottom %v", m.autoScroll, m.viewport.AtBottom())
	}

	m = press(t, loadedBreakdown(t, 100, 24, sortRows(40)), "g", "j", "j", "j")
	offset := m.viewport.YOffset
	m = press(t, m, "s", "j", "s")
	if m.viewport.YOffset != offset || m.autoScroll {
		t.Errorf("scrolled up before s: offset %d, want %d; following %v", m.viewport.YOffset, offset, m.autoScroll)
	}
	if !strings.Contains(tableContent(m), "── ") {
		t.Error("time order should draw its day dividers again")
	}
}

// p still walks the most expensive rows; sorted, the peak is the top row.
func TestBreakdownSort_PeakIsAtTheTop(t *testing.T) {
	m := press(t, loadedBreakdown(t, 100, 24, sortRows(40)), "s", "G", "p")
	if m.viewport.YOffset != 0 || m.selectedKey != breakdownMsgKey(m.messages[5]) {
		t.Errorf("p in cost order: offset %d, selected %q", m.viewport.YOffset, m.selectedKey)
	}
	if !strings.Contains(m.renderNotifyRow(), "top 1 of 5: #6") {
		t.Errorf("notify row: %q", m.renderNotifyRow())
	}
}

// Another session opens in time order, following, whatever the last showed.
func TestBreakdownSort_SessionSwitchResets(t *testing.T) {
	m := press(t, loadedBreakdown(t, 100, 24, sortRows(40)), "f", "s")
	updated, _ := m.Update(sessionActivityMsg{path: "/fixture/new.jsonl", id: "new", created: true})
	m = updated.(BreakdownModel)
	if m.sortByCost || !m.autoScroll {
		t.Errorf("after a switch: sorted %v, following %v", m.sortByCost, m.autoScroll)
	}
}

// A table that empties while sorted (a truncated transcript) can still be
// put back in time order, and following resumes.
func TestBreakdownSort_LeavesWhenEmpty(t *testing.T) {
	m := press(t, loadedBreakdown(t, 100, 24, sortRows(40)), "s")
	updated, _ := m.Update(breakdownMsgsMsg{insights: &models.MessageInsights{}})
	m = press(t, updated.(BreakdownModel), "s")
	if m.sortByCost || !m.autoScroll {
		t.Errorf("s on an empty sorted table: sorted %v, following %v", m.sortByCost, m.autoScroll)
	}
	if m = press(t, m, "s"); m.sortByCost {
		t.Error("s on an empty table should not sort it")
	}
}
