package cmd

import (
	"fmt"
	"os"

	"github.com/bardisty/ccusage/internal/analyzer"
	"github.com/bardisty/ccusage/internal/formatter"
	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show [session-id]",
	Short: "Show session cost breakdown",
	Long: `Show cost breakdown for a Claude Code session.

If no session ID is provided, shows the most recent session.

Examples:
  ccusage show                    Show latest session
  ccusage show abc123             Show specific session
  ccusage show --live             Watch latest session in real-time
  ccusage show -f json            Output as JSON`,
	Args: cobra.MaximumNArgs(1),
	RunE: runShow,
}

func runShow(cmd *cobra.Command, args []string) error {
	session, projectDir, explicitSessionID, err := selectSession(args)
	if err != nil {
		return err
	}

	// Live mode
	if live {
		// Auto-follow is enabled by default unless:
		// - User specified a session ID explicitly (pinned to that session)
		// - User passed --no-follow flag
		followMode := !explicitSessionID && !noFollow
		return runLiveMode(session, projectDir, followMode)
	}

	// Analyze the session
	includeMessages := format == "csv" || verbose
	analysis, err := analyzer.AnalyzeSession(session.FullPath, session.SessionID, includeMessages)
	if err != nil {
		return fmt.Errorf("analyzing session: %w", err)
	}

	// Warn about skipped agents
	if analysis.SkippedAgents > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d agent sub-session(s) could not be parsed\n", analysis.SkippedAgents)
	}

	// Warn about unknown models (using fallback pricing)
	warnUnknownModels(analysis.CostByModel)

	// Output in requested format
	output, err := formatOutput(analysis, includeMessages)
	if err != nil {
		return fmt.Errorf("formatting output: %w", err)
	}

	fmt.Println(output)
	return nil
}

func runLiveMode(session *models.SessionEntry, projectDir string, followMode bool) error {
	model := tui.NewModel(session.FullPath, session.SessionID, verbose, noColor, projectDir, followMode)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}
	return nil
}

func formatOutput(analysis *models.SessionAnalysis, includeMessages bool) (string, error) {
	switch format {
	case "json":
		return formatter.FormatSessionJSON(analysis, true)
	case "csv":
		return formatter.FormatSessionCSV(analysis, includeMessages)
	default:
		return formatter.FormatSessionTable(analysis, noColor), nil
	}
}
