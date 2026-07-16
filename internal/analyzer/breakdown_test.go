package analyzer

import (
	"os"
	"path/filepath"
	"reflect"
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
	result, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages failed: %v", err)
	}
	messages := result.Messages

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

func TestGetBreakdownMessages_SkippedLines(t *testing.T) {
	tmpDir := t.TempDir()

	sessionID := "skipped-session"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	// 1 valid message + 1 malformed line in the parent
	parentContent := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":50}}}
{not json`

	if err := os.WriteFile(sessionPath, []byte(parentContent), 0644); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatalf("failed to create subagents dir: %v", err)
	}

	// 1 valid message + 2 malformed lines in the agent
	agentContent := `{"type":"assistant","timestamp":"2024-01-15T10:30:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":50,"output_tokens":25}}}
garbage
{"broken`

	if err := os.WriteFile(filepath.Join(subagentsDir, "agent-x1.jsonl"), []byte(agentContent), 0644); err != nil {
		t.Fatalf("failed to write agent file: %v", err)
	}

	result, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages failed: %v", err)
	}
	messages := result.Messages

	if result.SkippedLines != 3 {
		t.Errorf("skippedLines: got %d, want 3 (1 parent + 2 agent)", result.SkippedLines)
	}
	if len(messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(messages))
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
	result, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages failed: %v", err)
	}
	messages := result.Messages

	// Verify total message count (2 parent + 1 agent)
	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
	}

	// Verify chronological ordering (agent message should be in the middle)
	expectedOrder := []string{"", "abc123", ""} // parent, agent, parent
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

	// Verify the agent message carries the real ID from its filename
	if messages[1].AgentID != "abc123" {
		t.Errorf("agent message should have AgentID 'abc123', got %q", messages[1].AgentID)
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

	result, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages failed: %v", err)
	}
	messages := result.Messages

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

	result, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages failed: %v", err)
	}
	messages := result.Messages

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

	// Verify exact cost: 1000 input @ $3/M + 500 output @ $15/M = 0.003 + 0.0075 = 0.0105
	if !almostEqual(msg.Cost.TotalCost, 0.0105, 0.0001) {
		t.Errorf("TotalCost: got %f, want 0.0105", msg.Cost.TotalCost)
	}

	// Verify model is preserved
	if msg.Model != "claude-sonnet-4" {
		t.Errorf("expected model 'claude-sonnet-4', got %q", msg.Model)
	}
}

// BRK-03/D25(a): breakdown insights must be computed over the file-order merged
// list, matching show/watch's parent-only file-order insights. For an agent-free
// session the two lists are identical, so the insights must match exactly even
// when timestamps are out of order (or zero) — a case where the old
// timestamp-sorted computation diverged.
func TestGetBreakdownMessages_InsightsMatchSessionFileOrder(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "insights-order"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	// Six agent-free assistant messages. File order != timestamp order: the last
	// file line has the earliest timestamp, and one line carries no timestamp
	// (parses to the zero time, which sorts to the very front). The first file
	// line is deliberately the most expensive so file-order FirstMessage/Highest
	// differ from what a timestamp sort would pick.
	content := `{"type":"assistant","timestamp":"2024-01-15T10:02:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":5000}}}
{"type":"assistant","timestamp":"2024-01-15T10:03:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":100}}}
{"type":"assistant","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":100}}}
{"type":"assistant","timestamp":"2024-01-15T10:05:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":100}}}
{"type":"assistant","timestamp":"2024-01-15T10:06:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":100}}}
{"type":"assistant","timestamp":"2024-01-15T10:01:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":4000}}}`

	if err := os.WriteFile(sessionPath, []byte(content), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	result, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages: %v", err)
	}
	// The same surface show/watch use: parent-only, file order.
	analysis, err := AnalyzeSession(sessionPath, sessionID, NoMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}

	if !reflect.DeepEqual(result.Insights, analysis.Insights) {
		t.Errorf("breakdown insights diverge from show/watch for an agent-free session\n breakdown: %+v\n show/watch: %+v", result.Insights, analysis.Insights)
	}

	// Pin file-order semantics so a regression to timestamp-sorted insights is
	// caught even if both surfaces drifted together: the first file line (the
	// 5000-output message) must be FirstMessage, not the zero-timestamp line a
	// sort would float to the front.
	if result.Insights == nil || result.Insights.FirstMessage == nil {
		t.Fatal("expected insights with a FirstMessage")
	}
	if result.Insights.FirstMessage.Index != 1 {
		t.Fatalf("FirstMessage index = %d, want 1 (file order)", result.Insights.FirstMessage.Index)
	}
	firstCost := result.Insights.FirstMessage.Cost
	lastCost := result.Insights.LastMessage.Cost
	if firstCost <= lastCost {
		t.Fatalf("expected file-order FirstMessage (5000-output) to cost more than LastMessage; got first=%.6f last=%.6f (insights look timestamp-sorted)", firstCost, lastCost)
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

	result, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages failed: %v", err)
	}
	messages := result.Messages

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
	expectedAgentIDs := []string{"", "test", "", "test", ""}
	for i, msg := range messages {
		if msg.AgentID != expectedAgentIDs[i] {
			t.Errorf("message %d: expected AgentID %q, got %q", i, expectedAgentIDs[i], msg.AgentID)
		}
	}
}

// Equal timestamps must keep the append order (parent rows, then agents in
// discovery order) — the sort is stable, so the merged view is deterministic
// even when a burst of messages shares one timestamp.
func TestGetBreakdownMessages_StableOrderForEqualTimestamps(t *testing.T) {
	tmpDir := t.TempDir()

	sessionID := "equal-ts"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatalf("failed to create subagents dir: %v", err)
	}

	// Every message carries the identical timestamp
	line := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":50}}}`
	lines := func(n int) string {
		s := line
		for i := 1; i < n; i++ {
			s += "\n" + line
		}
		return s
	}

	if err := os.WriteFile(sessionPath, []byte(lines(8)), 0644); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}
	for _, name := range []string{"agent-aaa.jsonl", "agent-bbb.jsonl"} {
		if err := os.WriteFile(filepath.Join(subagentsDir, name), []byte(lines(8)), 0644); err != nil {
			t.Fatalf("failed to write agent file: %v", err)
		}
	}

	var want []string
	for _, id := range []string{"", "aaa", "bbb"} {
		for i := 0; i < 8; i++ {
			want = append(want, id)
		}
	}

	for run := 0; run < 2; run++ {
		result, err := GetBreakdownMessages(sessionPath, sessionID)
		if err != nil {
			t.Fatalf("GetBreakdownMessages failed: %v", err)
		}
		if len(result.Messages) != len(want) {
			t.Fatalf("run %d: expected %d messages, got %d", run, len(want), len(result.Messages))
		}
		for i, msg := range result.Messages {
			if msg.AgentID != want[i] {
				t.Errorf("run %d, message %d: expected AgentID %q, got %q", run, i, want[i], msg.AgentID)
			}
		}
	}
}

func TestGetBreakdownMessages_WithWorkflowAgents(t *testing.T) {
	tmpDir := t.TempDir()

	sessionID := "test-session-wf"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	runDir := filepath.Join(tmpDir, sessionID, "subagents", "workflows", "wf_run-1")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}

	parentContent := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":50}}}
{"type":"assistant","timestamp":"2024-01-15T10:10:00Z","message":{"model":"claude-sonnet-4","usage":{"input_tokens":200,"output_tokens":100}}}`
	if err := os.WriteFile(sessionPath, []byte(parentContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Workflow agent message between the two parent messages
	wfContent := `{"type":"assistant","timestamp":"2024-01-15T10:05:00Z","message":{"model":"claude-opus-4-1","usage":{"input_tokens":50,"output_tokens":25}}}`
	if err := os.WriteFile(filepath.Join(runDir, "agent-w1.jsonl"), []byte(wfContent), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages failed: %v", err)
	}
	messages := result.Messages

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages (2 parent + 1 workflow agent), got %d", len(messages))
	}

	// Chronological merge: parent, workflow agent, parent
	expectedOrder := []string{"", "w1", ""}
	for i, msg := range messages {
		if msg.AgentID != expectedOrder[i] {
			t.Errorf("message %d: expected AgentID %q, got %q", i, expectedOrder[i], msg.AgentID)
		}
	}
}

// AGENT-07: the breakdown TUI is the only surface for its own numbers, so an
// agent it could not read must reach the footer. Both sources count: an agent
// file that fails to parse, and a directory that cannot be listed at all.
func TestGetBreakdownMessages_SkippedAgents(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")
	writeJSONLFile(t, sessionPath, []string{forkMsg1})

	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeJSONLFile(t, filepath.Join(subagentsDir, "agent-good.jsonl"), []string{forkMsg2})

	badAgent := filepath.Join(subagentsDir, "agent-bad.jsonl")
	writeJSONLFile(t, badAgent, []string{forkMsg3})
	if err := os.Chmod(badAgent, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badAgent, 0644) })
	if _, err := os.ReadFile(badAgent); err == nil {
		t.Skip("chmod 000 does not bar reads (running as root?)")
	}

	workflowsDir := filepath.Join(subagentsDir, "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	makeUnreadableDir(t, workflowsDir)

	result, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages: %v", err)
	}
	// 1 unreadable workflows dir + 1 unparseable agent file
	if result.SkippedAgents != 2 {
		t.Errorf("SkippedAgents: got %d, want 2", result.SkippedAgents)
	}
	// Parent message + the one readable agent's message
	if len(result.Messages) != 2 {
		t.Errorf("Messages: got %d, want 2", len(result.Messages))
	}
}

// A healthy session reports nothing skipped, so the footer stays clean.
func TestGetBreakdownMessages_NoSkippedAgents(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")
	writeJSONLFile(t, sessionPath, []string{forkMsg1})
	writeAgentSession(t, tmpDir, sessionID, "agent-a.jsonl", []string{forkMsg2})

	result, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages: %v", err)
	}
	if result.SkippedAgents != 0 || result.SkippedLines != 0 {
		t.Errorf("SkippedAgents=%d SkippedLines=%d, want 0/0",
			result.SkippedAgents, result.SkippedLines)
	}
}
