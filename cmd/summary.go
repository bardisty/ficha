package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"time"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/formatter"
	"github.com/spf13/cobra"
)

func newSummaryCmd(cfg *config) *cobra.Command {
	summaryCmd := &cobra.Command{
		Use:   "summary",
		Short: "Show totals across this project's sessions",
		Long: `Show aggregate statistics across all Claude Code sessions for this project.

This calculates the total cost and token usage across all sessions.

--details adds a per-session breakdown: rows in the table and in csv, a
sessions array in json. --expand-agents, which needs --details, adds each
session's agents: tree rows in the table, after a row for the main
conversation's cost, nested agents in json, agent rows in csv.

Examples:
  ficha summary                           Show aggregate stats
  ficha summary --details                 Show per-session cost breakdown
  ficha summary --details --expand-agents Include agent sub-sessions
  ficha summary --details -f json         Per-session records as JSON
  ficha summary --since 7d                The last 7 days
  ficha summary --since 2026-09-01 --until 2026-09-30   September
  ficha summary --nested                  Include the repo's worktrees

` + windowForms + `

Both count messages by their own timestamps, agents' too, so a session that
crosses a bound is split at it. They apply to json and csv as well as the
table.

Claude Code records a session under the directory it ran in, so a worktree
under the repo, from claude --worktree or git worktree add, is a project of
its own. summary notes when projects under this directory have sessions,
and --nested counts them in. --details then names each session's project.`,
		Args:              noArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSummary(cfg)
		},
	}

	addFormatFlag(summaryCmd, cfg, false)
	addProjectFlags(summaryCmd, cfg)
	summaryCmd.Flags().BoolVarP(&cfg.showDetails, "details", "d", false, "Add a per-session breakdown")
	summaryCmd.Flags().BoolVar(&cfg.expandAgents, "expand-agents", false, "Add each session's agents; needs --details")
	addWindowFlags(summaryCmd, cfg)
	addNestedFlag(summaryCmd, cfg)

	return summaryCmd
}

func runSummary(cfg *config) error {
	if cfg.expandAgents && !cfg.showDetails {
		return usageErrorf("--expand-agents requires --details")
	}
	if err := checkNested(cfg); err != nil {
		return err
	}
	window, err := cfg.timeWindow(time.Now())
	if err != nil {
		return err
	}

	sessions, root, nested, err := loadProjectAndNestedSessions(cfg)
	if err != nil {
		return err
	}
	if cfg.nested {
		sessions = append(sessions, nested.sessions...)
	}
	pickPalette(cfg)

	// Analyze all sessions. The per-session results feed the --details view so
	// the formatter doesn't re-parse every session.
	analysis, results, err := analyzer.AnalyzeMultipleSessionsInWindow(sessions, window)
	if err != nil {
		return fmt.Errorf("analyzing sessions: %w", err)
	}
	nested.mark(results)

	var warnings bytes.Buffer
	skipWarning{
		counts:   "totals",
		sessions: analysis.SkippedSessions,
		agents:   analysis.SkippedAgents,
		lines:    analysis.SkippedLines,
		details:  labelSkips("", analysis.SkipDetails),
	}.write(&warnings, cfg.verbose)
	warnEstimatedCosts(&warnings, analysis.EstimatedCostMessages)
	analysis.UnpricedModels = unpricedModels(analysis.CostByModel)
	for _, r := range results {
		if r.Analysis != nil {
			markUnpriced(r.Analysis)
			r.Analysis.Context = sessionContext(r.Analysis)
		}
	}
	warnUnknownModels(&warnings, analysis.UnpricedModels)
	if !cfg.nested {
		writeNestedNote(&warnings, cfg, nested, window, root.name)
	}

	// Mark as summary (don't overwrite SessionID). SessionCount is already the
	// skip-adjusted count — AnalyzeMultipleSessions sets it where the totals
	// are summed.
	analysis.IsSummary = true
	analysis.Project = nestedLabel(root.name, results)
	if cfg.nested {
		// The directory's own, though the first session read may be a
		// nested project's, and though it may have no project of its own.
		analysis.ProjectPath = filepath.Clean(root.dir)
	}

	// Output in requested format. --details/--expand-agents add per-session and
	// per-agent records to json/csv (not just the table); without --details the
	// machine formats emit the aggregate alone.
	var output string
	switch cfg.format {
	case "json":
		if cfg.showDetails {
			output, err = formatter.FormatSummaryDetailJSON(analysis, results, cfg.expandAgents, true)
		} else {
			output, err = formatter.FormatSessionJSON(analysis, true)
		}
		if err != nil {
			return fmt.Errorf("formatting output: %w", err)
		}
	case "csv":
		if cfg.showDetails {
			output, err = formatter.FormatSummaryDetailCSV(results, cfg.expandAgents)
		} else {
			output, err = formatter.FormatSessionCSV(analysis, false)
		}
		if err != nil {
			return fmt.Errorf("formatting output: %w", err)
		}
	default:
		if cfg.showDetails {
			// The header names the project; the storage directory is only
			// for debugging.
			storageDir := ""
			if cfg.verbose {
				storageDir = root.dir
			}
			output = formatter.FormatSummaryTableWithDetails(analysis, results, storageDir, cfg.noColor, cfg.expandAgents, terminalWidth(cfg.stdout))
		} else {
			output = formatter.FormatSessionTable(analysis, cfg.noColor, terminalWidth(cfg.stdout))
		}
	}

	printReport(cfg, &warnings, output)
	return nil
}
