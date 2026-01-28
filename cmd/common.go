package cmd

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/parser"
	"github.com/bardisty/ccusage/internal/paths"
	"github.com/bardisty/ccusage/internal/pricing"
)

// ErrNoSessions is returned when no sessions are found
var ErrNoSessions = errors.New("no sessions found")

// loadProjectSessions loads all sessions for the current project.
// It handles project path resolution, disk scanning, index loading, and source merging.
// Returns the merged sessions list or an error.
func loadProjectSessions() ([]models.SessionEntry, error) {
	sessions, _, err := loadProjectSessionsWithDir()
	return sessions, err
}

// loadProjectSessionsWithDir loads all sessions and returns the project directory path.
// Used by live-view commands that need to watch the project directory for new sessions.
func loadProjectSessionsWithDir() ([]models.SessionEntry, string, error) {
	// Get project path
	projPath, err := getProjectPath()
	if err != nil {
		return nil, "", err
	}

	// Get Claude project directory
	projectDir, err := paths.GetProjectDirForPath(projPath)
	if err != nil {
		return nil, "", err
	}

	// Scan disk for session files
	diskSessions, err := parser.DiscoverSessionsFromDisk(projectDir)
	if err != nil {
		return nil, "", fmt.Errorf("scanning sessions in %s: %w", projectDir, err)
	}

	// Try to load index (may fail or be incomplete)
	indexPath := paths.GetSessionsIndexPath(projectDir)
	index, indexErr := parser.ParseSessionsIndex(indexPath)
	if indexErr != nil && !os.IsNotExist(indexErr) {
		// Index exists but is malformed - warn but continue
		fmt.Fprintf(os.Stderr, "Warning: failed to parse sessions-index.json: %v\n", indexErr)
	}

	// Merge sources
	sessions, orphanCount := parser.MergeSessionSources(index, diskSessions)

	if len(sessions) == 0 {
		return nil, "", ErrNoSessions
	}

	// Warn about orphans (only in verbose mode - this is common and usually not actionable)
	if orphanCount > 0 && verbose {
		fmt.Fprintf(os.Stderr, "Note: Found %d session(s) not in sessions-index.json\n", orphanCount)
	}

	return sessions, projectDir, nil
}

// warnUnknownModels prints a warning if any models in the analysis have unknown pricing
func warnUnknownModels(analysis *models.SessionAnalysis) {
	var unknownModels []string
	for model := range analysis.CostByModel {
		if !pricing.IsKnownModel(model) {
			unknownModels = append(unknownModels, model)
		}
	}
	if len(unknownModels) > 0 {
		if len(unknownModels) == 1 {
			fmt.Fprintf(os.Stderr, "Warning: unknown model %q using fallback pricing\n", unknownModels[0])
		} else {
			fmt.Fprintf(os.Stderr, "Warning: %d unknown models using fallback pricing: %v\n", len(unknownModels), unknownModels)
		}
	}
}

// selectSession finds the appropriate session based on CLI args.
// Returns the session, project directory, whether a session ID was explicitly provided, and any error.
func selectSession(args []string) (*models.SessionEntry, string, bool, error) {
	sessions, projectDir, err := loadProjectSessionsWithDir()
	if err != nil {
		return nil, "", false, err
	}

	explicitSessionID := len(args) > 0
	if explicitSessionID {
		session, err := findSessionByPartialID(sessions, args[0])
		if err != nil {
			return nil, "", false, err
		}
		if session == nil {
			return nil, "", false, fmt.Errorf("session not found: %s", args[0])
		}
		return session, projectDir, true, nil
	}

	// Get latest session (sort by modified time)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Modified.After(sessions[j].Modified)
	})
	// Note: len(sessions) == 0 case is already handled by loadProjectSessionsWithDir returning ErrNoSessions
	return &sessions[0], projectDir, false, nil
}

// findSessionByPartialID finds a session by partial ID match.
// Returns the matching session, or an error if multiple sessions match.
func findSessionByPartialID(sessions []models.SessionEntry, partialID string) (*models.SessionEntry, error) {
	if len(sessions) == 0 {
		return nil, nil
	}

	// Empty ID would match all sessions via prefix matching - reject it
	if partialID == "" {
		return nil, nil
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
		return nil, nil
	}

	if len(matches) == 1 {
		return &matches[0], nil
	}

	// Multiple matches - return error with list
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ambiguous session ID %q matches %d sessions:\n", partialID, len(matches)))
	for _, m := range matches {
		sb.WriteString(fmt.Sprintf("  %s\n", m.SessionID))
	}
	return nil, fmt.Errorf("%s", sb.String())
}
