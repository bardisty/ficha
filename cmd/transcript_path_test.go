package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func sessionIDOf(t *testing.T, stdout string) string {
	t.Helper()
	var s struct {
		SessionID string `json:"session_id"`
		Project   string `json:"project"`
	}
	if err := json.Unmarshal([]byte(stdout), &s); err != nil {
		t.Fatalf("not json: %v\n%s", err, stdout)
	}
	return s.SessionID
}

func TestTranscriptPathArgument(t *testing.T) {
	root := setupE2EFixture(t)
	proj := filepath.Join(root, "projects", e2eProjDir)
	beta := filepath.Join(proj, e2eBetaID+".jsonl")

	want, _, err := executeCLISplit(t, "show", projFlag, e2eBetaID, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"show", beta, "-f", "json"},
		{beta, "-f", "json"},
		// The path names its own project; -p pointing elsewhere doesn't apply.
		{"show", "-p", t.TempDir(), beta, "-f", "json"},
	} {
		got, stderr, err := executeCLISplit(t, args...)
		if err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		if got != want {
			t.Errorf("%v differs from the ID lookup:\n got: %s\nwant: %s", args, got, want)
		}
		if stderr != "" {
			t.Errorf("%v wrote to stderr: %q", args, stderr)
		}
	}

	// Relative, and a bare file name that exists here.
	t.Chdir(proj)
	for _, arg := range []string{e2eBetaID + ".jsonl", "." + string(filepath.Separator) + e2eBetaID + ".jsonl"} {
		out, _, err := executeCLISplit(t, "show", arg, "-f", "json")
		if err != nil {
			t.Fatalf("%s: %v", arg, err)
		}
		if id := sessionIDOf(t, out); id != e2eBetaID {
			t.Errorf("%s shows %s", arg, id)
		}
	}
}

func TestTranscriptPathErrors(t *testing.T) {
	root := setupE2EFixture(t)
	proj := filepath.Join(root, "projects", e2eProjDir)
	beta := filepath.Join(proj, e2eBetaID+".jsonl")
	agent := filepath.Join(proj, e2eBetaID, "subagents", "agent-g1.jsonl")
	wfAgent := filepath.Join(proj, e2eBetaID, "subagents", "workflows", "wf_e2e-run1", "agent-w1.jsonl")
	missing := filepath.Join(t.TempDir(), "gone.jsonl")

	agentErr := func(file string) string {
		return file + " is an agent's transcript, part of session bbbbbbbb. Run: ficha show " + shellQuote(beta)
	}
	tests := []struct {
		name     string
		args     []string
		wantErr  string
		wantCode int
	}{
		{"agent transcript", []string{"show", agent}, agentErr("agent-g1.jsonl"), exitUsage},
		{"workflow agent transcript", []string{"show", wfAgent}, agentErr("agent-w1.jsonl"), exitUsage},
		{"missing path", []string{"show", missing}, "can't read transcript " + missing + ": ", 1},
		// Not "unknown command": it's a path.
		{"missing path, bare ficha", []string{missing}, "can't read transcript " + missing + ": ", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := executeCLISplit(t, tt.args...)
			if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
				t.Fatalf("\n got: %v\nwant: %s", err, tt.wantErr)
			}
			if got := exitCode(err); got != tt.wantCode {
				t.Errorf("exit %d, want %d", got, tt.wantCode)
			}
		})
	}
}

// A full session ID can't be ambiguous, so one found in another project is
// shown, with the project named on stderr and stdout left as clean json.
func TestFullSessionIDFromAnotherProject(t *testing.T) {
	setupE2EFixture(t)
	note := "Note: session cccccccc is in " + e2eOtherDir + ".\n"
	for _, args := range [][]string{
		{"show", projFlag, e2eGammaID, "-f", "json"},
		{"show", projFlag, strings.ToUpper(e2eGammaID) + ".jsonl", "-f", "json"},
		// This directory has no project at all.
		{"show", "-p", t.TempDir(), e2eGammaID, "-f", "json"},
	} {
		stdout, stderr, err := executeCLISplit(t, args...)
		if err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		if id := sessionIDOf(t, stdout); id != e2eGammaID {
			t.Errorf("%v shows %s", args, id)
		}
		if stderr != note {
			t.Errorf("%v stderr %q, want %q", args, stderr, note)
		}
	}
}

// A folder in the projects dir is a project, which --project-dir takes by
// name, and a folder named for a session holds its agents, beside the
// transcript that's meant. Both are usage errors, as any directory is. The
// suggested command is the line as typed, so the runs go through the
// command line the way main passes it.
func TestClaudeFolderArguments(t *testing.T) {
	root := setupE2EFixture(t)
	folder := filepath.Join(root, "projects", e2eProjDir)
	agents := filepath.Join(folder, e2eBetaID)
	agentsCmd := shellQuote(agents + ".jsonl")

	tests := []struct {
		args []string
		want string
	}{
		{[]string{"show", folder}, strconv.Quote(folder) + " is a Claude project folder, not a session. To show its latest session, run: ficha show --project-dir=" + e2eProjDir},
		{[]string{folder}, strconv.Quote(folder) + " is a Claude project folder, not a session. To show its latest session, run: ficha --project-dir=" + e2eProjDir},
		{[]string{"watch", folder}, strconv.Quote(folder) + " is a Claude project folder, not a session. To watch its sessions, run: ficha watch --project-dir=" + e2eProjDir},
		{[]string{"breakdown", folder}, strconv.Quote(folder) + " is a Claude project folder, not a session. To break down its latest session, run: ficha breakdown --project-dir=" + e2eProjDir},
		{[]string{"-v", folder}, strconv.Quote(folder) + " is a Claude project folder, not a session. To show its latest session, run: ficha -v --project-dir=" + e2eProjDir},
		{[]string{"show", agents}, strconv.Quote(agents) + " holds the agents of session bbbbbbbb, not a session. Run: ficha show " + agentsCmd},
		{[]string{agents}, strconv.Quote(agents) + " holds the agents of session bbbbbbbb, not a session. Run: ficha " + agentsCmd},
	}
	for _, tt := range tests {
		_, err := executeLikeMain(t, tt.args...)
		if exitCode(err) != 2 || err.Error() != tt.want {
			t.Errorf("%v: exit %d\n got: %v\nwant: %s", tt.args, exitCode(err), err, tt.want)
		}
	}
	if _, _, err := executeCLISplit(t, "show", "--project-dir="+e2eProjDir, "-f", "json"); err != nil {
		t.Errorf("the suggested command fails: %v", err)
	}

	// A directory anywhere else keeps the -p advice.
	elsewhere := t.TempDir()
	_, _, err := executeCLISplit(t, "show", elsewhere)
	if err == nil || !strings.Contains(err.Error(), "is a directory, not a session") {
		t.Errorf("elsewhere: %v", err)
	}
}

// After --, an argument can't be moved in front of it, and a flag can't
// stay behind it. Each suggestion here is run as printed, the way a user
// would paste it.
func TestClaudeFolderAfterDashes(t *testing.T) {
	root := setupE2EFixture(t)
	t.Chdir(filepath.Join(root, "projects"))
	folder := filepath.Join(root, "projects", e2eProjDir)
	// From inside the projects dir, an agents folder's relative path starts
	// with -, as a flag does.
	agents := filepath.Join(e2eProjDir, e2eBetaID)
	agentsCmd := shellQuote(agents + ".jsonl")

	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--", folder}, "ficha --project-dir=" + e2eProjDir},
		{[]string{"-v", "--", folder}, "ficha -v --project-dir=" + e2eProjDir},
		{[]string{"show", "--", folder}, "ficha show --project-dir=" + e2eProjDir},
		{[]string{"--", agents}, "ficha -- " + agentsCmd},
		{[]string{"show", "--", agents}, "ficha show -- " + agentsCmd},
	}
	for _, tt := range tests {
		_, err := executeLikeMain(t, tt.args...)
		if exitCode(err) != 2 {
			t.Errorf("%v: exit %d: %v", tt.args, exitCode(err), err)
			continue
		}
		msg := err.Error()
		suggested := msg[strings.LastIndex(msg, "un: ")+len("un: "):]
		if suggested != tt.want {
			t.Errorf("%v suggests %s, want %s", tt.args, suggested, tt.want)
		}
		words := shellWords(suggested)
		if _, err := executeLikeMain(t, words[1:]...); err != nil {
			t.Errorf("%v: the suggested %s fails: %v", tt.args, suggested, err)
		}
	}
}

// shellWords splits a line quoteLine built back into its words.
func shellWords(line string) []string {
	var words []string
	var word strings.Builder
	inWord, quoted, escaped := false, false, false
	for _, r := range line {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
		case r == '\\' && !quoted:
			escaped, inWord = true, true
		case r == '\'':
			quoted, inWord = !quoted, true
		case r == ' ' && !quoted:
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
}

// A folder in the projects dir given as -p, or run from, reads as that
// project, the way --project-dir does. A config dir reached through a
// symlink, as ~/.claude can be, still counts.
func TestProjectFolderAsProjectPath(t *testing.T) {
	root := setupE2EFixture(t)
	folder := filepath.Join(root, "projects", e2eProjDir)
	want, _, err := executeCLISplit(t, "show", "--project-dir="+e2eProjDir, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := executeCLISplit(t, "show", "-p", folder, "-f", "json")
	if err != nil || got != want {
		t.Errorf("show -p: %v\n%s", err, got)
	}
	for _, command := range []string{"list", "summary"} {
		if _, _, err := executeCLISplit(t, command, "-p", folder, "-f", "json"); err != nil {
			t.Errorf("%s -p: %v", command, err)
		}
	}

	t.Chdir(folder)
	got, _, err = executeCLISplit(t, "show", "-f", "json")
	if err != nil || got != want {
		t.Errorf("from inside: %v\n%s", err, got)
	}
	cfg := config{}
	if _, _, _, ok := waitingProject(&cfg); ok {
		t.Error("watch would wait in a project folder")
	}

	if runtime.GOOS != "windows" {
		link := filepath.Join(t.TempDir(), "config")
		if err := os.Symlink(root, link); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CLAUDE_CONFIG_DIR", link)
		got, _, err = executeCLISplit(t, "show", "-p", folder, "-f", "json")
		if err != nil || sessionIDOf(t, got) != sessionIDOf(t, want) {
			t.Errorf("through a symlinked config dir: %v\n%s", err, got)
		}
	}
}

// A file that can't be a session says so, and exits 1: one that isn't a
// .jsonl, and a .jsonl with no Claude Code record in it, such as
// ~/.claude/history.jsonl. A session that has just started, empty or with
// only a prompt, still reads as one.
func TestFilesThatAreNotSessions(t *testing.T) {
	root := setupE2EFixture(t)
	dir := t.TempDir()
	t.Chdir(dir)
	write := func(name, content string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return name
	}
	example := filepath.Join(root, "projects", "<project>", "<session-id>.jsonl")
	readme := write("README.md", "hi\n")
	for _, args := range [][]string{{"show", readme}, {readme}, {"watch", readme}, {"breakdown", readme}} {
		_, _, err := executeCLISplit(t, args...)
		want := "README.md is a file, not a session ID. ficha reads .jsonl transcripts, such as " + example + "."
		if exitCode(err) != 1 || err.Error() != want {
			t.Errorf("%v: exit %d\n got: %v\nwant: %s", args, exitCode(err), err, want)
		}
	}

	history := write("history.jsonl", `{"display":"fix the login bug","pastedContents":{},"timestamp":1761880000000,"project":"/x"}`+"\n")
	twoLines := write("two.jsonl", `{"display":"a"}`+"\n"+`{"display":"b"}`+"\n")
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"show", history, "-f", "json"}, "history.jsonl isn't a Claude Code transcript: its one line isn't a Claude Code record."},
		{[]string{"watch", history}, "history.jsonl isn't a Claude Code transcript: its one line isn't a Claude Code record."},
		{[]string{"breakdown", twoLines}, "two.jsonl isn't a Claude Code transcript: none of its 2 lines is a Claude Code record."},
	} {
		stdout, _, err := executeCLISplit(t, tt.args...)
		if exitCode(err) != 1 || err.Error() != tt.want || stdout != "" {
			t.Errorf("%v: exit %d, stdout %q\n got: %v\nwant: %s", tt.args, exitCode(err), stdout, err, tt.want)
		}
	}

	for name, content := range map[string]string{
		"aaaa0000-0000-4000-8000-000000000000.jsonl": "",
		"bbbb0000-0000-4000-8000-000000000000.jsonl": `{"type":"user","timestamp":"2026-02-05T10:00:00Z","message":{"role":"user","content":"hi"}}` + "\n" +
			`{"type":"file-history-snapshot","messageId":"m","snapshot":{}}` + "\n",
	} {
		stdout, _, err := executeCLISplit(t, "show", write(name, content))
		if want := "No assistant messages in session " + name[:8] + " yet."; err != nil || strings.TrimSpace(stdout) != want {
			t.Errorf("%s: %v, %q", name, err, stdout)
		}
	}

	// Lines that don't parse are the parse's to report, not a reason to
	// reject the file.
	if _, _, err := executeCLISplit(t, "show", write("broken.jsonl", "{broken\n")); err != nil && strings.Contains(err.Error(), "isn't a Claude Code transcript") {
		t.Errorf("broken: %v", err)
	}

	// A transcript named like an ID is read as a transcript, and a file
	// named like an ID with no .jsonl leaves the word to the ID lookup.
	beta, err := os.ReadFile(filepath.Join(root, "projects", e2eProjDir, e2eBetaID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := executeCLISplit(t, "show", write("abc123.jsonl", string(beta)), "-f", "json")
	if err != nil || sessionIDOf(t, out) != "abc123" {
		t.Errorf("abc123.jsonl: %v", err)
	}
	write("bbbbbbbb", "hi\n")
	out, _, err = executeCLISplit(t, "show", projFlag, "bbbbbbbb", "-f", "json")
	if err != nil || sessionIDOf(t, out) != e2eBetaID {
		t.Errorf("bbbbbbbb: %v", err)
	}
}
