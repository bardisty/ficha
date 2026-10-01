package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
	"github.com/bardisty/ficha/internal/render"
)

// tracef prints one step of project and session resolution under -v, so a
// "can't find sessions" error can be traced to the directory ficha looked in.
func (cfg *config) tracef(format string, args ...any) {
	if cfg.verbose && cfg.stderr != nil {
		fmt.Fprintf(cfg.stderr, "Debug: "+format+"\n", args...)
	}
}

// command is the command as typed, for suggestions the user can paste back.
func (cfg *config) command() string {
	if cfg.commandPath == "" {
		return "ficha"
	}
	return cfg.commandPath
}

// projectsDirOrigin says where the projects directory came from.
func projectsDirOrigin() string {
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		return "from CLAUDE_CONFIG_DIR"
	}
	return "default ~/.claude"
}

// noDataError reports a projects directory that is missing or holds no
// projects: either CLAUDE_CONFIG_DIR points somewhere else, or Claude Code
// has never run as this user.
func noDataError(projectsDir string) error {
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		return fmt.Errorf("no Claude Code data in %s (from CLAUDE_CONFIG_DIR). Check that CLAUDE_CONFIG_DIR points at Claude Code's config directory.", projectsDir)
	}
	return fmt.Errorf("no Claude Code data in %s (default ~/.claude). Start Claude Code in a project, then run ficha from the same directory.", projectsDir)
}

// projectArgs is the flag that selects proj: -p with its real path, which
// normally resolves by exact match (unless the directory has since moved or a
// symlink changed). A path from another OS (a Windows transcript read under
// WSL) isn't absolute here and can't resolve, so it gets the encoded
// directory name instead.
func projectArgs(proj models.ProjectInfo) string {
	if filepath.IsAbs(proj.OriginalPath) {
		return "-p " + shellQuote(proj.OriginalPath)
	}
	return "--project-dir=" + shellQuote(proj.EncodedPath)
}

// typedProjectArgs repeats the project flag the user passed, if any, so a
// suggested follow-up command looks at the same project.
func (cfg *config) typedProjectArgs() string {
	switch {
	case cfg.projectDir != "":
		return " --project-dir=" + shellQuote(cfg.projectDir)
	case cfg.projectPath != "":
		return " -p " + shellQuote(cfg.projectPath)
	}
	return ""
}

// resolvedProject is the Claude project directory a command reads, and the
// name to call it by in messages: the directory Claude Code ran in when
// known, since the encoded directory name is nothing anyone typed.
type resolvedProject struct {
	dir   string
	label string
}

// resolveProjectDirectory determines which Claude project directory to use.
// Priority: --project-dir flag > --project/-p flag > current directory.
// Uses name matching when the exact encoded path doesn't exist.
func resolveProjectDirectory(cfg *config) (resolvedProject, error) {
	projectsDir, err := paths.GetProjectsDir()
	if err != nil {
		return resolvedProject{}, err
	}
	cfg.tracef("projects dir %s (%s)", projectsDir, projectsDirOrigin())

	if cfg.projectDir != "" {
		dir, err := paths.ResolveProjectDir(cfg.projectDir)
		if err != nil {
			cfg.tracef("--project-dir %s: %v", cfg.projectDir, err)
			if !paths.LooksLikePath(cfg.projectDir) && !hasProjects(projectsDir) {
				return resolvedProject{}, noDataError(projectsDir)
			}
			return resolvedProject{}, err
		}
		cfg.tracef("--project-dir %s is %s", cfg.projectDir, dir)
		return resolvedProject{dir: dir, label: cfg.projectDir}, nil
	}

	projPath, err := getProjectPath(cfg)
	if err != nil {
		return resolvedProject{}, err
	}
	origin := "current directory"
	if cfg.projectPath != "" {
		origin = "from -p"
	}
	cfg.tracef("project path %s (%s)", projPath, origin)
	canonical, err := paths.CanonicalizePath(projPath)
	if err != nil {
		return resolvedProject{}, err
	}
	if canonical != projPath {
		cfg.tracef("resolves to %s", canonical)
	}

	exactDir := filepath.Join(projectsDir, paths.PathToProjectDir(canonical))
	if isDir(exactDir) {
		cfg.tracef("found project directory %s", filepath.Base(exactDir))
		return resolvedProject{dir: exactDir, label: canonical}, nil
	}
	cfg.tracef("no project directory %s", filepath.Base(exactDir))

	allProjects, err := parser.DiscoverAllProjects()
	if err != nil {
		return resolvedProject{}, fmt.Errorf("discovering projects: %w", err)
	}
	if len(allProjects) == 0 {
		cfg.tracef("projects dir is missing or empty")
		// From WSL, noDataError's advice to start Claude Code in a project is
		// wrong: the user did, in WSL, where this build doesn't look.
		if hint := wslHint(canonical); hint != "" {
			return resolvedProject{}, fmt.Errorf("no Claude Code data in %s (default ~/.claude).%s", projectsDir, hint)
		}
		return resolvedProject{}, noDataError(projectsDir)
	}

	// Claude Code shortens a folder name past 200 characters with a hash
	// suffix PathToProjectDir can't reproduce, so a deep directory's own
	// project is found by the path its transcripts recorded, before any
	// parent can claim it.
	for _, p := range allProjects {
		if p.OriginalPath == canonical {
			cfg.tracef("found project directory %s by its recorded path", p.EncodedPath)
			return resolvedProject{dir: p.FullPath, label: p.OriginalPath}, nil
		}
	}

	// A parent directory's project is far likelier to be the one meant than a
	// project elsewhere that shares this directory's name, and common names
	// ("src", "api") make such matches easy. So the parent hint comes first,
	// and the name fallback is left to directories with no project above them,
	// such as a project moved or renamed on disk.
	if parent := ancestorWithProject(canonical, projectsDir); parent != "" {
		cfg.tracef("parent directory %s has a project", parent)
		return resolvedProject{}, noProjectError(cfg, projPath, canonical, parent, allProjects)
	}

	match, err := paths.FindProjectDir(projPath, allProjects)
	var ambiguous *paths.AmbiguousProjectError
	switch {
	case errors.Is(err, paths.ErrNoProjectFound):
		cfg.tracef("no project among %d has the name %q", len(allProjects), filepath.Base(canonical))
		return resolvedProject{}, noProjectError(cfg, projPath, canonical, "", allProjects)
	case errors.As(err, &ambiguous):
		cfg.tracef("%d projects have the name %q", len(ambiguous.Matches), ambiguous.Basename)
		return resolvedProject{}, ambiguousProjectError(cfg, canonical, ambiguous)
	case err != nil:
		return resolvedProject{}, err
	}

	label := match.EncodedPath
	for _, p := range allProjects {
		if p.FullPath == match.ProjectDir && p.OriginalPath != "" {
			label = p.OriginalPath
		}
	}
	if match.MatchInfo != "" {
		// The note names the project's path, which has to stay whole if
		// the note wraps
		writeNote(cfg, "%s", strings.Replace(match.MatchInfo, label, render.NoBreak(label), 1))
	}
	return resolvedProject{dir: match.ProjectDir, label: label}, nil
}

// hasProjects reports whether projectsDir holds at least one project.
func hasProjects(projectsDir string) bool {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if isDir(filepath.Join(projectsDir, e.Name())) && !strings.HasPrefix(e.Name(), ".") {
			return true
		}
	}
	return false
}

// noProjectError explains a directory with no Claude project, one cause per
// message: a -p path that doesn't exist or isn't a directory, or a real
// directory Claude Code never ran in. Next steps are parent, the nearest
// parent that has sessions ("" for none), and projects with a similar name.
func noProjectError(cfg *config, projPath, canonical, parent string, allProjects []models.ProjectInfo) error {
	var similar strings.Builder
	if projects := similarProjects(canonical, allProjects); len(projects) > 0 {
		similar.WriteString("\nProjects with a similar name:")
		for _, p := range projects {
			fmt.Fprintf(&similar, "\n  %s %s", cfg.command(), projectArgs(p))
		}
	}

	if info, err := os.Stat(projPath); err != nil {
		return fmt.Errorf("no such directory: %s%s", projPath, similar.String())
	} else if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", projPath)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Claude Code has no sessions for %s. Sessions are recorded per directory Claude Code was started in.", canonical)
	hint := wslHint(canonical)
	sb.WriteString(hint)
	if parent != "" {
		fmt.Fprintf(&sb, "\nFound sessions for parent directory %s. Run: %s -p %s", parent, cfg.command(), shellQuote(parent))
	}
	sb.WriteString(similar.String())
	// Every project this build can list is on the Windows side, so for a
	// directory in WSL the list is no help.
	if hint == "" {
		sb.WriteString("\nRun 'ficha global' to see every project.")
	}
	return errors.New(sb.String())
}

// ancestorWithProject returns the nearest parent of dir whose Claude project
// has sessions, or "". A project directory can outlive its transcripts
// (Claude Code's cleanup leaves the folder), and suggesting one of those would
// fail. It's only ever a hint, never auto-selected, and the home directory is
// skipped: many people have run Claude Code in ~ itself, which would make
// every unmatched directory under it look like part of that project.
func ancestorWithProject(dir, projectsDir string) string {
	home, _ := os.UserHomeDir()
	home, _ = paths.CanonicalizePath(home)
	for parent := filepath.Dir(dir); parent != dir; dir, parent = parent, filepath.Dir(parent) {
		if parent == home || filepath.Dir(parent) == parent {
			continue
		}
		if hasTranscripts(filepath.Join(projectsDir, paths.PathToProjectDir(parent))) {
			return parent
		}
	}
	return ""
}

// hasTranscripts reports whether a project directory holds any session.
func hasTranscripts(projectDir string) bool {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			return true
		}
	}
	return false
}

// similarProjects returns up to five projects whose real path's name shares a
// prefix with dir's name.
func similarProjects(dir string, allProjects []models.ProjectInfo) []models.ProjectInfo {
	const maxSuggestions = 5
	basename := strings.ToLower(filepath.Base(dir))
	var out []models.ProjectInfo
	for _, proj := range allProjects {
		if proj.OriginalPath == "" {
			continue
		}
		projBasename := strings.ToLower(paths.BasenameCrossOS(proj.OriginalPath))
		if strings.HasPrefix(projBasename, basename) || strings.HasPrefix(basename, projBasename) {
			out = append(out, proj)
			if len(out) == maxSuggestions {
				break
			}
		}
	}
	return out
}

// ambiguousProjectError lists the projects sharing dir's name as commands to
// pick one with.
func ambiguousProjectError(cfg *config, dir string, e *paths.AmbiguousProjectError) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Claude Code has no sessions for %s, and %d projects share its name %q. Pick one:", dir, len(e.Matches), e.Basename)
	for _, p := range e.Matches {
		fmt.Fprintf(&sb, "\n  %s %s", cfg.command(), projectArgs(p))
	}
	return errors.New(sb.String())
}

// noSessionsError explains a project directory that holds no transcripts.
// A real directory passed to --project-dir, rather than one under the
// projects dir, is almost always meant for -p.
func noSessionsError(cfg *config, project resolvedProject) error {
	projectsDir, _ := paths.GetProjectsDir()
	if cfg.projectDir != "" && paths.LooksLikePath(cfg.projectDir) && !isWithin(project.dir, projectsDir) {
		return fmt.Errorf("%s has no Claude Code sessions. --project-dir takes a directory under %s. For sessions started in %s, run: %s -p %s",
			cfg.projectDir, projectsDir, cfg.projectDir, cfg.command(), shellQuote(cfg.projectDir))
	}
	return fmt.Errorf("Claude Code has no sessions for %s yet%s", project.label, wslHint(project.label))
}

// wslHint explains the Windows build run from a directory inside WSL, when
// windowsDefaultInWSL says its sessions are out of reach.
func wslHint(dir string) string {
	if !windowsDefaultInWSL(dir) {
		return ""
	}
	return "\nClaude Code in WSL keeps its sessions in your WSL home directory, where the Windows build of ficha doesn't look. Run the Linux build inside WSL."
}

// windowsDefaultInWSL reports whether this is the Windows build, reading the
// Windows profile's .claude, from a directory inside WSL. Claude Code running
// in WSL writes to the WSL home, so that directory's sessions are out of
// reach. With CLAUDE_CONFIG_DIR set, the user chose where ficha looks, and
// that may not hold.
func windowsDefaultInWSL(dir string) bool {
	return runtime.GOOS == "windows" && isWSLPath(dir) && os.Getenv("CLAUDE_CONFIG_DIR") == ""
}

// isWSLPath reports whether dir is a Windows path into a WSL distribution:
// \\wsl.localhost\<distro>\... or the older \\wsl$\<distro>\... form.
func isWSLPath(dir string) bool {
	dir = strings.ToLower(strings.ReplaceAll(dir, "/", `\`))
	return strings.HasPrefix(dir, `\\wsl.localhost\`) || strings.HasPrefix(dir, `\\wsl$\`)
}

// isWithin reports whether path is inside dir, comparing the symlink-resolved,
// absolute forms of both.
func isWithin(path, dir string) bool {
	path, _ = paths.CanonicalizePath(path)
	dir, _ = paths.CanonicalizePath(dir)
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
