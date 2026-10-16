package cmd

import (
	"context"
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
'ficha help output' explains the terms the reports use.

Run ficha from the directory Claude Code is running in, or point -p at it.

Examples:
  ficha                    Cost breakdown for the latest session
  ficha watch              Live dashboard that follows new sessions
  ficha breakdown          Live per-message cost table
  ficha summary            Totals across this project's sessions
  ficha global             Totals across every project

Exit status:
    0  Success, including a report that skipped unreadable input
    1  No report, for example no sessions or no matching session
    2  Usage error: unknown command or flag, bad value, or flags that clash
  130  A live view interrupted from outside, for example by kill -INT`,
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       versionLine(),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Resolve output sinks once, before any command writes: cobra walks
			// up to the root's SetOut/SetErr, so this honors test redirection.
			cfg.stdout = cmd.OutOrStdout()
			cfg.stderr = cmd.ErrOrStderr()
			cfg.commandPath = cmd.CommandPath()
			if cfg.live && cmd.Name() != "watch" && cmd.Name() != "breakdown" {
				cfg.commandPath += " --live"
			}

			resolveColor(cmd, cfg)
			// Glyphs and the palette are process-wide; set them every run so
			// an in-process caller's earlier --ascii or background can't leak
			// into this one. The palette starts dark, and pickPalette changes
			// it once a report is about to draw in color.
			styles.SetASCII(cfg.ascii)
			styles.SetBasic(false)
			styles.SetDark(true)

			if cmd.Annotations[noGlobalFlagsAnnotation] != "" {
				return noGlobalFlags(cmd)
			}
			// A -v run pasted into a bug report says which build it came
			// from. Completion requests never get here with -v set: cobra
			// hands their flags to the command being completed, unparsed.
			cfg.tracef("%s", versionLine())
			// Validate format flag
			if !validFormats[strings.ToLower(cfg.format)] {
				return usageErrorf("invalid format %q: must be one of table, json, csv", cfg.format)
			}
			cfg.format = strings.ToLower(cfg.format)
			// watch and breakdown are always TUIs; cfg.live covers `show --live`
			// and bare `ficha --live`.
			isTUI := cfg.live || cmd.Name() == "breakdown" || cmd.Name() == "watch"
			if isTUI && cfg.format != "table" {
				return liveFormatError(cfg, cmd, args)
			}
			if cmd.Name() == "statusline" && cfg.format != "table" {
				return usageErrorf("--format %s is not supported by statusline, which prints one line for Claude Code. For scripting, use 'ficha show <transcript_path> -f %s'", cfg.format, cfg.format)
			}
			// Runs ahead of cobra's own mutual-exclusion check, whose message
			// doesn't say what either flag is for.
			if cmd.Flags().Changed("project") && cmd.Flags().Changed("project-dir") {
				return usageErrorf("use either -p or --project-dir, not both: -p is the directory Claude Code ran in, --project-dir a Claude project directory name")
			}
			// --no-follow is registered on show and the root, where it only
			// means something with --live.
			if cfg.noFollow && !isTUI {
				writeWarning(cfg, "--no-follow has no effect outside live/watch/breakdown mode")
			}
			return nil
		},
		Args: rootArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rootRun(cfg, cmd, args)
		},
	}
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.SetFlagErrorFunc(flagError)
	// cobra defaults this lazily, only on its own unknown-command path, and
	// rootArgs calls SuggestionsFor directly.
	rootCmd.SuggestionsMinimumDistance = 2

	// Flags every command honors. Everything else is registered only on the
	// commands that use it, so help lists nothing a command would ignore.
	rootCmd.PersistentFlags().BoolVarP(&cfg.verbose, "verbose", "v", false, "Show debug information")
	rootCmd.PersistentFlags().BoolVar(&cfg.noColor, "no-color", false, "Disable colored output (same as setting NO_COLOR)")
	rootCmd.PersistentFlags().BoolVar(&cfg.ascii, "ascii", false, "Draw frames and symbols in ASCII, not Unicode")

	// Bare `ficha` is `ficha show`. --live stays for compatibility, hidden so
	// help steers people to `ficha watch`; --no-follow only matters with it.
	addFormatFlag(rootCmd, cfg, false)
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
	rootCmd.AddCommand(newStatuslineCmd(cfg))
	version := newVersionCmd()
	rootCmd.AddCommand(version)
	takeNoGlobalFlags(version)
	rootCmd.AddCommand(newOutputHelpTopic())
	rootCmd.AddCommand(newEnvironmentHelpTopic())
	// cobra adds help lazily too, but its Run reads the output streams only
	// when it runs, so adding it here is safe.
	rootCmd.InitDefaultHelpCmd()
	if help := subcommandNamed(rootCmd, "help"); help != nil {
		help.Args = helpTopicArgs
	}
	// help and its topics take the global flags, which change nothing in
	// their fixed text. -f is still one of them there, out of sight.
	for _, c := range rootCmd.Commands() {
		if c.Flags().Lookup("format") == nil {
			addFormatFlag(c, cfg, true)
		}
	}
	usageArgs(rootCmd)

	return rootCmd
}

// initCompletion adds cobra's completion command ahead of Execute, which
// would otherwise add it lazily, out of usageArgs' reach. Its scripts go to
// the stdout set at this point, so a caller redirecting output must do that
// first. Without a RunE, `completion bogus` prints help and exits 0 instead
// of failing on its Args check.
func initCompletion(root *cobra.Command) {
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() == "completion" {
			c.RunE = func(c *cobra.Command, _ []string) error { return c.Help() }
			c.Args = shellArgs
			usageArgs(c)
			takeNoGlobalFlags(c)
			for _, shell := range c.Commands() {
				takeNoGlobalFlags(shell)
				// cobra's one line past 80 columns, which a terminal of that
				// width would wrap mid-word.
				shell.Long = strings.Replace(shell.Long,
					"If it is not installed already, you can install it via your OS's package manager.",
					"If it is not installed already, install it with your OS's package manager.", 1)
			}
		}
	}
}

// shellArgs replaces the completion command's cobra.NoArgs, whose `unknown
// command "x" for "ficha completion"` doesn't say which shells there are.
func shellArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	var shells []string
	for _, c := range cmd.Commands() {
		if c.IsAvailableCommand() {
			shells = append(shells, c.Name())
		}
	}
	return fmt.Errorf("unknown shell %q: want %s", args[0], joinWords(shells, "or"))
}

// Execute runs the root command
func Execute() {
	root := newRootCmd()
	initCompletion(root)
	err := root.ExecuteContext(withCommandLine(context.Background(), os.Args[1:]))
	if err == nil {
		return
	}
	if err.Error() != "" {
		fmt.Fprint(os.Stderr, formatError(os.Stderr, err))
	}
	os.Exit(exitCode(err))
}

// addFormatFlag registers -f on a command. hidden keeps it out of the help
// and completion of a command that prints only a table, where it still
// parses, so -f json on a live view gets liveFormatError rather than an
// unknown flag.
func addFormatFlag(cmd *cobra.Command, cfg *config, hidden bool) {
	cmd.Flags().StringVarP(&cfg.format, "format", "f", "table", "Output format: table, json, csv")
	if hidden {
		_ = cmd.Flags().MarkHidden("format")
		_ = cmd.RegisterFlagCompletionFunc("format", cobra.NoFileCompletions)
		return
	}
	_ = cmd.RegisterFlagCompletionFunc("format", fixedValues(formatValues))
}

// liveFormatError rejects -f json and -f csv on a live view, naming the
// show command that prints the same session in that format. It carries the
// session ID, the project and --messages from the line, and drops the rest:
// the live flags mean nothing to a report, and the display flags nothing to
// json.
func liveFormatError(cfg *config, cmd *cobra.Command, args []string) error {
	what := "the session as " + cfg.format
	line := []string{cmd.Root().Name(), "show"}
	switch {
	case cmd.Flags().Changed("project"):
		line = append(line, "-p", shellQuote(cfg.projectPath))
	case cmd.Flags().Changed("project-dir"):
		line = append(line, "--project-dir="+shellQuote(cfg.projectDir))
	}
	for _, a := range args {
		line = append(line, shellQuote(a))
	}
	line = append(line, "-f", cfg.format)
	if cmd.Name() == "breakdown" || cfg.messages {
		what = "the session's messages as " + cfg.format
		if cfg.format == "csv" {
			what = "one row per message"
		}
		line = append(line, "--messages")
	}
	return usageErrorf("%s is a live view and has no %s output. For %s, run: %s", cfg.command(), cfg.format, what, strings.Join(line, " "))
}

// addProjectFlags registers -p and --project-dir on a command that analyzes one
// project.
func addProjectFlags(cmd *cobra.Command, cfg *config) {
	cmd.Flags().StringVarP(&cfg.projectPath, "project", "p", "", "Project directory (default: current directory)")
	cmd.Flags().StringVar(&cfg.projectDir, "project-dir", "", "Claude project directory name, instead of -p")
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
	cmd.Flags().BoolVar(&cfg.noFollow, "no-follow", false, "Stay on the starting session when a new one starts")
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
