package formatter

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

// globalAnalysisWithProjects builds a minimal GlobalAnalysis for table rendering.
// Projects are given in slice order (callers control the sort).
func globalAnalysisWithProjects(costs map[string]float64, order []string) *models.GlobalAnalysis {
	analysis := &models.GlobalAnalysis{
		ProjectCount: len(order),
	}
	for _, name := range order {
		p := models.ProjectAnalysis{
			SessionCount: 1,
			MessageCount: 1,
		}
		p.DisplayName = name
		p.TotalCost.TotalCost = costs[name]
		analysis.Projects = append(analysis.Projects, p)
		analysis.TotalCost.TotalCost += costs[name]
		analysis.SessionCount++
		analysis.MessageCount++
	}
	return analysis
}

// projectsTable renders the projects section alone, laid out as
// FormatGlobalTable would with opts, in cost order unless opts says otherwise.
func projectsTable(analysis *models.GlobalAnalysis, noColor bool, opts GlobalTableOptions) string {
	if opts.SortBy == "" {
		opts.SortBy = "cost"
	}
	if opts.Now.IsZero() {
		opts.Now = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	}
	return renderProjectsTable(analysis, noColor, newProjectsLayout(analysis.Projects, opts), opts)
}

// A negative topN must be clamped, not used as a slice index (projects[-1] panics).
func TestRenderProjectsTable_NegativeTopN(t *testing.T) {
	analysis := globalAnalysisWithProjects(
		map[string]float64{"alpha": 1.0, "beta": 2.0},
		[]string{"beta", "alpha"},
	)

	out := projectsTable(analysis, true, GlobalTableOptions{TopN: -1})
	if !strings.Contains(out, "(2 projects totaling $3.00)") {
		t.Errorf("negative topN should show all projects as remaining, got:\n%s", out)
	}
	if strings.Contains(out, "alpha") || strings.Contains(out, "beta") {
		t.Errorf("negative topN should render no project rows, got:\n%s", out)
	}

	// The full-table entry point clamps too, not just renderProjectsTable.
	full := FormatGlobalTable(analysis, true, GlobalTableOptions{TopN: -1})
	if !strings.Contains(full, "PROJECTS (0 of 2, by cost)") || !strings.Contains(full, "(2 projects totaling $3.00)") || strings.Contains(full, "SESSIONS") {
		t.Errorf("negative topN should render like --top 0, got:\n%s", full)
	}
}

func TestRenderProjectsTable_TopNZero(t *testing.T) {
	analysis := globalAnalysisWithProjects(
		map[string]float64{"alpha": 1.0},
		[]string{"alpha"},
	)

	out := projectsTable(analysis, true, GlobalTableOptions{TopN: 0})
	if out != "  (1 project totaling $1.00)\n" {
		t.Errorf("topN=0 should print only the remainder line, got:\n%s", out)
	}
}

// Gradient bounds must be scanned from the actual costs, not read positionally
// (first=max, last=min): the caller re-sorts by name|sessions|activity, so
// positional bounds invert and neutralize the gradient.
func TestRenderProjectsTable_GradientIgnoresSortOrder(t *testing.T) {
	r := lipgloss.DefaultRenderer()
	origProfile := r.ColorProfile()
	r.SetColorProfile(termenv.ANSI256)
	defer r.SetColorProfile(origProfile)

	costs := map[string]float64{"alpha": 1.0, "beta": 10.0, "zeta": 5.0}
	// Name order: cheapest first, so positional bounds would give maxCost < minCost
	byName := globalAnalysisWithProjects(costs, []string{"alpha", "beta", "zeta"})

	out := projectsTable(byName, false, GlobalTableOptions{TopN: 10})

	// The most expensive project must get the top-of-gradient color computed
	// from the true min/max, regardless of slice order
	wantStyled := lipgloss.NewStyle().
		Foreground(styles.GetCostGradientColor(10.0, 1.0, 10.0)).
		Render("$10.00")
	if !strings.Contains(out, wantStyled) {
		t.Errorf("max-cost project should be styled with true-bounds gradient color\nwant substring: %q\ngot:\n%q", wantStyled, out)
	}
}

// The PROJECT column pads by display width: fmt's %-45s counts runes, so a
// CJK or emoji name (2 cells per rune) used to under-pad and shift every
// column to its right on that row alone.
func TestRenderProjectsTableWideNamesAlign(t *testing.T) {
	names := []string{
		"ascii-project",
		"项目分析工具",                 // CJK: 6 runes, 12 cells
		"🚀-rocket",               // emoji: 2 cells
		strings.Repeat("分析", 30), // 60 cells: middle-truncated to the column
	}
	costs := make(map[string]float64, len(names))
	for i, n := range names {
		costs[n] = float64(i + 1)
	}
	analysis := globalAnalysisWithProjects(costs, names)

	for _, tc := range []struct {
		name        string
		noColor     bool
		showDetails bool
	}{
		{"no-color", true, false},
		{"no-color-details", true, true},
		{"color", false, false},
		{"color-details", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.noColor {
				forceProfile(t, termenv.Ascii)
			} else {
				forceProfile(t, termenv.ANSI256)
			}
			out := projectsTable(analysis, tc.noColor, GlobalTableOptions{TopN: 10, Details: tc.showDetails})

			var headerWidth int
			var rowWidths []int
			for _, line := range strings.Split(out, "\n") {
				switch {
				case strings.Contains(line, "PROJECT"):
					headerWidth = lipgloss.Width(line)
				case strings.Contains(line, "$"):
					rowWidths = append(rowWidths, lipgloss.Width(line))
				}
			}
			if headerWidth == 0 || len(rowWidths) != len(names) {
				t.Fatalf("expected a header and %d data rows, got header=%d rows=%d:\n%s",
					len(names), headerWidth, len(rowWidths), out)
			}
			for i, w := range rowWidths {
				if w != headerWidth {
					t.Errorf("row %d is %d columns, header is %d:\n%s", i, w, headerWidth, out)
				}
			}
		})
	}
}

// truncateLeft must cut on rune boundaries and measure display width, not
// bytes, in both glyph sets.
func TestTruncateLeft(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
	}{
		{"ascii long", strings.Repeat("abcdef", 20), 45},
		{"multibyte latin", strings.Repeat("é", 60), 45},
		{"cjk wide runes", strings.Repeat("世界", 40), 45},
		{"mixed ascii and cjk", "project-" + strings.Repeat("日本語", 20) + "-end", 45},
		{"tiny max width", strings.Repeat("é", 20), 3},
		{"max width 2", strings.Repeat("世", 20), 2},
		{"path", "~/source/github.com/acme/billing-service/worktrees/fix-invoice-rounding", 45},
	}

	for _, ascii := range []bool{false, true} {
		t.Run(fmt.Sprintf("ascii=%v", ascii), func(t *testing.T) {
			if ascii {
				useASCII(t)
			}
			for _, tt := range tests {
				got := truncateLeft(tt.input, tt.maxWidth)
				if !utf8.ValidString(got) {
					t.Errorf("%s: result is not valid UTF-8: %q", tt.name, got)
				}
				if w := runewidth.StringWidth(got); w > tt.maxWidth {
					t.Errorf("%s: display width %d exceeds max %d: %q", tt.name, w, tt.maxWidth, got)
				}
			}
		})
	}

	t.Run("short string unchanged", func(t *testing.T) {
		if got := truncateLeft("short", 45); got != "short" {
			t.Errorf("got %q, want %q", got, "short")
		}
	})

	t.Run("keeps the tail", func(t *testing.T) {
		got := truncateLeft("~/source/github.com/acme/billing-service/worktrees/fix-invoice-rounding", 45)
		if !strings.HasSuffix(got, "service/worktrees/fix-invoice-rounding") {
			t.Errorf("the distinctive tail should survive, got %q", got)
		}
		if runewidth.StringWidth(got) != 45 || !strings.HasPrefix(got, "…") {
			t.Errorf("want 45 columns starting with the ellipsis, got %q", got)
		}
	})

	t.Run("snaps to a nearby separator", func(t *testing.T) {
		// Cutting "~/fx/work/ml-pipeline" to 19 columns leaves "x/work/…";
		// the separator one column on reads better.
		if got := truncateLeft("~/fx/work/ml-pipeline", 19); got != "…/work/ml-pipeline" {
			t.Errorf("got %q, want %q", got, "…/work/ml-pipeline")
		}
		// A separator far from the cut would throw away too much.
		if got := truncateLeft("~/src/billing-service-worktrees/x", 20); got != "…service-worktrees/x" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("byte length exceeds width but display width fits", func(t *testing.T) {
		in := strings.Repeat("é", 20)
		if got := truncateLeft(in, 25); got != in {
			t.Errorf("string with display width 20 should fit in 25, got %q", got)
		}
	})
}

// maxLineWidth is the widest line of out in display columns.
func maxLineWidth(out string) int {
	w := 0
	for _, line := range strings.Split(out, "\n") {
		w = max(w, lipgloss.Width(line))
	}
	return w
}

// Piped, the report keeps a fixed layout that fits in 80 columns whatever
// the path lengths, with or without the cumulative column.
func TestGlobalTableFitsEightyColumnsWhenPiped(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	for _, details := range []bool{false, true} {
		opts := goldenGlobalOptions(details)
		out := FormatGlobalTable(goldenGlobalAnalysis(), true, opts)
		if w := maxLineWidth(out); w > 80 {
			t.Errorf("details=%v: widest line is %d columns, want <= 80:\n%s", details, w, out)
		}
		// Nor do large costs, which widen COST and CUMULATIVE.
		rich := goldenGlobalAnalysis()
		for i := range rich.Projects {
			rich.Projects[i].TotalCost.TotalCost *= 1000
		}
		rich.TotalCost.TotalCost *= 1000
		if w := maxLineWidth(FormatGlobalTable(rich, true, opts)); w > 80 {
			t.Errorf("details=%v: with $191k of costs the widest line is %d columns, want <= 80", details, w)
		}
		// Long paths don't widen the piped layout.
		long := goldenGlobalAnalysis()
		long.Projects[0].DisplayName = "~/" + strings.Repeat("deep/", 30) + "project"
		if got, want := maxLineWidth(FormatGlobalTable(long, true, opts)), maxLineWidth(out); got != want {
			t.Errorf("details=%v: a long path changed the piped width from %d to %d", details, want, got)
		}
	}
}

// On a terminal, PROJECT widens to show a long path whole, up to the
// terminal's width, and no line runs past it. The other columns take 50.
func TestGlobalTableSizesProjectToTerminal(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	analysis := goldenGlobalAnalysis()
	path := "~/source/github.com/acme/billing-service/fix-round"
	if len(path) != 50 {
		t.Fatalf("path is %d columns, want 50", len(path))
	}
	analysis.Projects[0].DisplayName = path

	for _, width := range []int{60, 80, 100, 200} {
		opts := goldenGlobalOptions(false)
		opts.Width = width
		out := FormatGlobalTable(analysis, true, opts)
		if w := maxLineWidth(out); w > width {
			t.Errorf("width %d: widest line is %d columns:\n%s", width, w, out)
		}
		if whole := strings.Contains(out, path); whole != (width >= 100) {
			t.Errorf("width %d: path shown whole = %v:\n%s", width, whole, out)
		}
	}
}

// A terminal too narrow for every column gives up % TOTAL, then LAST ACTIVE,
// then SESSIONS, each unless the rows are sorted by it, rather than wrapping
// each row.
func TestGlobalTableNarrowTerminalDropsColumns(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	for _, tc := range []struct {
		width                 int
		sortBy                string
		pct, active, sessions bool
	}{
		{80, "cost", true, true, true},
		{60, "cost", false, true, true},
		{60, "activity", false, true, true},
		{50, "cost", false, false, true},
		{50, "activity", false, true, false},
		{50, "sessions", false, false, true},
		{43, "cost", false, false, false},
		{44, "sessions", false, false, true},
		{36, "name", false, false, false},
	} {
		opts := goldenGlobalOptions(false)
		opts.Width = tc.width
		opts.SortBy = tc.sortBy
		out := FormatGlobalTable(goldenGlobalAnalysis(), true, opts)
		if got := strings.Contains(out, "% TOTAL"); got != tc.pct {
			t.Errorf("width %d by %s: %% TOTAL shown = %v, want %v:\n%s", tc.width, tc.sortBy, got, tc.pct, out)
		}
		if got := strings.Contains(out, "LAST ACTIVE"); got != tc.active {
			t.Errorf("width %d by %s: LAST ACTIVE shown = %v, want %v:\n%s", tc.width, tc.sortBy, got, tc.active, out)
		}
		if got := strings.Contains(out, "SESSIONS"); got != tc.sessions {
			t.Errorf("width %d by %s: SESSIONS shown = %v, want %v:\n%s", tc.width, tc.sortBy, got, tc.sessions, out)
		}
		// Below 60, the cost rows above the table are wider than the
		// terminal, so only the header box, the table and the footer are
		// held to it.
		table := out
		if tc.width < 60 {
			lines := strings.Split(out, "\n")
			table = strings.Join(lines[:3], "\n") + "\n" + out[strings.Index(out, "PROJECTS ("):]
		}
		if w := maxLineWidth(table); w > tc.width {
			t.Errorf("width %d by %s: widest line is %d columns:\n%s", tc.width, tc.sortBy, w, table)
		}
	}
}

// A footer that needs two lines splits into even halves, not a full line and
// a lone field, and never runs past the width when the fields fit.
func TestSplitFooterFields(t *testing.T) {
	const sep = " | "
	global := []string{"Total: $124.42", "Messages: 1,532", "Sessions: 41", "Projects: 20"}
	for _, tc := range []struct {
		fields []string
		width  int
		want   []string
	}{
		{global, 80, []string{"Total: $124.42 | Messages: 1,532 | Sessions: 41 | Projects: 20"}},
		{global, 60, []string{"Total: $124.42 | Messages: 1,532", "Sessions: 41 | Projects: 20"}},
		{global, 20, []string{"Total: $124.42", "Messages: 1,532", "Sessions: 41", "Projects: 20"}},
		// Three fields over two lines: the fuller line comes first.
		{[]string{"aaaa", "bbbb", "cccc"}, 12, []string{"aaaa | bbbb", "cccc"}},
		// A field wider than the width still gets its own line.
		{[]string{"a-very-long-field", "b"}, 10, []string{"a-very-long-field", "b"}},
		{[]string{"only"}, 2, []string{"only"}},
	} {
		got := splitFooterFields(tc.fields, sep, tc.width)
		if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("width %d: got %q, want %q", tc.width, got, tc.want)
		}
	}
}

// The title names the sort, LAST ACTIVE shows the key for --sort-by activity,
// and a running total only appears where it means something: cost order.
func TestGlobalTableNamesItsSort(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	for _, tc := range []struct {
		sortBy     string
		details    bool
		title      string
		cumulative bool
	}{
		{"cost", false, "PROJECTS (3 of 5, by cost)", false},
		{"cost", true, "PROJECTS (all 5, by cost)", true},
		{"name", true, "PROJECTS (all 5, by name)", false},
		{"sessions", true, "PROJECTS (all 5, by sessions)", false},
		{"activity", false, "PROJECTS (3 of 5, by activity)", false},
	} {
		opts := goldenGlobalOptions(tc.details)
		opts.SortBy = tc.sortBy
		out := FormatGlobalTable(goldenGlobalAnalysis(), true, opts)
		if !strings.Contains(out, tc.title) {
			t.Errorf("%s details=%v: missing title %q:\n%s", tc.sortBy, tc.details, tc.title, out)
		}
		if got := strings.Contains(out, "CUMULATIVE"); got != tc.cumulative {
			t.Errorf("%s details=%v: CUMULATIVE shown = %v, want %v", tc.sortBy, tc.details, got, tc.cumulative)
		}
		if !strings.Contains(out, "LAST ACTIVE") || !strings.Contains(out, "30m ago") {
			t.Errorf("%s details=%v: want LAST ACTIVE with relative times:\n%s", tc.sortBy, tc.details, out)
		}
	}
}
