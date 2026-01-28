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
	Run:  runShow,
}

func runShow(cmd *cobra.Command, args []string) {
	session, projectDir, explicitSessionID, err := selectSession(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Live mode
	if live {
		// Auto-follow is enabled by default unless:
		// - User specified a session ID explicitly (pinned to that session)
		// - User passed --no-follow flag
		followMode := !explicitSessionID && !noFollow
		runLiveMode(session, projectDir, followMode)
		return
	}

	// Analyze the session
	includeMessages := format == "csv" || verbose
	analysis, err := analyzer.AnalyzeSession(session.FullPath, session.SessionID, includeMessages)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analyzing session: %v\n", err)
		os.Exit(1)
	}

	// Warn about skipped agents
	if analysis.SkippedAgents > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d agent sub-session(s) could not be parsed\n", analysis.SkippedAgents)
	}

	// Warn about unknown models (using fallback pricing)
	warnUnknownModels(analysis)

	// Output in requested format
	output, err := formatOutput(analysis, includeMessages)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting output: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(output)
}

func runLiveMode(session *models.SessionEntry, projectDir string, followMode bool) {
	model := tui.NewModel(session.FullPath, session.SessionID, verbose, noColor, projectDir, followMode)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
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
