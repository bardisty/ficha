package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

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
	webappDir := addProject(t, configDir, withSessions, false)
	// Real transcripts carry the cwd, which similar-name matching reads.
	cwdLine := `{"type":"user","timestamp":"2026-02-01T09:59:00Z","cwd":` + strconv.Quote(withSessions) + `,"message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(filepath.Join(webappDir, "eeeeeeee-0000-0000-0000-000000000000.jsonl"), []byte(cwdLine), 0o644); err != nil {
		t.Fatal(err)
	}
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
			dir, _, ok := waitingProject(&cfg)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if want := filepath.Join(projects, paths.PathToProjectDir(tt.wantDir)); dir != want {
					t.Errorf("dir = %s, want %s", dir, want)
				}
			}
		})
	}

	// Without -p the path is the real cwd, so a similar name is no typo.
	t.Chdir(sibling)
	cwdCfg := config{}
	if _, _, ok := waitingProject(&cwdCfg); !ok {
		t.Error("cwd next to a similarly named project didn't wait")
	}

	// An empty projects directory is a config problem, like a missing one.
	emptyConfig := realDir(t, "empty-config")
	if err := os.MkdirAll(filepath.Join(emptyConfig, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", emptyConfig)
	freshCfg := config{projectPath: fresh}
	if _, _, ok := waitingProject(&freshCfg); ok {
		t.Error("waited with an empty projects directory")
	}

	// No projects directory at all is a config problem, not a wait.
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(configDir, "missing"))
	cfg := config{projectPath: fresh}
	if _, _, ok := waitingProject(&cfg); ok {
		t.Error("waited with no projects directory")
	}
}
