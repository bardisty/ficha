package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// An agent's meta file can appear after its first lines, and watch and
// breakdown reload through one AgentParseCache for the life of the view. The
// cache keeps parsed lines, never the meta file, so the next load names the
// task without a restart.
func TestAgentTaskAppearsOnTheNextLoad(t *testing.T) {
	proj := t.TempDir()
	const sessionID = "11111111-2222-3333-4444-555555555555"
	session := filepath.Join(proj, sessionID+".jsonl")
	agent := filepath.Join(proj, sessionID, "subagents", "agent-a1b2c3d4e5f6a7b8c.jsonl")
	line := `{"type":"assistant","timestamp":"2026-01-15T10:00:00Z","requestId":"req_%s","message":{"id":"msg_%s","model":"claude-haiku-4-5","usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(session, fmt.Sprintf(line, "p", "p"))
	write(agent, fmt.Sprintf(line, "a", "a"))

	cache := NewAgentParseCache()
	tasks := func() (watch, breakdown string) {
		t.Helper()
		a, err := AnalyzeSessionWithCache(session, sessionID, AllMessages, cache)
		if err != nil || len(a.Agents) != 1 {
			t.Fatalf("AnalyzeSessionWithCache: %v, %d agents", err, len(a.Agents))
		}
		b, err := GetBreakdownMessagesWithCache(session, sessionID, cache)
		if err != nil {
			t.Fatal(err)
		}
		return a.Agents[0].TaskLabel(), b.Tasks[a.Agents[0].AgentID]
	}
	if w, b := tasks(); w != "" || b != "" {
		t.Fatalf("before the meta file: tasks %q, %q, want none", w, b)
	}
	write(filepath.Join(filepath.Dir(agent), "agent-a1b2c3d4e5f6a7b8c.meta.json"), `{"agentType":"Explore","description":"Find the auth middleware"}`)
	if w, b := tasks(); w != "Find the auth middleware" || b != w {
		t.Errorf("after the meta file: tasks %q, %q, want %q in both", w, b, "Find the auth middleware")
	}
}
