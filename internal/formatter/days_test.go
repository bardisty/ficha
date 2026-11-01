package formatter

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bardisty/ficha/internal/models"
)

// Where daylight saving skips midnight, as Chile's did on 6 September
// 2026, the day still reads as itself, not the day before.
func TestDayLabelsWhereMidnightIsSkipped(t *testing.T) {
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Skipf("no zone data: %v", err)
	}
	saved := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = saved })
	if m := time.Date(2026, 9, 6, 0, 0, 0, 0, loc); m.Day() == 6 && m.Hour() == 0 {
		t.Skip("zone data doesn't skip that midnight")
	}
	got := dayLabels([]models.DayCost{{Date: "2026-09-06"}, {Date: "2025-09-06"}}, true)
	if got[0] != "Sun Sep 06" || got[1] != "Sat Sep 06 2025" {
		t.Errorf("labels %q, want Sun Sep 06 and Sat Sep 06 2025", got)
	}
}

// A terminal too narrow for every column drops the weekday, then SESSIONS,
// so a $1,000 day beside one from last year still fits in 45 columns.
func TestDaysSectionFitsNarrowTerminals(t *testing.T) {
	days := []models.DayCost{
		{Date: "2026-01-15", SessionCount: 3, MessageCount: 1_204, TotalCost: models.CostBreakdown{TotalCost: 1_042.5}},
		{Date: "2025-12-31", SessionCount: 1, MessageCount: 18, TotalCost: models.CostBreakdown{TotalCost: 0.4072}},
	}
	for _, width := range []int{45, 50, 76} {
		out := renderDaysSection(days, width, true)
		for _, line := range strings.Split(out, "\n") {
			if n := utf8.RuneCountInString(line); n > width {
				t.Errorf("width %d: a %d-column line: %q", width, n, line)
			}
		}
		if width == 76 && (!strings.Contains(out, "SESSIONS") || !strings.Contains(out, "Wed Dec 31 2025")) {
			t.Errorf("width 76 should fit every column and weekday:\n%s", out)
		}
		if width == 45 && (strings.Contains(out, "SESSIONS") || !strings.Contains(out, "Dec 31 2025")) {
			t.Errorf("width 45 should drop SESSIONS and keep the date:\n%s", out)
		}
	}
}
