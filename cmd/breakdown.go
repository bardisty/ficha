package cmd

import (
	"github.com/bardisty/ficha/internal/tui"
	"github.com/spf13/cobra"
)

func newBreakdownCmd(cfg *config) *cobra.Command {
	breakdownCmd := &cobra.Command{
		Use:   "breakdown [session-id]",
		Short: "Show live per-message cost breakdown",
		Long: `Show a live, scrollable per-message cost table for a Claude Code session.

Run this alongside 'ficha watch' in a separate terminal for detailed
cost visibility during Claude Code sessions.

Features:
  - Live updates as new messages arrive
  - Auto-follows latest session (switches when new session starts)
  - Auto-scroll to latest messages (can scroll up manually)
  - Agent sub-session messages shown inline with [A1], [A2] markers
  - New rows highlighted briefly when they appear

Examples:
  ficha breakdown              Show breakdown and auto-follow latest session
  ficha breakdown --no-follow  Show breakdown for latest, don't auto-follow
  ficha breakdown abc123       Show breakdown for specific session (pinned)`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeSessionIDs(cfg),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBreakdown(cfg, args)
		},
	}

	addProjectFlags(breakdownCmd, cfg)
	// breakdown is always live; --live is accepted, hidden, for scripts that pass it.
	addLiveFlags(breakdownCmd, cfg, true)

	return breakdownCmd
}

func runBreakdown(cfg *config, args []string) error {
	session, projectDir, explicitSessionID, err := selectSession(cfg, args)
	if err != nil {
		return err
	}

	// Auto-follow is enabled by default unless:
	// - User specified a session ID explicitly (pinned to that session)
	// - User passed --no-follow flag
	followMode := !explicitSessionID && !cfg.noFollow
	if err := requireTerminal(cfg.stdout, cfg.commandPath, "For per-message rows in a script, use 'ficha show -f csv --messages'."); err != nil {
		return err
	}

	// Run the breakdown TUI
	model := tui.NewBreakdownModel(session.FullPath, session.SessionID, cfg.noColor, projectDir, followMode)
	return runTUI(cfg.stdout, model)
}
