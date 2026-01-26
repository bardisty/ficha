package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/bardisty/ccusage/internal/formatter"
	"github.com/bardisty/ccusage/internal/models"
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
	sessions, err := loadProjectSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
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
		output, err = formatter.FormatSessionListCSV(sessions)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting output: %v\n", err)
			os.Exit(1)
		}
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
