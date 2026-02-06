package cmd

import (
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch [session-id]",
	Short: "Watch session in real-time (alias for show --live)",
	Long: `Watch a Claude Code session in real-time, updating as new messages arrive.

This is an alias for 'ccusage show --live'.

By default, auto-follows the latest session in the project. When you start a new
Claude Code session (via /exit, /clear, or restart), the view automatically
switches to the new session.

Examples:
  ccusage watch                   Watch and auto-follow latest session
  ccusage watch --no-follow       Watch latest session, don't auto-follow
  ccusage watch abc123            Watch specific session (pinned, no auto-follow)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Set live mode and run show
		live = true
		return runShow(cmd, args)
	},
}
