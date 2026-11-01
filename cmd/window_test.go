package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/bardisty/ficha/internal/render"
)

func TestParseWindowBound(t *testing.T) {
	orig := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = orig })

	now := time.Date(2026, 9, 28, 15, 30, 0, 0, time.UTC)
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	tests := []struct {
		in   string
		end  bool
		want time.Time
	}{
		{"2026-09-01", false, day(1)},
		{"2026-09-01", true, day(2)}, // --until includes the day
		{"today", false, day(28)},
		{"Today", true, day(29)},
		{"yesterday", false, day(27)},
		{"7d", false, now.Add(-7 * 24 * time.Hour)},
		{"2w", false, now.Add(-14 * 24 * time.Hour)},
		{"12h", false, now.Add(-12 * time.Hour)},
		{"30m", false, now.Add(-30 * time.Minute)},
		{"2026-09-01T14:00", false, time.Date(2026, 9, 1, 14, 0, 0, 0, time.UTC)},
		{"2026-09-01T14:00:00+02:00", false, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)},
		{"2026-09-01T14:00Z", false, time.Date(2026, 9, 1, 14, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		got, err := parseWindowBound(tt.in, now, tt.end)
		if err != nil {
			t.Errorf("%q: %v", tt.in, err)
			continue
		}
		if !got.Equal(tt.want) {
			t.Errorf("%q (end=%v) = %v, want %v", tt.in, tt.end, got, tt.want)
		}
	}
	for _, bad := range []string{"", "last week", "-7d", "7x", "2026-13-01", "200000d", "99999999w", "9999999h"} {
		if _, err := parseWindowBound(bad, now, false); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// Santiago's clocks jump from 00:00 to 01:00 on 2026-09-06, so that date's
// first instant is 01:00, and --until 2026-09-05 ends there too.
func TestDateStartsAfterSkippedMidnight(t *testing.T) {
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	orig := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = orig })

	now := time.Date(2026, 10, 31, 15, 30, 0, 0, loc)
	want := time.Date(2026, 9, 6, 1, 0, 0, 0, loc)
	for _, tt := range []struct {
		in  string
		end bool
	}{{"2026-09-06", false}, {"2026-09-05", true}} {
		got, err := parseWindowBound(tt.in, now, tt.end)
		if err != nil {
			t.Fatalf("%q: %v", tt.in, err)
		}
		if !got.Equal(want) {
			t.Errorf("%q (end=%v) = %v, want %v", tt.in, tt.end, got, want)
		}
	}
}

func TestTimeWindowRejectsBackwardsRange(t *testing.T) {
	cfg := &config{since: "2026-09-10", until: "2026-09-01"}
	if _, err := cfg.timeWindow(time.Now()); err == nil {
		t.Error("--since after --until should be an error")
	}
}

// headerProject is the project a report's header names, so a test can
// expect the same name elsewhere without knowing how this platform spells
// the fixture's project.
func headerProject(t *testing.T, out string) string {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "║  "); ok {
			if project, _, ok := strings.Cut(rest, "  │"); ok {
				return project
			}
		}
	}
	t.Fatalf("no project in the header:\n%s", out)
	return ""
}

// A --since still to come is a usage error, against the clock passed in. An
// --until to come is fine on its own or after a past --since.
func TestTimeWindowRejectsFutureSince(t *testing.T) {
	orig := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = orig })
	now := time.Date(2026, 10, 31, 15, 30, 0, 0, time.UTC)
	for _, tt := range []struct {
		since, until, wantErr string
	}{
		{"2026-12-01", "", "--since 2026-12-01 is after now"},
		{"2026-10-31T16:00", "", "--since 2026-10-31T16:00 is after now"},
		{"2026-12-01", "2026-12-02", "--since 2026-12-01 is after now"},
		{"today", "", ""},
		{"2026-10-31T15:30", "", ""},
		{"", "2026-12-31", ""},
		{"2026-10-01", "2026-12-31", ""},
	} {
		cfg := &config{since: tt.since, until: tt.until}
		_, err := cfg.timeWindow(now)
		switch {
		case tt.wantErr == "" && err != nil:
			t.Errorf("--since %q --until %q: %v", tt.since, tt.until, err)
		case tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr):
			t.Errorf("--since %q --until %q: error %v, want %q", tt.since, tt.until, err, tt.wantErr)
		case tt.wantErr != "" && exitCode(err) != 2:
			t.Errorf("--since %q: exit %d, want 2", tt.since, exitCode(err))
		}
	}
}

// A future --since fails the same way in every format, before any output.
func TestE2EFutureSinceIsAUsageError(t *testing.T) {
	setupE2EFixture(t)
	for _, args := range [][]string{
		{"summary", projFlag, "--since", "2999-01-01"},
		{"summary", projFlag, "--since", "2999-01-01", "-f", "json"},
		{"list", projFlag, "--since", "2999-01-01", "-f", "csv"},
		{"global", "--since", "2999-01-01", "-f", "json"},
	} {
		stdout, _, err := executeCLISplit(t, args...)
		if err == nil || err.Error() != "--since 2999-01-01 is after now" || exitCode(err) != 2 {
			t.Errorf("%v: err %v (exit %d), want a usage error saying --since is after now", args, err, exitCode(err))
		}
		if stdout != "" {
			t.Errorf("%v: printed %q before failing", args, stdout)
		}
	}
	if _, _, err := executeCLISplit(t, "summary", projFlag, "--until", "2999-01-01", "-f", "json"); err != nil {
		t.Errorf("a future --until alone should work: %v", err)
	}
}

// A window counts messages by their own timestamps, agents' too, and the same
// scope reaches table, json and csv. The e2e fixture: alpha on Feb 1, beta on
// Feb 2 (parent 09:00 and 09:10, agents 09:05 and 09:07), gamma on Feb 3 in
// another project.
func TestE2EWindowFiltersMessages(t *testing.T) {
	setupE2EFixture(t)
	orig := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = orig })

	summaryJSON := func(args ...string) summaryWindowJSON {
		t.Helper()
		out, _, err := executeCLISplit(t, append([]string{"summary", projFlag, "-f", "json"}, args...)...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		var s summaryWindowJSON
		mustJSON(t, out, &s)
		return s
	}

	all := summaryJSON()
	if all.SessionCount != 2 || all.MessageCount != 6 || all.Window != nil {
		t.Fatalf("unwindowed: %+v", all)
	}

	// Only beta is on Feb 2, with its agents.
	feb2 := summaryJSON("--since", "2026-02-02")
	if feb2.SessionCount != 1 || feb2.MessageCount != 4 || feb2.Window == nil || feb2.Window.Since == "" || feb2.Window.Until != "" {
		t.Errorf("--since 2026-02-02: %+v", feb2)
	}

	// From 09:06 beta splits: its 09:10 reply and the 09:07 agent count,
	// the 09:00 reply and the 09:05 agent don't.
	split := summaryJSON("--since", "2026-02-02T09:06")
	if split.SessionCount != 1 || split.MessageCount != 2 || split.AgentMessageCount != 1 {
		t.Errorf("--since 2026-02-02T09:06: %+v", split)
	}
	if split.TotalCost.TotalCost >= feb2.TotalCost.TotalCost {
		t.Errorf("a split session should cost less than the whole: %v vs %v", split.TotalCost.TotalCost, feb2.TotalCost.TotalCost)
	}

	// --until a date includes that day.
	feb1 := summaryJSON("--until", "2026-02-01")
	if feb1.SessionCount != 1 || feb1.MessageCount != 2 {
		t.Errorf("--until 2026-02-01: %+v", feb1)
	}

	// The table names the window, and says so when nothing is in it.
	out, _, err := executeCLISplit(t, "summary", projFlag, "--since", "2026-02-02", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	// cmd reads the real clock, and dates carry a year only outside it.
	feb2Label := render.Date(time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC), time.Now())
	mustContainAll(t, out, "Window: "+feb2Label+" → now", "1 session ")
	project := headerProject(t, out)
	out, _, err = executeCLISplit(t, "summary", projFlag, "--since", "2026-03-01", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	mar1Label := render.Date(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Now())
	if strings.TrimSpace(out) != "No messages in "+project+": "+mar1Label+" → now." {
		t.Errorf("empty window: %q", out)
	}

	// global: only the other project has messages on Feb 3.
	out, _, err = executeCLISplit(t, "global", "--since", "2026-02-03", "-f", "csv")
	if err != nil {
		t.Fatal(err)
	}
	if records := mustCSV(t, out); len(records) != 2 {
		t.Errorf("global --since 2026-02-03 csv: want 1 project, got %d rows:\n%s", len(records)-1, out)
	}

	// A bad value names the flag and what it takes.
	if _, _, err := executeCLISplit(t, "global", "--since", "last week"); err == nil || !strings.Contains(err.Error(), "--since") {
		t.Errorf("bad --since: %v", err)
	}
}

type summaryWindowJSON struct {
	SessionCount      int `json:"session_count"`
	MessageCount      int `json:"message_count"`
	AgentMessageCount int `json:"agent_message_count"`
	TotalCost         struct {
		TotalCost float64 `json:"total_cost"`
	} `json:"total_cost"`
	Window *struct {
		Since string `json:"since"`
		Until string `json:"until"`
	} `json:"window"`
}

// A session with nothing in the window leaves the report, even when it has an
// unreadable line; the warning about that line stays. An empty window gives
// global's json an empty projects list, not null.
func TestE2EWindowDropsOldSessionsWithSkips(t *testing.T) {
	root := setupE2EFixture(t)
	orig := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = orig })

	// alpha (Feb 1) gains an unreadable line.
	path := filepath.Join(root, "projects", e2eProjDir, e2eAlphaID+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"broken\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	out, stderr, err := executeCLISplit(t, "summary", projFlag, "--since", "2026-02-02", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	var s summaryWindowJSON
	mustJSON(t, out, &s)
	if s.SessionCount != 1 {
		t.Errorf("alpha has nothing after Feb 2 and should drop out: session_count %d", s.SessionCount)
	}
	if !strings.Contains(stderr, "unparseable line") {
		t.Errorf("the unreadable line should still be reported:\n%s", stderr)
	}

	// global: the project with only alpha and beta, from Feb 3, is gone, and
	// its old dates don't stretch the span.
	out, _, err = executeCLISplit(t, "global", "--since", "2026-02-03", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		SessionCount int       `json:"session_count"`
		FirstActive  time.Time `json:"first_active"`
		Projects     []any     `json:"projects"`
	}
	mustJSON(t, out, &g)
	if g.SessionCount != 1 || len(g.Projects) != 1 || g.FirstActive.Before(time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("global --since 2026-02-03: %+v", g)
	}

	out, _, err = executeCLISplit(t, "global", "--since", "2026-06-01", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"projects": []`) {
		t.Errorf("an empty window should give an empty projects list:\n%s", out)
	}
}
