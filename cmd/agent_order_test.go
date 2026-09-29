package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// show json lists agents by first message, plain agents first, whatever
// order their file names give. The run whose agent started first comes
// first, and the per-message rows follow the same agent order.
func TestE2EShowJSONAgentsInStartOrder(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "projects", "-home-test-order")
	const sessionID = "dddddddd-1111-2222-3333-444444444444"
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(proj, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	agent := func(dir, id, ts string) {
		write(filepath.Join(sessionID, "subagents", dir, "agent-"+id+".jsonl"),
			e2eMsg(ts, id, "claude-sonnet-5", 100, 10, 0, 0, 0))
	}
	write(sessionID+".jsonl", e2eMsg("2026-03-01T10:00:00Z", "p1", "claude-opus-4-8", 100, 10, 0, 0, 0))
	agent("", "a1", "2026-03-01T10:30:00Z")
	agent("", "a2", "2026-03-01T10:10:00Z")
	agent("workflows/wf_a", "w1", "2026-03-01T10:40:00Z")
	agent("workflows/wf_b", "w2", "2026-03-01T10:20:00Z")
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	var got struct {
		Agents []struct {
			AgentID string `json:"agent_id"`
		} `json:"agents"`
		Workflows []struct {
			RunID string `json:"run_id"`
		} `json:"workflows"`
		Messages []struct {
			AgentID string `json:"agent_id"`
		} `json:"messages"`
	}
	stdout, _, err := executeCLISplit(t, "show", "--project-dir=-home-test-order", sessionID, "-f", "json", "--messages")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decoding show json: %v\n%s", err, stdout)
	}

	var agents, runs, rows []string
	for _, a := range got.Agents {
		agents = append(agents, a.AgentID)
	}
	for _, w := range got.Workflows {
		runs = append(runs, w.RunID)
	}
	for _, m := range got.Messages {
		if m.AgentID != "" {
			rows = append(rows, m.AgentID)
		}
	}
	const want = "a2 a1 w2 w1"
	if s := strings.Join(agents, " "); s != want {
		t.Errorf("agents: got %s, want %s", s, want)
	}
	if s := strings.Join(runs, " "); s != "wf_b wf_a" {
		t.Errorf("workflows: got %s, want wf_b wf_a", s)
	}
	if s := strings.Join(rows, " "); s != want {
		t.Errorf("agent message rows: got %s, want %s", s, want)
	}
}
