package formatter

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// chartFixture returns n costs, oldest first, with one expensive session
// first ($50) and cheap ($0.10–$0.80) sessions after it. With n > 68 the
// peak is evicted from the drawn window.
func chartFixture(n int) []float64 {
	costs := make([]float64, n)
	for i := range costs {
		costs[i] = 0.10 + float64(i%8)*0.10
	}
	costs[0] = 50.0
	return costs
}

// Over-width dataset: labels must describe the drawn 68-session tail, not the
// full dataset — count discloses truncation, min/max exclude the evicted
// early peak.
func TestRenderCostChartTruncationDisclosed(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		if noColor {
			forceProfile(t, termenv.Ascii)
		} else {
			forceProfile(t, termenv.ANSI256)
		}
		got := renderCostChart(chartFixture(80), 76, noColor)
		if !strings.Contains(got, "last 68 of 80 sessions with a cost, oldest") {
			t.Errorf("noColor=%v: count label must disclose truncation, got:\n%s", noColor, got)
		}
		if strings.Contains(got, "max: $50") || !strings.Contains(got, "max: $0.80") {
			t.Errorf("noColor=%v: max must reflect the visible window ($0.80), got:\n%s", noColor, got)
		}
	}
}

// The chart is as wide as its points, so the first session's bar is at the
// left edge rather than the points bunching against the right one. The axis
// says the points are sessions in order, not dates.
func TestRenderCostChartSizedToPoints(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	got := renderCostChart(chartFixture(15), 76, true)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	bars := lines[:len(lines)-2]
	for _, l := range bars {
		if w := lipgloss.Width(l); w > 4+15 {
			t.Errorf("chart line is %d columns, want at most 19 for 15 points: %q", w, l)
		}
	}
	if !strings.Contains(got, "15 sessions with a cost, oldest") || !strings.Contains(got, "newest") {
		t.Errorf("want an axis label, got:\n%s", got)
	}
	if strings.Contains(got, "JAN") || strings.Contains(got, "Jan") {
		t.Errorf("no date labels on an axis spaced by session:\n%s", got)
	}
}

// Too few points for a chart to say more than the table under it.
func TestRenderCostChartNeedsFivePoints(t *testing.T) {
	if got := renderCostChart(chartFixture(4), 76, true); got != "" {
		t.Errorf("4 points should draw no chart, got:\n%s", got)
	}
	if got := renderCostChart(chartFixture(5), 76, true); got == "" {
		t.Error("5 points should draw a chart")
	}
}
