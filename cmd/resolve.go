package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
	"github.com/bardisty/ficha/internal/render"
)

// tracef prints one step of project and session resolution under -v, so a
// "can't find sessions" error can be traced to the directory ficha looked in.
// On a terminal the line wraps like a note, keeping each absolute path in
// args whole.
func (cfg *config) tracef(format string, args ...any) {
	if !cfg.verbose || cfg.stderr == nil {
		return
	}
	for i, a := range args {
		if p, ok := a.(string); ok && filepath.IsAbs(p) {
			args[i] = render.NoBreak(p)
		}
	}
	fmt.Fprint(cfg.stderr, wrapStderr(cfg.stderr, fmt.Sprintf("Debug: "+format+"\n", args...)))
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
		return newDisplayError("no Claude Code data in %s (from CLAUDE_CONFIG_DIR). Check that CLAUDE_CONFIG_DIR points at Claude Code's config directory.", errPath(projectsDir))
	}
	return newDisplayError("no Claude Code data in %s (default ~/.claude). Start Claude Code in a project, then run ficha from the same directory.", errPath(projectsDir))
}

// projectArgs is the flag that selects proj: -p with its real path, which
// normally resolves by exact match (unless the directory has since moved or a
// symlink changed). A path from another OS (a Windows transcript read under
// WSL) isn't absolute here and can't resolve, so it gets the encoded
// directory name instead.
func projectArgs(proj models.ProjectInfo) string {
	return suggestProject("", proj).String()
}

// suggestProject is command followed by projectArgs, for an error.
func suggestProject(command string, proj models.ProjectInfo) errCommand {
	if filepath.IsAbs(proj.OriginalPath) {
		return errCommand{text: command + "-p ", path: proj.OriginalPath}
	}
	return errCommand{text: command + "--project-dir=", path: proj.EncodedPath}
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
	// path is the directory Claude Code ran in, when the project was found
	// by it: "" for --project-dir and for a match by name elsewhere.
	path string
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
	if name, ok := projectFolderName(canonical); ok {
		return projectFolder(cfg, filepath.Join(projectsDir, name)), nil
	}

	exactDir := filepath.Join(projectsDir, paths.PathToProjectDir(canonical))
	if isDir(exactDir) {
		cfg.tracef("found project directory %s", filepath.Base(exactDir))
		return resolvedProject{dir: exactDir, label: canonical, path: canonical}, nil
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
			return resolvedProject{}, newDisplayError("no Claude Code data in %s (default ~/.claude).%s", errPath(projectsDir), hint)
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
			return resolvedProject{dir: p.FullPath, label: p.OriginalPath, path: canonical}, nil
		}
	}

	// A directory where only claude --worktree ever ran has no project of
	// its own, just projects under it. For summary and list, which read
	// those, it stands for itself, before a parent or a same-named project
	// elsewhere can claim it. A project directory can outlive its
	// transcripts, and one of those has nothing to read.
	if cfg.readsNested && len(nestedWithSessions(canonical, projectsDir)) > 0 {
		cfg.tracef("projects under %s", canonical)
		return resolvedProject{dir: exactDir, label: canonical, path: canonical}, nil
	}

	// A parent directory's project is far likelier to be the one meant than a
	// project elsewhere that shares this directory's name, and common names
	// ("src", "api") make such matches easy. So the parent hint comes first,
	// and the name fallback is left to directories with no project above them,
	// such as a project moved or renamed on disk.
	if parent := ancestorWithProject(canonical, projectsDir); parent != "" {
		cfg.tracef("parent directory %s has a project", parent)
		return resolvedProject{}, noProjectError(cfg, projPath, canonical, parent, projectsDir, allProjects)
	}

	match, err := paths.FindProjectDir(projPath, allProjects)
	var ambiguous *paths.AmbiguousProjectError
	switch {
	case errors.Is(err, paths.ErrNoProjectFound):
		cfg.tracef("no project among %d has the name %q", len(allProjects), filepath.Base(canonical))
		return resolvedProject{}, noProjectError(cfg, projPath, canonical, "", projectsDir, allProjects)
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
	// The note is the only sign the numbers may be another directory's. It
	// says what ficha found, not that the directory has no sessions: across
	// WSL, the project shown can be this directory under its other path, or
	// this directory's sessions can be in a config dir ficha isn't reading.
	// Both paths stay whole if it wraps.
	if match.MatchMethod == "suffix" {
		writeNote(cfg, "found no sessions for %s. Showing %s, the only project named %s.",
			render.NoBreak(tildePath(canonical)), render.NoBreak(tildePath(label)), paths.BasenameCrossOS(label))
	}
	return resolvedProject{dir: match.ProjectDir, label: label}, nil
}

// projectFolderName returns the name of the projects dir's folder that dir
// is, which --project-dir takes, when it's one. Both are compared with
// symlinks resolved, since ~/.claude can be a link.
func projectFolderName(dir string) (string, bool) {
	projectsDir, err := paths.GetProjectsDir()
	if err != nil || !isDir(dir) {
		return "", false
	}
	canonical, err := paths.CanonicalizePath(dir)
	if err != nil {
		return "", false
	}
	projects, err := paths.CanonicalizePath(projectsDir)
	if err != nil || filepath.Dir(canonical) != projects {
		return "", false
	}
	return filepath.Base(canonical), true
}

// projectFolder is a projects dir folder given as -p or run from: Claude
// Code's record of a project rather than a directory it ran in. It reads as
// --project-dir would, under the path the project's transcripts recorded.
func projectFolder(cfg *config, dir string) resolvedProject {
	cfg.tracef("%s is a project folder in the projects dir", filepath.Base(dir))
	project := resolvedProject{dir: dir, label: filepath.Base(dir)}
	if recorded := parser.ProjectOriginalPath(dir); filepath.IsAbs(recorded) {
		project.label, project.path = recorded, recorded
	}
	return project
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
// message: a -p path that doesn't exist or isn't a directory, a directory
// whose sessions are all in the projects under it, or a real directory
// Claude Code never ran in. Next steps are parent, the nearest parent that
// has sessions ("" for none), and projects with a similar name.
func noProjectError(cfg *config, projPath, canonical, parent, projectsDir string, allProjects []models.ProjectInfo) error {
	addSimilar := func(e *displayError) {
		if projects := similarProjects(canonical, allProjects); len(projects) > 0 {
			e.add("\nProjects with a similar name:")
			for _, p := range projects {
				e.add("\n  %s", suggestProject(cfg.command()+" ", p))
			}
		}
	}

	if info, err := os.Stat(projPath); err != nil {
		e := newDisplayError("no such directory: %s", errPath(projPath))
		addSimilar(e)
		return e
	} else if !info.IsDir() {
		return newDisplayError("not a directory: %s", errPath(projPath))
	}

	parentHint := func(e *displayError) {
		if parent != "" {
			e.add("\nFound sessions for parent directory %s. Run: %s", errPath(parent), errCommand{text: cfg.command() + " -p ", path: parent})
		}
	}

	if e := nestedOnlyError(cfg, canonical, projectsDir); e != nil {
		parentHint(e)
		return e
	}

	e := newDisplayError("Claude Code has no sessions for %s. Sessions are recorded per directory Claude Code was started in.", errPath(canonical))
	hint := wslHint(canonical)
	e.add("%s", hint)
	parentHint(e)
	addSimilar(e)
	// Every project this build can list is on the Windows side, so for a
	// directory in WSL the list is no help.
	if hint == "" {
		e.add("\nRun %s to see every project.", errWhole("'ficha global'"))
	}
	return e
}

// ancestorWithProject returns the nearest parent of dir whose Claude project
// has sessions, or "". A project directory can outlive its transcripts
// (Claude Code's cleanup leaves the folder), and suggesting one of those would
// fail. It's only ever a hint, never auto-selected, and the home directory
// and the ones above it are skipped: many people have run Claude Code in ~
// itself, which would make every unmatched directory under it look like part
// of that project, and a project in /home would do the same for everyone's.
func ancestorWithProject(dir, projectsDir string) string {
	home := canonicalHome()
	for parent := filepath.Dir(dir); parent != dir; dir, parent = parent, filepath.Dir(parent) {
		if holdsHome(parent, home) || filepath.Dir(parent) == parent {
			continue
		}
		if hasTranscripts(filepath.Join(projectsDir, paths.PathToProjectDir(parent))) {
			return parent
		}
	}
	return ""
}

// nestedProject is a Claude project whose recorded path lies under another
// project's, such as a worktree's under its repo.
type nestedProject struct {
	dir string
	// path is the directory Claude Code recorded for it.
	path string
	// name is its path below the outer project's, slash-separated and less
	// the .claude/worktrees/ that claude --worktree puts it under, as in
	// "fix-auth" or "packages/api".
	name string
}

// nestedProjects returns the projects whose recorded path lies under
// project's, in the order the projects dir lists them. Like
// ancestorWithProject it skips the home directory, which many people have
// run Claude Code in, and every directory above it, such as /home: every
// project would be under them.
//
// Claude Code names a project's directory after its path, so a nested
// project's name starts with the outer one's. Only those are opened to read
// their recorded path, which the lossy name can't stand in for.
func nestedProjects(project resolvedProject) []nestedProject {
	if project.path == "" || holdsHome(project.path, canonicalHome()) {
		return nil
	}
	projectsDir := filepath.Dir(project.dir)
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}
	prefix := strings.ToLower(paths.PathToProjectDir(project.path) + "-")
	var nested []nestedProject
	for _, e := range entries {
		dir := filepath.Join(projectsDir, e.Name())
		if !strings.HasPrefix(strings.ToLower(e.Name()), prefix) || dir == project.dir || !isDir(dir) {
			continue
		}
		recorded := parser.ProjectOriginalPath(dir)
		if recorded == "" || !isWithin(recorded, project.path) {
			continue
		}
		rel, err := filepath.Rel(project.path, recorded)
		if err != nil || rel == "." {
			continue
		}
		rel = filepath.ToSlash(rel)
		nested = append(nested, nestedProject{dir: dir, path: recorded, name: strings.TrimPrefix(rel, ".claude/worktrees/")})
	}
	return nested
}

// nestedWithSessions is nestedProjects for the directory path, less the
// projects with no session left, sorted by name.
func nestedWithSessions(path, projectsDir string) []nestedProject {
	root := resolvedProject{dir: filepath.Join(projectsDir, paths.PathToProjectDir(path)), path: path}
	nested := slices.DeleteFunc(nestedProjects(root), func(p nestedProject) bool { return !hasTranscripts(p.dir) })
	slices.SortFunc(nested, func(a, b nestedProject) int { return strings.Compare(a.name, b.name) })
	return nested
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
	d := newDisplayError("Claude Code has no sessions for %s, and %d projects share its name %q. Pick one:", errPath(dir), len(e.Matches), e.Basename)
	for _, p := range e.Matches {
		d.add("\n  %s", suggestProject(cfg.command()+" ", p))
	}
	return d
}

// nestedOnlyError explains a directory with no sessions of its own whose
// sessions are all in the projects under it, as for someone who only runs
// claude --worktree, with the command that shows each. It's nil when no
// project under it has a session.
func nestedOnlyError(cfg *config, path, projectsDir string) *displayError {
	const maxListed = 5
	nested := nestedWithSessions(path, projectsDir)
	if len(nested) == 0 {
		return nil
	}
	names := make([]string, len(nested))
	for i, p := range nested {
		names[i] = p.name
	}
	e := newDisplayError("Claude Code has no sessions for %s itself, but %s.", errPath(path), nestedHave(names))
	if len(nested) == 1 {
		e.add(" Run: %s", errCommand{text: cfg.command() + " -p ", path: nested[0].path})
		return e
	}
	e.add(" Pick one:")
	for _, p := range nested[:min(len(nested), maxListed)] {
		e.add("\n  %s", errCommand{text: cfg.command() + " -p ", path: p.path})
	}
	if len(nested) > maxListed {
		e.add("\nTo list them all, run: %s", errWhole("ficha list"+cfg.typedProjectArgs()+" --nested"))
	}
	return e
}

// noSessionsError explains a project directory that holds no transcripts.
// A real directory passed to --project-dir, rather than one under the
// projects dir, is almost always meant for -p.
func noSessionsError(cfg *config, project resolvedProject) error {
	projectsDir, _ := paths.GetProjectsDir()
	if cfg.projectDir != "" && paths.LooksLikePath(cfg.projectDir) && !isWithin(project.dir, projectsDir) {
		return newDisplayError("%s has no Claude Code sessions. --project-dir takes a directory under %s. For sessions started in %s, run: %s",
			errPath(cfg.projectDir), errPath(projectsDir), errPath(cfg.projectDir), errCommand{text: cfg.command() + " -p ", path: cfg.projectDir})
	}
	if project.path != "" {
		if e := nestedOnlyError(cfg, project.path, projectsDir); e != nil {
			return e
		}
	}
	return newDisplayError("Claude Code has no sessions for %s yet%s", errPath(project.label), wslHint(project.label))
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

// canonicalHome returns the home directory with symlinks resolved, or "" if
// there's none.
func canonicalHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	home, _ = paths.CanonicalizePath(home)
	return home
}

// holdsHome reports whether dir is the home directory or one above it. With
// no home it's false: isWithin would read "" as the working directory.
func holdsHome(dir, home string) bool {
	return home != "" && isWithin(home, dir)
}

// isWithin reports whether path is inside dir, comparing the symlink-resolved,
// absolute forms of both.
func isWithin(path, dir string) bool {
	path, _ = paths.CanonicalizePath(path)
	dir, _ = paths.CanonicalizePath(dir)
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
