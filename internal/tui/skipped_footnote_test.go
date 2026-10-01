package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
	tea "github.com/charmbracelet/bubbletea"
)

// The footer is one line and already carries the fallback-pricing footnote, so
// skipped agents/lines and estimated costs share a single segment rather than
// claiming a separator each.
func TestAccountingFootnote(t *testing.T) {
	tests := []struct {
		name                     string
		agents, lines, estimated int
		ascii                    bool
		want                     string
	}{
		{"nothing to report", 0, 0, 0, false, ""},
		{"lines only keeps the original wording", 0, 3, 0, false, "⚠ 3 skipped line(s)"},
		{"lines only, ASCII glyph", 0, 3, 0, true, "! 3 skipped line(s)"},
		{"agents only", 2, 0, 0, true, "! 2 skipped agent(s)"},
		{"both share one segment", 2, 3, 0, false, "⚠ 2 skipped agent(s), 3 skipped line(s)"},
		{"estimated only", 0, 0, 4, true, "! 4 estimated cost(s)"},
		{"estimated joins the same segment", 2, 3, 4, false, "⚠ 2 skipped agent(s), 3 skipped line(s), 4 estimated cost(s)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.ascii {
				useASCII(t)
			}
			if got := accountingFootnote(tt.agents, tt.lines, tt.estimated); got != tt.want {
				t.Errorf("accountingFootnote(%d, %d, %d) ascii=%v = %q, want %q",
					tt.agents, tt.lines, tt.estimated, tt.ascii, got, tt.want)
			}
		})
	}
}

// An agent the breakdown could not read must show up in its footer.
// The breakdown TUI owns the screen — stderr warnings never reach it.
func TestBreakdownFooterShowsSkippedAgents(t *testing.T) {
	m := NewBreakdownModel("/fixture/sess.jsonl", "sess", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(BreakdownModel)
	updated, _ = m.Update(breakdownMsgsMsg{
		messages:       goldenBreakdownMessages(),
		totalCost:      3.34,
		insights:       &models.MessageInsights{MessageCount: 6},
		skippedAgents:  2,
		skippedLines:   3,
		estimatedCosts: 4,
	})
	m = updated.(BreakdownModel)

	view := m.View()
	if !strings.Contains(view, "⚠ 2 skipped agent(s), 3 skipped line(s), 4 estimated cost(s)") {
		t.Errorf("breakdown footer missing the combined accounting segment:\n%s", view)
	}
}

// The watch footer follows the same contract from SessionAnalysis.
func TestWatchFooterShowsSkippedAgents(t *testing.T) {
	m := NewModel("/fixture/sess.jsonl", "sess", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	updated, _ = m.Update(analysisMsg{analysis: &models.SessionAnalysis{
		SessionID:             "sess",
		CostByModel:           map[string]models.CostBreakdown{},
		MessageCount:          4,
		SkippedAgents:         1,
		SkippedLines:          2,
		EstimatedCostMessages: 3,
	}})
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "⚠ 1 skipped agent(s), 2 skipped line(s), 3 estimated cost(s)") {
		t.Errorf("watch footer missing the combined accounting segment:\n%s", view)
	}
}

// A session that parses to zero messages but skipped input must still disclose
// it. Suppressing the footer in the empty state would hide the only channel
// watch has for skip accounting. Driven end-to-end from a real
// fixture whose single line is unparseable (0 messages, SkippedLines=1) so the
// empty-session + nonzero-skips path exercises the actual load.
func TestWatchEmptySessionDisclosesSkippedLines(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "empty-with-skip"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")
	// A non-JSON line increments SkippedLines and yields no message.
	if err := os.WriteFile(sessionPath, []byte("not json at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel(sessionPath, sessionID, true, "", false)
	msg, ok := m.loadAnalysis().(analysisMsg)
	if !ok {
		t.Fatalf("loadAnalysis returned %T, want analysisMsg", m.loadAnalysis())
	}
	if msg.analysis.MessageCount != 0 || msg.analysis.SkippedLines == 0 {
		t.Fatalf("fixture precondition: MessageCount=%d SkippedLines=%d, want 0 and >0",
			msg.analysis.MessageCount, msg.analysis.SkippedLines)
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if !m.isEmptySession() {
		t.Fatal("expected an empty session (0 messages, 0 cost)")
	}
	view := m.View()
	if !strings.Contains(view, "Awaiting first message") {
		t.Errorf("expected the empty state body:\n%s", view)
	}
	if !strings.Contains(view, "skipped line(s)") {
		t.Errorf("empty-session watch view hid the skipped-line disclosure:\n%s", view)
	}
}

// loadBreakdown is the hop that carries the analyzer's counters into the TUI.
// Asserting the footer from a hand-built breakdownMsgsMsg would leave this line
// free to drop the count, so drive the real load against a real fixture. The
// parent line's flat-only cache tokens also exercise the estimated-cost hop.
func TestLoadBreakdownCarriesSkippedAgents(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	sessionPath := filepath.Join(tmpDir, sessionID+".jsonl")
	line := `{"type":"assistant","timestamp":"2026-02-01T10:00:00Z","requestId":"r1","message":{"id":"m1","model":"claude-opus-4-8","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":800}}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	subagents := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagents, "agent-a.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(subagents, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(subagents, 0o755) })
	if _, err := os.ReadDir(subagents); err == nil {
		t.Skip("chmod 000 does not bar directory reads (running as root?)")
	}

	m := NewBreakdownModel(sessionPath, sessionID, true, "", false)
	msg, ok := m.loadBreakdown().(breakdownMsgsMsg)
	if !ok {
		t.Fatalf("loadBreakdown returned %T, want breakdownMsgsMsg", m.loadBreakdown())
	}
	if msg.skippedAgents != 1 {
		t.Errorf("breakdownMsgsMsg.skippedAgents: got %d, want 1", msg.skippedAgents)
	}
	if msg.estimatedCosts != 1 {
		t.Errorf("breakdownMsgsMsg.estimatedCosts: got %d, want 1 (flat-only parent line)", msg.estimatedCosts)
	}
}
