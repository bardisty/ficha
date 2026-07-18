package formatter

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/muesli/termenv"
)

// chartFixture returns n parallel costs/dates with one expensive session at
// index 0 ($50) and cheap ($0.10–$0.89) sessions after it, one day apart
// starting 2026-01-01. With n > 68 the peak is evicted from the drawn window.
func chartFixture(n int) ([]float64, []time.Time) {
	costs := make([]float64, n)
	dates := make([]time.Time, n)
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := range costs {
		costs[i] = 0.10 + float64(i%8)*0.10
		dates[i] = start.AddDate(0, 0, i)
	}
	costs[0] = 50.0
	return costs, dates
}

// Over-width dataset: labels must describe the drawn 68-session tail, not the
// full dataset — count discloses truncation, min/max exclude the evicted early
// peak, and the date line starts at the window's first visible session.
func TestRenderCostChartTruncationDisclosed(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	costs, dates := chartFixture(80)

	got := renderCostChart(costs, dates, 76, true)

	if !strings.Contains(got, "(last 68 of 80 sessions)") {
		t.Errorf("count label must disclose truncation, got:\n%s", got)
	}
	if strings.Contains(got, "max: $50") {
		t.Errorf("max must exclude the evicted $50 peak, got:\n%s", got)
	}
	if !strings.Contains(got, "max: $0.80") {
		t.Errorf("max must reflect the visible window's peak ($0.80), got:\n%s", got)
	}
	// Window starts at index 80-68=12 → 2026-01-13; full dataset starts 01 JAN
	if !strings.Contains(got, "13 JAN") {
		t.Errorf("date line must start at the visible window's first session (13 JAN), got:\n%s", got)
	}
	// Last session is 2026-01-01 + 79 days = 2026-03-21; a literal-"JAN"
	// layout bug would render it as "21 JAN"
	if !strings.Contains(got, "21 MAR") {
		t.Errorf("date line must render the real month (21 MAR), got:\n%s", got)
	}
	lines := strings.Split(got, "\n")
	dateLine := lines[len(lines)-2] // last content line before trailing ""
	if strings.Contains(dateLine, "01 JAN") {
		t.Errorf("date line must not span sessions off-screen (01 JAN), got: %q", dateLine)
	}
}

func TestRenderCostChartTruncationDisclosedStyled(t *testing.T) {
	forceProfile(t, termenv.ANSI256)
	costs, dates := chartFixture(80)

	got := renderCostChart(costs, dates, 76, false)

	if !strings.Contains(got, "(last 68 of 80 sessions)") {
		t.Errorf("styled count label must disclose truncation, got:\n%s", got)
	}
	if strings.Contains(got, "max: $50") {
		t.Errorf("styled max must exclude the evicted $50 peak, got:\n%s", got)
	}
	if !strings.Contains(got, "13 JAN") {
		t.Errorf("styled date line must start at the visible window (13 JAN), got:\n%s", got)
	}
}

// When everything fits, the label keeps the pre-windowing "(N sessions)" form
// and the date line spans the full dataset — no "last 68 of 68" noise, and
// existing summary goldens stay byte-identical.
func TestRenderCostChartNoTruncationKeepsPlainLabel(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	costs, dates := chartFixture(68)

	got := renderCostChart(costs, dates, 76, true)

	if !strings.Contains(got, "(68 sessions)") {
		t.Errorf("exact-fit render must use the plain count label, got:\n%s", got)
	}
	if strings.Contains(got, "last") {
		t.Errorf("exact-fit render must not claim truncation, got:\n%s", got)
	}
	if !strings.Contains(got, "max: $50") {
		t.Errorf("exact-fit max must include the $50 peak, got:\n%s", got)
	}
	if !strings.Contains(got, "01 JAN") {
		t.Errorf("exact-fit date line must start at the first session (01 JAN), got:\n%s", got)
	}
}

func TestRenderCostChartNoTruncationStyled(t *testing.T) {
	forceProfile(t, termenv.ANSI256)
	costs, dates := chartFixture(5)

	got := renderCostChart(costs, dates, 76, false)

	if !strings.Contains(got, "(5 sessions)") {
		t.Errorf("styled small render must use the plain count label, got:\n%s", got)
	}
	if !strings.Contains(got, "01 JAN") || !strings.Contains(got, fmt.Sprintf("%02d JAN", 5)) {
		t.Errorf("styled small render must span the full date range, got:\n%s", got)
	}
}
