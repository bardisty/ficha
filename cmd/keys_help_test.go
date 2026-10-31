package cmd

import (
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/tui"
)

// A key row too long for helpWidth puts its note on the lines after, under
// the text after the keys, wrapped between words. A short one stays on its
// line.
func TestKeysHelpWrapsLongRows(t *testing.T) {
	got := keysHelp([]tui.Key{
		{Keys: "q", Does: "quit", Note: "or ctrl+c"},
		{Keys: "ctrl+z", Does: "suspend", Note: strings.Repeat("word ", 20) + "end"},
	})
	lines := strings.Split(got, "\n")
	if lines[0] != "Keys:" || lines[1] != "  q       quit; or ctrl+c" || lines[2] != "  ctrl+z  suspend;" {
		t.Fatalf("got:\n%s", got)
	}
	if !strings.HasSuffix(got, " end") || len(lines) < 5 {
		t.Fatalf("no continuation line:\n%s", got)
	}
	for _, line := range lines[3:] {
		if !strings.HasPrefix(line, strings.Repeat(" ", 10)) || line[10] == ' ' {
			t.Errorf("continuation %q isn't under the text after the keys", line)
		}
	}
	for _, line := range lines {
		if len(line) > helpWidth {
			t.Errorf("%d columns: %q", len(line), line)
		}
	}
}
