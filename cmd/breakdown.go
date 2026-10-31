package cmd

import (
	tea "charm.land/bubbletea/v2"
	"github.com/bardisty/ficha/internal/tui"
	"github.com/spf13/cobra"
)

func newBreakdownCmd(cfg *config) *cobra.Command {
	breakdownCmd := &cobra.Command{
		Use:   "breakdown [session-id]",
		Short: "Every message, live, with its agent, model, cost and tokens",
		Long: `Every message of the project's latest Claude Code session in a live,
scrollable table, with its agent, model, cost and tokens.

New rows show up as messages arrive, highlighted briefly, and the view stays
on the newest until you scroll up. When a new session starts, after /clear or
a restart, breakdown switches to it. Give a session ID, the start of one, or
a transcript path, or pass --no-follow, to stay on one session. With no
session in the project yet, it waits for the first.

The AGENT column marks agents' rows with the IDs watch shows. A workflow
agent's cell ends in its run's initials, such as rc for a review-changes
run, and the footer names those on screen when it has room. Sorted by cost
with s, costliest first, the view stays put as rows arrive, and time order
comes back where you left it. On a narrow terminal, columns give way whole,
IN first, and ? names the ones hidden.

Examples:
  ficha breakdown               Follow the latest session
  ficha breakdown --no-follow   Stay on the latest session
  ficha breakdown abc123        Show one session

` + keysHelp(tui.BreakdownKeys()),
		Args:              sessionArgs,
		ValidArgsFunction: completeSessionIDs(cfg),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBreakdown(cfg, args)
		},
	}

	addFormatFlag(breakdownCmd, cfg, true)
	addProjectFlags(breakdownCmd, cfg)
	// breakdown is always live; --live is accepted, hidden, for scripts that pass it.
	addLiveFlags(breakdownCmd, cfg, true)

	return breakdownCmd
}

func runBreakdown(cfg *config, args []string) error {
	// No session yet: wait for one, as watch does, where that's the answer
	if len(args) == 0 {
		if waited, err := waitForFirstSession(cfg, waitingBreakdown(cfg)); waited {
			return err
		}
	}
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
	return runTUI(cfg, func() tea.Model {
		return tui.NewBreakdownModel(session.FullPath, session.SessionID, cfg.noColor, projectDir, followMode)
	})
}
