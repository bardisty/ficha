package formatter

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// narrowReports renders every show and summary fixture at a terminal width,
// in color or not.
func narrowReports(t *testing.T, width int, noColor bool) map[string]string {
	t.Helper()
	analysis, results := summaryDetailsAnalysis(t, summaryDetailsFixture(t))
	wfAnalysis, wfResults := summaryDetailsAnalysis(t, summaryWorkflowFixture(t))
	// Past $1000 the sum widens COST, which the agent trees must make room for.
	pricey, priceyResults := summaryDetailsAnalysis(t, summaryWorkflowFixture(t))
	priceyResults[0].Analysis.TotalCost.TotalCost = 600
	priceyResults = append(priceyResults, sessionRow("bbbb2222", "claude-opus-4-8", 10, 600))
	return map[string]string{
		"show":                     FormatSessionTable(goldenShowAnalysis(), noColor, width),
		"show workflows":           FormatSessionTable(goldenShowWorkflowAnalysis(), noColor, width),
		"show mixed widths":        FormatSessionTable(goldenMixedWidthAnalysis(), noColor, width),
		"summary":                  FormatSessionTable(goldenSummaryAnalysis(), noColor, width),
		"summary details":          FormatSummaryTableWithDetails(analysis, results, "", noColor, false, width),
		"summary expand":           FormatSummaryTableWithDetails(analysis, results, "", noColor, true, width),
		"summary expand workflows": FormatSummaryTableWithDetails(wfAnalysis, wfResults, "", noColor, true, width),
		"summary details pricey":   FormatSummaryTableWithDetails(pricey, priceyResults, "", noColor, false, width),
		"summary expand pricey":    FormatSummaryTableWithDetails(pricey, priceyResults, "", noColor, true, width),
	}
}

// On a terminal from 50 to 76 columns wide, no line of show or summary is
// wider than the terminal, so none wraps.
func TestReportsFitNarrowTerminals(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		if !noColor {
			forceProfile(t, termenv.ANSI256)
		}
		for width := 50; width <= 76; width++ {
			for name, out := range narrowReports(t, width, noColor) {
				for line := range strings.SplitSeq(out, "\n") {
					if w := lipgloss.Width(line); w > width {
						t.Errorf("%s at %d (noColor=%v): line is %d wide:\n%s", name, width, noColor, w, line)
					}
				}
			}
		}
	}
}

// A terminal at least 76 wide gets exactly the piped report: the layout
// doesn't stretch.
func TestWideTerminalMatchesPiped(t *testing.T) {
	piped := narrowReports(t, 0, true)
	for _, width := range []int{76, 80, 200} {
		for name, out := range narrowReports(t, width, true) {
			if out != piped[name] {
				t.Errorf("%s at %d differs from piped output", name, width)
			}
		}
	}
}

func TestReportWidth(t *testing.T) {
	for terminal, want := range map[int]int{0: 76, -1: 76, 200: 76, 76: 76, 60: 60, 50: 50, 45: 45, 40: 45, 20: 45} {
		if got := reportWidth(terminal); got != want {
			t.Errorf("reportWidth(%d) = %d, want %d", terminal, got, want)
		}
	}
}

// The cost rows lose their trailing notes, all together, before they'd wrap,
// and a cache write's TTL moves into its label so the 5m and 1h rows stay
// apart. Then the counts lose "tokens".
func TestCostRowsDropNotesWhenNarrow(t *testing.T) {
	cost := models.CostBreakdown{InputCost: 1, CacheWrite5mCost: 1, CacheWrite1hCost: 2, CacheReadCost: 1, CacheSavings: 9}
	usage := models.TokenUsage{
		InputTokens:              1000,
		CacheCreationInputTokens: 3000,
		CacheCreation:            &models.CacheCreation{Ephemeral5mInputTokens: 1000, Ephemeral1hInputTokens: 2000},
		CacheReadInputTokens:     5000,
	}
	cases := []struct {
		width      int
		want, not  []string
		maxRowWide int
	}{
		{76, []string{"Cache write  ", "5m TTL", "1h TTL", "(from cache reads)", "tokens"}, []string{"Cache write 5m"}, 76},
		{57, []string{"5m TTL", "1h TTL", "(from cache reads)", "tokens"}, []string{"Cache write 5m"}, 57},
		{56, []string{"Cache write 5m", "Cache write 1h", "Savings", "tokens"}, []string{"TTL", "(from cache reads)"}, 56},
		{49, []string{"Cache write 5m", "tokens"}, []string{"TTL", "(from cache reads)"}, 49},
		{48, []string{"Cache write 5m", "Cache write 1h", "Savings"}, []string{"TTL", "(from cache reads)", "tokens"}, 48},
	}
	for _, tc := range cases {
		out := renderCostRows(cost, usage, tc.width, true)
		for _, s := range tc.want {
			if !strings.Contains(out, s) {
				t.Errorf("width %d: missing %q:\n%s", tc.width, s, out)
			}
		}
		for _, s := range tc.not {
			if strings.Contains(out, s) {
				t.Errorf("width %d: has %q:\n%s", tc.width, s, out)
			}
		}
		if w := lipgloss.Width(out); w > tc.maxRowWide {
			t.Errorf("width %d: rows are %d wide:\n%s", tc.width, w, out)
		}
	}
}

// A show's header keeps the project over its duration when the box is too
// narrow for both. A windowed summary's header never loses its window: the
// figures only cover it.
func TestPanelGivesWayInOrder(t *testing.T) {
	show := []string{"Session: 68994c84", "Duration: 3h 12m"}
	showOrder := []int{1, panelLead}
	if out := renderPanel("~/src/webapp", show, showOrder, 50, true); !strings.Contains(out, "~/src/webapp") || strings.Contains(out, "Duration") {
		t.Errorf("want the project and no duration:\n%s", out)
	}
	if out := renderPanel("~/src/webapp", show, showOrder, 76, true); !strings.Contains(out, "~/src/webapp") || !strings.Contains(out, "Duration: 3h 12m") {
		t.Errorf("want every field at 76:\n%s", out)
	}
	windowed := []string{"123 sessions", "Window: Dec 01 2025 → Dec 31 2025"}
	for width := 45; width <= 76; width++ {
		if out := renderPanel("~/source/claude-code-usage", windowed, []int{panelLead, 0}, width, true); !strings.Contains(out, "Window: Dec 01 2025 → Dec 31 2025") {
			t.Errorf("width %d: want the window kept:\n%s", width, out)
		}
	}
}

// A window too long for a narrow header on its own keeps its range and
// drops its label, rather than wrap.
func TestHeaderWindowFitsNarrowBox(t *testing.T) {
	a := goldenSummaryAnalysis()
	a.Window = &models.TimeWindow{
		Since: time.Date(2025, 1, 3, 14, 30, 0, 0, time.UTC),
		Until: time.Date(2025, 12, 18, 9, 15, 0, 0, time.UTC),
	}
	for width := 50; width <= 76; width++ {
		out := renderHeaderPanel(a, width, true)
		if !strings.Contains(out, "Jan 03 2025 14:30") || !strings.Contains(out, "Dec 18 2025 09:15") {
			t.Errorf("width %d: want the window's range:\n%s", width, out)
		}
		if w := lipgloss.Width(out); w > width {
			t.Errorf("width %d: header is %d wide:\n%s", width, w, out)
		}
	}
}

// A workflow's status goes whole before its name is cut, and a cut never
// leaves a space before the ellipsis, piped or narrowed alike.
func TestFitWorkflowLabel(t *testing.T) {
	meta := models.WorkflowMeta{RunID: "wf1", Name: "review-changes", Status: "completed"}
	for width, want := range map[int]string{
		40: "workflow: review-changes (completed)",
		30: "workflow: review-changes",
		24: "workflow: review-changes",
		20: "workflow: review-ch…",
		16: "workflow: revie…",
	} {
		if got := fitWorkflowLabel("", meta, width, width >= 36); got != want {
			t.Errorf("width %d: got %q, want %q", width, got, want)
		}
	}
	if got := fitWorkflowLabel("", meta, 11, false); strings.Contains(got, " "+"…") {
		t.Errorf("width 11: %q leaves a space before the ellipsis", got)
	}
}

// First and Last keep or lose their detail together, whatever each one's
// detail is.
func TestInsightsDetailIsAllOrNothing(t *testing.T) {
	insights := goldenShowAnalysis().Insights
	insights.FirstMessage.MainCostComponent = "output"
	insights.LastMessage.MainCostComponent = "cache_write_5m"
	for width := 45; width <= 76; width++ {
		out := formatInsightsSectionContent(insights, false, width, true)
		first := strings.Contains(out, "Output:")
		last := strings.Contains(out, "Cache write 5m:")
		if first != last {
			t.Errorf("width %d: First detail %v, Last detail %v:\n%s", width, first, last, out)
		}
	}
}

// Workflow headings keep or lose their statuses together, so one without
// a status never sits beside one with.
func TestWorkflowStatusesAllOrNothing(t *testing.T) {
	a := goldenShowWorkflowAnalysis()
	a.Workflows = append(a.Workflows, models.WorkflowMeta{RunID: "wf-long", Name: "a-much-longer-workflow-name", Status: "running"})
	a.Agents = append(a.Agents, models.AgentAnalysis{AgentID: "long1", WorkflowID: "wf-long", MessageCount: 2,
		TotalCost: models.CostBreakdown{TotalCost: 0.5}, CostByModel: map[string]models.CostBreakdown{"claude-opus-4-8": {TotalCost: 0.5}}})
	for width := 50; width < 76; width++ {
		out := FormatSessionTable(a, true, width)
		withStatus := strings.Count(out, "(completed)") + strings.Count(out, "(running)")
		if withStatus != 0 && withStatus != 2 {
			t.Errorf("width %d: %d of 2 workflow headings show a status:\n%s", width, withStatus, out)
		}
	}
}

func TestGoldenSessionTableShowNarrow(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	checkGolden(t, "session_table_show_w50", FormatSessionTable(goldenShowWorkflowAnalysis(), true, 50))
}

func TestGoldenSummaryDetailsNarrow(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	analysis, results := summaryDetailsAnalysis(t, summaryWorkflowFixture(t))
	checkGolden(t, "summary_details_expand_workflows_w50", FormatSummaryTableWithDetails(analysis, results, "", true, true, 50))
}

// The summary chart narrows with the report, 4 columns in from each side,
// and so does its label.
func TestCostChartFitsNarrowTerminals(t *testing.T) {
	for width := 50; width <= 76; width++ {
		for line := range strings.SplitSeq(renderCostChart(chartFixture(80), width, true), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("chart at %d: line is %d wide:\n%s", width, w, line)
			}
		}
	}
}

// At 45 and 46 columns summary -d's MODEL column narrows far enough to cut
// a display name, and the cut drops the version whole.
func TestGoldenSummaryDetailsModelCut(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	analysis, results := summaryDetailsAnalysis(t, summaryDetailsFixture(t))
	for _, width := range []int{45, 46} {
		checkGolden(t, fmt.Sprintf("summary_details_%d", width), FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, false, width))
	}
}
