package cmd

import (
	"os"

	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/colorprofile"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

// stdoutIsTerminal is isTerminal, swappable because tests can't give ficha a
// terminal.
var stdoutIsTerminal = isTerminal

// resolveColor decides whether this run prints color and how many colors it
// may use. It reads the flags, the environment and whether stdout is a
// terminal, and asks the terminal nothing, so every command can afford it.
//
// NO_COLOR (no-color.org), or CLICOLOR=0 without CLICOLOR_FORCE, means the
// same as --no-color. Past that the rules are termenv's: CI, a pipe and
// TERM=dumb are plain, and CLICOLOR_FORCE on a pipe gives the 16 basic
// colors. An explicit --no-color=false on a terminal beats NO_COLOR,
// CLICOLOR=0 and CI, as no-color.org asks. The first two are hidden from
// termenv. The third is part of termenv's terminal test, which
// stdoutIsTerminal has already answered.
func resolveColor(cmd *cobra.Command, cfg *config) {
	explicit := cmd.Flags().Changed("no-color")
	if !explicit && termenv.EnvNoColor() {
		cfg.noColor = true
	}
	cfg.profile, cfg.terminal, cfg.palettePicked = colorprofile.NoTTY, nil, false
	if cfg.noColor {
		return
	}
	tty := stdoutIsTerminal(cfg.stdout)
	opts := []termenv.OutputOption{termenv.WithTTY(tty && (explicit || os.Getenv("CI") == ""))}
	if explicit && tty {
		opts = append(opts, termenv.WithEnvironment(colorOnEnv{}))
	}
	cfg.terminal = termenv.NewOutput(cfg.stdout, opts...)
	cfg.profile = writerProfile(cfg.terminal.EnvColorProfile())
}

// writerProfile is termenv's profile as the writers and Bubble Tea name it.
// termenv's Ascii means no escapes at all, bold included, which is
// colorprofile's NoTTY. colorprofile's own ASCII keeps bold.
func writerProfile(p termenv.Profile) colorprofile.Profile {
	switch p {
	case termenv.TrueColor:
		return colorprofile.TrueColor
	case termenv.ANSI256:
		return colorprofile.ANSI256
	case termenv.ANSI:
		return colorprofile.ANSI
	default:
		return colorprofile.NoTTY
	}
}

// pickPalette asks the terminal for its background and picks the light or
// dark palette, and the 16-color one where that's all the terminal has. It
// is the only place ficha queries the terminal, and a terminal that never
// answers costs one wait. So it runs once per run, right before a table is
// formatted or a live view is built, and not at all for a run that prints
// no color: json, csv, --no-color, a pipe, CI, and every command that never
// gets as far as a report.
//
// Inside tmux or screen termenv doesn't ask, and goes by COLORFGBG. On
// Windows, and wherever it can't tell, the answer is dark.
func pickPalette(cfg *config) {
	if cfg.format != "table" || cfg.noColor || cfg.profile == colorprofile.NoTTY || cfg.palettePicked {
		return
	}
	cfg.palettePicked = true
	styles.SetBasic(cfg.profile == colorprofile.ANSI)
	styles.SetDark(cfg.terminal.HasDarkBackground())
}

// colorOnEnv is the environment with the variables --no-color=false outranks
// left out.
type colorOnEnv struct{}

func (colorOnEnv) Environ() []string { return os.Environ() }

func (colorOnEnv) Getenv(key string) string {
	if key == "NO_COLOR" || key == "CLICOLOR" {
		return ""
	}
	return os.Getenv(key)
}
