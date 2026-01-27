package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var breakdownCmd = &cobra.Command{
	Use:   "breakdown [session-id]",
	Short: "Show live per-message cost breakdown",
	Long: `Show a live, scrollable per-message cost table for a Claude Code session.

Run this alongside 'ccusage watch' in a separate terminal for detailed
cost visibility during Claude Code sessions.

Features:
  - Live updates as new messages arrive
  - Auto-follows latest session (switches when new session starts)
  - Auto-scroll to latest messages (can scroll up manually)
  - Agent sub-session messages shown inline with [A1], [A2] markers
  - New rows highlighted briefly when they appear

Examples:
  ccusage breakdown              Show breakdown and auto-follow latest session
  ccusage breakdown --no-follow  Show breakdown for latest, don't auto-follow
  ccusage breakdown abc123       Show breakdown for specific session (pinned)`,
	Args: cobra.MaximumNArgs(1),
	Run:  runBreakdown,
}

func init() {
	rootCmd.AddCommand(breakdownCmd)
}

func runBreakdown(cmd *cobra.Command, args []string) {
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

	// Auto-follow is enabled by default unless:
	// - User specified a session ID explicitly (pinned to that session)
	// - User passed --no-follow flag
	followMode := !explicitSessionID && !noFollow

	// Run the breakdown TUI
	model := tui.NewBreakdownModel(session.FullPath, session.SessionID, noColor, projectDir, followMode)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
