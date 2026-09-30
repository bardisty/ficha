package cmd

import (
	"errors"
	"fmt"
	"io"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
)

// Title stack sequences (xterm window ops). Terminals without a title
// stack ignore them.
const (
	pushTitle = "\x1b[22;0t"
	popTitle  = "\x1b[23;0t"
)

// exitInterrupted is the shell's status for a process ended by SIGINT.
const exitInterrupted = 130

// runTUI runs a live view full-screen. watch sets the terminal title, and
// bubbletea can't put the old one back, so the title is pushed before the
// program starts and popped after it exits. w must be the terminal (callers
// check with requireTerminal). Skipped on Windows, where the console may
// not interpret escapes until bubbletea enables VT processing, so the
// sequence could print as text.
func runTUI(w io.Writer, model tea.Model) error {
	if runtime.GOOS != "windows" {
		fmt.Fprint(w, pushTitle)
		defer fmt.Fprint(w, popTitle)
	}
	_, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	return tuiError(err)
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
