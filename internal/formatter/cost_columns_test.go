package formatter

import (
	"regexp"
	"strings"
	"testing"
	"time"

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

// endsInTwoDecimalCost matches a line whose last cell is a two-decimal cost,
// which trimLineEnds leaves without the two-space pad that aligns it.
var endsInTwoDecimalCost = regexp.MustCompile(`\$[0-9,]+\.[0-9]{2}(\x1b\[[0-9;]*m)*$`)

// paddedWidth is a row's width with its end-of-line cost pad counted, so rows
// compare with a header whose last column is the cost cell's full width.
func paddedWidth(line string) int {
	w := lipgloss.Width(line)
	if endsInTwoDecimalCost.MatchString(line) {
		w += 2
	}
	return w
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
		if w := paddedWidth(row); w != headerWidth {
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
		assertRowsAligned(t, out, "MODIFIED", []string{"aaaa1111", "bbbb2222", "cccc3333"})
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

func listResult(id, title string, cost float64) models.SessionResult {
	return models.SessionResult{
		Entry: models.SessionEntry{SessionID: id},
		Analysis: &models.SessionAnalysis{
			Title: title, MessageCount: 1,
			TotalCost: models.CostBreakdown{TotalCost: cost},
		},
	}
}

// IDs shorten to 8 characters unless another listed session shares that
// prefix, so every ID shown still works with `ficha show`.
func TestListShortIDsStayUnique(t *testing.T) {
	got := shortSessionIDs([]models.SessionResult{
		listResult("abcdef12-0000", "", 0),
		listResult("abcdef12-1111", "", 0),
		listResult("99999999-2222", "", 0),
		listResult("short", "", 0),
	})
	want := []string{"abcdef12-0", "abcdef12-1", "99999999", "short"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("id %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// On a terminal, titles are cut to fit its width. Piped, they print whole.
// Control characters from the transcript never reach the terminal.
func TestListTitlesFitTerminal(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	long := "Refactor the auth middleware to use short-lived tokens and rotate refresh keys"
	results := []models.SessionResult{
		listResult("aaaaaaaa-1", long, 1.5),
		listResult("bbbbbbbb-2", "evil\x1b[31mred\x07title", 0.25),
	}
	for _, width := range []int{40, 50, 55, 57, 60, 80, 120} {
		out := FormatSessionListTable(results, true, ListTableOptions{Width: width})
		if w := maxLineWidth(out); w > width {
			t.Errorf("width %d: widest line is %d:\n%s", width, w, out)
		}
	}
	piped := FormatSessionListTable(results, true, ListTableOptions{})
	if !strings.Contains(piped, long) {
		t.Errorf("piped output should keep the whole title:\n%s", piped)
	}
	if strings.ContainsAny(piped, "\x1b\x07") || !strings.Contains(piped, "evil [31mred title") {
		t.Errorf("control characters should become spaces:\n%q", piped)
	}
}

// A narrow terminal gives up LENGTH and AGENTS, then MODEL once TITLE would
// get less than 12 columns. Past that, TITLE shrinks, to nothing at the
// narrowest, rather than the rows wrap.
func TestListNarrowTerminalDropsColumns(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	results := []models.SessionResult{
		listResult("aaaaaaaa-1", "Refactor the auth middleware", 1.5),
		listResult("bbbbbbbb-2", "Fix flaky test", 0.25),
	}
	for _, tc := range []struct {
		width                 int
		length, agents, model bool
		title                 string // what's left of TITLE's header
	}{
		{90, true, true, true, "TITLE"},
		{66, false, false, true, "TITLE"},
		{55, false, false, true, "TITLE"},
		{54, false, false, false, "TITLE"},
		{35, false, false, false, "TIT…"},
		{31, false, false, false, ""},
	} {
		out := FormatSessionListTable(results, true, ListTableOptions{Width: tc.width})
		header := findLine(t, out, "WHEN")
		for _, col := range []struct {
			name string
			want bool
		}{{"LENGTH", tc.length}, {"AGENTS", tc.agents}, {"MODEL", tc.model}} {
			if got := strings.Contains(header, col.name); got != col.want {
				t.Errorf("width %d: %s shown = %v, want %v:\n%s", tc.width, col.name, got, col.want, out)
			}
		}
		if !strings.HasSuffix(strings.TrimRight(header, " "), "COST"+map[bool]string{true: "  " + tc.title, false: ""}[tc.title != ""]) {
			t.Errorf("width %d: header should end in %q:\n%q", tc.width, tc.title, header)
		}
		if w := maxLineWidth(out); w > tc.width {
			t.Errorf("width %d: widest line is %d:\n%s", tc.width, w, out)
		}
	}
}

// findLine returns the first line of out containing needle.
func findLine(t *testing.T, out, needle string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line containing %q in:\n%s", needle, out)
	return ""
}

// A session left open for days has a LENGTH wider than "12h 34m"; the
// column grows rather than pushing that row out of line.
func TestListLongSessionStaysAligned(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	long := listResult("aaaaaaaa-1", "long", 1.5)
	long.Analysis.Duration = models.Duration(200*time.Hour + 30*time.Minute)
	results := []models.SessionResult{long, listResult("bbbbbbbb-2", "short", 0.25)}
	for _, width := range []int{0, 120} {
		out := FormatSessionListTable(results, true, ListTableOptions{Width: width})
		// Titles start where the TITLE header does.
		var col []int
		for _, line := range strings.Split(out, "\n") {
			for _, key := range []string{"TITLE", " long", " short"} {
				if i := strings.Index(line, key); i >= 0 {
					col = append(col, lipgloss.Width(line[:i+len(key)-len(strings.TrimSpace(key))]))
				}
			}
		}
		if len(col) != 3 || col[0] != col[1] || col[0] != col[2] {
			t.Errorf("width %d: TITLE column starts at %v:\n%s", width, col, out)
		}
		if width > 0 && maxLineWidth(out) > width {
			t.Errorf("width %d: widest line %d:\n%s", width, maxLineWidth(out), out)
		}
	}
}

// A project path comes from a transcript's cwd, so an escape in it must not
// reach the terminal or throw off the header box.
func TestHeaderStripsControlCharacters(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	out := renderPanel("/tmp/x\x1b[31mred", []string{"3 sessions"}, 76, true)
	if strings.Contains(out, "\x1b") {
		t.Errorf("escape reached the header: %q", out)
	}
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		if w := lipgloss.Width(l); w != 76 {
			t.Errorf("header line is %d columns, want 76: %q", w, l)
		}
	}
}

// Trailing padding goes, spaces between escape codes included, and the codes
// themselves stay, since one may close a style opened earlier on the line.
func TestTrimLineEnds(t *testing.T) {
	in := "a  $1.00  \n\x1b[32m$1.00\x1b[0m  \nbar \x1b[2m \x1b[0m\x1b[2m  \x1b[0m\nkeep"
	want := "a  $1.00\n\x1b[32m$1.00\x1b[0m\nbar\x1b[2m\x1b[0m\x1b[2m\x1b[0m\nkeep"
	if got := trimLineEnds(in); got != want {
		t.Errorf("trimLineEnds:\n got %q\nwant %q", got, want)
	}
}
