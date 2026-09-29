package formatter

import (
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// withLocal swaps the process timezone for one test. TestMain pins UTC, and
// nothing in this package runs in parallel, so the swap can't leak into
// another test.
func withLocal(t *testing.T, loc *time.Location) {
	t.Helper()
	orig := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = orig })
}

// Per-message times in human output are local, like the footer's "Last
// active" clock next to them. The fixture's peak is at 10:42:13 UTC, so in
// UTC+9 it must read 19:42:13, in both render paths.
func TestShowInsightTimesAreLocal(t *testing.T) {
	withLocal(t, time.FixedZone("UTC+9", 9*3600))

	for _, noColor := range []bool{true, false} {
		out := FormatSessionTable(goldenShowAnalysis(), noColor)
		for _, want := range []string{"19:00:05", "20:29:55", "19:42:13"} {
			if !strings.Contains(out, want) {
				t.Errorf("noColor=%v: missing local time %s", noColor, want)
			}
		}
		for _, utc := range []string{"10:00:05", "11:29:55", "10:42:13"} {
			if strings.Contains(out, utc) {
				t.Errorf("noColor=%v: still shows UTC time %s", noColor, utc)
			}
		}
	}
}

// json and csv must not follow the display conversion: the same analysis has
// to produce byte-identical machine output in every timezone.
func TestMachineOutputIgnoresLocalZone(t *testing.T) {
	analysis := goldenShowAnalysis()
	analysis.Messages = []models.MessageAnalysis{
		{Timestamp: goldenTime(10, 0, 5), Model: "claude-opus-4-8", Cost: models.CostBreakdown{TotalCost: 0.35}},
		{Timestamp: goldenTime(23, 59, 59), Model: "claude-opus-4-8", Cost: models.CostBreakdown{TotalCost: 0.52}},
	}

	render := func() (string, string) {
		t.Helper()
		j, err := FormatSessionJSON(analysis, true)
		if err != nil {
			t.Fatalf("FormatSessionJSON: %v", err)
		}
		c, err := FormatSessionCSV(analysis, true)
		if err != nil {
			t.Fatalf("FormatSessionCSV: %v", err)
		}
		return j, c
	}

	utcJSON, utcCSV := render()
	withLocal(t, time.FixedZone("UTC-7", -7*3600))
	localJSON, localCSV := render()

	if localJSON != utcJSON {
		t.Error("json output changed with the local timezone")
	}
	if localCSV != utcCSV {
		t.Error("csv output changed with the local timezone")
	}
	if !strings.Contains(localCSV, "2026-01-15T23:59:59Z") {
		t.Errorf("csv should keep the UTC timestamp, got:\n%s", localCSV)
	}
	if !strings.Contains(localJSON, `"2026-01-15T23:59:59Z"`) {
		t.Errorf("json should keep the UTC timestamp, got:\n%s", localJSON)
	}
}
