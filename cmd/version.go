package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set via ldflags at build time
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "ccusage %s\n", Version)
			return nil
		},
	}
}
