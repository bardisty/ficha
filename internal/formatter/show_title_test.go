package formatter

import (
	"strings"
	"testing"
)

// A title is transcript text: its newlines and control characters become
// spaces, so it stays on its one line. A session without a title gets no
// line for one.
func TestShowHeaderTitleLine(t *testing.T) {
	a := goldenShowAnalysis()
	a.Title = "Fix the router\ntests\x1b[31m\tnow"
	header := renderHeaderPanel(a, 76, true)
	lines := strings.Split(header, "\n")
	if len(lines) != 4 || lines[2] != "║  Fix the router tests [31m now"+strings.Repeat(" ", 70-len("Fix the router tests [31m now"))+"  ║" {
		t.Errorf("want the title on one line of its own:\n%s", header)
	}

	a.Title = ""
	if lines := strings.Split(renderHeaderPanel(a, 76, true), "\n"); len(lines) != 3 {
		t.Errorf("a session without a title should keep a one-line panel:\n%s", strings.Join(lines, "\n"))
	}
}
