package cmd

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// End-to-end tests drive the full parser→analyzer→formatter pipeline through a
// real command invocation (newRootCmd().Execute()) against an on-disk fixture,
// with CLAUDE_CONFIG_DIR pointed at a temp projects tree. They assert on output
// structure and exit behavior, not exact dollar amounts — costs derive from the
// live pricing catalog, so pinning numbers here would duplicate the golden tests
// and break whenever a rate changes.

const (
	e2eProjDir  = "-home-test-proj"  // project with two sessions + an agent
	e2eOtherDir = "-home-test-other" // second project, so `global` sees two
	e2eAlphaID  = "aaaaaaaa-1111-2222-3333-444444444444"
	e2eBetaID   = "bbbbbbbb-5555-6666-7777-888888888888"
	e2eGammaID  = "cccccccc-9999-0000-1111-222222222222"
)

// e2eMsg builds one assistant JSONL line matching Claude Code's schema.
func e2eMsg(ts, id, model string, in, out, cc5m, cc1h, read int64) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"req_%s","message":{"id":%q,"model":%q,"usage":{"input_tokens":%d,"output_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"cache_creation":{"ephemeral_5m_input_tokens":%d,"ephemeral_1h_input_tokens":%d}}}}`,
		ts, id, id, model, in, out, cc5m+cc1h, read, cc5m, cc1h)
}

// setupE2EFixture builds a temp CLAUDE_CONFIG_DIR with two projects and points
// the env var at it for the duration of the (sub)test. Project 1 has two
// sessions (one with a Sonnet agent sub-session); project 2 has a single small
// session so `global` discovers more than one project.
func setupE2EFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	projects := filepath.Join(root, "projects")

	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(projects, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	lines := func(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

	write(filepath.Join(e2eProjDir, e2eAlphaID+".jsonl"), lines(
		e2eMsg("2026-02-01T10:00:00Z", "a1", "claude-opus-4-8", 1200, 800, 3000, 1000, 20000),
		e2eMsg("2026-02-01T10:05:00Z", "a2", "claude-opus-4-8", 400, 1500, 2000, 0, 30000),
	))
	write(filepath.Join(e2eProjDir, e2eBetaID+".jsonl"), lines(
		e2eMsg("2026-02-02T09:00:00Z", "b1", "claude-haiku-4-5", 5000, 2500, 10000, 0, 0),
		e2eMsg("2026-02-02T09:10:00Z", "b2", "claude-haiku-4-5", 300, 900, 0, 0, 15000),
	))
	write(filepath.Join(e2eProjDir, e2eBetaID, "subagents", "agent-g1.jsonl"), lines(
		e2eMsg("2026-02-02T09:05:00Z", "s1", "claude-sonnet-5", 2000, 1200, 5000, 0, 0),
	))
	// A workflow run under beta: one agent transcript plus the non-agent files
	// discovery must skip (journal, meta), and the run metadata with a large
	// script field the parser must ignore.
	wfRun := filepath.Join(e2eProjDir, e2eBetaID, "subagents", "workflows", "wf_e2e-run1")
	write(filepath.Join(wfRun, "agent-w1.jsonl"), lines(
		e2eMsg("2026-02-02T09:07:00Z", "w1", "claude-sonnet-5", 3000, 1500, 0, 0, 0),
	))
	write(filepath.Join(wfRun, "agent-w1.meta.json"), `{"agentType":"general-purpose","spawnDepth":1}`)
	write(filepath.Join(wfRun, "journal.jsonl"), lines(
		`{"type":"started","key":"v2:abc","agentId":"w1"}`,
		`{"type":"result","key":"v2:abc","agentId":"w1","result":{"ok":true}}`,
	))
	write(filepath.Join(e2eProjDir, e2eBetaID, "workflows", "wf_e2e-run1.json"),
		`{"runId":"wf_e2e-run1","workflowName":"e2e-flow","status":"completed","script":"`+strings.Repeat("x", 8192)+`"}`)
	write(filepath.Join(e2eOtherDir, e2eGammaID+".jsonl"), lines(
		e2eMsg("2026-02-03T12:00:00Z", "c1", "claude-opus-4-8", 100, 200, 0, 0, 0),
	))

	t.Setenv("CLAUDE_CONFIG_DIR", root)
	return root
}

// executeCLISplit runs a fresh command tree, capturing stdout and stderr in
// separate buffers so machine-format (json/csv) assertions parse clean stdout
// while warnings on stderr are checked independently.
func executeCLISplit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := newRootCmd()
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// projFlag is the --project-dir token for the fixture's first project. The `=`
// form is required: a bare leading-dash value is parsed as flags.
var projFlag = "--project-dir=" + e2eProjDir

// TestE2ECommands drives each command end-to-end and asserts on output shape and
// exit behavior. Success cases run check(stdout); error cases match wantErr.
func TestE2ECommands(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string // non-empty => expect an error whose message contains this
		check   func(t *testing.T, stdout string)
	}{
		{
			name: "show table",
			args: []string{"show", projFlag, e2eAlphaID},
			check: func(t *testing.T, out string) {
				mustContainAll(t, out, e2eAlphaID, "$", "TOTAL")
			},
		},
		{
			name: "show json",
			args: []string{"show", projFlag, e2eAlphaID, "-f", "json"},
			check: func(t *testing.T, out string) {
				var a models.SessionAnalysis
				mustJSON(t, out, &a)
				if a.SessionID != e2eAlphaID {
					t.Errorf("session_id = %q, want %q", a.SessionID, e2eAlphaID)
				}
				if a.TotalCost.TotalCost <= 0 {
					t.Errorf("total_cost = %v, want > 0", a.TotalCost.TotalCost)
				}
				if len(a.CostByModel) == 0 {
					t.Error("cost_by_model is empty")
				}
			},
		},
		{
			name: "show csv is one session table",
			args: []string{"show", projFlag, e2eAlphaID, "-f", "csv"},
			check: func(t *testing.T, out string) {
				// mustCSV uses a default reader, which errors on a ragged table —
				// so this passing is itself the proof that show no longer stacks a
				// second per-message table with a different column count (CLI-2).
				records := mustCSV(t, out)
				if len(records) != 2 { // header + one session row
					t.Fatalf("show csv: got %d rows, want 2 (header + session)", len(records))
				}
				if records[0][0] != "session_id" {
					t.Errorf("header col 0: got %q, want session_id", records[0][0])
				}
				if records[1][0] != e2eAlphaID {
					t.Errorf("session_id: got %q, want %q", records[1][0], e2eAlphaID)
				}
			},
		},
		{
			name: "show table groups workflow agents under a header",
			args: []string{"show", projFlag, e2eBetaID},
			check: func(t *testing.T, out string) {
				mustContainAll(t, out, "AGENT SUB-SESSIONS", "workflow: e2e-flow (completed)", "[A2]")
			},
		},
		{
			name: "show json carries workflow fields",
			args: []string{"show", projFlag, e2eBetaID, "-f", "json"},
			check: func(t *testing.T, out string) {
				var a models.SessionAnalysis
				mustJSON(t, out, &a)
				if a.AgentCount != 2 {
					t.Errorf("agent_count = %d, want 2 (regular + workflow)", a.AgentCount)
				}
				if a.WorkflowCount != 1 || len(a.Workflows) != 1 {
					t.Fatalf("workflow_count = %d (metas %d), want 1", a.WorkflowCount, len(a.Workflows))
				}
				if wf := a.Workflows[0]; wf.RunID != "wf_e2e-run1" || wf.Name != "e2e-flow" || wf.Status != "completed" {
					t.Errorf("workflow meta: got %+v", wf)
				}
				sum := a.ParentCost.TotalCost + a.AgentsCost.TotalCost
				if diff := a.TotalCost.TotalCost - sum; diff > 1e-9 || diff < -1e-9 {
					t.Errorf("total_cost %v != parent + agents %v", a.TotalCost.TotalCost, sum)
				}
			},
		},
		{
			name: "show csv includes workflow_count",
			args: []string{"show", projFlag, e2eBetaID, "-f", "csv"},
			check: func(t *testing.T, out string) {
				records := mustCSV(t, out)
				col := -1
				for i, c := range records[0] {
					if c == "workflow_count" {
						col = i
					}
				}
				if col == -1 {
					t.Fatal("workflow_count column missing from header")
				}
				if records[1][col] != "1" {
					t.Errorf("workflow_count: got %q, want 1", records[1][col])
				}
			},
		},
		{
			name: "show csv --messages switches to per-message rows",
			args: []string{"show", projFlag, e2eAlphaID, "-f", "csv", "--messages"},
			check: func(t *testing.T, out string) {
				records := mustCSV(t, out)
				if records[0][0] != "agent_id" {
					t.Errorf("header col 0: got %q, want agent_id (per-message table)", records[0][0])
				}
				if len(records) != 3 { // header + 2 messages (alpha has two)
					t.Fatalf("show csv --messages: got %d rows, want 3 (header + 2 messages)", len(records))
				}
				for i, r := range records[1:] {
					if r[0] != "" {
						t.Errorf("row %d agent_id: got %q, want empty (alpha has no agents)", i, r[0])
					}
				}
			},
		},
		{
			// Agent rows carry an agent_id and the whole table sums to the session
			// total that `show -f csv` reports.
			name: "show csv --messages includes agent rows summing to the session total",
			args: []string{"show", projFlag, e2eBetaID, "-f", "csv", "--messages"},
			check: func(t *testing.T, out string) {
				records := mustCSV(t, out)
				if len(records) != 5 { // header + 2 parent + 1 agent + 1 workflow agent
					t.Fatalf("rows: got %d, want 5 (header + 2 parent + 2 agent)", len(records))
				}
				agentCol := slices.Index(records[0], "agent_id")
				costCol := slices.Index(records[0], "total_cost")
				if agentCol == -1 || costCol == -1 {
					t.Fatalf("agent_id/total_cost missing from header %v", records[0])
				}

				var sum float64
				gotAgents := make([]string, 0, 4)
				for _, r := range records[1:] {
					gotAgents = append(gotAgents, r[agentCol])
					c, err := strconv.ParseFloat(r[costCol], 64)
					if err != nil {
						t.Fatalf("total_cost %q: %v", r[costCol], err)
					}
					sum += c
				}
				wantAgents := []string{"", "", "g1", "w1"}
				if !slices.Equal(gotAgents, wantAgents) {
					t.Errorf("agent_id column: got %v, want %v", gotAgents, wantAgents)
				}

				// Same analysis, session granularity — the two must agree.
				total := betaSessionTotalCost(t)
				// Rows print at 6dp, so tolerate the rounding of 4 of them.
				if diff := sum - total; diff > 5e-6 || diff < -5e-6 {
					t.Errorf("sum of message total_cost %v != session total_cost %v", sum, total)
				}
			},
		},
		{
			name: "show json --messages tags agent messages",
			args: []string{"show", projFlag, e2eBetaID, "-f", "json", "--messages"},
			check: func(t *testing.T, out string) {
				var a models.SessionAnalysis
				mustJSON(t, out, &a)
				if len(a.Messages) != 4 {
					t.Fatalf("messages: got %d, want 4 (2 parent + 2 agent)", len(a.Messages))
				}
				var sum float64
				byAgent := map[string]int{}
				for _, m := range a.Messages {
					sum += m.Cost.TotalCost
					byAgent[m.AgentID]++
				}
				if byAgent[""] != 2 || byAgent["g1"] != 1 || byAgent["w1"] != 1 {
					t.Errorf("agent_id distribution: got %v, want 2 parent + g1 + w1", byAgent)
				}
				if diff := sum - a.TotalCost.TotalCost; diff > 1e-9 || diff < -1e-9 {
					t.Errorf("sum of message costs %v != total_cost %v", sum, a.TotalCost.TotalCost)
				}
				// messages[].agent_id joins to agents[].agent_id
				for _, ag := range a.Agents {
					if byAgent[ag.AgentID] == 0 {
						t.Errorf("agent %q has no messages in the export", ag.AgentID)
					}
				}
			},
		},
		{
			name: "show json omits messages by default, includes them with --messages",
			args: []string{"show", projFlag, e2eAlphaID, "-f", "json"},
			check: func(t *testing.T, out string) {
				var a models.SessionAnalysis
				mustJSON(t, out, &a)
				if len(a.Messages) != 0 {
					t.Errorf("messages should be omitted without --messages, got %d", len(a.Messages))
				}
			},
		},
		{
			name: "show json --messages includes the message array",
			args: []string{"show", projFlag, e2eAlphaID, "-f", "json", "--messages"},
			check: func(t *testing.T, out string) {
				var a models.SessionAnalysis
				mustJSON(t, out, &a)
				if len(a.Messages) != 2 {
					t.Errorf("messages: got %d, want 2", len(a.Messages))
				}
			},
		},
		{
			name: "bare show picks a session",
			args: []string{"show", projFlag},
			check: func(t *testing.T, out string) {
				// Which session is "latest" depends on write mtimes; only assert a
				// session rendered at all.
				mustContainAll(t, out, "$", "TOTAL")
			},
		},
		{
			name: "list table",
			args: []string{"list", projFlag},
			check: func(t *testing.T, out string) {
				mustContainAll(t, out, e2eAlphaID, e2eBetaID)
			},
		},
		{
			name: "list json",
			args: []string{"list", projFlag, "-f", "json"},
			check: func(t *testing.T, out string) {
				var entries []models.SessionEntry
				mustJSON(t, out, &entries)
				if len(entries) != 2 {
					t.Fatalf("got %d sessions, want 2", len(entries))
				}
				ids := entries[0].SessionID + "|" + entries[1].SessionID
				mustContainAll(t, ids, e2eAlphaID, e2eBetaID)
			},
		},
		{
			name: "list csv",
			args: []string{"list", projFlag, "-f", "csv"},
			check: func(t *testing.T, out string) {
				records := mustCSV(t, out)
				if len(records) != 3 { // header + 2 sessions
					t.Fatalf("got %d CSV rows (incl header), want 3", len(records))
				}
				if records[0][0] != "session_id" {
					t.Errorf("first column = %q, want session_id", records[0][0])
				}
			},
		},
		{
			name: "summary table",
			args: []string{"summary", projFlag},
			check: func(t *testing.T, out string) {
				mustContainAll(t, out, "Summary:", "$", "TOTAL")
			},
		},
		{
			name: "summary details lists sessions and agent",
			args: []string{"summary", projFlag, "--details", "--expand-agents"},
			check: func(t *testing.T, out string) {
				// --details renders per-session rows; --expand-agents adds the
				// agent sub-session as an indented row.
				mustContainAll(t, out, e2eAlphaID[:8], e2eBetaID[:8])
			},
		},
		{
			name: "summary json",
			args: []string{"summary", projFlag, "-f", "json"},
			check: func(t *testing.T, out string) {
				var a models.SessionAnalysis
				mustJSON(t, out, &a)
				if a.TotalCost.TotalCost <= 0 {
					t.Errorf("total_cost = %v, want > 0", a.TotalCost.TotalCost)
				}
				if a.MessageCount <= 0 {
					t.Errorf("message_count = %d, want > 0", a.MessageCount)
				}
				// The aggregate carries the same partition a session does.
				if !a.HasAgents || a.AgentCount != 2 || a.WorkflowCount != 1 {
					t.Errorf("partition: has_agents=%v agent_count=%d workflow_count=%d, want true/2/1",
						a.HasAgents, a.AgentCount, a.WorkflowCount)
				}
				if a.AgentsCost.TotalCost <= 0 || a.ParentCost.TotalCost <= 0 {
					t.Errorf("parent_cost=%v agents_cost=%v, want both > 0", a.ParentCost.TotalCost, a.AgentsCost.TotalCost)
				}
				sum := a.ParentCost.TotalCost + a.AgentsCost.TotalCost
				if diff := a.TotalCost.TotalCost - sum; diff > 1e-9 || diff < -1e-9 {
					t.Errorf("total_cost %v != parent + agents %v", a.TotalCost.TotalCost, sum)
				}
				if len(a.ParentCostByModel) == 0 {
					t.Error("parent_cost_by_model is empty")
				}
				// Nested per-agent records stay on the per-session results.
				if len(a.Agents) != 0 {
					t.Errorf("aggregate agents: got %d, want 0", len(a.Agents))
				}
			},
		},
		{
			name: "summary csv carries the agent partition",
			args: []string{"summary", projFlag, "-f", "csv"},
			check: func(t *testing.T, out string) {
				records := mustCSV(t, out)
				if len(records) != 2 {
					t.Fatalf("rows: got %d, want 2 (header + aggregate)", len(records))
				}
				col := func(name string) string {
					i := slices.Index(records[0], name)
					if i == -1 {
						t.Fatalf("column %q missing from header %v", name, records[0])
					}
					return records[1][i]
				}
				if col("agent_count") != "2" || col("workflow_count") != "1" {
					t.Errorf("agent_count=%q workflow_count=%q, want 2/1", col("agent_count"), col("workflow_count"))
				}
				parent, agents, total := mustFloat(t, col("parent_cost")), mustFloat(t, col("agents_cost")), mustFloat(t, col("total_cost"))
				if agents <= 0 {
					t.Errorf("agents_cost = %v, want > 0", agents)
				}
				if diff := total - (parent + agents); diff > 5e-6 || diff < -5e-6 {
					t.Errorf("total_cost %v != parent_cost + agents_cost %v", total, parent+agents)
				}
			},
		},
		{
			name: "summary --details json carries per-session records",
			args: []string{"summary", projFlag, "--details", "-f", "json"},
			check: func(t *testing.T, out string) {
				var d models.SummaryDetail
				mustJSON(t, out, &d)
				if d.Summary == nil || d.Summary.TotalCost.TotalCost <= 0 {
					t.Error("summary aggregate missing or zero cost")
				}
				if len(d.Sessions) != 2 {
					t.Fatalf("sessions: got %d, want 2", len(d.Sessions))
				}
				// No --expand-agents: nested agents omitted even though beta owns one.
				for _, s := range d.Sessions {
					if len(s.Agents) != 0 {
						t.Errorf("session %s: agents must be omitted without --expand-agents", s.SessionID)
					}
				}
			},
		},
		{
			name: "summary --details --expand-agents json nests the agent",
			args: []string{"summary", projFlag, "--details", "--expand-agents", "-f", "json"},
			check: func(t *testing.T, out string) {
				var d models.SummaryDetail
				mustJSON(t, out, &d)
				total, wfTagged := 0, 0
				for _, s := range d.Sessions {
					total += len(s.Agents)
					for _, a := range s.Agents {
						if a.WorkflowID == "wf_e2e-run1" {
							wfTagged++
						}
					}
					if s.SessionID == e2eBetaID {
						if len(s.Workflows) != 1 || s.Workflows[0].Name != "e2e-flow" || s.Workflows[0].Status != "completed" {
							t.Errorf("beta workflows metadata: got %+v", s.Workflows)
						}
					}
				}
				if total != 2 { // beta has one regular + one workflow agent
					t.Errorf("nested agents across sessions: got %d, want 2", total)
				}
				if wfTagged != 1 {
					t.Errorf("workflow-tagged agents: got %d, want 1", wfTagged)
				}
			},
		},
		{
			name: "summary --details csv is one per-session table",
			args: []string{"summary", projFlag, "--details", "-f", "csv"},
			check: func(t *testing.T, out string) {
				records := mustCSV(t, out)
				if records[0][0] != "row_type" {
					t.Errorf("header col 0: got %q, want row_type", records[0][0])
				}
				if len(records) != 3 { // header + 2 session rows, no agent rows
					t.Fatalf("rows: got %d, want 3 (header + 2 sessions)", len(records))
				}
				for _, r := range records[1:] {
					if r[0] != "session" {
						t.Errorf("row_type: got %q, want session (agents excluded without --expand-agents)", r[0])
					}
				}
			},
		},
		{
			name: "summary --details --expand-agents csv adds an agent row",
			args: []string{"summary", projFlag, "--details", "--expand-agents", "-f", "csv"},
			check: func(t *testing.T, out string) {
				records := mustCSV(t, out)
				if len(records) != 5 { // header + 2 sessions + 2 agents (1 regular + 1 workflow)
					t.Fatalf("rows: got %d, want 5 (header + 2 sessions + 2 agents)", len(records))
				}
				wfCol := -1
				for i, col := range records[0] {
					if col == "workflow_id" {
						wfCol = i
					}
				}
				if wfCol == -1 {
					t.Fatal("workflow_id column missing from header")
				}
				agentRows, wfRows := 0, 0
				for _, r := range records[1:] {
					if r[0] != "agent" {
						continue
					}
					agentRows++
					if r[wfCol] == "wf_e2e-run1" {
						wfRows++
					}
				}
				if agentRows != 2 || wfRows != 1 {
					t.Errorf("agent rows: got %d (workflow-tagged %d), want 2 (1)", agentRows, wfRows)
				}

				// agent_id is one key space across every export: these rows must
				// join to `show --messages` rows, not to an ordinal.
				idCol := slices.Index(records[0], "agent_id")
				if idCol == -1 {
					t.Fatal("agent_id column missing from header")
				}
				got := []string{}
				for _, r := range records[1:] {
					if r[0] == "agent" {
						got = append(got, r[idCol])
					}
				}
				slices.Sort(got)
				if !slices.Equal(got, []string{"g1", "w1"}) {
					t.Errorf("agent_id values: got %v, want [g1 w1] (real IDs, not ordinals)", got)
				}
			},
		},
		{
			name: "global table shows both projects",
			args: []string{"global"},
			check: func(t *testing.T, out string) {
				// Default --top is 10 >= 2 projects, so the header reports all 2.
				mustContainAll(t, out, "TOP PROJECTS (2)", "Projects: 2", "TOTAL")
			},
		},
		{
			name: "global --top 1 limits the projects shown",
			args: []string{"global", "--top", "1"},
			check: func(t *testing.T, out string) {
				// Header count reflects the cap without depending on decoded names.
				mustContainAll(t, out, "TOP PROJECTS (1)", "Projects: 2")
				if strings.Contains(out, "TOP PROJECTS (2)") {
					t.Error("--top 1 should not report 2 projects in the header")
				}
			},
		},
		{
			name: "global json",
			args: []string{"global", "-f", "json"},
			check: func(t *testing.T, out string) {
				var g models.GlobalAnalysis
				mustJSON(t, out, &g)
				if g.ProjectCount != 2 {
					t.Errorf("project_count = %d, want 2", g.ProjectCount)
				}
				if g.TotalCost.TotalCost <= 0 {
					t.Errorf("total_cost = %v, want > 0", g.TotalCost.TotalCost)
				}
			},
		},
		{
			name: "global --top does not truncate json (D3: machine formats export all)",
			args: []string{"global", "--top", "1", "-f", "json"},
			check: func(t *testing.T, out string) {
				var g models.GlobalAnalysis
				mustJSON(t, out, &g)
				if len(g.Projects) != 2 {
					t.Errorf("projects in json: got %d, want 2 (--top is table-only)", len(g.Projects))
				}
			},
		},
		{
			name: "global --top does not truncate csv (D3: machine formats export all)",
			args: []string{"global", "--top", "1", "-f", "csv"},
			check: func(t *testing.T, out string) {
				records := mustCSV(t, out)
				if len(records) != 3 { // header + 2 project rows regardless of --top
					t.Errorf("csv rows: got %d, want 3 (header + 2 projects); --top is table-only", len(records))
				}
			},
		},
		// breakdown launches a TUI on success, which would hang a non-TTY test.
		// Its pre-TUI guards (format validation, session selection) run before
		// the program starts, so those error paths are the safe surface to test.
		{
			name:    "breakdown rejects non-table format before launching TUI",
			args:    []string{"breakdown", projFlag, "-f", "json"},
			wantErr: "not supported in live/TUI mode",
		},
		{
			name:    "breakdown reports an unknown session before launching TUI",
			args:    []string{"breakdown", projFlag, "no-such-session"},
			wantErr: "session not found",
		},
		{
			name:    "invalid format is rejected",
			args:    []string{"show", projFlag, "-f", "xml"},
			wantErr: "invalid format",
		},
		{
			name:    "invalid sort-by is rejected",
			args:    []string{"global", "--sort-by", "bogus"},
			wantErr: "invalid --sort-by",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupE2EFixture(t)
			stdout, stderr, err := executeCLISplit(t, tt.args...)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil\nstdout: %s", tt.wantErr, stdout)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
			}
			if tt.check != nil {
				tt.check(t, stdout)
			}
		})
	}
}

// TestE2EWarningsGoToStderr verifies unknown-model warnings land on stderr while
// stdout stays clean JSON — the property the config refactor's writer routing
// exists to guarantee for machine formats.
func TestE2EWarningsGoToStderr(t *testing.T) {
	root := t.TempDir()
	sessionID := "dddddddd-0000-1111-2222-333333333333"
	path := filepath.Join(root, "projects", "-home-test-unknown", sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := e2eMsg("2026-02-04T10:00:00Z", "u1", "claude-made-up-9", 1000, 500, 0, 0, 0) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	stdout, stderr, err := executeCLISplit(t, "show", "--project-dir=-home-test-unknown", sessionID, "-f", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr, "unknown model") {
		t.Errorf("stderr should warn about the unknown model, got: %q", stderr)
	}
	// stdout must remain parseable despite the warning (mustJSON already fails
	// if any warning text precedes the JSON). Also assert the warning's unique
	// phrasing never appears in stdout — "Warning" alone would false-match the
	// test-name-derived temp path embedded in project_path.
	var a models.SessionAnalysis
	mustJSON(t, stdout, &a)
	if strings.Contains(stdout, "using fallback pricing") {
		t.Errorf("warning leaked into stdout: %q", stdout)
	}
}

// TestE2EReentrant guards the ARCH-3 fix: flag state must not leak between
// invocations. The original bug was watch mutating a package-level live=true
// that persisted; here we confirm a prior -f json run doesn't taint a later
// default run, which only holds if each Execute gets a fresh config.
func TestE2EReentrant(t *testing.T) {
	setupE2EFixture(t)

	jsonOut, _, err := executeCLISplit(t, "list", projFlag, "-f", "json")
	if err != nil {
		t.Fatalf("json run failed: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(jsonOut), "[") {
		t.Fatalf("first run should be JSON, got: %q", jsonOut)
	}

	tableOut, _, err := executeCLISplit(t, "list", projFlag)
	if err != nil {
		t.Fatalf("table run failed: %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(tableOut), "[") {
		t.Errorf("second run should be a table (format leaked from prior run): %q", tableOut)
	}
}

// --- assertion helpers ---

func mustContainAll(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("output missing %q\n---\n%s\n---", sub, s)
		}
	}
}

func mustJSON(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("output is not valid JSON: %v\n---\n%s\n---", err, s)
	}
}

func mustCSV(t *testing.T, s string) [][]string {
	t.Helper()
	records, err := csv.NewReader(strings.NewReader(strings.TrimSpace(s))).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v\n---\n%s\n---", err, s)
	}
	return records
}

// mustFloat parses a CSV cost cell.
func mustFloat(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("parse %q as float: %v", s, err)
	}
	return v
}

// betaSessionTotalCost reads the beta session's total from `show -f csv`, the
// session-granularity view of the same analysis `--messages` breaks out.
func betaSessionTotalCost(t *testing.T) float64 {
	t.Helper()
	out, _, err := executeCLISplit(t, "show", projFlag, e2eBetaID, "-f", "csv")
	if err != nil {
		t.Fatalf("show -f csv: %v", err)
	}
	records := mustCSV(t, out)
	i := slices.Index(records[0], "total_cost")
	if i == -1 {
		t.Fatalf("total_cost missing from header %v", records[0])
	}
	return mustFloat(t, records[1][i])
}

// TestE2EGlobalExposesSkippedInputs drives `global` over a project holding one
// good session, one unreadable session, and one unreadable agent directory. The
// json document and the stderr warnings must both account for what the totals
// left out — before, session_count silently counted a session no cost came from.
func TestE2EGlobalExposesSkippedInputs(t *testing.T) {
	root := t.TempDir()
	projDir := filepath.Join(root, "projects", "-home-test-skips")
	sessOK := "11111111-aaaa-bbbb-cccc-000000000000"
	sessBad := "22222222-aaaa-bbbb-cccc-000000000000"
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Good session: two billable lines plus one the strict parse rejects.
	good := e2eMsg("2026-02-01T10:00:00Z", "g1", "claude-opus-4-8", 1000, 500, 0, 0, 0) + "\n" +
		e2eMsg("2026-02-01T16:00:00Z", "g2", "claude-opus-4-8", 1000, 500, 0, 0, 0) + "\n" +
		`{"type":"assistant","timestamp":"not-a-time","requestId":"r9","message":{"id":"m9","usage":{"input_tokens":10}}}` + "\n"
	if err := os.WriteFile(filepath.Join(projDir, sessOK+".jsonl"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}

	// An unreadable agent dir under the good session.
	subagents := filepath.Join(projDir, sessOK, "subagents")
	if err := os.MkdirAll(subagents, 0o755); err != nil {
		t.Fatal(err)
	}
	agentLine := e2eMsg("2026-02-01T11:00:00Z", "a1", "claude-sonnet-5", 500, 100, 0, 0, 0) + "\n"
	if err := os.WriteFile(filepath.Join(subagents, "agent-x.jsonl"), []byte(agentLine), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(subagents, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(subagents, 0o755) })
	if _, err := os.ReadDir(subagents); err == nil {
		t.Skip("chmod 000 does not bar directory reads (running as root?)")
	}

	// An unreadable session file.
	badPath := filepath.Join(projDir, sessBad+".jsonl")
	if err := os.WriteFile(badPath, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(badPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badPath, 0o644) })

	t.Setenv("CLAUDE_CONFIG_DIR", root)
	stdout, stderr, err := executeCLISplit(t, "global", "-f", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
	}

	var g models.GlobalAnalysis
	mustJSON(t, stdout, &g)
	if g.SessionCount != 1 {
		t.Errorf("session_count: got %d, want 1 (the session the totals cover)", g.SessionCount)
	}
	if g.SkippedSessions != 1 {
		t.Errorf("skipped_sessions: got %d, want 1", g.SkippedSessions)
	}
	if g.SkippedAgents != 1 {
		t.Errorf("skipped_agents: got %d, want 1", g.SkippedAgents)
	}
	if g.SkippedLines != 1 {
		t.Errorf("skipped_lines: got %d, want 1", g.SkippedLines)
	}
	if len(g.Projects) != 1 || g.Projects[0].SkippedSessions != 1 {
		t.Errorf("per-project skipped_sessions missing: %+v", g.Projects)
	}
	// AGG-02: the span comes from message timestamps, not file mtimes.
	if got := g.Duration.Duration(); got != 6*time.Hour {
		t.Errorf("duration: got %v, want 6h0m0s (message timestamps, not mtimes)", got)
	}

	for _, want := range []string{
		"1 session(s) could not be parsed",
		"1 agent sub-session(s) could not be read",
		"unparseable line(s) skipped",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q, got: %q", want, stderr)
		}
	}
}

// TestE2EListShowCountParity pins DEDUP-01: `list` derived its message counts
// from a laxer decode than the analysis, so a line show skipped was counted by
// list. Both surfaces must now agree, and list must warn about the skip.
func TestE2EListShowCountParity(t *testing.T) {
	root := t.TempDir()
	projDir := filepath.Join(root, "projects", "-home-test-parity")
	sessionID := "33333333-aaaa-bbbb-cccc-000000000000"
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Line 2's timestamp fails the strict decode the analysis performs.
	content := e2eMsg("2026-02-01T10:00:00Z", "p1", "claude-opus-4-8", 1000, 500, 0, 0, 0) + "\n" +
		`{"type":"assistant","timestamp":"not-a-time","requestId":"r2","message":{"id":"m2","usage":{"input_tokens":10}}}` + "\n" +
		e2eMsg("2026-02-01T10:05:00Z", "p3", "claude-opus-4-8", 200, 100, 0, 0, 0) + "\n"
	if err := os.WriteFile(filepath.Join(projDir, sessionID+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	listOut, listErr, err := executeCLISplit(t, "list", "--project-dir=-home-test-parity", "-f", "json")
	if err != nil {
		t.Fatalf("list: %v\nstderr: %s", err, listErr)
	}
	var entries []models.SessionEntry
	mustJSON(t, listOut, &entries)
	if len(entries) != 1 {
		t.Fatalf("list entries: got %d, want 1", len(entries))
	}

	showOut, _, err := executeCLISplit(t, "show", "--project-dir=-home-test-parity", sessionID, "-f", "json")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	var a models.SessionAnalysis
	mustJSON(t, showOut, &a)

	if entries[0].MessageCount != a.MessageCount {
		t.Errorf("list message_count %d != show message_count %d",
			entries[0].MessageCount, a.MessageCount)
	}
	if a.MessageCount != 2 {
		t.Errorf("show message_count: got %d, want 2", a.MessageCount)
	}
	if entries[0].SkippedLines != 1 {
		t.Errorf("list skipped_lines: got %d, want 1", entries[0].SkippedLines)
	}
	if !strings.Contains(listErr, "unparseable line(s) skipped") {
		t.Errorf("list should warn on stderr about the skipped line, got: %q", listErr)
	}
}

// TestE2EEstimatedCostSurface pins COST-04/D14: a session whose cache-write
// tokens carry no TTL detail (the older format) must say so — the json export
// carries estimated_cost_messages and stderr warns — while fully detailed
// data reports nothing, so exact totals never look approximate.
func TestE2EEstimatedCostSurface(t *testing.T) {
	root := t.TempDir()
	projDir := filepath.Join(root, "projects", "-home-test-estimated")
	sessionID := "44444444-aaaa-bbbb-cccc-000000000000"
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// One old-format line (flat count only) + one detailed line.
	content := `{"type":"assistant","timestamp":"2026-02-01T10:00:00Z","requestId":"r1","message":{"id":"m1","model":"claude-opus-4-8","usage":{"input_tokens":1000,"output_tokens":500,"cache_creation_input_tokens":800}}}` + "\n" +
		e2eMsg("2026-02-01T10:05:00Z", "m2", "claude-opus-4-8", 200, 100, 300, 400, 0) + "\n"
	if err := os.WriteFile(filepath.Join(projDir, sessionID+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	stdout, stderr, err := executeCLISplit(t, "show", "--project-dir=-home-test-estimated", sessionID, "-f", "json")
	if err != nil {
		t.Fatalf("show: %v\nstderr: %s", err, stderr)
	}
	var a models.SessionAnalysis
	mustJSON(t, stdout, &a)
	if a.EstimatedCostMessages != 1 {
		t.Errorf("estimated_cost_messages: got %d, want 1", a.EstimatedCostMessages)
	}
	if !strings.Contains(stderr, "1 message(s) lack cache-write TTL detail") {
		t.Errorf("stderr missing the estimated-pricing warning, got: %q", stderr)
	}
	// The reconciled representation reaches the export: every write token is
	// TTL-attributed, buckets summing to the flat count.
	if a.TotalUsage.CacheCreation == nil {
		t.Fatalf("total_usage.cache_creation missing from reconciled export")
	}
	sum := a.TotalUsage.CacheCreation.Ephemeral5mInputTokens + a.TotalUsage.CacheCreation.Ephemeral1hInputTokens
	if sum != a.TotalUsage.CacheCreationInputTokens {
		t.Errorf("exported buckets sum %d != flat %d", sum, a.TotalUsage.CacheCreationInputTokens)
	}

	// A fully detailed session must not carry the field or the warning.
	exactID := "55555555-aaaa-bbbb-cccc-000000000000"
	exact := e2eMsg("2026-02-01T10:00:00Z", "e1", "claude-opus-4-8", 1000, 500, 300, 400, 0) + "\n"
	if err := os.WriteFile(filepath.Join(projDir, exactID+".jsonl"), []byte(exact), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = executeCLISplit(t, "show", "--project-dir=-home-test-estimated", exactID, "-f", "json")
	if err != nil {
		t.Fatalf("show exact: %v", err)
	}
	if strings.Contains(stdout, "estimated_cost_messages") {
		t.Errorf("exact session exported estimated_cost_messages: %s", stdout)
	}
	if strings.Contains(stderr, "cache-write TTL") {
		t.Errorf("exact session warned about estimated pricing: %q", stderr)
	}
}
