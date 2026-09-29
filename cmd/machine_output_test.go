package cmd

import (
	"math"
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

// TestE2EExpandAgentsCSVReconciles: summary -d --expand-agents csv has
// session rows, whose total_cost includes their agents, and agent rows.
// Summing session rows gives the total, summing agent rows gives agent
// spend, and each session row splits into parent_cost + agents_cost.
func TestE2EExpandAgentsCSVReconciles(t *testing.T) {
	setupE2EFixture(t)
	out, _, err := executeCLISplit(t, "summary", projFlag, "-d", "--expand-agents", "-f", "csv")
	if err != nil {
		t.Fatal(err)
	}
	records := mustCSV(t, out)
	col := func(name string) int {
		i := slices.Index(records[0], name)
		if i < 0 {
			t.Fatalf("no %s column in %v", name, records[0])
		}
		return i
	}
	rowType, sessionID := col("row_type"), col("session_id")
	total, parent, agents := col("total_cost"), col("parent_cost"), col("agents_cost")

	// Each cell is rounded to 6 dp, so a sum of two can be off by one in the
	// last place.
	const tol = 2e-6
	var sessionSum, agentRowSum, agentsCostSum float64
	var agentRows int
	for _, r := range records[1:] {
		switch r[rowType] {
		case "session":
			p, a, tc := mustFloat(t, r[parent]), mustFloat(t, r[agents]), mustFloat(t, r[total])
			if math.Abs(p+a-tc) > tol {
				t.Errorf("session %s: parent_cost %v + agents_cost %v != total_cost %v", r[sessionID], p, a, tc)
			}
			sessionSum += tc
			agentsCostSum += a
		case "agent":
			agentRows++
			if r[parent] != "" || r[agents] != "" {
				t.Errorf("agent row %v: parent_cost and agents_cost should be empty", r)
			}
			agentRowSum += mustFloat(t, r[total])
		default:
			t.Errorf("unexpected row_type %q", r[rowType])
		}
	}
	if agentRows == 0 {
		t.Fatal("the fixture should produce agent rows")
	}
	if math.Abs(agentRowSum-agentsCostSum) > float64(agentRows)*tol {
		t.Errorf("agent rows sum to %v, session agents_cost to %v", agentRowSum, agentsCostSum)
	}

	jsonOut, _, err := executeCLISplit(t, "summary", projFlag, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		TotalCost struct {
			TotalCost float64 `json:"total_cost"`
		} `json:"total_cost"`
	}
	mustJSON(t, jsonOut, &summary)
	if math.Abs(sessionSum-summary.TotalCost.TotalCost) > float64(len(records))*tol {
		t.Errorf("session rows sum to %v, summary -f json total_cost.total_cost is %v", sessionSum, summary.TotalCost.TotalCost)
	}
}
