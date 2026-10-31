package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
	"github.com/bardisty/ficha/internal/tui"
)

// waitForFirstSession opens a live view in a waiting state when the project
// has no session yet, so the panes can be laid out before Claude Code
// starts. open builds the waiting model from the project directory to wait
// on, the path Claude Code will run in, and the waiting screen's hint line
// in its forms from longest to shortest. waited is false when there is
// nothing to wait for, and the caller reports its own error instead.
func waitForFirstSession(cfg *config, open func(dir, projectPath string, hint []string) tea.Model) (waited bool, err error) {
	// A script is better served by the error than by a wait.
	if !isTerminal(cfg.stdout) {
		return false, nil
	}
	dir, projectPath, hint, ok := waitingProject(cfg)
	if !ok {
		return false, nil
	}
	cfg.tracef("no session yet; waiting in %s", filepath.Base(dir))
	return true, runTUI(cfg, func() tea.Model { return open(dir, tildePath(projectPath), hint) })
}

// waitingWatch opens watch waiting for a first session.
func waitingWatch(cfg *config) func(dir, projectPath string, hint []string) tea.Model {
	return func(dir, projectPath string, hint []string) tea.Model {
		return tui.NewWaitingModel(dir, projectPath, hint, cfg.noColor, !cfg.noFollow)
	}
}

// waitingBreakdown opens breakdown waiting for a first session.
func waitingBreakdown(cfg *config) func(dir, projectPath string, hint []string) tea.Model {
	return func(dir, projectPath string, hint []string) tea.Model {
		return tui.NewWaitingBreakdownModel(dir, projectPath, hint, cfg.noColor, !cfg.noFollow)
	}
}

// waitingProject returns the project directory to wait on: the exact one for
// the path ficha runs in (the cwd, or -p), which may not exist until Claude
// Code first runs there. Other projects that only share its name don't stop
// the wait: whatever Claude Code starts here is recorded under this path, so
// a live view following one of them would never see it. hint names them, so
// someone who meant one can switch, unless projects under this directory
// have sessions: hint names those instead. ok is false when waiting would hide a
// real problem or a better answer: an explicit --project-dir, a missing or
// empty projects directory (usually a wrong CLAUDE_CONFIG_DIR), a path that
// isn't a directory, a project that recorded this path as its own, a
// same-named project recorded from the other OS (it may be this directory),
// a -p path with similarly named projects and none of the same name (a likely
// typo), a subdirectory of a project with sessions, or a directory inside WSL
// seen from the Windows build's default config, whose sessions will never
// show up there. A repository or worktree root nested in such a project still
// waits: Claude Code records sessions under the directory it starts in, and
// that is where it would start.
func waitingProject(cfg *config) (dir, projectPath string, hint []string, ok bool) {
	if cfg.projectDir != "" {
		return "", "", nil, false
	}
	projectsDir, err := paths.GetProjectsDir()
	if err != nil || !isDir(projectsDir) {
		return "", "", nil, false
	}
	projPath, err := getProjectPath(cfg)
	if err != nil {
		return "", "", nil, false
	}
	if !isDir(projPath) {
		return "", "", nil, false
	}
	canonical, err := paths.CanonicalizePath(projPath)
	if err != nil {
		return "", "", nil, false
	}
	if windowsDefaultInWSL(canonical) {
		return "", "", nil, false
	}
	if !isRepoRoot(canonical) && ancestorWithProject(canonical, projectsDir) != "" {
		return "", "", nil, false
	}
	dir = filepath.Join(projectsDir, paths.PathToProjectDir(canonical))
	if isDir(dir) {
		// hasTranscripts can't tell an unreadable directory from an empty one
		if _, err := os.ReadDir(dir); err != nil || hasTranscripts(dir) {
			return "", "", nil, false
		}
		return dir, canonical, nestedHint(cfg, canonical, projectsDir), true
	}
	all, err := parser.DiscoverAllProjects()
	if err != nil || len(all) == 0 {
		return "", "", nil, false
	}
	var sameName []models.ProjectInfo
	for _, p := range all {
		// A folder name Claude Code shortened is found by its recorded path,
		// and that project is this directory's own.
		if p.OriginalPath == canonical {
			return "", "", nil, false
		}
		if p.OriginalPath == "" || !strings.EqualFold(paths.BasenameCrossOS(p.OriginalPath), filepath.Base(canonical)) {
			continue
		}
		// A path from the other OS, such as Windows' Claude Code seen through
		// a shared config from WSL, may well be this directory under another
		// name, and the name match is how the README says to reach it.
		if !filepath.IsAbs(p.OriginalPath) {
			return "", "", nil, false
		}
		sameName = append(sameName, p)
	}
	// The projects under this directory are where its sessions went, while
	// one elsewhere that shares its name is only a guess, so they win the
	// hint's one line. And a directory with sessions under it is no typo.
	if hint := nestedHint(cfg, canonical, projectsDir); hint != nil {
		return dir, canonical, hint, true
	}
	// Refusing with a same-named project would hand the view to it.
	if cfg.projectPath != "" && len(sameName) == 0 && len(similarProjects(canonical, all)) > 0 {
		return "", "", nil, false
	}
	return dir, canonical, sameNameHint(cfg, filepath.Base(canonical), sameName), true
}

// nestedHint is the waiting screen's line naming the projects under the
// waiting directory that have sessions, longest form first: the one project
// with the command that opens it, or how many there are.
func nestedHint(cfg *config, path, projectsDir string) []string {
	nested := nestedWithSessions(path, projectsDir)
	switch len(nested) {
	case 0:
		return nil
	case 1:
		cmd := errCommand{text: cfg.command() + " -p ", path: nested[0].path}.onTerminal()
		return []string{
			"Under it, " + nested[0].name + " has sessions: " + cmd,
			"Under it, " + nested[0].name + " has sessions",
			"1 project under it has sessions",
		}
	}
	have := plural(len(nested), "project") + " under it have sessions"
	return []string{have + ": ficha list" + cfg.typedProjectArgs() + " --nested", have}
}

// sameNameHint is the waiting screen's line naming the projects that share
// the waiting directory's name, longest form first: the one project with the
// command that opens it, or how many there are.
func sameNameHint(cfg *config, name string, sameName []models.ProjectInfo) []string {
	switch len(sameName) {
	case 0:
		return nil
	case 1:
		p := sameName[0]
		where, arg := tildePath(p.OriginalPath), projectArgs(p)
		// "~" expands only unquoted
		if shellQuote(where) == where {
			arg = "-p " + where
		}
		return []string{
			fmt.Sprintf("%s has sessions: %s %s", where, cfg.command(), arg),
			where + " has sessions",
		}
	}
	others := fmt.Sprintf("%d projects named %s", len(sameName), name)
	return []string{others + " have sessions: ficha global", others + " have sessions", others}
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
