package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// writeRunOrderSession writes a session whose two review-changes runs have
// IDs that sort opposite to their starts: wf_a starts at 10:30, wf_b at
// 10:10.
func writeRunOrderSession(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	line := func(id, ts string) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":"m-%s","model":"claude-haiku-4-5","usage":{"input_tokens":100,"output_tokens":10}}}`+"\n", ts, id)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sessionPath := filepath.Join(dir, "sess.jsonl")
	write(sessionPath, line("parent", "2026-09-01T10:00:00Z"))
	runs := filepath.Join(dir, "sess", "subagents", "workflows")
	write(filepath.Join(runs, "wf_a", "agent-a1111111111111111.jsonl"), line("a1", "2026-09-01T10:30:00Z"))
	write(filepath.Join(runs, "wf_b", "agent-a2222222222222222.jsonl"), line("a2", "2026-09-01T10:10:00Z"))
	for _, run := range []string{"wf_a", "wf_b"} {
		write(filepath.Join(dir, "sess", "workflows", run+".json"), `{"workflowName":"review-changes","status":"completed"}`)
	}
	return sessionPath
}

// Breakdown numbers a workflow's repeated runs by start, so the first run
// is rc and the key names it first, whichever run ID sorts first.
func TestBreakdownRunTagsFollowStart(t *testing.T) {
	m := NewBreakdownModel(writeRunOrderSession(t), "sess", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(BreakdownModel)
	updated, _ = m.Update(m.loadBreakdown())
	m = updated.(BreakdownModel)

	if got := m.runTags; got["wf_b"] != "rc" || got["wf_a"] != "rc2" {
		t.Errorf("run tags = %v, want wf_b rc, wf_a rc2", got)
	}
	var cells []string
	for _, msg := range m.messages {
		cells = append(cells, strings.TrimSpace(m.agentCell(msg, true)))
	}
	if got, want := strings.Join(cells, "|"), "|[A2222222] rc|[A1111111] rc2"; got != want {
		t.Errorf("AGENT cells = %q, want %q", got, want)
	}
	if got, want := strings.Join(m.runTagKey(), ", "), "rc = review-changes, rc2 = review-changes"; got != want {
		t.Errorf("key = %q, want %q", got, want)
	}
}
