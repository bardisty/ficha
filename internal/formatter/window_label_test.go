package formatter

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/bardisty/ficha/internal/models"
)

// Each window reads once, the same in the header and the empty line: an open
// start as "up to", one whole day as the day alone, and anything else as a
// range, clock times kept.
func TestWindowLabel(t *testing.T) {
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.Local) }
	at := func(h int) time.Time { return time.Date(2026, 1, 15, h, 0, 0, 0, time.Local) }
	tests := []struct {
		name  string
		w     models.TimeWindow
		want  string
		empty string
	}{
		{"open start, a date", models.TimeWindow{Until: day(1, 1)}, "up to Dec 31 2025", "No messages in ~/work/webapp: up to Dec 31 2025."},
		{"open start, a time", models.TimeWindow{Until: at(14)}, "up to Jan 15 14:00", ""},
		{"open end", models.TimeWindow{Since: day(1, 8)}, "Jan 08 → now", "No messages in ~/work/webapp: Jan 08 → now."},
		{"one whole day", models.TimeWindow{Since: day(1, 15), Until: day(1, 16)}, "Jan 15", "No messages in ~/work/webapp: Jan 15."},
		{"two whole days", models.TimeWindow{Since: day(1, 14), Until: day(1, 16)}, "Jan 14 → Jan 15", ""},
		{"hours within a day", models.TimeWindow{Since: at(13), Until: at(14)}, "Jan 15 13:00 → Jan 15 14:00", ""},
		{"midnight to a time", models.TimeWindow{Since: day(1, 15), Until: at(14)}, "Jan 15 → Jan 15 14:00", ""},
	}
	for _, tt := range tests {
		if got := windowLabel(tt.w); got != tt.want {
			t.Errorf("%s: label %q, want %q", tt.name, got, tt.want)
		}
		if tt.empty == "" {
			continue
		}
		if got := emptyWindow(tt.w, "~/work/webapp"); got != tt.empty {
			t.Errorf("%s: empty line %q, want %q", tt.name, got, tt.empty)
		}
	}
	if got, want := emptyWindow(models.TimeWindow{Until: day(1, 1)}, ""), "No messages in any project: up to Dec 31 2025."; got != want {
		t.Errorf("global's empty line %q, want %q", got, want)
	}

	useASCII(t)
	if got, want := windowLabel(models.TimeWindow{Since: day(1, 14), Until: day(1, 16)}), "Jan 14 -> Jan 15"; got != want {
		t.Errorf("--ascii: label %q, want %q", got, want)
	}
}

// Santiago's clocks jump from 00:00 to 01:00 on 2026-09-06, so that day
// starts at 01:00, and a whole day there must still read as its date.
func TestWindowLabelSkippedMidnight(t *testing.T) {
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	orig := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = orig })

	dayStart := time.Date(2026, 9, 6, 1, 0, 0, 0, loc)
	tests := []struct {
		name string
		w    models.TimeWindow
		want string
	}{
		{"one whole day", models.TimeWindow{Since: dayStart, Until: time.Date(2026, 9, 7, 0, 0, 0, 0, loc)}, "Sep 06"},
		{"up to the day before", models.TimeWindow{Until: dayStart}, "up to Sep 05"},
		{"two whole days", models.TimeWindow{Since: time.Date(2026, 9, 5, 0, 0, 0, 0, loc), Until: time.Date(2026, 9, 7, 0, 0, 0, 0, loc)}, "Sep 05 → Sep 06"},
	}
	for _, tt := range tests {
		if got := windowLabel(tt.w); got != tt.want {
			t.Errorf("%s: label %q, want %q", tt.name, got, tt.want)
		}
	}
}
