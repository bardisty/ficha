package cmd

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/formatter"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
	"github.com/spf13/cobra"
)

func newGlobalCmd(cfg *config) *cobra.Command {
	globalCmd := &cobra.Command{
		Use:   "global",
		Short: "Show aggregated stats across ALL projects",
		Long: `Show aggregate statistics across all Claude Code projects.

This calculates total cost and token usage across every project in
~/.claude/projects, or $CLAUDE_CONFIG_DIR/projects when that's set.

json/csv always export every project; --top and --details only shape the table
(filter downstream with jq/head if you need a subset).

Examples:
  ficha global                   Show global stats (top 10 projects)
  ficha global --details         Show all projects, with a cumulative cost column
  ficha global --top 20          Show top 20 projects
  ficha global --sort-by name    Sort by project name
  ficha global -f json           Output every project as JSON
  ficha global --since 7d        Every project, the last 7 days

--since and --until count messages by their own timestamps, so a session
that crosses a bound is split at it. They apply to json and csv as well as
the table.`,
		Args:              noArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGlobal(cfg)
		},
	}

	globalCmd.Flags().BoolVarP(&cfg.globalDetails, "details", "d", false, "Show all projects, plus a cumulative column when sorted by cost (table view only)")
	globalCmd.Flags().IntVarP(&cfg.globalTopN, "top", "n", 10, "Top projects to show in the table (json/csv always export all)")
	globalCmd.Flags().StringVar(&cfg.globalSortBy, "sort-by", "cost", "Sort by: cost, sessions, name, activity")
	globalCmd.Flags().BoolVar(&cfg.globalNoCache, "no-cache", false, "Skip cache, force fresh analysis (reserved for future use)")
	_ = globalCmd.Flags().MarkHidden("no-cache")
	_ = globalCmd.RegisterFlagCompletionFunc("sort-by", fixedValues(sortByValues))
	addWindowFlags(globalCmd, cfg)

	return globalCmd
}

func runGlobal(cfg *config) error {
	// Validate --sort-by
	validSortValues := map[string]bool{"cost": true, "sessions": true, "name": true, "activity": true}
	if !validSortValues[strings.ToLower(cfg.globalSortBy)] {
		return usageErrorf("invalid --sort-by value %q: must be one of cost, sessions, name, activity", cfg.globalSortBy)
	}
	cfg.globalSortBy = strings.ToLower(cfg.globalSortBy)

	// Validate --top (0 = show no project rows, just the summary)
	if cfg.globalTopN < 0 {
		return usageErrorf("invalid --top value %d: must be >= 0", cfg.globalTopN)
	}
	window, err := cfg.timeWindow(time.Now())
	if err != nil {
		return err
	}

	// Discover all projects
	if projectsDir, err := paths.GetProjectsDir(); err == nil {
		cfg.tracef("projects dir %s (%s)", projectsDir, projectsDirOrigin())
	}
	projects, err := parser.DiscoverAllProjects()
	if err != nil {
		return fmt.Errorf("discovering projects: %w", err)
	}
	cfg.tracef("%s", plural(len(projects), "project"))

	if len(projects) == 0 {
		projectsDir, err := paths.GetProjectsDir()
		if err != nil {
			return err
		}
		return noDataError(projectsDir)
	}
	pickPalette(cfg)

	// Analyze all projects (analyzeProject handles empty-session projects internally)
	analysis, err := analyzer.AnalyzeAllProjectsInWindow(projects, window)
	if err != nil {
		return fmt.Errorf("analyzing projects: %w", err)
	}

	// Warn about every input the totals could not account for
	var warnings bytes.Buffer
	if analysis.SkippedProjects > 0 {
		fmt.Fprintf(&warnings, "Warning: %d project(s) could not be analyzed\n", analysis.SkippedProjects)
	}
	var details []namedSkip
	for _, p := range slices.Concat(analysis.Projects, analysis.OutOfWindow) {
		details = append(details, labelSkips(projectLabel(p.ProjectInfo), p.SkipDetails)...)
	}
	skipWarning{
		counts:   "totals",
		sessions: analysis.SkippedSessions,
		agents:   analysis.SkippedAgents,
		lines:    analysis.SkippedLines,
		details:  details,
	}.write(&warnings, cfg.verbose)
	warnEstimatedCosts(&warnings, analysis.EstimatedCostMessages)
	analysis.UnpricedModels = unpricedModels(analysis.CostByModel)
	for i := range analysis.Projects {
		analysis.Projects[i].UnpricedModels = unpricedModels(analysis.Projects[i].CostByModel)
	}
	warnUnknownModels(&warnings, analysis.UnpricedModels)

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
		output = formatter.FormatGlobalTable(analysis, cfg.noColor, formatter.GlobalTableOptions{
			TopN:    cfg.globalTopN,
			Details: cfg.globalDetails,
			SortBy:  cfg.globalSortBy,
			Width:   terminalWidth(cfg.stdout),
		})
	}

	printReport(cfg, &warnings, output)
	return nil
}
