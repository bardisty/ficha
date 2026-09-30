package cmd

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

// stdoutIsTerminal is isTerminal, swappable because tests can't give ficha a
// terminal.
var stdoutIsTerminal = isTerminal

// restoreColorProfile puts back the profile the last run's override replaced.
var restoreColorProfile = func() {}

// applyColorOverride makes an explicit --no-color=false on a terminal show
// color even when NO_COLOR, CLICOLOR=0 or CI would turn it off. Keeping
// cfg.noColor false isn't enough: lipgloss reads all three on its own, through
// termenv's EnvColorProfile. So the profile is picked again the way lipgloss
// picks it, CLICOLOR_FORCE included, with NO_COLOR and CLICOLOR hidden.
// WithTTY(true) skips the CI check in termenv's terminal test, but it skips
// the real terminal test too, so stdout is tested here first. Piped output
// stays plain.
//
// The default renderer is process-wide and Execute is re-entrant, so every
// run first undoes the previous run's override, background included.
func applyColorOverride(cmd *cobra.Command, cfg *config) {
	restoreColorProfile()
	restoreColorProfile = func() {}
	if cfg.noColor || !cmd.Flags().Changed("no-color") || !stdoutIsTerminal(cfg.stdout) {
		return
	}
	out := termenv.NewOutput(cfg.stdout, termenv.WithTTY(true), termenv.WithEnvironment(colorOnEnv{}))
	r := lipgloss.DefaultRenderer()
	prev, prevDark := r.ColorProfile(), r.HasDarkBackground()
	r.SetColorProfile(out.EnvColorProfile())
	// With CI set, termenv didn't ask the terminal for its background when
	// Bubble Tea's init first wanted it, so the renderer kept the dark
	// default. Ask now, as a run without CI already did at init. Without
	// CI the renderer has the terminal's answer, and asking again would
	// query the terminal twice. json and csv have no color to pick, so they
	// skip the query and the wait it can cost on a pty nobody answers.
	if os.Getenv("CI") != "" && strings.EqualFold(cfg.format, "table") {
		r.SetHasDarkBackground(out.HasDarkBackground())
	}
	restoreColorProfile = func() {
		r.SetColorProfile(prev)
		r.SetHasDarkBackground(prevDark)
	}
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
