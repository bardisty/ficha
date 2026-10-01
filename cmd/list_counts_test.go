package cmd

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
)

const e2eForkID = "ffffffff-0000-4000-8000-000000000001"

// messyListProject adds to the e2e fixture's first project the cases where
// a session's counts and its analysis part ways: a fork that copies alpha's
// transcript, malformed lines in a parent and in an agent, and an agent
// directory that can't be listed. It returns the project directory.
func messyListProject(t *testing.T) string {
	t.Helper()
	root := setupE2EFixture(t)
	proj := filepath.Join(root, "projects", e2eProjDir)

	alpha, err := os.ReadFile(filepath.Join(proj, e2eAlphaID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	fork := string(alpha) + e2eMsg("2026-02-04T08:00:00Z", "f1", "claude-opus-4-8", 700, 300, 0, 0, 0) + "\n"
	if err := os.WriteFile(filepath.Join(proj, e2eForkID+".jsonl"), []byte(fork), 0o644); err != nil {
		t.Fatal(err)
	}
	appendBrokenLine(t, filepath.Join(proj, e2eBetaID+".jsonl"))
	appendBrokenLine(t, filepath.Join(proj, e2eBetaID, "subagents", "agent-g1.jsonl"))
	// A file where the fork's workflow runs should be can't be listed on
	// any OS.
	subagents := filepath.Join(proj, e2eForkID, "subagents")
	if err := os.MkdirAll(subagents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagents, "workflows"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return proj
}

// makeUnreadable takes away read access to path, and skips the test where
// that doesn't stop a read.
func makeUnreadable(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 doesn't block reading a file on Windows")
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if f, err := os.Open(path); err == nil {
		f.Close()
		t.Skip("chmod 000 does not bar reads (running as root?)")
	}
}

// listEntries returns what `list -f json` and `-f csv` print counts from,
// next to what the discovery scan counts for the same sessions.
func listEntries(t *testing.T, proj string) (got, want []models.SessionEntry) {
	t.Helper()
	want, err := parser.DiscoverSessionsFromDisk(proj, true)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := parser.DiscoverSessionsFromDisk(proj, false)
	if err != nil {
		t.Fatal(err)
	}
	_, results, err := analyzer.AnalyzeMultipleSessions(sessions)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		got = append(got, countedEntry(r))
	}
	return got, want
}

// list takes its counts from the analysis's one parse of each transcript.
// They must be the counts a scan of each session on its own gives, which are
// show's: a fork counts the messages it copied, though the analysis bills
// them to the original.
func TestListCountsMatchAScanOfEachSession(t *testing.T) {
	proj := messyListProject(t)
	got, want := listEntries(t, proj)
	if len(got) != len(want) {
		t.Fatalf("got %d sessions, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("session %s:\n got %+v\nwant %+v", want[i].SessionID, got[i], want[i])
		}
	}

	stdout, _, err := executeCLISplit(t, "list", projFlag, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	var listed []struct {
		SessionID         string `json:"session_id"`
		MessageCount      int    `json:"message_count"`
		AgentCount        int    `json:"agent_count"`
		AgentMessageCount int    `json:"agent_message_count"`
		SkippedAgents     int    `json:"skipped_agents"`
		SkippedLines      int    `json:"skipped_lines"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("%v\n%s", err, stdout)
	}
	type counts struct{ messages, agents, agentMessages, skippedAgents, skippedLines int }
	wantCounts := map[string]counts{
		e2eAlphaID: {messages: 2},
		// Alpha's two messages and its own one.
		e2eForkID: {messages: 3, skippedAgents: 1},
		e2eBetaID: {messages: 4, agents: 2, agentMessages: 2, skippedLines: 2},
	}
	if len(listed) != len(wantCounts) {
		t.Fatalf("listed %d sessions, want %d:\n%s", len(listed), len(wantCounts), stdout)
	}
	for _, s := range listed {
		if got := (counts{s.MessageCount, s.AgentCount, s.AgentMessageCount, s.SkippedAgents, s.SkippedLines}); got != wantCounts[s.SessionID] {
			t.Errorf("session %s: got %+v, want %+v", s.SessionID, got, wantCounts[s.SessionID])
		}
	}
}

// A session whose own transcript can't be read has no analysis, and its
// agents count as skipped in the totals. list still counts those agents'
// messages in the session's row, and marks the session skipped.
func TestListCountsAgentsOfAnUnreadableSession(t *testing.T) {
	proj := messyListProject(t)
	makeUnreadable(t, filepath.Join(proj, e2eBetaID+".jsonl"))

	got, want := listEntries(t, proj)
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("session %s:\n got %+v\nwant %+v", want[i].SessionID, got[i], want[i])
		}
	}

	stdout, stderr, err := executeCLISplit(t, "list", projFlag, "-f", "csv", "-v")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(stdout)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	wantCols := map[string]string{
		"message_count": "2", "agent_count": "2", "agent_message_count": "2",
		"skipped_sessions": "1", "skipped_agents": "0", "skipped_lines": "1",
	}
	found := false
	for _, row := range rows[1:] {
		if row[0] != e2eBetaID {
			continue
		}
		found = true
		for col, name := range rows[0] {
			if w, ok := wantCols[name]; ok && row[col] != w {
				t.Errorf("beta's %s: got %s, want %s", name, row[col], w)
			}
		}
	}
	if !found {
		t.Errorf("beta is missing from the list:\n%s", stdout)
	}
	if !strings.Contains(stderr, "1 session(s)") || !strings.Contains(stderr, "bbbbbbbb") {
		t.Errorf("the warning should name the unreadable session:\n%s", stderr)
	}
}
