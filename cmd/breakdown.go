package cmd

import (
	"fmt"

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
	RunE: runBreakdown,
}

func init() {
	rootCmd.AddCommand(breakdownCmd)
}

func runBreakdown(cmd *cobra.Command, args []string) error {
	session, projectDir, explicitSessionID, err := selectSession(args)
	if err != nil {
		return err
	}

	// Auto-follow is enabled by default unless:
	// - User specified a session ID explicitly (pinned to that session)
	// - User passed --no-follow flag
	followMode := !explicitSessionID && !noFollow

	// Run the breakdown TUI
	model := tui.NewBreakdownModel(session.FullPath, session.SessionID, noColor, projectDir, followMode)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}
	return nil
}
