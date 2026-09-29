package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

func orderAgent(id, run string, start time.Time) models.AgentAnalysis {
	return models.AgentAnalysis{AgentID: id, WorkflowID: run, StartTime: start}
}

func at(h, m int) time.Time { return time.Date(2026, 9, 1, h, m, 0, 0, time.UTC) }

// agentIDs lists agents' IDs, with a "run:<id>" marker where a new run
// begins, so a test can read the grouping as well as the order.
func agentIDs(agents []models.AgentAnalysis) string {
	var ids []string
	prev := ""
	for _, a := range agents {
		if a.WorkflowID != prev {
			prev = a.WorkflowID
			ids = append(ids, "run:"+a.WorkflowID)
		}
		ids = append(ids, a.AgentID)
	}
	return strings.Join(ids, " ")
}

func TestAgentOrder(t *testing.T) {
	agents := []models.AgentAnalysis{
		orderAgent("new", "", time.Time{}),
		orderAgent("late", "", at(11, 0)),
		orderAgent("r1a", "wf_a", at(10, 40)),
		orderAgent("early", "", at(10, 0)),
		orderAgent("r2b", "wf_b", at(10, 50)),
		orderAgent("r2a", "wf_b", at(10, 30)),
		orderAgent("tie", "", at(11, 0)),
		orderAgent("r1new", "wf_a", time.Time{}),
		orderAgent("r3new", "wf_c", time.Time{}),
	}
	var sorted []models.AgentAnalysis
	for _, pos := range agentOrder(agents) {
		sorted = append(sorted, agents[pos])
	}
	// Plain agents first even where a run started earlier; a tie keeps
	// discovery order; no timestamp sorts last, within its group and, for a
	// run with none, among runs.
	want := "early late tie new run:wf_b r2a r2b run:wf_a r1a r1new run:wf_c r3new"
	if got := agentIDs(sorted); got != want {
		t.Errorf("order:\n got %s\nwant %s", got, want)
	}
}

// orderLine is an assistant line for agent id at ts; a zero ts leaves the
// timestamp out.
func orderLine(id string, ts time.Time) string {
	stamp := ""
	if !ts.IsZero() {
		stamp = fmt.Sprintf(`"timestamp":%q,`, ts.Format(time.RFC3339))
	}
	return fmt.Sprintf(`{"type":"assistant",%s"message":{"id":"m-%s","model":"claude-haiku-4-5","usage":{"input_tokens":100,"output_tokens":10}}}`, stamp, id)
}

// writeOrderAgent writes agent id, starting at start, into sessionID's
// subagents directory, or into workflow run's directory when run is set.
func writeOrderAgent(t *testing.T, projectDir, sessionID, run, id string, start time.Time) {
	t.Helper()
	dir := filepath.Join(projectDir, sessionID, "subagents")
	if run != "" {
		dir = filepath.Join(dir, "workflows", run)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeJSONLFile(t, filepath.Join(dir, "agent-"+id+".jsonl"), []string{orderLine(id, start)})
}

// Discovery lists subagents/ by file name, then each run directory by name.
// The analysis lists agents by start instead, and the workflow runs and the
// per-agent message blocks follow that order.
func TestAnalyzeSessionOrdersAgentsByStart(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "sess.jsonl")
	writeJSONLFile(t, sessionPath, []string{orderLine("parent", at(10, 0))})
	writeOrderAgent(t, dir, "sess", "", "a1", at(10, 30))
	writeOrderAgent(t, dir, "sess", "", "a2", time.Time{})
	writeOrderAgent(t, dir, "sess", "", "a3", at(10, 10))
	writeOrderAgent(t, dir, "sess", "", "a4", at(10, 30))
	writeOrderAgent(t, dir, "sess", "wf_a", "w1", at(10, 50))
	writeOrderAgent(t, dir, "sess", "wf_a", "w2", at(10, 40))
	writeOrderAgent(t, dir, "sess", "wf_b", "w3", at(10, 20))

	analysis, err := AnalyzeSession(sessionPath, "sess", AllMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}

	want := "a3 a1 a4 a2 run:wf_b w3 run:wf_a w2 w1"
	if got := agentIDs(analysis.Agents); got != want {
		t.Errorf("agents:\n got %s\nwant %s", got, want)
	}

	var runs []string
	for _, w := range analysis.Workflows {
		runs = append(runs, w.RunID)
	}
	if got := strings.Join(runs, " "); got != "wf_b wf_a" {
		t.Errorf("workflows: got %s, want wf_b wf_a", got)
	}

	// Each agent wrote one message, so the rows after the parent's name the
	// agents in the same order.
	var rows []string
	for _, m := range analysis.Messages[1:] {
		rows = append(rows, m.AgentID)
	}
	if got := strings.Join(rows, " "); got != "a3 a1 a4 a2 w3 w2 w1" {
		t.Errorf("message blocks: got %s, want a3 a1 a4 a2 w3 w2 w1", got)
	}
}

// Watch re-analyzes through the parse cache every poll and lists agents in
// the order it gets them. An agent that appears between polls lands at the
// bottom of its group even when its file name sorts first, and the agents
// served from the cache keep their places.
func TestAnalyzeSessionWithCacheOrdersNewAgentLast(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "sess.jsonl")
	writeJSONLFile(t, sessionPath, []string{orderLine("parent", at(10, 0))})
	writeOrderAgent(t, dir, "sess", "", "b1", at(10, 20))
	writeOrderAgent(t, dir, "sess", "", "b2", at(10, 10))
	writeOrderAgent(t, dir, "sess", "wf_a", "w2", at(10, 30))
	writeOrderAgent(t, dir, "sess", "wf_b", "w3", at(10, 40))

	cache := NewAgentParseCache()
	first, err := AnalyzeSessionWithCache(sessionPath, "sess", AllMessages, cache)
	if err != nil {
		t.Fatalf("first analysis: %v", err)
	}
	if got, want := agentIDs(first.Agents), "b2 b1 run:wf_a w2 run:wf_b w3"; got != want {
		t.Fatalf("first analysis:\n got %s\nwant %s", got, want)
	}

	writeOrderAgent(t, dir, "sess", "", "a0", at(11, 0))
	writeOrderAgent(t, dir, "sess", "wf_a", "w0", at(11, 0))
	second, err := AnalyzeSessionWithCache(sessionPath, "sess", AllMessages, cache)
	if err != nil {
		t.Fatalf("second analysis: %v", err)
	}
	if got, want := agentIDs(second.Agents), "b2 b1 a0 run:wf_a w2 w0 run:wf_b w3"; got != want {
		t.Errorf("second analysis:\n got %s\nwant %s", got, want)
	}
}

// writeOrderRunMeta names workflow run as an instance of workflow.
func writeOrderRunMeta(t *testing.T, projectDir, sessionID, run, workflow string) {
	t.Helper()
	dir := filepath.Join(projectDir, sessionID, "workflows")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	meta := fmt.Sprintf(`{"workflowName":%q,"status":"completed"}`, workflow)
	if err := os.WriteFile(filepath.Join(dir, run+".json"), []byte(meta), 0644); err != nil {
		t.Fatal(err)
	}
}

// Run IDs are random, so a workflow's second run can sort first by ID.
// Breakdown lists runs by start, as show does, whatever order discovery
// finds them in; a run with no timestamped message goes last.
func TestGetBreakdownMessagesOrdersRunsByStart(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "sess.jsonl")
	writeJSONLFile(t, sessionPath, []string{orderLine("parent", at(10, 0))})
	writeOrderAgent(t, dir, "sess", "wf_0", "w0", time.Time{})
	writeOrderAgent(t, dir, "sess", "wf_a", "w1", at(10, 50))
	writeOrderAgent(t, dir, "sess", "wf_a", "w2", at(10, 30))
	writeOrderAgent(t, dir, "sess", "wf_b", "w3", at(10, 40))
	for _, run := range []string{"wf_0", "wf_a", "wf_b"} {
		writeOrderRunMeta(t, dir, "sess", run, "review-changes")
	}

	result, err := GetBreakdownMessages(sessionPath, "sess")
	if err != nil {
		t.Fatalf("GetBreakdownMessages: %v", err)
	}
	var runs []string
	for _, w := range result.Workflows {
		runs = append(runs, w.RunID)
	}
	// wf_a starts at w2's 10:30, before wf_b's 10:40, though w1 is later.
	if got := strings.Join(runs, " "); got != "wf_a wf_b wf_0" {
		t.Errorf("workflows: got %s, want wf_a wf_b wf_0", got)
	}

	// Same order as show over the same session.
	analysis, err := AnalyzeSession(sessionPath, "sess", AllMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	var showRuns []string
	for _, w := range analysis.Workflows {
		showRuns = append(showRuns, w.RunID)
	}
	if !slices.Equal(runs, showRuns) {
		t.Errorf("breakdown runs %v, show runs %v", runs, showRuns)
	}
}
