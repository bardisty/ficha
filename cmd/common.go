package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/parser"
	"github.com/bardisty/ccusage/internal/paths"
)

// ErrNoSessions is returned when no sessions are found
var ErrNoSessions = errors.New("no sessions found")

// loadProjectSessions loads all sessions for the current project.
// It handles project path resolution, disk scanning, index loading, and source merging.
// Returns the merged sessions list or an error.
func loadProjectSessions() ([]models.SessionEntry, error) {
	// Get project path
	projPath, err := getProjectPath()
	if err != nil {
		return nil, err
	}

	// Get Claude project directory
	projectDir, err := paths.GetProjectDirForPath(projPath)
	if err != nil {
		return nil, err
	}

	// Scan disk for session files
	diskSessions, err := parser.DiscoverSessionsFromDisk(projectDir)
	if err != nil {
		return nil, fmt.Errorf("scanning sessions in %s: %w", projectDir, err)
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
		return nil, ErrNoSessions
	}

	// Warn about orphans
	if orphanCount > 0 {
		fmt.Fprintf(os.Stderr, "Note: Found %d session(s) not in sessions-index.json\n", orphanCount)
	}

	return sessions, nil
}
