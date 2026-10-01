package cmd

import (
	"errors"
	"fmt"
	"runtime"

	tea "charm.land/bubbletea/v2"
)

// Title stack sequences (xterm window ops). Terminals without a title
// stack ignore them.
const (
	pushTitle = "\x1b[22;0t"
	popTitle  = "\x1b[23;0t"
)

// tuiFPS caps how often the renderer wakes to compare frames, which it does
// even when nothing changed. Bubble Tea's default of 60 costs an idle view
// about 50% more CPU. At 20 a held j starts to lag.
const tuiFPS = 30

// exitInterrupted is the shell's status for a process ended by SIGINT.
const exitInterrupted = 130

// The size a live view draws at on a terminal that reports none.
const (
	fallbackWidth  = 80
	fallbackHeight = 24
)

// runTUI runs a live view full-screen. It takes the view unbuilt: a view
// copies its spinner's and chart's colors when it's built, so the palette
// has to be picked first.
//
// watch sets the terminal title, and Bubble Tea blanks it on exit, and while
// the view is stopped, instead of putting the old one back. So the title is
// pushed before the program starts and popped after it exits. cfg.stdout
// must be the terminal (callers check with requireTerminal). Skipped on
// Windows, where the console may not interpret escapes until Bubble Tea
// enables VT processing, so the sequence could print as text.
//
// The program gets the run's color profile. Left to detect one, Bubble Tea
// would not apply ficha's rules for CI or --no-color=false.
//
// A pty can report zero rows or columns: bare `script`, some CI shells, a
// serial console. Bubble Tea draws into a buffer of the size reported, so
// there it would draw nothing. sizeOrFallback gives it a size to draw at.
func runTUI(cfg *config, build func() tea.Model) error {
	model := liveView(cfg, build)
	if runtime.GOOS != "windows" {
		fmt.Fprint(cfg.stdout, pushTitle)
		defer fmt.Fprint(cfg.stdout, popTitle)
	}
	_, err := tea.NewProgram(model, tea.WithFPS(tuiFPS), tea.WithColorProfile(cfg.profile), tea.WithFilter(sizeOrFallback)).Run()
	return tuiError(err)
}

// liveView picks the palette, then builds the view with it.
func liveView(cfg *config, build func() tea.Model) tea.Model {
	pickPalette(cfg)
	return build()
}

// sizeOrFallback replaces a dimension the terminal reported as zero.
func sizeOrFallback(_ tea.Model, msg tea.Msg) tea.Msg {
	size, ok := msg.(tea.WindowSizeMsg)
	if !ok {
		return msg
	}
	if size.Width <= 0 {
		size.Width = fallbackWidth
	}
	if size.Height <= 0 {
		size.Height = fallbackHeight
	}
	return size
}

// tuiError maps how a live view ended to what ficha exits with. A SIGINT
// from outside, sent by `kill -INT`, a process manager or an IDE's stop
// button, is a request to stop, not a failure: exit 130 and print nothing.
// q and ctrl+c are key presses the models handle, so they end with no error.
func tuiError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, tea.ErrInterrupted) {
		return &exitError{code: exitInterrupted}
	}
	return fmt.Errorf("running TUI: %w", err)
}
