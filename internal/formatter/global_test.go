package formatter

import (
	"fmt"
	"strings"
	"testing"
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

// A negative topN must be clamped, not used as a slice index (projects[-1] panics).
func TestRenderProjectsTable_NegativeTopN(t *testing.T) {
	analysis := globalAnalysisWithProjects(
		map[string]float64{"alpha": 1.0, "beta": 2.0},
		[]string{"beta", "alpha"},
	)

	out := renderProjectsTable(analysis, true, -1, false)
	if !strings.Contains(out, "(2 more projects totaling $3.00)") {
		t.Errorf("negative topN should show all projects as remaining, got:\n%s", out)
	}
	if strings.Contains(out, "alpha") || strings.Contains(out, "beta") {
		t.Errorf("negative topN should render no project rows, got:\n%s", out)
	}

	// The full-table entry point clamps too, not just renderProjectsTable.
	full := FormatGlobalTable(analysis, true, -1, false)
	if !strings.Contains(full, "TOP PROJECTS (0)") {
		t.Errorf("negative topN should clamp header count to 0, got:\n%s", full)
	}
}

func TestRenderProjectsTable_TopNZero(t *testing.T) {
	analysis := globalAnalysisWithProjects(
		map[string]float64{"alpha": 1.0},
		[]string{"alpha"},
	)

	out := renderProjectsTable(analysis, true, 0, false)
	if !strings.Contains(out, "(1 more projects totaling $1.00)") {
		t.Errorf("topN=0 should show all projects as remaining, got:\n%s", out)
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

	out := renderProjectsTable(byName, false, 10, false)

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
			out := renderProjectsTable(analysis, tc.noColor, 10, tc.showDetails)

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

	t.Run("byte length exceeds width but display width fits", func(t *testing.T) {
		in := strings.Repeat("é", 20)
		if got := truncateLeft(in, 25); got != in {
			t.Errorf("string with display width 20 should fit in 25, got %q", got)
		}
	})
}
