package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ccusage/internal/models"
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

	result, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions failed: %v", err)
	}

	// MessageCount: 2 + 1 = 3
	if result.MessageCount != 3 {
		t.Errorf("MessageCount: got %d, want 3", result.MessageCount)
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

	_, err := AnalyzeMultipleSessions(entries)
	if err == nil {
		t.Fatal("expected error when all sessions fail")
	}
	if !strings.Contains(err.Error(), "all") {
		t.Errorf("error message should contain 'all', got: %s", err.Error())
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

	result, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SkippedSessions should be 1
	if result.SkippedSessions != 1 {
		t.Errorf("SkippedSessions: got %d, want 1", result.SkippedSessions)
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
