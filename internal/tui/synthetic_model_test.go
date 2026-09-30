package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

// syntheticSession writes a transcript mixing real replies with the zero-token
// "<synthetic>" lines Claude Code records for API errors, the shape it writes
// them in, and returns its path.
func syntheticSession(t *testing.T) (path, id string) {
	t.Helper()
	id = "synthetic-mix"
	path = filepath.Join(t.TempDir(), id+".jsonl")
	lines := []string{
		`{"type":"assistant","timestamp":"2026-02-01T10:00:00Z","requestId":"r1","message":{"id":"m1","model":"claude-opus-4-8","usage":{"input_tokens":1000,"output_tokens":500}}}`,
		`{"type":"assistant","timestamp":"2026-02-01T10:01:00Z","isApiErrorMessage":true,"message":{"id":"s1","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
		`{"type":"assistant","timestamp":"2026-02-01T10:02:00Z","requestId":"r2","message":{"id":"m2","model":"claude-opus-4-8","usage":{"input_tokens":400,"output_tokens":900}}}`,
		`{"type":"assistant","timestamp":"2026-02-01T10:03:00Z","isApiErrorMessage":true,"message":{"id":"s2","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, id
}

// A synthetic line is a known zero-cost pseudo-model: watch neither lists it
// in COST BY MODEL nor names it in the fallback-pricing footnote.
func TestWatchIgnoresSyntheticModel(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	path, id := syntheticSession(t)

	m := NewModel(path, id, true, "", false)
	msg, ok := m.loadAnalysis().(analysisMsg)
	if !ok {
		t.Fatalf("loadAnalysis returned %T, want analysisMsg", m.loadAnalysis())
	}
	if msg.analysis.MessageCount != 4 {
		t.Errorf("MessageCount = %d, want 4 (synthetic lines count as messages)", msg.analysis.MessageCount)
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	m = updated.(Model)
	updated, _ = m.Update(msg)
	m = updated.(Model)
	out := stripANSI(m.View())

	if strings.Contains(out, "fallback pricing") {
		t.Errorf("watch footnote flags <synthetic>:\n%s", out)
	}
	rows := costByModelRows(out)
	if len(rows) != 1 || !strings.Contains(rows[0], "Opus 4.8") {
		t.Errorf("COST BY MODEL rows = %q, want only Opus 4.8", rows)
	}
}

// A session that only ever hit API errors has no model with a cost, so watch
// leaves COST BY MODEL out rather than draw a heading over nothing.
func TestWatchAllSyntheticSessionHasNoCostByModel(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	id := "synthetic-only"
	path := filepath.Join(t.TempDir(), id+".jsonl")
	line := `{"type":"assistant","timestamp":"2026-02-01T10:01:00Z","isApiErrorMessage":true,"message":{"id":"s1","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0}}}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel(path, id, true, "", false)
	msg, ok := m.loadAnalysis().(analysisMsg)
	if !ok {
		t.Fatalf("loadAnalysis returned %T, want analysisMsg", m.loadAnalysis())
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	m = updated.(Model)
	updated, _ = m.Update(msg)
	m = updated.(Model)
	if out := m.View(); strings.Contains(out, "COST BY MODEL") {
		t.Errorf("watch draws COST BY MODEL with no model under it:\n%s", out)
	}
}

// breakdown keeps each synthetic line as a row, since it is a line in the
// transcript, but without the fallback marker or footnote.
func TestBreakdownIgnoresSyntheticModel(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	path, id := syntheticSession(t)

	m := NewBreakdownModel(path, id, true, "", false)
	msg, ok := m.loadBreakdown().(breakdownMsgsMsg)
	if !ok {
		t.Fatalf("loadBreakdown returned %T, want breakdownMsgsMsg", m.loadBreakdown())
	}
	if len(msg.unknownModels) > 0 {
		t.Errorf("unknownModels = %q for a transcript whose only non-catalog model is <synthetic>", msg.unknownModels)
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(BreakdownModel)
	updated, _ = m.Update(msg)
	m = updated.(BreakdownModel)
	out := stripANSI(m.View())

	if strings.Contains(out, "fallback pricing") {
		t.Errorf("breakdown footnote flags <synthetic>:\n%s", out)
	}
	row := findRow(t, out, "10:01:00")
	if !strings.Contains(row, "synthetic ") {
		t.Errorf("synthetic row should name the model uncut:\n%q", row)
	}
	if strings.Contains(row, unknownModelMarker) {
		t.Errorf("synthetic row carries the fallback marker:\n%q", row)
	}
}
