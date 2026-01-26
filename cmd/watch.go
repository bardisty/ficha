package cmd

import (
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch [session-id]",
	Short: "Watch session in real-time (alias for show --live)",
	Long: `Watch a Claude Code session in real-time, updating as new messages arrive.

This is an alias for 'ccusage show --live'.

Examples:
  ccusage watch                   Watch latest session
  ccusage watch abc123            Watch specific session`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// Set live mode and run show
		live = true
		runShow(cmd, args)
	},
}
