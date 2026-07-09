package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

func TestBuildSessionAnalysisEmpty(t *testing.T) {
	analysis := buildSessionAnalysis("test-session", "/path/to/session.jsonl", nil, false)

	if analysis.SessionID != "test-session" {
		t.Errorf("SessionID: got %s, want test-session", analysis.SessionID)
	}
	if analysis.MessageCount != 0 {
		t.Errorf("MessageCount: got %d, want 0", analysis.MessageCount)
	}
	if analysis.TotalCost.TotalCost != 0 {
		t.Errorf("TotalCost: got %f, want 0", analysis.TotalCost.TotalCost)
	}
}

func TestBuildSessionAnalysisWithMessages(t *testing.T) {
	t1 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC)

	messages := []models.MessageAnalysis{
		{
			Timestamp: t1,
			Model:     "claude-opus-4-5",
			Usage:     models.TokenUsage{InputTokens: 1000, OutputTokens: 500},
			Cost:      models.CostBreakdown{TotalCost: 1.50},
		},
		{
			Timestamp: t2,
			Model:     "claude-opus-4-5",
			Usage:     models.TokenUsage{InputTokens: 2000, OutputTokens: 1000},
			Cost:      models.CostBreakdown{TotalCost: 3.00},
		},
		{
			Timestamp: t3,
			Model:     "claude-sonnet-4-5",
			Usage:     models.TokenUsage{InputTokens: 500, OutputTokens: 200},
			Cost:      models.CostBreakdown{TotalCost: 0.50},
		},
	}

	analysis := buildSessionAnalysis("test-session", "/path", messages, false)

	if analysis.MessageCount != 3 {
		t.Errorf("MessageCount: got %d, want 3", analysis.MessageCount)
	}
	if analysis.TotalUsage.InputTokens != 3500 {
		t.Errorf("TotalUsage.InputTokens: got %d, want 3500", analysis.TotalUsage.InputTokens)
	}
	if analysis.TotalUsage.OutputTokens != 1700 {
		t.Errorf("TotalUsage.OutputTokens: got %d, want 1700", analysis.TotalUsage.OutputTokens)
	}
	if !almostEqual(analysis.TotalCost.TotalCost, 5.0, 0.001) {
		t.Errorf("TotalCost: got %f, want 5.0", analysis.TotalCost.TotalCost)
	}

	// Check time range
	if !analysis.StartTime.Equal(t1) {
		t.Errorf("StartTime: got %v, want %v", analysis.StartTime, t1)
	}
	if !analysis.EndTime.Equal(t3) {
		t.Errorf("EndTime: got %v, want %v", analysis.EndTime, t3)
	}
	expectedDuration := time.Hour
	if analysis.Duration.Duration() != expectedDuration {
		t.Errorf("Duration: got %v, want %v", analysis.Duration.Duration(), expectedDuration)
	}

	// Check cost by model
	if len(analysis.CostByModel) != 2 {
		t.Errorf("CostByModel count: got %d, want 2", len(analysis.CostByModel))
	}
	if opusCost, ok := analysis.CostByModel["claude-opus-4-5"]; !ok {
		t.Error("CostByModel missing claude-opus-4-5")
	} else if !almostEqual(opusCost.TotalCost, 4.5, 0.001) {
		t.Errorf("CostByModel[claude-opus-4-5]: got %f, want 4.5", opusCost.TotalCost)
	}
}

func TestBuildSessionAnalysisIncludeMessages(t *testing.T) {
	messages := []models.MessageAnalysis{
		{Model: "claude-opus-4-5"},
		{Model: "claude-sonnet-4-5"},
	}

	// Without include messages
	analysis1 := buildSessionAnalysis("test", "/path", messages, false)
	if len(analysis1.Messages) != 0 {
		t.Errorf("Messages should be empty when includeMessages=false, got %d", len(analysis1.Messages))
	}

	// With include messages
	analysis2 := buildSessionAnalysis("test", "/path", messages, true)
	if len(analysis2.Messages) != 2 {
		t.Errorf("Messages should be included when includeMessages=true, got %d", len(analysis2.Messages))
	}
}

func TestAnalyzeSessionFromMessages(t *testing.T) {
	jsonlMessages := []models.JSONLMessage{
		{
			Type:      "assistant",
			Timestamp: time.Now(),
			Message: &models.AssistantMessage{
				Model: "claude-sonnet-4-5",
				Usage: models.TokenUsage{InputTokens: 1000, OutputTokens: 500},
			},
		},
	}

	analysis := AnalyzeSessionFromMessages("test-session", "/path", jsonlMessages, false)

	if analysis.SessionID != "test-session" {
		t.Errorf("SessionID: got %s, want test-session", analysis.SessionID)
	}
	if analysis.MessageCount != 1 {
		t.Errorf("MessageCount: got %d, want 1", analysis.MessageCount)
	}
	// Cost: 1000 input @ $3/M + 500 output @ $15/M = 0.003 + 0.0075 = 0.0105
	if !almostEqual(analysis.TotalCost.TotalCost, 0.0105, 0.0001) {
		t.Errorf("TotalCost: got %f, want 0.0105", analysis.TotalCost.TotalCost)
	}
}

// --- 2A: TestAnalyzeSession_Basic ---

func TestAnalyzeSession_Basic(t *testing.T) {
	tmpDir := t.TempDir()

	sessionID := "sess-basic"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	// 3 assistant messages with claude-sonnet-4-5
	// Message 1: 1000 input, 500 output
	// Message 2: 2000 input, 1000 output
	// Message 3: 500 input, 200 output
	//
	// claude-sonnet-4-5 pricing: $3/M input, $15/M output
	// Msg1: (1000/1e6)*3 + (500/1e6)*15  = 0.003 + 0.0075 = 0.0105
	// Msg2: (2000/1e6)*3 + (1000/1e6)*15 = 0.006 + 0.015  = 0.021
	// Msg3: (500/1e6)*3  + (200/1e6)*15  = 0.0015 + 0.003 = 0.0045
	// Total: 0.036
	content := strings.Join([]string{
		`{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`,
		`{"type":"assistant","timestamp":"2024-01-15T10:10:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":2000,"output_tokens":1000}}}`,
		`{"type":"assistant","timestamp":"2024-01-15T10:20:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":500,"output_tokens":200}}}`,
	}, "\n")

	if err := os.WriteFile(sessionPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	analysis, err := AnalyzeSession(sessionPath, sessionID, false)
	if err != nil {
		t.Fatalf("AnalyzeSession failed: %v", err)
	}

	// MessageCount
	if analysis.MessageCount != 3 {
		t.Errorf("MessageCount: got %d, want 3", analysis.MessageCount)
	}

	// TotalCost
	expectedCost := 0.036
	if !almostEqual(analysis.TotalCost.TotalCost, expectedCost, 0.0001) {
		t.Errorf("TotalCost: got %f, want %f", analysis.TotalCost.TotalCost, expectedCost)
	}

	// StartTime and EndTime
	expectedStart := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	expectedEnd := time.Date(2024, 1, 15, 10, 20, 0, 0, time.UTC)
	if !analysis.StartTime.Equal(expectedStart) {
		t.Errorf("StartTime: got %v, want %v", analysis.StartTime, expectedStart)
	}
	if !analysis.EndTime.Equal(expectedEnd) {
		t.Errorf("EndTime: got %v, want %v", analysis.EndTime, expectedEnd)
	}

	// CostByModel
	if _, ok := analysis.CostByModel["claude-sonnet-4-5"]; !ok {
		t.Error("CostByModel missing claude-sonnet-4-5")
	}
	if len(analysis.CostByModel) != 1 {
		t.Errorf("CostByModel count: got %d, want 1", len(analysis.CostByModel))
	}

	// HasAgents should be false (no subagents dir)
	if analysis.HasAgents {
		t.Error("HasAgents should be false")
	}
}

// --- 2B: TestAnalyzeSession_WithAgents ---

func TestAnalyzeSession_WithAgents(t *testing.T) {
	tmpDir := t.TempDir()

	sessionID := "sess-with-agents"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	// Parent session: 1 message
	// 1000 input, 500 output
	// Cost: (1000/1e6)*3 + (500/1e6)*15 = 0.003 + 0.0075 = 0.0105
	parentContent := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`

	if err := os.WriteFile(sessionPath, []byte(parentContent), 0644); err != nil {
		t.Fatalf("failed to write parent session: %v", err)
	}

	// Create subagents directory
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatalf("failed to create subagents dir: %v", err)
	}

	// Agent session: 1 message
	// 500 input, 200 output
	// Cost: (500/1e6)*3 + (200/1e6)*15 = 0.0015 + 0.003 = 0.0045
	agentContent := `{"type":"assistant","timestamp":"2024-01-15T10:30:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":500,"output_tokens":200}}}`

	agentPath := filepath.Join(subagentsDir, "agent-abc123.jsonl")
	if err := os.WriteFile(agentPath, []byte(agentContent), 0644); err != nil {
		t.Fatalf("failed to write agent session: %v", err)
	}

	analysis, err := AnalyzeSession(sessionPath, sessionID, false)
	if err != nil {
		t.Fatalf("AnalyzeSession failed: %v", err)
	}

	// HasAgents should be true
	if !analysis.HasAgents {
		t.Error("HasAgents should be true")
	}

	// AgentCount
	if analysis.AgentCount != 1 {
		t.Errorf("AgentCount: got %d, want 1", analysis.AgentCount)
	}

	// AgentsCost > 0
	if analysis.AgentsCost.TotalCost <= 0 {
		t.Error("AgentsCost should be > 0")
	}
	expectedAgentCost := 0.0045
	if !almostEqual(analysis.AgentsCost.TotalCost, expectedAgentCost, 0.0001) {
		t.Errorf("AgentsCost: got %f, want %f", analysis.AgentsCost.TotalCost, expectedAgentCost)
	}

	// TotalCost should include both parent and agent
	expectedParentCost := 0.0105
	expectedTotalCost := expectedParentCost + expectedAgentCost // 0.015
	if !almostEqual(analysis.TotalCost.TotalCost, expectedTotalCost, 0.0001) {
		t.Errorf("TotalCost: got %f, want %f", analysis.TotalCost.TotalCost, expectedTotalCost)
	}

	// TotalCost > ParentCost
	if analysis.TotalCost.TotalCost <= analysis.ParentCost.TotalCost {
		t.Errorf("TotalCost (%f) should be > ParentCost (%f)",
			analysis.TotalCost.TotalCost, analysis.ParentCost.TotalCost)
	}

	// Time range should be extended by agent (agent timestamp is 10:30, parent is 10:00)
	expectedEnd := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !analysis.EndTime.Equal(expectedEnd) {
		t.Errorf("EndTime: got %v, want %v (should be extended by agent)", analysis.EndTime, expectedEnd)
	}

	// MessageCount includes agent messages
	if analysis.MessageCount != 2 {
		t.Errorf("MessageCount: got %d, want 2 (1 parent + 1 agent)", analysis.MessageCount)
	}
}

func TestAnalyzeSession_SkippedLines(t *testing.T) {
	tmpDir := t.TempDir()

	sessionID := "sess-skipped-lines"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	// Parent: 1 valid message + 2 malformed lines
	parentContent := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}
not json at all
{"type":"assistant","broken`

	if err := os.WriteFile(sessionPath, []byte(parentContent), 0644); err != nil {
		t.Fatalf("failed to write parent session: %v", err)
	}

	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatalf("failed to create subagents dir: %v", err)
	}

	// Agent: 1 valid message + 1 malformed line
	agentContent := `{"type":"assistant","timestamp":"2024-01-15T10:30:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":500,"output_tokens":200}}}
{malformed`

	if err := os.WriteFile(filepath.Join(subagentsDir, "agent-abc123.jsonl"), []byte(agentContent), 0644); err != nil {
		t.Fatalf("failed to write agent session: %v", err)
	}

	analysis, err := AnalyzeSession(sessionPath, sessionID, false)
	if err != nil {
		t.Fatalf("AnalyzeSession failed: %v", err)
	}

	if analysis.SkippedLines != 3 {
		t.Errorf("SkippedLines: got %d, want 3 (2 parent + 1 agent)", analysis.SkippedLines)
	}
	if len(analysis.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(analysis.Agents))
	}
	if analysis.Agents[0].SkippedLines != 1 {
		t.Errorf("agent SkippedLines: got %d, want 1", analysis.Agents[0].SkippedLines)
	}
	// Valid messages must still be counted despite skips
	if analysis.MessageCount != 2 {
		t.Errorf("MessageCount: got %d, want 2", analysis.MessageCount)
	}
}

// --- 2C: TestAnalyzeMultipleSessions_Basic ---

func TestAnalyzeMultipleSessions_Basic(t *testing.T) {
	tmpDir := t.TempDir()

	// Session 1: 2 messages
	// Msg1: 1000 input, 500 output -> 0.0105
	// Msg2: 500 input, 200 output  -> 0.0045
	// Session 1 total: 0.015
	sess1Path := filepath.Join(tmpDir, "sess-one.jsonl")
	sess1Content := strings.Join([]string{
		`{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`,
		`{"type":"assistant","timestamp":"2024-01-15T10:10:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":500,"output_tokens":200}}}`,
	}, "\n")
	if err := os.WriteFile(sess1Path, []byte(sess1Content), 0644); err != nil {
		t.Fatalf("failed to write session 1: %v", err)
	}

	// Session 2: 1 message
	// 2000 input, 1000 output -> 0.021
	sess2Path := filepath.Join(tmpDir, "sess-two.jsonl")
	sess2Content := `{"type":"assistant","timestamp":"2024-01-16T12:00:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":2000,"output_tokens":1000}}}`
	if err := os.WriteFile(sess2Path, []byte(sess2Content), 0644); err != nil {
		t.Fatalf("failed to write session 2: %v", err)
	}

	entries := []models.SessionEntry{
		{SessionID: "sess-one", FullPath: sess1Path},
		{SessionID: "sess-two", FullPath: sess2Path},
	}

	result, results, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions failed: %v", err)
	}

	// MessageCount: 2 + 1 = 3
	if result.MessageCount != 3 {
		t.Errorf("MessageCount: got %d, want 3", result.MessageCount)
	}

	// One result per input entry, in input order, both parsed successfully
	if len(results) != 2 {
		t.Fatalf("results: got %d, want 2", len(results))
	}
	for i, r := range results {
		if r.Entry.SessionID != entries[i].SessionID {
			t.Errorf("results[%d].Entry.SessionID: got %q, want %q", i, r.Entry.SessionID, entries[i].SessionID)
		}
		if r.Analysis == nil {
			t.Errorf("results[%d].Analysis: got nil, want non-nil", i)
		}
	}

	// TotalCost: 0.015 + 0.021 = 0.036
	expectedTotal := 0.036
	if !almostEqual(result.TotalCost.TotalCost, expectedTotal, 0.001) {
		t.Errorf("TotalCost: got %f, want %f", result.TotalCost.TotalCost, expectedTotal)
	}

	// Time range should span both sessions
	expectedStart := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	expectedEnd := time.Date(2024, 1, 16, 12, 0, 0, 0, time.UTC)
	if !result.StartTime.Equal(expectedStart) {
		t.Errorf("StartTime: got %v, want %v", result.StartTime, expectedStart)
	}
	if !result.EndTime.Equal(expectedEnd) {
		t.Errorf("EndTime: got %v, want %v", result.EndTime, expectedEnd)
	}

	// CostByModel
	if _, ok := result.CostByModel["claude-sonnet-4-5"]; !ok {
		t.Error("CostByModel missing claude-sonnet-4-5")
	}

	// No skipped sessions
	if result.SkippedSessions != 0 {
		t.Errorf("SkippedSessions: got %d, want 0", result.SkippedSessions)
	}
}

// --- 2D: TestAnalyzeMultipleSessions_AllFail ---

func TestAnalyzeMultipleSessions_AllFail(t *testing.T) {
	entries := []models.SessionEntry{
		{SessionID: "bad-1", FullPath: "/nonexistent/path/bad-1.jsonl"},
		{SessionID: "bad-2", FullPath: "/nonexistent/path/bad-2.jsonl"},
	}

	_, results, err := AnalyzeMultipleSessions(entries)
	if err == nil {
		t.Fatal("expected error when all sessions fail")
	}
	if !strings.Contains(err.Error(), "all") {
		t.Errorf("error message should contain 'all', got: %s", err.Error())
	}
	// When every session fails, no results are returned alongside the error
	if results != nil {
		t.Errorf("results: got %v, want nil on total failure", results)
	}
}

// --- 2E: TestAnalyzeMultipleSessions_MixedSuccess ---

func TestAnalyzeMultipleSessions_MixedSuccess(t *testing.T) {
	tmpDir := t.TempDir()

	// One valid session
	validPath := filepath.Join(tmpDir, "valid-sess.jsonl")
	validContent := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`
	if err := os.WriteFile(validPath, []byte(validContent), 0644); err != nil {
		t.Fatalf("failed to write valid session: %v", err)
	}

	entries := []models.SessionEntry{
		{SessionID: "valid-sess", FullPath: validPath},
		{SessionID: "invalid-sess", FullPath: "/nonexistent/invalid-sess.jsonl"},
	}

	result, results, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SkippedSessions should be 1
	if result.SkippedSessions != 1 {
		t.Errorf("SkippedSessions: got %d, want 1", result.SkippedSessions)
	}

	// Per-session results carry every entry in order; the failed one has a nil
	// Analysis (rendered as an error row), the valid one does not
	if len(results) != 2 {
		t.Fatalf("results: got %d, want 2", len(results))
	}
	if results[0].Analysis == nil {
		t.Error("results[0].Analysis (valid-sess): got nil, want non-nil")
	}
	if results[1].Analysis != nil {
		t.Error("results[1].Analysis (invalid-sess): got non-nil, want nil")
	}

	// Result should be non-nil with data from valid session
	if result.MessageCount != 1 {
		t.Errorf("MessageCount: got %d, want 1", result.MessageCount)
	}

	// Cost: (1000/1e6)*3 + (500/1e6)*15 = 0.003 + 0.0075 = 0.0105
	expectedCost := 0.0105
	if !almostEqual(result.TotalCost.TotalCost, expectedCost, 0.0001) {
		t.Errorf("TotalCost: got %f, want %f", result.TotalCost.TotalCost, expectedCost)
	}

	if _, ok := result.CostByModel["claude-sonnet-4-5"]; !ok {
		t.Error("CostByModel missing claude-sonnet-4-5")
	}
}

// TestAnalyzeSession_DeduplicatesStreamingLines verifies end-to-end that
// streamed duplicate lines (same message.id + requestId, growing usage) are
// billed once at the final line's usage. Without dedup this session would
// count 4 messages and sum output 5+120+394+10 = 529.
func TestAnalyzeSession_DeduplicatesStreamingLines(t *testing.T) {
	tmpDir := t.TempDir()

	sessionID := "sess-dedup"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	content := strings.Join([]string{
		`{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","requestId":"req_A","message":{"id":"msg_A","model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":5}}}`,
		`{"type":"assistant","timestamp":"2024-01-15T10:00:02Z","requestId":"req_A","message":{"id":"msg_A","model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":120}}}`,
		`{"type":"assistant","timestamp":"2024-01-15T10:00:05Z","requestId":"req_A","message":{"id":"msg_A","model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":394}}}`,
		`{"type":"assistant","timestamp":"2024-01-15T10:10:00Z","requestId":"req_B","message":{"id":"msg_B","model":"claude-sonnet-4-5","usage":{"input_tokens":500,"output_tokens":10}}}`,
	}, "\n")

	if err := os.WriteFile(sessionPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	analysis, err := AnalyzeSession(sessionPath, sessionID, false)
	if err != nil {
		t.Fatalf("AnalyzeSession failed: %v", err)
	}

	if analysis.MessageCount != 2 {
		t.Errorf("MessageCount: got %d, want 2 (4 lines, 2 distinct message ids)", analysis.MessageCount)
	}
	if analysis.TotalUsage.InputTokens != 1500 {
		t.Errorf("InputTokens: got %d, want 1500", analysis.TotalUsage.InputTokens)
	}
	if analysis.TotalUsage.OutputTokens != 404 {
		t.Errorf("OutputTokens: got %d, want 404 (394 final + 10, not 529 summed)", analysis.TotalUsage.OutputTokens)
	}

	// claude-sonnet-4-5: $3/M input, $15/M output
	// (1500/1e6)*3 + (404/1e6)*15 = 0.0045 + 0.00606 = 0.01056
	if !almostEqual(analysis.TotalCost.TotalCost, 0.01056, 0.0001) {
		t.Errorf("TotalCost: got %f, want 0.01056", analysis.TotalCost.TotalCost)
	}

	// EndTime comes from msg_B; StartTime from msg_A's last streaming line
	expectedStart := time.Date(2024, 1, 15, 10, 0, 5, 0, time.UTC)
	if !analysis.StartTime.Equal(expectedStart) {
		t.Errorf("StartTime: got %v, want %v", analysis.StartTime, expectedStart)
	}
}

// TestAnalyzeSessionFromMessages_Deduplicates covers the pre-parsed entry
// point, which must dedup independently of ParseJSONLWithResult.
func TestAnalyzeSessionFromMessages_Deduplicates(t *testing.T) {
	ts := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	jsonlMessages := []models.JSONLMessage{
		{
			Type:      "assistant",
			Timestamp: ts,
			RequestID: "req_A",
			Message: &models.AssistantMessage{
				ID:    "msg_A",
				Model: "claude-sonnet-4-5",
				Usage: models.TokenUsage{InputTokens: 1000, OutputTokens: 5},
			},
		},
		{
			Type:      "assistant",
			Timestamp: ts.Add(3 * time.Second),
			RequestID: "req_A",
			Message: &models.AssistantMessage{
				ID:    "msg_A",
				Model: "claude-sonnet-4-5",
				Usage: models.TokenUsage{InputTokens: 1000, OutputTokens: 500},
			},
		},
	}

	analysis := AnalyzeSessionFromMessages("test-session", "/path", jsonlMessages, false)

	if analysis.MessageCount != 1 {
		t.Errorf("MessageCount: got %d, want 1", analysis.MessageCount)
	}
	if analysis.TotalUsage.OutputTokens != 500 {
		t.Errorf("OutputTokens: got %d, want 500 (last occurrence)", analysis.TotalUsage.OutputTokens)
	}
}

// Start/EndTime must be the min/max over timestamps, not the first/last message
// positionally — out-of-order or zero timestamps otherwise yield negative durations.
func TestTimeRange(t *testing.T) {
	t1 := time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC)
	var zero time.Time

	tests := []struct {
		name       string
		timestamps []time.Time
		wantStart  time.Time
		wantEnd    time.Time
	}{
		{"chronological", []time.Time{t1, t2, t3}, t1, t3},
		{"reversed", []time.Time{t3, t2, t1}, t1, t3},
		{"interleaved", []time.Time{t2, t1, t3}, t1, t3},
		{"zero first", []time.Time{zero, t1, t2}, t1, t2},
		{"zero last", []time.Time{t1, t2, zero}, t1, t2},
		{"all zero", []time.Time{zero, zero}, zero, zero},
		{"empty", nil, zero, zero},
		{"single", []time.Time{t2}, t2, t2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := make([]models.MessageAnalysis, len(tt.timestamps))
			for i, ts := range tt.timestamps {
				messages[i].Timestamp = ts
			}
			start, end := timeRange(messages)
			if !start.Equal(tt.wantStart) {
				t.Errorf("start: got %v, want %v", start, tt.wantStart)
			}
			if !end.Equal(tt.wantEnd) {
				t.Errorf("end: got %v, want %v", end, tt.wantEnd)
			}
		})
	}
}

func TestBuildSessionAnalysis_OutOfOrderTimestamps(t *testing.T) {
	early := time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC)
	late := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// Later timestamp appears first (resumed/interleaved session)
	messages := []models.MessageAnalysis{
		{Timestamp: late, Cost: models.CostBreakdown{TotalCost: 0.1}},
		{Timestamp: early, Cost: models.CostBreakdown{TotalCost: 0.1}},
	}

	analysis := buildSessionAnalysis("sess", "/path", messages, false)

	if !analysis.StartTime.Equal(early) {
		t.Errorf("StartTime: got %v, want %v", analysis.StartTime, early)
	}
	if !analysis.EndTime.Equal(late) {
		t.Errorf("EndTime: got %v, want %v", analysis.EndTime, late)
	}
	if analysis.Duration.Duration() != time.Hour {
		t.Errorf("Duration: got %v, want 1h (was negative before fix)", analysis.Duration.Duration())
	}
}

func TestBuildSessionAnalysis_ZeroTimestampLast(t *testing.T) {
	t1 := time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)

	// Final message has a missing timestamp (unmarshals to zero time)
	messages := []models.MessageAnalysis{
		{Timestamp: t1},
		{Timestamp: t2},
		{},
	}

	analysis := buildSessionAnalysis("sess", "/path", messages, false)

	if !analysis.EndTime.Equal(t2) {
		t.Errorf("EndTime: got %v, want %v (zero timestamp must be ignored)", analysis.EndTime, t2)
	}
	if analysis.Duration.Duration() != 30*time.Minute {
		t.Errorf("Duration: got %v, want 30m", analysis.Duration.Duration())
	}
}

func TestAnalyzeAgent_OutOfOrderTimestamps(t *testing.T) {
	tmpDir := t.TempDir()
	agentPath := filepath.Join(tmpDir, "agent-ooo.jsonl")

	// Later timestamp first, then earlier — duration must not go negative
	content := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"id":"m1","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":50}}}
{"type":"assistant","timestamp":"2024-01-15T09:00:00Z","message":{"id":"m2","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":50}}}`
	if err := os.WriteFile(agentPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write agent file: %v", err)
	}

	analysis, err := AnalyzeAgent(agentPath, false)
	if err != nil {
		t.Fatalf("AnalyzeAgent failed: %v", err)
	}

	wantStart := time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	if !analysis.StartTime.Equal(wantStart) {
		t.Errorf("StartTime: got %v, want %v", analysis.StartTime, wantStart)
	}
	if !analysis.EndTime.Equal(wantEnd) {
		t.Errorf("EndTime: got %v, want %v", analysis.EndTime, wantEnd)
	}
	if analysis.Duration.Duration() != time.Hour {
		t.Errorf("Duration: got %v, want 1h (was negative before fix)", analysis.Duration.Duration())
	}
}

func TestAnalyzeSession_WithWorkflowAgents(t *testing.T) {
	tmpDir := t.TempDir()

	sessionID := "sess-with-workflows"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	// Parent: 1000 input, 500 output on sonnet-4-5 → 0.003 + 0.0075 = 0.0105
	parentContent := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`
	if err := os.WriteFile(sessionPath, []byte(parentContent), 0644); err != nil {
		t.Fatal(err)
	}

	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	runDir := filepath.Join(subagentsDir, "workflows", "wf_run-1")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Regular agent: 500 input, 200 output on sonnet-4-5 → 0.0015 + 0.003 = 0.0045
	regularContent := `{"type":"assistant","timestamp":"2024-01-15T10:10:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":500,"output_tokens":200}}}`
	if err := os.WriteFile(filepath.Join(subagentsDir, "agent-reg1.jsonl"), []byte(regularContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Two workflow agents on different models
	wfAgent1 := `{"type":"assistant","timestamp":"2024-01-15T10:20:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":100}}}`
	wfAgent2 := `{"type":"assistant","timestamp":"2024-01-15T10:40:00Z","message":{"model":"claude-opus-4-1","usage":{"input_tokens":1000,"output_tokens":100}}}`
	if err := os.WriteFile(filepath.Join(runDir, "agent-wf1.jsonl"), []byte(wfAgent1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "agent-wf2.jsonl"), []byte(wfAgent2), 0644); err != nil {
		t.Fatal(err)
	}
	// Journal must be ignored
	if err := os.WriteFile(filepath.Join(runDir, "journal.jsonl"), []byte(`{"type":"started","key":"v2:x","agentId":"wf1"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Workflow run metadata
	wfMetaDir := filepath.Join(tmpDir, sessionID, "workflows")
	if err := os.MkdirAll(wfMetaDir, 0755); err != nil {
		t.Fatal(err)
	}
	wfMeta := `{"runId":"wf_run-1","workflowName":"audit-codebase","status":"completed","script":"export const meta = {}"}`
	if err := os.WriteFile(filepath.Join(wfMetaDir, "wf_run-1.json"), []byte(wfMeta), 0644); err != nil {
		t.Fatal(err)
	}

	analysis, err := AnalyzeSession(sessionPath, sessionID, false)
	if err != nil {
		t.Fatalf("AnalyzeSession failed: %v", err)
	}

	if analysis.AgentCount != 3 {
		t.Errorf("AgentCount: got %d, want 3 (1 regular + 2 workflow)", analysis.AgentCount)
	}
	if analysis.MessageCount != 4 {
		t.Errorf("MessageCount: got %d, want 4", analysis.MessageCount)
	}

	// Cost invariant: total == parent + agents
	sum := analysis.ParentCost.TotalCost + analysis.AgentsCost.TotalCost
	if !almostEqual(analysis.TotalCost.TotalCost, sum, 1e-9) {
		t.Errorf("TotalCost (%f) != ParentCost + AgentsCost (%f)", analysis.TotalCost.TotalCost, sum)
	}

	// WorkflowID tags: regular agent untagged, workflow agents tagged
	tags := make(map[string]string)
	for _, a := range analysis.Agents {
		tags[a.AgentID] = a.WorkflowID
	}
	if tags["reg1"] != "" {
		t.Errorf("regular agent WorkflowID: got %q, want empty", tags["reg1"])
	}
	if tags["wf1"] != "wf_run-1" || tags["wf2"] != "wf_run-1" {
		t.Errorf("workflow agent tags: got wf1=%q wf2=%q, want wf_run-1", tags["wf1"], tags["wf2"])
	}

	// Workflow metadata
	if analysis.WorkflowCount != 1 || len(analysis.Workflows) != 1 {
		t.Fatalf("WorkflowCount: got %d (metas %d), want 1", analysis.WorkflowCount, len(analysis.Workflows))
	}
	wf := analysis.Workflows[0]
	if wf.RunID != "wf_run-1" || wf.Name != "audit-codebase" || wf.Status != "completed" {
		t.Errorf("workflow meta: got %+v", wf)
	}

	// Both workflow models merged into CostByModel
	if _, ok := analysis.CostByModel["claude-opus-4-1"]; !ok {
		t.Error("CostByModel missing workflow agent model claude-opus-4-1")
	}
	if _, ok := analysis.CostByModel["claude-sonnet-4-5"]; !ok {
		t.Error("CostByModel missing claude-sonnet-4-5")
	}

	// Time range extended by the latest workflow agent (10:40)
	expectedEnd := time.Date(2024, 1, 15, 10, 40, 0, 0, time.UTC)
	if !analysis.EndTime.Equal(expectedEnd) {
		t.Errorf("EndTime: got %v, want %v", analysis.EndTime, expectedEnd)
	}
}

func TestAnalyzeSession_WorkflowOrphanRun(t *testing.T) {
	tmpDir := t.TempDir()

	sessionID := "sess-wf-orphan"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")
	parentContent := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`
	if err := os.WriteFile(sessionPath, []byte(parentContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Workflow agent dir WITHOUT a matching workflows/{runID}.json
	runDir := filepath.Join(tmpDir, sessionID, "subagents", "workflows", "wf_orphan")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}
	wfAgent := `{"type":"assistant","timestamp":"2024-01-15T10:20:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":500,"output_tokens":200}}}`
	if err := os.WriteFile(filepath.Join(runDir, "agent-w1.jsonl"), []byte(wfAgent), 0644); err != nil {
		t.Fatal(err)
	}

	analysis, err := AnalyzeSession(sessionPath, sessionID, false)
	if err != nil {
		t.Fatalf("AnalyzeSession failed: %v", err)
	}

	if analysis.AgentCount != 1 {
		t.Errorf("AgentCount: got %d, want 1 (orphan run still analyzed)", analysis.AgentCount)
	}
	if analysis.AgentsCost.TotalCost <= 0 {
		t.Error("orphan workflow agent cost should be counted")
	}
	if analysis.WorkflowCount != 1 || len(analysis.Workflows) != 1 {
		t.Fatalf("WorkflowCount: got %d, want 1", analysis.WorkflowCount)
	}
	if wf := analysis.Workflows[0]; wf.RunID != "wf_orphan" || wf.Name != "" || wf.Status != "" {
		t.Errorf("expected runID-only fallback meta, got %+v", wf)
	}
}

// --- Cross-file dedup: fork/branch copies the transcript into a new session file ---

// Fork fixture: the original session holds m1+m2; the fork file holds cloned
// copies of m1+m2 (same message.id:requestId, same usage) plus novel m3.
// Costs (claude-sonnet-4-5): m1 = 0.0105, m2 = 0.0045, m3 = 0.021.
const (
	forkMsg1 = `{"type":"assistant","requestId":"req_1","timestamp":"2024-01-15T10:00:00Z","message":{"id":"msg_1","model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`
	forkMsg2 = `{"type":"assistant","requestId":"req_2","timestamp":"2024-01-15T10:10:00Z","message":{"id":"msg_2","model":"claude-sonnet-4-5","usage":{"input_tokens":500,"output_tokens":200}}}`
	forkMsg3 = `{"type":"assistant","requestId":"req_3","timestamp":"2024-01-16T09:00:00Z","message":{"id":"msg_3","model":"claude-sonnet-4-5","usage":{"input_tokens":2000,"output_tokens":1000}}}`
)

func TestAnalyzeMultipleSessions_CrossFileDedup(t *testing.T) {
	tmpDir := t.TempDir()

	originalPath := filepath.Join(tmpDir, "original.jsonl")
	writeJSONLFile(t, originalPath, []string{forkMsg1, forkMsg2})

	forkPath := filepath.Join(tmpDir, "fork.jsonl")
	writeJSONLFile(t, forkPath, []string{forkMsg1, forkMsg2, forkMsg3})

	// Fork first in input order but with a later mtime: attribution must
	// follow mtime (original keeps the shared history), results input order.
	entries := []models.SessionEntry{
		{SessionID: "fork", FullPath: forkPath, Modified: time.Date(2024, 1, 16, 9, 0, 0, 0, time.UTC)},
		{SessionID: "original", FullPath: originalPath, Modified: time.Date(2024, 1, 15, 10, 10, 0, 0, time.UTC)},
	}

	aggregate, results, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions failed: %v", err)
	}

	// Aggregate counts each API response once: m1 + m2 + m3
	if aggregate.MessageCount != 3 {
		t.Errorf("aggregate MessageCount: got %d, want 3", aggregate.MessageCount)
	}
	if !almostEqual(aggregate.TotalCost.TotalCost, 0.036, 0.0001) {
		t.Errorf("aggregate TotalCost: got %f, want 0.036", aggregate.TotalCost.TotalCost)
	}
	if aggregate.TotalUsage.InputTokens != 3500 {
		t.Errorf("aggregate InputTokens: got %d, want 3500", aggregate.TotalUsage.InputTokens)
	}
	if aggregate.TotalUsage.OutputTokens != 1700 {
		t.Errorf("aggregate OutputTokens: got %d, want 1700", aggregate.TotalUsage.OutputTokens)
	}

	// Results stay in input order; the fork keeps only its novel message, the
	// original keeps the shared history — rows sum to the aggregate.
	if len(results) != 2 {
		t.Fatalf("results: got %d, want 2", len(results))
	}
	fork, original := results[0], results[1]
	if fork.Entry.SessionID != "fork" || original.Entry.SessionID != "original" {
		t.Fatalf("results out of input order: %q, %q", fork.Entry.SessionID, original.Entry.SessionID)
	}
	if fork.Analysis.MessageCount != 1 {
		t.Errorf("fork MessageCount: got %d, want 1", fork.Analysis.MessageCount)
	}
	if !almostEqual(fork.Analysis.TotalCost.TotalCost, 0.021, 0.0001) {
		t.Errorf("fork TotalCost: got %f, want 0.021", fork.Analysis.TotalCost.TotalCost)
	}
	if original.Analysis.MessageCount != 2 {
		t.Errorf("original MessageCount: got %d, want 2", original.Analysis.MessageCount)
	}
	if !almostEqual(original.Analysis.TotalCost.TotalCost, 0.015, 0.0001) {
		t.Errorf("original TotalCost: got %f, want 0.015", original.Analysis.TotalCost.TotalCost)
	}
}

// Standalone single-session analysis (show/watch/breakdown) stays file-local:
// a forked session still reports its full transcript, inherited history included.
func TestAnalyzeSession_ForkFileStaysFileLocal(t *testing.T) {
	tmpDir := t.TempDir()
	forkPath := filepath.Join(tmpDir, "fork.jsonl")
	writeJSONLFile(t, forkPath, []string{forkMsg1, forkMsg2, forkMsg3})

	analysis, err := AnalyzeSession(forkPath, "fork", false)
	if err != nil {
		t.Fatalf("AnalyzeSession failed: %v", err)
	}
	if analysis.MessageCount != 3 {
		t.Errorf("MessageCount: got %d, want 3", analysis.MessageCount)
	}
	if !almostEqual(analysis.TotalCost.TotalCost, 0.036, 0.0001) {
		t.Errorf("TotalCost: got %f, want 0.036", analysis.TotalCost.TotalCost)
	}
}

// Lines without a message id have no identity — identical content in two
// files must never be collapsed by the cross-file dedup.
func TestAnalyzeMultipleSessions_CrossFileDedup_KeepsUnidentifiedLines(t *testing.T) {
	tmpDir := t.TempDir()
	line := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`

	pathA := filepath.Join(tmpDir, "a.jsonl")
	writeJSONLFile(t, pathA, []string{line})
	pathB := filepath.Join(tmpDir, "b.jsonl")
	writeJSONLFile(t, pathB, []string{line})

	entries := []models.SessionEntry{
		{SessionID: "a", FullPath: pathA, Modified: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)},
		{SessionID: "b", FullPath: pathB, Modified: time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC)},
	}

	aggregate, _, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions failed: %v", err)
	}
	if aggregate.MessageCount != 2 {
		t.Errorf("MessageCount: got %d, want 2 (no-id lines must not dedup)", aggregate.MessageCount)
	}
	if !almostEqual(aggregate.TotalCost.TotalCost, 0.021, 0.0001) {
		t.Errorf("TotalCost: got %f, want 0.021", aggregate.TotalCost.TotalCost)
	}
}

// Agent sub-session files are outside the cross-file dedup set (fork clones
// only the parent transcript): an agent message sharing a key with another
// session's parent stays counted, file-local.
func TestAnalyzeMultipleSessions_AgentFilesStayFileLocal(t *testing.T) {
	tmpDir := t.TempDir()

	pathA := filepath.Join(tmpDir, "sess-a.jsonl")
	writeJSONLFile(t, pathA, []string{forkMsg1})

	pathB := filepath.Join(tmpDir, "sess-b.jsonl")
	writeJSONLFile(t, pathB, []string{forkMsg3})
	agentDir := filepath.Join(tmpDir, "sess-b", "subagents")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Same message.id:requestId as sess-a's parent message
	writeJSONLFile(t, filepath.Join(agentDir, "agent-x.jsonl"), []string{forkMsg1})

	entries := []models.SessionEntry{
		{SessionID: "sess-a", FullPath: pathA, Modified: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)},
		{SessionID: "sess-b", FullPath: pathB, Modified: time.Date(2024, 1, 16, 9, 0, 0, 0, time.UTC)},
	}

	aggregate, results, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions failed: %v", err)
	}

	// 1 (sess-a parent) + 1 (sess-b parent) + 1 (sess-b agent, not deduped)
	if aggregate.MessageCount != 3 {
		t.Errorf("aggregate MessageCount: got %d, want 3", aggregate.MessageCount)
	}
	// m1 twice (parent + agent) + m3 once = 0.0105*2 + 0.021
	if !almostEqual(aggregate.TotalCost.TotalCost, 0.042, 0.0001) {
		t.Errorf("aggregate TotalCost: got %f, want 0.042", aggregate.TotalCost.TotalCost)
	}
	if results[1].Analysis.AgentMessageCount != 1 {
		t.Errorf("sess-b AgentMessageCount: got %d, want 1", results[1].Analysis.AgentMessageCount)
	}
}
