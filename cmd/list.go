package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/bah/ccusage/internal/formatter"
	"github.com/bah/ccusage/internal/models"
	"github.com/bah/ccusage/internal/parser"
	"github.com/bah/ccusage/internal/paths"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available sessions",
	Long: `List all Claude Code sessions for the current project.

Sessions are sorted by modification time (most recent first).

Examples:
  ccusage list                    List all sessions
  ccusage list -f json            Output as JSON
  ccusage list -f csv             Output as CSV`,
	Run: runList,
}

func runList(cmd *cobra.Command, args []string) {
	// Get project path
	projPath, err := getProjectPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Get Claude project directory
	projectDir, err := paths.GetProjectDirForPath(projPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Scan disk for session files
	diskSessions, err := parser.DiscoverSessionsFromDisk(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scanning sessions: %v\n", err)
		fmt.Fprintf(os.Stderr, "Project directory: %s\n", projectDir)
		os.Exit(1)
	}

	// Try to load index (may fail or be incomplete)
	indexPath := paths.GetSessionsIndexPath(projectDir)
	index, _ := parser.ParseSessionsIndex(indexPath) // Ignore error

	// Merge sources
	sessions, orphanCount := parser.MergeSessionSources(index, diskSessions)

	if len(sessions) == 0 {
		fmt.Println("No sessions found")
		os.Exit(1)
	}

	// Warn about orphans
	if orphanCount > 0 {
		fmt.Fprintf(os.Stderr, "Note: Found %d session(s) not in sessions-index.json\n", orphanCount)
	}

	// Sort sessions by modified time (most recent first)
	sortSessionsByModified(sessions)

	// Output in requested format
	var output string
	switch format {
	case "json":
		output, err = formatter.FormatSessionListJSON(sessions, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting output: %v\n", err)
			os.Exit(1)
		}
	case "csv":
		output = formatter.FormatSessionListCSV(sessions)
	default:
		output = formatter.FormatSessionListTable(sessions, noColor)
	}

	fmt.Println(output)
}

// sortSessionsByModified sorts sessions by modified time (most recent first)
func sortSessionsByModified(sessions []models.SessionEntry) {
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Modified.After(sessions[j].Modified)
	})
}
