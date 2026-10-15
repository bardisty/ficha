package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/styles"
)

// datedBreakdown is loadedBreakdown with the clock at now, in UTC.
func datedBreakdown(t *testing.T, width, height int, now time.Time, msgs []models.BreakdownMessage) BreakdownModel {
	t.Helper()
	withLocal(t, time.UTC)
	m := NewBreakdownModel("/fixture/sess.jsonl", "0a1b2c3d", true, "", false)
	m.now = func() time.Time { return now }
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	updated, _ = updated.Update(breakdownMsgsMsg{messages: msgs, insights: &models.MessageInsights{}})
	return updated.(BreakdownModel)
}

// tableLinesOf is the table's lines from the top, trimmed.
func tableLinesOf(m BreakdownModel) []string {
	lines := strings.Split(tableContent(m), "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return lines
}

// A session from a past day opens under a divider, and g shows it above
// row 1. The footer still counts message rows only.
func TestBreakdownDated_PastSessionMarksFirstDay(t *testing.T) {
	m := datedBreakdown(t, 80, 24, goldenTime(12, 0, 0).AddDate(0, 0, 2), chromeRows(goldenTime(10, 0, 0), 40))
	if lines := tableLinesOf(m); lines[0] != "── Thu 15 Jan ──" || !strings.HasPrefix(lines[1], "1 ") {
		t.Errorf("a past session should open under its day:\n%s", strings.Join(lines[:3], "\n"))
	}
	m = press(t, m, "g")
	if m.viewport.YOffset() != 0 {
		t.Errorf("g should show the first divider, offset %d", m.viewport.YOffset())
	}
	if rule := m.renderFooterRule(m.panelWidth()); !strings.Contains(rule, "[ rows 1-13 of 40 ]") {
		t.Errorf("footer rule should count message rows: %q", rule)
	}
}

// A session that crossed midnight into today marks both days; one that's
// all today, or runs past the clock, marks neither.
func TestBreakdownDated_OnlyBeforeToday(t *testing.T) {
	crossing := datedBreakdown(t, 80, 24, goldenTime(12, 0, 0).AddDate(0, 0, 1), sortRows(40))
	lines := tableLinesOf(crossing)
	if lines[0] != "── Thu 15 Jan ──" || strings.Count(tableContent(crossing), "── Fri 16 Jan ──") != 1 {
		t.Errorf("a session that crossed midnight should mark both days:\n%s", strings.Join(lines[:3], "\n"))
	}
	today := datedBreakdown(t, 80, 24, goldenTime(12, 0, 0), chromeRows(goldenTime(10, 0, 0), 40))
	if strings.Contains(tableContent(today), "──") {
		t.Error("a session from today needs no divider")
	}
	// The clock reads Jan 15 noon, behind rows that run into Jan 16.
	ahead := datedBreakdown(t, 80, 24, goldenTime(12, 0, 0), sortRows(40))
	if lines := tableLinesOf(ahead); strings.HasPrefix(lines[0], "──") {
		t.Errorf("rows later than the clock count as today:\n%s", strings.Join(lines[:2], "\n"))
	}
	// A row with no timestamp has no day, so it can't date the session.
	zero := append([]models.BreakdownMessage{{Index: 1, Model: "claude-opus-4-8"}}, chromeRows(goldenTime(10, 0, 0), 3)...)
	for i := range zero[1:] {
		zero[i+1].Index = i + 2
	}
	if m := datedBreakdown(t, 80, 24, goldenTime(12, 0, 0), zero); m.dated || strings.Contains(tableContent(m), "──") {
		t.Errorf("a zero timestamp dated the session:\n%s", tableContent(m))
	}
}

// Sorted by cost, a dated session's TIME carries the weekday without the
// seconds, one column wider, and the header lines up with it. A session from
// today keeps its TIME as it was.
func TestBreakdownDated_SortedTimeHasWeekday(t *testing.T) {
	m := press(t, datedBreakdown(t, 80, 24, goldenTime(12, 0, 0).AddDate(0, 0, 1), sortRows(40)), "s")
	lines := tableLinesOf(m)
	if !strings.HasPrefix(lines[0], "6    Thu 23:55  ") {
		t.Errorf("sorted TIME should carry the weekday: %q", lines[0])
	}
	if !strings.HasPrefix(lines[2], "20   Fri 00:09  ") {
		t.Errorf("the next day's rows carry theirs: %q", lines[2])
	}
	header := m.table.header()
	if strings.Index(header, "MODEL") != strings.Index(m.renderLine(0), "Opus") {
		t.Errorf("header and rows disagree:\n%s\n%s", header, m.renderLine(0))
	}
	if m = press(t, m, "s"); m.table.timeFmt != timeClock || !strings.Contains(m.renderLine(1), "23:50:00") {
		t.Errorf("time order keeps the seconds and no weekday: %q", m.renderLine(1))
	}

	today := press(t, datedBreakdown(t, 80, 24, goldenTime(12, 0, 0), chromeRows(goldenTime(10, 0, 0), 40)), "s")
	if line := today.renderLine(0); !strings.Contains(line, "10:00:00") || strings.Contains(line, "Thu") {
		t.Errorf("a session from today sorts with its TIME unchanged: %q", line)
	}
}

// The Peak names the weekday in a dated session, the date too from six days
// back, and neither otherwise.
func TestBreakdownDated_PeakHasWeekday(t *testing.T) {
	msgs := chromeRows(goldenTime(10, 0, 0), 40)
	insights := &models.MessageInsights{
		HighestCost: &models.MessageSnapshot{Index: 3, Timestamp: msgs[2].Timestamp, Cost: 0.05},
		AverageCost: 0.05, MessageCount: 40,
	}
	for _, tt := range []struct {
		now  time.Time
		want string
	}{
		{goldenTime(12, 0, 0), "Peak: #3 $0.0500 @ 10:02:00"},
		{goldenTime(12, 0, 0).AddDate(0, 0, 3), "Peak: #3 $0.0500 @ Thu 10:02:00"},
		{goldenTime(12, 0, 0).AddDate(0, 0, 5), "Peak: #3 $0.0500 @ Thu 10:02:00 ("},
		{goldenTime(12, 0, 0).AddDate(0, 0, 6), "Peak: #3 $0.0500 @ Thu 15 Jan 10:02:00"},
	} {
		m := datedBreakdown(t, 100, 24, tt.now, msgs)
		m.insights = insights
		if got := m.renderCompactInsights(); !strings.Contains(got, tt.want) {
			t.Errorf("clock %v: insights %q, want %q", tt.now, got, tt.want)
		}
	}
}

// The view dates itself at midnight on the clock tick, with no load.
func TestBreakdownDated_TurnsDatedAtMidnight(t *testing.T) {
	now := goldenTime(23, 59, 0)
	m := datedBreakdown(t, 80, 24, now, chromeRows(goldenTime(10, 0, 0), 40))
	if m.dated {
		t.Fatal("setup: today's session shouldn't be dated")
	}
	now = now.Add(2 * time.Minute)
	m.now = func() time.Time { return now }
	updated, _ := m.Update(clockMsg{gen: m.clockGen})
	if m = updated.(BreakdownModel); !strings.HasPrefix(tableLinesOf(m)[0], "── Thu 15 Jan") {
		t.Errorf("after midnight the session should be dated:\n%s", tableLinesOf(m)[0])
	}
}

// Turning dated puts a divider above row 1. Scrolled down in time order, the
// reader keeps their row at the top; at the top they stay there and see the
// divider; following keeps the bottom. Sorted by cost, the time order's line
// moves with it, for s to return to.
func TestBreakdownDated_MidnightKeepsReadersLine(t *testing.T) {
	midnight := func(t *testing.T, m BreakdownModel) BreakdownModel {
		t.Helper()
		now := goldenTime(0, 1, 0).AddDate(0, 0, 1)
		m.now = func() time.Time { return now }
		updated, _ := m.Update(clockMsg{gen: m.clockGen})
		return updated.(BreakdownModel)
	}
	before := func() BreakdownModel {
		return datedBreakdown(t, 80, 24, goldenTime(23, 59, 0), chromeRows(goldenTime(10, 0, 0), 40))
	}

	m := press(t, press(t, before(), "g"), "j", "j", "j", "j", "j")
	top := tableLinesOf(m)[m.viewport.YOffset()]
	if !strings.HasPrefix(top, "6 ") {
		t.Fatalf("setup: row 6 should top the view: %q", top)
	}
	if m = midnight(t, m); tableLinesOf(m)[m.viewport.YOffset()] != top {
		t.Errorf("scrolled down, the top row moved: %q, want %q", tableLinesOf(m)[m.viewport.YOffset()], top)
	}

	if m = midnight(t, press(t, before(), "g")); m.viewport.YOffset() != 0 || !strings.HasPrefix(tableLinesOf(m)[0], "──") {
		t.Errorf("at the top the view should stay at offset 0 under the divider, at %d", m.viewport.YOffset())
	}

	if m = midnight(t, before()); !m.autoScroll || !m.viewport.AtBottom() {
		t.Error("following should keep the bottom")
	}

	m = press(t, press(t, press(t, before(), "g"), "j", "j", "j", "j", "j"), "s")
	if m = press(t, midnight(t, m), "s"); tableLinesOf(m)[m.viewport.YOffset()] != top {
		t.Errorf("s returned to %q, want %q", tableLinesOf(m)[m.viewport.YOffset()], top)
	}
}

// Short of room for the Peak's date, the line drops the multiplier first,
// then the date, then the weekday, then the time.
func TestBreakdownDated_PeakDateGivesWay(t *testing.T) {
	msgs := chromeRows(goldenTime(10, 0, 0), 40)
	for _, want := range []string{
		"Peak: #3 $0.0500 @ Thu 15 Jan 10:02:00 (1.0x avg)",
		"Peak: #3 $0.0500 @ Thu 15 Jan 10:02:00",
		"Peak: #3 $0.0500 @ Thu 10:02:00",
		"Peak: #3 $0.0500 @ 10:02:00",
		"Peak: #3 $0.0500",
	} {
		m := datedBreakdown(t, len(want)+4, 24, goldenTime(12, 0, 0).AddDate(0, 0, 8), msgs)
		m.noColor = true
		m.insights = &models.MessageInsights{
			HighestCost: &models.MessageSnapshot{Index: 3, Timestamp: msgs[2].Timestamp, Cost: 0.05},
			AverageCost: 0.05, MessageCount: 1, // too few for a Trend
		}
		if got := strings.TrimSpace(m.renderCompactInsights()); got != want {
			t.Errorf("at %d columns: %q, want %q", m.width, got, want)
		}
	}
}

// --ascii draws the divider with the ASCII rule.
func TestBreakdownDated_ASCIIDivider(t *testing.T) {
	styles.SetASCII(true)
	t.Cleanup(func() { styles.SetASCII(false) })
	m := datedBreakdown(t, 80, 24, goldenTime(12, 0, 0).AddDate(0, 0, 2), chromeRows(goldenTime(10, 0, 0), 4))
	if got := tableLinesOf(m)[0]; got != "-- Thu 15 Jan --" {
		t.Errorf("ASCII divider: %q", got)
	}
}

// At the narrowest width the table fits, sorting a dated session drops the
// weekday rather than turn the frame into the too-small screen. One column
// wider, the weekday fits.
func TestBreakdownDated_SortKeepsWidthFloor(t *testing.T) {
	m := press(t, datedBreakdown(t, 39, 15, goldenTime(12, 0, 0).AddDate(0, 0, 2), chromeRows(goldenTime(10, 0, 0), 40)), "s")
	if m.tooSmall() || m.table.timeFmt != timeClock {
		t.Errorf("sorted at 39 columns: too small %v, TIME %v; want it drawn without the weekday", m.tooSmall(), m.table.timeFmt)
	}
	m = press(t, datedBreakdown(t, 40, 15, goldenTime(12, 0, 0).AddDate(0, 0, 2), chromeRows(goldenTime(10, 0, 0), 40)), "s")
	if m.tooSmall() || m.table.timeFmt != timeWeekday {
		t.Errorf("sorted at 40 columns: too small %v, TIME %v; want the weekday", m.tooSmall(), m.table.timeFmt)
	}
}

// A week-old session's date needs three columns more than the weekday: short
// of them, TIME keeps the weekday, and short of that, the bare clock.
func TestBreakdownDated_DateGivesWayToWeekday(t *testing.T) {
	for _, tt := range []struct {
		width int
		want  timeFormat
	}{{39, timeClock}, {40, timeWeekday}, {42, timeWeekday}, {43, timeDate}} {
		m := press(t, datedBreakdown(t, tt.width, 15, goldenTime(12, 0, 0).AddDate(0, 0, 8), chromeRows(goldenTime(10, 0, 0), 40)), "s")
		if m.tooSmall() || m.table.timeFmt != tt.want {
			t.Errorf("sorted at %d columns: too small %v, TIME %v; want %v", tt.width, m.tooSmall(), m.table.timeFmt, tt.want)
		}
	}
}

// From six local days back, sorted TIME carries the date in place of the
// weekday, and the header lines up with it. Time order keeps the bare clock
// under its dividers.
func TestBreakdownDated_SortedTimeHasDate(t *testing.T) {
	m := press(t, datedBreakdown(t, 80, 24, goldenTime(0, 1, 0).AddDate(0, 0, 7), sortRows(40)), "s")
	lines := tableLinesOf(m)
	if !strings.HasPrefix(lines[0], "6    15 Jan 23:55  ") || !strings.HasPrefix(lines[2], "20   16 Jan 00:09  ") {
		t.Errorf("sorted TIME should carry the date:\n%s", strings.Join(lines[:3], "\n"))
	}
	if header := m.table.header(); strings.Index(header, "MODEL") != strings.Index(m.renderLine(0), "Opus") {
		t.Errorf("header and rows disagree:\n%s\n%s", header, m.renderLine(0))
	}
	if m = press(t, m, "s"); m.table.timeFmt != timeClock || !strings.Contains(m.renderLine(1), "23:50:00") {
		t.Errorf("time order keeps the bare clock: %q", m.renderLine(1))
	}
	// Jan 16's rows are six days back, but Jan 15's seven, which dates them all.
	if !m.weekOld {
		t.Error("a session with a row seven days back should be week-old")
	}
	five := datedBreakdown(t, 80, 24, goldenTime(12, 0, 0).AddDate(0, 0, 5), chromeRows(goldenTime(10, 0, 0), 4))
	six := datedBreakdown(t, 80, 24, goldenTime(0, 0, 0).AddDate(0, 0, 6), chromeRows(goldenTime(10, 0, 0), 4))
	if five.weekOld || !six.weekOld {
		t.Errorf("week-old at 5 days %v, at 6 %v; want false, true", five.weekOld, six.weekOld)
	}
}

// A session crosses the six-day line at midnight on the clock tick, as it
// turns dated, with no load.
func TestBreakdownDated_TurnsWeekOldAtMidnight(t *testing.T) {
	now := goldenTime(23, 59, 0).AddDate(0, 0, 5)
	m := press(t, datedBreakdown(t, 80, 24, now, chromeRows(goldenTime(10, 0, 0), 40)), "s")
	if m.weekOld || m.table.timeFmt != timeWeekday {
		t.Fatalf("setup: five days back should read the weekday, TIME %v", m.table.timeFmt)
	}
	now = now.Add(2 * time.Minute)
	m.now = func() time.Time { return now }
	updated, _ := m.Update(clockMsg{gen: m.clockGen})
	if m = updated.(BreakdownModel); m.table.timeFmt != timeDate || !strings.Contains(tableLinesOf(m)[0], "15 Jan 10:") {
		t.Errorf("after midnight sorted TIME should carry the date: %q", tableLinesOf(m)[0])
	}
}

// Where a zone skips midnight, as Santiago's does on its spring change, the
// day still starts at the date line: last night's 23:30 is yesterday's.
func TestBreakdownDated_DaySkipsMidnight(t *testing.T) {
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Skipf("no tz database: %v", err)
	}
	withLocal(t, loc)
	at := func(s string) time.Time {
		v, err := time.ParseInLocation(time.DateTime, s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	m := BreakdownModel{messages: []models.BreakdownMessage{{Index: 1, Timestamp: at("2026-09-05 23:30:00").UTC()}}}
	if dated, _ := m.isDated(at("2026-09-06 12:00:00")); !dated {
		t.Error("last night's row should date the session on the change day")
	}
	if _, weekOld := m.isDated(at("2026-09-11 12:00:00")); !weekOld {
		t.Error("six days later the row should be week-old")
	}
}
