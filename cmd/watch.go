package cmd

import (
	"github.com/spf13/cobra"

	"github.com/bardisty/ficha/internal/tui"
)

func newWatchCmd(cfg *config) *cobra.Command {
	watchCmd := &cobra.Command{
		Use:   "watch [session-id]",
		Short: "The latest session as a live dashboard, following new sessions",
		Long: `A live dashboard for the project's latest Claude Code session: cost, tokens,
context window and agents, updated as messages arrive.

When a new session starts, after /clear or a restart, watch switches to it.
Give a session ID, the start of one, or a transcript path, or pass
--no-follow, to stay on one session. With no session in the project yet, it
waits for the first.

Examples:
  ficha watch                   Follow the latest session
  ficha watch --no-follow       Stay on the latest session
  ficha watch abc123            Watch one session

` + keysHelp(tui.WatchKeys()),
		Args:              sessionArgs,
		ValidArgsFunction: completeSessionIDs(cfg),
		RunE: func(cmd *cobra.Command, args []string) error {
			// watch is show with live mode forced on; derived from the command
			// rather than mutating a shared flag, so it can't leak into a later run.
			return runShow(cfg, args, true)
		},
	}

	addFormatFlag(watchCmd, cfg, true)
	addProjectFlags(watchCmd, cfg)
	// watch is always live; --live is accepted, hidden, for scripts that pass it.
	addLiveFlags(watchCmd, cfg, true)

	return watchCmd
}
