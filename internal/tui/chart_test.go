package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bardisty/ficha/internal/models"
)

func chartMsg(ts time.Time, agentID string, cost float64) models.MessageAnalysis {
	return models.MessageAnalysis{
		Timestamp: ts,
		AgentID:   agentID,
		Cost:      models.CostBreakdown{TotalCost: cost},
	}
}

// Agent messages arrive after the parent block in the analyzer's list, but
// their spend happened mid-session — the chart must plot it where it happened.
func TestMergedCostHistory_InterleavesAgentByTimestamp(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []models.MessageAnalysis{
		chartMsg(base, "", 0.10),
		chartMsg(base.Add(20*time.Minute), "", 0.30),
		chartMsg(base.Add(10*time.Minute), "x", 0.20), // agent block, mid-parent timestamp
	}

	got := mergedCostHistory(msgs, maxCostHistorySize)

	want := []float64{0.10, 0.20, 0.30}
	if len(got) != len(want) {
		t.Fatalf("history length: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("history[%d]: got %f, want %f", i, got[i], want[i])
		}
	}
}

// Equal (or zero) timestamps keep the analyzer's order — parent block, then
// agent blocks — so the chart is deterministic across reloads.
func TestMergedCostHistory_StableForEqualTimestamps(t *testing.T) {
	var msgs []models.MessageAnalysis
	var want []float64
	for i := 0; i < 10; i++ {
		msgs = append(msgs, chartMsg(time.Time{}, "", float64(i)))
		want = append(want, float64(i))
	}
	for i := 10; i < 20; i++ {
		msgs = append(msgs, chartMsg(time.Time{}, "agent-a", float64(i)))
		want = append(want, float64(i))
	}

	for run := 0; run < 2; run++ {
		got := mergedCostHistory(msgs, maxCostHistorySize)
		if len(got) != len(want) {
			t.Fatalf("run %d: history length: got %d, want %d", run, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("run %d: history[%d]: got %f, want %f", run, i, got[i], want[i])
			}
		}
	}
}

func TestMergedCostHistory_CapsAtMax(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	n := maxCostHistorySize + 5
	msgs := make([]models.MessageAnalysis, n)
	for i := 0; i < n; i++ {
		msgs[i] = chartMsg(base.Add(time.Duration(i)*time.Second), "", float64(i))
	}

	got := mergedCostHistory(msgs, maxCostHistorySize)

	if len(got) != maxCostHistorySize {
		t.Fatalf("history length: got %d, want %d", len(got), maxCostHistorySize)
	}
	// Oldest entries fall off the front
	if got[0] != 5.0 {
		t.Errorf("history[0]: got %f, want 5.0", got[0])
	}
	if got[len(got)-1] != float64(n-1) {
		t.Errorf("history[last]: got %f, want %f", got[len(got)-1], float64(n-1))
	}
}

func TestMergedCostHistory_EmptyInput(t *testing.T) {
	if got := mergedCostHistory(nil, maxCostHistorySize); len(got) != 0 {
		t.Errorf("history length: got %d, want 0", len(got))
	}
}

// The analyzer's Messages order (parent block then agent blocks) is its
// documented contract; the chart must sort a copy, never the analysis itself.
func TestUpdateCostChart_DoesNotReorderAnalysisMessages(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	m := NewModel("/test/path", "test-session", false, true, "", false)
	m.analysis = &models.SessionAnalysis{
		Messages: []models.MessageAnalysis{
			chartMsg(base, "", 0.10),
			chartMsg(base.Add(20*time.Minute), "", 0.30),
			chartMsg(base.Add(10*time.Minute), "x", 0.20),
		},
	}

	m.updateCostChart()

	wantAgentIDs := []string{"", "", "x"}
	for i, msg := range m.analysis.Messages {
		if msg.AgentID != wantAgentIDs[i] {
			t.Errorf("analysis.Messages[%d].AgentID: got %q, want %q — updateCostChart reordered the analysis",
				i, msg.AgentID, wantAgentIDs[i])
		}
	}
	if len(m.costHistory) != 3 || m.costHistory[1] != 0.20 {
		t.Errorf("costHistory: got %v, want agent cost 0.20 interleaved at index 1", m.costHistory)
	}
}

// An early expensive message must not pin the visible window's scale: once it
// rotates out of the sparkline's ring buffer, the drawn bars must scale to the
// visible max, and the min/max/count label must describe the visible window —
// not a peak that is off-screen.
func TestUpdateCostChart_ScalesToVisibleWindow(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	const n = 210
	msgs := make([]models.MessageAnalysis, n)
	for i := 0; i < n; i++ {
		cost := 0.01 + float64(i%50)*0.001 // all small, well under the early peak
		if i == 10 {
			cost = 5.00 // early expensive message, far outside the last 68
		}
		msgs[i] = chartMsg(base.Add(time.Duration(i)*time.Second), "", cost)
	}

	m := NewModel("/test/path", "sess", false, true, "", false)
	m.width = 100 // getChartWidth -> 68
	m.analysis = &models.SessionAnalysis{Messages: msgs}
	m.updateCostChart()

	w := m.getChartWidth()
	if w != 68 {
		t.Fatalf("getChartWidth: got %d, want 68", w)
	}
	if len(m.costHistory) != n {
		t.Fatalf("costHistory length: got %d, want %d", len(m.costHistory), n)
	}
	visible := m.visibleCostHistory()
	if len(visible) != w {
		t.Fatalf("visible window: got %d, want %d", len(visible), w)
	}
	// The $5 peak (index 10) is outside the last 68, so it must not set the scale.
	if mv := m.costChart.MaxValue(); mv >= 5.0 {
		t.Errorf("chart scale MaxValue: got %f, want < 5.0 — early peak still ratchets the visible window", mv)
	}
	label := m.renderCostChart()
	if !strings.Contains(label, "(last 68 of 210 msgs)") {
		t.Errorf("label: %q\nwant substring %q", label, "(last 68 of 210 msgs)")
	}
	if strings.Contains(label, "$5.0000") {
		t.Errorf("label advertises the off-screen peak: %q", label)
	}
}

// Resizing the terminal re-windows the chart: the drawn window, its scale, and
// the shown/total count must all track the new width.
func TestRenderCostChart_ResizeChangesWindow(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	const n = 210
	msgs := make([]models.MessageAnalysis, n)
	for i := 0; i < n; i++ {
		msgs[i] = chartMsg(base.Add(time.Duration(i)*time.Second), "", 0.01+float64(i%40)*0.001)
	}

	m := NewModel("/test/path", "sess", false, true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(Model)
	updated, _ = m.Update(analysisMsg{analysis: &models.SessionAnalysis{Messages: msgs}})
	m = updated.(Model)

	if !strings.Contains(m.renderCostChart(), "(last 68 of 210 msgs)") {
		t.Errorf("width 100 label: %q\nwant substring %q", m.renderCostChart(), "(last 68 of 210 msgs)")
	}

	// Narrow the terminal: getChartWidth = 50-8 = 42, so the window shrinks.
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 40})
	m = updated.(Model)
	if w := m.getChartWidth(); w != 42 {
		t.Fatalf("getChartWidth after resize: got %d, want 42", w)
	}
	if len(m.visibleCostHistory()) != 42 {
		t.Fatalf("visible window after resize: got %d, want 42", len(m.visibleCostHistory()))
	}
	if !strings.Contains(m.renderCostChart(), "(last 42 of 210 msgs)") {
		t.Errorf("width 50 label: %q\nwant substring %q", m.renderCostChart(), "(last 42 of 210 msgs)")
	}
}

// A message with a missing timestamp must plot next to its file neighbors, not
// jump to chart position 0 the way the zero time.Time would under a naive sort.
func TestMergedCostHistory_ZeroTimestampKeptWithNeighbors(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// Agent block (zero-ts) appended after the parent block, as the analyzer emits.
	trailing := mergedCostHistory([]models.MessageAnalysis{
		chartMsg(base, "", 0.10),
		chartMsg(base.Add(20*time.Minute), "", 0.30),
		chartMsg(time.Time{}, "x", 2.00), // $2 spike, no timestamp
	}, maxCostHistorySize)
	if want := []float64{0.10, 0.30, 2.00}; !floatsEqual(trailing, want) {
		t.Errorf("trailing zero-ts: got %v, want %v (spike must not sort to position 0)", trailing, want)
	}

	// A zero-ts message mid-block keeps its file position between its neighbors.
	mid := mergedCostHistory([]models.MessageAnalysis{
		chartMsg(base, "", 0.10),
		chartMsg(time.Time{}, "", 2.00),
		chartMsg(base.Add(20*time.Minute), "", 0.30),
	}, maxCostHistorySize)
	if want := []float64{0.10, 2.00, 0.30}; !floatsEqual(mid, want) {
		t.Errorf("mid zero-ts: got %v, want %v", mid, want)
	}
}

// The newest spend must survive tail truncation even when its timestamp is
// missing: inheriting the prior message's time keeps it at the end, not the
// front where the zero time would land it and be dropped first.
func TestMergedCostHistory_LateZeroTimestampSurvivesTruncation(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := make([]models.MessageAnalysis, 10)
	for i := 0; i < 9; i++ {
		msgs[i] = chartMsg(base.Add(time.Duration(i)*time.Minute), "", float64(i))
	}
	msgs[9] = chartMsg(time.Time{}, "x", 9.9) // newest spend, missing timestamp

	got := mergedCostHistory(msgs, 5) // keep only the newest 5

	if len(got) != 5 {
		t.Fatalf("history length: got %d, want 5", len(got))
	}
	if got[len(got)-1] != 9.9 {
		t.Errorf("newest zero-ts spend dropped: got %v, want it last (9.9)", got)
	}
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

	m := NewModel(sessionPath, sessionID, false, true, "", false)
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

	// And the chart folds the agent's spend in, chronologically mid-list:
	// 1M input tokens at $3/M dwarfs both parent messages.
	m.analysis = analysis
	m.updateCostChart()
	if len(m.costHistory) != 3 {
		t.Fatalf("costHistory length: got %d, want 3", len(m.costHistory))
	}
	if m.costHistory[1] < 2.9 {
		t.Errorf("costHistory[1]: got %f, want the agent's ~$3.00 spend interleaved mid-chart", m.costHistory[1])
	}
}

// The sparkline scales to the visible window's peak, not its built-in $1
// floor, so a session of sub-dollar messages still uses the full height.
func TestCostChartScalesToVisiblePeak(t *testing.T) {
	m := NewModel("/test/path", "s", false, true, "", false)
	m.costHistory = []float64{0.02, 0.22, 0.05}
	m.rebuildCostChart()
	if got := m.costChart.MaxValue(); got != 0.22 {
		t.Errorf("chart max = %v, want the visible peak 0.22", got)
	}
	lines := strings.Split(strings.TrimRight(m.costChart.View(), "\n"), "\n")
	if strings.TrimSpace(lines[0]) == "" {
		t.Errorf("peak doesn't reach the top row:\n%s", m.costChart.View())
	}

	// An all-zero window keeps the default scale rather than dividing by 0.
	m.costHistory = []float64{0, 0}
	m.rebuildCostChart()
	if got := m.costChart.MaxValue(); got <= 0 {
		t.Errorf("zero window: chart max = %v, want > 0", got)
	}
}
