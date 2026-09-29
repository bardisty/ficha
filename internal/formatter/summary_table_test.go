package formatter

import (
	"encoding/csv"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// sessionRow builds a one-session result whose parent primary model is modelID.
func sessionRow(id, modelID string, day int, cost float64) models.SessionResult {
	breakdown := models.CostBreakdown{TotalCost: cost}
	return models.SessionResult{
		Entry: models.SessionEntry{
			SessionID: id,
			Modified:  time.Date(2026, 1, day, 12, 0, 0, 0, time.UTC),
		},
		Analysis: &models.SessionAnalysis{
			SessionID:         id,
			MessageCount:      1,
			TotalCost:         breakdown,
			CostByModel:       map[string]models.CostBreakdown{modelID: breakdown},
			ParentCostByModel: map[string]models.CostBreakdown{modelID: breakdown},
		},
	}
}

// summaryRowRe matches a SESSION BREAKDOWN data row: two spaces, a right-aligned
// row number, then the 8-char session ID. Anchored so the sum line, the
// sparkline and the section rules never sneak in.
var summaryRowRe = regexp.MustCompile(`^ {2}\s*\d+ [0-9a-f]{8} `)

// summaryTableRows returns the SESSION BREAKDOWN header followed by each
// per-session data row, rendered without color.
func summaryTableRows(t *testing.T, results []models.SessionResult, expandAgents bool) []string {
	t.Helper()
	aggregate := &models.SessionAnalysis{
		SessionID:    "aggregate",
		IsSummary:    true,
		SessionCount: len(results),
		CostByModel:  map[string]models.CostBreakdown{},
	}
	out := FormatSummaryTableWithDetails(aggregate, results, "/home/user/src/app", true, expandAgents)

	var rows []string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "SESSION ") && strings.Contains(line, "MODEL"):
			rows = append(rows, line)
		case len(rows) > 0 && summaryRowRe.MatchString(line):
			rows = append(rows, line)
		}
	}
	if len(rows) != len(results)+1 {
		t.Fatalf("expected a header + %d session rows, got %d:\n%s", len(results), len(rows), out)
	}
	return rows
}

// The MODEL column is fixed-width, but Go's "%-Ns" pads without truncating. A
// display name wider than the column (every "Sonnet 4.x"/"Sonnet 3.x" is 10
// chars) or an unknown model's raw ID fallback (24+ chars) used to push AGENTS,
// COST and CUMULATIVE right on that row alone, misaligning it against the
// header and its neighbours.
func TestSummaryTableModelColumnNeverShiftsRow(t *testing.T) {
	for _, expandAgents := range []bool{false, true} {
		name := "default"
		if expandAgents {
			name = "expand-agents"
		}
		t.Run(name, func(t *testing.T) {
			rows := summaryTableRows(t, []models.SessionResult{
				sessionRow("aaaa1111", "claude-opus-4-8", 10, 1.00),                  // 8 chars
				sessionRow("bbbb2222", "claude-sonnet-4-6", 11, 2.00),                // 10 chars: the overflow
				sessionRow("cccc3333", "claude-3-5-sonnet", 12, 3.00),                // 10 chars
				sessionRow("dddd4444", "claude-opus-4-9-20260101", 13, 4.00),         // unknown: 24-char raw ID
				sessionRow("eeee5555", "us.anthropic.claude-opus-4-9-v1:0", 14, 5.0), // unknown: 33-char raw ID
			}, expandAgents)

			want := lipgloss.Width(rows[0])
			for i, row := range rows {
				if got := paddedWidth(row); got != want {
					t.Errorf("row %d is %d columns, header is %d:\n%q", i, got, want, row)
				}
			}

			// Every row's cost must sit in the same column as the header's.
			costCol := strings.Index(rows[0], "COST")
			for i, row := range rows[1:] {
				if !strings.Contains(row[:costCol], "  ") {
					t.Errorf("row %d overruns the COST column at offset %d:\n%q", i+1, costCol, row)
				}
			}
		})
	}
}

// sessionRowWithAgents extends sessionRow with agent aggregates so the AGENTS
// column renders "N [$X.XX]" content instead of "-".
func sessionRowWithAgents(id, modelID string, day int, cost float64, agentCount int, agentsCost float64) models.SessionResult {
	r := sessionRow(id, modelID, day, cost)
	r.Analysis.HasAgents = true
	r.Analysis.AgentCount = agentCount
	r.Analysis.AgentsCost = models.CostBreakdown{TotalCost: agentsCost}
	return r
}

// The AGENTS column is fixed-width like MODEL, but "N [$X.XX]" grows with the
// agent count and subtotal: "1 [$100.00]" and "10 [$10.00]" are 11 chars and
// used to push COST and CUMULATIVE right on that row alone.
func TestSummaryTableAgentsColumnNeverShiftsRow(t *testing.T) {
	rows := summaryTableRows(t, []models.SessionResult{
		sessionRow("aaaa1111", "claude-opus-4-8", 10, 1.00),                          // no agents: "-"
		sessionRowWithAgents("bbbb2222", "claude-opus-4-8", 11, 2.00, 1, 0.05),       // fits at 2 decimals
		sessionRowWithAgents("cccc3333", "claude-opus-4-8", 12, 3.00, 1, 100.00),     // 11 chars at 2 decimals
		sessionRowWithAgents("dddd4444", "claude-opus-4-8", 13, 4.00, 10, 10.00),     // 11 chars at 2 decimals
		sessionRowWithAgents("eeee5555", "claude-opus-4-8", 14, 5.00, 100, 12345.67), // overflows every format: clamped
	}, false)

	want := lipgloss.Width(rows[0])
	for i, row := range rows {
		if got := paddedWidth(row); got != want {
			t.Errorf("row %d is %d columns, header is %d:\n%q", i, got, want, row)
		}
	}
}

// Same defect class as TestSummaryTableModelColumnAlignsWithColor, for the
// AGENTS column's overflow values.
func TestSummaryTableAgentsColumnAlignsWithColor(t *testing.T) {
	forceProfile(t, termenv.ANSI256)
	aggregate := &models.SessionAnalysis{
		SessionID: "aggregate", IsSummary: true, SessionCount: 3,
		CostByModel: map[string]models.CostBreakdown{},
	}
	results := []models.SessionResult{
		sessionRow("aaaa1111", "claude-opus-4-8", 10, 1.00),
		sessionRowWithAgents("bbbb2222", "claude-opus-4-8", 11, 2.00, 1, 100.00),
		sessionRowWithAgents("cccc3333", "claude-opus-4-8", 12, 3.00, 100, 12345.67),
	}
	out := FormatSummaryTableWithDetails(aggregate, results, "/home/user/src/app", false, false)

	var widths []int
	for _, line := range strings.Split(out, "\n") {
		for _, id := range []string{"aaaa1111", "bbbb2222", "cccc3333"} {
			if strings.Contains(line, id) {
				widths = append(widths, lipgloss.Width(line))
			}
		}
	}
	if len(widths) != 3 {
		t.Fatalf("expected 3 session rows, got %d:\n%s", len(widths), out)
	}
	for i, w := range widths[1:] {
		if w != widths[0] {
			t.Errorf("colored row %d is %d columns, row 0 is %d", i+1, w, widths[0])
		}
	}
}

// The Sum figure must right-align exactly under the CUMULATIVE column — the
// two lines end at the same display column — in both views and color paths.
func TestSummaryTableSumAlignsUnderCost(t *testing.T) {
	for _, tc := range []struct {
		name         string
		noColor      bool
		expandAgents bool
	}{
		{"default", true, false},
		{"expand-agents", true, true},
		{"default-color", false, false},
		{"expand-agents-color", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.noColor {
				forceProfile(t, termenv.Ascii)
			} else {
				forceProfile(t, termenv.ANSI256)
			}
			aggregate := &models.SessionAnalysis{
				SessionID: "aggregate", IsSummary: true, SessionCount: 2,
				CostByModel: map[string]models.CostBreakdown{},
			}
			results := []models.SessionResult{
				sessionRow("aaaa1111", "claude-opus-4-8", 10, 1.00),
				sessionRow("bbbb2222", "claude-opus-4-8", 11, 2.00),
			}
			out := FormatSummaryTableWithDetails(aggregate, results, "/home/user/src/app", tc.noColor, tc.expandAgents)

			var headerWidth, sumWidth int
			var sumLine string
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "MODIFIED") {
					headerWidth = lipgloss.Width(line)
				}
				if strings.Contains(line, "Sum (") {
					sumLine = line
					sumWidth = paddedWidth(line)
				}
			}
			if headerWidth == 0 || sumLine == "" {
				t.Fatalf("missing header or sum line:\n%s", out)
			}
			if sumWidth != headerWidth {
				t.Errorf("sum line is %d columns, header is %d — sum does not end under COST:\n%q", sumWidth, headerWidth, sumLine)
			}
		})
	}
}

// One valid session must render "Sum (1 session):", matching the header
// panel's singular wording.
func TestSummaryTableSumLabelSingularizes(t *testing.T) {
	aggregate := &models.SessionAnalysis{
		SessionID: "aggregate", IsSummary: true, SessionCount: 1,
		CostByModel: map[string]models.CostBreakdown{},
	}
	out := FormatSummaryTableWithDetails(aggregate, []models.SessionResult{
		sessionRow("aaaa1111", "claude-opus-4-8", 10, 1.00),
	}, "/home/user/src/app", true, false)

	if !strings.Contains(out, "Sum (1 session):") {
		t.Errorf("one session must singularize the sum label, got:\n%s", out)
	}
}

// The MODEL column must stay within the 72-char separator that bounds every
// section, and the widened column must still fit the longest display name whole.
func TestSummaryTableModelColumnFitsLongestDisplayName(t *testing.T) {
	rows := summaryTableRows(t, []models.SessionResult{
		sessionRow("aaaa1111", "claude-sonnet-4-6", 10, 1.00),
	}, false)

	if w := lipgloss.Width(rows[0]); w > 74 {
		t.Errorf("row is %d columns, exceeds the 74-column separator (2 indent + 72)", w)
	}
	if !strings.Contains(rows[1], "Sonnet 4.6") {
		t.Errorf("the longest display name must render whole, got:\n%q", rows[1])
	}
}

// Unknown models render their raw ID; it must be cut to the column, not padded
// past it.
func TestSummaryTableClampsUnknownModelID(t *testing.T) {
	rows := summaryTableRows(t, []models.SessionResult{
		sessionRow("aaaa1111", "claude-opus-4-9-20260101", 10, 1.00),
	}, false)

	if strings.Contains(rows[1], "claude-opus-4-9-20260101") {
		t.Errorf("raw model ID must be clamped to the MODEL column, got:\n%q", rows[1])
	}
	if !strings.Contains(rows[1], "…") {
		t.Errorf("a clamped model ID must show it was cut, got:\n%q", rows[1])
	}
}

// The color path formats the MODEL column independently of the no-color path;
// both must clamp. lipgloss.Width discounts the ANSI escapes.
func TestSummaryTableModelColumnAlignsWithColor(t *testing.T) {
	forceProfile(t, termenv.ANSI256)
	aggregate := &models.SessionAnalysis{
		SessionID: "aggregate", IsSummary: true, SessionCount: 2,
		CostByModel: map[string]models.CostBreakdown{},
	}
	results := []models.SessionResult{
		sessionRow("aaaa1111", "claude-opus-4-8", 10, 1.00),
		sessionRow("bbbb2222", "claude-sonnet-4-6", 11, 2.00),
		sessionRow("cccc3333", "claude-opus-4-9-20260101", 12, 3.00),
	}
	out := FormatSummaryTableWithDetails(aggregate, results, "/home/user/src/app", false, false)

	var widths []int
	for _, line := range strings.Split(out, "\n") {
		for _, id := range []string{"aaaa1111", "bbbb2222", "cccc3333"} {
			if strings.Contains(line, id) {
				widths = append(widths, lipgloss.Width(line))
			}
		}
	}
	if len(widths) != 3 {
		t.Fatalf("expected 3 session rows, got %d:\n%s", len(widths), out)
	}
	for i, w := range widths[1:] {
		if w != widths[0] {
			t.Errorf("colored row %d is %d columns, row 0 is %d", i+1, w, widths[0])
		}
	}
}

// Sessions whose file mtimes tie must appear in the same order — with the same
// cumulative value on each row — in the SESSION BREAKDOWN table and the detail
// CSV. Both surfaces sort by Modified with a stable sort, so ties keep input
// order; an unstable sort on either side would let the surfaces disagree on
// intermediate cumulative values (the final sum always matches). 40 rows so an
// unstable sort has room to actually permute equal keys.
func TestSummaryTableTieMtimeOrderMirrorsDetailCSV(t *testing.T) {
	tied := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	const n = 40
	results := make([]models.SessionResult, 0, n)
	wantIDs := make([]string, 0, n)
	for i := range n {
		// Deliberately not ID-sorted input order.
		id := fmt.Sprintf("%04x%04x", (i*7)%n, i)
		r := sessionRow(id, "claude-opus-4-8", 5, 0.01*float64(i+1))
		r.Entry.Modified = tied
		results = append(results, r)
		wantIDs = append(wantIDs, id)
	}

	// Table: the session ID of each data row, newest first.
	rows := summaryTableRows(t, results, false)[1:]
	idRe := regexp.MustCompile(`^ {2}\s*\d+ ([0-9a-f]{8}) `)
	tableIDs := make([]string, 0, n)
	for _, row := range rows {
		m := idRe.FindStringSubmatch(row)
		if m == nil {
			t.Fatalf("row does not match data-row shape: %q", row)
		}
		tableIDs = append(tableIDs, m[1])
	}

	// Detail CSV: session_id (col 1), oldest first.
	out, err := FormatSummaryDetailCSV(results, false)
	if err != nil {
		t.Fatalf("FormatSummaryDetailCSV returned error: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}
	if len(records) != n+1 || len(tableIDs) != n {
		t.Fatalf("csv rows: got %d, table rows %d, want %d", len(records)-1, len(tableIDs), n)
	}
	for i, rec := range records[1:] {
		if rec[1] != wantIDs[i] {
			t.Fatalf("csv row %d session_id: got %q, want %q (tie order must be stable)", i, rec[1], wantIDs[i])
		}
		// The table is newest first, so it reads the csv backwards.
		if got := tableIDs[n-1-i]; got != wantIDs[i] {
			t.Fatalf("table row %d session_id: got %q, want %q (tie order must mirror csv)", n-1-i, got, wantIDs[i])
		}
	}
}
