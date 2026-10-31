package formatter

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/models"
)

// chartFixture returns n points, oldest first, with one expensive session
// first ($50) and cheap ($0.10–$0.80) sessions after it, numbered newest
// first as the table numbers them. With n > 68 the peak is evicted from the
// drawn window.
func chartFixture(n int) []chartPoint {
	points := make([]chartPoint, n)
	for i := range points {
		points[i] = chartPoint{cost: 0.10 + float64(i%8)*0.10, row: n - i, id: fmt.Sprintf("s%07d", i)}
	}
	points[0].cost = 50.0
	return points
}

// Over-width dataset: labels must describe the drawn 68-session tail, not the
// full dataset — count discloses truncation, min/max exclude the evicted
// early peak.
func TestRenderCostChartTruncationDisclosed(t *testing.T) {
	for _, noColor := range []bool{true, false} {
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

// The caption names the max and min sessions by the table's row number and
// short ID, max first, from the drawn tail only.
func TestRenderCostChartNamesExtremes(t *testing.T) {
	points := chartFixture(15)
	points[9].cost = 7.25 // row 6, the known peak
	got := renderCostChart(points, 76, true)
	if want := "    max: $50.00 (#15 s0000000)  min: $0.1000 (#7 s0000008)\n"; !strings.Contains(got, want) {
		t.Errorf("want %q in:\n%s", want, got)
	}
	points[0].cost = 0.30
	got = renderCostChart(points, 76, true)
	if want := "    max: $7.25 (#6 s0000009)  min: $0.1000 (#7 s0000008)\n"; !strings.Contains(got, want) {
		t.Errorf("want %q in:\n%s", want, got)
	}

	// With more sessions than the chart's width, the names come from the
	// points drawn: the $50 session is off the chart. A tie names the newer
	// session.
	got = renderCostChart(chartFixture(80), 76, true)
	if want := "    max: $0.8000 (#1 s0000079)  min: $0.1000 (#8 s0000072)\n"; !strings.Contains(got, want) {
		t.Errorf("want %q in:\n%s", want, got)
	}
}

// A narrow report shortens the caption: the count's words, keeping the
// direction longest, since the table under the chart runs the other way;
// the extremes' IDs, then min's row, then max's. Every line fits.
func TestRenderCostChartCaptionNarrows(t *testing.T) {
	points := chartFixture(150)
	points[149].cost = 1234.56
	for _, tt := range []struct {
		width         int
		order, extrem string
	}{
		{80, "last 68 of 150 sessions with a cost, oldest → newest", "max: $1,234.56 (#1 s0000149)  min: $0.1000 (#6 s0000144)"},
		{55, "last 47 of 150 with a cost, oldest → newest", "max: $1,234.56 (#1)  min: $0.1000 (#6)"},
		{50, "last 42 of 150 with a cost, oldest → newest", "max: $1,234.56 (#1)  min: $0.1000 (#6)"},
		{45, "last 37 of 150, oldest → newest", "max: $1,234.56 (#1)  min: $0.1000 (#6)"},
		{38, "last 30 of 150, oldest → newest", "max: $1,234.56 (#1)  min: $0.1000"},
		{32, "last 24 of 150 with a cost", "max: $1,234.56  min: $0.1000"},
	} {
		got := renderCostChart(points, tt.width, true)
		lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
		if o, e := lines[len(lines)-2], lines[len(lines)-1]; o != "    "+tt.order || e != "    "+tt.extrem {
			t.Errorf("width %d: caption\n%q\n%q\nwant\n%q\n%q", tt.width, o, e, "    "+tt.order, "    "+tt.extrem)
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > tt.width {
				t.Errorf("width %d: line is %d columns: %q", tt.width, w, l)
			}
		}
	}
}

// The caption's row numbers are the table's, in either order, with error and
// no-reply rows counted.
func TestSummaryChartCaptionMatchesTableRows(t *testing.T) {
	var results []models.SessionResult
	for day, cost := range []float64{1.10, 2.20, 9.75, 0.40, 3.30, 1.70} {
		results = append(results, sessionRow(fmt.Sprintf("a000000%d", day+1), "claude-opus-4-8", day+1, cost))
	}
	noReply := sessionRow("b0000007", "claude-opus-4-8", 7, 0)
	noReply.Analysis.MessageCount = 0
	failed := sessionRow("c0000008", "claude-opus-4-8", 8, 0)
	failed.Analysis = nil
	results = append(results, noReply, failed)
	aggregate := &models.SessionAnalysis{
		SessionID: "aggregate", IsSummary: true, SessionCount: len(results),
		CostByModel: map[string]models.CostBreakdown{},
	}
	captionRe := regexp.MustCompile(`max: \$\S+ \(#(\d+) (\w+)\)  min: \$\S+ \(#(\d+) (\w+)\)`)
	for _, sortBy := range []string{SortByTime, SortByCost} {
		out := FormatSummaryTableWithDetails(aggregate, results, "/home/user/src/app", true, false, 80, sortBy)
		m := captionRe.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("%s: no caption naming its rows:\n%s", sortBy, out)
		}
		if m[2] != "a0000003" || m[4] != "a0000004" {
			t.Errorf("%s: caption names %s and %s, want a0000003 and a0000004:\n%s", sortBy, m[2], m[4], out)
		}
		for _, pair := range [][2]string{{m[1], m[2]}, {m[3], m[4]}} {
			if !regexp.MustCompile(`(?m)^ +` + pair[0] + ` ` + pair[1] + ` `).MatchString(out) {
				t.Errorf("%s: caption says #%s is %s, the table doesn't:\n%s", sortBy, pair[0], pair[1], out)
			}
		}
	}
}
