package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set via ldflags at build time
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(cmd.OutOrStdout(), "ccusage %s\n", Version)
		return nil
	},
}

func init() {
	// Register --version on the root command; match the version subcommand's output
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("ccusage {{.Version}}\n")
	rootCmd.AddCommand(versionCmd)
}
