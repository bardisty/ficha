package cmd

import (
	"slices"
	"strings"
	"testing"
)

// machineOutputs is every command shape that has json and csv output.
var machineOutputs = [][]string{
	{"show", projFlag, e2eBetaID},
	{"show", projFlag, e2eBetaID, "--messages"},
	{"list", projFlag},
	{"summary", projFlag},
	{"summary", projFlag, "-d"},
	{"summary", projFlag, "-d", "--expand-agents"},
	{"global"},
}

// TestE2EMachineOutputEndsInOneNewline: a second newline after the last csv
// record reads as an empty record to csv.reader, and as a blank last line to
// tail and wc.
func TestE2EMachineOutputEndsInOneNewline(t *testing.T) {
	setupE2EFixture(t)
	for _, args := range machineOutputs {
		for _, format := range []string{"json", "csv"} {
			out, _, err := executeCLISplit(t, append(args, "-f", format)...)
			if err != nil {
				t.Fatalf("%v -f %s: %v", args, format, err)
			}
			if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
				t.Errorf("%v -f %s: want exactly one trailing newline, got ending %q", args, format, out[max(0, len(out)-20):])
			}
		}
	}
}

// TestE2ECostTrendIsAName: cost_trend is written by name, and left out, with
// the other trend fields, on a session too short to have a trend.
func TestE2ECostTrendIsAName(t *testing.T) {
	setupKeysFixture(t)
	insights := func(id string) map[string]any {
		t.Helper()
		out, _, err := executeCLISplit(t, "show", keysProj, id, "-f", "json")
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Insights map[string]any `json:"insights"`
		}
		mustJSON(t, out, &v)
		return v.Insights
	}

	long := insights(keysFullID)
	if trend := long["cost_trend"]; !slices.Contains([]any{"increasing", "decreasing", "stable"}, trend) {
		t.Errorf("cost_trend = %#v, want increasing, decreasing or stable", trend)
	}
	short := insights(keysNoAgentsDirID)
	for _, k := range []string{"cost_trend", "recent_avg_cost", "trend_window"} {
		if v, ok := short[k]; ok {
			t.Errorf("one-message session: %s = %v, want it absent", k, v)
		}
	}
}
