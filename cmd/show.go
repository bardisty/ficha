package cmd

import (
	"bytes"
	"fmt"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/formatter"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/tui"
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
  ficha show                    Show latest session
  ficha show abc123             Show specific session
  ficha show --live             Watch latest session in real-time
  ficha show -f json            Output as JSON
  ficha show -f csv --messages  Per-message rows as CSV`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeSessionIDs(cfg),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(cfg, args, cfg.live)
		},
	}

	addProjectFlags(showCmd, cfg)
	addLiveFlags(showCmd, cfg, false)
	showCmd.Flags().BoolVar(&cfg.messages, "messages", false, "Output per-message rows/records instead of the session summary, agent sub-sessions included and tagged with agent_id (json/csv only)")

	return showCmd
}

// runShow renders (or, when live, watches) a single session. live is passed
// explicitly rather than read from cfg so watch can force it on without a
// shared mutation: `show`/bare root pass cfg.live, `watch` passes true.
func runShow(cfg *config, args []string, live bool) error {
	switch {
	case cfg.messages && live:
		fmt.Fprintln(cfg.stderr, "Warning: --messages has no effect in live mode")
	case cfg.messages && cfg.format == "table":
		fmt.Fprintln(cfg.stderr, "Warning: --messages has no effect on table output (use -f json or -f csv)")
	}

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
		if err := requireTerminal(cfg.stdout, cfg.commandPath, "For scripting, use 'ficha show -f json' (add --messages for per-message rows)."); err != nil {
			return err
		}
		return runLiveMode(cfg, session, projectDir, followMode)
	}

	// Analyze the session. Per-message data is retained only for --messages,
	// which switches json/csv to per-message granularity; the table never renders
	// the message list (insights are computed regardless), so it ignores the flag.
	includeMessages := cfg.messages
	scope := analyzer.NoMessages
	if includeMessages {
		scope = analyzer.AllMessages
	}
	analysis, err := analyzer.AnalyzeSession(session.FullPath, session.SessionID, scope)
	if err != nil {
		return fmt.Errorf("analyzing session: %w", err)
	}

	var warnings bytes.Buffer
	skipWarning{counts: "totals", agents: analysis.SkippedAgents, lines: analysis.SkippedLines}.write(&warnings, cfg.verbose)
	warnEstimatedCosts(&warnings, analysis.EstimatedCostMessages)
	warnUnknownModels(&warnings, analysis.CostByModel)

	// Output in requested format
	output, err := formatOutput(cfg, analysis, includeMessages)
	if err != nil {
		return fmt.Errorf("formatting output: %w", err)
	}

	printReport(cfg, &warnings, output)
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
