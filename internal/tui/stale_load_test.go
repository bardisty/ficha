package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
	tea "github.com/charmbracelet/bubbletea"
)

var errTestStale = errors.New("stale-session load error")

// In follow mode a slow in-flight load for the previous session can
// land after the switch. Load results are stamped with the session they were
// loaded for; the handler must drop a result whose sessionPath no longer
// matches the current session so it can't overwrite the new session's data
// under the new header. A result tagged for the current session still applies.

func TestBreakdownStaleLoadDroppedAfterSwitch(t *testing.T) {
	oldPath := "/proj/old.jsonl"
	newPath := "/proj/new.jsonl"
	m := NewBreakdownModel(oldPath, "old", true, "", true)

	// Switch to the new session (resets state, sessionPath = newPath).
	updated, _ := m.Update(sessionActivityMsg{path: newPath, id: "new", created: true})
	m = updated.(BreakdownModel)
	if m.sessionPath != newPath {
		t.Fatalf("after switch sessionPath = %q, want %q", m.sessionPath, newPath)
	}

	// A stale load for the OLD session lands after the switch: must be dropped.
	stale := breakdownMsgsMsg{
		messages:      []models.BreakdownMessage{{Index: 1, Cost: models.CostBreakdown{TotalCost: 9.99}}},
		totalCost:     9.99,
		unknownModels: []string{"m9"},
		sessionPath:   oldPath,
	}
	updated, _ = m.Update(stale)
	m = updated.(BreakdownModel)
	if len(m.messages) != 0 || m.totalCost != 0 || len(m.unknownModels) > 0 {
		t.Fatalf("stale load applied: messages=%d totalCost=%v unknownModels=%q", len(m.messages), m.totalCost, m.unknownModels)
	}
	if !m.loading {
		t.Fatal("stale load cleared the loading state; the real new-session load is still pending")
	}

	// A stale load ERROR for the old session must likewise not stamp its error.
	updated, _ = m.Update(breakdownErrorMsg{err: errTestStale, sessionPath: oldPath})
	m = updated.(BreakdownModel)
	if m.err != nil {
		t.Fatalf("stale load error stamped: %v", m.err)
	}

	// The current session's load applies.
	current := breakdownMsgsMsg{
		messages:    []models.BreakdownMessage{{Index: 1, Cost: models.CostBreakdown{TotalCost: 1.23}}},
		totalCost:   1.23,
		sessionPath: newPath,
	}
	updated, _ = m.Update(current)
	m = updated.(BreakdownModel)
	if len(m.messages) != 1 || m.totalCost != 1.23 {
		t.Fatalf("current load did not apply: messages=%d totalCost=%v", len(m.messages), m.totalCost)
	}
	if m.loading {
		t.Fatal("current load did not clear loading")
	}
}

// Switching away from a session that used fallback pricing must clear
// unknownModels (and any header error), so the "* = fallback pricing" footnote and
// a stale error don't bleed into the new session during its loading window.
func TestBreakdownSwitchClearsUnknownFootnote(t *testing.T) {
	m := NewBreakdownModel("/proj/old.jsonl", "old", true, "", true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(BreakdownModel)

	// Old session: a fallback-priced row and a header error.
	m.messages = []models.BreakdownMessage{{Index: 1}}
	m.unknownModels = []string{"m9"}
	m.err = errTestStale
	m.loading = false
	if !strings.Contains(frameOf(m), "fallback pricing") {
		t.Fatal("precondition: old session should show the fallback-pricing footnote")
	}

	// Switch to a clean session. During the loading window (before the new
	// load lands) neither the footnote nor the stale error may remain.
	updated, _ = m.Update(sessionActivityMsg{path: "/proj/new.jsonl", id: "new", created: true})
	m = updated.(BreakdownModel)
	if len(m.unknownModels) > 0 {
		t.Error("unknownModels leaked across the switch")
	}
	if m.err != nil {
		t.Errorf("err leaked across the switch: %v", m.err)
	}
	if !m.loading {
		t.Fatal("expected the new session to be loading")
	}
	if strings.Contains(frameOf(m), "fallback pricing") {
		t.Errorf("fallback-pricing footnote persisted into the new session during loading:\n%s", frameOf(m))
	}
}

func TestWatchStaleLoadDroppedAfterSwitch(t *testing.T) {
	oldPath := "/proj/old.jsonl"
	newPath := "/proj/new.jsonl"
	m := NewModel(oldPath, "old", true, "", true)

	updated, _ := m.Update(sessionActivityMsg{path: newPath, id: "new", created: true})
	m = updated.(Model)
	if m.sessionPath != newPath {
		t.Fatalf("after switch sessionPath = %q, want %q", m.sessionPath, newPath)
	}

	// Stale analysis for the old session: dropped.
	staleAnalysis := &models.SessionAnalysis{SessionID: "old", MessageCount: 42}
	updated, _ = m.Update(analysisMsg{analysis: staleAnalysis, sessionPath: oldPath})
	m = updated.(Model)
	if m.analysis != nil {
		t.Fatalf("stale analysis applied: %+v", m.analysis)
	}
	if !m.loading {
		t.Fatal("stale analysis cleared loading; the real new-session load is still pending")
	}

	// Stale error for the old session: not stamped.
	updated, _ = m.Update(errorMsg{err: errTestStale, sessionPath: oldPath})
	m = updated.(Model)
	if m.err != nil {
		t.Fatalf("stale error stamped: %v", m.err)
	}

	// Current session's analysis applies.
	newAnalysis := &models.SessionAnalysis{SessionID: "new", MessageCount: 7}
	updated, _ = m.Update(analysisMsg{analysis: newAnalysis, sessionPath: newPath})
	m = updated.(Model)
	if m.analysis == nil || m.analysis.SessionID != "new" {
		t.Fatalf("current analysis did not apply: %+v", m.analysis)
	}
	if m.loading {
		t.Fatal("current analysis did not clear loading")
	}
}
