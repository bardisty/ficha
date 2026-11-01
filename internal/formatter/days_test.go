package formatter

import (
	"testing"
	"time"

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
