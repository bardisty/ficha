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
	if !strings.HasPrefix(strings.TrimSpace(out), "scope:") {
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
	if out := m.renderCompactInsights(); !strings.Contains(out, "scope: parent + agents") {
		t.Errorf("scope label should show when it fits: %q", out)
	}
}
