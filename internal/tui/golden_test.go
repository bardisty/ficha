package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/styles"
)

var update = flag.Bool("update", false, "rewrite .golden files with current rendered output")

// TestMain pins the process timezone: per-message times and the header clock
// render in local time, so goldens would otherwise depend on the machine's TZ.
//
// The palette is dark unless a test calls styles.SetDark(false), and a view
// built with color on always carries its xterm-256 escapes. A test of the
// light palette must switch before it builds the view: the spinner and the
// chart take their colors then, not when the frame is drawn.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

// checkGolden compares got against testdata/<name>.golden byte-for-byte.
// Regenerate with `make update-golden` after intentional rendering changes.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("creating testdata dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		return
	}
	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file %s (regenerate with `make update-golden`): %v", path, err)
	}
	want := string(wantBytes)
	if got == want {
		return
	}
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			t.Fatalf("%s: output differs from golden at line %d\n got: %q\nwant: %q\n(intentional change? regenerate with `make update-golden` and review the diff)",
				path, i+1, g, w)
		}
	}
	t.Fatalf("%s: output differs from golden (same lines, different trailing bytes)", path)
}

func goldenTime(hour, min, sec int) time.Time {
	return time.Date(2026, 1, 15, hour, min, sec, 0, time.UTC)
}

// goldenViewAnalysis mirrors the formatter golden fixture: both cache TTLs,
// cache reads, savings, context window, three models, two agents, insights,
// plus per-message costs so the watch view's COST TREND chart has data.
func goldenViewAnalysis() *models.SessionAnalysis {
	msgCosts := []float64{0.10, 0.35, 1.87, 0.42, 0.52, 0.55}
	messages := make([]models.MessageAnalysis, len(msgCosts))
	for i, c := range msgCosts {
		messages[i] = models.MessageAnalysis{
			Timestamp: goldenTime(11, 4+5*i, 0),
			Model:     "claude-opus-4-8",
			Usage:     models.TokenUsage{InputTokens: 100, OutputTokens: 200},
			Cost:      models.CostBreakdown{TotalCost: c},
		}
	}
	return &models.SessionAnalysis{
		SessionID:    "0a1b2c3d-4e5f-6789-abcd-ef0123456789",
		StartTime:    goldenTime(10, 0, 0),
		EndTime:      goldenTime(11, 30, 0),
		Duration:     models.Duration(90 * time.Minute),
		MessageCount: 37,
		Messages:     messages,
		TotalUsage: models.TokenUsage{
			InputTokens:              45678,
			OutputTokens:             23456,
			CacheCreationInputTokens: 250000,
			CacheReadInputTokens:     1500000,
			CacheCreation: &models.CacheCreation{
				Ephemeral5mInputTokens: 200000,
				Ephemeral1hInputTokens: 50000,
			},
		},
		TotalCost: models.CostBreakdown{
			InputCost:        0.30,
			OutputCost:       2.10,
			CacheWrite5mCost: 3.75,
			CacheWrite1hCost: 1.50,
			CacheReadCost:    2.25,
			TotalCost:        9.90,
			CacheSavings:     20.25,
		},
		CostByModel: map[string]models.CostBreakdown{
			"claude-opus-4-8":  {TotalCost: 7.90},
			"claude-haiku-4-5": {TotalCost: 0.42},
			"claude-sonnet-5":  {TotalCost: 1.58},
		},
		LastMessageUsage: models.TokenUsage{
			InputTokens:              12,
			CacheCreationInputTokens: 8000,
			CacheReadInputTokens:     371988,
			CacheCreation:            &models.CacheCreation{Ephemeral5mInputTokens: 8000},
		},
		LastMessageModel: "claude-opus-4-8",
		Agents: []models.AgentAnalysis{
			{
				AgentID:      "f6e5d4c3b2a1",
				MessageCount: 1,
				// Listed in the analyzer's order, by start; wrote 22s before
				// the clock, so it carries the live dot.
				StartTime:   goldenTime(10, 5, 0),
				EndTime:     goldenTime(11, 29, 50),
				TotalCost:   models.CostBreakdown{TotalCost: 1.58},
				CostByModel: map[string]models.CostBreakdown{"claude-sonnet-5": {TotalCost: 1.58}},
				Context:     &models.ContextUsage{Tokens: 380000, Window: 1000000, Percent: 38},
			},
			{
				AgentID:      "a1b2c3d4e5f6",
				MessageCount: 12,
				StartTime:    goldenTime(10, 20, 0),
				EndTime:      goldenTime(10, 40, 0),
				TotalCost:    models.CostBreakdown{TotalCost: 0.42},
				CostByModel:  map[string]models.CostBreakdown{"claude-haiku-4-5": {TotalCost: 0.42}},
				// Past ~compaction, so the color goldens show it red
				Context: &models.ContextUsage{Tokens: 156000, Window: 200000, Percent: 78},
			},
			// Workflow agents: exercise the dim group-header line. The first
			// has no reading yet, so its row leaves the column blank.
			{
				AgentID:      "w6e5d4c3b2a1",
				WorkflowID:   "wf_2e7850b6-b19",
				MessageCount: 1,
				StartTime:    goldenTime(10, 45, 0),
				EndTime:      goldenTime(11, 0, 0),
				TotalCost:    models.CostBreakdown{TotalCost: 0.55},
				CostByModel:  map[string]models.CostBreakdown{"claude-sonnet-5": {TotalCost: 0.55}},
			},
			{
				AgentID:      "w1a2b3c4d5e6",
				WorkflowID:   "wf_2e7850b6-b19",
				MessageCount: 21,
				StartTime:    goldenTime(10, 50, 0),
				EndTime:      goldenTime(11, 10, 0),
				TotalCost:    models.CostBreakdown{TotalCost: 1.10},
				CostByModel:  map[string]models.CostBreakdown{"claude-opus-4-8": {TotalCost: 1.10}},
				Context:      &models.ContextUsage{Tokens: 670000, Window: 1000000, Percent: 67},
			},
		},
		ParentCost:         models.CostBreakdown{TotalCost: 7.90},
		ParentCostByModel:  map[string]models.CostBreakdown{"claude-opus-4-8": {TotalCost: 7.90}},
		AgentsCost:         models.CostBreakdown{TotalCost: 3.65},
		HasAgents:          true,
		AgentCount:         4,
		ParentMessageCount: 24,
		AgentMessageCount:  35,
		Workflows: []models.WorkflowMeta{
			{RunID: "wf_2e7850b6-b19", Name: "audit-codebase", Status: "completed"},
		},
		WorkflowCount: 1,
		Insights: &models.MessageInsights{
			FirstMessage: &models.MessageSnapshot{
				Index: 1, Timestamp: goldenTime(10, 0, 5),
				Cost: 0.35, MainCostComponent: "cache_write_5m", MainCostValue: 0.30,
			},
			LastMessage: &models.MessageSnapshot{
				Index: 24, Timestamp: goldenTime(11, 29, 55),
				Cost: 0.52, MainCostComponent: "cache_read", MainCostValue: 0.31,
			},
			HighestCost: &models.MessageSnapshot{
				Index: 9, Timestamp: goldenTime(10, 42, 13),
				Cost: 1.87, MainCostComponent: "output", MainCostValue: 1.02,
			},
			CostTrend:     models.TrendIncreasing,
			RecentAvgCost: 0.55,
			TrendWindow:   12,
			AverageCost:   0.41,
			MessageCount:  24,
		},
	}
}

// goldenWatchView builds a watch Model at a fixed size, feeds it the fixture
// analysis, pins the time-dependent state, and returns View().
// First load records no change highlights (m.analysis is nil in detectChanges'
// guard), so the rendered frame is deterministic once lastUpdated and the
// clock are pinned. The clock sits 12s after the session's end, so the 10m
// rate window holds the last two chart messages.
func goldenWatchView(t *testing.T, noColor bool) string {
	t.Helper()
	// Height 60: tall enough that the whole analysis body fits the viewport,
	// so the golden pins every section
	return goldenWatchViewSized(t, noColor, 100, 60)
}

func goldenWatchViewSized(t *testing.T, noColor bool, width, height int) string {
	t.Helper()
	m := NewModel("/fixture/sess.jsonl", "0a1b2c3d-4e5f-6789-abcd-ef0123456789", noColor, "", true)
	m.project = "webapp"
	m.now = func() time.Time { return goldenTime(11, 30, 12) }
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = updated.(Model)
	updated, _ = m.Update(analysisMsg{analysis: goldenViewAnalysis()})
	m = updated.(Model)
	m.lastUpdated = goldenTime(11, 30, 0)
	return frameOf(m)
}

func TestGoldenWatchView(t *testing.T) {
	checkGolden(t, "watch_view", goldenWatchView(t, true))
}

func TestGoldenWatchViewColor(t *testing.T) {
	checkGolden(t, "watch_view_color", goldenWatchView(t, false))
}

// At a common terminal size the first screen is the top of the body — the
// total and the token rows — with the rest flagged in the footer rule.
func TestGoldenWatchView80x24(t *testing.T) {
	checkGolden(t, "watch_view_80x24", goldenWatchViewSized(t, true, 80, 24))
}

// A narrow terminal gets the whole body, fitted: each row sheds whole
// pieces from the right, never half a word, and the costs stay on screen.
func TestGoldenWatchView50(t *testing.T) {
	checkGolden(t, "watch_view_50x70", goldenWatchViewSized(t, true, 50, 70))
}

// A short terminal gets compact chrome: a plain header line, no help row,
// and the body's blank spacers dropped.
func TestGoldenWatchViewCompact(t *testing.T) {
	checkGolden(t, "watch_view_80x12", goldenWatchViewSized(t, true, 80, 12))
}

// The compact header on a session with no message yet, at the two narrowest
// widths: the status shortens to "no msgs" and nothing is cut.
func TestGoldenWatchCompactEmptyNarrow(t *testing.T) {
	for _, width := range []int{40, 41} {
		m := NewModel("/fixture/sess.jsonl", "0a1b2c3d-4e5f-6789-abcd-ef0123456789", true, "", true)
		m.project = "webapp"
		m.now = func() time.Time { return goldenTime(11, 30, 12) }
		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 14})
		updated, _ = updated.Update(analysisMsg{analysis: &models.SessionAnalysis{}})
		m = updated.(Model)
		m.lastUpdated = goldenTime(11, 30, 0)
		checkGolden(t, fmt.Sprintf("watch_empty_%dx14", width), frameOf(m))
	}
}

func goldenBreakdownMessages() []models.BreakdownMessage {
	return []models.BreakdownMessage{
		{
			Index: 1, Timestamp: goldenTime(10, 0, 5), Model: "claude-opus-4-8",
			Usage: models.TokenUsage{InputTokens: 1200, OutputTokens: 800, CacheCreationInputTokens: 4000, CacheReadInputTokens: 20000},
			Cost:  models.CostBreakdown{TotalCost: 0.35},
		},
		{
			Index: 2, Timestamp: goldenTime(10, 5, 0), Model: "claude-opus-4-8",
			Usage: models.TokenUsage{InputTokens: 400, OutputTokens: 1500, CacheCreationInputTokens: 2000, CacheReadInputTokens: 30000},
			Cost:  models.CostBreakdown{TotalCost: 0.52},
		},
		// Real 12-char agent ID: the marker truncates it to [Ag7h8i9j] and its
		// color comes from the FNV hash path (index 2 — pinned in styles_test),
		// not the ordinal palette walk.
		{
			Index: 3, AgentID: "g7h8i9j0k1l2", Timestamp: goldenTime(10, 6, 0), Model: "claude-haiku-4-5",
			Usage: models.TokenUsage{InputTokens: 5000, OutputTokens: 2500, CacheCreationInputTokens: 10000},
			Cost:  models.CostBreakdown{TotalCost: 0.04},
		},
		{
			Index: 4, AgentID: "g7h8i9j0k1l2", Timestamp: goldenTime(10, 8, 0), Model: "claude-haiku-4-5",
			Usage: models.TokenUsage{InputTokens: 300, OutputTokens: 900, CacheReadInputTokens: 15000},
			Cost:  models.CostBreakdown{TotalCost: 0.01},
		},
		{
			Index: 5, Timestamp: goldenTime(10, 42, 13), Model: "claude-opus-4-8",
			Usage: models.TokenUsage{InputTokens: 90, OutputTokens: 4200, CacheReadInputTokens: 50000},
			Cost:  models.CostBreakdown{TotalCost: 1.87},
		},
		// A workflow agent with a real-shaped ID ("a" + 16 hex): the marker
		// drops the constant "a", and the run tag follows it.
		{
			Index: 6, AgentID: "a9f8e7d6c5b4a3210", WorkflowID: "wf_2e7850b6-b19", Timestamp: goldenTime(11, 0, 0), Model: "claude-sonnet-5",
			Usage: models.TokenUsage{InputTokens: 40, OutputTokens: 1100, CacheCreationInputTokens: 3000, CacheReadInputTokens: 21000},
			Cost:  models.CostBreakdown{TotalCost: 0.07},
		},
		{
			Index: 7, Timestamp: goldenTime(11, 29, 55), Model: "claude-sonnet-5",
			Usage: models.TokenUsage{InputTokens: 12, OutputTokens: 600, CacheCreationInputTokens: 8000, CacheReadInputTokens: 371988},
			Cost:  models.CostBreakdown{TotalCost: 0.55},
		},
	}
}

// goldenBreakdownView builds a BreakdownModel at a fixed size, feeds it fixed
// rows (first load: no "new message" highlights), pins lastUpdated, and
// returns View(). skippedLines is set to exercise the footer warning segment.
func goldenBreakdownView(t *testing.T, noColor bool) string {
	t.Helper()
	return goldenBreakdownViewSized(t, noColor, 100, 40)
}

func goldenBreakdownViewSized(t *testing.T, noColor bool, width, height int) string {
	t.Helper()
	m := NewBreakdownModel("/fixture/sess.jsonl", "0a1b2c3d-4e5f-6789-abcd-ef0123456789", noColor, "", false)
	// 5s after the last fixture message, so the header reads "last msg 5s ago"
	m.now = func() time.Time { return goldenTime(11, 30, 0) }
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = updated.(BreakdownModel)
	updated, _ = m.Update(breakdownMsgsMsg{
		messages:  goldenBreakdownMessages(),
		workflows: []models.WorkflowMeta{{RunID: "wf_2e7850b6-b19", Name: "audit-codebase", Status: "completed"}},
		totalCost: 3.41,
		minCost:   0.01,
		maxCost:   1.87,
		insights: &models.MessageInsights{
			HighestCost: &models.MessageSnapshot{
				Index: 5, Timestamp: goldenTime(10, 42, 13),
				Cost: 1.87, MainCostComponent: "output", MainCostValue: 1.02,
			},
			CostTrend:     models.TrendIncreasing,
			RecentAvgCost: 0.81,
			TrendWindow:   3,
			AverageCost:   0.49,
			MessageCount:  7,
		},
		skippedLines: 3,
		// The fixture rows include agent messages (indices 3-4 and 6), so the breakdown
		// merges parent+agent — exercises the "main conversation + agents" label that
		// distinguishes this surface from show/watch's parent-only insights.
		hasAgents: true,
	})
	m = updated.(BreakdownModel)
	m.lastUpdated = goldenTime(11, 30, 0)
	return frameOf(m)
}

func TestGoldenBreakdownView(t *testing.T) {
	checkGolden(t, "breakdown_view", goldenBreakdownView(t, true))
}

// Half a 160-column screen: the row is too wide for every column, so IN gives
// way whole rather than the right edge cutting C_RD in half.
func TestGoldenBreakdownView79(t *testing.T) {
	checkGolden(t, "breakdown_view_79", goldenBreakdownViewSized(t, true, 79, 24))
}

func TestGoldenBreakdownViewColor(t *testing.T) {
	checkGolden(t, "breakdown_view_color", goldenBreakdownView(t, false))
}

// useASCII switches to the ASCII glyph set for one test.
func useASCII(t *testing.T) {
	t.Helper()
	styles.SetASCII(true)
	t.Cleanup(func() { styles.SetASCII(false) })
}

// --ascii --no-color: plain ASCII frames, symbols and chart, no escapes.
func TestGoldenWatchViewASCII(t *testing.T) {
	useASCII(t)
	checkGolden(t, "watch_view_ascii", goldenWatchView(t, true))
}

// --ascii alone keeps color; the colored branches must draw ASCII glyphs too.
func TestGoldenWatchViewASCIIColor(t *testing.T) {
	useASCII(t)
	checkGolden(t, "watch_view_ascii_color", goldenWatchView(t, false))
}

func TestGoldenBreakdownViewASCII(t *testing.T) {
	useASCII(t)
	checkGolden(t, "breakdown_view_ascii", goldenBreakdownView(t, true))
}

// breakdown's header panel says "loading..." while the first load is in
// flight, and a reload keeps the last status up.
func TestGoldenBreakdownHeaderLoading(t *testing.T) {
	header := func(m tea.Model) string {
		return strings.Join(strings.Split(frameOf(m), "\n")[:3], "\n")
	}
	m := NewBreakdownModel("/fixture/sess.jsonl", "0a1b2c3d-4e5f-6789-abcd-ef0123456789", true, "", false)
	m.now = func() time.Time { return goldenTime(11, 30, 0) }
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	checkGolden(t, "breakdown_header_first_load", header(model))

	model, _ = model.Update(breakdownMsgsMsg{messages: goldenBreakdownMessages(), insights: &models.MessageInsights{}})
	model = press(t, model, "r")
	checkGolden(t, "breakdown_header_reload", header(model))
}
