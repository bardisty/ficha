package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
	"github.com/bardisty/ficha/internal/pricing"
)

// ErrSessionNotFound is returned when a specific session ID cannot be matched
var ErrSessionNotFound = errors.New("session not found")

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
// countMessages gates the discovery-time message-count scan: only `list`
// displays those counts, so every analysis path passes false and lets the
// analyzer recompute counts from its own parse (avoids scanning each file twice).
func loadProjectSessionsWithDir(cfg *config, countMessages bool) ([]models.SessionEntry, string, error) {
	// Resolve project directory
	projDir, err := resolveProjectDirectory(cfg)
	if err != nil {
		return nil, "", err
	}

	// Scan disk for session files
	diskSessions, err := parser.DiscoverSessionsFromDisk(projDir, countMessages)
	if err != nil {
		return nil, "", fmt.Errorf("scanning sessions in %s: %w", projDir, err)
	}

	// Try to load index (may fail or be incomplete)
	indexPath := paths.GetSessionsIndexPath(projDir)
	index, indexErr := parser.ParseSessionsIndex(indexPath)
	if indexErr != nil && !os.IsNotExist(indexErr) {
		// Index exists but is malformed - warn but continue
		fmt.Fprintf(cfg.stderr, "Warning: failed to parse sessions-index.json: %v\n", indexErr)
	}

	// Merge sources
	sessions, orphanCount := parser.MergeSessionSources(index, diskSessions, projDir, countMessages)

	if len(sessions) == 0 {
		return nil, "", fmt.Errorf("no sessions found in %s", projDir)
	}

	// Warn about orphans (only in verbose mode - this is common and usually not actionable)
	if orphanCount > 0 && cfg.verbose {
		fmt.Fprintf(cfg.stderr, "Note: Found %d session(s) not in sessions-index.json\n", orphanCount)
	}

	return sessions, projDir, nil
}

// resolveProjectDirectory determines which Claude project directory to use.
// Priority: --project-dir flag > --project/-p flag > current directory
// Uses fallback matching when exact encoded path doesn't exist.
func resolveProjectDirectory(cfg *config) (string, error) {
	// If --project-dir is set, use it directly (bypass all auto-detection)
	if cfg.projectDir != "" {
		return paths.ResolveProjectDir(cfg.projectDir)
	}

	// Get the source path (from --project or cwd)
	projPath, err := getProjectPath(cfg)
	if err != nil {
		return "", err
	}

	// Try exact match first (fast path, duplicated in paths.FindProjectDir for same reason)
	exactDir, err := paths.GetProjectDirForPath(projPath)
	if err != nil {
		return "", err
	}

	if info, statErr := os.Stat(exactDir); statErr == nil && info.IsDir() {
		return exactDir, nil
	}

	// Exact match failed - try fallback matching
	allProjects, err := parser.DiscoverAllProjects()
	if err != nil {
		return "", fmt.Errorf("discovering projects: %w", err)
	}

	match, err := paths.FindProjectDir(projPath, allProjects)
	if err != nil {
		// Enhance error messages with helpful suggestions
		if errors.Is(err, paths.ErrNoProjectFound) {
			return "", formatNoProjectError(projPath, allProjects)
		}
		return "", err
	}

	// Print match info if matched by suffix
	if match.MatchInfo != "" {
		fmt.Fprintf(cfg.stderr, "Note: %s\n", match.MatchInfo)
	}

	return match.ProjectDir, nil
}

// formatNoProjectError creates a helpful error message when no project is found
func formatNoProjectError(projPath string, allProjects []models.ProjectInfo) error {
	basename := strings.ToLower(filepath.Base(projPath))

	// Find similar projects by prefix basename match
	var suggestions []string
	const maxSuggestions = 5
	for _, proj := range allProjects {
		if proj.OriginalPath != "" {
			projBasename := strings.ToLower(filepath.Base(proj.OriginalPath))
			if strings.HasPrefix(projBasename, basename) || strings.HasPrefix(basename, projBasename) {
				suggestions = append(suggestions, fmt.Sprintf("  %s (original: %s)", proj.EncodedPath, proj.OriginalPath))
				if len(suggestions) >= maxSuggestions {
					break
				}
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("no Claude sessions found for: %s", projPath))

	if len(suggestions) > 0 {
		sb.WriteString("\n\nSimilar projects:\n")
		for _, s := range suggestions {
			sb.WriteString(s)
			sb.WriteString("\n")
		}
		sb.WriteString("\nUse --project-dir to specify the exact directory")
	}

	return errors.New(sb.String())
}

// warnSkippedLines prints a stderr warning when JSONL lines were skipped
// during parsing (malformed or oversized), so undercounted totals don't
// look authoritative. Stderr keeps -f json/csv stdout clean.
func warnSkippedLines(w io.Writer, skippedLines int) {
	if skippedLines > 0 {
		fmt.Fprintf(w, "Warning: %d unparseable line(s) skipped (malformed or oversized) — totals may be undercounted\n", skippedLines)
	}
}

// warnUnknownModels prints a warning if any models have unknown pricing
func warnUnknownModels(w io.Writer, costByModel map[string]models.CostBreakdown) {
	var unknownModels []string
	for model := range costByModel {
		if !pricing.IsKnownModel(model) {
			unknownModels = append(unknownModels, model)
		}
	}
	if len(unknownModels) > 0 {
		sort.Strings(unknownModels)
		if len(unknownModels) == 1 {
			fmt.Fprintf(w, "Warning: unknown model %q using fallback pricing\n", unknownModels[0])
		} else {
			fmt.Fprintf(w, "Warning: %d unknown models using fallback pricing: %v\n", len(unknownModels), unknownModels)
		}
	}
}

// selectSession finds the appropriate session based on CLI args.
// Returns the session, project directory, whether a session ID was explicitly provided, and any error.
func selectSession(cfg *config, args []string) (*models.SessionEntry, string, bool, error) {
	// Analysis paths (show/watch/breakdown) recompute counts from their own
	// parse, so skip the discovery-time message-count scan.
	sessions, projectDir, err := loadProjectSessionsWithDir(cfg, false)
	if err != nil {
		return nil, "", false, err
	}

	explicitSessionID := len(args) > 0
	if explicitSessionID {
		session, err := findSessionByPartialID(sessions, args[0])
		if err != nil {
			if errors.Is(err, ErrSessionNotFound) {
				return nil, "", false, fmt.Errorf("session not found: %s", args[0])
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

// findSessionByPartialID finds a session by partial ID match.
// Returns the matching session, or an error if multiple sessions match.
func findSessionByPartialID(sessions []models.SessionEntry, partialID string) (*models.SessionEntry, error) {
	if len(sessions) == 0 {
		return nil, ErrSessionNotFound
	}

	// Empty ID would match all sessions via prefix matching - reject it
	if partialID == "" {
		return nil, ErrSessionNotFound
	}

	// Try exact match first
	for i := range sessions {
		if sessions[i].SessionID == partialID {
			return &sessions[i], nil
		}
	}

	// Try prefix match - collect all matches
	var matches []models.SessionEntry
	for i := range sessions {
		if len(sessions[i].SessionID) >= len(partialID) && sessions[i].SessionID[:len(partialID)] == partialID {
			matches = append(matches, sessions[i])
		}
	}

	if len(matches) == 0 {
		return nil, ErrSessionNotFound
	}

	if len(matches) == 1 {
		return &matches[0], nil
	}

	// Multiple matches - return error with list (capped at 10)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ambiguous session ID %q matches %d sessions:\n", partialID, len(matches)))
	displayLimit := 10
	for i, m := range matches {
		if i >= displayLimit {
			sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(matches)-displayLimit))
			break
		}
		sb.WriteString(fmt.Sprintf("  %s\n", m.SessionID))
	}
	return nil, fmt.Errorf("%s", sb.String())
}
