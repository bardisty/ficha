package cmd

import "github.com/spf13/cobra"

// newEnvironmentHelpTopic is `ficha help environment`, the environment
// variables ficha reads. Like `help output` it has no Run, so cobra lists it
// under "Additional help topics".
func newEnvironmentHelpTopic() *cobra.Command {
	return &cobra.Command{
		Use:   "environment",
		Short: "Environment variables ficha reads",
		Long: `Environment variables

CLAUDE_CONFIG_DIR
  Claude Code's config directory, ~/.claude when unset. ficha reads the
  transcripts in its projects folder, the same place Claude Code writes them.

NO_COLOR
  Any non-empty value turns color off, like --no-color. On a terminal,
  --no-color=false turns it back on for that run.

CLICOLOR
  Set to 0 to turn color off, like NO_COLOR. CLICOLOR_FORCE=1 outranks it.

CLICOLOR_FORCE
  Set to 1 to keep color when output isn't a terminal, as in
  CLICOLOR_FORCE=1 ficha summary | less -R. Piped color is the dark palette
  in the 16 basic colors, coarser than on screen. NO_COLOR and --no-color
  still turn color off.

CI
  Any non-empty value turns color off, even on a terminal, and CI=false
  counts. CI services set it, and CLICOLOR_FORCE=1 keeps color in their
  logs. If your shell exports it, --no-color=false brings color back.

COLORFGBG
  The terminal's colors as foreground;background, such as '0;15' for dark
  text on white. ficha reads the background from it only when it can't ask
  the terminal, as inside tmux or screen. On Windows ficha ignores it and
  always uses the dark palette.`,
	}
}
