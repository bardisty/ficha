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
		Render(fmt.Sprintf("%10s", "$10.00"))
	if !strings.Contains(out, wantStyled) {
		t.Errorf("max-cost project should be styled with true-bounds gradient color\nwant substring: %q\ngot:\n%q", wantStyled, out)
	}
}

// truncateMiddle must cut on rune boundaries and measure display width, not
// byte-slice UTF-8 (which corrupts multi-byte runes and miscounts width).
func TestTruncateMiddle(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateMiddle(tt.input, tt.maxWidth)
			if !utf8.ValidString(got) {
				t.Errorf("result is not valid UTF-8: %q", got)
			}
			if w := runewidth.StringWidth(got); w > tt.maxWidth {
				t.Errorf("display width %d exceeds max %d: %q", w, tt.maxWidth, got)
			}
		})
	}

	t.Run("short string unchanged", func(t *testing.T) {
		if got := truncateMiddle("short", 45); got != "short" {
			t.Errorf("got %q, want %q", got, "short")
		}
	})

	t.Run("keeps start and end", func(t *testing.T) {
		in := "AAAA" + strings.Repeat("x", 100) + "ZZZZ"
		got := truncateMiddle(in, 45)
		if !strings.HasPrefix(got, "AAAA") || !strings.HasSuffix(got, "ZZZZ") {
			t.Errorf("expected start...end shape, got %q", got)
		}
		if !strings.Contains(got, "...") {
			t.Errorf("expected ellipsis, got %q", got)
		}
	})

	t.Run("byte length exceeds width but display width fits", func(t *testing.T) {
		// 20 runes, 40 bytes, display width 20 — old byte-based check truncated this
		in := strings.Repeat("é", 20)
		if got := truncateMiddle(in, 25); got != in {
			t.Errorf("string with display width 20 should fit in 25, got %q", got)
		}
	})
}
