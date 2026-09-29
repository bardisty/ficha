package formatter

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/bardisty/ficha/internal/models"
)

// columnDots returns the offset of every "." in a plain-text line: with
// costs as the only decimals in these rows, they mark the decimal points.
func columnDots(line string) []int {
	var dots []int
	for i, r := range line {
		if r == '.' {
			dots = append(dots, i)
		}
	}
	return dots
}

// assertRowsAligned checks that every row is as wide as the header and puts
// its decimal points at the same offsets as the first row.
func assertRowsAligned(t *testing.T, out, header string, rowKeys []string) {
	t.Helper()
	var headerWidth int
	rows := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, header) {
			headerWidth = lipgloss.Width(line)
		}
		for _, k := range rowKeys {
			if strings.Contains(line, k) {
				rows[k] = line
			}
		}
	}
	if headerWidth == 0 || len(rows) != len(rowKeys) {
		t.Fatalf("missing header or rows:\n%s", out)
	}
	want := columnDots(rows[rowKeys[0]])
	for _, k := range rowKeys {
		row := rows[k]
		if w := lipgloss.Width(row); w != headerWidth {
			t.Errorf("row %s is %d columns, header is %d:\n%q", k, w, headerWidth, row)
		}
		got := columnDots(row)
		if len(got) != len(want) {
			t.Errorf("row %s has %d decimals, want %d: %q", k, len(got), len(want), row)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("row %s decimal %d at %d, want %d: %q", k, i, got[i], want[i], row)
			}
		}
	}
}

// Costs of $1000 and up widen the column instead of overflowing it, and
// two- and four-decimal values share a decimal column.
func TestGlobalTableLargeCostsStayAligned(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	names := []string{"alpha", "beta", "gamma"}
	analysis := globalAnalysisWithProjects(
		map[string]float64{"alpha": 0.25, "beta": 1234.56, "gamma": 98765.43},
		names,
	)
	for _, details := range []bool{false, true} {
		out := projectsTable(analysis, true, GlobalTableOptions{TopN: 10, Details: details})
		assertRowsAligned(t, out, "PROJECT", names)
	}
}

func TestSummaryTableLargeCostsStayAligned(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	aggregate := &models.SessionAnalysis{
		SessionID: "aggregate", IsSummary: true, SessionCount: 3,
		CostByModel: map[string]models.CostBreakdown{},
	}
	results := []models.SessionResult{
		sessionRow("aaaa1111", "claude-opus-4-8", 10, 0.25),
		sessionRow("bbbb2222", "claude-opus-4-8", 11, 1234.56),
		sessionRow("cccc3333", "claude-opus-4-8", 12, 12345.67),
	}
	for _, expand := range []bool{false, true} {
		out := FormatSummaryTableWithDetails(aggregate, results, "/home/user/src/app", true, expand)
		assertRowsAligned(t, out, "CUMULATIVE", []string{"aaaa1111", "bbbb2222", "cccc3333"})
		if !strings.Contains(out, "$13580.48") {
			t.Errorf("expand=%v: sum missing:\n%s", expand, out)
		}
	}
}

// A billion-plus token count reads as billions, not "1638.00M".
func TestShowTableBillionTokens(t *testing.T) {
	analysis := goldenShowAnalysis()
	analysis.TotalUsage.CacheReadInputTokens = 1_638_000_000
	out := FormatSessionTable(analysis, true)
	if !strings.Contains(out, "1.64B tokens") || strings.Contains(out, "1638.00M") {
		t.Errorf("want a B suffix for 1.638B tokens:\n%s", out)
	}
}
