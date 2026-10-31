package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
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

// An unknown shell names the ones there are, and a known one still writes
// its script.
func TestCompletionUnknownShell(t *testing.T) {
	_, err := executeLikeMain(t, "completion", "bogus")
	if want := `unknown shell "bogus": want bash, fish, powershell or zsh`; err == nil || err.Error() != want {
		t.Fatalf("error:\n got: %v\nwant: %s", err, want)
	}
	if got := exitCode(err); got != exitUsage {
		t.Errorf("exit %d, want %d", got, exitUsage)
	}
	if out, err := executeLikeMain(t, "completion", "fish"); err != nil || !strings.Contains(out, "complete -c ficha") {
		t.Errorf("completion fish: %v\n%s", err, out)
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
			"unknown flag: --since. --since is a list, summary and global flag: ficha list --since 7d"},
		// cobra reads the command after an unknown flag as its value.
		{[]string{"--details", "summary"},
			"unknown flag: --details. --details is a summary and global flag: ficha summary --details"},
		{[]string{"--messages", "show", "-f", "csv"},
			"unknown flag: --messages. --messages is a show flag: ficha show --messages -f csv"},
		// A command that doesn't own the flag gives way to one that does.
		{[]string{"--messages", "list"},
			"unknown flag: --messages. --messages is a show flag: ficha show --messages"},
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
	_, err = executeCLI(t, "list", "--messages")
	if want := "ficha list doesn't take --messages. --messages is a show flag."; err == nil || err.Error() != want {
		t.Errorf("got %v, want %s", err, want)
	}
}

// A flag on a command that doesn't take it names the commands that do, and
// suggests the line with one of them in its place.
func TestSubcommandFlagHintNamesTheOwners(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr string
	}{
		{[]string{"show", "--since", "today"},
			"ficha show doesn't take --since. --since is a list, summary and global flag: ficha list --since today"},
		{[]string{"summary", "--top", "5"},
			"ficha summary doesn't take --top. --top is a global flag: ficha global --top 5"},
		{[]string{"summary", "-n", "5", "-f", "json"},
			"ficha summary doesn't take -n. -n is a global flag: ficha global -n 5 -f json"},
		{[]string{"--no-color", "watch", "--messages"},
			"ficha watch doesn't take --messages. --messages is a show flag: ficha show --no-color --messages"},
		{[]string{"global", "-p", "my dir"},
			"ficha global covers every project and doesn't take -p. summary covers one project: ficha summary -p 'my dir'"},
		{[]string{"global", "--project-dir=x", "--since", "7d"},
			"ficha global covers every project and doesn't take --project-dir. summary covers one project: ficha summary --project-dir=x --since 7d"},
		{[]string{"statusline", "-p", "."},
			"ficha statusline doesn't take -p: it reads the session from the JSON Claude Code writes on stdin."},
		// No line that would fail in turn: global doesn't take -p, and list
		// takes no session ID.
		{[]string{"summary", "-p", ".", "--top", "5"},
			"ficha summary doesn't take --top. --top is a global flag."},
		{[]string{"show", "abc", "--since", "today"},
			"ficha show doesn't take --since. --since is a list, summary and global flag."},
		// list and summary sort sessions by time or cost, and global sorts
		// projects by four keys of its own, so the line goes to the command
		// that takes the value.
		{[]string{"show", "--sort-by", "name"},
			"ficha show doesn't take --sort-by. --sort-by is a list, summary and global flag: ficha global --sort-by name"},
		{[]string{"show", "--sort-by=cost"},
			"ficha show doesn't take --sort-by. --sort-by is a list, summary and global flag: ficha list --sort-by=cost"},
		{[]string{"watch", "--sort-by", "activity"},
			"ficha watch doesn't take --sort-by. --sort-by is a list, summary and global flag: ficha global --sort-by activity"},
		{[]string{"show", "--sort-by", "time"},
			"ficha show doesn't take --sort-by. --sort-by is a list, summary and global flag: ficha list --sort-by time"},
		// list doesn't take --details, so the line goes to summary.
		{[]string{"show", "--since", "7d", "--details"},
			"ficha show doesn't take --since. --since is a list, summary and global flag: ficha summary --since 7d --details"},
		{[]string{"summary", "--top"},
			"ficha summary doesn't take --top. --top is a global flag."},
		{[]string{"summary", "--details", "--messages"},
			"ficha summary doesn't take --messages. --messages is a show flag."},
		// The live views print only a table.
		{[]string{"summary", "-f", "json", "--no-follow"},
			"ficha summary doesn't take --no-follow. --no-follow is a breakdown and watch flag."},
		{[]string{"list", "--format=csv", "--no-follow"},
			"ficha list doesn't take --no-follow. --no-follow is a breakdown and watch flag."},
		{[]string{"list", "-vfjson", "--no-follow"},
			"ficha list doesn't take --no-follow. --no-follow is a breakdown and watch flag."},
		{[]string{"list", "-f", "table", "--no-follow"},
			"ficha list doesn't take --no-follow. --no-follow is a breakdown and watch flag: ficha breakdown -f table --no-follow"},
		// pflag panics on a shorthand that isn't one byte.
		{[]string{"list", "-é"}, "unknown shorthand flag: 'Ã' in -é"},
		{[]string{"summary", "--top", "5", "-é"}, "ficha summary doesn't take --top. --top is a global flag."},
		{[]string{"-é"}, "unknown shorthand flag: 'Ã' in -é"},
		{[]string{"list", "--bogus"}, "unknown flag: --bogus"},
		// Hidden everywhere, so no command offers it.
		{[]string{"list", "--live"}, "unknown flag: --live"},
		{[]string{"completion", "bash", "--top", "3"}, "unknown flag: --top"},
		{[]string{"completion", "--top", "3"}, "unknown flag: --top"},
		{[]string{"version", "--top", "3"}, "unknown flag: --top"},
		{[]string{"help", "--top", "5"}, "unknown flag: --top"},
		{[]string{"help", "summary", "--top", "5"}, "unknown flag: --top"},
		{[]string{"output", "--top"}, "unknown flag: --top"},
	}
	setupE2EFixture(t)
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, err := executeLikeMain(t, tt.args...)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("error:\n got: %v\nwant: %s", err, tt.wantErr)
			}
			if got := exitCode(err); got != exitUsage {
				t.Errorf("exit %d, want %d", got, exitUsage)
			}
			// The suggested line parses. It can still fail for want of a
			// project here, but not on its arguments.
			_, line, ok := strings.Cut(tt.wantErr, ": ficha ")
			if !ok {
				return
			}
			if _, err := executeLikeMain(t, strings.Fields(strings.ReplaceAll(line, "'my dir'", "my-dir"))...); exitCode(err) == exitUsage {
				t.Errorf("suggested line ficha %s: %v", line, err)
			}
		})
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

// ficha help output names the labels watch and breakdown print that it
// explains, so a reader who sees one can find it, and the ? lists point here.
func TestHelpOutputNamesTheLiveViewsTerms(t *testing.T) {
	out, _, err := executeCLISplit(t, "help", "output")
	if err != nil {
		t.Fatal(err)
	}
	for _, term := range []string{"IN, OUT, C_WR and C_RD", "/h (10m)", "-/h", "~compaction", "COST TREND", "$ per minute, last 68m"} {
		if !strings.Contains(out, term) {
			t.Errorf("help output lacks %q", term)
		}
	}
}

// The live views and statusline print only a table, so their help leaves -f
// out. They still parse it: json and csv name the show command that prints
// them, with the session and project from the line, and table still works.
func TestLiveViewsRejectMachineFormats(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"watch", "-f", "json"}, "ficha watch is a live view and has no json output. For the session as json, run: ficha show -f json"},
		{[]string{"watch", "-f", "csv"}, "ficha watch is a live view and has no csv output. For the session as csv, run: ficha show -f csv"},
		{[]string{"breakdown", "-f", "csv"}, "ficha breakdown is a live view and has no csv output. For one row per message, run: ficha show -f csv --messages"},
		{[]string{"breakdown", "--format=JSON"}, "ficha breakdown is a live view and has no json output. For the session's messages as json, run: ficha show -f json --messages"},
		{[]string{"watch", "-p", "my proj", "abc123", "-vf", "csv", "--no-follow"}, "ficha watch is a live view and has no csv output. For the session as csv, run: ficha show -p 'my proj' abc123 -f csv"},
		{[]string{"breakdown", "--project-dir=-home-x", "-f", "json"}, "ficha breakdown is a live view and has no json output. For the session's messages as json, run: ficha show --project-dir=-home-x -f json --messages"},
		// -f ahead of the command parses on the command cobra finds.
		{[]string{"-f", "json", "watch"}, "ficha watch is a live view and has no json output. For the session as json, run: ficha show -f json"},
		{[]string{"show", "--live", "-f", "json"}, "ficha show --live is a live view and has no json output. For the session as json, run: ficha show -f json"},
		{[]string{"--live", "-f", "json"}, "ficha --live is a live view and has no json output. For the session as json, run: ficha show -f json"},
		{[]string{"show", "--live", "--messages", "-f", "csv"}, "ficha show --live is a live view and has no csv output. For one row per message, run: ficha show -f csv --messages"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, err := executeLikeMain(t, tt.args...)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error:\n got: %v\nwant: %s", err, tt.want)
			}
			if got := exitCode(err); got != exitUsage {
				t.Errorf("exit %d, want %d", got, exitUsage)
			}
		})
	}

	for _, name := range []string{"watch", "breakdown", "statusline"} {
		out, err := executeLikeMain(t, name, "--help")
		if err != nil {
			t.Fatalf("%s --help: %v", name, err)
		}
		if strings.Contains(out, "--format") {
			t.Errorf("%s --help lists --format:\n%s", name, out)
		}
		if out, _ := executeLikeMain(t, "__complete", name, "-"); strings.Contains(out, "--format") {
			t.Errorf("%s completes --format:\n%s", name, out)
		}
	}
	// The reports list it with their own flags.
	for _, args := range [][]string{{"--help"}, {"show", "--help"}, {"list", "--help"}, {"summary", "--help"}, {"global", "--help"}} {
		out, err := executeLikeMain(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		flags, global, _ := strings.Cut(out, "Global Flags:")
		if !strings.Contains(flags, "-f, --format string") || strings.Contains(global, "--format") {
			t.Errorf("%v should list -f under Flags, not Global Flags:\n%s", args, out)
		}
	}

	// -f table is what these commands print anyway.
	t.Run("watch -f table", func(t *testing.T) {
		setupE2EFixture(t)
		_, err := executeLikeMain(t, "watch", projFlag, "-f", "table")
		if err == nil || strings.Contains(err.Error(), "format") || exitCode(err) != 1 {
			t.Errorf("watch -f table should get as far as the terminal check, got: %v", err)
		}
	})
	t.Run("help ignores it", func(t *testing.T) {
		for _, args := range [][]string{{"help", "-f", "json"}, {"help", "output", "-f", "csv"}} {
			if _, err := executeLikeMain(t, args...); err != nil {
				t.Errorf("%v: %v", args, err)
			}
		}
	})
	t.Run("statusline -f table", func(t *testing.T) {
		proj := setupStatusFixture(t)
		stdin := statusInput(t, filepath.Join(proj, statusID+".jsonl"), "Opus 5.5")
		want, _, err := executeStatusline(t, stdin)
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := executeStatusline(t, stdin, "-f", "table")
		if err != nil || got != want {
			t.Errorf("statusline -f table: %v\n got: %q\nwant: %q", err, got, want)
		}
	})
}

// -f before the command is still the command's flag.
func TestFormatBeforeCommand(t *testing.T) {
	setupE2EFixture(t)
	want, err := executeLikeMain(t, "list", projFlag, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := executeLikeMain(t, "-f", "json", "list", projFlag)
	if err != nil || got != want {
		t.Errorf("ficha -f json list: %v\n got: %s\nwant: %s", err, got, want)
	}
}

// cobra pads the flag column and never wraps a description, so a line past
// 80 columns wraps mid-word in a terminal that wide. Every help page, help
// topic and completion shell's help stays within helpWidth, which leaves a
// column to spare.
func TestHelpFitsHelpWidth(t *testing.T) {
	root := newRootCmd()
	initCompletion(root)
	var pages [][]string
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden {
			return
		}
		path := strings.Fields(c.CommandPath())[1:]
		pages = append(pages, append(slices.Clone(path), "--help"), append([]string{"help"}, path...))
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	for _, args := range pages {
		out, err := executeLikeMain(t, args...)
		if err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > helpWidth {
				t.Errorf("ficha %s: %d columns:\n%s", strings.Join(args, " "), w, line)
			}
		}
	}
}

// The --since and --until flag lines leave the forms they take to the
// command's text, so it lists every one, and so does the parse error.
func TestWindowHelpListsEveryForm(t *testing.T) {
	forms := append(regexp.MustCompile(`[0-9][0-9T:-]*[dwhm]?|today|yesterday`).FindAllString(windowHelp, -1), "whole day")
	if len(forms) < 8 {
		t.Fatalf("forms read from windowHelp: %q", forms)
	}
	for _, name := range []string{"list", "summary", "global"} {
		out, err := executeLikeMain(t, name, "--help")
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range forms {
			if !strings.Contains(out, f) {
				t.Errorf("%s --help doesn't mention %q", name, f)
			}
		}
	}
	_, err := executeLikeMain(t, "summary", "--since", "soon")
	if want := `invalid --since "soon": want ` + windowHelp; err == nil || err.Error() != want {
		t.Errorf("error:\n got: %v\nwant: %s", err, want)
	}
}
