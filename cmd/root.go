package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// Global flags
	projectPath string
	format      string
	verbose     bool
	noColor     bool
	live        bool
)

// validFormats contains the allowed output format values
var validFormats = map[string]bool{
	"table": true,
	"json":  true,
	"csv":   true,
}

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "ccusage",
	Short: "Claude Code Usage Analyzer",
	Long: `ccusage - Claude Code Usage Analyzer

Analyzes Claude Code sessions and calculates API costs based on Anthropic pricing.

Examples:
  ccusage                    Show cost breakdown for latest session
  ccusage show abc123        Show cost breakdown for specific session
  ccusage list               List all sessions for current project
  ccusage summary            Show aggregate stats across all sessions
  ccusage --live             Watch session in real-time`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Validate format flag
		if !validFormats[format] {
			return fmt.Errorf("invalid format %q: must be one of table, json, csv", format)
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		// Default behavior: show latest session
		showCmd.Run(cmd, args)
	},
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVarP(&projectPath, "project", "p", "", "Project directory (default: current directory)")
	rootCmd.PersistentFlags().StringVarP(&format, "format", "f", "table", "Output format: table, json, csv")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show debug information")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().BoolVarP(&live, "live", "l", false, "Enable live mode (auto-updates)")

	// Add subcommands
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(summaryCmd)
	rootCmd.AddCommand(watchCmd)
}

// getProjectPath returns the project path to use (flag value or CWD)
func getProjectPath() (string, error) {
	if projectPath != "" {
		return projectPath, nil
	}
	return os.Getwd()
}
