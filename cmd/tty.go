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
	if f, ok := stdout.(*os.File); ok && term.IsTerminal(f.Fd()) {
		return nil
	}
	return fmt.Errorf("%s needs an interactive terminal, and stdout isn't one. %s", command, alternative)
}
