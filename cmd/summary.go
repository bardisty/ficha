package cmd

import (
	"bytes"
	"fmt"
	"time"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/formatter"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/spf13/cobra"
)

func newSummaryCmd(cfg *config) *cobra.Command {
	summaryCmd := &cobra.Command{
		Use:   "summary",
		Short: "Show aggregate stats across all sessions",
		Long: `Show aggregate statistics across all Claude Code sessions for the current project.

This calculates the total cost and token usage across all sessions.

--details and --expand-agents also enrich json/csv output: --details adds a
per-session breakdown, --expand-agents adds each session's agent sub-sessions.

Examples:
  ficha summary                           Show aggregate stats
  ficha summary --details                 Show per-session cost breakdown
  ficha summary --details --expand-agents Include agent sub-sessions
  ficha summary --details -f json         Per-session records as JSON
  ficha summary --since 7d                The last 7 days
  ficha summary --since 2026-09-01 --until 2026-09-30   September

--since and --until count messages by their own timestamps, agents' too, so
a session that crosses a bound is split at it. They apply to json and csv
as well as the table.`,
		Args:              noArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSummary(cfg)
		},
	}

	addProjectFlags(summaryCmd, cfg)
	summaryCmd.Flags().BoolVarP(&cfg.showDetails, "details", "d", false, "Add a per-session breakdown (table rows / json sessions / csv rows)")
	summaryCmd.Flags().BoolVar(&cfg.expandAgents, "expand-agents", false, "Include per-agent records: tree rows (table), nested agents (json), agent rows (csv); requires --details")
	addWindowFlags(summaryCmd, cfg)

	return summaryCmd
}

func runSummary(cfg *config) error {
	if cfg.expandAgents && !cfg.showDetails {
		return fmt.Errorf("--expand-agents requires --details")
	}

	// Summary analyzes every session, recomputing counts, so skip the
	// discovery-time message-count scan.
	sessions, projectDir, err := loadProjectSessionsWithDir(cfg, false)
	if err != nil {
		return err
	}

	// Analyze all sessions. The per-session results feed the --details view so
	// the formatter doesn't re-parse every session.
	window, err := cfg.timeWindow(time.Now())
	if err != nil {
		return err
	}
	analysis, results, err := analyzer.AnalyzeMultipleSessionsInWindow(sessions, window)
	if err != nil {
		return fmt.Errorf("analyzing sessions: %w", err)
	}

	var warnings bytes.Buffer
	skipWarning{
		counts:   "totals",
		sessions: analysis.SkippedSessions,
		agents:   analysis.SkippedAgents,
		lines:    analysis.SkippedLines,
		details:  labelSkips("", analyzer.SkipDetails(results)),
	}.write(&warnings, cfg.verbose)
	warnEstimatedCosts(&warnings, analysis.EstimatedCostMessages)
	warnUnknownModels(&warnings, analysis.CostByModel)

	// Mark as summary (don't overwrite SessionID). SessionCount is already the
	// skip-adjusted count — AnalyzeMultipleSessions sets it where the totals
	// are summed.
	analysis.IsSummary = true
	analysis.Project = parser.ProjectDisplayName(projectDir)

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
				storageDir = projectDir
			}
			output = formatter.FormatSummaryTableWithDetails(analysis, results, storageDir, cfg.noColor, cfg.expandAgents, terminalWidth(cfg.stdout))
		} else {
			output = formatter.FormatSessionTable(analysis, cfg.noColor, terminalWidth(cfg.stdout))
		}
	}

	printReport(cfg, &warnings, output)
	return nil
}
