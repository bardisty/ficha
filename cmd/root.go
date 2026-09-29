package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/bardisty/ficha/internal/styles"
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
		Use:   "ficha [session-id]",
		Short: "Track your Claude Code API-equivalent costs, token usage, and context window",
		Long: `Track your Claude Code API-equivalent costs, token usage, and context window.

Figures are API list-price estimates. On a subscription this is not your bill.

Run ficha from the directory Claude Code is running in, or point -p at it.

Examples:
  ficha                    Cost breakdown for the latest session
  ficha watch              Live dashboard that follows new sessions
  ficha breakdown          Live per-message cost table
  ficha summary            Totals across this project's sessions
  ficha global             Totals across every project`,
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       version(),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Resolve output sinks once, before any command writes: cobra walks
			// up to the root's SetOut/SetErr, so this honors test redirection.
			cfg.stdout = cmd.OutOrStdout()
			cfg.stderr = cmd.ErrOrStderr()
			cfg.commandPath = cmd.CommandPath()
			if cfg.live && cmd.Name() != "watch" && cmd.Name() != "breakdown" {
				cfg.commandPath += " --live"
			}

			// NO_COLOR (no-color.org) means the same as --no-color: any
			// non-empty value turns color off. lipgloss already drops escapes
			// for it, but ficha's own no-color text fallbacks key off noColor.
			// An explicit --no-color=false still wins, as no-color.org asks.
			if os.Getenv("NO_COLOR") != "" && !cmd.Flags().Changed("no-color") {
				cfg.noColor = true
			}
			// Glyphs are process-wide; set them every run (true or false) so
			// an in-process caller's earlier --ascii can't leak into this one.
			styles.SetASCII(cfg.ascii)

			// version prints plain text and ignores --format entirely
			if cmd.Name() == "version" {
				return nil
			}
			// Validate format flag
			if !validFormats[strings.ToLower(cfg.format)] {
				return fmt.Errorf("invalid format %q: must be one of table, json, csv", cfg.format)
			}
			cfg.format = strings.ToLower(cfg.format)
			// TUI/live modes only support table format. watch and breakdown are
			// always TUIs; cfg.live covers `show --live` and bare `ficha --live`.
			isTUI := cfg.live || cmd.Name() == "breakdown" || cmd.Name() == "watch"
			if isTUI && cfg.format != "table" {
				return fmt.Errorf("--format %s is not supported in live/TUI mode", cfg.format)
			}
			// Runs ahead of cobra's own mutual-exclusion check, whose message
			// doesn't say what either flag is for.
			if cmd.Flags().Changed("project") && cmd.Flags().Changed("project-dir") {
				return fmt.Errorf("use either -p or --project-dir, not both: -p is the directory Claude Code ran in, --project-dir a Claude project directory name")
			}
			// --no-follow is registered on show and the root, where it only
			// means something with --live.
			if cfg.noFollow && !isTUI {
				fmt.Fprintln(cfg.stderr, "Warning: --no-follow has no effect outside live/watch/breakdown mode")
			}
			return nil
		},
		Args: rootArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rootRun(cfg, cmd, args)
		},
	}
	rootCmd.SetVersionTemplate("ficha {{.Version}}\n")
	rootCmd.SetFlagErrorFunc(flagError)
	// cobra defaults this lazily, only on its own unknown-command path, and
	// rootArgs calls SuggestionsFor directly.
	rootCmd.SuggestionsMinimumDistance = 2

	// Flags every command honors. Everything else is registered only on the
	// commands that use it, so help lists nothing a command would ignore.
	rootCmd.PersistentFlags().StringVarP(&cfg.format, "format", "f", "table", "Output format: table, json, csv")
	rootCmd.PersistentFlags().BoolVarP(&cfg.verbose, "verbose", "v", false, "Show debug information")
	rootCmd.PersistentFlags().BoolVar(&cfg.noColor, "no-color", false, "Disable colored output (same as setting NO_COLOR)")
	rootCmd.PersistentFlags().BoolVar(&cfg.ascii, "ascii", false, "Draw frames and symbols in plain ASCII instead of Unicode")
	_ = rootCmd.RegisterFlagCompletionFunc("format", fixedValues(formatValues))

	// Bare `ficha` is `ficha show`. --live stays for compatibility, hidden so
	// help steers people to `ficha watch`; --no-follow only matters with it.
	addProjectFlags(rootCmd, cfg)
	addLiveFlags(rootCmd, cfg, true)
	_ = rootCmd.Flags().MarkHidden("no-follow")

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

// addProjectFlags registers -p and --project-dir on a command that analyzes one
// project.
func addProjectFlags(cmd *cobra.Command, cfg *config) {
	cmd.Flags().StringVarP(&cfg.projectPath, "project", "p", "", "Project directory (default: current directory)")
	cmd.Flags().StringVar(&cfg.projectDir, "project-dir", "", "Claude project directory name (bypass auto-detection)")
	// Validated with a clearer message in PersistentPreRunE; marking the group
	// as well stops completion offering the second flag once one is set.
	cmd.MarkFlagsMutuallyExclusive("project", "project-dir")
	_ = cmd.RegisterFlagCompletionFunc("project", func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	})
	_ = cmd.RegisterFlagCompletionFunc("project-dir", completeProjectDirs)
}

// addLiveFlags registers --live and --no-follow on a command that can run the
// live view. hideLive hides --live from help while still accepting it.
func addLiveFlags(cmd *cobra.Command, cfg *config, hideLive bool) {
	cmd.Flags().BoolVarP(&cfg.live, "live", "l", false, "Watch the session live (same as 'ficha watch')")
	cmd.Flags().BoolVar(&cfg.noFollow, "no-follow", false, "Stay on the starting session instead of following new ones (live mode only)")
	if hideLive {
		_ = cmd.Flags().MarkHidden("live")
	}
}

// getProjectPath returns the project path to use (flag value or CWD)
func getProjectPath(cfg *config) (string, error) {
	if cfg.projectPath != "" {
		return cfg.projectPath, nil
	}
	return os.Getwd()
}
