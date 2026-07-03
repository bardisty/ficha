package cmd

import (
	"fmt"

	"github.com/bardisty/ccusage/internal/analyzer"
	"github.com/bardisty/ccusage/internal/formatter"
	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func newShowCmd(cfg *config) *cobra.Command {
	showCmd := &cobra.Command{
		Use:   "show [session-id]",
		Short: "Show session cost breakdown",
		Long: `Show cost breakdown for a Claude Code session.

If no session ID is provided, shows the most recent session.

Examples:
  ccusage show                    Show latest session
  ccusage show abc123             Show specific session
  ccusage show --live             Watch latest session in real-time
  ccusage show -f json            Output as JSON
  ccusage show -f csv --messages  Per-message rows as CSV`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(cfg, args, cfg.live)
		},
	}

	showCmd.Flags().BoolVar(&cfg.messages, "messages", false, "Output per-message rows/records instead of the session summary (json/csv only)")

	return showCmd
}

// runShow renders (or, when live, watches) a single session. live is passed
// explicitly rather than read from cfg so watch can force it on without a
// shared mutation: `show`/bare root pass cfg.live, `watch` passes true.
func runShow(cfg *config, args []string, live bool) error {
	session, projectDir, explicitSessionID, err := selectSession(cfg, args)
	if err != nil {
		return err
	}

	// Live mode
	if live {
		// Auto-follow is enabled by default unless:
		// - User specified a session ID explicitly (pinned to that session)
		// - User passed --no-follow flag
		followMode := !explicitSessionID && !cfg.noFollow
		return runLiveMode(cfg, session, projectDir, followMode)
	}

	// Analyze the session. Per-message data is retained only for --messages,
	// which switches json/csv to per-message granularity; the table never renders
	// the message list (insights are computed regardless), so it ignores the flag.
	includeMessages := cfg.messages
	analysis, err := analyzer.AnalyzeSession(session.FullPath, session.SessionID, includeMessages)
	if err != nil {
		return fmt.Errorf("analyzing session: %w", err)
	}

	// Warn about skipped agents and skipped lines
	if analysis.SkippedAgents > 0 {
		fmt.Fprintf(cfg.stderr, "Warning: %d agent sub-session(s) could not be parsed\n", analysis.SkippedAgents)
	}
	warnSkippedLines(cfg.stderr, analysis.SkippedLines)

	// Warn about unknown models (using fallback pricing)
	warnUnknownModels(cfg.stderr, analysis.CostByModel)

	// Output in requested format
	output, err := formatOutput(cfg, analysis, includeMessages)
	if err != nil {
		return fmt.Errorf("formatting output: %w", err)
	}

	fmt.Fprintln(cfg.stdout, output)
	return nil
}

func runLiveMode(cfg *config, session *models.SessionEntry, projectDir string, followMode bool) error {
	model := tui.NewModel(session.FullPath, session.SessionID, cfg.verbose, cfg.noColor, projectDir, followMode)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}
	return nil
}

func formatOutput(cfg *config, analysis *models.SessionAnalysis, includeMessages bool) (string, error) {
	switch cfg.format {
	case "json":
		return formatter.FormatSessionJSON(analysis, true)
	case "csv":
		return formatter.FormatSessionCSV(analysis, includeMessages)
	default:
		return formatter.FormatSessionTable(analysis, cfg.noColor), nil
	}
}
