package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
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

// assistantMsg builds a one-message JSONL entry billed to model.
func assistantMsg(model string, inputTokens int64) models.JSONLMessage {
	return models.JSONLMessage{
		Type:      "assistant",
		Timestamp: time.Now(),
		Message: &models.AssistantMessage{
			Model: model,
			Usage: models.TokenUsage{InputTokens: inputTokens},
		},
	}
}

// CostByModel keys on the canonical catalog ID, so every dated snapshot and
// provider spelling of one model shares a row. Keying on the raw ID split one
// model's cost across rows: the COST BY MODEL section printed the same display
// name twice, and PrimaryModel could crown a model that cost less in total.
func TestCostByModelKeysAreNormalized(t *testing.T) {
	// Two dated snapshots of Sonnet 4.5 ($3/M in) at $3.00 each, against one
	// Opus 4.5 ($5/M in) at $5.00. Split, Opus wins each row; merged, Sonnet
	// leads $6.00 to $5.00.
	analysis := AnalyzeSessionFromMessages("s", "/p", []models.JSONLMessage{
		assistantMsg("claude-sonnet-4-5-20250929", 1_000_000),
		assistantMsg("claude-sonnet-4-5-20251119", 1_000_000),
		assistantMsg("claude-opus-4-5", 1_000_000),
	}, false)

	want := map[string]float64{
		"claude-sonnet-4-5": 6.00,
		"claude-opus-4-5":   5.00,
	}
	if len(analysis.CostByModel) != len(want) {
		t.Fatalf("CostByModel has %d keys (%v), want %d", len(analysis.CostByModel), analysis.CostByModel, len(want))
	}
	for model, wantCost := range want {
		got, ok := analysis.CostByModel[model]
		if !ok {
			t.Fatalf("CostByModel missing key %q, got %v", model, analysis.CostByModel)
		}
		if !almostEqual(got.TotalCost, wantCost, 0.0001) {
			t.Errorf("CostByModel[%q].TotalCost = %f, want %f", model, got.TotalCost, wantCost)
		}
	}

	// Per-message rows keep the raw ID: normalization is an aggregation concern.
	if analysis.LastMessageModel != "claude-opus-4-5" {
		t.Errorf("LastMessageModel = %q, want the raw ID", analysis.LastMessageModel)
	}
}

// Claude Code writes synthetic API-error lines (model "<synthetic>",
// all-zero usage) that can end a transcript. Captured as the positional last
// message they zero the CONTEXT readout even though real context exists, so
// last-message capture walks back to the last message that carries context.
func TestLastMessageSkipsTrailingSyntheticLines(t *testing.T) {
	analysis := AnalyzeSessionFromMessages("s", "/p", []models.JSONLMessage{
		assistantMsg("claude-opus-4-5", 500),
		assistantMsg("claude-sonnet-4-5", 1500),
		assistantMsg("<synthetic>", 0),
		assistantMsg("<synthetic>", 0),
	}, false)

	if analysis.LastMessageModel != "claude-sonnet-4-5" {
		t.Errorf("LastMessageModel = %q, want the last real message's model", analysis.LastMessageModel)
	}
	if got := analysis.LastMessageUsage.ContextWindowSize(); got != 1500 {
		t.Errorf("LastMessageUsage context = %d, want 1500 (last real message)", got)
	}
	// Synthetic lines stay part of the transcript (dedup/cost/count).
	if analysis.MessageCount != 4 {
		t.Errorf("MessageCount = %d, want 4 (synthetic lines stay counted)", analysis.MessageCount)
	}
}

// An all-synthetic session carries no context anywhere, so the last-message
// capture degrades to zero exactly as before the walk-back was added.
func TestLastMessageAllSyntheticDegradesToZero(t *testing.T) {
	analysis := AnalyzeSessionFromMessages("s", "/p", []models.JSONLMessage{
		assistantMsg("<synthetic>", 0),
		assistantMsg("<synthetic>", 0),
	}, false)

	if got := analysis.LastMessageUsage.ContextWindowSize(); got != 0 {
		t.Errorf("LastMessageUsage context = %d, want 0", got)
	}
	if analysis.LastMessageModel != "<synthetic>" {
		t.Errorf("LastMessageModel = %q, want %q (positional last, unchanged)", analysis.LastMessageModel, "<synthetic>")
	}
}

// Decorated spellings of one model (Vertex '@date', Bedrock profile+version,
// the 1M-context beta marker) all resolve to the same catalog row, so they must
// not each open a CostByModel key. Unknown models keep their raw ID — nothing
// ficha cannot price is silently merged into a row it can.
func TestCostByModelMergesDecoratedIDsAndKeepsUnknownRaw(t *testing.T) {
	analysis := AnalyzeSessionFromMessages("s", "/p", []models.JSONLMessage{
		assistantMsg("claude-opus-4-8", 1_000_000),
		assistantMsg("claude-opus-4-8[1m]", 1_000_000),
		assistantMsg("claude-opus-4-8@20251101", 1_000_000),
		assistantMsg("us.anthropic.claude-opus-4-8-20251101-v1:0", 1_000_000),
		assistantMsg("claude-opus-4-9", 1_000_000),     // unknown family version
		assistantMsg("claude-opus-4-9", 1_000_000),     // ... aggregates with itself
		assistantMsg("claude-opus-4-9[2m]", 1_000_000), // unrecognized decorator: its own row
	}, false)

	// 4 × Opus 4.8 @ $5/M = $20.00 in one row.
	opus48, ok := analysis.CostByModel["claude-opus-4-8"]
	if !ok {
		t.Fatalf("CostByModel missing claude-opus-4-8, got %v", analysis.CostByModel)
	}
	if !almostEqual(opus48.TotalCost, 20.00, 0.0001) {
		t.Errorf("claude-opus-4-8 TotalCost = %f, want 20.00 (all four spellings merged)", opus48.TotalCost)
	}

	// 2 × unknown @ the $3/M default = $6.00, still under the raw ID.
	unknown, ok := analysis.CostByModel["claude-opus-4-9"]
	if !ok {
		t.Fatalf("CostByModel missing claude-opus-4-9, got %v", analysis.CostByModel)
	}
	if !almostEqual(unknown.TotalCost, 6.00, 0.0001) {
		t.Errorf("claude-opus-4-9 TotalCost = %f, want 6.00", unknown.TotalCost)
	}
	if _, ok := analysis.CostByModel["claude-opus-4-9[2m]"]; !ok {
		t.Errorf("an unrecognized decorator must keep its own raw key, got %v", analysis.CostByModel)
	}
	if len(analysis.CostByModel) != 3 {
		t.Errorf("CostByModel has %d keys (%v), want 3", len(analysis.CostByModel), analysis.CostByModel)
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

	analysis, err := AnalyzeSession(sessionPath, sessionID, NoMessages)
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

	analysis, err := AnalyzeSession(sessionPath, sessionID, NoMessages)
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

	analysis, err := AnalyzeSession(sessionPath, sessionID, NoMessages)
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

	analysis, err := AnalyzeSession(sessionPath, sessionID, NoMessages)
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

	analysis, err := AnalyzeAgent(agentPath)
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

	analysis, err := AnalyzeSession(sessionPath, sessionID, NoMessages)
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

	analysis, err := AnalyzeSession(sessionPath, sessionID, NoMessages)
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
	// Carries a cache_creation block, so Usage.CacheCreation is a non-nil pointer.
	cacheCreationMsg = `{"type":"assistant","requestId":"req_4","timestamp":"2024-01-15T10:20:00Z","message":{"id":"msg_4","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":800,"cache_creation":{"ephemeral_5m_input_tokens":800,"ephemeral_1h_input_tokens":0}}}}`
)

func TestAnalyzeMultipleSessions_CrossFileDedup(t *testing.T) {
	tmpDir := t.TempDir()

	originalPath := filepath.Join(tmpDir, "original.jsonl")
	writeJSONLFile(t, originalPath, []string{forkMsg1, forkMsg2})

	forkPath := filepath.Join(tmpDir, "fork.jsonl")
	writeJSONLFile(t, forkPath, []string{forkMsg1, forkMsg2, forkMsg3})

	// Fork first in input order: the earliest-timestamp keys tie (the fork
	// clones the original's lines, timestamps included) and no Created is
	// set, so attribution falls back to mtime — the original (earlier mtime)
	// keeps the shared history; results stay in input order.
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

	analysis, err := AnalyzeSession(forkPath, "fork", NoMessages)
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

// Keeping work in the original session after forking pushes its mtime past
// the fork's, so mtime-ordered attribution would hand the fork the shared
// history. The Created tie-break (session creation time from the
// index) must keep the original first regardless of who was written to last.
func TestAnalyzeMultipleSessions_ForkAttribution_OriginalMtimeLater(t *testing.T) {
	tmpDir := t.TempDir()

	// The user continued in the original after forking: it holds the shared
	// m1+m2 plus post-fork m3, and its mtime is the latest of the pair.
	originalPath := filepath.Join(tmpDir, "original.jsonl")
	writeJSONLFile(t, originalPath, []string{forkMsg1, forkMsg2, forkMsg3})

	// The abandoned fork holds only verbatim clones of m1+m2, so both files
	// share the same earliest message timestamp.
	forkPath := filepath.Join(tmpDir, "fork.jsonl")
	writeJSONLFile(t, forkPath, []string{forkMsg1, forkMsg2})

	entries := []models.SessionEntry{
		{
			SessionID: "fork", FullPath: forkPath,
			Created:  time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			Modified: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			SessionID: "original", FullPath: originalPath,
			Created:  time.Date(2024, 1, 15, 9, 55, 0, 0, time.UTC),
			Modified: time.Date(2024, 1, 16, 10, 0, 0, 0, time.UTC),
		},
	}

	aggregate, results, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions failed: %v", err)
	}

	if aggregate.MessageCount != 3 {
		t.Errorf("aggregate MessageCount: got %d, want 3", aggregate.MessageCount)
	}
	fork, original := results[0], results[1]
	if original.Analysis.MessageCount != 3 {
		t.Errorf("original MessageCount: got %d, want 3 (owns the shared history)", original.Analysis.MessageCount)
	}
	if !almostEqual(original.Analysis.TotalCost.TotalCost, 0.036, 0.0001) {
		t.Errorf("original TotalCost: got %f, want 0.036", original.Analysis.TotalCost.TotalCost)
	}
	if fork.Analysis.MessageCount != 0 {
		t.Errorf("fork MessageCount: got %d, want 0 (its lines are all clones)", fork.Analysis.MessageCount)
	}
	if !almostEqual(fork.Analysis.TotalCost.TotalCost, 0, 0.0001) {
		t.Errorf("fork TotalCost: got %f, want 0", fork.Analysis.TotalCost.TotalCost)
	}
}

// Index-sourced entries can carry zero Modified times. Ordering must come
// from the message timestamps — deterministic, no panic, and no arbitrary
// grab of the shared history by whichever entry sorted first.
func TestAnalyzeMultipleSessions_ZeroModifiedOrdersByMessageTime(t *testing.T) {
	tmpDir := t.TempDir()

	// The continuation holds a clone of m2 but not m1 (compact-style fork),
	// so the two files differ in earliest message timestamp.
	originalPath := filepath.Join(tmpDir, "original.jsonl")
	writeJSONLFile(t, originalPath, []string{forkMsg1, forkMsg2})

	contPath := filepath.Join(tmpDir, "continuation.jsonl")
	writeJSONLFile(t, contPath, []string{forkMsg2, forkMsg3})

	// Continuation first in input order, all Modified/Created zero.
	entries := []models.SessionEntry{
		{SessionID: "continuation", FullPath: contPath},
		{SessionID: "original", FullPath: originalPath},
	}

	aggregate, results, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions failed: %v", err)
	}

	if aggregate.MessageCount != 3 {
		t.Errorf("aggregate MessageCount: got %d, want 3", aggregate.MessageCount)
	}
	cont, original := results[0], results[1]
	if original.Analysis.MessageCount != 2 {
		t.Errorf("original MessageCount: got %d, want 2 (earlier first message wins m2)", original.Analysis.MessageCount)
	}
	if cont.Analysis.MessageCount != 1 {
		t.Errorf("continuation MessageCount: got %d, want 1 (novel m3 only)", cont.Analysis.MessageCount)
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

// A session whose parent transcript cannot be read still has agent
// sub-sessions on disk that `list` counts. Their spend never enters the
// aggregate (the parent parse fails before agent analysis), so it must be
// disclosed through SkippedAgents — not vanish behind a generic
// session-could-not-be-parsed warning with skipped-agent count 0.
func TestAnalyzeMultipleSessions_UnreadableParentDisclosesAgents(t *testing.T) {
	tmpDir := t.TempDir()

	okPath := filepath.Join(tmpDir, "sess-ok.jsonl")
	writeJSONLFile(t, okPath, []string{forkMsg1})

	badPath := filepath.Join(tmpDir, "sess-bad.jsonl")
	writeJSONLFile(t, badPath, []string{forkMsg3})
	writeAgentSession(t, tmpDir, "sess-bad", "agent-a1.jsonl", []string{
		`{"type":"assistant","requestId":"req_a1","timestamp":"2024-01-15T10:05:00Z","message":{"id":"msg_a1","model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`,
	})
	writeAgentSession(t, tmpDir, "sess-bad", "agent-a2.jsonl", []string{
		`{"type":"assistant","requestId":"req_a2","timestamp":"2024-01-15T10:06:00Z","message":{"id":"msg_a2","model":"claude-sonnet-4-5","usage":{"input_tokens":1000,"output_tokens":500}}}`,
	})
	if err := os.Chmod(badPath, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badPath, 0644) })
	if _, err := os.ReadFile(badPath); err == nil {
		t.Skip("chmod 000 does not bar reads (running as root?)")
	}

	// Build entries the way `list` does, so the two surfaces can be compared.
	entries, err := parser.DiscoverSessionsFromDisk(tmpDir, true)
	if err != nil {
		t.Fatal(err)
	}
	var badEntry *models.SessionEntry
	for i := range entries {
		if entries[i].SessionID == "sess-bad" {
			badEntry = &entries[i]
		}
	}
	if badEntry == nil {
		t.Fatal("sess-bad not discovered")
	}
	// list still shows the readable agents' activity for the unreadable parent
	if badEntry.AgentMessageCount != 2 {
		t.Fatalf("list-side AgentMessageCount: got %d, want 2", badEntry.AgentMessageCount)
	}

	aggregate, results, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions failed: %v", err)
	}

	if aggregate.SkippedSessions != 1 {
		t.Errorf("SkippedSessions: got %d, want 1", aggregate.SkippedSessions)
	}
	// ...and summary discloses those same agents as skipped, matching list.
	if aggregate.SkippedAgents != 2 {
		t.Errorf("SkippedAgents: got %d, want 2 (readable agents of the unreadable parent)", aggregate.SkippedAgents)
	}
	// Only the readable session's spend is counted: m1 = 0.0105.
	if !almostEqual(aggregate.TotalCost.TotalCost, 0.0105, 0.0001) {
		t.Errorf("TotalCost: got %f, want 0.0105 (agent spend excluded, disclosed)", aggregate.TotalCost.TotalCost)
	}
	for _, r := range results {
		if r.Entry.SessionID == "sess-bad" && r.Analysis != nil {
			t.Error("sess-bad should have nil Analysis")
		}
	}
}

// writeAgentSession writes an agent transcript for sessionID under projectDir.
func writeAgentSession(t *testing.T, projectDir, sessionID, agentFile string, lines []string) {
	t.Helper()
	dir := filepath.Join(projectDir, sessionID, "subagents")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir subagents: %v", err)
	}
	writeJSONLFile(t, filepath.Join(dir, agentFile), lines)
}

// The summary aggregate must carry the same parent/agent partition each session
// carries: machine formats serialize it whole, so a zeroed partition beside a
// total that includes agent spend is a lie.
func TestAnalyzeMultipleSessions_AggregatesAgentPartition(t *testing.T) {
	tmpDir := t.TempDir()

	// sess-a: parent only. sess-b: parent + two agents.
	pathA := filepath.Join(tmpDir, "sess-a.jsonl")
	writeJSONLFile(t, pathA, []string{forkMsg1})

	pathB := filepath.Join(tmpDir, "sess-b.jsonl")
	writeJSONLFile(t, pathB, []string{forkMsg3})
	writeAgentSession(t, tmpDir, "sess-b", "agent-x.jsonl", []string{forkMsg2})
	writeAgentSession(t, tmpDir, "sess-b", "agent-y.jsonl", []string{forkMsg2})

	entries := []models.SessionEntry{
		{SessionID: "sess-a", FullPath: pathA, Modified: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)},
		{SessionID: "sess-b", FullPath: pathB, Modified: time.Date(2024, 1, 16, 9, 0, 0, 0, time.UTC)},
	}

	aggregate, results, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions failed: %v", err)
	}

	if !aggregate.HasAgents {
		t.Error("aggregate HasAgents: got false, want true (sess-b owns agents)")
	}
	if aggregate.AgentCount != 2 {
		t.Errorf("aggregate AgentCount: got %d, want 2", aggregate.AgentCount)
	}

	// The invariant machine consumers rely on: the partition covers the total.
	if !almostEqual(aggregate.ParentCost.TotalCost+aggregate.AgentsCost.TotalCost,
		aggregate.TotalCost.TotalCost, 1e-9) {
		t.Errorf("ParentCost (%f) + AgentsCost (%f) != TotalCost (%f)",
			aggregate.ParentCost.TotalCost, aggregate.AgentsCost.TotalCost, aggregate.TotalCost.TotalCost)
	}

	// And each half is the sum of its per-session halves.
	var wantParent, wantAgents float64
	for _, r := range results {
		wantParent += r.Analysis.ParentCost.TotalCost
		wantAgents += r.Analysis.AgentsCost.TotalCost
	}
	if !almostEqual(aggregate.ParentCost.TotalCost, wantParent, 1e-9) {
		t.Errorf("aggregate ParentCost: got %f, want %f", aggregate.ParentCost.TotalCost, wantParent)
	}
	if !almostEqual(aggregate.AgentsCost.TotalCost, wantAgents, 1e-9) {
		t.Errorf("aggregate AgentsCost: got %f, want %f", aggregate.AgentsCost.TotalCost, wantAgents)
	}

	// ParentCostByModel excludes agent spend, so it must be lighter than CostByModel
	// for the model both share.
	const model = "claude-sonnet-4-5"
	if len(aggregate.ParentCostByModel) == 0 {
		t.Fatal("aggregate ParentCostByModel is empty")
	}
	if aggregate.ParentCostByModel[model].TotalCost >= aggregate.CostByModel[model].TotalCost {
		t.Errorf("ParentCostByModel[%s] (%f) should be below CostByModel[%s] (%f)",
			model, aggregate.ParentCostByModel[model].TotalCost,
			model, aggregate.CostByModel[model].TotalCost)
	}
}

// project_path must name the project directory (not the transcript
// path), and session_file must carry the transcript. AnalyzeSession (the show
// path) sets both.
func TestAnalyzeSession_PopulatesProjectPathAndSessionFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sess.jsonl")
	writeJSONLFile(t, path, []string{forkMsg1})

	analysis, err := AnalyzeSession(path, "sess", NoMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession failed: %v", err)
	}
	if analysis.ProjectPath != tmpDir {
		t.Errorf("ProjectPath: got %q, want the project dir %q", analysis.ProjectPath, tmpDir)
	}
	if analysis.SessionFile != path {
		t.Errorf("SessionFile: got %q, want the transcript %q", analysis.SessionFile, path)
	}
}

// The summary aggregate names the project (project_path) so machine
// consumers can join it to the per-session records, and its session_file is
// empty because it spans many files. Each per-session record repeats the same
// project_path and carries its own session_file.
func TestAnalyzeMultipleSessions_ProjectPathJoinsRecords(t *testing.T) {
	tmpDir := t.TempDir()
	pathA := filepath.Join(tmpDir, "sess-a.jsonl")
	writeJSONLFile(t, pathA, []string{forkMsg1})
	pathB := filepath.Join(tmpDir, "sess-b.jsonl")
	writeJSONLFile(t, pathB, []string{forkMsg3})

	entries := []models.SessionEntry{
		{SessionID: "sess-a", FullPath: pathA},
		{SessionID: "sess-b", FullPath: pathB},
	}

	aggregate, results, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions failed: %v", err)
	}

	if aggregate.ProjectPath != tmpDir {
		t.Errorf("aggregate ProjectPath: got %q, want %q", aggregate.ProjectPath, tmpDir)
	}
	if aggregate.SessionFile != "" {
		t.Errorf("aggregate SessionFile: got %q, want empty (spans many files)", aggregate.SessionFile)
	}
	for i, r := range results {
		if r.Analysis == nil {
			t.Fatalf("results[%d].Analysis is nil", i)
		}
		if r.Analysis.ProjectPath != aggregate.ProjectPath {
			t.Errorf("results[%d].ProjectPath %q != aggregate %q (not joinable)",
				i, r.Analysis.ProjectPath, aggregate.ProjectPath)
		}
		if r.Analysis.SessionFile != entries[i].FullPath {
			t.Errorf("results[%d].SessionFile: got %q, want %q", i, r.Analysis.SessionFile, entries[i].FullPath)
		}
	}
}

// A symlinked workflow run dir's spend must reach the session total
// and not inflate SkippedAgents; a broken-symlink run dir must be disclosed.
func TestAnalyzeSession_SymlinkedWorkflowRunDirCounted(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sess.jsonl")
	writeJSONLFile(t, path, []string{forkMsg1})

	// Parent-only baseline cost, for comparison.
	base, err := AnalyzeSession(path, "sess", NoMessages)
	if err != nil {
		t.Fatalf("baseline AnalyzeSession failed: %v", err)
	}

	// Relocate a workflow run dir outside the session tree and symlink it back.
	workflowsDir := filepath.Join(tmpDir, "sess", "subagents", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(tmpDir, "external-run")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	writeJSONLFile(t, filepath.Join(target, "agent-w1.jsonl"), []string{forkMsg2})
	if err := os.Symlink(target, filepath.Join(workflowsDir, "wf_linked")); err != nil {
		t.Skipf("symlink creation not supported: %v", err)
	}

	got, err := AnalyzeSession(path, "sess", NoMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession failed: %v", err)
	}
	if got.SkippedAgents != 0 {
		t.Errorf("SkippedAgents: got %d, want 0 (symlinked run dir is readable)", got.SkippedAgents)
	}
	if got.AgentCount != 1 {
		t.Errorf("AgentCount: got %d, want 1", got.AgentCount)
	}
	if got.TotalCost.TotalCost <= base.TotalCost.TotalCost {
		t.Errorf("TotalCost %f should exceed the parent-only baseline %f — the linked run's spend is missing",
			got.TotalCost.TotalCost, base.TotalCost.TotalCost)
	}
}

func TestAnalyzeSession_BrokenSymlinkRunDirSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sess.jsonl")
	writeJSONLFile(t, path, []string{forkMsg1})

	workflowsDir := filepath.Join(tmpDir, "sess", "subagents", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(tmpDir, "gone"), filepath.Join(workflowsDir, "wf_broken")); err != nil {
		t.Skipf("symlink creation not supported: %v", err)
	}

	got, err := AnalyzeSession(path, "sess", NoMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession failed: %v", err)
	}
	if got.SkippedAgents != 1 {
		t.Errorf("SkippedAgents: got %d, want 1 (broken-symlink run dir disclosed)", got.SkippedAgents)
	}
}

// A workflow run's agents contribute to the aggregate's WorkflowCount.
func TestAnalyzeMultipleSessions_AggregatesWorkflowCount(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sess.jsonl")
	writeJSONLFile(t, path, []string{forkMsg1})

	runDir := filepath.Join(tmpDir, "sess", "subagents", "workflows", "wf_run1")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeJSONLFile(t, filepath.Join(runDir, "agent-w1.jsonl"), []string{forkMsg2})

	aggregate, _, err := AnalyzeMultipleSessions([]models.SessionEntry{
		{SessionID: "sess", FullPath: path, Modified: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions failed: %v", err)
	}
	if aggregate.WorkflowCount != 1 {
		t.Errorf("aggregate WorkflowCount: got %d, want 1", aggregate.WorkflowCount)
	}
}

// AllMessages appends each agent's messages after the parent block, tagged with
// the agent ID, so per-message rows sum to the session total.
func TestAnalyzeSession_MessageScope(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "sess.jsonl")
	writeJSONLFile(t, sessionPath, []string{forkMsg1, forkMsg2})
	writeAgentSession(t, tmpDir, "sess", "agent-x.jsonl", []string{forkMsg3})

	t.Run("NoMessages", func(t *testing.T) {
		analysis, err := AnalyzeSession(sessionPath, "sess", NoMessages)
		if err != nil {
			t.Fatalf("AnalyzeSession: %v", err)
		}
		if len(analysis.Messages) != 0 {
			t.Errorf("Messages: got %d, want 0", len(analysis.Messages))
		}
	})

	t.Run("AllMessages", func(t *testing.T) {
		analysis, err := AnalyzeSession(sessionPath, "sess", AllMessages)
		if err != nil {
			t.Fatalf("AnalyzeSession: %v", err)
		}
		if len(analysis.Messages) != 3 {
			t.Fatalf("Messages: got %d, want 3 (2 parent + 1 agent)", len(analysis.Messages))
		}
		wantAgentIDs := []string{"", "", "x"}
		var sum float64
		for i, msg := range analysis.Messages {
			if msg.AgentID != wantAgentIDs[i] {
				t.Errorf("Messages[%d].AgentID: got %q, want %q", i, msg.AgentID, wantAgentIDs[i])
			}
			sum += msg.Cost.TotalCost
		}
		// Rows must sum to the session total.
		if !almostEqual(sum, analysis.TotalCost.TotalCost, 1e-9) {
			t.Errorf("sum of message costs: got %f, want TotalCost %f", sum, analysis.TotalCost.TotalCost)
		}
		if analysis.Messages[2].AgentID != analysis.Agents[0].AgentID {
			t.Errorf("agent message tag %q does not join to Agents[0].AgentID %q",
				analysis.Messages[2].AgentID, analysis.Agents[0].AgentID)
		}
	})
}

// The parse cache hands back its own slice; tagging agent messages must not
// write an AgentID into the cached entry, and must not alias the cached
// Usage.CacheCreation pointer (MessageAnalysis copies shallowly). The exported
// analysis is checked against the cache directly — asserting on a later pass
// that omits agent rows would pass vacuously.
func TestAnalyzeSession_AllMessagesDoesNotMutateCache(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "sess.jsonl")
	writeJSONLFile(t, sessionPath, []string{forkMsg1})
	writeAgentSession(t, tmpDir, "sess", "agent-x.jsonl", []string{cacheCreationMsg})
	agentPath := filepath.Join(tmpDir, "sess", "subagents", "agent-x.jsonl")

	cache := NewAgentParseCache()
	analysis, err := AnalyzeSessionWithCache(sessionPath, "sess", AllMessages, cache)
	if err != nil {
		t.Fatalf("AnalyzeSessionWithCache: %v", err)
	}

	cached, _, err := loadAgentMessages(agentPath, cache)
	if err != nil {
		t.Fatalf("loadAgentMessages: %v", err)
	}
	if len(cached) != 1 {
		t.Fatalf("cached messages: got %d, want 1", len(cached))
	}
	if cached[0].AgentID != "" {
		t.Errorf("cached AgentID: got %q, want empty (tagging mutated the cache)", cached[0].AgentID)
	}

	// The exported row must own its CacheCreation, not point at the cache's.
	var exported *models.MessageAnalysis
	for i := range analysis.Messages {
		if analysis.Messages[i].AgentID == "x" {
			exported = &analysis.Messages[i]
		}
	}
	if exported == nil {
		t.Fatal("no agent row in the export")
	}
	if exported.Usage.CacheCreation == nil || cached[0].Usage.CacheCreation == nil {
		t.Fatal("fixture must carry a cache_creation block")
	}
	if exported.Usage.CacheCreation == cached[0].Usage.CacheCreation {
		t.Error("exported row aliases the cache's CacheCreation pointer")
	}
}

// A session whose parent transcript has no billable messages still exports its
// agents' rows: buildSessionAnalysis returns early before assigning Messages,
// so the agent rows append onto a nil slice.
func TestAnalyzeSession_AllMessagesNoParentMessages(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "sess.jsonl")
	writeJSONLFile(t, sessionPath, []string{`{"type":"user","timestamp":"2024-01-15T10:00:00Z"}`})
	writeAgentSession(t, tmpDir, "sess", "agent-x.jsonl", []string{forkMsg2})

	analysis, err := AnalyzeSession(sessionPath, "sess", AllMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	if len(analysis.Messages) != 1 {
		t.Fatalf("Messages: got %d, want 1 (agent only)", len(analysis.Messages))
	}
	if analysis.Messages[0].AgentID != "x" {
		t.Errorf("AgentID: got %q, want %q", analysis.Messages[0].AgentID, "x")
	}
	if !almostEqual(analysis.Messages[0].Cost.TotalCost, analysis.TotalCost.TotalCost, 1e-9) {
		t.Errorf("row cost %f != TotalCost %f", analysis.Messages[0].Cost.TotalCost, analysis.TotalCost.TotalCost)
	}
}

// An agent that fails to parse still counts toward AgentCount but contributes
// no rows and no cost, so the rows-sum-to-total invariant survives it.
func TestAnalyzeSession_AllMessagesSkipsUnparseableAgent(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "sess.jsonl")
	writeJSONLFile(t, sessionPath, []string{forkMsg1})
	writeAgentSession(t, tmpDir, "sess", "agent-good.jsonl", []string{forkMsg2})
	// An unreadable agent file is discovered but fails to parse.
	writeAgentSession(t, tmpDir, "sess", "agent-bad.jsonl", []string{forkMsg3})
	badPath := filepath.Join(tmpDir, "sess", "subagents", "agent-bad.jsonl")
	if err := os.Chmod(badPath, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badPath, 0644) })
	if _, err := os.ReadFile(badPath); err == nil {
		t.Skip("chmod 000 does not bar reads (running as root?)")
	}

	analysis, err := AnalyzeSession(sessionPath, "sess", AllMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	// AgentCount describes the agents actually analyzed, so it matches the
	// Agents slice every exporter derives its rows from; the unreadable one is
	// in SkippedAgents.
	if analysis.AgentCount != 1 || len(analysis.Agents) != 1 || analysis.SkippedAgents != 1 {
		t.Errorf("AgentCount=%d len(Agents)=%d SkippedAgents=%d, want 1/1/1",
			analysis.AgentCount, len(analysis.Agents), analysis.SkippedAgents)
	}

	var sum float64
	for _, msg := range analysis.Messages {
		if msg.AgentID == "bad" {
			t.Error("the skipped agent leaked a message row")
		}
		sum += msg.Cost.TotalCost
	}
	if !almostEqual(sum, analysis.TotalCost.TotalCost, 1e-9) {
		t.Errorf("row sum %f != TotalCost %f with a skipped agent", sum, analysis.TotalCost.TotalCost)
	}
}

// makeUnreadableDir strips every permission bit from dir for the test's
// duration. Skips only where chmod 000 still permits reads (root, or a
// filesystem without POSIX modes).
func makeUnreadableDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0000); err != nil {
		t.Fatalf("chmod 000 %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("chmod 000 does not bar directory reads (running as root?)")
	}
}

// An unreadable subagents/ dir must count as skipped: HasAgents=false and
// SkippedAgents=0 would make a session that silently lost every agent's cost
// look exactly like a session that never had one.
func TestAnalyzeSession_UnreadableSubagentsDirCountsAsSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "sess.jsonl")
	writeJSONLFile(t, sessionPath, []string{forkMsg1})
	writeAgentSession(t, tmpDir, "sess", "agent-a.jsonl", []string{forkMsg2})
	makeUnreadableDir(t, filepath.Join(tmpDir, "sess", "subagents"))

	analysis, err := AnalyzeSession(sessionPath, "sess", NoMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	if analysis.SkippedAgents != 1 {
		t.Errorf("SkippedAgents: got %d, want 1", analysis.SkippedAgents)
	}
	if analysis.AgentCount != 0 || len(analysis.Agents) != 0 {
		t.Errorf("AgentCount=%d len(Agents)=%d, want 0/0 (none could be read)",
			analysis.AgentCount, len(analysis.Agents))
	}
}

// The same accounting when only the workflows/ dir is unreadable: the regular
// subagent beside it still analyzes.
func TestAnalyzeSession_UnreadableWorkflowsDirCountsAsSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "sess.jsonl")
	writeJSONLFile(t, sessionPath, []string{forkMsg1})
	writeAgentSession(t, tmpDir, "sess", "agent-a.jsonl", []string{forkMsg2})
	workflowsDir := filepath.Join(tmpDir, "sess", "subagents", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	makeUnreadableDir(t, workflowsDir)

	analysis, err := AnalyzeSession(sessionPath, "sess", NoMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	if analysis.SkippedAgents != 1 {
		t.Errorf("SkippedAgents: got %d, want 1", analysis.SkippedAgents)
	}
	if analysis.AgentCount != 1 || !analysis.HasAgents {
		t.Errorf("AgentCount=%d HasAgents=%v, want 1/true", analysis.AgentCount, analysis.HasAgents)
	}
}

// A session with no agent trouble must report nothing skipped.
func TestAnalyzeSession_HealthyAgentsReportNoSkips(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "sess.jsonl")
	writeJSONLFile(t, sessionPath, []string{forkMsg1})
	writeAgentSession(t, tmpDir, "sess", "agent-a.jsonl", []string{forkMsg2})

	analysis, err := AnalyzeSession(sessionPath, "sess", NoMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	if analysis.SkippedAgents != 0 || analysis.AgentCount != 1 {
		t.Errorf("SkippedAgents=%d AgentCount=%d, want 0/1",
			analysis.SkippedAgents, analysis.AgentCount)
	}
}
