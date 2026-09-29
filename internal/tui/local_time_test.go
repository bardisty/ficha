package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"

	"github.com/bardisty/ficha/internal/models"
)

// withLocal swaps the process timezone for one test. TestMain pins UTC and
// nothing in this package runs in parallel.
func withLocal(t *testing.T, loc *time.Location) {
	t.Helper()
	orig := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = orig })
}

func breakdownViewFor(t *testing.T, msgs []models.BreakdownMessage, noColor bool) string {
	t.Helper()
	m := NewBreakdownModel("/fixture/sess.jsonl", "0a1b2c3d", noColor, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(BreakdownModel)
	updated, _ = m.Update(breakdownMsgsMsg{messages: msgs, totalCost: 0.3, minCost: 0.1, maxCost: 0.1})
	return updated.(BreakdownModel).View()
}

func crossMidnightMessages() []models.BreakdownMessage {
	// 22:50 and 23:10 UTC on the 28th: the same UTC day, but in UTC+1 the
	// second lands after local midnight. The third stays on the 29th.
	ts := []time.Time{
		time.Date(2026, 9, 28, 22, 50, 0, 0, time.UTC),
		time.Date(2026, 9, 28, 23, 10, 0, 0, time.UTC),
		time.Date(2026, 9, 29, 0, 30, 0, 0, time.UTC),
	}
	msgs := make([]models.BreakdownMessage, len(ts))
	for i, tt := range ts {
		msgs[i] = models.BreakdownMessage{
			Index: i + 1, Timestamp: tt, Model: "claude-opus-4-8",
			Cost: models.CostBreakdown{TotalCost: 0.1},
		}
	}
	return msgs
}

// Rows show local time, and a marker row appears where the local day changes,
// which is not where the UTC day changes.
func TestBreakdownRowsUseLocalTimeAndMarkDayChange(t *testing.T) {
	withLocal(t, time.FixedZone("UTC+1", 3600))
	forceProfile(t, termenv.Ascii)

	for _, noColor := range []bool{true, false} {
		out := breakdownViewFor(t, crossMidnightMessages(), noColor)
		for _, want := range []string{"23:50:00", "00:10:00", "01:30:00"} {
			if !strings.Contains(out, want) {
				t.Errorf("noColor=%v: missing local row time %s", noColor, want)
			}
		}
		if strings.Contains(out, "22:50:00") {
			t.Errorf("noColor=%v: row still shows UTC time", noColor)
		}
		if got := strings.Count(out, "Tue 29 Sep"); got != 1 {
			t.Errorf("noColor=%v: want one day marker for Tue 29 Sep, got %d\n%s", noColor, got, out)
		}
		if strings.Contains(out, "Mon 28 Sep") {
			t.Errorf("noColor=%v: the first day needs no marker\n%s", noColor, out)
		}
		// The marker sits between row 1 (23:50) and row 2 (00:10)
		lines := strings.Split(out, "\n")
		for i, l := range lines {
			if strings.Contains(l, "Tue 29 Sep") {
				if i == 0 || i+1 >= len(lines) || !strings.Contains(lines[i-1], "23:50:00") || !strings.Contains(lines[i+1], "00:10:00") {
					t.Errorf("noColor=%v: marker is not between the day's last and next rows\n%s", noColor, out)
				}
			}
		}
	}
}

// In UTC the same three rows span two UTC days, so the marker moves with the
// zone: it goes before the 00:30 row.
func TestBreakdownDayMarkerFollowsZone(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	out := breakdownViewFor(t, crossMidnightMessages(), true)
	lines := strings.Split(out, "\n")
	found := false
	for i, l := range lines {
		if strings.Contains(l, "Tue 29 Sep") {
			found = true
			if i+1 >= len(lines) || !strings.Contains(lines[i+1], "00:30:00") {
				t.Errorf("UTC marker should precede the 00:30 row\n%s", out)
			}
		}
	}
	if !found {
		t.Errorf("missing UTC day marker\n%s", out)
	}
}

// The watch view's insight times are local, matching its header clock.
func TestWatchInsightTimesAreLocal(t *testing.T) {
	withLocal(t, time.FixedZone("UTC-7", -7*3600))
	forceProfile(t, termenv.Ascii)

	for _, noColor := range []bool{true, false} {
		m := NewModel("/fixture/sess.jsonl", "0a1b2c3d", false, noColor, "", false)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 60})
		m = updated.(Model)
		updated, _ = m.Update(analysisMsg{analysis: goldenViewAnalysis()})
		out := updated.(Model).View()
		// Fixture peak is 10:42:13 UTC
		if !strings.Contains(out, "03:42:13") || strings.Contains(out, "10:42:13") {
			t.Errorf("noColor=%v: peak time not converted to local\n%s", noColor, out)
		}
	}
}
