package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// dayLine is a reply at ts, every one the same price, so a day's cost is
// its message count times dayLineCost.
func dayLine(ts, id string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"req_%s","message":{"id":"msg_%s","model":"claude-opus-4-8","usage":{"input_tokens":100,"output_tokens":300}}}`, ts, id, id)
}

var dayLineCost = CalculateCost(models.TokenUsage{InputTokens: 100, OutputTokens: 300}, "claude-opus-4-8").TotalCost

// inZone sets time.Local, which TZ sets for ficha, to name for one test, so
// no test depends on the machine's zone.
func inZone(t *testing.T, name string) {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("no zone data for %s: %v", name, err)
	}
	saved := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = saved })
}

// wantDays fails unless days are want, as date, sessions and messages, and
// each day costs its messages' price.
func wantDays(t *testing.T, days []models.DayCost, want []models.DayCost) {
	t.Helper()
	if len(days) != len(want) {
		t.Fatalf("got %d days %+v, want %d", len(days), days, len(want))
	}
	for i, w := range want {
		d := days[i]
		if d.Date != w.Date || d.SessionCount != w.SessionCount || d.MessageCount != w.MessageCount {
			t.Errorf("day %d: %s, %d sessions, %d messages; want %s, %d, %d", i, d.Date, d.SessionCount, d.MessageCount, w.Date, w.SessionCount, w.MessageCount)
		}
		if !almostEqual(d.TotalCost.TotalCost, float64(d.MessageCount)*dayLineCost, 1e-12) {
			t.Errorf("day %s costs %v for %d messages", d.Date, d.TotalCost.TotalCost, d.MessageCount)
		}
	}
}

// sumsToTotals fails unless the days add up to the aggregate's totals.
func sumsToTotals(t *testing.T, days []models.DayCost, messages int, cost float64) {
	t.Helper()
	var n int
	var sum float64
	for _, d := range days {
		n += d.MessageCount
		sum += d.TotalCost.TotalCost
	}
	if n != messages || !almostEqual(sum, cost, 1e-9) {
		t.Errorf("days hold %d messages costing %v; the totals are %d and %v", n, sum, messages, cost)
	}
}

// A message counts on its own local day, so a session that runs past
// midnight in New York is on both days there, though both messages are on
// one day in UTC. A day that isn't 24 hours long, the one DST starts on,
// still holds everything from its midnight to the next.
func TestByDaySplitsAtLocalMidnight(t *testing.T) {
	inZone(t, "America/New_York")
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	writeJSONLFile(t, path, []string{
		dayLine("2026-03-07T04:30:00Z", "a"), // Mar 6, 23:30 EST
		dayLine("2026-03-07T05:30:00Z", "b"), // Mar 7, 00:30 EST
		dayLine("2026-03-08T05:30:00Z", "c"), // Mar 8, 00:30 EST
		dayLine("2026-03-09T03:30:00Z", "d"), // Mar 8, 23:30 EDT, 22 hours on
	})
	agg, _, err := AnalyzeMultipleSessionsByDay(diskEntries(t, path), models.TimeWindow{})
	if err != nil {
		t.Fatal(err)
	}
	wantDays(t, agg.Days, []models.DayCost{
		{Date: "2026-03-08", SessionCount: 1, MessageCount: 2},
		{Date: "2026-03-07", SessionCount: 1, MessageCount: 1},
		{Date: "2026-03-06", SessionCount: 1, MessageCount: 1},
	})
	sumsToTotals(t, agg.Days, agg.MessageCount, agg.TotalCost.TotalCost)

	// Without --by-day the aggregate is what it always was.
	plain, _, err := AnalyzeMultipleSessionsInWindow(diskEntries(t, path), models.TimeWindow{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Days != nil {
		t.Errorf("Days without --by-day: %+v", plain.Days)
	}
	sameTotals(t, "by day", plain, agg)
}

// An agent's messages count on their own days, and put its session on a
// day the main conversation has nothing on. A window keeps only the part
// of each day inside it.
func TestByDayCountsAgentsOnTheirOwnDays(t *testing.T) {
	inZone(t, "UTC")
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	writeJSONLFile(t, path, []string{dayLine("2026-02-01T23:00:00Z", "p1")})
	agentDir := filepath.Join(dir, "s", "subagents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONLFile(t, filepath.Join(agentDir, "agent-x.jsonl"), []string{
		dayLine("2026-02-01T23:30:00Z", "x1"),
		dayLine("2026-02-02T00:30:00Z", "x2"),
		dayLine("2026-02-02T09:00:00Z", "x3"),
	})
	agg, _, err := AnalyzeMultipleSessionsByDay(diskEntries(t, path), models.TimeWindow{})
	if err != nil {
		t.Fatal(err)
	}
	wantDays(t, agg.Days, []models.DayCost{
		{Date: "2026-02-02", SessionCount: 1, MessageCount: 2},
		{Date: "2026-02-01", SessionCount: 1, MessageCount: 2},
	})
	sumsToTotals(t, agg.Days, agg.MessageCount, agg.TotalCost.TotalCost)

	window := models.TimeWindow{Since: time.Date(2026, 2, 1, 23, 15, 0, 0, time.UTC), Until: time.Date(2026, 2, 2, 1, 0, 0, 0, time.UTC)}
	agg, _, err = AnalyzeMultipleSessionsByDay(diskEntries(t, path), window)
	if err != nil {
		t.Fatal(err)
	}
	wantDays(t, agg.Days, []models.DayCost{
		{Date: "2026-02-02", SessionCount: 1, MessageCount: 1},
		{Date: "2026-02-01", SessionCount: 1, MessageCount: 1},
	})
}

// A fork's copies count once, on the day they were first written, under
// the original, so the fork is on that day only if it added something then.
func TestByDayCountsAForksCopiesOnce(t *testing.T) {
	inZone(t, "UTC")
	dir := t.TempDir()
	orig, fork := filepath.Join(dir, "orig.jsonl"), filepath.Join(dir, "fork.jsonl")
	writeJSONLFile(t, orig, []string{dayLine("2026-02-01T10:00:00Z", "o1"), dayLine("2026-02-01T11:00:00Z", "o2")})
	writeJSONLFile(t, fork, []string{dayLine("2026-02-01T10:00:00Z", "o1"), dayLine("2026-02-01T11:00:00Z", "o2"), dayLine("2026-02-05T10:00:00Z", "f1")})
	setMtime(t, orig, time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC))
	setMtime(t, fork, time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC))
	agg, _, err := AnalyzeMultipleSessionsByDay(diskEntries(t, fork, orig), models.TimeWindow{})
	if err != nil {
		t.Fatal(err)
	}
	wantDays(t, agg.Days, []models.DayCost{
		{Date: "2026-02-05", SessionCount: 1, MessageCount: 1},
		{Date: "2026-02-01", SessionCount: 1, MessageCount: 2},
	})
	sumsToTotals(t, agg.Days, agg.MessageCount, agg.TotalCost.TotalCost)
}

// global adds each project's days, sessions and all, since no session is
// in two projects, and its days add up to its totals.
func TestByDayAcrossProjects(t *testing.T) {
	inZone(t, "UTC")
	var projects []models.ProjectInfo
	for i, lines := range [][]string{
		{dayLine("2026-02-01T10:00:00Z", "a1"), dayLine("2026-02-02T10:00:00Z", "a2")},
		{dayLine("2026-02-02T11:00:00Z", "b1")},
	} {
		dir := filepath.Join(t.TempDir(), fmt.Sprintf("p%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeJSONLFile(t, filepath.Join(dir, "s.jsonl"), lines)
		projects = append(projects, models.ProjectInfo{EncodedPath: filepath.Base(dir), FullPath: dir, DisplayName: filepath.Base(dir)})
	}
	global, err := AnalyzeAllProjectsByDay(projects, models.TimeWindow{})
	if err != nil {
		t.Fatal(err)
	}
	wantDays(t, global.Days, []models.DayCost{
		{Date: "2026-02-02", SessionCount: 2, MessageCount: 2},
		{Date: "2026-02-01", SessionCount: 1, MessageCount: 1},
	})
	sumsToTotals(t, global.Days, global.MessageCount, global.TotalCost.TotalCost)
	plain, err := AnalyzeAllProjectsInWindow(projects, models.TimeWindow{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Days != nil {
		t.Errorf("Days without --by-day: %+v", plain.Days)
	}
}

// A message without a timestamp is counted in the totals but on no day, so
// it gets a row of its own, after the dated ones.
func TestByDayKeepsUndatedMessages(t *testing.T) {
	inZone(t, "UTC")
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeJSONLFile(t, path, []string{
		`{"type":"assistant","requestId":"req_n","message":{"id":"msg_n","model":"claude-opus-4-8","usage":{"input_tokens":100,"output_tokens":300}}}`,
		dayLine("2026-02-01T10:00:00Z", "a"),
	})
	agg, _, err := AnalyzeMultipleSessionsByDay(diskEntries(t, path), models.TimeWindow{})
	if err != nil {
		t.Fatal(err)
	}
	wantDays(t, agg.Days, []models.DayCost{
		{Date: "2026-02-01", SessionCount: 1, MessageCount: 1},
		{Date: "", SessionCount: 1, MessageCount: 1},
	})
}
