package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/styles"
)

func TestStatusLineParts(t *testing.T) {
	insights := func(n int, recent float64, trend models.TrendDirection) *models.MessageInsights {
		return &models.MessageInsights{MessageCount: n, RecentAvgCost: recent, CostTrend: trend}
	}
	full := func() *models.SessionAnalysis {
		return &models.SessionAnalysis{
			TotalCost:  models.CostBreakdown{TotalCost: 43.66796885},
			AgentsCost: models.CostBreakdown{TotalCost: 12.54245205},
			Context:    &models.ContextUsage{Percent: 67.8235},
			Insights:   insights(424, 0.18954351, models.TrendIncreasing),
		}
	}
	tests := []struct {
		name   string
		model  string
		change func(*models.SessionAnalysis)
		want   string
	}{
		{"every part", "Opus 5.5", nil, "Opus 5.5 · ctx 68% · $0.19/msg ▲ · $43.67 (agents $12.54)"},
		{"falling", "Opus 5.5", func(a *models.SessionAnalysis) { a.Insights.CostTrend = models.TrendDecreasing },
			"Opus 5.5 · ctx 68% · $0.19/msg ▼ · $43.67 (agents $12.54)"},
		{"stable has no arrow", "Opus 5.5", func(a *models.SessionAnalysis) { a.Insights.CostTrend = models.TrendStable },
			"Opus 5.5 · ctx 68% · $0.19/msg · $43.67 (agents $12.54)"},
		{"five messages have no recent cost", "Opus 5.5", func(a *models.SessionAnalysis) { a.Insights = insights(5, 0, models.TrendStable) },
			"Opus 5.5 · ctx 68% · $43.67 (agents $12.54)"},
		{"six messages have one", "Opus 5.5", func(a *models.SessionAnalysis) { a.Insights.MessageCount = 6 },
			"Opus 5.5 · ctx 68% · $0.19/msg ▲ · $43.67 (agents $12.54)"},
		{"no insights", "Opus 5.5", func(a *models.SessionAnalysis) { a.Insights = nil },
			"Opus 5.5 · ctx 68% · $43.67 (agents $12.54)"},
		{"no context reading", "Opus 5.5", func(a *models.SessionAnalysis) { a.Context = nil },
			"Opus 5.5 · $0.19/msg ▲ · $43.67 (agents $12.54)"},
		{"no agents", "Opus 5.5", func(a *models.SessionAnalysis) { a.AgentsCost = models.CostBreakdown{} },
			"Opus 5.5 · ctx 68% · $0.19/msg ▲ · $43.67"},
		{"no model", "", nil, "ctx 68% · $0.19/msg ▲ · $43.67 (agents $12.54)"},
		{"nothing yet", "Opus 5.5", func(a *models.SessionAnalysis) { *a = models.SessionAnalysis{} }, "Opus 5.5 · $0.00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := full()
			if tt.change != nil {
				tt.change(a)
			}
			if got := statusLine(tt.model, a); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}

	styles.SetASCII(true)
	t.Cleanup(func() { styles.SetASCII(false) })
	if got, want := statusLine("Opus 5.5", full()), "Opus 5.5 | ctx 68% | $0.19/msg ^ | $43.67 (agents $12.54)"; got != want {
		t.Errorf("--ascii:\n got  %q\nwant %q", got, want)
	}
}

const (
	statusProjDir = "-home-test-status"
	statusID      = "dddddddd-1111-2222-3333-444444444444"
	statusSoloID  = "eeeeeeee-1111-2222-3333-444444444444"
)

// setupStatusFixture writes a project with two sessions: one of seven
// messages whose costs climb, with an agent, and one of three messages with
// none. It returns the project's directory.
func setupStatusFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	proj := filepath.Join(root, "projects", statusProjDir)
	write := func(rel string, lines ...string) {
		t.Helper()
		path := filepath.Join(proj, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var climbing []string
	for i := range 7 {
		climbing = append(climbing, e2eMsg(fmt.Sprintf("2026-03-01T10:0%d:00Z", i), fmt.Sprintf("m%d", i),
			"claude-opus-5-5", 1000, int64(500*(i+1)*(i+1)), 0, 0, int64(20000*(i+1))))
	}
	write(statusID+".jsonl", climbing...)
	write(filepath.Join(statusID, "subagents", "agent-a1.jsonl"),
		e2eMsg("2026-03-01T10:03:30Z", "s1", "claude-sonnet-5", 2000, 1200, 5000, 0, 0))
	write(statusSoloID+".jsonl",
		e2eMsg("2026-03-02T10:00:00Z", "n1", "claude-sonnet-5", 1000, 400, 0, 0, 0),
		e2eMsg("2026-03-02T10:01:00Z", "n2", "claude-sonnet-5", 1000, 400, 0, 0, 30000),
		e2eMsg("2026-03-02T10:02:00Z", "n3", "claude-sonnet-5", 1000, 400, 0, 0, 40000),
	)
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	return proj
}

// statusInput is the JSON Claude Code writes on statusline's stdin, with a
// few of the fields statusline doesn't read. json.Marshal escapes a Windows
// path's backslashes, as Claude Code does.
func statusInput(t *testing.T, transcript, model string) string {
	t.Helper()
	in := map[string]any{
		"session_id":      statusID,
		"transcript_path": transcript,
		"cwd":             filepath.Dir(transcript),
		"version":         "2.1.300",
		"workspace":       map[string]any{"current_dir": filepath.Dir(transcript), "added_dirs": []string{}},
		"cost":            map[string]any{"total_cost_usd": 1.5},
		"context_window":  map[string]any{"used_percentage": nil},
	}
	if model != "" {
		in["model"] = map[string]any{"id": "claude-opus-5-5", "display_name": model}
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func executeStatusline(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut strings.Builder
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"statusline"}, args...))
	err = root.Execute()
	return out.String(), errOut.String(), err
}

// scriptLine is what the README's jq status line script printed for a
// session, from show's json: the spec statusline replaces it with.
func scriptLine(t *testing.T, transcript, model string) string {
	t.Helper()
	out, _, err := executeCLISplit(t, "show", transcript, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	type cost struct {
		TotalCost float64 `json:"total_cost"`
	}
	var s struct {
		TotalCost  cost `json:"total_cost"`
		AgentsCost cost `json:"agents_cost"`
		Context    *struct {
			Percent float64 `json:"percent"`
		} `json:"context"`
		Insights struct {
			RecentAvgCost *float64 `json:"recent_avg_cost"`
			CostTrend     *string  `json:"cost_trend"`
		} `json:"insights"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}
	line := model
	if s.Context != nil {
		line += fmt.Sprintf(" · ctx %.0f%%", s.Context.Percent)
	}
	if s.Insights.CostTrend != nil {
		line += fmt.Sprintf(" · $%.2f/msg", *s.Insights.RecentAvgCost)
		switch *s.Insights.CostTrend {
		case "increasing":
			line += " ▲"
		case "decreasing":
			line += " ▼"
		}
	}
	line += fmt.Sprintf(" · $%.2f", s.TotalCost.TotalCost)
	if s.AgentsCost.TotalCost != 0 {
		line += fmt.Sprintf(" (agents $%.2f)", s.AgentsCost.TotalCost)
	}
	return line + "\n"
}

func TestStatuslinePrintsWhatTheScriptDid(t *testing.T) {
	proj := setupStatusFixture(t)
	for _, id := range []string{statusID, statusSoloID} {
		t.Run(id[:8], func(t *testing.T) {
			// filepath.Join gives a path with backslashes on Windows, which
			// the JSON carries escaped.
			transcript := filepath.Join(proj, id+".jsonl")
			stdout, stderr, err := executeStatusline(t, statusInput(t, transcript, "Opus 5.5"))
			if err != nil {
				t.Fatal(err)
			}
			if want := scriptLine(t, transcript, "Opus 5.5"); stdout != want {
				t.Errorf("got  %q\nwant %q", stdout, want)
			}
			if stderr != "" {
				t.Errorf("stderr: %q", stderr)
			}
		})
	}

	// Pin the parts each session must show, so the comparison above can't
	// pass on two lines that both leave everything out.
	full, _, _ := executeStatusline(t, statusInput(t, filepath.Join(proj, statusID+".jsonl"), "Opus 5.5"))
	for _, part := range []string{" · ctx ", "/msg ▲ · $", " (agents $"} {
		if !strings.Contains(full, part) {
			t.Errorf("seven climbing messages and an agent: %q lacks %q", full, part)
		}
	}
	short, _, _ := executeStatusline(t, statusInput(t, filepath.Join(proj, statusSoloID+".jsonl"), "Opus 5.5"))
	if strings.Contains(short, "/msg") || strings.Contains(short, "agents") {
		t.Errorf("three messages and no agents: %q", short)
	}
}

// Without model.display_name, the line names the session's last model the
// way ficha's reports do.
func TestStatuslineModelFallback(t *testing.T) {
	proj := setupStatusFixture(t)
	stdout, _, err := executeStatusline(t, statusInput(t, filepath.Join(proj, statusSoloID+".jsonl"), ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout, "Sonnet 5 · ") {
		t.Errorf("got %q, want it to start with the session's model", stdout)
	}
}

func TestStatuslineReadsAWindowsPath(t *testing.T) {
	var in statusLineInput
	if err := json.Unmarshal([]byte(`{"transcript_path":"C:\\Users\\me\\.claude\\projects\\C--src-app\\s.jsonl","model":{"display_name":"Opus 5.5"}}`), &in); err != nil {
		t.Fatal(err)
	}
	if want := `C:\Users\me\.claude\projects\C--src-app\s.jsonl`; in.TranscriptPath != want {
		t.Errorf("transcript_path = %q, want %q", in.TranscriptPath, want)
	}
}

// Claude Code shows whatever reaches stdout, so a failure must leave it
// empty, with the reason on stderr and the exit status saying which kind.
func TestStatuslineFailures(t *testing.T) {
	proj := setupStatusFixture(t)
	transcript := filepath.Join(proj, statusID+".jsonl")
	good := statusInput(t, transcript, "Opus 5.5")
	tests := []struct {
		name    string
		stdin   string
		args    []string
		code    int
		wantErr string
	}{
		{"empty stdin", "", nil, 1, "no JSON on stdin"},
		{"invalid JSON", "{transcript_path", nil, 1, "reading Claude Code's status line JSON on stdin"},
		{"no transcript_path", `{"model":{"display_name":"Opus 5.5"}}`, nil, 1, "no transcript_path"},
		{"missing transcript", statusInput(t, filepath.Join(proj, "nope.jsonl"), "Opus 5.5"), nil, 1, "can't read transcript"},
		{"-f json", good, []string{"-f", "json"}, 2, "--format json is not supported by statusline"},
		{"-f csv", good, []string{"-f", "csv"}, 2, "--format csv is not supported by statusline"},
		{"an argument", good, []string{transcript}, 2, "unknown command"},
		{"a project flag", good, []string{"-p", proj}, 2, "unknown shorthand flag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, err := executeStatusline(t, tt.stdin, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
			}
			if code := exitCode(err); code != tt.code {
				t.Errorf("exit status %d, want %d", code, tt.code)
			}
			if stdout != "" {
				t.Errorf("stdout: %q", stdout)
			}
		})
	}
}
