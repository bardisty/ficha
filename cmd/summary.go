package cmd

import (
	"fmt"
	"os"

	"github.com/bardisty/ccusage/internal/analyzer"
	"github.com/bardisty/ccusage/internal/formatter"
	"github.com/spf13/cobra"
)

var showDetails bool

var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Show aggregate stats across all sessions",
	Long: `Show aggregate statistics across all Claude Code sessions for the current project.

This calculates the total cost and token usage across all sessions.

Examples:
  ccusage summary                 Show aggregate stats
  ccusage summary --details       Show per-session cost breakdown
  ccusage summary -f json         Output as JSON`,
	Run: runSummary,
}

func init() {
	summaryCmd.Flags().BoolVarP(&showDetails, "details", "d", false, "Show per-session cost breakdown")
}

func runSummary(cmd *cobra.Command, args []string) {
	sessions, projectDir, err := loadProjectSessionsWithDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Analyze all sessions
	analysis, err := analyzer.AnalyzeMultipleSessions(sessions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analyzing sessions: %v\n", err)
		os.Exit(1)
	}

	// Warn about skipped sessions/agents
	if analysis.SkippedSessions > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d session(s) could not be parsed\n", analysis.SkippedSessions)
	}
	if analysis.SkippedAgents > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d agent sub-session(s) could not be parsed\n", analysis.SkippedAgents)
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
		output, err = formatter.FormatSessionCSV(analysis, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting output: %v\n", err)
			os.Exit(1)
		}
	default:
		if showDetails {
			output = formatter.FormatSummaryTableWithDetails(analysis, sessions, projectDir, noColor)
		} else {
			output = formatter.FormatSessionTable(analysis, noColor)
		}
	}

	fmt.Println(output)
}
