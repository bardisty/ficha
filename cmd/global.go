package cmd

import (
	"fmt"
	"os"

	"github.com/bardisty/ccusage/internal/analyzer"
	"github.com/bardisty/ccusage/internal/formatter"
	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/parser"
	"github.com/spf13/cobra"
)

var (
	globalDetails bool
	globalTopN    int
	globalSortBy  string
	globalNoCache bool
)

var globalCmd = &cobra.Command{
	Use:   "global",
	Short: "Show aggregated stats across ALL projects",
	Long: `Show aggregate statistics across all Claude Code projects.

This calculates total cost and token usage across every project in ~/.claude/projects/.

Examples:
  ccusage global                   Show global stats (top 10 projects)
  ccusage global --details         Show all projects with cumulative column
  ccusage global --top 20          Show top 20 projects
  ccusage global --sort-by name    Sort by project name
  ccusage global -f json           Output as JSON`,
	Run: runGlobal,
}

func init() {
	globalCmd.Flags().BoolVarP(&globalDetails, "details", "d", false, "Show all projects with cumulative column")
	globalCmd.Flags().IntVarP(&globalTopN, "top", "n", 10, "Number of top projects to show")
	globalCmd.Flags().StringVar(&globalSortBy, "sort-by", "cost", "Sort by: cost, sessions, name, activity")
	globalCmd.Flags().BoolVar(&globalNoCache, "no-cache", false, "Skip cache, force fresh analysis (reserved for future use)")

	rootCmd.AddCommand(globalCmd)
}

func runGlobal(cmd *cobra.Command, args []string) {
	// Discover all projects
	projects, err := parser.DiscoverAllProjects()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error discovering projects: %v\n", err)
		os.Exit(1)
	}

	if len(projects) == 0 {
		fmt.Fprintln(os.Stderr, "No Claude projects found in ~/.claude/projects/")
		os.Exit(1)
	}

	// Filter to projects that have sessions
	var projectsWithSessions []models.ProjectInfo
	for _, p := range projects {
		if parser.HasSessions(p.FullPath) {
			projectsWithSessions = append(projectsWithSessions, p)
		}
	}

	if len(projectsWithSessions) == 0 {
		fmt.Fprintln(os.Stderr, "No Claude projects with sessions found")
		os.Exit(1)
	}

	// Analyze all projects
	analysis, err := analyzer.AnalyzeAllProjects(projectsWithSessions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analyzing projects: %v\n", err)
		os.Exit(1)
	}

	// Warn about skipped projects
	if analysis.SkippedProjects > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d project(s) could not be analyzed\n", analysis.SkippedProjects)
	}

	// Apply custom sort if requested
	if globalSortBy != "cost" {
		analyzer.SortProjectsBy(analysis.Projects, globalSortBy)
	}

	// Output in requested format
	var output string
	switch format {
	case "json":
		output, err = formatter.FormatGlobalJSON(analysis, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting output: %v\n", err)
			os.Exit(1)
		}
	case "csv":
		output, err = formatter.FormatGlobalCSV(analysis)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting output: %v\n", err)
			os.Exit(1)
		}
	default:
		output = formatter.FormatGlobalTable(analysis, noColor, globalTopN, globalDetails)
	}

	fmt.Println(output)
}
