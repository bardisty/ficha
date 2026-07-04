package cmd

import (
	"fmt"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/formatter"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/spf13/cobra"
)

func newGlobalCmd(cfg *config) *cobra.Command {
	globalCmd := &cobra.Command{
		Use:   "global",
		Short: "Show aggregated stats across ALL projects",
		Long: `Show aggregate statistics across all Claude Code projects.

This calculates total cost and token usage across every project in ~/.claude/projects/.

json/csv always export every project; --top and --details only shape the table
(filter downstream with jq/head if you need a subset).

Examples:
  ficha global                   Show global stats (top 10 projects)
  ficha global --details         Show all projects with cumulative column
  ficha global --top 20          Show top 20 projects
  ficha global --sort-by name    Sort by project name
  ficha global -f json           Output every project as JSON`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGlobal(cfg)
		},
	}

	globalCmd.Flags().BoolVarP(&cfg.globalDetails, "details", "d", false, "Show all projects with a cumulative column (table view only)")
	globalCmd.Flags().IntVarP(&cfg.globalTopN, "top", "n", 10, "Top projects to show in the table (json/csv always export all)")
	globalCmd.Flags().StringVar(&cfg.globalSortBy, "sort-by", "cost", "Sort by: cost, sessions, name, activity")
	globalCmd.Flags().BoolVar(&cfg.globalNoCache, "no-cache", false, "Skip cache, force fresh analysis (reserved for future use)")
	_ = globalCmd.Flags().MarkHidden("no-cache")

	return globalCmd
}

func runGlobal(cfg *config) error {
	// Validate --sort-by
	validSortValues := map[string]bool{"cost": true, "sessions": true, "name": true, "activity": true}
	if !validSortValues[cfg.globalSortBy] {
		return fmt.Errorf("invalid --sort-by value %q: must be one of cost, sessions, name, activity", cfg.globalSortBy)
	}

	// Validate --top (0 = show no project rows, just the summary)
	if cfg.globalTopN < 0 {
		return fmt.Errorf("invalid --top value %d: must be >= 0", cfg.globalTopN)
	}

	// Global command shows all projects — reject project-specific flags
	if cfg.projectPath != "" || cfg.projectDir != "" {
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
		fmt.Fprintf(cfg.stderr, "Warning: %d project(s) could not be analyzed\n", analysis.SkippedProjects)
	}

	// Warn about unknown models (using fallback pricing)
	warnUnknownModels(cfg.stderr, analysis.CostByModel)

	// Apply custom sort if requested
	if cfg.globalSortBy != "cost" {
		analyzer.SortProjectsBy(analysis.Projects, cfg.globalSortBy)
	}

	// Output in requested format
	var output string
	switch cfg.format {
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
		output = formatter.FormatGlobalTable(analysis, cfg.noColor, cfg.globalTopN, cfg.globalDetails)
	}

	fmt.Fprintln(cfg.stdout, output)
	return nil
}
