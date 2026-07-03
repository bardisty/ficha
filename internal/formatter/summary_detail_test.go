package formatter

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ccusage/internal/models"
)

// sampleSummaryResults returns three results: two that parsed (sess-02 modified
// earlier than sess-01, so modified-order sorting reverses input order) and one
// that failed (nil Analysis, must be dropped from machine output). sess-01 owns
// a single agent sub-session so --expand-agents has something to emit.
func sampleSummaryResults() []models.SessionResult {
	return []models.SessionResult{
		{
			Entry: models.SessionEntry{
				SessionID: "sess-01",
				Modified:  time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC),
			},
			Analysis: &models.SessionAnalysis{
				SessionID:    "sess-01",
				MessageCount: 10,
				HasAgents:    true,
				AgentCount:   1,
				TotalCost: models.CostBreakdown{
					InputCost: 1.0, OutputCost: 2.0, TotalCost: 5.0, CacheSavings: 0.5,
				},
				ParentCostByModel: map[string]models.CostBreakdown{
					"claude-opus-4-8": {TotalCost: 4.0},
				},
				CostByModel: map[string]models.CostBreakdown{
					"claude-opus-4-8":  {TotalCost: 4.0},
					"claude-haiku-4-5": {TotalCost: 1.0},
				},
				Agents: []models.AgentAnalysis{
					{
						AgentID:      "agent-x",
						MessageCount: 3,
						TotalCost:    models.CostBreakdown{InputCost: 0.3, OutputCost: 0.7, TotalCost: 1.0},
						CostByModel: map[string]models.CostBreakdown{
							"claude-haiku-4-5": {TotalCost: 1.0},
						},
					},
				},
			},
		},
		{
			Entry: models.SessionEntry{
				SessionID: "sess-02",
				Modified:  time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			},
			Analysis: &models.SessionAnalysis{
				SessionID:    "sess-02",
				MessageCount: 4,
				TotalCost:    models.CostBreakdown{InputCost: 0.5, OutputCost: 1.5, TotalCost: 2.0},
				CostByModel: map[string]models.CostBreakdown{
					"claude-sonnet-5": {TotalCost: 2.0},
				},
			},
		},
		{
			Entry:    models.SessionEntry{SessionID: "sess-bad", Modified: time.Date(2024, 1, 3, 10, 0, 0, 0, time.UTC)},
			Analysis: nil, // failed to parse — excluded from json/csv
		},
	}
}

func sampleSummaryAggregate() *models.SessionAnalysis {
	return &models.SessionAnalysis{
		SessionID:    "aggregate",
		MessageCount: 14,
		TotalCost:    models.CostBreakdown{TotalCost: 7.0},
		IsSummary:    true,
		SessionCount: 2,
	}
}

func TestFormatSummaryDetailJSON_NoExpand(t *testing.T) {
	out, err := FormatSummaryDetailJSON(sampleSummaryAggregate(), sampleSummaryResults(), false, true)
	if err != nil {
		t.Fatalf("FormatSummaryDetailJSON returned error: %v", err)
	}

	var detail models.SummaryDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}

	if detail.Summary == nil || detail.Summary.MessageCount != 14 {
		t.Errorf("summary.message_count: got %+v, want 14", detail.Summary)
	}
	// Two parsed sessions; the nil-analysis one is dropped.
	if len(detail.Sessions) != 2 {
		t.Fatalf("sessions: got %d, want 2 (failed session must be excluded)", len(detail.Sessions))
	}
	// Modified-time order: sess-02 (Jan 1) before sess-01 (Jan 2).
	if detail.Sessions[0].SessionID != "sess-02" || detail.Sessions[1].SessionID != "sess-01" {
		t.Errorf("session order: got [%s, %s], want [sess-02, sess-01]",
			detail.Sessions[0].SessionID, detail.Sessions[1].SessionID)
	}
	// Without --expand-agents the nested agents array is omitted.
	if len(detail.Sessions[1].Agents) != 0 {
		t.Errorf("agents should be omitted without --expand-agents, got %d", len(detail.Sessions[1].Agents))
	}
	// The raw JSON should not carry the "agents" key for the session that has one.
	if strings.Contains(out, "\"agent_id\": \"agent-x\"") {
		t.Error("agent detail leaked into json without --expand-agents")
	}
}

func TestFormatSummaryDetailJSON_Expand(t *testing.T) {
	out, err := FormatSummaryDetailJSON(sampleSummaryAggregate(), sampleSummaryResults(), true, true)
	if err != nil {
		t.Fatalf("FormatSummaryDetailJSON returned error: %v", err)
	}

	var detail models.SummaryDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	// sess-01 is second in modified order and owns the agent.
	sess01 := detail.Sessions[1]
	if sess01.SessionID != "sess-01" {
		t.Fatalf("expected sess-01 at index 1, got %q", sess01.SessionID)
	}
	if len(sess01.Agents) != 1 {
		t.Fatalf("expected 1 nested agent with --expand-agents, got %d", len(sess01.Agents))
	}
	if sess01.Agents[0].AgentID != "agent-x" {
		t.Errorf("agent_id: got %q, want agent-x", sess01.Agents[0].AgentID)
	}
}

func TestFormatSummaryDetailCSV_NoExpand(t *testing.T) {
	out, err := FormatSummaryDetailCSV(sampleSummaryResults(), false)
	if err != nil {
		t.Fatalf("FormatSummaryDetailCSV returned error: %v", err)
	}

	records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v\n%s", err, out)
	}

	// header + 2 session rows (no agent rows, failed session dropped)
	if len(records) != 3 {
		t.Fatalf("rows: got %d, want 3 (header + 2 sessions)", len(records))
	}
	wantHeader := []string{
		"row_type", "session_id", "agent_id", "modified", "model",
		"message_count", "agent_count", "input_cost", "output_cost",
		"cache_write_5m_cost", "cache_write_1h_cost", "cache_read_cost",
		"total_cost", "cache_savings", "cumulative_cost",
	}
	if len(records[0]) != len(wantHeader) {
		t.Fatalf("header columns: got %d, want %d", len(records[0]), len(wantHeader))
	}
	for i, col := range wantHeader {
		if records[0][i] != col {
			t.Errorf("header col %d: got %q, want %q", i, records[0][i], col)
		}
	}

	// Row 1: sess-02 (earlier modified), cumulative = 2.0
	r1 := records[1]
	if r1[0] != "session" || r1[1] != "sess-02" {
		t.Errorf("row1 row_type/session_id: got %q/%q, want session/sess-02", r1[0], r1[1])
	}
	if r1[4] != "claude-sonnet-5" {
		t.Errorf("row1 model: got %q, want claude-sonnet-5", r1[4])
	}
	if r1[14] != "2.000000" {
		t.Errorf("row1 cumulative_cost: got %q, want 2.000000", r1[14])
	}

	// Row 2: sess-01, agent_count 1, cumulative = 2.0 + 5.0 = 7.0
	r2 := records[2]
	if r2[1] != "sess-01" {
		t.Errorf("row2 session_id: got %q, want sess-01", r2[1])
	}
	if r2[6] != "1" {
		t.Errorf("row2 agent_count: got %q, want 1", r2[6])
	}
	if r2[4] != "claude-opus-4-8" {
		t.Errorf("row2 model (parent primary): got %q, want claude-opus-4-8", r2[4])
	}
	if r2[14] != "7.000000" {
		t.Errorf("row2 cumulative_cost: got %q, want 7.000000", r2[14])
	}
}

func TestFormatSummaryDetailCSV_Expand(t *testing.T) {
	out, err := FormatSummaryDetailCSV(sampleSummaryResults(), true)
	if err != nil {
		t.Fatalf("FormatSummaryDetailCSV returned error: %v", err)
	}

	records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}

	// header + session sess-02 + session sess-01 + agent row for sess-01
	if len(records) != 4 {
		t.Fatalf("rows: got %d, want 4 (header + 2 sessions + 1 agent)", len(records))
	}

	agent := records[3]
	if agent[0] != "agent" {
		t.Errorf("row_type: got %q, want agent", agent[0])
	}
	if agent[1] != "sess-01" {
		t.Errorf("agent session_id: got %q, want sess-01 (parent)", agent[1])
	}
	if agent[2] != "1" {
		t.Errorf("agent_id: got %q, want 1", agent[2])
	}
	if agent[4] != "claude-haiku-4-5" {
		t.Errorf("agent model: got %q, want claude-haiku-4-5", agent[4])
	}
	if agent[12] != "1.000000" {
		t.Errorf("agent total_cost: got %q, want 1.000000", agent[12])
	}
	// Agent rows do not participate in the session cumulative.
	if agent[14] != "" {
		t.Errorf("agent cumulative_cost: got %q, want empty", agent[14])
	}
	// agent_count is a session-row concept; empty on agent rows.
	if agent[6] != "" {
		t.Errorf("agent agent_count: got %q, want empty", agent[6])
	}
}

// TestFormatSummaryDetailCSV_UniformColumns guards the CLI-2/CLI-3 invariant that
// the detail CSV is a single table: every row (header, session, agent) has the
// same column count, so encoding/csv in the default (non-variadic) mode parses it.
func TestFormatSummaryDetailCSV_UniformColumns(t *testing.T) {
	out, err := FormatSummaryDetailCSV(sampleSummaryResults(), true)
	if err != nil {
		t.Fatalf("FormatSummaryDetailCSV returned error: %v", err)
	}
	r := csv.NewReader(strings.NewReader(out))
	r.FieldsPerRecord = 0 // lock to the first record's field count; mismatch errors
	if _, err := r.ReadAll(); err != nil {
		t.Fatalf("detail CSV is not a single uniform table: %v", err)
	}
}
