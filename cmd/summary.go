package cmd

import (
	"fmt"
	"os"

	"github.com/bardisty/ccusage/internal/analyzer"
	"github.com/bardisty/ccusage/internal/formatter"
	"github.com/bardisty/ccusage/internal/parser"
	"github.com/bardisty/ccusage/internal/paths"
	"github.com/spf13/cobra"
)

var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Show aggregate stats across all sessions",
	Long: `Show aggregate statistics across all Claude Code sessions for the current project.

This calculates the total cost and token usage across all sessions.

Examples:
  ccusage summary                 Show aggregate stats
  ccusage summary -f json         Output as JSON`,
	Run: runSummary,
}

func runSummary(cmd *cobra.Command, args []string) {
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

	// Analyze all sessions
	analysis, err := analyzer.AnalyzeMultipleSessions(sessions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analyzing sessions: %v\n", err)
		os.Exit(1)
	}

	// Update session ID to indicate it's a summary
	analysis.SessionID = fmt.Sprintf("Summary (%d sessions)", len(sessions))

	// Output in requested format
	var output string
	switch format {
	case "json":
		output, err = formatter.FormatSessionJSON(analysis, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting output: %v\n", err)
			os.Exit(1)
		}
	case "csv":
		output = formatter.FormatSessionCSV(analysis, false)
	default:
		output = formatter.FormatSessionTable(analysis, noColor)
	}

	fmt.Println(output)
}
