package cmd

import (
	"github.com/spf13/cobra"
)

func newWatchCmd(cfg *config) *cobra.Command {
	watchCmd := &cobra.Command{
		Use:   "watch [session-id]",
		Short: "Watch session in real-time (alias for show --live)",
		Long: `Watch a Claude Code session in real-time, updating as new messages arrive.

This is an alias for 'ficha show --live'.

By default, auto-follows the latest session in the project. When you start a new
Claude Code session (via /exit, /clear, or restart), the view automatically
switches to the new session.

Examples:
  ficha watch                   Watch and auto-follow latest session
  ficha watch --no-follow       Watch latest session, don't auto-follow
  ficha watch abc123            Watch specific session (pinned, no auto-follow)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// watch is show with live mode forced on; derived from the command
			// rather than mutating a shared flag, so it can't leak into a later run.
			return runShow(cfg, args, true)
		},
	}

	addProjectFlags(watchCmd, cfg)
	// watch is always live; --live is accepted, hidden, for scripts that pass it.
	addLiveFlags(watchCmd, cfg, true)

	return watchCmd
}
