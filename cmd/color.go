package cmd

import (
	"os"

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
// run first undoes the previous run's override.
func applyColorOverride(cmd *cobra.Command, cfg *config) {
	restoreColorProfile()
	restoreColorProfile = func() {}
	if cfg.noColor || !cmd.Flags().Changed("no-color") || !stdoutIsTerminal(cfg.stdout) {
		return
	}
	out := termenv.NewOutput(cfg.stdout, termenv.WithTTY(true), termenv.WithEnvironment(colorOnEnv{}))
	r := lipgloss.DefaultRenderer()
	prev := r.ColorProfile()
	r.SetColorProfile(out.EnvColorProfile())
	restoreColorProfile = func() { r.SetColorProfile(prev) }
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
