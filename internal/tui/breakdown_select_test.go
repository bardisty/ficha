package tui

import (
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
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
