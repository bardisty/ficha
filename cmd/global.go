package cmd

import (
	"fmt"
	"os"

	"github.com/bardisty/ccusage/internal/analyzer"
	"github.com/bardisty/ccusage/internal/formatter"
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
	Args: cobra.NoArgs,
	RunE: runGlobal,
}

func init() {
	globalCmd.Flags().BoolVarP(&globalDetails, "details", "d", false, "Show all projects with cumulative column")
	globalCmd.Flags().IntVarP(&globalTopN, "top", "n", 10, "Number of top projects to show")
	globalCmd.Flags().StringVar(&globalSortBy, "sort-by", "cost", "Sort by: cost, sessions, name, activity")
	globalCmd.Flags().BoolVar(&globalNoCache, "no-cache", false, "Skip cache, force fresh analysis (reserved for future use)")
	_ = globalCmd.Flags().MarkHidden("no-cache")

	rootCmd.AddCommand(globalCmd)
}

func runGlobal(cmd *cobra.Command, args []string) error {
	// Validate --sort-by
	validSortValues := map[string]bool{"cost": true, "sessions": true, "name": true, "activity": true}
	if !validSortValues[globalSortBy] {
		return fmt.Errorf("invalid --sort-by value %q: must be one of cost, sessions, name, activity", globalSortBy)
	}

	// Validate --top (0 = show no project rows, just the summary)
	if globalTopN < 0 {
		return fmt.Errorf("invalid --top value %d: must be >= 0", globalTopN)
	}

	// Global command shows all projects — reject project-specific flags
	if projectPath != "" || projectDir != "" {
		return fmt.Errorf("--project and --project-dir flags are not supported with the global command")
	}

	// Discover all projects
	projects, err := parser.DiscoverAllProjects()
	if err != nil {
		return fmt.Errorf("discovering projects: %w", err)
	}

	if len(projects) == 0 {
		return fmt.Errorf("no Claude projects found in ~/.claude/projects/")
	}

	// Analyze all projects (analyzeProject handles empty-session projects internally)
	analysis, err := analyzer.AnalyzeAllProjects(projects)
	if err != nil {
		return fmt.Errorf("analyzing projects: %w", err)
	}

	// Warn about skipped projects
	if analysis.SkippedProjects > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d project(s) could not be analyzed\n", analysis.SkippedProjects)
	}

	// Warn about unknown models (using fallback pricing)
	warnUnknownModels(analysis.CostByModel)

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
			return fmt.Errorf("formatting output: %w", err)
		}
	case "csv":
		output, err = formatter.FormatGlobalCSV(analysis)
		if err != nil {
			return fmt.Errorf("formatting output: %w", err)
		}
	default:
		output = formatter.FormatGlobalTable(analysis, noColor, globalTopN, globalDetails)
	}

	fmt.Println(output)
	return nil
}
