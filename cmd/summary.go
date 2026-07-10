package cmd

import (
	"fmt"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/formatter"
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
  ficha summary --details -f json         Per-session records as JSON`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSummary(cfg)
		},
	}

	summaryCmd.Flags().BoolVarP(&cfg.showDetails, "details", "d", false, "Add a per-session breakdown (table rows / json sessions / csv rows)")
	summaryCmd.Flags().BoolVar(&cfg.expandAgents, "expand-agents", false, "Include per-agent records: tree rows (table), nested agents (json), agent rows (csv); requires --details")

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
	analysis, results, err := analyzer.AnalyzeMultipleSessions(sessions)
	if err != nil {
		return fmt.Errorf("analyzing sessions: %w", err)
	}

	// Warn about skipped sessions/agents
	warnSkippedSessions(cfg.stderr, analysis.SkippedSessions)
	warnSkippedAgents(cfg.stderr, analysis.SkippedAgents)
	warnSkippedLines(cfg.stderr, analysis.SkippedLines)
	warnEstimatedCosts(cfg.stderr, analysis.EstimatedCostMessages)

	// Warn about unknown models (using fallback pricing)
	warnUnknownModels(cfg.stderr, analysis.CostByModel)

	// Mark as summary with session count (don't overwrite SessionID)
	analysis.IsSummary = true
	analysis.SessionCount = len(sessions)

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
			output = formatter.FormatSummaryTableWithDetails(analysis, results, projectDir, cfg.noColor, cfg.expandAgents)
		} else {
			output = formatter.FormatSessionTable(analysis, cfg.noColor)
		}
	}

	fmt.Fprintln(cfg.stdout, output)
	return nil
}
