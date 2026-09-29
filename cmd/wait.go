package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
	"github.com/bardisty/ficha/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// waitForFirstSession opens a live view in a waiting state when the project
// has no session yet, so the panes can be laid out before Claude Code
// starts. open builds the waiting model from the project directory to wait
// on and the path Claude Code will run in. waited is false when there is
// nothing to wait for, and the caller reports its own error instead.
func waitForFirstSession(cfg *config, open func(dir, projectPath string) tea.Model) (waited bool, err error) {
	// A script is better served by the error than by a wait.
	if !isTerminal(cfg.stdout) {
		return false, nil
	}
	dir, projectPath, ok := waitingProject(cfg)
	if !ok {
		return false, nil
	}
	cfg.tracef("no session yet; waiting in %s", filepath.Base(dir))
	return true, runTUI(cfg.stdout, open(dir, tildePath(projectPath)))
}

// waitingWatch opens watch waiting for a first session.
func waitingWatch(cfg *config) func(dir, projectPath string) tea.Model {
	return func(dir, projectPath string) tea.Model {
		return tui.NewWaitingModel(dir, projectPath, cfg.verbose, cfg.noColor, !cfg.noFollow)
	}
}

// waitingBreakdown opens breakdown waiting for a first session.
func waitingBreakdown(cfg *config) func(dir, projectPath string) tea.Model {
	return func(dir, projectPath string) tea.Model {
		return tui.NewWaitingBreakdownModel(dir, projectPath, cfg.noColor, !cfg.noFollow)
	}
}

// waitingProject returns the project directory to wait on: the exact one for
// the path ficha runs in (the cwd, or -p), which may not exist until Claude
// Code first runs there. ok is false when waiting would hide a real problem
// or a better answer: an explicit --project-dir, a missing or empty projects
// directory (usually a wrong CLAUDE_CONFIG_DIR), a path that isn't a
// directory, a path that resolves to some other project by name, a -p path
// with similarly named projects (a likely typo), a subdirectory of a project
// with sessions, or a directory inside WSL seen from the Windows build, whose
// sessions go to the WSL home and will never show up here. A repository or
// worktree root nested in such a project still waits: Claude Code records
// sessions under the directory it starts in, and that is where it would
// start.
func waitingProject(cfg *config) (dir, projectPath string, ok bool) {
	if cfg.projectDir != "" {
		return "", "", false
	}
	projectsDir, err := paths.GetProjectsDir()
	if err != nil || !isDir(projectsDir) {
		return "", "", false
	}
	projPath, err := getProjectPath(cfg)
	if err != nil {
		return "", "", false
	}
	if !isDir(projPath) {
		return "", "", false
	}
	canonical, err := paths.CanonicalizePath(projPath)
	if err != nil {
		return "", "", false
	}
	if windowsBuildInWSL(canonical) {
		return "", "", false
	}
	if !isRepoRoot(canonical) && ancestorWithProject(canonical, projectsDir) != "" {
		return "", "", false
	}
	dir = filepath.Join(projectsDir, paths.PathToProjectDir(canonical))
	if isDir(dir) {
		sessions, err := parser.DiscoverSessionsFromDisk(dir, false)
		return dir, canonical, err == nil && len(sessions) == 0
	}
	all, err := parser.DiscoverAllProjects()
	if err != nil || len(all) == 0 {
		return "", "", false
	}
	if _, err := paths.FindProjectDir(projPath, all); !errors.Is(err, paths.ErrNoProjectFound) {
		return "", "", false
	}
	if cfg.projectPath != "" && len(similarProjects(canonical, all)) > 0 {
		return "", "", false
	}
	return dir, canonical, true
}

// isRepoRoot reports whether dir is the root of a git repository or
// worktree (".git" is a directory in one, a file in the other).
func isRepoRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// tildePath shortens a path under the home directory to "~/…".
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return p
}
