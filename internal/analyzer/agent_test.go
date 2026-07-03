package analyzer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Direct tests for AnalyzeAgent. AnalyzeSession's agent roll-up exercises it
// transitively, but not its own contract: empty files, malformed lines, ID
// extraction, and time range.

func writeAgentFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write agent file: %v", err)
	}
	return path
}

func TestAnalyzeAgent_Normal(t *testing.T) {
	content := `{"type":"assistant","timestamp":"2026-01-15T10:00:00Z","requestId":"req_1","message":{"id":"msg_1","model":"claude-haiku-4-5","usage":{"input_tokens":1000,"output_tokens":500,"cache_creation_input_tokens":2000,"cache_read_input_tokens":8000}}}
{"type":"user","timestamp":"2026-01-15T10:01:00Z"}
{"type":"assistant","timestamp":"2026-01-15T10:05:00Z","requestId":"req_2","message":{"id":"msg_2","model":"claude-haiku-4-5","usage":{"input_tokens":300,"output_tokens":700,"cache_creation_input_tokens":0,"cache_read_input_tokens":10000}}}
`
	path := writeAgentFile(t, "agent-norm123.jsonl", content)

	analysis, err := AnalyzeAgent(path, false)
	if err != nil {
		t.Fatalf("AnalyzeAgent returned error: %v", err)
	}

	if analysis.AgentID != "norm123" {
		t.Errorf("AgentID: got %q, want %q", analysis.AgentID, "norm123")
	}
	if analysis.FullPath != path {
		t.Errorf("FullPath: got %q, want %q", analysis.FullPath, path)
	}
	if analysis.MessageCount != 2 {
		t.Errorf("MessageCount: got %d, want 2", analysis.MessageCount)
	}
	if analysis.TotalUsage.InputTokens != 1300 {
		t.Errorf("InputTokens: got %d, want 1300", analysis.TotalUsage.InputTokens)
	}
	if analysis.TotalUsage.OutputTokens != 1200 {
		t.Errorf("OutputTokens: got %d, want 1200", analysis.TotalUsage.OutputTokens)
	}
	if analysis.TotalUsage.CacheReadInputTokens != 18000 {
		t.Errorf("CacheReadInputTokens: got %d, want 18000", analysis.TotalUsage.CacheReadInputTokens)
	}
	if analysis.TotalCost.TotalCost <= 0 {
		t.Errorf("TotalCost should be positive, got %f", analysis.TotalCost.TotalCost)
	}
	// Haiku 4.5: in $1/M, out $5/M, cache write 1.25x in, cache read 0.1x in
	wantCost := 1300.0/1e6*1.00 + 1200.0/1e6*5.00 + 2000.0/1e6*1.00*1.25 + 18000.0/1e6*1.00*0.1
	if !almostEqual(analysis.TotalCost.TotalCost, wantCost, 1e-9) {
		t.Errorf("TotalCost: got %f, want %f", analysis.TotalCost.TotalCost, wantCost)
	}
	if len(analysis.CostByModel) != 1 {
		t.Errorf("CostByModel: got %d models, want 1", len(analysis.CostByModel))
	}
	if _, ok := analysis.CostByModel["claude-haiku-4-5"]; !ok {
		t.Error("CostByModel missing claude-haiku-4-5")
	}
	wantStart := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 1, 15, 10, 5, 0, 0, time.UTC)
	if !analysis.StartTime.Equal(wantStart) {
		t.Errorf("StartTime: got %v, want %v", analysis.StartTime, wantStart)
	}
	if !analysis.EndTime.Equal(wantEnd) {
		t.Errorf("EndTime: got %v, want %v", analysis.EndTime, wantEnd)
	}
	if analysis.Duration.Duration() != 5*time.Minute {
		t.Errorf("Duration: got %v, want 5m", analysis.Duration.Duration())
	}
	if analysis.SkippedLines != 0 {
		t.Errorf("SkippedLines: got %d, want 0", analysis.SkippedLines)
	}
}

func TestAnalyzeAgent_EmptyFile(t *testing.T) {
	path := writeAgentFile(t, "agent-empty.jsonl", "")

	analysis, err := AnalyzeAgent(path, false)
	if err != nil {
		t.Fatalf("AnalyzeAgent on empty file returned error: %v", err)
	}

	if analysis.AgentID != "empty" {
		t.Errorf("AgentID: got %q, want %q", analysis.AgentID, "empty")
	}
	if analysis.MessageCount != 0 {
		t.Errorf("MessageCount: got %d, want 0", analysis.MessageCount)
	}
	if analysis.TotalCost.TotalCost != 0 {
		t.Errorf("TotalCost: got %f, want 0", analysis.TotalCost.TotalCost)
	}
	if !analysis.StartTime.IsZero() || !analysis.EndTime.IsZero() {
		t.Errorf("time range should be zero for empty file, got %v–%v", analysis.StartTime, analysis.EndTime)
	}
	if analysis.CostByModel == nil {
		t.Error("CostByModel should be initialized (non-nil) for empty file")
	}
}

func TestAnalyzeAgent_MalformedLines(t *testing.T) {
	content := `this is not json
{"type":"assistant","timestamp":"2026-01-15T12:00:00Z","requestId":"req_1","message":{"id":"msg_1","model":"claude-haiku-4-5","usage":{"input_tokens":100,"output_tokens":50}}}
{"broken":
`
	path := writeAgentFile(t, "agent-mal.jsonl", content)

	analysis, err := AnalyzeAgent(path, false)
	if err != nil {
		t.Fatalf("AnalyzeAgent on malformed file returned error: %v", err)
	}

	if analysis.MessageCount != 1 {
		t.Errorf("MessageCount: got %d, want 1 (only the valid line)", analysis.MessageCount)
	}
	if analysis.SkippedLines != 2 {
		t.Errorf("SkippedLines: got %d, want 2", analysis.SkippedLines)
	}
	if analysis.TotalUsage.InputTokens != 100 {
		t.Errorf("InputTokens: got %d, want 100", analysis.TotalUsage.InputTokens)
	}
}

func TestAnalyzeAgent_MissingFile(t *testing.T) {
	_, err := AnalyzeAgent(filepath.Join(t.TempDir(), "agent-nope.jsonl"), false)
	if err == nil {
		t.Fatal("AnalyzeAgent on missing file should return error")
	}
}
