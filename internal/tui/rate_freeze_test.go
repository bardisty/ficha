package tui

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// rateModel is watch on a session whose last 10 minutes cost $1.20, or
// $7.20/h, with a clock the test moves.
func rateModel(t *testing.T) (Model, *time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	a := tallAnalysis(30.05)
	a.MessageCount = 2
	a.Messages = []models.MessageAnalysis{
		{Timestamp: now.Add(-3 * time.Hour), Cost: models.CostBreakdown{TotalCost: 5}},
		{Timestamp: now.Add(-3 * time.Minute), Cost: models.CostBreakdown{TotalCost: 1.20}},
	}
	m := NewModel("/fixture/sess.jsonl", "sess", true, "", false)
	m.now = func() time.Time { return now }
	m = sized(t, m, 80, 24)
	m = load(t, m, a)
	return m, &now
}

func rateOf(m Model) string {
	for _, seg := range strings.Split(m.renderStatsLine(78), " │ ") {
		if strings.HasSuffix(seg, "(10m)") {
			return seg
		}
	}
	return ""
}

// While the notify row says the numbers are out of date, the rate stays
// what it was at the last load instead of falling with the clock.
func TestRateFreezesWhileTheDataIsStale(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  func(Model) any
	}{
		{"load failed", func(Model) any { return errorMsg{err: fs.ErrPermission} }},
		{"file gone", func(m Model) any { return fileWatchErrMsg{err: errSessionFileGone, watcher: m.watcher} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, now := rateModel(t)
			updated, _ := m.Update(tc.msg(m))
			m = updated.(Model)
			*now = now.Add(8 * time.Minute)
			if got := rateOf(m); got != "$7.20/h (10m)" {
				t.Errorf("stale rate = %q, want the last load's $7.20/h (10m)", got)
			}

			// A load that lands works the rate out against the clock again.
			m = load(t, m, m.analysis)
			if got := rateOf(m); got == "$7.20/h (10m)" {
				t.Errorf("rate after a good load = %q, want it against the clock", got)
			}
		})
	}
}

// A quiet session with nothing failing is slowing down, and its rate falls.
// So does one whose data is current though something else failed.
func TestRateFallsWhenTheDataIsCurrent(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  any
	}{
		{"quiet", nil},
		{"session watcher failed", errorMsg{err: errors.New("too many open files"), sessionWatcher: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, now := rateModel(t)
			if tc.msg != nil {
				updated, _ := m.Update(tc.msg)
				m = updated.(Model)
			}
			*now = now.Add(8 * time.Minute)
			if got := rateOf(m); got == "$7.20/h (10m)" {
				t.Errorf("rate = %q, want it lower as the clock moves", got)
			}
		})
	}
}

// A session quiet long enough for its rate to fall keeps that rate when its
// data then goes stale, rather than the one from its last load.
func TestRateFreezesWhenTheDataGoesStale(t *testing.T) {
	m, now := rateModel(t)
	*now = now.Add(30 * time.Minute)
	before := rateOf(m)
	updated, _ := m.Update(fileWatchErrMsg{err: errSessionFileGone, watcher: m.watcher})
	m = updated.(Model)
	*now = now.Add(time.Minute)
	updated, _ = m.Update(errorMsg{err: fs.ErrPermission})
	m = updated.(Model)
	if got := rateOf(m); got != before {
		t.Errorf("rate after going stale = %q, want %q, as it was when it went stale", got, before)
	}
}

// Once the data has been stale for a whole window, no rate speaks for the
// last 10 minutes.
func TestRateGoesUnknownAfterAStaleWindow(t *testing.T) {
	m, now := rateModel(t)
	updated, _ := m.Update(errorMsg{err: fs.ErrPermission})
	m = updated.(Model)
	*now = now.Add(rateWindow - time.Second)
	if got := rateOf(m); got != "$7.20/h (10m)" {
		t.Errorf("rate just inside the window = %q, want $7.20/h (10m)", got)
	}
	*now = now.Add(time.Second)
	if got := rateOf(m); got != "-/h (10m)" {
		t.Errorf("rate a window after going stale = %q, want -/h (10m)", got)
	}
}
