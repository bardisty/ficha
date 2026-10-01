package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// startupQueriers are packages that ask the terminal for its background
// color while the program is still initializing, before main runs: Bubble
// Tea v1 from an init function, lipgloss v1 on its behalf, and lipgloss v2's
// compat package from a package variable. bubbles v1 is here because it
// imports the first two.
var startupQueriers = []string{
	"github.com/charmbracelet/bubbletea",
	"github.com/charmbracelet/lipgloss",
	"github.com/charmbracelet/bubbles",
	"charm.land/lipgloss/v2/compat",
}

// TestNothingQueriesTheTerminalAtStartup fails when ficha, or anything it
// depends on, links a package that queries the terminal during
// initialization. Go runs every linked package's initialization whichever
// command runs, so one such import makes version, --help, json and csv all
// send the query, and wait for an answer over ssh or on a pty that never
// gives one. The pty tests would see that query on Linux. This test names
// the package, and covers what only another platform links.
func TestNothingQueriesTheTerminalAtStartup(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			list := exec.Command("go", "list", "-deps", "./...")
			list.Dir = ".."
			list.Env = append(os.Environ(), "GOOS="+goos, "GOFLAGS=-mod=readonly")
			out, err := list.Output()
			if err != nil {
				t.Fatalf("go list -deps: %v", err)
			}
			for pkg := range strings.FieldsSeq(string(out)) {
				for _, q := range startupQueriers {
					if pkg == q || strings.HasPrefix(pkg, q+"/") {
						t.Errorf("%s is linked. Run `go mod why %s` to see what imports it", pkg, pkg)
					}
				}
			}
		})
	}
}
