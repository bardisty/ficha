package cmd

import (
	tea "charm.land/bubbletea/v2"
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
  - Live updates as new messages arrive, highlighted briefly
  - Follows new sessions as they start; a session ID or transcript path
    pins it to one
  - Stays on the newest rows until you scroll up
  - Agent rows marked in an AGENT column, with the IDs watch shows
  - A workflow agent's AGENT cell ends in its run's initials, such as
    rc for a review-changes run; the footer names those on screen when
    it has room
  - Can sort by cost, most expensive first. While sorted, the view stays
    put as rows arrive, and time order comes back where you left it
  - With no session in the project yet, waits for the first one

Examples:
  ficha breakdown              Show breakdown and auto-follow latest session
  ficha breakdown --no-follow  Show breakdown for latest, don't auto-follow
  ficha breakdown abc123       Show breakdown for specific session (pinned)

` + keysHelp(tui.BreakdownKeys()),
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
		// No session yet: wait for one, as watch does, where that's the answer
		if len(args) == 0 {
			if waited, waitErr := waitForFirstSession(cfg, waitingBreakdown(cfg)); waited {
				return waitErr
			}
		}
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
	return runTUI(cfg, func() tea.Model {
		return tui.NewBreakdownModel(session.FullPath, session.SessionID, cfg.noColor, projectDir, followMode)
	})
}
