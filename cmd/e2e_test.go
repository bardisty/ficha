package cmd

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
			name: "show csv --messages switches to per-message rows",
			args: []string{"show", projFlag, e2eAlphaID, "-f", "csv", "--messages"},
			check: func(t *testing.T, out string) {
				records := mustCSV(t, out)
				if records[0][0] != "timestamp" {
					t.Errorf("header col 0: got %q, want timestamp (per-message table)", records[0][0])
				}
				if len(records) != 3 { // header + 2 messages (alpha has two)
					t.Fatalf("show csv --messages: got %d rows, want 3 (header + 2 messages)", len(records))
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
				total := 0
				for _, s := range d.Sessions {
					total += len(s.Agents)
				}
				if total != 1 { // beta has exactly one agent sub-session
					t.Errorf("nested agents across sessions: got %d, want 1", total)
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
				if len(records) != 4 { // header + 2 sessions + 1 agent
					t.Fatalf("rows: got %d, want 4 (header + 2 sessions + 1 agent)", len(records))
				}
				agentRows := 0
				for _, r := range records[1:] {
					if r[0] == "agent" {
						agentRows++
					}
				}
				if agentRows != 1 {
					t.Errorf("agent rows: got %d, want 1", agentRows)
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
