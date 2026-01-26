package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/bah/ccusage/internal/analyzer"
	"github.com/bah/ccusage/internal/formatter"
	"github.com/bah/ccusage/internal/models"
	"github.com/bah/ccusage/internal/parser"
	"github.com/bah/ccusage/internal/paths"
	"github.com/bah/ccusage/internal/tui"
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
	// Get project path
	projPath, err := getProjectPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Get Claude project directory
	projectDir, err := paths.GetProjectDirForPath(projPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Scan disk for session files
	diskSessions, err := parser.DiscoverSessionsFromDisk(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scanning sessions: %v\n", err)
		fmt.Fprintf(os.Stderr, "Project directory: %s\n", projectDir)
		os.Exit(1)
	}

	// Try to load index (may fail or be incomplete)
	indexPath := paths.GetSessionsIndexPath(projectDir)
	index, _ := parser.ParseSessionsIndex(indexPath) // Ignore error

	// Merge sources
	sessions, orphanCount := parser.MergeSessionSources(index, diskSessions)

	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no sessions found")
		os.Exit(1)
	}

	// Warn about orphans
	if orphanCount > 0 {
		fmt.Fprintf(os.Stderr, "Note: Found %d session(s) not in sessions-index.json\n", orphanCount)
	}

	// Find the session to show
	var session *models.SessionEntry
	if len(args) > 0 {
		// Find by ID (partial match)
		sessionID := args[0]
		var err error
		session, err = findSessionByPartialID(sessions, sessionID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v", err)
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
		runLiveMode(session)
		return
	}

	// Analyze the session
	includeMessages := format == "csv" || verbose
	analysis, err := analyzer.AnalyzeSession(session.FullPath, session.SessionID, includeMessages)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analyzing session: %v\n", err)
		os.Exit(1)
	}

	// Output in requested format
	output, err := formatOutput(analysis, includeMessages)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting output: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(output)
}

func runLiveMode(session *models.SessionEntry) {
	model := tui.NewModel(session.FullPath, session.SessionID, verbose, noColor)
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
		return formatter.FormatSessionCSV(analysis, includeMessages), nil
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
