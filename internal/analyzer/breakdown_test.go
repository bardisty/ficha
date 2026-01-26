package analyzer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGetBreakdownMessages(t *testing.T) {
	// Create temp directory for test files
	tmpDir, err := os.MkdirTemp("", "breakdown-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sessionID := "test-session"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	// Create parent session file with two messages
	parentContent := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":50}}}
{"type":"assistant","timestamp":"2024-01-15T10:05:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":150,"output_tokens":75}}}`

	if err := os.WriteFile(sessionPath, []byte(parentContent), 0644); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	// Get breakdown messages
	messages, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages failed: %v", err)
	}

	// Verify message count
	if len(messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(messages))
	}

	// Verify indices are sequential and 1-based
	for i, msg := range messages {
		expectedIndex := i + 1
		if msg.Index != expectedIndex {
			t.Errorf("message %d has index %d, expected %d", i, msg.Index, expectedIndex)
		}
	}

	// Verify parent messages have empty AgentID
	for _, msg := range messages {
		if msg.AgentID != "" {
			t.Errorf("parent message should have empty AgentID, got %q", msg.AgentID)
		}
	}

	// Verify chronological order
	for i := 1; i < len(messages); i++ {
		if messages[i].Timestamp.Before(messages[i-1].Timestamp) {
			t.Error("messages should be in chronological order")
		}
	}
}

func TestGetBreakdownMessages_WithAgents(t *testing.T) {
	// Create temp directory structure
	tmpDir, err := os.MkdirTemp("", "breakdown-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sessionID := "test-session"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	// Create subagents directory
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatalf("failed to create subagents dir: %v", err)
	}

	// Create parent session file
	parentContent := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":50}}}
{"type":"assistant","timestamp":"2024-01-15T10:10:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":200,"output_tokens":100}}}`

	if err := os.WriteFile(sessionPath, []byte(parentContent), 0644); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	// Create agent session file with message between parent messages
	agentContent := `{"type":"assistant","timestamp":"2024-01-15T10:05:00Z","message":{"model":"claude-haiku-4-5","usage":{"input_tokens":50,"output_tokens":25}}}`

	agentPath := filepath.Join(subagentsDir, "agent-abc123.jsonl")
	if err := os.WriteFile(agentPath, []byte(agentContent), 0644); err != nil {
		t.Fatalf("failed to write agent file: %v", err)
	}

	// Get breakdown messages
	messages, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages failed: %v", err)
	}

	// Verify total message count (2 parent + 1 agent)
	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
	}

	// Verify chronological ordering (agent message should be in the middle)
	expectedOrder := []string{"", "1", ""} // parent, agent, parent
	for i, msg := range messages {
		if msg.AgentID != expectedOrder[i] {
			t.Errorf("message %d: expected AgentID %q, got %q", i, expectedOrder[i], msg.AgentID)
		}
	}

	// Verify indices are sequential
	for i, msg := range messages {
		if msg.Index != i+1 {
			t.Errorf("message %d has index %d, expected %d", i, msg.Index, i+1)
		}
	}

	// Verify agent message has proper AgentID
	if messages[1].AgentID != "1" {
		t.Errorf("agent message should have AgentID '1', got %q", messages[1].AgentID)
	}
}

func TestGetBreakdownMessages_EmptySession(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "breakdown-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sessionID := "empty-session"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	// Create empty session file
	if err := os.WriteFile(sessionPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	messages, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages failed: %v", err)
	}

	if len(messages) != 0 {
		t.Errorf("expected 0 messages for empty session, got %d", len(messages))
	}
}

func TestGetBreakdownMessages_CostCalculation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "breakdown-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sessionID := "cost-test"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	// Create session with known token counts
	content := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":1000,"output_tokens":500}}}`

	if err := os.WriteFile(sessionPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	messages, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages failed: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	msg := messages[0]

	// Verify usage data is preserved
	if msg.Usage.InputTokens != 1000 {
		t.Errorf("expected 1000 input tokens, got %d", msg.Usage.InputTokens)
	}
	if msg.Usage.OutputTokens != 500 {
		t.Errorf("expected 500 output tokens, got %d", msg.Usage.OutputTokens)
	}

	// Verify cost was calculated (should be > 0 for non-zero tokens)
	if msg.Cost.TotalCost <= 0 {
		t.Error("expected positive total cost")
	}

	// Verify model is preserved
	if msg.Model != "claude-sonnet-4" {
		t.Errorf("expected model 'claude-sonnet-4', got %q", msg.Model)
	}
}

func TestGetBreakdownMessages_ChronologicalMerge(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "breakdown-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sessionID := "merge-test"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	// Create subagents directory
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatalf("failed to create subagents dir: %v", err)
	}

	// Parent messages at 10:00, 10:20, 10:40
	parentContent := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":50}}}
{"type":"assistant","timestamp":"2024-01-15T10:20:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":50}}}
{"type":"assistant","timestamp":"2024-01-15T10:40:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":50}}}`

	if err := os.WriteFile(sessionPath, []byte(parentContent), 0644); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	// Agent messages at 10:10, 10:30
	agentContent := `{"type":"assistant","timestamp":"2024-01-15T10:10:00Z","message":{"model":"claude-haiku-4-5","usage":{"input_tokens":50,"output_tokens":25}}}
{"type":"assistant","timestamp":"2024-01-15T10:30:00Z","message":{"model":"claude-haiku-4-5","usage":{"input_tokens":50,"output_tokens":25}}}`

	agentPath := filepath.Join(subagentsDir, "agent-test.jsonl")
	if err := os.WriteFile(agentPath, []byte(agentContent), 0644); err != nil {
		t.Fatalf("failed to write agent file: %v", err)
	}

	messages, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages failed: %v", err)
	}

	// Should have 5 messages total
	if len(messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(messages))
	}

	// Verify strict chronological order
	expectedTimes := []time.Time{
		time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 15, 10, 10, 0, 0, time.UTC),
		time.Date(2024, 1, 15, 10, 20, 0, 0, time.UTC),
		time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		time.Date(2024, 1, 15, 10, 40, 0, 0, time.UTC),
	}

	for i, msg := range messages {
		if !msg.Timestamp.Equal(expectedTimes[i]) {
			t.Errorf("message %d: expected time %v, got %v", i, expectedTimes[i], msg.Timestamp)
		}
	}

	// Verify alternating pattern: parent(10:00), agent(10:10), parent(10:20), agent(10:30), parent(10:40)
	expectedAgentIDs := []string{"", "1", "", "1", ""}
	for i, msg := range messages {
		if msg.AgentID != expectedAgentIDs[i] {
			t.Errorf("message %d: expected AgentID %q, got %q", i, expectedAgentIDs[i], msg.AgentID)
		}
	}
}
