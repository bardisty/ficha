package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
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
			Context:    &models.ContextUsage{Tokens: 678235, Window: 1000000, Percent: 67.8235, CompactAt: 967000},
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
		// No window, no percentage: the tokens alone.
		{"window unknown", "claude-mystery-9", func(a *models.SessionAnalysis) { a.Context = &models.ContextUsage{Tokens: 680400} },
			"claude-mystery-9 · ctx 680.4K · $0.19/msg ▲ · $43.67 (agents $12.54)"},
		{"no agents", "Opus 5.5", func(a *models.SessionAnalysis) { a.AgentsCost = models.CostBreakdown{} },
			"Opus 5.5 · ctx 68% · $0.19/msg ▲ · $43.67"},
		{"no model", "", nil, "ctx 68% · $0.19/msg ▲ · $43.67 (agents $12.54)"},
		{"nothing yet", "Opus 5.5", func(a *models.SessionAnalysis) { *a = models.SessionAnalysis{} }, "Opus 5.5 · $0.00"},
		// Under a cent, two decimals would read $0.00 for a real cost.
		{"cheap session", "Haiku 4.5", func(a *models.SessionAnalysis) {
			a.Insights.RecentAvgCost, a.Insights.CostTrend = 0.00271838, models.TrendStable
			a.TotalCost.TotalCost, a.AgentsCost.TotalCost = 0.07791215, 0.00412
		}, "Haiku 4.5 · ctx 68% · $0.0027/msg · $0.08 (agents $0.0041)"},
		{"total under a cent", "Haiku 4.5", func(a *models.SessionAnalysis) {
			a.Insights = nil
			a.TotalCost.TotalCost, a.AgentsCost.TotalCost = 0.0042, 0
		}, "Haiku 4.5 · ctx 68% · $0.0042"},
		// Four decimals would round these up to $0.0100.
		{"at the cutoff", "Haiku 4.5", func(a *models.SessionAnalysis) {
			a.Insights.RecentAvgCost = 0.00995
			a.TotalCost.TotalCost, a.AgentsCost.TotalCost = 0.009996, 0
		}, "Haiku 4.5 · ctx 68% · $0.01/msg ▲ · $0.01"},
		{"just under the cutoff", "Haiku 4.5", func(a *models.SessionAnalysis) {
			a.Insights.RecentAvgCost = 0.0099
			a.TotalCost.TotalCost, a.AgentsCost.TotalCost = 0.005, 0
		}, "Haiku 4.5 · ctx 68% · $0.0099/msg ▲ · $0.0050"},
		{"over $1,000", "Opus 5.5", func(a *models.SessionAnalysis) {
			a.TotalCost.TotalCost, a.AgentsCost.TotalCost = 1234.56, 999.996
		}, "Opus 5.5 · ctx 68% · $0.19/msg ▲ · $1,234.56 (agents $1,000.00)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := full()
			if tt.change != nil {
				tt.change(a)
			}
			if got := statusLine(tt.model, a, true); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}

	styles.SetASCII(true)
	t.Cleanup(func() { styles.SetASCII(false) })
	if got, want := statusLine("Opus 5.5", full(), true), "Opus 5.5 | ctx 68% | $0.19/msg ^ | $43.67 (agents $12.54)"; got != want {
		t.Errorf("--ascii:\n got  %q\nwant %q", got, want)
	}
}

// The ctx part says how near ~compaction the reading is once it's in the
// warning band, against each model's own mark: the words show's note uses,
// short. Below the band it's the percentage alone.
func TestStatusLineCompaction(t *testing.T) {
	reading := func(tokens int64, window, compactAt int) *models.ContextUsage {
		return &models.ContextUsage{Tokens: tokens, Window: window, Percent: float64(tokens) / float64(window) * 100, CompactAt: compactAt}
	}
	tests := []struct {
		name string
		c    *models.ContextUsage
		want string
	}{
		{"1M below the band", reading(678_200, 1_000_000, 967_000), "ctx 68%"},
		{"1M in the band", reading(900_000, 1_000_000, 967_000), "ctx 90%, 67.0K to ~compaction"},
		{"1M at the mark", reading(967_000, 1_000_000, 967_000), "ctx 97%, at ~compaction"},
		{"1M past the mark", reading(980_000, 1_000_000, 967_000), "ctx 98%, past ~compaction"},
		{"200K below the band", reading(150_000, 200_000, 200_000), "ctx 75%"},
		{"200K in the band", reading(180_000, 200_000, 200_000), "ctx 90%, 20.0K to ~compaction"},
		{"200K at the mark", reading(200_000, 200_000, 200_000), "ctx 100%, at ~compaction"},
		// A request past 200K on a 200K model reads against 1M, with the
		// mark at the end of it.
		{"200K model past 200K", reading(250_000, 1_000_000, 1_000_000), "ctx 25%"},
		{"window unknown", &models.ContextUsage{Tokens: 680_400}, "ctx 680.4K"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &models.SessionAnalysis{Context: tt.c}
			want := "Opus 5.5 · " + tt.want + " · $0.00"
			if got := statusLine("Opus 5.5", a, true); got != want {
				t.Errorf("got  %q\nwant %q", got, want)
			}
		})
	}
}

// In color, only the percentage takes its zone's color, and only from the
// band on: 93 (bright yellow) in it and 91 (bright red) past the mark, from
// the basic palette statusline uses. Below the band it's plain.
func TestStatusLineCompactionColor(t *testing.T) {
	styles.SetBasic(true)
	t.Cleanup(func() { styles.SetBasic(false) })
	for _, tt := range []struct {
		c    models.ContextUsage
		want string
	}{
		{models.ContextUsage{Tokens: 678_200, Window: 1_000_000, Percent: 67.82, CompactAt: 967_000}, "ctx 68% · $0.00"},
		{models.ContextUsage{Tokens: 900_000, Window: 1_000_000, Percent: 90, CompactAt: 967_000}, "ctx \x1b[93m90%\x1b[m, 67.0K to ~compaction · $0.00"},
		{models.ContextUsage{Tokens: 980_000, Window: 1_000_000, Percent: 98, CompactAt: 967_000}, "ctx \x1b[91m98%\x1b[m, past ~compaction · $0.00"},
	} {
		if got := statusLine("", &models.SessionAnalysis{Context: &tt.c}, false); got != tt.want {
			t.Errorf("got  %q\nwant %q", got, tt.want)
		}
	}
}

// statusline colors its ctx part on the pipe Claude Code reads, in the 16
// basic colors, and nothing else on the line. --no-color, NO_COLOR and
// CLICOLOR=0 leave it plain, with the same words.
func TestStatuslineColor(t *testing.T) {
	root := t.TempDir()
	transcript := filepath.Join(root, "projects", statusProjDir, statusID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		t.Fatal(err)
	}
	// One request at 900K on Opus 5.5: in the band before its 967K mark.
	msg := e2eMsg("2026-03-01T10:00:00Z", "m1", "claude-opus-5-5", 10, 500, 0, 0, 899_990)
	if err := os.WriteFile(transcript, []byte(msg+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR", "")
	t.Setenv("CLICOLOR_FORCE", "")
	// statusline picks the basic palette, and it must not outlive the test.
	t.Cleanup(func() { styles.SetBasic(false) })
	in := statusInput(t, transcript, "Opus 5.5")
	const plain = "Opus 5.5 · ctx 90%, 67.0K to ~compaction · $0.19\n"

	got, _, err := executeStatusline(t, in)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Opus 5.5 · ctx \x1b[93m90%\x1b[m, 67.0K to ~compaction · $0.19\n"; got != want {
		t.Errorf("colored:\n got  %q\nwant %q", got, want)
	}

	for _, tc := range []struct {
		name string
		env  [2]string
		args []string
	}{
		{"--no-color", [2]string{}, []string{"--no-color"}},
		{"NO_COLOR=1", [2]string{"NO_COLOR", "1"}, nil},
		{"CLICOLOR=0", [2]string{"CLICOLOR", "0"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env[0] != "" {
				t.Setenv(tc.env[0], tc.env[1])
			}
			got, _, err := executeStatusline(t, in, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if got != plain {
				t.Errorf("got  %q\nwant %q", got, plain)
			}
		})
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

// lineFromShowJSON builds the status line from show's json for the same
// transcript, so statusline and show can't disagree on a figure or on which
// parts a session has.
func lineFromShowJSON(t *testing.T, transcript, model string) string {
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
			Tokens  int64   `json:"tokens"`
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
	switch {
	case s.Context == nil:
	case s.Context.Percent == 0:
		line += " · ctx " + render.Number(s.Context.Tokens)
	default:
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

func TestStatuslineAgreesWithShowJSON(t *testing.T) {
	proj := setupStatusFixture(t)
	for _, id := range []string{statusID, statusSoloID} {
		t.Run(id[:8], func(t *testing.T) {
			// filepath.Join gives a path with backslashes on Windows, which
			// the JSON carries escaped.
			transcript := filepath.Join(proj, id+".jsonl")
			// The figures are what's compared; TestStatuslineColor pins
			// the color.
			stdout, stderr, err := executeStatusline(t, statusInput(t, transcript, "Opus 5.5"), "--no-color")
			if err != nil {
				t.Fatal(err)
			}
			if want := lineFromShowJSON(t, transcript, "Opus 5.5"); stdout != want {
				t.Errorf("got  %q\nwant %q", stdout, want)
			}
			if stderr != "" {
				t.Errorf("stderr: %q", stderr)
			}
		})
	}

	// Pin the parts each session must show, so the comparison above can't
	// pass on two lines that both leave everything out.
	full, _, _ := executeStatusline(t, statusInput(t, filepath.Join(proj, statusID+".jsonl"), "Opus 5.5"), "--no-color")
	for _, part := range []string{" · ctx ", "/msg ▲ · $", " (agents $"} {
		if !strings.Contains(full, part) {
			t.Errorf("seven climbing messages and an agent: %q lacks %q", full, part)
		}
	}
	short, _, _ := executeStatusline(t, statusInput(t, filepath.Join(proj, statusSoloID+".jsonl"), "Opus 5.5"), "--no-color")
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

// A transcript with no line ficha can read is still a transcript, so like
// show, statusline reports the $0.00 it found and warns that it skipped
// lines. The extension check ignores case, as show's does.
func TestStatuslineUnreadableTranscript(t *testing.T) {
	proj := setupStatusFixture(t)
	for _, name := range []string{"ffffffff-1111-2222-3333-444444444444.jsonl", "ffffffff-1111-2222-3333-555555555555.JSONL"} {
		t.Run(filepath.Ext(name), func(t *testing.T) {
			transcript := filepath.Join(proj, name)
			if err := os.WriteFile(transcript, []byte("not json\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := executeStatusline(t, statusInput(t, transcript, "Opus 5.5"))
			if err != nil {
				t.Fatal(err)
			}
			if want := "Opus 5.5 · $0.00\n"; stdout != want {
				t.Errorf("stdout = %q, want %q", stdout, want)
			}
			if !strings.Contains(stderr, "1 unparseable line(s) skipped") {
				t.Errorf("stderr = %q, want the skip warning", stderr)
			}
		})
	}
}

// Claude Code runs the command as a session starts, before the transcript
// exists. That's a session with no messages yet, as an empty file is, while
// show still fails on the same path.
func TestStatuslineTranscriptNotWrittenYet(t *testing.T) {
	proj := setupStatusFixture(t)
	transcript := filepath.Join(proj, "00000000-0000-4000-8000-000000000001.jsonl")
	stdout, stderr, err := executeStatusline(t, statusInput(t, transcript, "Opus 5.5"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "Opus 5.5 · $0.00\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q", stderr)
	}

	_, _, err = executeCLISplit(t, "show", transcript)
	if err == nil || !strings.Contains(err.Error(), "can't read transcript") {
		t.Errorf("show error = %v, want can't read transcript", err)
	}
}

// Claude Code shows whatever reaches stdout, so a failure must leave it
// empty, with the reason on stderr and the exit status saying which kind.
func TestStatuslineFailures(t *testing.T) {
	proj := setupStatusFixture(t)
	transcript := filepath.Join(proj, statusID+".jsonl")
	good := statusInput(t, transcript, "Opus 5.5")
	notes := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(notes, []byte("# Notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
		{"a number for transcript_path", `{"transcript_path":5}`, nil, 1, "unexpected number at transcript_path"},
		{"an array", `[1,2]`, nil, 1, "unexpected array at the top level"},
		{"trailing data", good + " garbage", nil, 1, "reading Claude Code's status line JSON on stdin"},
		{"an agent's transcript", statusInput(t, filepath.Join(proj, statusID, "subagents", "agent-a1.jsonl"), "Opus 5.5"), nil, 1, "is an agent's transcript, part of session dddddddd"},
		{"a transcript in a missing directory", statusInput(t, filepath.Join(proj, "gone", "nope.jsonl"), "Opus 5.5"), nil, 1, "can't read transcript"},
		{"a file that isn't .jsonl", statusInput(t, notes, "Opus 5.5"), nil, 1, "notes.md isn't a .jsonl transcript"},
		{"a missing file that isn't .jsonl", statusInput(t, filepath.Join(proj, "nope.md"), "Opus 5.5"), nil, 1, "nope.md isn't a .jsonl transcript"},
		{"-f json", good, []string{"-f", "json"}, 2, "--format json is not supported by statusline"},
		{"-f csv", good, []string{"-f", "csv"}, 2, "--format csv is not supported by statusline"},
		{"an argument", good, []string{transcript}, 2, "ficha statusline takes no arguments"},
		{"a project flag", good, []string{"-p", proj}, 2, "ficha statusline doesn't take -p"},
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
			if strings.Contains(err.Error(), "Run:") {
				t.Errorf("suggests a command: %v", err)
			}
		})
	}

	// A relative path is an agent's by where it resolves.
	t.Run("an agent's transcript, relative", func(t *testing.T) {
		t.Chdir(filepath.Join(proj, statusID, "subagents"))
		stdout, _, err := executeStatusline(t, `{"transcript_path":"agent-a1.jsonl"}`)
		if err == nil || !strings.Contains(err.Error(), "part of session dddddddd") || strings.Contains(err.Error(), "Run:") {
			t.Fatalf("error = %v", err)
		}
		if code := exitCode(err); code != 1 || stdout != "" {
			t.Errorf("exit status %d, stdout %q", code, stdout)
		}
	})
}
