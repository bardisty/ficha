package cmd

import (
	"fmt"

	"github.com/bardisty/ccusage/internal/analyzer"
	"github.com/bardisty/ccusage/internal/formatter"
	"github.com/spf13/cobra"
)

func newSummaryCmd(cfg *config) *cobra.Command {
	summaryCmd := &cobra.Command{
		Use:   "summary",
		Short: "Show aggregate stats across all sessions",
		Long: `Show aggregate statistics across all Claude Code sessions for the current project.

This calculates the total cost and token usage across all sessions.

Examples:
  ccusage summary                          Show aggregate stats
  ccusage summary --details                Show per-session cost breakdown
  ccusage summary --details --expand-agents Show agent sub-sessions in tree view
  ccusage summary -f json                  Output as JSON`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSummary(cfg)
		},
	}

	summaryCmd.Flags().BoolVarP(&cfg.showDetails, "details", "d", false, "Show per-session cost breakdown")
	summaryCmd.Flags().BoolVar(&cfg.expandAgents, "expand-agents", false, "Show agent sub-sessions as indented tree rows (requires --details)")

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
	if analysis.SkippedSessions > 0 {
		fmt.Fprintf(cfg.stderr, "Warning: %d session(s) could not be parsed\n", analysis.SkippedSessions)
	}
	if analysis.SkippedAgents > 0 {
		fmt.Fprintf(cfg.stderr, "Warning: %d agent sub-session(s) could not be parsed\n", analysis.SkippedAgents)
	}
	warnSkippedLines(cfg.stderr, analysis.SkippedLines)

	// Warn about unknown models (using fallback pricing)
	warnUnknownModels(cfg.stderr, analysis.CostByModel)

	// Mark as summary with session count (don't overwrite SessionID)
	analysis.IsSummary = true
	analysis.SessionCount = len(sessions)

	// Output in requested format
	var output string
	switch cfg.format {
	case "json":
		output, err = formatter.FormatSessionJSON(analysis, true)
		if err != nil {
			return fmt.Errorf("formatting output: %w", err)
		}
	case "csv":
		output, err = formatter.FormatSessionCSV(analysis, false)
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
