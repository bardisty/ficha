package analyzer

import (
	"path/filepath"
	"testing"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
)

// Every cache_creation shape the parser reconciles, all on one model so cost
// linearity holds exactly: detailed, flat-only (older format), empty object,
// flat exceeding the buckets, and a corrupt negative line.
var reconcileMixLines = []string{
	`{"type":"assistant","requestId":"r1","timestamp":"2024-01-15T10:00:00Z","message":{"id":"m1","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":700,"cache_creation":{"ephemeral_5m_input_tokens":300,"ephemeral_1h_input_tokens":400}}}}`,
	`{"type":"assistant","requestId":"r2","timestamp":"2024-01-15T10:01:00Z","message":{"id":"m2","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":800}}}`,
	`{"type":"assistant","requestId":"r3","timestamp":"2024-01-15T10:02:00Z","message":{"id":"m3","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":900,"cache_creation":{}}}}`,
	`{"type":"assistant","requestId":"r4","timestamp":"2024-01-15T10:03:00Z","message":{"id":"m4","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":1000,"cache_creation":{"ephemeral_5m_input_tokens":300,"ephemeral_1h_input_tokens":400}}}}`,
	`{"type":"assistant","requestId":"r5","timestamp":"2024-01-15T10:04:00Z","message":{"id":"m5","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":-500,"cache_creation_input_tokens":-300}}}`,
}

// Messages m2, m3, m4 carry write tokens the data didn't attribute to a TTL.
const reconcileMixEstimated = 3

// TestCacheReconciliationInvariant is the session-17 theme test: for any mix
// of detailed/flat/empty/nil cache_creation messages, the displayed per-TTL
// token counts must cover every billed write token, and the billed cost must
// equal CalculateCost of the displayed representation — one reconciled truth,
// not per-consumer interpretations.
func TestCacheReconciliationInvariant(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "mix.jsonl")
	writeJSONLFile(t, sessionPath, reconcileMixLines)

	analysis, err := AnalyzeSession(sessionPath, "mix", NoMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}

	// Displayed 5m/1h token rows must cover every billed write token: the
	// per-TTL split (what the tables and TUIs render) sums to the flat total
	// (what the cost was computed from).
	tok5m, tok1h := render.CacheTokensByTTL(analysis.TotalUsage)
	if got, want := tok5m+tok1h, analysis.TotalUsage.CacheCreationInputTokens; got != want {
		t.Errorf("displayed cache-write tokens %d don't cover billed write tokens %d", got, want)
	}
	// 5m bucket: 300 + 800 + 900 + (300 + 300 unattributed remainder) = 2600; 1h: 400 + 400 = 800
	if tok5m != 2600 || tok1h != 800 {
		t.Errorf("per-TTL split: got (%d, %d), want (2600, 800)", tok5m, tok1h)
	}

	// Billed cost must equal the cost of the displayed representation.
	recomputed := CalculateCost(analysis.TotalUsage, "claude-sonnet-4-5")
	if !almostEqual(analysis.TotalCost.TotalCost, recomputed.TotalCost, 1e-9) {
		t.Errorf("TotalCost %.12f != CalculateCost(TotalUsage) %.12f", analysis.TotalCost.TotalCost, recomputed.TotalCost)
	}
	if !almostEqual(analysis.TotalCost.CacheWrite5mCost, recomputed.CacheWrite5mCost, 1e-9) {
		t.Errorf("CacheWrite5mCost %.12f != recomputed %.12f", analysis.TotalCost.CacheWrite5mCost, recomputed.CacheWrite5mCost)
	}
	if !almostEqual(analysis.TotalCost.CacheWrite1hCost, recomputed.CacheWrite1hCost, 1e-9) {
		t.Errorf("CacheWrite1hCost %.12f != recomputed %.12f", analysis.TotalCost.CacheWrite1hCost, recomputed.CacheWrite1hCost)
	}

	// The corrupt line's negative counts must not survive into token
	// aggregates, just as they never reach costs.
	if analysis.TotalUsage.OutputTokens != 4*50 {
		t.Errorf("OutputTokens: got %d, want %d (negative clamped)", analysis.TotalUsage.OutputTokens, 4*50)
	}
	if analysis.TotalUsage.CacheCreationInputTokens != 700+800+900+1000 {
		t.Errorf("CacheCreationInputTokens: got %d, want %d", analysis.TotalUsage.CacheCreationInputTokens, 700+800+900+1000)
	}

	if analysis.EstimatedCostMessages != reconcileMixEstimated {
		t.Errorf("EstimatedCostMessages: got %d, want %d", analysis.EstimatedCostMessages, reconcileMixEstimated)
	}
}

// The 5m fallback assumption stays, but the surfaces must be able
// to say how often it fired — per session (incl. agents), per agent, on the
// multi-session aggregate, and on the breakdown result.
func TestEstimatedCostMessagesFlow(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")

	// Parent: one old-format (estimated) + one detailed (exact) message.
	writeJSONLFile(t, sessionPath, []string{
		`{"type":"assistant","requestId":"r1","timestamp":"2024-01-15T10:00:00Z","message":{"id":"m1","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":800}}}`,
		`{"type":"assistant","requestId":"r2","timestamp":"2024-01-15T10:01:00Z","message":{"id":"m2","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":700,"cache_creation":{"ephemeral_5m_input_tokens":300,"ephemeral_1h_input_tokens":400}}}}`,
	})
	// Agent: one old-format message.
	writeAgentSession(t, tmpDir, sessionID, "agent-a.jsonl", []string{
		`{"type":"assistant","requestId":"r3","timestamp":"2024-01-15T10:02:00Z","message":{"id":"m3","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":500}}}`,
	})

	analysis, err := AnalyzeSession(sessionPath, sessionID, NoMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	if analysis.EstimatedCostMessages != 2 {
		t.Errorf("session EstimatedCostMessages: got %d, want 2 (1 parent + 1 agent)", analysis.EstimatedCostMessages)
	}
	if len(analysis.Agents) != 1 {
		t.Fatalf("agents: got %d, want 1", len(analysis.Agents))
	}
	if analysis.Agents[0].EstimatedCostMessages != 1 {
		t.Errorf("agent EstimatedCostMessages: got %d, want 1", analysis.Agents[0].EstimatedCostMessages)
	}

	// Aggregate surface sums the per-session counters.
	entries := []models.SessionEntry{{SessionID: sessionID, FullPath: sessionPath}}
	aggregate, _, err := AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions: %v", err)
	}
	if aggregate.EstimatedCostMessages != 2 {
		t.Errorf("aggregate EstimatedCostMessages: got %d, want 2", aggregate.EstimatedCostMessages)
	}

	// Breakdown surface carries the same counter (parent + agents).
	result, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("GetBreakdownMessages: %v", err)
	}
	if result.EstimatedCostMessages != 2 {
		t.Errorf("breakdown EstimatedCostMessages: got %d, want 2", result.EstimatedCostMessages)
	}
}

// A fully detailed session must report zero estimated messages — the counter
// exists to qualify totals, and a spurious nonzero would make exact totals
// look approximate.
func TestEstimatedCostMessagesZeroOnDetailedData(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "detailed.jsonl")
	writeJSONLFile(t, sessionPath, []string{
		`{"type":"assistant","requestId":"r1","timestamp":"2024-01-15T10:00:00Z","message":{"id":"m1","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":700,"cache_creation":{"ephemeral_5m_input_tokens":300,"ephemeral_1h_input_tokens":400}}}}`,
	})

	analysis, err := AnalyzeSession(sessionPath, "detailed", NoMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	if analysis.EstimatedCostMessages != 0 {
		t.Errorf("EstimatedCostMessages: got %d, want 0", analysis.EstimatedCostMessages)
	}
}
