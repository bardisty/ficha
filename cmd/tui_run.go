package cmd

import (
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
	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}
	return nil
}
