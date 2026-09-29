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
	"github.com/bardisty/ficha/internal/styles"
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
				// so this passing is itself the proof that show doesn't stack a
				// second per-message table with a different column count.
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
				mustContainAll(t, out, "AGENT SUB-SESSIONS", "workflow: e2e-flow (completed)", "[Aw1]")
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
				mustContainAll(t, out, "PROJECTS (all 2, by cost)", "Projects: 2", "TOTAL")
			},
		},
		{
			name: "global --top 1 limits the projects shown",
			args: []string{"global", "--top", "1"},
			check: func(t *testing.T, out string) {
				// Header count reflects the cap without depending on decoded names.
				mustContainAll(t, out, "PROJECTS (1 of 2, by cost)", "Projects: 2")
				if strings.Contains(out, "PROJECTS (all 2, by cost)") {
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
			name: "global --top does not truncate json (machine formats export all)",
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
			name: "global --top does not truncate csv (machine formats export all)",
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
	if strings.Contains(stdout, "priced at fallback") {
		t.Errorf("warning leaked into stdout: %q", stdout)
	}
}

// TestE2EReentrant: flag state must not leak between invocations. A
// package-level flag one command mutates (watch setting live=true) would
// persist into the next; here we confirm a prior -f json run doesn't taint a
// later default run, which only holds if each Execute gets a fresh config.
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

// TestE2EProjectPathJoinsShowAndSummary: project_path names the project
// directory (not the transcript) and is identical on show, the summary
// aggregate, and every per-session detail record, so machine consumers can
// join them. session_file carries the transcript path (empty on the
// aggregate, which spans many files).
func TestE2EProjectPathJoinsShowAndSummary(t *testing.T) {
	setupE2EFixture(t)

	// show: project_path is the project dir; session_file is this transcript.
	showOut, _, err := executeCLISplit(t, "show", projFlag, e2eAlphaID, "-f", "json")
	if err != nil {
		t.Fatalf("show json failed: %v", err)
	}
	var show models.SessionAnalysis
	mustJSON(t, showOut, &show)
	if show.ProjectPath == "" {
		t.Fatal("show project_path is empty")
	}
	if filepath.Base(show.ProjectPath) != e2eProjDir {
		t.Errorf("show project_path %q should end in the project dir %q", show.ProjectPath, e2eProjDir)
	}
	if filepath.Base(show.SessionFile) != e2eAlphaID+".jsonl" {
		t.Errorf("show session_file %q should be the transcript", show.SessionFile)
	}
	if filepath.Dir(show.SessionFile) != show.ProjectPath {
		t.Errorf("show project_path %q should be the dir of session_file %q", show.ProjectPath, show.SessionFile)
	}

	// csv carries the same two columns with the same values as json.
	csvOut, _, err := executeCLISplit(t, "show", projFlag, e2eAlphaID, "-f", "csv")
	if err != nil {
		t.Fatalf("show csv failed: %v", err)
	}
	csvRows := mustCSV(t, csvOut)
	if len(csvRows) != 2 {
		t.Fatalf("show csv: got %d rows, want header + 1", len(csvRows))
	}
	col := func(name string) string {
		for i, h := range csvRows[0] {
			if h == name {
				return csvRows[1][i]
			}
		}
		t.Fatalf("show csv missing column %q", name)
		return ""
	}
	if col("project_path") != show.ProjectPath {
		t.Errorf("csv project_path %q != json %q", col("project_path"), show.ProjectPath)
	}
	if col("session_file") != show.SessionFile {
		t.Errorf("csv session_file %q != json %q", col("session_file"), show.SessionFile)
	}

	// summary aggregate: same project_path (joinable), empty session_file.
	sumOut, _, err := executeCLISplit(t, "summary", projFlag, "-f", "json")
	if err != nil {
		t.Fatalf("summary json failed: %v", err)
	}
	var agg models.SessionAnalysis
	mustJSON(t, sumOut, &agg)
	if agg.ProjectPath != show.ProjectPath {
		t.Errorf("summary project_path %q != show %q (not joinable)", agg.ProjectPath, show.ProjectPath)
	}
	if agg.SessionFile != "" {
		t.Errorf("summary aggregate session_file %q should be empty", agg.SessionFile)
	}

	// summary --details: each per-session record joins on project_path and
	// carries its own transcript.
	detOut, _, err := executeCLISplit(t, "summary", projFlag, "-d", "-f", "json")
	if err != nil {
		t.Fatalf("summary details json failed: %v", err)
	}
	var det models.SummaryDetail
	mustJSON(t, detOut, &det)
	if len(det.Sessions) == 0 {
		t.Fatal("summary details returned no sessions")
	}
	for _, s := range det.Sessions {
		if s.ProjectPath != agg.ProjectPath {
			t.Errorf("session %s project_path %q != aggregate %q", s.SessionID, s.ProjectPath, agg.ProjectPath)
		}
		if filepath.Base(s.SessionFile) != s.SessionID+".jsonl" {
			t.Errorf("session %s session_file %q should be its transcript", s.SessionID, s.SessionFile)
		}
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
	// The span comes from message timestamps, not file mtimes.
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

// TestE2EListShowCountParity: if `list` derived its message counts from a
// laxer decode than the analysis, a line show skips would still be counted by
// list. Both surfaces must agree, and list must warn about the skip.
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

// TestE2EEstimatedCostSurface: a session whose cache-write
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

// TestE2ESummarySessionCountExcludesUnparseable: summary's "Summary: N
// sessions" must not count a discovered-but-unparseable session that
// contributes nothing to the totals, or it disagrees with global's per-project
// session_count for the same directory. Both surfaces must report the same
// count, and the skipped-session warning must still disclose the remainder.
func TestE2ESummarySessionCountExcludesUnparseable(t *testing.T) {
	root := t.TempDir()
	projName := "-home-test-rollup"
	projDir := filepath.Join(root, "projects", projName)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	good := e2eMsg("2026-03-01T10:00:00Z", "g1", "claude-opus-4-8", 1000, 500, 0, 0, 0) + "\n"
	if err := os.WriteFile(filepath.Join(projDir, "11111111-aaaa-bbbb-cccc-000000000000.jsonl"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}

	// A discovered-but-unreadable session: no cost comes from it.
	badPath := filepath.Join(projDir, "22222222-aaaa-bbbb-cccc-000000000000.jsonl")
	if err := os.WriteFile(badPath, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(badPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badPath, 0o644) })
	if f, err := os.Open(badPath); err == nil {
		_ = f.Close()
		t.Skip("chmod 000 does not bar file reads (running as root?)")
	}

	t.Setenv("CLAUDE_CONFIG_DIR", root)

	// summary: the rendered count excludes the unparseable session and warns.
	stdout, stderr, err := executeCLISplit(t, "summary", "--project-dir="+projName)
	if err != nil {
		t.Fatalf("summary: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "Summary: 1 session") {
		t.Errorf("summary count should exclude the unparseable session; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "Summary: 2 session") {
		t.Errorf("summary counted the unparseable session:\n%s", stdout)
	}
	if !strings.Contains(stderr, "could not be parsed") {
		t.Errorf("summary should warn about the skipped session; stderr: %q", stderr)
	}

	// global: its per-project session_count must agree with summary's count.
	gout, _, err := executeCLISplit(t, "global", "-f", "json")
	if err != nil {
		t.Fatalf("global: %v", err)
	}
	var g models.GlobalAnalysis
	mustJSON(t, gout, &g)
	if len(g.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(g.Projects))
	}
	if g.Projects[0].SessionCount != 1 {
		t.Errorf("global session_count: got %d, want 1 (must agree with summary)", g.Projects[0].SessionCount)
	}
}

// TestE2EUsageErrors pins the messages for input mistakes caught before any
// analysis: a mistyped command at the root (which otherwise takes a session
// ID), stray args, and flag values pflag can't parse.
func TestE2EUsageErrors(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my project")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"typo suggests the command", []string{"lst"}, `unknown command "lst" for "ficha". Did you mean "list"?`},
		{"prefix lists every match", []string{"s"}, `unknown command "s" for "ficha". Did you mean "show" or "summary"?`},
		{"unrelated word points at help", []string{"frobnicate"}, `unknown command "frobnicate" for "ficha". Run 'ficha --help' to see the commands.`},
		{"directory points at -p", []string{dir}, "unknown command " + strconv.Quote(dir) + ` for "ficha". To analyze that directory, run: ficha -p ` + shellQuote(dir)},
		{"unmatched word in the project", []string{projFlag, "frobnicate"}, `unknown command "frobnicate" for "ficha". Run 'ficha --help' to see the commands.`},
		{"hex arg is still a session lookup", []string{projFlag, "abc123"}, "session not found: abc123"},
		{"uppercase hex is a session lookup", []string{projFlag, "ABC123"}, "session not found: ABC123"},
		{"pasted filename is a session lookup", []string{projFlag, "abc123.jsonl"}, "session not found: abc123.jsonl"},
		{"stray arg on a no-arg command", []string{"list", "extra"}, `ficha list takes no arguments, got "extra"`},
		{"non-numeric int flag", []string{"global", "--top", "abc"}, `invalid --top value "abc": must be a whole number`},
		{"overflowing int flag", []string{"global", "--top", "99999999999999999999"}, `invalid --top value "99999999999999999999": is out of range`},
		{"non-boolean bool flag", []string{"global", "--details=maybe"}, `invalid --details value "maybe": must be true or false`},
		{"unknown flag passes through", []string{"global", "--bogus"}, "unknown flag: --bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupE2EFixture(t)
			stdout, _, err := executeCLISplit(t, tt.args...)
			if err == nil {
				t.Fatalf("expected error %q, got nil\nstdout: %s", tt.wantErr, stdout)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error:\n got: %s\nwant: %s", err, tt.wantErr)
			}
		})
	}
}

// TestE2ERootShowsNonHexSessionID: older Claude Code wrote agent-<id>.jsonl
// transcripts at the project root, and they list as sessions. Bare `ficha
// <id>` must still show them rather than call the ID an unknown command.
func TestE2ERootShowsNonHexSessionID(t *testing.T) {
	root := setupE2EFixture(t)
	legacy := filepath.Join(root, "projects", e2eProjDir, "agent-a1b2c3d4.jsonl")
	line := e2eMsg("2026-02-01T11:00:00Z", "l1", "claude-opus-4-8", 100, 50, 0, 0, 0) + "\n"
	if err := os.WriteFile(legacy, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := executeCLISplit(t, projFlag, "agent-a1b2c3d4", "-f", "json")
	if err != nil {
		t.Fatalf("ficha agent-a1b2c3d4: %v\nstderr: %s", err, stderr)
	}
	var a models.SessionAnalysis
	mustJSON(t, stdout, &a)
	if a.SessionID != "agent-a1b2c3d4" {
		t.Errorf("session_id = %q, want agent-a1b2c3d4", a.SessionID)
	}
}

// TestE2EFlagScope: flags are registered only on the commands that use them,
// so a misplaced one fails at parse time instead of being silently ignored,
// and the flags that remain but do nothing in context say so on stderr.
func TestE2EFlagScope(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantErr    string // exact error; empty means the command must succeed
		wantStderr string
	}{
		{name: "list rejects --live", args: []string{"list", projFlag, "--live"}, wantErr: "unknown flag: --live"},
		{name: "summary rejects --no-follow", args: []string{"summary", projFlag, "--no-follow"}, wantErr: "unknown flag: --no-follow"},
		{name: "global rejects -p", args: []string{"global", "-p", "."}, wantErr: "unknown shorthand flag: 'p' in -p"},
		{name: "global rejects --project-dir", args: []string{"global", projFlag}, wantErr: "unknown flag: --project-dir"},
		{
			name:    "-p and --project-dir are exclusive",
			args:    []string{"list", "-p", ".", projFlag},
			wantErr: "use either -p or --project-dir, not both: -p is the directory Claude Code ran in, --project-dir a Claude project directory name",
		},
		// Parsing --live on root and watch is what reaches the TUI format check.
		{name: "root still accepts hidden --live", args: []string{projFlag, "--live", "-f", "json"}, wantErr: "--format json is not supported in live/TUI mode"},
		{name: "watch still accepts hidden --live", args: []string{"watch", projFlag, "--live", "-f", "json"}, wantErr: "--format json is not supported in live/TUI mode"},
		{
			name:       "--messages on a table warns",
			args:       []string{"show", projFlag, e2eAlphaID, "--messages"},
			wantStderr: "Warning: --messages has no effect on table output (use -f json or -f csv)",
		},
		{
			name:       "--no-follow outside live mode warns",
			args:       []string{"show", projFlag, e2eAlphaID, "--no-follow"},
			wantStderr: "Warning: --no-follow has no effect outside live/watch/breakdown mode",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupE2EFixture(t)
			stdout, stderr, err := executeCLISplit(t, tt.args...)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil\nstdout: %s", tt.wantErr, stdout)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("error:\n got: %s\nwant: %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr)
			}
			if !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("stderr missing %q, got: %q", tt.wantStderr, stderr)
			}
		})
	}
}

// TestRootHelp: root help leads with the live commands, says the figures are
// estimates, and keeps the legacy --live spelling out of sight.
func TestRootHelp(t *testing.T) {
	out, err := executeCLI(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	mustContainAll(t, out,
		"API-equivalent costs",
		"On a subscription this is not your bill.",
		"ficha watch ", "ficha breakdown ", "ficha summary ", "ficha global ",
		"--project-dir",
	)
	for _, hidden := range []string{"-l, --live", "--no-follow"} {
		if strings.Contains(out, hidden) {
			t.Errorf("root help should not list %s:\n%s", hidden, out)
		}
	}
}

// completeLines runs cobra's hidden __complete command and returns the
// candidate lines and the trailing ":<directive>" line.
func completeLines(t *testing.T, args ...string) (candidates []string, directive string) {
	t.Helper()
	stdout, _, err := executeCLISplit(t, append([]string{"__complete"}, args...)...)
	if err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if strings.HasPrefix(line, ":") {
			directive = line
			continue
		}
		if line != "" {
			candidates = append(candidates, line)
		}
	}
	return candidates, directive
}

// TestE2ECompletion: session-ID slots offer this project's sessions (never
// filenames), flags with a closed set of values offer them, and commands that
// take no positional args offer nothing.
func TestE2ECompletion(t *testing.T) {
	root := setupE2EFixture(t)
	// Beta newer than alpha, so the newest-first order is observable.
	proj := filepath.Join(root, "projects", e2eProjDir)
	older, newer := time.Now().Add(-3*time.Hour), time.Now().Add(-5*time.Minute)
	if err := os.Chtimes(filepath.Join(proj, e2eAlphaID+".jsonl"), older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(proj, e2eBetaID+".jsonl"), newer, newer); err != nil {
		t.Fatal(err)
	}

	const (
		noFile    = ":4"  // ShellCompDirectiveNoFileComp
		keepOrder = ":36" // NoFileComp | KeepOrder
		dirsOnly  = ":16" // ShellCompDirectiveFilterDirs
	)
	tests := []struct {
		name          string
		args          []string
		want          []string
		wantDirective string
	}{
		{"show offers sessions newest first", []string{"show", projFlag, ""},
			[]string{"bbbbbbbb\tmodified 5m ago · 2 msgs", "aaaaaaaa\tmodified 3h ago · 2 msgs"}, keepOrder},
		{"prefix narrows the sessions", []string{"watch", projFlag, "aa"},
			[]string{"aaaaaaaa\tmodified 3h ago · 2 msgs"}, keepOrder},
		{"typing past 8 chars completes the full ID", []string{"breakdown", projFlag, "aaaaaaaa-1"},
			[]string{e2eAlphaID + "\tmodified 3h ago · 2 msgs"}, keepOrder},
		{"one session ID only", []string{"show", projFlag, e2eAlphaID, ""}, nil, noFile},
		{"--format values", []string{"list", "-f", ""}, []string{"table", "json", "csv"}, noFile},
		{"--sort-by values", []string{"global", "--sort-by", ""}, []string{"cost", "sessions", "name", "activity"}, noFile},
		{"--project-dir offers project dirs", []string{"list", "--project-dir", ""}, []string{e2eOtherDir, e2eProjDir}, noFile},
		// A word starting with "-" completes as a flag name unless it's
		// attached with "=".
		{"--project-dir= filters by prefix", []string{"list", "--project-dir=-home-test-p"}, []string{e2eProjDir}, noFile},
		{"-p completes directories", []string{"summary", "-p", ""}, nil, dirsOnly},
		{"list takes no args", []string{"list", ""}, nil, noFile},
		{"version takes no args", []string{"version", ""}, nil, noFile},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, directive := completeLines(t, tt.args...)
			if !slices.Equal(got, tt.want) {
				t.Errorf("candidates:\n got: %q\nwant: %q", got, tt.want)
			}
			if directive != tt.wantDirective {
				t.Errorf("directive: got %s, want %s", directive, tt.wantDirective)
			}
		})
	}
}

// TestE2ECaseInsensitiveValues: -f and --sort-by accept any case.
func TestE2ECaseInsensitiveValues(t *testing.T) {
	setupE2EFixture(t)
	out, _, err := executeCLISplit(t, "list", projFlag, "-f", "JSON")
	if err != nil {
		t.Fatalf("-f JSON: %v", err)
	}
	var entries []models.SessionEntry
	mustJSON(t, out, &entries)

	out, _, err = executeCLISplit(t, "global", "--sort-by", "Name", "-f", "CSV")
	if err != nil {
		t.Fatalf("--sort-by Name -f CSV: %v", err)
	}
	mustCSV(t, out)

	// An invalid value is echoed as typed.
	_, _, err = executeCLISplit(t, "list", projFlag, "-f", "XML")
	if err == nil || err.Error() != `invalid format "XML": must be one of table, json, csv` {
		t.Errorf("-f XML: got %v", err)
	}
}

// TestE2ECompletionSkipsCountsOverBudget: counting messages parses whole
// transcripts, so past the byte budget the descriptions drop the count.
func TestE2ECompletionSkipsCountsOverBudget(t *testing.T) {
	root := setupE2EFixture(t)
	big := filepath.Join(root, "projects", e2eProjDir, e2eAlphaID+".jsonl")
	f, err := os.OpenFile(big, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	// Blank lines are skipped by the parser and cheap to write.
	if _, err := f.Write(bytes.Repeat([]byte("\n"), countBudgetBytes)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, _ := completeLines(t, "show", projFlag, "")
	if len(got) != 2 {
		t.Fatalf("want 2 candidates, got %q", got)
	}
	for _, c := range got {
		if strings.Contains(c, "msgs") {
			t.Errorf("candidate over budget should not carry a count: %q", c)
		}
	}
}

// TestE2ELiveViewsNeedATerminal: with stdout redirected, the live views fail
// fast with a pointer at the scripting alternative instead of writing frames
// into the pipe. Test stdout is a buffer, which is never a terminal.
func TestE2ELiveViewsNeedATerminal(t *testing.T) {
	const (
		showAlt      = "For scripting, use 'ficha show -f json' (add --messages for per-message rows)."
		breakdownAlt = "For per-message rows in a script, use 'ficha show -f csv --messages'."
	)
	tests := []struct {
		name       string
		args       []string
		wantErr    string
		wantStderr string
	}{
		{name: "watch", args: []string{"watch", projFlag},
			wantErr: "ficha watch needs an interactive terminal, and stdout isn't one. " + showAlt},
		{name: "pinned watch", args: []string{"watch", projFlag, e2eAlphaID},
			wantErr: "ficha watch needs an interactive terminal, and stdout isn't one. " + showAlt},
		{name: "show --live", args: []string{"show", projFlag, "--live"},
			wantErr: "ficha show --live needs an interactive terminal, and stdout isn't one. " + showAlt},
		{name: "root --live", args: []string{projFlag, "-l"},
			wantErr: "ficha --live needs an interactive terminal, and stdout isn't one. " + showAlt},
		{name: "breakdown", args: []string{"breakdown", projFlag},
			wantErr: "ficha breakdown needs an interactive terminal, and stdout isn't one. " + breakdownAlt},
		{name: "--messages in live mode warns without format advice", args: []string{"show", projFlag, "--live", "--messages"},
			wantErr:    "ficha show --live needs an interactive terminal, and stdout isn't one. " + showAlt,
			wantStderr: "Warning: --messages has no effect in live mode\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupE2EFixture(t)
			stdout, stderr, err := executeCLISplit(t, tt.args...)
			if err == nil {
				t.Fatalf("expected error, got nil\nstdout: %s", stdout)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error:\n got: %s\nwant: %s", err, tt.wantErr)
			}
			if stdout != "" {
				t.Errorf("nothing should reach stdout, got %q", stdout)
			}
			if stderr != tt.wantStderr {
				t.Errorf("stderr: got %q, want %q", stderr, tt.wantStderr)
			}
		})
	}
}

// TestE2EWarningsNameTheirSessions: skip warnings on reports spanning several
// sessions say how to find the affected ones, and -v names them; the
// unknown-model warning gives the rate it priced at.
func TestE2EWarningsNameTheirSessions(t *testing.T) {
	root := t.TempDir()
	projName := "-home-test-warn"
	projDir := filepath.Join(root, "projects", projName)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	badLine := `{"type":"assistant","timestamp":"not-a-time","requestId":"r9","message":{"id":"m9","usage":{"input_tokens":10}}}`
	write := func(id, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(projDir, id+".jsonl"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("11111111-aaaa-bbbb-cccc-000000000000", e2eMsg("2026-03-01T10:00:00Z", "g1", "claude-opus-4-8", 1000, 500, 0, 0, 0)+"\n")
	write("22222222-aaaa-bbbb-cccc-000000000000", e2eMsg("2026-03-01T11:00:00Z", "g2", "claude-nova-9", 1000, 500, 0, 0, 0)+"\n"+badLine+"\n"+badLine+"\n")
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	proj := "--project-dir=" + projName

	const (
		linesSummary = "Warning: 2 unparseable line(s) skipped; totals may be undercounted\n"
		linesList    = "Warning: 2 unparseable line(s) skipped; message counts may be undercounted\n"
		hint         = "  Run with -v to list the affected sessions.\n"
		unknown      = "Warning: unknown model \"claude-nova-9\" priced at fallback $3/$15 per MTok\n"
	)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"summary", []string{"summary", proj}, linesSummary + hint + unknown},
		{"summary -v", []string{"summary", proj, "-v"}, linesSummary + "  22222222: 2 lines\n" + unknown},
		{"list", []string{"list", proj}, linesList + hint},
		{"list -v", []string{"list", proj, "-v"}, linesList + "  22222222: 2 lines\n"},
		{"global -v", []string{"global", "-v"}, linesSummary + "  -home-test-warn/22222222: 2 lines\n" + unknown},
		// A single session is the one on screen: nothing to name.
		{"show", []string{"show", proj, "22222222", "-v"}, linesSummary + unknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, err := executeCLISplit(t, tt.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if stderr != tt.want {
				t.Errorf("stderr:\n got: %q\nwant: %q", stderr, tt.want)
			}
		})
	}
}

// TestE2EFootersCarryTheTotal: the footer is what stays on screen after a long
// report, so it leads with the total.
func TestE2EFootersCarryTheTotal(t *testing.T) {
	setupE2EFixture(t)
	for _, args := range [][]string{
		{"show", projFlag, e2eAlphaID, "--no-color"},
		{"summary", projFlag, "--no-color"},
		{"summary", projFlag, "-d", "--no-color"},
		{"global", "--no-color"},
	} {
		stdout, _, err := executeCLISplit(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		lines := strings.Split(stdout, "\n")
		footer := -1
		for i, l := range lines {
			// The heavy rule above the footer; --no-color keeps Unicode frames
			if strings.HasPrefix(l, strings.Repeat(styles.BoxHorizontal, 5)) {
				footer = i
			}
		}
		if footer < 0 || footer+1 >= len(lines) || !strings.HasPrefix(lines[footer+1], "Total: $") {
			t.Errorf("%v: footer should start with the total:\n%s", args, stdout)
		}
	}
}

// TestE2EVerboseNamesUnreadableSession: an unreadable transcript's agents were
// never analyzed, so -v charges them to it and the per-session lines add up
// to the warning's agent count.
func TestE2EVerboseNamesUnreadableSession(t *testing.T) {
	root := t.TempDir()
	projName := "-home-test-unreadable"
	projDir := filepath.Join(root, "projects", projName)
	badID := "33333333-aaaa-bbbb-cccc-000000000000"
	agentPath := filepath.Join(projDir, badID, "subagents", "agent-x.jsonl")
	if err := os.MkdirAll(filepath.Dir(agentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	line := e2eMsg("2026-03-01T10:00:00Z", "g1", "claude-opus-4-8", 1000, 500, 0, 0, 0) + "\n"
	for _, p := range []string{filepath.Join(projDir, "11111111-aaaa-bbbb-cccc-000000000000.jsonl"), agentPath} {
		if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	badPath := filepath.Join(projDir, badID+".jsonl")
	if err := os.WriteFile(badPath, []byte(line), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badPath, 0o644) })
	if f, err := os.Open(badPath); err == nil {
		_ = f.Close()
		t.Skip("chmod 000 does not bar file reads (running as root, or Windows)")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	_, stderr, err := executeCLISplit(t, "summary", "--project-dir="+projName, "-v")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	want := "Warning: 1 session(s) could not be parsed\n" +
		"Warning: 1 agent sub-session(s) could not be read\n" +
		"  33333333: unreadable, 1 agent\n"
	if stderr != want {
		t.Errorf("stderr:\n got: %q\nwant: %q", stderr, want)
	}
}
