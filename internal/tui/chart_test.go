package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
	analysis, ok := msg.(analysisMsg)
	if !ok {
		t.Fatalf("loadAnalysis returned %T, want analysisMsg", msg)
	}

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
