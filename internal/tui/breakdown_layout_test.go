package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/bardisty/ficha/internal/models"
)

func TestNewBreakdownLayout_ShedsColumnsWhole(t *testing.T) {
	all := breakdownLayout{indexWidth: 3, agentWidth: 13, runTags: true, in: true, out: true, cacheWrite: true, cacheRead: true}
	with := func(f func(*breakdownLayout)) breakdownLayout {
		l := all
		f(&l)
		return l
	}
	tests := []struct {
		name        string
		markerWidth int
		cellWidth   int
		termWidth   int
		want        breakdownLayout
	}{
		{"unknown width keeps all", 10, 13, 0, all},
		{"wide keeps all", 10, 13, 120, all},
		{"exact fit keeps all", 10, 13, 85, all},
		{"one short drops IN", 10, 13, 84, with(func(l *breakdownLayout) { l.in = false })},
		{"then C_WR", 10, 13, 76, with(func(l *breakdownLayout) { l.in, l.cacheWrite = false, false })},
		{"then C_RD", 10, 13, 68, with(func(l *breakdownLayout) { l.in, l.cacheWrite, l.cacheRead = false, false, false })},
		{"then OUT", 10, 13, 60, with(func(l *breakdownLayout) { l.in, l.cacheWrite, l.cacheRead, l.out = false, false, false, false })},
		{"then run tags", 10, 13, 53, breakdownLayout{indexWidth: 3, agentWidth: 10}},
		{"then AGENT, so COST stays", 10, 13, 50, breakdownLayout{indexWidth: 3}},
		{"narrower still: clipped", 10, 13, 30, breakdownLayout{indexWidth: 3}},
		{"half of 160 without run tags drops IN", 10, 10, 79, breakdownLayout{indexWidth: 3, agentWidth: 10, out: true, cacheWrite: true, cacheRead: true}},
		{"no agents: no AGENT column", 0, 0, 80, breakdownLayout{indexWidth: 3, in: true, out: true, cacheWrite: true, cacheRead: true}},
		{"AGENT is at least its header", 3, 3, 0, breakdownLayout{indexWidth: 3, agentWidth: 5, in: true, out: true, cacheWrite: true, cacheRead: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newBreakdownLayout(2, tt.markerWidth, tt.cellWidth, tt.termWidth)
			if got != tt.want {
				t.Errorf("newBreakdownLayout(2, %d, %d, %d) = %+v, want %+v", tt.markerWidth, tt.cellWidth, tt.termWidth, got, tt.want)
			}
			if w := len(got.header()); w != got.width() {
				t.Errorf("header is %d wide, layout says %d", w, got.width())
			}
		})
	}
	// The # column grows with the row count.
	if got := newBreakdownLayout(5, 0, 0, 0).indexWidth; got != 5 {
		t.Errorf("indexWidth for 5-digit rows = %d, want 5", got)
	}
}

func TestWorkflowRunTag(t *testing.T) {
	tests := map[string]string{
		"review-changes":    "rc",
		"audit-codebase":    "ac",
		"Review Changes v2": "rc",
		"deploy":            "de",
		"x":                 "x",
		"":                  "wf",
		"プロジェクト":            "wf", // no ASCII letters: the tag must stay one column per byte
		"--":                "wf",
	}
	for name, want := range tests {
		if got := workflowRunTag(name); got != want {
			t.Errorf("workflowRunTag(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestWorkflowRunTags_NumbersCollisions(t *testing.T) {
	got := workflowRunTags([]models.WorkflowMeta{
		{RunID: "wf_1", Name: "review-changes"},
		{RunID: "wf_2", Name: "audit-codebase"},
		{RunID: "wf_3", Name: "review-changes"},
		{RunID: "wf_4"},
	})
	want := map[string]string{"wf_1": "rc", "wf_2": "ac", "wf_3": "rc2", "wf_4": "wf"}
	for id, tag := range want {
		if got[id] != tag {
			t.Errorf("run %s tag = %q, want %q (all: %v)", id, got[id], tag, got)
		}
	}
}

// A highlighted (new) row, a settled row, the no-color row and the header must
// put every column in the same place, so highlighting a new row never moves a
// column.
func TestBreakdownRow_HighlightKeepsColumns(t *testing.T) {
	forceProfile(t, termenv.ANSI256)
	msg := models.BreakdownMessage{
		Index: 7, AgentID: "a641f79aa692e33b9", WorkflowID: "wf_1",
		Timestamp: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC), Model: "claude-opus-4-8",
		Usage: models.TokenUsage{InputTokens: 12, OutputTokens: 3000, CacheReadInputTokens: 190000},
		Cost:  models.CostBreakdown{TotalCost: 1.5},
	}
	colored := NewBreakdownModel("/p", "s", false, "", false)
	colored.messages = []models.BreakdownMessage{msg}
	colored.runTags = map[string]string{"wf_1": "rc"}
	plain := colored
	plain.noColor = true

	layout := colored.layout()
	settled := stripANSI(colored.renderRow(msg, false, layout))
	highlighted := stripANSI(colored.renderRow(msg, true, layout))
	noColor := plain.renderRow(msg, false, layout)

	if settled != highlighted || settled != noColor {
		t.Errorf("rows differ once styling is removed:\n settled:     %q\n highlighted: %q\n no-color:    %q", settled, highlighted, noColor)
	}
	if !strings.Contains(settled, "[A641f79a] rc") {
		t.Errorf("AGENT cell should read [A641f79a] rc: %q", settled)
	}
	header := layout.header()
	costEnd := strings.Index(header, "COST") + len("COST")
	if got := strings.Index(settled, "$1.50") + len("$1.50") + 2; got != costEnd {
		t.Errorf("COST header ends at column %d, the cost cell at %d:\n%s\n%s", costEnd, got, header, settled)
	}
	if lipgloss.Width(settled) != layout.width() {
		t.Errorf("row is %d wide, layout says %d", lipgloss.Width(settled), layout.width())
	}
}

// The Peak text carries the time to the second and the agent marker of the
// row it names, so the row can be matched by eye.
func TestBreakdownInsights_PeakNamesAgentRow(t *testing.T) {
	ts := time.Date(2026, 1, 15, 10, 11, 12, 0, time.UTC)
	m := BreakdownModel{
		noColor:   true,
		hasAgents: true,
		messages: []models.BreakdownMessage{
			{Index: 1, Timestamp: ts.Add(-time.Minute), Cost: models.CostBreakdown{TotalCost: 0.1}},
			{Index: 2, AgentID: "aa499eb92f589de14", Timestamp: ts, Cost: models.CostBreakdown{TotalCost: 0.44}},
		},
		insights: &models.MessageInsights{
			HighestCost: &models.MessageSnapshot{Index: 2, Timestamp: ts, Cost: 0.44},
			AverageCost: 0.1,
		},
	}
	out := m.renderCompactInsights()
	if !strings.Contains(out, "Peak: #2 $0.4400 @ 10:11:12 [Aa499eb9] (4.4x avg)") {
		t.Errorf("Peak text should name row 2 with seconds and its agent marker:\n%s", out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "main conversation + agents") {
		t.Errorf("scope should lead the line so a narrow terminal cuts the rest first:\n%s", out)
	}

	// A parent peak has no marker.
	m.insights.HighestCost = &models.MessageSnapshot{Index: 1, Timestamp: ts.Add(-time.Minute), Cost: 0.44}
	if out := m.renderCompactInsights(); strings.Contains(out, "[A") {
		t.Errorf("a parent peak should carry no agent marker:\n%s", out)
	}
	// A snapshot that doesn't match the row it indexes (a stale load) shows no
	// marker rather than a wrong one.
	m.insights.HighestCost = &models.MessageSnapshot{Index: 2, Timestamp: ts.Add(time.Hour), Cost: 0.44}
	if out := m.renderCompactInsights(); strings.Contains(out, "[A") {
		t.Errorf("a mismatched snapshot should carry no agent marker:\n%s", out)
	}
}

func TestJoinSegments(t *testing.T) {
	parts := []string{"scope: parent + agents", "Peak: #5 $1.87", "Trend: rising"}
	tests := []struct {
		width int
		want  string
	}{
		{0, "scope: parent + agents | Peak: #5 $1.87 | Trend: rising"},
		{55, "scope: parent + agents | Peak: #5 $1.87 | Trend: rising"},
		{54, "scope: parent + agents | Peak: #5 $1.87"},
		{39, "scope: parent + agents | Peak: #5 $1.87"},
		{38, "scope: parent + agents"},
		{5, "scope: parent + agents"}, // the first part stays; the frame clips it
	}
	for _, tt := range tests {
		if got := joinSegments(parts, " | ", tt.width); got != tt.want {
			t.Errorf("joinSegments(width %d) = %q, want %q", tt.width, got, tt.want)
		}
	}
	if got := joinSegments(nil, " | ", 10); got != "" {
		t.Errorf("joinSegments(nil) = %q, want empty", got)
	}
}

// On a narrow terminal the scope label gives way before Peak does: the label
// only qualifies the figures, so it must never be all the line shows.
func TestBreakdownInsights_ScopeGivesWayToPeak(t *testing.T) {
	ts := time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)
	m := BreakdownModel{
		noColor:   true,
		hasAgents: true,
		messages:  []models.BreakdownMessage{{Index: 1, AgentID: "a9f8e7d6c5b4a3210", Timestamp: ts, Cost: models.CostBreakdown{TotalCost: 12.34}}},
		insights: &models.MessageInsights{
			HighestCost: &models.MessageSnapshot{Index: 1, Timestamp: ts, Cost: 12.34},
			AverageCost: 1,
		},
	}
	for _, width := range []int{100, 79, 72, 64} {
		m.width = width
		out := m.renderCompactInsights()
		if !strings.Contains(out, "Peak: #1 $12.34") {
			t.Errorf("width %d: Peak missing from %q", width, out)
		}
		if lipgloss.Width(out) > width && width >= 60 {
			t.Errorf("width %d: line is %d wide: %q", width, lipgloss.Width(out), out)
		}
	}
	m.width = 100
	if out := m.renderCompactInsights(); !strings.Contains(out, "main conversation + agents") {
		t.Errorf("scope label should show when it fits: %q", out)
	}
}

// The header box, the rule under the column headings and the footer rule
// are as wide as the table once the table outgrows watch's panel, whether
// IN or a widened MODEL column is what grew it, and never wider than the
// terminal.
func TestBreakdownFrameTracksTableWidth(t *testing.T) {
	unknown := append(goldenBreakdownMessages(), models.BreakdownMessage{
		Index: 8, Timestamp: goldenTime(11, 30, 0), Model: "claude-experimental-model-with-a-long-id",
		Cost: models.CostBreakdown{TotalCost: 0.2},
	})
	for _, tc := range []struct {
		name string
		msgs []models.BreakdownMessage
	}{{"agents", goldenBreakdownMessages()}, {"unknown model", unknown}} {
		for _, width := range []int{60, 80, 86, 100, 120, 160} {
			m := loadedBreakdown(t, width, 40, tc.msgs)
			lines := strings.Split(frameOf(m), "\n")
			table := m.layout().width()
			frame := max(table, defaultPanelWidth+bdIndent)
			frame = min(frame, width)
			// Box top, box bottom, the column rule, and the footer rule.
			for _, i := range []int{0, 2, 6, len(lines) - 3} {
				if got := lipgloss.Width(strings.TrimRight(lines[i], " ")); got != frame {
					t.Errorf("%s, %d cols: line %d is %d wide, want %d (table %d): %q",
						tc.name, width, i, got, frame, table, lines[i])
				}
			}
		}
	}
}

// At 80 columns the insights line keeps a scope marker beside the Trend:
// without one, breakdown's Trend reads as contradicting watch's. The long
// label and the Peak's multiplier give way first.
func TestBreakdownInsights_ScopeStaysWithTrend(t *testing.T) {
	ts := goldenTime(10, 42, 13)
	m := BreakdownModel{
		noColor:   true,
		hasAgents: true,
		messages: []models.BreakdownMessage{
			{Index: 1, Timestamp: ts.Add(-time.Minute), Cost: models.CostBreakdown{TotalCost: 0.1}},
			{Index: 231, AgentID: "aa499eb92f589de14", Timestamp: ts, Cost: models.CostBreakdown{TotalCost: 0.4419}},
		},
		insights: &models.MessageInsights{
			HighestCost:   &models.MessageSnapshot{Index: 2, Timestamp: ts, Cost: 0.4419},
			AverageCost:   0.075,
			MessageCount:  models.MinMessagesForTrend,
			CostTrend:     models.TrendDecreasing,
			RecentAvgCost: 0.05,
			TrendWindow:   3,
		},
	}
	tests := []struct {
		width int
		want  string
	}{
		{120, "main conversation + agents │ Peak: #2 $0.4419 @ 10:42:13 [Aa499eb9] (5.9x avg) │ Trend: ▼ falling"},
		{90, "incl. agents │ Peak: #2 $0.4419 @ 10:42:13 [Aa499eb9] (5.9x avg) │ Trend: ▼ falling"},
		{80, "incl. agents │ Peak: #2 $0.4419 @ 10:42:13 [Aa499eb9] │ Trend: ▼ falling"},
		{60, "incl. agents │ Peak: #2 $0.4419 │ Trend: ▼ falling"},
		{45, "incl. agents │ Peak: #2 $0.4419"},
	}
	for _, tt := range tests {
		m.width = tt.width
		if got := strings.TrimSpace(m.renderCompactInsights()); got != tt.want {
			t.Errorf("width %d:\n got %q\nwant %q", tt.width, got, tt.want)
		}
	}
	for width := 40; width <= 160; width++ {
		m.width = width
		out := m.renderCompactInsights()
		if strings.Contains(out, "Trend") && !strings.Contains(out, "agents") {
			t.Errorf("width %d: Trend without its scope: %q", width, out)
		}
		if lipgloss.Width(out) > width {
			t.Errorf("width %d: line is %d wide: %q", width, lipgloss.Width(out), out)
		}
	}
}

// With agents but neither a Peak (no row stands out) nor a Trend (too few
// rows), the line is empty rather than a scope label qualifying nothing.
func TestBreakdownInsights_NoFiguresNoScope(t *testing.T) {
	m := BreakdownModel{
		noColor:   true,
		hasAgents: true,
		width:     100,
		messages:  []models.BreakdownMessage{{Index: 1}, {Index: 2, AgentID: "a1111111111111111"}},
		insights:  &models.MessageInsights{MessageCount: 2, AverageCost: 0.11},
	}
	if out := m.renderCompactInsights(); out != "" {
		t.Errorf("insights line = %q, want empty", out)
	}
}

// runTagRows is n parent rows with two workflow runs' agents among them:
// review-changes at rows 3 and 4, audit-codebase at row n-1.
func runTagRows(n int) ([]models.BreakdownMessage, []models.WorkflowMeta) {
	msgs := chromeRows(goldenTime(10, 0, 0), n)
	for _, i := range []int{2, 3} {
		msgs[i].AgentID, msgs[i].WorkflowID = "a1111111111111111", "wf_rc"
	}
	msgs[n-2].AgentID, msgs[n-2].WorkflowID = "a2222222222222222", "wf_ac"
	return msgs, []models.WorkflowMeta{{RunID: "wf_rc", Name: "review-changes"}, {RunID: "wf_ac", Name: "audit-codebase"}}
}

// The stats line spells out the run tags drawn on the visible rows, and
// only those: a key to a tag that's scrolled away explains nothing.
func TestBreakdownRunTagKey_NamesVisibleTags(t *testing.T) {
	msgs, runs := runTagRows(40)
	m := loadedBreakdown(t, 100, 24, nil)
	updated, _ := m.Update(breakdownMsgsMsg{messages: msgs, workflows: runs, insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)

	// Following: the bottom rows show audit-codebase's agent only.
	if got := m.renderStatsLine(m.layout()); !strings.HasSuffix(got, "│ ac = audit-codebase") || strings.Contains(got, "rc =") {
		t.Errorf("at the bottom: %q", got)
	}
	m.viewport.GotoTop()
	if got := m.renderStatsLine(m.layout()); !strings.HasSuffix(got, "│ rc = review-changes") || strings.Contains(got, "ac =") {
		t.Errorf("at the top: %q", got)
	}
	// Both on screen, each named once, in the order they appear.
	m.viewport.Height = 40
	if got := m.renderStatsLine(m.layout()); !strings.HasSuffix(got, "│ rc = review-changes, ac = audit-codebase") {
		t.Errorf("both visible: %q", got)
	}
	// No run on screen, no key.
	m.viewport.Height = 10
	m.viewport.SetYOffset(10)
	if got := m.renderStatsLine(m.layout()); strings.Contains(got, " = ") {
		t.Errorf("no tag visible: %q", got)
	}
}

// The key never pushes the totals off: entries that don't fit go whole, and
// a layout that dropped the tags from the AGENT column has nothing to key.
func TestBreakdownRunTagKey_GivesWay(t *testing.T) {
	msgs, runs := runTagRows(6)
	for _, tc := range []struct {
		width int
		want  string
	}{
		{100, "  Messages: 6 │ Total: $0.0000 │ rc = review-changes, ac = audit-codebase"},
		{70, "  Messages: 6 │ Total: $0.0000 │ rc = review-changes"},
		{50, "  Messages: 6 │ Total: $0.0000"},
	} {
		m := loadedBreakdown(t, tc.width, 24, nil)
		updated, _ := m.Update(breakdownMsgsMsg{messages: msgs, workflows: runs, insights: &models.MessageInsights{}})
		m = updated.(BreakdownModel)
		if got := m.renderStatsLine(m.layout()); got != tc.want {
			t.Errorf("width %d:\n got %q\nwant %q", tc.width, got, tc.want)
		}
		if !m.layout().runTags && strings.Contains(frameOf(m), " rc") {
			t.Errorf("width %d: tags dropped from AGENT but still drawn", tc.width)
		}
	}
}

// A workflow name comes from a file on disk. Whatever it holds, the key
// stays on one row and sends the terminal nothing but text.
func TestBreakdownRunTagKey_PrintableNames(t *testing.T) {
	msgs, runs := runTagRows(6)
	runs[0].Name = "review\nchan\x1b[31mges\t"
	runs[1].Name = ""
	m := loadedBreakdown(t, 120, 24, nil)
	updated, _ := m.Update(breakdownMsgsMsg{messages: msgs, workflows: runs, insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if got := m.renderStatsLine(m.layout()); !strings.HasSuffix(got, "│ rc = reviewchan[31mges, wf = wf_ac") {
		t.Errorf("stats line: %q", got)
	}
	if rows := len(strings.Split(frameOf(m), "\n")); rows != 24 {
		t.Errorf("frame is %d rows, want 24", rows)
	}
}
