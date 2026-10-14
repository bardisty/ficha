package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/paths"
)

// waitingProject decides when a live view may open empty and wait for a
// first session instead of exiting.
func TestWaitingProject(t *testing.T) {
	configDir := realDir(t, "config")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	projects := filepath.Join(configDir, "projects")
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}

	withSessions := realDir(t, "work", "webapp")
	addCwdProject(t, configDir, withSessions)
	emptyProject := realDir(t, "work", "empty")
	addProject(t, configDir, emptyProject, true)
	fresh := realDir(t, "work", "brand-new-thing")
	subdir := filepath.Join(withSessions, "src")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(withSessions, ".worktrees", "feature")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sibling := realDir(t, "work2", "webapp-v2")

	tests := []struct {
		name    string
		cfg     config
		wantOK  bool
		wantDir string
	}{
		{"project dir exists, no sessions", config{projectPath: emptyProject}, true, emptyProject},
		{"Claude Code never ran here", config{projectPath: fresh}, true, fresh},
		{"project has sessions", config{projectPath: withSessions}, false, ""},
		{"subdirectory of a project: the parent hint wins", config{projectPath: subdir}, false, ""},
		{"explicit --project-dir", config{projectDir: "-x"}, false, ""},
		{"no such directory", config{projectPath: filepath.Join(fresh, "nope")}, false, ""},
		{"worktree nested in a project with sessions", config{projectPath: worktree}, true, worktree},
		{"-p with a similarly named project: likely a typo", config{projectPath: sibling}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			dir, _, hint, ok := waitingProject(&cfg)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if want := filepath.Join(projects, paths.PathToProjectDir(tt.wantDir)); dir != want {
					t.Errorf("dir = %s, want %s", dir, want)
				}
			}
			if hint != nil {
				t.Errorf("hint = %q with no same-named project", hint)
			}
		})
	}

	// Without -p the path is the real cwd, so a similar name is no typo.
	t.Chdir(sibling)
	cwdCfg := config{}
	if _, _, _, ok := waitingProject(&cwdCfg); !ok {
		t.Error("cwd next to a similarly named project didn't wait")
	}

	// A project elsewhere with the same name isn't this directory's: what
	// Claude Code starts here lands in this path's own project, from the cwd
	// or from -p. The hint names the other project, then counts several.
	clone := realDir(t, "clone", "webapp")
	sameName := func(name, want string) {
		t.Helper()
		t.Chdir(clone)
		for _, cfg := range []config{{}, {projectPath: clone}} {
			dir, _, hint, ok := waitingProject(&cfg)
			if !ok {
				t.Fatalf("%s, -p %q: didn't wait", name, cfg.projectPath)
			}
			if wantDir := filepath.Join(projects, paths.PathToProjectDir(clone)); dir != wantDir {
				t.Errorf("%s: dir = %s, want %s", name, dir, wantDir)
			}
			if len(hint) == 0 || !strings.HasPrefix(hint[0], want) {
				t.Errorf("%s: hint = %q, want it to start %q", name, hint, want)
			}
		}
	}
	sameName("one same-named project", tildePath(withSessions)+" has sessions: ficha ")
	addCwdProject(t, configDir, realDir(t, "other", "webapp"))
	sameName("two same-named projects", "2 projects named webapp have sessions: ficha global")

	// A similar name is no typo when the same name is there too: refusing
	// would hand the view to the same-named project.
	addCwdProject(t, configDir, realDir(t, "work", "webapp-api"))
	sameName("same-named and similarly named projects", "2 projects named webapp have sessions: ficha global")

	// From WSL with a shared Windows config, the same-named Windows project
	// may be this very directory, so the name match keeps it.
	winDir := filepath.Join(projects, "C--Users-me-src-mobile")
	writeCwdSession(t, winDir, `C:\Users\me\src\mobile`)
	if !filepath.IsAbs(`C:\Users\me\src\mobile`) {
		mobileCfg := config{projectPath: realDir(t, "mnt", "c", "Users", "me", "src", "mobile")}
		if _, _, _, ok := waitingProject(&mobileCfg); ok {
			t.Error("waited beside a same-named project recorded from the other OS")
		}
	}

	// A folder name Claude Code shortened doesn't match the path's encoding,
	// but the project recorded the path, so it's this directory's own.
	deep := realDir(t, "deep", "webapp")
	writeCwdSession(t, filepath.Join(projects, "-deep-webapp-1a2b3c"), deep)
	deepCfg := config{projectPath: deep}
	if _, _, _, ok := waitingProject(&deepCfg); ok {
		t.Error("waited for a directory whose project is under a shortened name")
	}

	// An empty projects directory is a config problem, like a missing one.
	emptyConfig := realDir(t, "empty-config")
	if err := os.MkdirAll(filepath.Join(emptyConfig, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", emptyConfig)
	freshCfg := config{projectPath: fresh}
	if _, _, _, ok := waitingProject(&freshCfg); ok {
		t.Error("waited with an empty projects directory")
	}

	// No projects directory at all is a config problem, not a wait.
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(configDir, "missing"))
	cfg := config{projectPath: fresh}
	if _, _, _, ok := waitingProject(&cfg); ok {
		t.Error("waited with no projects directory")
	}
}

// addCwdProject creates workDir's Claude project with one session that
// records workDir as its cwd.
func addCwdProject(t *testing.T, configDir, workDir string) {
	t.Helper()
	writeCwdSession(t, addProject(t, configDir, workDir, false), workDir)
}

// writeCwdSession writes a session into projectDir that records cwd, as real
// transcripts do; name matching reads it.
func writeCwdSession(t *testing.T, projectDir, cwd string) {
	t.Helper()
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","timestamp":"2026-02-01T09:59:00Z","cwd":` + strconv.Quote(cwd) + `,"message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projectDir, "eeeeeeee-0000-0000-0000-000000000000.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The hint names a single same-named project with a command that opens it.
// "~" only works unquoted, so a path that needs quotes is spelled out.
func TestSameNameHint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := config{commandPath: "ficha watch"}

	plain := filepath.Join(home, "work", "webapp")
	got := sameNameHint(&cfg, "webapp", []models.ProjectInfo{{OriginalPath: plain}})
	where := "~" + string(filepath.Separator) + filepath.Join("work", "webapp")
	arg := "-p " + where
	if runtime.GOOS == "windows" {
		arg = "-p " + shellQuote(plain)
	}
	want := []string{where + " has sessions: ficha watch " + arg, where + " has sessions"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}

	spaced := filepath.Join(home, "my work", "webapp")
	got = sameNameHint(&cfg, "webapp", []models.ProjectInfo{{OriginalPath: spaced}})
	if want := "ficha watch -p " + shellQuote(spaced); !strings.HasSuffix(got[0], want) {
		t.Errorf("got %q, want it to end %q", got[0], want)
	}

	if got := sameNameHint(&cfg, "webapp", nil); got != nil {
		t.Errorf("no same-named project: got %q", got)
	}
}
