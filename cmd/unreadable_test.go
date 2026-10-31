package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/paths"
)

// unreadableFixture is setupErrorFixture's ~/work/webapp, its config moved
// under home, with its one transcript's mode set to 000, as a file left
// owned by root after Claude Code ran under sudo reads to anyone else. It
// returns home, the project's folder and the transcript.
func unreadableFixture(t *testing.T) (home, folder, transcript string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows ignores a file's mode bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
	home = setupErrorFixture(t)
	config := filepath.Join(home, ".claude")
	if err := os.Rename(os.Getenv("CLAUDE_CONFIG_DIR"), config); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	folder = filepath.Join(config, "projects", paths.PathToProjectDir(filepath.Join(home, "work", "webapp")))
	transcript = filepath.Join(folder, "aaaaaaaa-0000-0000-0000-000000000001.jsonl")
	if err := os.Chmod(transcript, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(transcript, 0o644) })
	t.Chdir(filepath.Join(home, "work", "webapp"))
	return home, folder, transcript
}

// A project whose transcripts can't be read says which and why, in every
// command, and says what to check, and never calls it a parse failure.
func TestUnreadableTranscriptsSayWhichAndWhy(t *testing.T) {
	home, folder, _ := unreadableFixture(t)
	tilde := func(p string) string { return "~" + strings.TrimPrefix(p, home) }

	out, stderr, err := executeCLISplit(t, "global", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"skipped_projects": 1`) {
		t.Errorf("json:\n%s", out)
	}
	// With its one transcript unreadable, nothing records where the project
	// ran, so it goes by its folder.
	want := "Warning: can't read the one session in " + folder + ": permission denied, so it's not in the totals. Check the owner and mode of its files.\n"
	if stderr != want {
		t.Errorf("global piped:\n got: %q\nwant: %q", stderr, want)
	}

	withStderrWidth(t, 80)
	_, stderr, err = executeCLISplit(t, "global", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if flat := strings.Join(strings.Fields(stderr), " "); !strings.Contains(flat, "can't read the one session in "+tilde(folder)+":") {
		t.Errorf("global on a terminal should read ~:\n%s", stderr)
	}
	for line := range strings.SplitSeq(strings.TrimSuffix(stderr, "\n"), "\n") {
		if len(line) > 80 && !strings.Contains(line, tilde(folder)) {
			t.Errorf("line past 80 columns: %q", line)
		}
	}
	withStderrWidth(t, 0)

	_, _, err = executeCLISplit(t, "summary")
	if want := "can't read this project's one session: permission denied. Check the owner and mode of the files in " + folder + "."; exitCode(err) != 1 || err.Error() != want {
		t.Errorf("summary: exit %d\n got: %v\nwant: %s", exitCode(err), err, want)
	}
	for _, args := range [][]string{{"show"}, {}} {
		_, _, err = executeCLISplit(t, args...)
		if want := "can't read session aaaaaaaa: permission denied. Check the owner and mode of its transcript in " + folder + "."; exitCode(err) != 1 || err.Error() != want {
			t.Errorf("%v: exit %d\n got: %v\nwant: %s", args, exitCode(err), err, want)
		}
	}
	withStderrWidth(t, 80)
	if got := formatError(nil, err); !strings.Contains(strings.Join(strings.Fields(got), " "), tilde(folder)+".") {
		t.Errorf("show on a terminal should read ~:\n%s", got)
	}

	// list still lists the session, as unreadable.
	out, _, err = executeCLISplit(t, "list", "-f", "json")
	if err != nil || !strings.Contains(out, "aaaaaaaa-0000-0000-0000-000000000001") {
		t.Errorf("list: %v\n%s", err, out)
	}
}

// A project with a recorded path goes by it. Of several sessions that
// failed, the warning names the first one's reason and counts the others
// that failed for another. A reason other than permissions names the first
// file instead of saying to check modes.
func TestSkippedProjectWarningWording(t *testing.T) {
	project := models.ProjectInfo{OriginalPath: filepath.Join("work", "api"), EncodedPath: "-work-api"}
	first := filepath.Join("projects", "-work-api", "a.jsonl")
	cfg := &config{}
	for _, tt := range []struct {
		err  error
		want string
	}{
		{
			&analyzer.ProjectError{Project: project, Sessions: 3, Err: &analyzer.NoSessionReadError{
				Sessions: 3, Path: first, Err: &fs.PathError{Op: "open", Path: first, Err: fs.ErrPermission}, Others: 1,
			}},
			"Warning: can't read any of " + project.OriginalPath + "'s 3 sessions: permission denied, and 1 more session for other reasons, so it's not in the totals. Check the owner and mode of the files in " + filepath.Dir(first) + ".\n",
		},
		{
			&analyzer.ProjectError{Project: project, Sessions: 1, Err: &analyzer.NoSessionReadError{
				Sessions: 1, Path: first, Err: &fs.PathError{Op: "read", Path: first, Err: syscall.EISDIR},
			}},
			"Warning: can't read " + project.OriginalPath + "'s one session: " + syscall.EISDIR.Error() + ", so it's not in the totals. The transcript is " + first + ".\n",
		},
		{
			&analyzer.ProjectError{Project: project, Err: &fs.PathError{Op: "open", Path: "projects", Err: fs.ErrPermission}},
			"Warning: left out " + project.OriginalPath + ": permission denied\n",
		},
	} {
		if got := wrapStderr(nil, skippedProjectWarning(cfg, tt.err)); got != tt.want {
			t.Errorf("\n got: %q\nwant: %q", got, tt.want)
		}
	}
}

// One cause can leave every project unreadable, so past maxListedFiles the
// rest are a count, and -v lists them all.
func TestSkippedProjectWarningsStopAtTen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ignores a file's mode bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
	config := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	for i := range 12 {
		dir := filepath.Join(config, "projects", fmt.Sprintf("-work-p%02d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		transcript := filepath.Join(dir, "aaaaaaaa-0000-0000-0000-000000000001.jsonl")
		if err := os.WriteFile(transcript, []byte(e2eMsg("2026-02-01T10:00:00Z", "m1", "claude-opus-4-8", 1, 1, 0, 0, 0)+"\n"), 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(transcript, 0o644) })
	}
	_, stderr, err := executeCLISplit(t, "global", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	if len(lines) != 11 || strings.Count(stderr, "Warning: can't read") != 10 || lines[10] != "Warning: left out 2 more projects. Run with -v to list all 12." {
		t.Errorf("stderr:\n%s", stderr)
	}
	_, stderr, err = executeCLISplit(t, "global", "-f", "json", "-v")
	if err != nil || strings.Count(stderr, "Warning: can't read") != 12 {
		t.Errorf("-v: %v\n%s", err, stderr)
	}
}
