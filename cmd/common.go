package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
	"github.com/bardisty/ficha/internal/render"
)

// ErrSessionNotFound is returned when a specific session ID cannot be matched
var ErrSessionNotFound = errors.New("session not found")

// sessionLookupError marks an error from finding the project or session to
// show, as opposed to one from analyzing it. The message is unchanged.
type sessionLookupError struct{ err error }

func (e *sessionLookupError) Error() string { return e.err.Error() }
func (e *sessionLookupError) Unwrap() error { return e.err }

// loadProjectSessions loads all sessions for the current project.
// It handles project path resolution, disk scanning, index loading, and source merging.
// countMessages controls whether per-file message counts are computed during
// discovery (see loadProjectSessionsWithDir). Returns the merged sessions list
// or an error.
func loadProjectSessions(cfg *config, countMessages bool) ([]models.SessionEntry, error) {
	sessions, _, err := loadProjectSessionsWithDir(cfg, countMessages)
	return sessions, err
}

// loadProjectSessionsWithDir loads all sessions and returns the project directory path.
// Used by live-view commands that need to watch the project directory for new sessions.
//
// countMessages gates the discovery-time message-count scan. Every command
// passes false and takes counts from the analyzer's own parse, so each file
// is read once.
func loadProjectSessionsWithDir(cfg *config, countMessages bool) ([]models.SessionEntry, string, error) {
	project, err := resolveProjectDirectory(cfg)
	if err != nil {
		return nil, "", err
	}
	projDir := project.dir

	// Scan disk for session files
	diskSessions, err := parser.DiscoverSessionsFromDisk(projDir, countMessages)
	if err != nil {
		// The *os.PathError already names the directory; wrapping it whole
		// would print the path twice.
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && pathErr.Path == projDir {
			if errors.Is(err, fs.ErrPermission) {
				return nil, "", fmt.Errorf("can't read %s: %w. Check its permissions.", projDir, fs.ErrPermission)
			}
			return nil, "", fmt.Errorf("can't read %s: %w", projDir, pathErr.Err)
		}
		return nil, "", fmt.Errorf("scanning sessions in %s: %w", projDir, err)
	}

	// Try to load index (may fail or be incomplete)
	indexPath := paths.GetSessionsIndexPath(projDir)
	index, indexErr := parser.ParseSessionsIndex(indexPath)
	if indexErr != nil && !os.IsNotExist(indexErr) {
		// Index exists but is malformed - warn but continue
		writeWarning(cfg, "failed to parse sessions-index.json: %s", errorText(indexErr))
	}

	// Merge sources
	sessions, orphanCount := parser.MergeSessionSources(index, diskSessions, projDir, countMessages)

	if len(sessions) == 0 {
		cfg.tracef("no transcripts in %s", filepath.Base(projDir))
		return nil, "", noSessionsError(cfg, project)
	}
	cfg.tracef("using %s (%d sessions)", filepath.Base(projDir), len(sessions))

	// Sessions missing from an index are only worth a note when there is an
	// index: current Claude Code doesn't write one, so without it every
	// session would count.
	if orphanCount > 0 && cfg.verbose && index != nil {
		writeNote(cfg, "Found %d session(s) not in sessions-index.json", orphanCount)
	}

	return sessions, projDir, nil
}

// selectSession finds the appropriate session based on CLI args.
// Returns the session, project directory, whether a session ID was explicitly provided, and any error.
func selectSession(cfg *config, args []string) (*models.SessionEntry, string, bool, error) {
	session, projectDir, explicit, err := findSession(cfg, args)
	if err != nil {
		return nil, "", false, &sessionLookupError{err}
	}
	cfg.tracef("session file %s", session.FullPath)
	return session, projectDir, explicit, nil
}

func findSession(cfg *config, args []string) (*models.SessionEntry, string, bool, error) {
	// Analysis paths (show/watch/breakdown) recompute counts from their own
	// parse, so skip the discovery-time message-count scan.
	explicitSessionID := len(args) > 0
	if explicitSessionID && isTranscriptPath(args[0]) {
		session, projectDir, err := sessionFromPath(cfg, args[0])
		return session, projectDir, true, err
	}
	sessions, projectDir, err := loadProjectSessionsWithDir(cfg, false)
	if err != nil {
		// A session ID copied from elsewhere may belong to a project other
		// than this directory's, and saying where beats a bare project error.
		if explicitSessionID {
			if session, dir, elsewhere := locateSession(cfg, args[0], ""); session != nil {
				return session, dir, true, nil
			} else if elsewhere != nil {
				return nil, "", false, elsewhere
			}
		}
		return nil, "", false, err
	}

	if explicitSessionID {
		session, err := findSessionByPartialID(sessions, args[0])
		if err != nil {
			if errors.Is(err, ErrSessionNotFound) {
				if session, dir, elsewhere := locateSession(cfg, args[0], projectDir); session != nil {
					return session, dir, true, nil
				} else if elsewhere != nil {
					return nil, "", false, elsewhere
				}
				return nil, "", false, &sessionNotFoundError{fmt.Sprintf("session not found: %s. Run 'ficha list%s' to see this project's sessions.", args[0], cfg.typedProjectArgs())}
			}
			return nil, "", false, err
		}
		return session, projectDir, true, nil
	}

	// Get latest session (sort by modified time, SessionID tiebreaker)
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].Modified.Equal(sessions[j].Modified) {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return sessions[i].Modified.After(sessions[j].Modified)
	})
	// Note: len(sessions) == 0 is already handled by loadProjectSessionsWithDir
	return &sessions[0], projectDir, false, nil
}

// findSessionByPartialID finds a session by partial ID match, ignoring case
// and a pasted .jsonl extension. Returns the matching session, or an error if
// multiple sessions match.
func findSessionByPartialID(sessions []models.SessionEntry, partialID string) (*models.SessionEntry, error) {
	id := normalizeSessionID(partialID)
	// Empty ID would match all sessions via prefix matching - reject it
	if len(sessions) == 0 || id == "" {
		return nil, ErrSessionNotFound
	}

	// Try exact match first
	for i := range sessions {
		if strings.ToLower(sessions[i].SessionID) == id {
			return &sessions[i], nil
		}
	}

	// Try prefix match - collect all matches
	var matches []models.SessionEntry
	for i := range sessions {
		if strings.HasPrefix(strings.ToLower(sessions[i].SessionID), id) {
			matches = append(matches, sessions[i])
		}
	}

	if len(matches) == 0 {
		return nil, ErrSessionNotFound
	}

	if len(matches) == 1 {
		return &matches[0], nil
	}

	// Multiple matches: list the newest, with what `list` would say about
	// each, so the right one can be picked without running it.
	const displayLimit = 10
	sortSessionsByModified(matches)
	shown := matches[:min(len(matches), displayLimit)]
	descs := describeSessions(shown, time.Now())
	var sb strings.Builder
	fmt.Fprintf(&sb, "ambiguous session ID %q matches %d sessions:", partialID, len(matches))
	for i, m := range shown {
		fmt.Fprintf(&sb, "\n  %s  %s", m.SessionID, descs[i])
	}
	if len(matches) > displayLimit {
		fmt.Fprintf(&sb, "\n  ... and %d more", len(matches)-displayLimit)
	}
	return nil, errors.New(sb.String())
}

// sessionNotFoundError is ErrSessionNotFound with a message that says where
// to look next.
type sessionNotFoundError struct{ msg string }

func (e *sessionNotFoundError) Error() string        { return e.msg }
func (e *sessionNotFoundError) Is(target error) bool { return target == ErrSessionNotFound }

// locateSession looks for a session ID in every project except excludeDir.
// It only lists directories, with no parsing, so a miss stays cheap. A full
// session ID can't be ambiguous, so when exactly one other project has it,
// locateSession returns that session and its project directory, and notes
// the project on stderr, leaving stdout clean for json. Otherwise it returns
// an error saying where the matches are, or nothing when there are none.
func locateSession(cfg *config, arg, excludeDir string) (*models.SessionEntry, string, error) {
	id := normalizeSessionID(arg)
	projectsDir, err := paths.GetProjectsDir()
	if id == "" || err != nil {
		return nil, "", nil
	}
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, "", nil
	}
	type hit struct{ projectDir, sessionID string }
	var hits []hit
	for _, e := range entries {
		dir := filepath.Join(projectsDir, e.Name())
		if dir == excludeDir || !isDir(dir) {
			continue
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			name := strings.ToLower(f.Name())
			if !f.IsDir() && strings.HasSuffix(name, ".jsonl") && strings.HasPrefix(name, id) {
				hits = append(hits, hit{dir, strings.TrimSuffix(f.Name(), ".jsonl")})
			}
		}
	}
	if len(hits) == 0 {
		return nil, "", nil
	}
	cfg.tracef("session %s found in %d other project(s)", arg, len(hits))

	projects, _ := parser.DiscoverAllProjects()
	byDir := make(map[string]models.ProjectInfo, len(projects))
	for _, p := range projects {
		byDir[p.FullPath] = p
	}
	project := func(dir string) models.ProjectInfo {
		if p, ok := byDir[dir]; ok {
			return p
		}
		return models.ProjectInfo{EncodedPath: filepath.Base(dir), FullPath: dir}
	}
	run := func(h hit) string {
		return fmt.Sprintf("%s %s %s", cfg.command(), shortSessionID(h.sessionID), projectArgs(project(h.projectDir)))
	}

	if len(hits) == 1 {
		h := hits[0]
		p := project(h.projectDir)
		where := p.OriginalPath
		if where == "" {
			where = p.EncodedPath
		}
		if isFullSessionID(id) && strings.EqualFold(h.sessionID, id) {
			session, err := parser.SessionFromFile(filepath.Join(h.projectDir, h.sessionID+".jsonl"))
			if err == nil {
				writeNote(cfg, "session %s is in %s.", shortSessionID(h.sessionID), render.NoBreak(where))
				return &session, h.projectDir, nil
			}
		}
		return nil, "", &sessionNotFoundError{fmt.Sprintf("session %s is in %s. Run: %s", shortSessionID(h.sessionID), where, run(h))}
	}
	const maxHits = 5
	var sb strings.Builder
	fmt.Fprintf(&sb, "session %s isn't here, but matches %d sessions in other projects:", arg, len(hits))
	for _, h := range hits[:min(len(hits), maxHits)] {
		sb.WriteString("\n  " + run(h))
	}
	if len(hits) > maxHits {
		fmt.Fprintf(&sb, "\n  ... and %d more", len(hits)-maxHits)
	}
	return nil, "", &sessionNotFoundError{sb.String()}
}

// isFullSessionID reports whether id is a whole UUID, 8-4-4-4-12 hex digits.
func isFullSessionID(id string) bool {
	if len(id) != 36 || !looksLikeSessionID(id) {
		return false
	}
	for i, r := range id {
		if (i == 8 || i == 13 || i == 18 || i == 23) != (r == '-') {
			return false
		}
	}
	return true
}

// isTranscriptPath reports whether arg names a .jsonl file rather than a
// session ID: a file that exists, or anything with a directory in it. A bare
// "<id>.jsonl" that doesn't exist here stays a pasted file name, which the ID
// lookup accepts.
func isTranscriptPath(arg string) bool {
	if !strings.EqualFold(filepath.Ext(arg), ".jsonl") {
		return false
	}
	if strings.ContainsAny(arg, `/\`) {
		return true
	}
	info, err := os.Stat(arg)
	return err == nil && !info.IsDir()
}

// sessionFromPath is the session a transcript path names. Its project is the
// directory it's in, which is where the analyzer finds its agents, so no
// project resolution runs, and -p and --project-dir don't apply.
func sessionFromPath(cfg *config, arg string) (*models.SessionEntry, string, error) {
	path, err := filepath.Abs(arg)
	if err != nil {
		return nil, "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", fmt.Errorf("can't read transcript %s: %w", arg, unwrapPathError(err))
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("%s isn't a transcript file", arg)
	}
	// Agents write to <project>/<session>/subagents/agent-*.jsonl, and
	// workflow agents one level further down, in workflows/<run>/.
	dir := filepath.Dir(path)
	if filepath.Base(filepath.Dir(dir)) == "workflows" {
		dir = filepath.Dir(filepath.Dir(dir))
	}
	if filepath.Base(dir) == "subagents" {
		sessionDir := filepath.Dir(dir)
		parent := sessionDir + filepath.Ext(path)
		return nil, "", usageErrorf("%s is an agent's transcript, part of session %s. Run: %s %s",
			filepath.Base(path), shortSessionID(filepath.Base(sessionDir)), cfg.command(), shellQuote(parent))
	}
	if cfg.projectPath != "" || cfg.projectDir != "" {
		cfg.tracef("a transcript path names its own project; ignoring -p and --project-dir")
	}
	session, err := parser.SessionFromFile(path)
	if err != nil {
		return nil, "", err
	}
	if agentsOutOfReach(path, session.SessionID) {
		dir := filepath.Join(filepath.Dir(arg), session.SessionID) + string(filepath.Separator)
		writeNote(cfg, "any agents this session ran aren't counted. ficha looks for them in %s, which isn't there.", render.NoBreak(dir))
	}
	return &session, filepath.Dir(path), nil
}

// agentsOutOfReach reports whether a transcript's agents can't be found:
// there's no folder named for the session beside it, where Claude Code writes
// them, and it isn't under the projects directory. A copy that took the folder
// along finds its agents, and a session there that ran none has nothing
// missing. It checks the directory the analyzer searches, the one the path
// names, so a link to a transcript in the projects directory still counts as
// outside it.
func agentsOutOfReach(path, sessionID string) bool {
	if info, err := os.Stat(filepath.Join(filepath.Dir(path), sessionID)); err == nil && info.IsDir() {
		return false
	}
	projects, err := paths.GetProjectsDir()
	if err != nil {
		return false
	}
	return !isWithin(filepath.Dir(path), projects)
}

// unwrapPathError drops an *os.PathError's own copy of the path, for a
// message that names the path already.
func unwrapPathError(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}
