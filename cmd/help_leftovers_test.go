package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHelpTopics(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string // in stdout when wantErr is empty
		wantErr string
	}{
		{"no topic is the root help", []string{"help"}, "Available Commands:", ""},
		{"a command", []string{"help", "show"}, "Show cost breakdown", ""},
		{"a help topic", []string{"help", "output"}, "Reading the output", ""},
		{"another help topic", []string{"help", "environment"}, "Environment variables", ""},
		{"a nested command", []string{"help", "completion", "bash"}, "autocompletion script for the bash shell", ""},
		{"a typo of a topic", []string{"help", "outpt"}, "",
			`unknown help topic "outpt" for "ficha". Did you mean "output"?`},
		{"a typo of a command", []string{"help", "sumary"}, "",
			`unknown help topic "sumary" for "ficha". Did you mean "summary"?`},
		{"nothing close", []string{"help", "zzzzz"}, "",
			`unknown help topic "zzzzz" for "ficha". Run 'ficha --help' to see the commands and help topics.`},
		{"a typo under a command", []string{"help", "completion", "fsh"}, "",
			`unknown help topic "fsh" for "ficha completion". Did you mean "bash", "fish" or "zsh"?`},
		{"a word under a command with none", []string{"help", "show", "extra"}, "",
			`unknown help topic "extra" for "ficha show". It has no subtopics. Run 'ficha show --help'.`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, err := executeCLISplit(t, tt.args...)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("error:\n got: %v\nwant: %s", err, tt.wantErr)
				}
				if got := exitCode(err); got != exitUsage {
					t.Errorf("exit %d, want %d", got, exitUsage)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout, tt.want) {
				t.Errorf("stdout lacks %q:\n%s", tt.want, stdout)
			}
		})
	}
}

// The help topics must be found from the command tree, so a topic added
// later is covered without touching the check.
func TestEveryHelpTopicIsAccepted(t *testing.T) {
	root := newRootCmd()
	for _, c := range root.Commands() {
		if c.IsAdditionalHelpTopicCommand() {
			if err := helpTopicArgs(root, []string{c.Name()}); err != nil {
				t.Errorf("help %s: %v", c.Name(), err)
			}
		}
	}
}

// executeLikeMain builds the tree the way Execute does, with completion
// added up front and the command line in the context.
func executeLikeMain(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	var out strings.Builder
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetArgs(args)
	initCompletion(root)
	err = root.ExecuteContext(withCommandLine(context.Background(), args))
	return out.String(), err
}

func TestNoFlagCommandsRejectGlobalFlags(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr string
	}{
		{[]string{"version", "-f", "json"}, "ficha version doesn't take --format"},
		{[]string{"version", "-v"}, "ficha version doesn't take --verbose"},
		{[]string{"completion", "--ascii"}, "ficha completion doesn't take --ascii"},
		{[]string{"completion", "bash", "--no-color"}, "ficha completion bash doesn't take --no-color"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, err := executeLikeMain(t, tt.args...)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("error:\n got: %v\nwant: %s", err, tt.wantErr)
			}
			if got := exitCode(err); got != exitUsage {
				t.Errorf("exit %d, want %d", got, exitUsage)
			}
		})
	}
	// Their own flags still work.
	if _, err := executeLikeMain(t, "completion", "bash", "--no-descriptions"); err != nil {
		t.Errorf("completion bash --no-descriptions: %v", err)
	}
}

func TestNoFlagCommandsHideGlobalFlags(t *testing.T) {
	for _, args := range [][]string{{"version", "--help"}, {"completion", "--help"}, {"completion", "zsh", "--help"}} {
		out, err := executeLikeMain(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(out, "Global Flags") || strings.Contains(out, "--format") {
			t.Errorf("%v lists global flags:\n%s", args, out)
		}
	}
	out, err := executeLikeMain(t, "show", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Global Flags") {
		t.Errorf("show --help should still list the global flags:\n%s", out)
	}
}

// Completion offers none of the global flags version and completion reject,
// and still offers them everywhere else.
func TestNoFlagCommandsCompleteNoGlobalFlags(t *testing.T) {
	globals := []string{"--format", "-f", "--verbose", "-v", "--no-color", "--ascii"}
	for _, args := range [][]string{{"version"}, {"completion"}, {"completion", "bash"}} {
		out, err := executeLikeMain(t, append(append([]string{"__complete"}, args...), "-")...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		for _, line := range strings.Split(out, "\n") {
			name, _, _ := strings.Cut(line, "\t")
			for _, g := range globals {
				if name == g {
					t.Errorf("%v completes %s:\n%s", args, g, out)
				}
			}
		}
		if !strings.Contains(out, "--help") {
			t.Errorf("%v should still complete --help:\n%s", args, out)
		}
	}
	out, err := executeLikeMain(t, "__complete", "completion", "bash", "-")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "--no-descriptions") {
		t.Errorf("completion bash should still complete its own flag:\n%s", out)
	}
	out, err = executeLikeMain(t, "__complete", "show", "-")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range globals {
		if !strings.Contains(out, g+"\t") {
			t.Errorf("show should complete %s:\n%s", g, out)
		}
	}
}

func TestHelpDoesNotCallWatchAnAlias(t *testing.T) {
	for _, args := range [][]string{{"watch", "--help"}, {"show", "--help"}, {"--help"}} {
		out, err := executeCLI(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		for _, stale := range []string{"alias", "show --live", "-l, --live"} {
			if strings.Contains(out, stale) {
				t.Errorf("%v mentions %q:\n%s", args, stale, out)
			}
		}
	}
}

func TestRootFlagHintNamesTheCommand(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr string
	}{
		{[]string{"-f", "csv", "--messages"},
			"unknown flag: --messages. --messages is a show flag: ficha show -f csv --messages"},
		{[]string{"--since", "7d"},
			"unknown flag: --since. --since is a summary and global flag: ficha summary --since 7d"},
		// cobra reads the command after an unknown flag as its value.
		{[]string{"--details", "summary"},
			"unknown flag: --details. --details is a summary and global flag: ficha summary --details"},
		{[]string{"--messages", "show", "-f", "csv"},
			"unknown flag: --messages. --messages is a show flag: ficha show --messages -f csv"},
		{[]string{"--messages", "list"},
			"unknown flag: --messages. --messages is a show flag."},
		{[]string{"-n", "3"},
			"unknown shorthand flag: 'n' in -n. -n is a global flag: ficha global -n 3"},
		{[]string{"--messages", "-p", "my dir"},
			"unknown flag: --messages. --messages is a show flag: ficha show --messages -p 'my dir'"},
		{[]string{"--bogus"}, "unknown flag: --bogus"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, err := executeLikeMain(t, tt.args...)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("error:\n got: %v\nwant: %s", err, tt.wantErr)
			}
			if got := exitCode(err); got != exitUsage {
				t.Errorf("exit %d, want %d", got, exitUsage)
			}
		})
	}
	// Without the command line, as in-process callers run it, the hint
	// still names the command.
	_, err := executeCLI(t, "--messages")
	if want := "unknown flag: --messages. --messages is a show flag."; err == nil || err.Error() != want {
		t.Errorf("got %v, want %s", err, want)
	}
	// Only bare ficha gets the hint: `list` has no show to point at.
	_, err = executeCLI(t, "list", "--messages")
	if want := "unknown flag: --messages"; err == nil || err.Error() != want {
		t.Errorf("got %v, want %s", err, want)
	}
}

func TestCompletionKeepsTypedCase(t *testing.T) {
	setupE2EFixture(t)
	tests := []struct {
		typed string
		want  string
	}{
		{"aa", "aaaaaaaa"},
		{"AA", "AAAAAAAA"},
		{"Aa", "Aaaaaaaa"},
		{"BBBBBBBB-5", "BBBBBBBB-5555-6666-7777-888888888888"},
	}
	for _, tt := range tests {
		t.Run(tt.typed, func(t *testing.T) {
			got, _ := completeLines(t, "show", projFlag, tt.typed)
			if len(got) != 1 || strings.SplitN(got[0], "\t", 2)[0] != tt.want {
				t.Errorf("candidates %q, want one starting %q", got, tt.want)
			}
		})
	}
}

func TestUnreadableProjectDirNamedOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 doesn't block reading a directory on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads any directory")
	}
	root := setupE2EFixture(t)
	dir := filepath.Join(root, "projects", e2eProjDir)
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	_, _, err := executeCLISplit(t, "summary", projFlag)
	if err == nil {
		t.Fatal("expected an error")
	}
	if n := strings.Count(err.Error(), e2eProjDir); n != 1 {
		t.Errorf("the path appears %d times: %v", n, err)
	}
	if !strings.HasSuffix(err.Error(), "permission denied. Check its permissions.") {
		t.Errorf("no next step: %v", err)
	}
}
