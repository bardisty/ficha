package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/paths"
)

// setupErrorFixture builds a home with one project, ~/work/webapp, and
// changes into ~/work/webapp/src, which has none. It returns home.
func setupErrorFixture(t *testing.T, project ...string) string {
	t.Helper()
	home := realDir(t, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	if len(project) == 0 {
		project = []string{"work", "webapp"}
	}
	webapp := filepath.Join(append([]string{home}, project...)...)
	dir := filepath.Join(config, "projects", paths.PathToProjectDir(webapp))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := strings.Replace(e2eMsg("2026-02-01T10:00:00Z", "m1", "claude-opus-4-8", 100, 200, 0, 0, 0),
		`{"type":"assistant",`, `{"type":"assistant","cwd":`+jsonString(webapp)+`,`, 1)
	if err := os.WriteFile(filepath.Join(dir, "aaaaaaaa-0000-0000-0000-000000000001.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(webapp, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(src)
	return home
}

// On a terminal an error wraps between words, every line after the first
// hangs under "Error: ", paths under home read as ~, and the command to run
// stays on one line.
func TestErrorWrapsOnATerminal(t *testing.T) {
	// shellQuote quotes a path with backslashes, so on Windows the command
	// keeps its absolute path, as TestErrorKeepsQuotedPathsAbsolute pins.
	if runtime.GOOS == "windows" {
		t.Skip("the suggested command quotes its path on Windows")
	}
	setupErrorFixture(t)
	_, _, err := executeCLISplit(t, "list")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, tt := range []struct {
		width int
		want  string
	}{
		{80, `Error: Claude Code has no sessions for ~/work/webapp/src. Sessions are recorded
       per directory Claude Code was started in.
       Found sessions for parent directory ~/work/webapp. Run:
       ficha list -p ~/work/webapp
       Run 'ficha global' to see every project.
`},
		{60, `Error: Claude Code has no sessions for ~/work/webapp/src.
       Sessions are recorded per directory Claude Code was
       started in.
       Found sessions for parent directory ~/work/webapp.
       Run: ficha list -p ~/work/webapp
       Run 'ficha global' to see every project.
`},
		{40, `Error: Claude Code has no sessions for
       ~/work/webapp/src. Sessions are
       recorded per directory Claude
       Code was started in.
       Found sessions for parent
       directory ~/work/webapp. Run:
       ficha list -p ~/work/webapp
       Run 'ficha global' to see every
       project.
`},
	} {
		withStderrWidth(t, tt.width)
		if got := formatError(nil, err); got != tt.want {
			t.Errorf("at %d columns:\n%s\nwant:\n%s", tt.width, got, tt.want)
		}
	}
}

// Off a terminal the message is err.Error() as it is: absolute paths, one
// line per line of the message, for logs and scripts to match on.
func TestErrorUnchangedOffATerminal(t *testing.T) {
	setupErrorFixture(t)
	_, _, err := executeCLISplit(t, "list")
	if err == nil {
		t.Fatal("want an error")
	}
	if got, want := formatError(nil, err), "Error: "+err.Error()+"\n"; got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if strings.Contains(err.Error(), "~") || strings.Contains(err.Error(), "\x00") {
		t.Errorf("message holds a display form: %q", err.Error())
	}
}

// A path the shell needs quoted keeps its absolute form in a command, since
// a shell doesn't expand ~ inside quotes. Outside the command it's ~.
func TestErrorKeepsQuotedPathsAbsolute(t *testing.T) {
	home := setupErrorFixture(t, "my work", "webapp")
	_, _, err := executeCLISplit(t, "list")
	if err == nil {
		t.Fatal("want an error")
	}
	withStderrWidth(t, 200)
	got := formatError(nil, err)
	quoted := "ficha list -p " + shellQuote(filepath.Join(home, "my work", "webapp"))
	parent := "parent directory " + filepath.Join("~", "my work", "webapp") + "."
	for _, want := range []string{quoted, parent} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

// -v's Debug lines wrap like notes, each absolute path kept whole.
func TestDebugWrapsOnATerminal(t *testing.T) {
	home := setupErrorFixture(t)
	withStderrWidth(t, 40)
	_, stderr, _ := executeCLISplit(t, "list", "-v")
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	var debug []string
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "Debug: "):
			debug = append(debug, line)
		case strings.HasPrefix(line, strings.Repeat(" ", len("Debug: "))), filepath.IsAbs(line):
			if len(debug) == 0 {
				t.Fatalf("continuation before any Debug line:\n%s", stderr)
			}
			debug[len(debug)-1] += " " + strings.TrimSpace(line)
		}
		if len(line) > 40 && !filepath.IsAbs(strings.TrimSpace(strings.TrimPrefix(line, "Debug: "))) && len(strings.Fields(line)) > 2 {
			t.Errorf("line is %d columns wide: %q", len(line), line)
		}
	}
	src := filepath.Join(home, "work", "webapp", "src")
	if want := "Debug: project path " + src + " (current directory)"; !strings.Contains(strings.Join(debug, "\n"), want) {
		t.Errorf("missing %q:\n%s", want, stderr)
	}
}

// The shell leaves ~ alone in quotes and after "=", so -p expands it. A path
// that doesn't exist once expanded is rejected with home's path in it.
func TestProjectFlagExpandsHome(t *testing.T) {
	home := setupErrorFixture(t)
	for _, args := range [][]string{
		{"list", "-p", "~/work/webapp"},
		{"list", "--project=~/work/webapp"},
		{"show", "-p", "~/work/webapp", "-f", "json"},
		{"summary", "--project=~/work/webapp", "-f", "json"},
	} {
		if _, _, err := executeCLISplit(t, args...); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}

	_, _, err := executeCLISplit(t, "show", "-p", "~/work/gone")
	if want := "no such directory: " + filepath.Join(home, "work", "gone"); err == nil || err.Error() != want {
		t.Fatalf("\n got: %v\nwant: %s", err, want)
	}
	withStderrWidth(t, 200)
	if got, want := formatError(nil, err), "Error: no such directory: "+filepath.Join("~", "work", "gone")+"\n"; got != want {
		t.Errorf("on a terminal:\n got: %q\nwant: %q", got, want)
	}
}

// The errors that name a directory someone passed read ~/… on a terminal,
// in the message and in the command to run, and keep the whole path piped.
func TestDirectoryErrorsShowHomeAsTilde(t *testing.T) {
	// shellQuote quotes a path with backslashes, so on Windows the commands
	// keep their absolute paths.
	if runtime.GOOS == "windows" {
		t.Skip("the suggested commands quote their paths on Windows")
	}
	home := setupErrorFixture(t)
	config := filepath.Join(home, ".claude")
	if err := os.CopyFS(config, os.DirFS(os.Getenv("CLAUDE_CONFIG_DIR"))); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	projects := filepath.Join(config, "projects")
	work := filepath.Join(home, "work")
	webapp := filepath.Join(work, "webapp")
	folder := filepath.Join(projects, paths.PathToProjectDir(webapp))
	// ~/work gets a project with no sessions, so summary -p ~/work finds
	// only webapp's, under it.
	if err := os.Mkdir(filepath.Join(projects, paths.PathToProjectDir(work)), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		args          []string
		piped, onTerm string
	}{
		{
			[]string{"list", "--project-dir", "nope"},
			"no project directory nope in " + projects + ". --project-dir=<Tab> completes the names, or pass the project's own directory with -p.",
			"no project directory nope in ~/.claude/projects. --project-dir=<Tab> completes the names, or pass the project's own directory with -p.",
		},
		{
			[]string{"list", "--project-dir", filepath.Join(work, "gone")},
			"no such directory: " + filepath.Join(work, "gone"),
			"no such directory: ~/work/gone",
		},
		{
			[]string{"show", webapp},
			`"` + webapp + `" is a directory, not a session. To show that directory's latest session, run: ficha show -p ` + webapp,
			`"~/work/webapp" is a directory, not a session. To show that directory's latest session, run: ficha show -p ~/work/webapp`,
		},
		{
			[]string{"list", webapp, "--sort-by", "cost"},
			"ficha list takes no arguments. For that directory's sessions, run: ficha list -p " + webapp + " --sort-by cost",
			"ficha list takes no arguments. For that directory's sessions, run: ficha list -p ~/work/webapp --sort-by cost",
		},
		{
			[]string{webapp},
			`unknown command "` + webapp + `" for "ficha". To analyze that directory, run: ficha -p ` + webapp,
			`unknown command "~/work/webapp" for "ficha". To analyze that directory, run: ficha -p ~/work/webapp`,
		},
		{
			[]string{"show", folder},
			`"` + folder + `" is a Claude project folder, not a session. To show its latest session, run: ficha show --project-dir=` + filepath.Base(folder),
			`"~/.claude/projects/` + filepath.Base(folder) + `" is a Claude project folder, not a session. To show its latest session, run: ficha show --project-dir=` + filepath.Base(folder),
		},
		{
			[]string{"summary", "-p", work},
			"Claude Code has no sessions for " + work + " itself, but 1 project under it has: webapp. Run: ficha summary -p " + work + " --nested",
			"Claude Code has no sessions for ~/work itself, but 1 project under it has: webapp. Run: ficha summary -p ~/work --nested",
		},
		{
			// The command repeats --project= as -p and a word of its own,
			// where the shell expands ~.
			[]string{"summary", "--project=" + work},
			"Claude Code has no sessions for " + work + " itself, but 1 project under it has: webapp. Run: ficha summary -p " + work + " --nested",
			"Claude Code has no sessions for ~/work itself, but 1 project under it has: webapp. Run: ficha summary -p ~/work --nested",
		},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, err := executeLikeMain(t, tt.args...)
			if err == nil || err.Error() != tt.piped {
				t.Fatalf("piped:\n got: %v\nwant: %s", err, tt.piped)
			}
			// Wide enough for the whole message on one line, however long
			// the temp dir in it is.
			withStderrWidth(t, len(errorLabel)+len(tt.piped))
			if got, want := formatError(nil, err), errorLabel+tt.onTerm+"\n"; got != want {
				t.Errorf("on a terminal:\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

// A path the shell has to quote keeps its quotes, and so its full path, in
// the command: a ~ inside quotes isn't expanded.
func TestQuotedDirectoryCommandKeepsFullPath(t *testing.T) {
	home := setupErrorFixture(t)
	spaced := filepath.Join(home, "my work")
	if err := os.Mkdir(spaced, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := executeLikeMain(t, "list", spaced)
	want := "ficha list takes no arguments. For that directory's sessions, run: ficha list -p " + shellQuote(spaced)
	if err == nil || err.Error() != want {
		t.Fatalf("\n got: %v\nwant: %s", err, want)
	}
	withStderrWidth(t, len(errorLabel)+len(want))
	if got := formatError(nil, err); got != errorLabel+want+"\n" {
		t.Errorf("on a terminal:\n got: %q\nwant: %q", got, errorLabel+want+"\n")
	}
}

func TestExpandHome(t *testing.T) {
	home := realDir(t, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	tests := map[string]string{
		"~":             home,
		"~/":            home,
		"~/work/webapp": filepath.Join(home, "work", "webapp"),
		"~user/work":    "~user/work",
		"work/~/webapp": "work/~/webapp",
		"~work":         "~work",
		"":              "",
		`~\work\webapp`: `~\work\webapp`,
	}
	if runtime.GOOS == "windows" {
		tests[`~\work\webapp`] = filepath.Join(home, "work", "webapp")
	}
	for in, want := range tests {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}
