package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

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
	sessions, projectDir, err := loadProjectSessionsWithDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Find the session to show
	var session *models.SessionEntry
	explicitSessionID := len(args) > 0 // User provided a specific session ID
	if explicitSessionID {
		// Find by ID (partial match)
		sessionID := args[0]
		var err error
		session, err = findSessionByPartialID(sessions, sessionID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if session == nil {
			fmt.Fprintf(os.Stderr, "Error: session not found: %s\n", sessionID)
			os.Exit(1)
		}
	} else {
		// Get latest session (sort by modified time first)
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].Modified.After(sessions[j].Modified)
		})
		session = &sessions[0]
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
	p := tea.NewProgram(model)

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

// findSessionByPartialID finds a session by partial ID match
// Returns the matching session, or an error if multiple sessions match
func findSessionByPartialID(sessions []models.SessionEntry, partialID string) (*models.SessionEntry, error) {
	if len(sessions) == 0 {
		return nil, nil
	}

	// Try exact match first
	for i := range sessions {
		if sessions[i].SessionID == partialID {
			return &sessions[i], nil
		}
	}

	// Try prefix match - collect all matches
	var matches []models.SessionEntry
	for i := range sessions {
		if len(sessions[i].SessionID) >= len(partialID) && sessions[i].SessionID[:len(partialID)] == partialID {
			matches = append(matches, sessions[i])
		}
	}

	if len(matches) == 0 {
		return nil, nil
	}

	if len(matches) == 1 {
		return &matches[0], nil
	}

	// Multiple matches - return error with list
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ambiguous session ID %q matches %d sessions:\n", partialID, len(matches)))
	for _, m := range matches {
		sb.WriteString(fmt.Sprintf("  %s\n", m.SessionID))
	}
	return nil, fmt.Errorf("%s", sb.String())
}
