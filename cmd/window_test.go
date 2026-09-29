package cmd

import (
	"strings"
	"testing"
	"time"

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
	for _, bad := range []string{"", "last week", "-7d", "7x", "2026-13-01"} {
		if _, err := parseWindowBound(bad, now, false); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestTimeWindowRejectsBackwardsRange(t *testing.T) {
	cfg := &config{since: "2026-09-10", until: "2026-09-01"}
	if _, err := cfg.timeWindow(time.Now()); err == nil {
		t.Error("--since after --until should be an error")
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
	out, _, err = executeCLISplit(t, "summary", projFlag, "--since", "2026-03-01", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	mar1Label := render.Date(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Now())
	if strings.TrimSpace(out) != "No messages in "+mar1Label+" → now." {
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
