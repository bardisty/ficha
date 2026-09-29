package tui

import (
	"strings"
	"testing"

	"github.com/muesli/termenv"
)

// --no-color must emit no escape codes in the TUIs even when the terminal
// supports color, including the watch chart and footer stats line.
func TestNoColorTUIsEmitNoEscapes(t *testing.T) {
	forceProfile(t, termenv.ANSI256)
	for _, ascii := range []bool{false, true} {
		if ascii {
			useASCII(t)
		}
		for name, out := range map[string]string{
			"watch":     goldenWatchView(t, true),
			"breakdown": goldenBreakdownView(t, true),
		} {
			if i := strings.Index(out, "\x1b["); i >= 0 {
				t.Errorf("ascii=%v %s: escape code in no-color view near %q", ascii, name, out[max(0, i-20):min(len(out), i+20)])
			}
		}
	}
}
