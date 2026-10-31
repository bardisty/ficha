package cmd

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/paths"
	"github.com/bardisty/ficha/internal/render"
)

// setupNestedFixture builds a repo project with two projects under it, one a
// claude --worktree worktree and one a package, and a sibling whose encoded
// name starts like the repo's but which isn't under it. Each session has
// its own message IDs, as separate sessions do. It returns the repo's
// directory, the projects dir, and home.
func setupNestedFixture(t *testing.T) (repo, projectsDir, home string) {
	t.Helper()
	home, err := paths.CanonicalizePath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	projectsDir = filepath.Join(config, "projects")
	repo = filepath.Join(home, "work", "repo")

	write := func(dir, id, ts string, out int64) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		project := filepath.Join(projectsDir, paths.PathToProjectDir(dir))
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		line := strings.Replace(e2eMsg(ts, id[:8], "claude-opus-4-8", 1000, out, 0, 0, 0),
			`{"type":"assistant",`, `{"type":"assistant","cwd":`+jsonString(dir)+`,`, 1)
		if err := os.WriteFile(filepath.Join(project, id+".jsonl"), []byte(line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(repo, "aaaaaaaa-0000-4000-8000-000000000001", "2026-02-01T10:00:00Z", 100)
	write(filepath.Join(repo, ".claude", "worktrees", "fix-auth"), "bbbbbbbb-0000-4000-8000-000000000002", "2026-02-02T10:00:00Z", 2000)
	write(filepath.Join(repo, "packages", "api"), "cccccccc-0000-4000-8000-000000000003", "2026-02-03T10:00:00Z", 3000)
	write(filepath.Join(home, "work", "repo-old"), "dddddddd-0000-4000-8000-000000000004", "2026-02-04T10:00:00Z", 4000)
	t.Chdir(repo)
	return repo, projectsDir, home
}

type nestedSummaryJSON struct {
	ProjectPath string `json:"project_path"`
	TotalCost   struct {
		TotalCost float64 `json:"total_cost"`
	} `json:"total_cost"`
	SessionCount int `json:"session_count"`
}

func TestNestedProjects(t *testing.T) {
	repo, projectsDir, _ := setupNestedFixture(t)
	repoProject := filepath.Join(projectsDir, paths.PathToProjectDir(repo))
	worktree := filepath.Join(repo, ".claude", "worktrees", "fix-auth")
	pkg := filepath.Join(repo, "packages", "api")

	costOf := func(args ...string) nestedSummaryJSON {
		t.Helper()
		out, _, err := executeCLISplit(t, append([]string{"summary", "-f", "json"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		var s nestedSummaryJSON
		mustJSON(t, out, &s)
		return s
	}
	own := costOf()
	nestedCost := costOf("-p", worktree).TotalCost.TotalCost + costOf("-p", pkg).TotalCost.TotalCost

	t.Run("summary notes the nested projects", func(t *testing.T) {
		stdout, stderr, err := executeCLISplit(t, "summary", "--no-color")
		if err != nil {
			t.Fatal(err)
		}
		note := "have 2 sessions, up to " + render.Cost(nestedCost) + ". Add --nested to count them in."
		if !strings.Contains(stderr, "Note: 2 projects under ") || !strings.Contains(stderr, note) {
			t.Errorf("stderr = %q, want a note ending %q", stderr, note)
		}
		if strings.Contains(stdout, "--nested") || !strings.Contains(stdout, "Sessions: 1") {
			t.Errorf("stdout:\n%s", stdout)
		}
	})

	t.Run("--nested totals them in", func(t *testing.T) {
		got := costOf("--nested")
		if math.Abs(got.TotalCost.TotalCost-(own.TotalCost.TotalCost+nestedCost)) > 1e-9 || got.SessionCount != 3 {
			t.Errorf("--nested: %+v, want $%v over 3 sessions", got, own.TotalCost.TotalCost+nestedCost)
		}
		if got.ProjectPath != repoProject {
			t.Errorf("aggregate project_path %q, want the repo's %q", got.ProjectPath, repoProject)
		}
		_, stderr, err := executeCLISplit(t, "summary", "--nested")
		if err != nil || strings.Contains(stderr, "Note:") {
			t.Errorf("--nested still notes: %v %q", err, stderr)
		}
	})

	t.Run("each record names its own project", func(t *testing.T) {
		want := map[string]string{
			"aaaaaaaa-0000-4000-8000-000000000001": repoProject,
			"bbbbbbbb-0000-4000-8000-000000000002": filepath.Join(projectsDir, paths.PathToProjectDir(worktree)),
			"cccccccc-0000-4000-8000-000000000003": filepath.Join(projectsDir, paths.PathToProjectDir(pkg)),
		}
		out, _, err := executeCLISplit(t, "summary", "-d", "--nested", "-f", "json")
		if err != nil {
			t.Fatal(err)
		}
		var detail struct {
			Sessions []struct {
				SessionID   string `json:"session_id"`
				ProjectPath string `json:"project_path"`
			} `json:"sessions"`
		}
		mustJSON(t, out, &detail)
		if len(detail.Sessions) != 3 {
			t.Fatalf("summary -d --nested json has %d sessions:\n%s", len(detail.Sessions), out)
		}
		for _, s := range detail.Sessions {
			if s.ProjectPath != want[s.SessionID] {
				t.Errorf("json %s: project_path %q, want %q", s.SessionID, s.ProjectPath, want[s.SessionID])
			}
		}

		out, _, err = executeCLISplit(t, "summary", "-d", "--nested", "-f", "csv")
		if err != nil {
			t.Fatal(err)
		}
		rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		col := map[string]int{}
		for i, name := range rows[0] {
			col[name] = i
		}
		for _, row := range rows[1:] {
			if id := row[col["session_id"]]; row[col["project_path"]] != want[id] {
				t.Errorf("csv %s: project_path %q, want %q", id, row[col["project_path"]], want[id])
			}
		}

		out, _, err = executeCLISplit(t, "list", "--nested", "-f", "json")
		if err != nil {
			t.Fatal(err)
		}
		var listed []struct {
			SessionID    string `json:"session_id"`
			ProjectPath  string `json:"project_path"`
			OriginalPath string `json:"original_path"`
		}
		mustJSON(t, out, &listed)
		recorded := map[string]string{
			"aaaaaaaa-0000-4000-8000-000000000001": repo,
			"bbbbbbbb-0000-4000-8000-000000000002": worktree,
			"cccccccc-0000-4000-8000-000000000003": pkg,
		}
		if len(listed) != 3 {
			t.Fatalf("list --nested json has %d records:\n%s", len(listed), out)
		}
		for _, s := range listed {
			if s.ProjectPath != want[s.SessionID] || s.OriginalPath != recorded[s.SessionID] {
				t.Errorf("list %s: %q, %q", s.SessionID, s.ProjectPath, s.OriginalPath)
			}
		}
	})

	t.Run("the tables name each session's project", func(t *testing.T) {
		out, _, err := executeCLISplit(t, "list", "--nested", "--no-color")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"fix-auth", "packages/api"} {
			if !strings.Contains(out, want) {
				t.Errorf("list --nested doesn't name %s:\n%s", want, out)
			}
		}
		if !strings.Contains(out, "+2 projects") || strings.Contains(out, "repo-old") || strings.Contains(out, "dddddddd") {
			t.Errorf("list --nested took in a sibling:\n%s", out)
		}
		out, _, err = executeCLISplit(t, "summary", "-d", "--nested", "--no-color")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"+2 projects", "PROJECT", "fix-auth", "packages/api", "  .\n"} {
			if !strings.Contains(out, want) {
				t.Errorf("summary -d --nested lacks %q:\n%s", want, out)
			}
		}
	})

	t.Run("a window counts what's inside it", func(t *testing.T) {
		_, stderr, err := executeCLISplit(t, "list", "--since", "2026-02-02T12:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stderr, "has 1 session in the window") {
			t.Errorf("stderr = %q", stderr)
		}
		_, stderr, err = executeCLISplit(t, "summary", "--until", "2026-02-01T23:00:00Z")
		if err != nil || strings.Contains(stderr, "Note:") {
			t.Errorf("a window with no nested session notes: %v %q", err, stderr)
		}
	})

	// The formatter looks a record's original_path up by its transcript's
	// directory, which a --project-dir typed with a trailing slash isn't.
	t.Run("original_path with a trailing slash", func(t *testing.T) {
		out, _, err := executeCLISplit(t, "list", "-f", "json", "--project-dir="+repoProject+string(filepath.Separator))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, `"original_path": `+jsonString(repo)) {
			t.Errorf("original_path lost:\n%s", out)
		}
	})

	t.Run("--project-dir has no directory to look under", func(t *testing.T) {
		_, _, err := executeCLISplit(t, "summary", "--nested", "--project-dir="+paths.PathToProjectDir(repo))
		if exitCode(err) != 2 || !strings.Contains(err.Error(), "--project-dir") {
			t.Errorf("error %v, exit %d", err, exitCode(err))
		}
	})
}

// Under ~, every project is nested, so home gets no note.
func TestNestedProjectsNotAtHome(t *testing.T) {
	_, _, home := setupNestedFixture(t)
	t.Setenv("HOME", filepath.Join(home, "work", "repo"))
	t.Setenv("USERPROFILE", filepath.Join(home, "work", "repo"))
	_, stderr, err := executeCLISplit(t, "summary")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr, "Note:") {
		t.Errorf("stderr = %q", stderr)
	}
}

// --nested at ~, or above it, counts no project under it. The report is
// what it would be without the flag, and a note says why and points to
// global, on stderr for every format.
func TestNestedAtHomeSaysItSkipped(t *testing.T) {
	const note = "Note: --nested doesn't look under your home directory. 'ficha global' totals every project."
	for _, tt := range []struct {
		name string
		home func(repo string) string
		args []string
	}{
		{"at home", func(repo string) string { return repo }, nil},
		{"-p home", func(repo string) string { return repo }, []string{"-p", "REPO"}},
		{"above home", func(repo string) string { return filepath.Join(repo, "home", "user") }, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo, _, _ := setupNestedFixture(t)
			home := tt.home(repo)
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			var extra []string
			for _, a := range tt.args {
				extra = append(extra, strings.ReplaceAll(a, "REPO", repo))
			}
			if len(extra) > 0 {
				t.Chdir(t.TempDir())
			}
			for _, command := range [][]string{
				{"summary"}, {"summary", "-f", "json"}, {"summary", "-f", "csv"},
				{"list"}, {"list", "-f", "json"}, {"list", "-f", "csv"},
			} {
				args := append(append([]string{}, command...), extra...)
				wantOut, _, err := executeCLISplit(t, args...)
				if err != nil {
					t.Fatalf("%v: %v", args, err)
				}
				out, stderr, err := executeCLISplit(t, append(args, "--nested")...)
				if err != nil {
					t.Fatalf("%v --nested: %v", args, err)
				}
				if out != wantOut {
					t.Errorf("%v --nested: stdout differs from without it:\n%s\nwant:\n%s", args, out, wantOut)
				}
				if strings.Count(stderr, "Note:") != 1 || !strings.Contains(stderr, note+"\n") {
					t.Errorf("%v --nested: stderr = %q, want the note once", args, stderr)
				}
			}
		})
	}
}

// At a directory whose sessions are all in the projects under it, show
// names those projects rather than opening one elsewhere that shares the
// directory's name, which comes up only as a hint. Tab completion offers
// none of that other project's sessions.
func TestShowPrefersNestedOverSameName(t *testing.T) {
	repo, projectsDir, home := setupNestedFixture(t)
	if err := os.RemoveAll(filepath.Join(projectsDir, paths.PathToProjectDir(repo))); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "elsewhere", "repo")
	writeCwdSession(t, filepath.Join(projectsDir, paths.PathToProjectDir(other)), other)

	want := "Claude Code has no sessions for " + repo + " itself, but 2 projects under it have: fix-auth, packages/api. Pick one:"
	for _, args := range [][]string{{"show"}, {}, {"show", "-p", repo}} {
		_, _, err := executeCLISplit(t, args...)
		if exitCode(err) != 1 || !strings.HasPrefix(err.Error(), want) {
			t.Fatalf("%v: exit %d, error %v, want it to start %q", args, exitCode(err), err, want)
		}
		command := strings.Join(append([]string{"ficha"}, args[:min(len(args), 1)]...), " ")
		if hint := `Another project is also named "repo". Run: ` + command + ` -p ` + shellQuote(other); !strings.Contains(err.Error(), hint) {
			t.Errorf("%v: error %v, want the hint %q", args, err, hint)
		}
	}
	if ids, _ := completeLines(t, "show", ""); len(ids) != 0 {
		t.Errorf("completion offers %q", ids)
	}
	// A full session ID still opens its session wherever it is.
	if _, _, err := executeCLISplit(t, "show", "eeeeeeee-0000-0000-0000-000000000000"); err != nil {
		t.Errorf("show by full ID: %v", err)
	}

	// Two same-named projects are listed, not an error of their own.
	third := filepath.Join(home, "old", "repo")
	writeCwdSession(t, filepath.Join(projectsDir, paths.PathToProjectDir(third)), third)
	_, _, err := executeCLISplit(t, "show")
	if err == nil || !strings.HasPrefix(err.Error(), want) || !strings.Contains(err.Error(), `2 other projects are also named "repo":`) {
		t.Errorf("error %v", err)
	}

}

// A directory above ~, such as /home, holds every project under ~ too, so it
// gets no note, doesn't stand in for the projects under it, and isn't
// offered as a parent.
func TestNestedProjectsNotAboveHome(t *testing.T) {
	setHome := func(t *testing.T, repo string) {
		t.Helper()
		home := filepath.Join(repo, "home", "user")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
	}

	t.Run("no note", func(t *testing.T) {
		repo, _, _ := setupNestedFixture(t)
		setHome(t, repo)
		_, stderr, err := executeCLISplit(t, "summary")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(stderr, "Note:") {
			t.Errorf("stderr = %q", stderr)
		}
	})

	t.Run("no stand-in", func(t *testing.T) {
		repo, projectsDir, _ := setupNestedFixture(t)
		setHome(t, repo)
		if err := os.RemoveAll(filepath.Join(projectsDir, paths.PathToProjectDir(repo))); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"summary"}, {"summary", "--nested"}} {
			_, _, err := executeCLISplit(t, args...)
			want := "Claude Code has no sessions for " + repo + ". Sessions are recorded per directory Claude Code was started in."
			if exitCode(err) != 1 || !strings.HasPrefix(err.Error(), want) {
				t.Errorf("%v: exit %d, error %v, want it to start %q", args, exitCode(err), err, want)
			}
		}
	})

	t.Run("no parent hint", func(t *testing.T) {
		repo, _, _ := setupNestedFixture(t)
		setHome(t, repo)
		src := filepath.Join(repo, "src")
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(src)
		_, _, err := executeCLISplit(t, "summary")
		if err == nil || strings.Contains(err.Error(), "parent directory") {
			t.Errorf("error %v", err)
		}
	})
}

// A repo where only claude --worktree ever ran has no sessions of its own:
// either no project directory at all, or one that holds no transcript.
func TestNestedProjectsWithoutOwnSessions(t *testing.T) {
	for _, tt := range []struct {
		name   string
		remove func(repoProject string) error
	}{
		{"no project directory", os.RemoveAll},
		{"an empty project directory", func(repoProject string) error {
			return os.Remove(filepath.Join(repoProject, "aaaaaaaa-0000-4000-8000-000000000001.jsonl"))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo, projectsDir, _ := setupNestedFixture(t)
			repoProject := filepath.Join(projectsDir, paths.PathToProjectDir(repo))
			if err := tt.remove(repoProject); err != nil {
				t.Fatal(err)
			}

			for _, command := range []string{"summary", "list"} {
				_, _, err := executeCLISplit(t, command)
				want := "Claude Code has no sessions for " + repo + " itself, but 2 projects under it have: fix-auth, packages/api. Run: ficha " + command + " --nested"
				if exitCode(err) != 1 || err.Error() != want {
					t.Errorf("%s: exit %d, error %v, want %q", command, exitCode(err), err, want)
				}
			}

			out, _, err := executeCLISplit(t, "summary", "--nested", "-f", "json")
			if err != nil {
				t.Fatal(err)
			}
			var got nestedSummaryJSON
			mustJSON(t, out, &got)
			if got.SessionCount != 2 || got.ProjectPath != repoProject {
				t.Errorf("summary --nested: %+v, want 2 sessions under the repo's own %q", got, repoProject)
			}
			out, _, err = executeCLISplit(t, "list", "--nested", "--no-color")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "~/work/repo +2 projects") {
				t.Errorf("list --nested names the repo some other way:\n%s", out)
			}
		})
	}

	// show, watch and breakdown read one project, so the projects under the
	// directory don't stand in for its own. The error names them instead,
	// with the -p that reads each, since those commands have no --nested.
	for _, tt := range []struct {
		name   string
		remove func(repoProject string) error
	}{
		{"no project directory", os.RemoveAll},
		{"an empty project directory", func(repoProject string) error {
			return os.Remove(filepath.Join(repoProject, "aaaaaaaa-0000-4000-8000-000000000001.jsonl"))
		}},
	} {
		t.Run("show names them, "+tt.name, func(t *testing.T) {
			repo, projectsDir, _ := setupNestedFixture(t)
			if err := tt.remove(filepath.Join(projectsDir, paths.PathToProjectDir(repo))); err != nil {
				t.Fatal(err)
			}
			for _, command := range [][]string{{"show"}, {}} {
				_, _, err := executeCLISplit(t, command...)
				line := strings.Join(append([]string{"ficha"}, command...), " ")
				want := "Claude Code has no sessions for " + repo + " itself, but 2 projects under it have: fix-auth, packages/api. Pick one:\n" +
					"  " + line + " -p " + shellQuote(filepath.Join(repo, ".claude", "worktrees", "fix-auth")) + "\n" +
					"  " + line + " -p " + shellQuote(filepath.Join(repo, "packages", "api"))
				if exitCode(err) != 1 || err.Error() != want {
					t.Errorf("%v: exit %d, error:\n%v\nwant:\n%s", command, exitCode(err), err, want)
				}
			}
		})
	}

	t.Run("show with one project under the directory", func(t *testing.T) {
		repo, projectsDir, _ := setupNestedFixture(t)
		for _, dir := range []string{repo, filepath.Join(repo, "packages", "api")} {
			if err := os.RemoveAll(filepath.Join(projectsDir, paths.PathToProjectDir(dir))); err != nil {
				t.Fatal(err)
			}
		}
		worktree := filepath.Join(repo, ".claude", "worktrees", "fix-auth")
		_, _, err := executeCLISplit(t, "show", "-p", repo)
		want := "Claude Code has no sessions for " + repo + " itself, but 1 project under it has: fix-auth. Run: ficha show -p " + shellQuote(worktree)
		if exitCode(err) != 1 || err.Error() != want {
			t.Fatalf("exit %d, error:\n%v\nwant:\n%s", exitCode(err), err, want)
		}
		if _, _, err := executeCLISplit(t, "show", "-p", worktree, "-f", "json"); err != nil {
			t.Errorf("the suggested command fails: %v", err)
		}
	})

	// A project directory left with an index and no transcripts doesn't
	// count as a project under the directory.
	for _, leftover := range []bool{false, true} {
		t.Run(fmt.Sprintf("a directory with neither gets the usual error, leftover %v", leftover), func(t *testing.T) {
			repo, projectsDir, _ := setupNestedFixture(t)
			src := filepath.Join(repo, "src")
			if err := os.MkdirAll(src, 0o755); err != nil {
				t.Fatal(err)
			}
			if leftover {
				old := filepath.Join(src, ".claude", "worktrees", "old")
				writeIndex(t, filepath.Join(projectsDir, paths.PathToProjectDir(old), "sessions-index.json"),
					`{"version":1,"entries":[],"originalPath":`+jsonString(old)+`}`)
			}
			t.Chdir(src)
			_, _, err := executeCLISplit(t, "summary", "--nested")
			want := "Claude Code has no sessions for " + src + ". Sessions are recorded per directory Claude Code was started in.\nFound sessions for parent directory " + repo + ". Run: ficha summary -p " + shellQuote(repo)
			if err == nil || !strings.HasPrefix(err.Error(), want) {
				t.Errorf("error %v, want it to start %q", err, want)
			}
		})
	}

	// Under ~, every project is nested, so home doesn't stand in for them.
	t.Run("not at home", func(t *testing.T) {
		repo, projectsDir, _ := setupNestedFixture(t)
		if err := os.RemoveAll(filepath.Join(projectsDir, paths.PathToProjectDir(repo))); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", repo)
		t.Setenv("USERPROFILE", repo)
		_, _, err := executeCLISplit(t, "summary", "--nested")
		if err == nil || strings.Contains(err.Error(), "projects under it") {
			t.Errorf("error %v", err)
		}
	})
}

// A project found by its name, as for a directory moved since Claude Code
// ran in it, has no directory on record to look under.
func TestNestedAfterANameMatch(t *testing.T) {
	_, _, home := setupNestedFixture(t)
	moved := filepath.Join(home, "moved", "repo-old")
	if err := os.MkdirAll(moved, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(moved)
	if _, _, err := executeCLISplit(t, "summary"); err != nil {
		t.Fatalf("the name match itself fails: %v", err)
	}
	for _, command := range []string{"summary", "list"} {
		_, _, err := executeCLISplit(t, command, "--nested")
		if exitCode(err) != 2 || !strings.Contains(err.Error(), "by its name") {
			t.Errorf("%s --nested: exit %d, error %v", command, exitCode(err), err)
		}
	}
}

// A worktree session that resumed one of the repo's own starts with a copy
// of its history, which --nested counts once. The note prices the nested
// sessions alone, so its figure is a bound on what --nested adds, and it
// says so.
func TestNestedNoteIsAnUpperBound(t *testing.T) {
	repo, projectsDir, _ := setupNestedFixture(t)
	own, err := os.ReadFile(filepath.Join(projectsDir, paths.PathToProjectDir(repo), "aaaaaaaa-0000-4000-8000-000000000001.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	fixAuth := filepath.Join(repo, ".claude", "worktrees", "fix-auth")
	resumed := filepath.Join(projectsDir, paths.PathToProjectDir(fixAuth), "bbbbbbbb-0000-4000-8000-000000000002.jsonl")
	worktree, err := os.ReadFile(resumed)
	if err != nil {
		t.Fatal(err)
	}
	// The copy is the same message, so --nested counts it once. Its cwd is
	// the worktree's, where the resumed session runs, which is how ficha
	// finds the project nested.
	copied := strings.Replace(string(own), jsonString(repo), jsonString(fixAuth), 1)
	if err := os.WriteFile(resumed, []byte(copied+string(worktree)), 0o644); err != nil {
		t.Fatal(err)
	}

	total := func(args ...string) float64 {
		t.Helper()
		out, _, err := executeCLISplit(t, append([]string{"summary", "-f", "json"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		var s nestedSummaryJSON
		mustJSON(t, out, &s)
		return s.TotalCost.TotalCost
	}
	added := total("--nested") - total()
	alone := total("-p", fixAuth) + total("-p", filepath.Join(repo, "packages", "api"))
	if alone <= added {
		t.Fatalf("the case is wrong: the nested sessions cost $%v alone and add $%v", alone, added)
	}
	for _, args := range [][]string{{"summary"}, {"list"}, {"list", "-f", "json"}} {
		_, stderr, err := executeCLISplit(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		note := "have 2 sessions, up to " + render.Cost(alone) + ". Add --nested to count them in."
		if !strings.Contains(stderr, note) {
			t.Errorf("%v: stderr = %q, want a note ending %q", args, stderr, note)
		}
	}
}

// Without --nested, the projects under the directory only feed the note, so
// what's wrong with their index is no warning of the report's.
func TestNestedIndexWarningsOnlyWithNested(t *testing.T) {
	repo, projectsDir, _ := setupNestedFixture(t)
	index := filepath.Join(projectsDir, paths.PathToProjectDir(filepath.Join(repo, "packages", "api")), "sessions-index.json")
	writeIndex(t, index, "{broken")
	for _, command := range []string{"summary", "list"} {
		_, stderr, err := executeCLISplit(t, command)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(stderr, "sessions-index.json") {
			t.Errorf("%s: stderr = %q", command, stderr)
		}
		_, stderr, err = executeCLISplit(t, command, "--nested")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stderr, "ignoring "+index) {
			t.Errorf("%s --nested: stderr = %q", command, stderr)
		}
	}
}

// Past five projects under the directory, show names five and points at
// list --nested for the rest.
func TestNestedOnlyErrorListsFive(t *testing.T) {
	repo, projectsDir, _ := setupNestedFixture(t)
	if err := os.RemoveAll(filepath.Join(projectsDir, paths.PathToProjectDir(repo))); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		wt := filepath.Join(repo, ".claude", "worktrees", name)
		writeCwdSession(t, filepath.Join(projectsDir, paths.PathToProjectDir(wt)), wt)
	}
	_, _, err := executeCLISplit(t, "show", "-p", repo)
	if err == nil {
		t.Fatal("want an error")
	}
	lines := strings.Split(err.Error(), "\n")
	want := "Claude Code has no sessions for " + repo + " itself, but 7 projects under it have: a, b, c, d, e and 2 more. Pick one:"
	if lines[0] != want {
		t.Errorf("first line:\n got %q\nwant %q", lines[0], want)
	}
	if last := lines[len(lines)-1]; last != "To list them all, run: ficha list -p "+shellQuote(repo)+" --nested" {
		t.Errorf("last line %q", last)
	}
	if len(lines) != 7 {
		t.Errorf("%d lines, want 7:\n%s", len(lines), err)
	}
}
