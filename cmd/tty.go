package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
)

// requireTerminal fails fast when the live view would render into a pipe or
// file. Bubble Tea would otherwise write every frame into it while the
// terminal sits blank with no prompt, and with no TTY at all it fails with a
// library error about /dev/tty.
func requireTerminal(stdout io.Writer, command, alternative string) error {
	if isTerminal(stdout) {
		return nil
	}
	return fmt.Errorf("%s needs an interactive terminal, and stdout isn't one. %s", command, alternative)
}

// isTerminal reports whether w is a terminal. Test buffers never are.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(f.Fd())
}
