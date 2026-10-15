package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/styles"
)

// splitRows is ten cheap rows with row 7 costing cost, so the first p
// selects row 7.
func splitRows(cost models.CostBreakdown) []models.BreakdownMessage {
	msgs := chromeRows(goldenTime(11, 0, 0), 10)
	for i := range msgs {
		msgs[i].Cost = models.CostBreakdown{OutputCost: 0.01, TotalCost: 0.01}
	}
	msgs[6].Cost = cost
	return msgs
}

// readHeavy is a late message on a 1M-window model: mostly cache read, with
// a small 5m cache write and an output too small to name.
var readHeavy = models.CostBreakdown{
	InputCost: 0.0004, OutputCost: 0.01, CacheWrite5mCost: 0.03, CacheReadCost: 0.19, TotalCost: 0.2304,
}

// threeWay is split three ways, with input too small to name.
var threeWay = models.CostBreakdown{
	InputCost: 0.0013, OutputCost: 0.0757, CacheWrite1hCost: 0.1487, CacheReadCost: 0.1222, TotalCost: 0.3479,
}

// p names the selected row's cost by component in dollars, largest first,
// with the cache write's TTL, and leaves out what's under 5% of the row.
func TestBreakdownSelection_SplitsCostByComponent(t *testing.T) {
	tests := []struct {
		name string
		cost models.CostBreakdown
		want string
	}{
		{"read heavy", readHeavy, "top 1 of 5: #7 $0.2304 • Cache read $0.1900, Cache write 5m $0.0300 • p next"},
		{"three way", threeWay, "top 1 of 5: #7 $0.3479 • Cache write 1h $0.1487, Cache read $0.1222, Output $0.0757 • p next"},
		{"one component", models.CostBreakdown{OutputCost: 0.2, TotalCost: 0.2}, "top 1 of 5: #7 $0.2000 • Output $0.2000 • p next"},
		// 5% is the cutoff exactly: 0.05 of 1.00 is named, 0.049 isn't.
		{"cutoff", models.CostBreakdown{InputCost: 0.049, OutputCost: 0.05, CacheReadCost: 0.901, TotalCost: 1},
			"top 1 of 5: #7 $1.00 • Cache read $0.9010, Output $0.0500 • p next"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := press(t, loadedBreakdown(t, 120, 24, splitRows(tt.cost)), "p")
			if got := m.selectionText(); got != tt.want {
				t.Errorf("selection:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// A line too long for the terminal drops components from the smallest end,
// then the split, and keeps p next to the last.
func TestBreakdownSelection_TrimsSplitBeforeKeyHint(t *testing.T) {
	tests := []struct {
		width int
		want  string
	}{
		{80, "top 1 of 5: #7 $0.3479 • Cache write 1h $0.1487, Cache read $0.1222 • p next"},
		{60, "top 1 of 5: #7 $0.3479 • Cache write 1h $0.1487 • p next"},
		{40, "top 1 of 5: #7 $0.3479 • p next"},
		{30, "top 1 of 5: #7 $0.3479"},
	}
	for _, tt := range tests {
		m := press(t, loadedBreakdown(t, tt.width, 24, splitRows(threeWay)), "p")
		if got := m.selectionText(); got != tt.want {
			t.Errorf("at %d columns:\n got %q\nwant %q", tt.width, got, tt.want)
		}
	}
}

// The compact frame has no notify row of its own: the selection takes the
// insights row's place, trimmed to the same width.
func TestBreakdownSelection_CompactFrame(t *testing.T) {
	m := press(t, loadedBreakdown(t, 100, 12, splitRows(threeWay)), "p")
	want := "  top 1 of 5: #7 $0.3479 • Cache write 1h $0.1487, Cache read $0.1222, Output $0.0757 • p next"
	if got := m.renderNotifyRow(); got != want {
		t.Errorf("notify row:\n got %q\nwant %q", got, want)
	}
	if lines := strings.Split(frameOf(m), "\n"); strings.TrimRight(lines[1], " ") != want {
		t.Errorf("the compact frame's second row should be the selection: %q", lines[1])
	}
}

// selectedIndex is the display Index of the row p selected, or 0.
func selectedIndex(m BreakdownModel) int {
	for _, msg := range m.messages {
		if breakdownMsgKey(msg) == m.selectedKey {
			return msg.Index
		}
	}
	return 0
}

// The selection holds through scrolling, the ends and sorting, so p goes on
// from its rank. The notify row's text lasts until the next key, and esc
// drops the selection.
func TestBreakdownSelection_HoldsThroughScrolling(t *testing.T) {
	// sortRows' top 5 are #6, #13, #20, #27 and #34.
	m := press(t, loadedBreakdown(t, 80, 24, sortRows(40)), "p", "p", "p", "j")
	if got := selectedIndex(m); got != 20 {
		t.Fatalf("after p p p j, selected #%d, want #20", got)
	}
	if row := m.renderNotifyRow(); row != "" {
		t.Errorf("after j the notify row should give way: %q", row)
	}
	m = press(t, m, "p")
	if got := m.renderNotifyRow(); !strings.HasPrefix(got, "  top 4 of 5: #27 ") {
		t.Errorf("p p p j p should land on top 4: %q", got)
	}
	for _, key := range []string{"g", "G", "s", "k", "s", "b"} {
		if m = press(t, m, key); selectedIndex(m) != 27 {
			t.Errorf("%s dropped the selection: selected #%d", key, selectedIndex(m))
		}
	}
	if m = press(t, m, "esc"); m.selectedKey != "" || m.renderNotifyRow() != "" {
		t.Errorf("esc should clear the selection: selected %q, notify row %q", m.selectedKey, m.renderNotifyRow())
	}
	if m = press(t, m, "p"); selectedIndex(m) != 6 {
		t.Errorf("after esc, p should start at the costliest row, got #%d", selectedIndex(m))
	}
}

// A hint about another session is newer than the selection, so it takes
// the notify row; the row stays marked, and p brings the selection back.
func TestBreakdownSelection_HintTakesNotifyRow(t *testing.T) {
	m := press(t, loadedBreakdown(t, 80, 24, sortRows(40)), "p")
	updated, _ := m.Update(sessionActivityMsg{path: "/fixture/other.jsonl", id: "other", created: true})
	m = updated.(BreakdownModel)
	if got := m.renderNotifyRow(); !strings.Contains(got, "other") || strings.Contains(got, "top 1 of 5") {
		t.Errorf("the hint should take the notify row: %q", got)
	}
	if selectedIndex(m) != 6 {
		t.Errorf("the hint dropped the selection: selected #%d", selectedIndex(m))
	}
	updated, _ = m.Update(sessionActivityMsg{path: "/fixture/third.jsonl", id: "third"})
	if m = press(t, updated.(BreakdownModel), "p"); !strings.HasPrefix(m.renderNotifyRow(), "  top 2 of 5: #13 ") {
		t.Errorf("p after the hint: %q", m.renderNotifyRow())
	}
	// More writes to sessions already active aren't news, even when two of
	// them take turns at the hint.
	for _, id := range []string{"other", "third", "other"} {
		updated, _ = m.Update(sessionActivityMsg{path: "/fixture/" + id + ".jsonl", id: id})
		if m = updated.(BreakdownModel); !strings.HasPrefix(m.renderNotifyRow(), "  top 2 of 5: #13 ") {
			t.Errorf("a write to %s took the row: %q", id, m.renderNotifyRow())
		}
	}
	// A session that just started is.
	updated, _ = m.Update(sessionActivityMsg{path: "/fixture/fourth.jsonl", id: "fourth", created: true})
	if m = updated.(BreakdownModel); !strings.Contains(m.renderNotifyRow(), "fourth") {
		t.Errorf("a new session should take the row: %q", m.renderNotifyRow())
	}
	// So is a hint after the last one faded.
	m = press(t, m, "p")
	later := goldenTime(12, 0, 0).Add(idleAfter + time.Second)
	m.now = func() time.Time { return later }
	updated, _ = m.Update(sessionActivityMsg{path: "/fixture/other.jsonl", id: "other"})
	if m = updated.(BreakdownModel); !strings.Contains(m.renderNotifyRow(), "other") {
		t.Errorf("a hint after the last faded should take the row: %q", m.renderNotifyRow())
	}
	if m = press(t, m, "n"); m.selectedKey != "" {
		t.Error("a session switch should clear the selection")
	}

	// The session left in a switch, writing in turn with another already
	// hinted, isn't news either, once the switch notice is gone.
	m = loadedBreakdown(t, 80, 24, sortRows(40))
	updated, _ = m.Update(sessionActivityMsg{path: "/fixture/other.jsonl", id: "other"})
	m = press(t, updated.(BreakdownModel), "n")
	updated, _ = m.Update(breakdownMsgsMsg{messages: sortRows(40), insights: &models.MessageInsights{}, sessionPath: "/fixture/other.jsonl"})
	updated, _ = updated.Update(sessionActivityMsg{path: "/fixture/third.jsonl", id: "third"})
	m = press(t, updated.(BreakdownModel), "p")
	for _, id := range []string{"0a1b2c3d", "third", "0a1b2c3d", "third"} {
		updated, _ = m.Update(sessionActivityMsg{path: "/fixture/" + id + ".jsonl", id: id})
		if m = updated.(BreakdownModel); !strings.HasPrefix(m.renderNotifyRow(), "  top 1 of 5") {
			t.Errorf("a write to %s took the row from the selection: %q", id, m.renderNotifyRow())
		}
	}

	// Nor does the session left, live on its own: its writes during the
	// notice are its hint, so the ones after p aren't news.
	m = loadedBreakdown(t, 80, 24, sortRows(40))
	updated, _ = m.Update(sessionActivityMsg{path: "/fixture/other.jsonl", id: "other"})
	m = press(t, updated.(BreakdownModel), "n")
	updated, _ = m.Update(breakdownMsgsMsg{messages: sortRows(40), insights: &models.MessageInsights{}, sessionPath: "/fixture/other.jsonl"})
	left := sessionActivityMsg{path: "/fixture/0a1b2c3d.jsonl", id: "0a1b2c3d"}
	updated, _ = updated.Update(left)
	m = press(t, updated.(BreakdownModel), "p")
	updated, _ = m.Update(left)
	if m = updated.(BreakdownModel); !strings.HasPrefix(m.renderNotifyRow(), "  top 1 of 5") {
		t.Errorf("the session left took the row from p: %q", m.renderNotifyRow())
	}
}

// The selected row carries a mark in its indent, which every row leaves
// blank, so no column moves: ▸ in color and without, > with --ascii.
func TestBreakdownSelection_MarksRowWithoutColor(t *testing.T) {
	for _, tt := range []struct {
		name    string
		noColor bool
		ascii   bool
		mark    string
	}{
		{"no color", true, false, "▸ "},
		{"color", false, false, "▸ "},
		{"ascii", true, true, "> "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			styles.SetASCII(tt.ascii)
			t.Cleanup(func() { styles.SetASCII(false) })
			m := loadedBreakdown(t, 80, 24, sortRows(40))
			m.noColor = tt.noColor
			m = press(t, m, "p")
			var marked, plain string
			for i, index := range m.lineRows {
				line := stripANSI(m.renderLine(i))
				switch index {
				case 6:
					marked = line
				case 7:
					plain = line
				}
			}
			if !strings.HasPrefix(marked, tt.mark+"6 ") {
				t.Errorf("selected row: %q, want it to start %q", marked, tt.mark)
			}
			if !strings.HasPrefix(plain, "  7 ") || lipgloss.Width(marked) != lipgloss.Width(plain) {
				t.Errorf("the mark moved a column:\n%q\n%q", marked, plain)
			}
		})
	}
}
