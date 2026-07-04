package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// validFormats contains the allowed output format values
var validFormats = map[string]bool{
	"table": true,
	"json":  true,
	"csv":   true,
}

// newRootCmd builds a fresh command tree bound to a new config. Each call is
// independent, which is what makes Execute re-entrant (see config).
func newRootCmd() *cobra.Command {
	cfg := &config{}

	rootCmd := &cobra.Command{
		Use:   "ficha",
		Short: "Claude Code Usage Analyzer",
		Long: `ficha - Claude Code Usage Analyzer

Analyzes Claude Code sessions and calculates API costs based on Anthropic pricing.

Examples:
  ficha                    Show cost breakdown for latest session
  ficha show abc123        Show cost breakdown for specific session
  ficha list               List all sessions for current project
  ficha summary            Show aggregate stats across all sessions
  ficha --live             Watch session in real-time`,
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       Version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Resolve output sinks once, before any command writes: cobra walks
			// up to the root's SetOut/SetErr, so this honors test redirection.
			cfg.stdout = cmd.OutOrStdout()
			cfg.stderr = cmd.ErrOrStderr()

			// version prints plain text and ignores --format entirely
			if cmd.Name() == "version" {
				return nil
			}
			// Validate format flag
			if !validFormats[cfg.format] {
				return fmt.Errorf("invalid format %q: must be one of table, json, csv", cfg.format)
			}
			// TUI/live modes only support table format. watch and breakdown are
			// TUI commands that ignore the --live flag, so gate on the command
			// name in addition to cfg.live (which covers `show --live`).
			isTUI := cfg.live || cmd.Name() == "breakdown" || cmd.Name() == "watch"
			if isTUI && cfg.format != "table" {
				return fmt.Errorf("--format %s is not supported in live/TUI mode", cfg.format)
			}
			// Warn about --no-follow outside live/watch/breakdown
			if cfg.noFollow && !cfg.live && cmd.Name() != "watch" && cmd.Name() != "breakdown" {
				fmt.Fprintln(cfg.stderr, "Warning: --no-follow has no effect outside live/watch/breakdown mode")
			}
			return nil
		},
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(cfg, args, cfg.live)
		},
	}
	rootCmd.SetVersionTemplate("ficha {{.Version}}\n")

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&cfg.projectPath, "project", "p", "", "Project directory (default: current directory)")
	rootCmd.PersistentFlags().StringVar(&cfg.projectDir, "project-dir", "", "Claude project directory name (bypass auto-detection)")
	rootCmd.PersistentFlags().StringVarP(&cfg.format, "format", "f", "table", "Output format: table, json, csv")
	rootCmd.PersistentFlags().BoolVarP(&cfg.verbose, "verbose", "v", false, "Show debug information")
	rootCmd.PersistentFlags().BoolVar(&cfg.noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().BoolVarP(&cfg.live, "live", "l", false, "Enable live mode (auto-updates)")
	rootCmd.PersistentFlags().BoolVar(&cfg.noFollow, "no-follow", false, "Disable auto-follow in live mode (pin to current session)")

	// Subcommands (each closes over the same cfg)
	rootCmd.AddCommand(newShowCmd(cfg))
	rootCmd.AddCommand(newListCmd(cfg))
	rootCmd.AddCommand(newSummaryCmd(cfg))
	rootCmd.AddCommand(newWatchCmd(cfg))
	rootCmd.AddCommand(newGlobalCmd(cfg))
	rootCmd.AddCommand(newBreakdownCmd(cfg))
	rootCmd.AddCommand(newVersionCmd())

	return rootCmd
}

// Execute runs the root command
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// getProjectPath returns the project path to use (flag value or CWD)
func getProjectPath(cfg *config) (string, error) {
	if cfg.projectPath != "" {
		return cfg.projectPath, nil
	}
	return os.Getwd()
}
