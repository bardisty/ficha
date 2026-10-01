package tui

import (
	"strings"
	"testing"
)

// --no-color must emit no escape codes in the TUIs, including the watch
// chart and footer stats line. A view writes an escape for every styled
// call, whatever the terminal, so one left in a no-color branch shows up
// here.
func TestNoColorTUIsEmitNoEscapes(t *testing.T) {
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
