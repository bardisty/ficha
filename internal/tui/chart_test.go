package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

func chartMsg(ts time.Time, agentID string, cost float64) models.MessageAnalysis {
	return models.MessageAnalysis{
		Timestamp: ts,
		AgentID:   agentID,
		Cost:      models.CostBreakdown{TotalCost: cost},
	}
}

// chartBase is a clock minute the chart tests count from.
var chartBase = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

// at is chartBase plus minutes and seconds.
func at(min, sec int) time.Time {
	return chartBase.Add(time.Duration(min)*time.Minute + time.Duration(sec)*time.Second)
}

func floatsEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Each bucket sums the minute's spend, parent and agents alike. Agent
// messages come after the parent block in the analyzer's list, and still
// land in the minute they happened.
func TestCostBucketsSumEachMinute(t *testing.T) {
	msgs := []models.MessageAnalysis{
		chartMsg(at(0, 5), "", 0.10),
		chartMsg(at(2, 50), "", 0.30),
		chartMsg(at(0, 40), "x", 0.20),
		chartMsg(at(1, 59), "x", 0.25),
	}
	got := costBuckets(msgs, at(2, 55), 68)
	if want := []float64{0.30000000000000004, 0.25, 0.30}; !floatsEqual(got, want) {
		t.Errorf("buckets = %v, want %v", got, want)
	}
}

// A pause shows as empty minutes, not as nothing.
func TestCostBucketsShowIdleMinutesAsZeros(t *testing.T) {
	msgs := []models.MessageAnalysis{
		chartMsg(at(0, 0), "", 1),
		chartMsg(at(4, 30), "", 2),
	}
	got := costBuckets(msgs, at(4, 30), 68)
	if want := []float64{1, 0, 0, 0, 2}; !floatsEqual(got, want) {
		t.Errorf("buckets = %v, want %v", got, want)
	}
}

// The newest bucket is the minute now is in, so the chart moves on while
// the session is idle, and minutes older than the chart's width drop off.
func TestCostBucketsEndAtNow(t *testing.T) {
	msgs := []models.MessageAnalysis{
		chartMsg(at(0, 0), "", 5),
		chartMsg(at(3, 0), "", 1),
	}
	got := costBuckets(msgs, at(6, 59), 4)
	if want := []float64{1, 0, 0, 0}; !floatsEqual(got, want) {
		t.Errorf("buckets = %v, want %v", got, want)
	}
}

// One message draws one minute, and a message stamped past now, by a clock
// running ahead, counts in the newest minute rather than vanishing.
func TestCostBucketsSingleAndFutureMessages(t *testing.T) {
	one := []models.MessageAnalysis{chartMsg(at(0, 10), "", 0.5)}
	if got := costBuckets(one, at(0, 20), 68); !floatsEqual(got, []float64{0.5}) {
		t.Errorf("one message: buckets = %v, want [0.5]", got)
	}
	if got := costBuckets(one, at(0, 0).Add(-time.Minute), 68); !floatsEqual(got, []float64{0.5}) {
		t.Errorf("message after now: buckets = %v, want [0.5]", got)
	}
	ahead := append(one, chartMsg(at(3, 0), "", 0.25))
	if got := costBuckets(ahead, at(1, 0), 68); !floatsEqual(got, []float64{0.5, 0.25}) {
		t.Errorf("message after now: buckets = %v, want [0.5 0.25]", got)
	}
}

// Messages without a timestamp can't be placed in a minute; they're
// skipped, as the footer's rate skips them, and with none stamped there is
// no chart.
func TestCostBucketsSkipMessagesWithoutTimestamp(t *testing.T) {
	msgs := []models.MessageAnalysis{
		chartMsg(time.Time{}, "", 9),
		chartMsg(at(0, 10), "", 1),
		chartMsg(time.Time{}, "x", 9),
		chartMsg(at(1, 10), "", 2),
	}
	if got := costBuckets(msgs, at(1, 30), 68); !floatsEqual(got, []float64{1, 2}) {
		t.Errorf("buckets = %v, want [1 2]", got)
	}
	if got := costBuckets(msgs[:1], at(1, 30), 68); got != nil {
		t.Errorf("no stamped message: buckets = %v, want none", got)
	}
}

// The newest 10 buckets hold the footer's 10-minute rate's spend, short
// only by what fell in the part of the window before the oldest bucket's
// minute: at most one minute of it.
func TestCostBucketsAgreeWithRollingRate(t *testing.T) {
	var msgs []models.MessageAnalysis
	for s := 0; s < 30*60; s += 7 {
		msgs = append(msgs, chartMsg(chartBase.Add(time.Duration(s)*time.Second), "", 0.01))
	}
	now := at(29, 50)
	rate, ok := rollingRate(msgs, now, rateWindow)
	if !ok {
		t.Fatal("no rate")
	}
	buckets := costBuckets(msgs, now, 68)
	var last10 float64
	for _, c := range buckets[len(buckets)-10:] {
		last10 += c
	}
	inRate := rate / 6
	if last10 > inRate+1e-9 || inRate-last10 > maxOf(buckets)+1e-9 {
		t.Errorf("newest 10 minutes sum to %v, the rate's window to %v", last10, inRate)
	}
}

// The analyzer's Messages order (parent block then agent blocks) is its
// documented contract; bucketing reads it and never reorders it.
func TestRebuildCostChartDoesNotReorderAnalysisMessages(t *testing.T) {
	m := NewModel("/test/path", "test-session", true, "", false)
	m.now = func() time.Time { return at(20, 30) }
	m.analysis = &models.SessionAnalysis{
		Messages: []models.MessageAnalysis{
			chartMsg(at(0, 0), "", 0.10),
			chartMsg(at(20, 0), "", 0.30),
			chartMsg(at(10, 0), "x", 0.20),
		},
	}
	m.rebuildCostChart()
	for i, want := range []string{"", "", "x"} {
		if got := m.analysis.Messages[i].AgentID; got != want {
			t.Errorf("analysis.Messages[%d].AgentID = %q, want %q", i, got, want)
		}
	}
}

// longAnalysis has three hours of cheap minutes and one expensive minute
// early on, far outside a 68-minute chart.
func longAnalysis() *models.SessionAnalysis {
	var msgs []models.MessageAnalysis
	for i := 0; i < 180; i++ {
		cost := 0.01 + float64(i%50)*0.001
		if i == 10 {
			cost = 5.00
		}
		msgs = append(msgs, chartMsg(at(i, 0), "", cost))
	}
	return &models.SessionAnalysis{Messages: msgs}
}

// An early expensive minute must not pin the scale once it's off the chart:
// the bars and the label's max describe the minutes drawn.
func TestCostChartScalesToDrawnMinutes(t *testing.T) {
	m := NewModel("/test/path", "sess", true, "", false)
	m.now = func() time.Time { return at(179, 30) }
	m.width = 80 // getChartWidth -> 68
	m.analysis = longAnalysis()
	m.rebuildCostChart()

	if len(m.costHistory) != 68 {
		t.Fatalf("costHistory length: got %d, want 68", len(m.costHistory))
	}
	if mv := m.costChart.MaxValue(); mv >= 5.0 {
		t.Errorf("chart scale = %f, want under the off-chart $5 minute", mv)
	}
	label := m.renderCostChart()
	for _, want := range []string{"$ per minute, last 68m", "max $0.0590", "parent and agents"} {
		if !strings.Contains(label, want) {
			t.Errorf("label lacks %q:\n%s", want, label)
		}
	}
	if strings.Contains(label, "$5.00") {
		t.Errorf("label advertises the off-chart peak:\n%s", label)
	}
}

// Resizing re-buckets at the new width: still one minute a column, so a
// narrower chart spans fewer minutes, and its label drops pieces whole.
func TestCostChartResizeChangesSpan(t *testing.T) {
	m := NewModel("/test/path", "sess", true, "", false)
	m.now = func() time.Time { return at(179, 30) }
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	updated, _ = updated.Update(analysisMsg{analysis: longAnalysis()})
	m = updated.(Model)
	if !strings.Contains(m.renderCostChart(), "last 68m") {
		t.Errorf("width 80 label:\n%s", m.renderCostChart())
	}

	// A wider terminal widens the panel, and the chart fills it, up to the
	// panel's cap.
	for width, minutes := range map[int]int{100: 88, 200: 110} {
		updated, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		m = updated.(Model)
		if len(m.costHistory) != minutes {
			t.Fatalf("buckets at %d: got %d, want %d", width, len(m.costHistory), minutes)
		}
		if want := fmt.Sprintf("last %dm", minutes); !strings.Contains(m.renderCostChart(), want) {
			t.Errorf("width %d label lacks %q:\n%s", width, want, m.renderCostChart())
		}
		rule := "  " + render.SectionHeader("COST TREND", panelWidthFor(width), true)
		for _, line := range strings.Split(strings.TrimRight(m.costChart.View(), "\n"), "\n") {
			if 4+lipgloss.Width(line) > lipgloss.Width(rule)-2 {
				t.Errorf("width %d: chart line runs past the rule's margin: %q", width, line)
			}
		}
	}

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 40})
	m = updated.(Model)
	if len(m.costHistory) != 42 {
		t.Fatalf("buckets after resize: got %d, want 42", len(m.costHistory))
	}
	label := m.renderCostChart()
	if !strings.Contains(label, "    last 42m · max $0.0590 · with agents\n") {
		t.Errorf("width 50 label:\n%s", label)
	}
	for _, line := range strings.Split(strings.TrimRight(label, "\n"), "\n") {
		if len([]rune(line)) > 50 {
			t.Errorf("line wider than 50 columns: %q", line)
		}
	}
}

// Each minute gets its own cell, the newest in the chart's last: a full
// chart fills every cell, and a short one ends at the right edge.
func TestCostChartDrawsOneMinutePerCell(t *testing.T) {
	for _, minutes := range []int{68, 5} {
		var msgs []models.MessageAnalysis
		for i := range minutes {
			msgs = append(msgs, chartMsg(at(i, 0), "", 1))
		}
		m := NewModel("/test/path", "sess", true, "", false)
		m.now = func() time.Time { return at(minutes-1, 30) }
		m.width = 100
		m.analysis = &models.SessionAnalysis{Messages: msgs}
		m.rebuildCostChart()

		top := []rune(strings.Split(m.costChart.View(), "\n")[0])
		var first, last, drawn int = -1, -1, 0
		for i, r := range top {
			if r != ' ' {
				if first < 0 {
					first = i
				}
				last, drawn = i, drawn+1
			}
		}
		if drawn != minutes || last != m.getChartWidth()-1 || first != m.getChartWidth()-minutes {
			t.Errorf("%d minutes: drawn in %d cells, %d to %d of %d:\n%s",
				minutes, drawn, first, last, m.getChartWidth(), m.costChart.View())
		}
	}
}

// --ascii draws columns, and a minute that spent nothing still marks the
// floor, so an idle stretch reads as time rather than margin.
func TestCostChartASCIIFloorsEmptyMinutes(t *testing.T) {
	useASCII(t)
	m := NewModel("/test/path", "sess", true, "", false)
	m.now = func() time.Time { return at(5, 30) }
	m.width = 100
	m.analysis = &models.SessionAnalysis{Messages: []models.MessageAnalysis{
		chartMsg(at(0, 0), "", 1), chartMsg(at(5, 0), "", 1),
	}}
	m.rebuildCostChart()
	rows := strings.Split(strings.TrimRight(m.costChart.View(), "\n"), "\n")
	bottom := styles.ASCIIChart(rows[len(rows)-1])
	if got, want := strings.TrimSpace(bottom), "#____#"; got != want {
		t.Errorf("bottom row = %q, want %q", got, want)
	}
}

// The chart's columns are clock minutes: a tick inside the minute leaves
// them alone, and the first tick of the next minute shifts them and opens
// an empty newest column, with no reload.
func TestCostChartRollsOverOnTheMinute(t *testing.T) {
	clock := at(5, 10)
	m := NewModel("/test/path", "sess", true, "", false)
	m.now = func() time.Time { return clock }
	m = load(t, sized(t, m, 100, 60), &models.SessionAnalysis{
		MessageCount: 2,
		Messages:     []models.MessageAnalysis{chartMsg(at(0, 0), "", 1), chartMsg(at(5, 5), "", 2)},
		TotalCost:    models.CostBreakdown{TotalCost: 3},
	})
	if want := []float64{1, 0, 0, 0, 0, 2}; !floatsEqual(m.costHistory, want) {
		t.Fatalf("buckets = %v, want %v", m.costHistory, want)
	}

	clock = at(5, 59)
	updated, _ := m.Update(clockMsg{gen: m.clockGen})
	m = updated.(Model)
	if len(m.costHistory) != 6 {
		t.Errorf("a tick within the minute changed the chart: %v", m.costHistory)
	}

	clock = at(6, 0)
	updated, _ = m.Update(clockMsg{gen: m.clockGen})
	m = updated.(Model)
	if want := []float64{1, 0, 0, 0, 0, 2, 0}; !floatsEqual(m.costHistory, want) {
		t.Errorf("after the minute rolled: buckets = %v, want %v", m.costHistory, want)
	}
	if frame := frameOf(m); !strings.Contains(frame, "last 7m") {
		t.Errorf("the drawn label didn't move on:\n%s", frame)
	}
}

// A one-message session draws its minute; the chart needs a timestamp, not
// a second message.
func TestCostChartDrawsOneMessage(t *testing.T) {
	m := NewModel("/test/path", "sess", true, "", false)
	m.now = func() time.Time { return at(0, 30) }
	m = load(t, sized(t, m, 100, 60), &models.SessionAnalysis{
		MessageCount: 1,
		Messages:     []models.MessageAnalysis{chartMsg(at(0, 10), "", 0.25)},
		TotalCost:    models.CostBreakdown{TotalCost: 0.25},
	})
	body := m.renderAnalysis()
	if !strings.Contains(body, "COST TREND") || !strings.Contains(body, "last 1m") {
		t.Errorf("one message drew no chart:\n%s", body)
	}
}

// Regression pin for the chart's data source: the goldens feed analysisMsg
// directly, so only this test notices if loadAnalysis stops requesting
// AllMessages — which would silently drop agent spend from the chart again.
func TestLoadAnalysisIncludesAgentMessages(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	parentContent := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":50}}}
{"type":"assistant","timestamp":"2024-01-15T10:20:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":200,"output_tokens":100}}}`
	if err := os.WriteFile(sessionPath, []byte(parentContent), 0644); err != nil {
		t.Fatal(err)
	}

	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentContent := `{"type":"assistant","timestamp":"2024-01-15T10:10:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":1000000,"output_tokens":0}}}`
	if err := os.WriteFile(filepath.Join(subagentsDir, "agent-x.jsonl"), []byte(agentContent), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewModel(sessionPath, sessionID, true, "", false)
	msg := m.loadAnalysis()
	loaded, ok := msg.(analysisMsg)
	if !ok {
		t.Fatalf("loadAnalysis returned %T, want analysisMsg", msg)
	}
	analysis := loaded.analysis

	agentRows := 0
	for _, row := range analysis.Messages {
		if row.AgentID == "x" {
			agentRows++
		}
	}
	if agentRows != 1 {
		t.Fatalf("agent rows in loaded Messages: got %d, want 1 — loadAnalysis must request AllMessages", agentRows)
	}

	// And the chart counts the agent's spend in the minute it happened:
	// 1M input tokens at $3/M dwarfs both parent messages.
	m.analysis = analysis
	m.now = func() time.Time { return time.Date(2024, 1, 15, 10, 20, 30, 0, time.UTC) }
	m.rebuildCostChart()
	if len(m.costHistory) != 21 {
		t.Fatalf("costHistory length: got %d, want 21 minutes", len(m.costHistory))
	}
	if m.costHistory[10] < 2.9 {
		t.Errorf("costHistory[10]: got %f, want the agent's ~$3.00 in its own minute", m.costHistory[10])
	}
}

// The sparkline scales to the drawn window's peak, not its built-in $1
// floor, so a session of sub-dollar minutes still uses the full height.
func TestCostChartScalesToVisiblePeak(t *testing.T) {
	m := NewModel("/test/path", "s", true, "", false)
	m.now = func() time.Time { return at(2, 30) }
	m.analysis = &models.SessionAnalysis{Messages: []models.MessageAnalysis{
		chartMsg(at(0, 0), "", 0.02), chartMsg(at(1, 0), "", 0.22), chartMsg(at(2, 0), "", 0.05),
	}}
	m.rebuildCostChart()
	if got := m.costChart.MaxValue(); got != 0.22 {
		t.Errorf("chart max = %v, want the visible peak 0.22", got)
	}
	lines := strings.Split(strings.TrimRight(m.costChart.View(), "\n"), "\n")
	if strings.TrimSpace(lines[0]) == "" {
		t.Errorf("peak doesn't reach the top row:\n%s", m.costChart.View())
	}

	// An all-zero window keeps the default scale rather than dividing by 0.
	m.analysis.Messages = []models.MessageAnalysis{chartMsg(at(0, 0), "", 0), chartMsg(at(1, 0), "", 0)}
	m.rebuildCostChart()
	if got := m.costChart.MaxValue(); got <= 0 {
		t.Errorf("zero window: chart max = %v, want > 0", got)
	}
}
