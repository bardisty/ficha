package cmd

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins the color state a developer's shell and terminal would
// otherwise decide. NO_COLOR turns ficha's color off, and CLICOLOR_FORCE, or
// running `go test` with no package argument so stdout is the terminal, makes
// lipgloss emit escapes into output the e2e tests expect plain. Tests that
// need color force a profile, and tests that need NO_COLOR set it with
// t.Setenv.
//
// The dark background matches the other packages' TestMains. Bubble Tea
// still asks the terminal for its background during package init, which
// nothing here can prevent.
func TestMain(m *testing.M) {
	for _, v := range []string{"NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE"} {
		_ = os.Unsetenv(v)
	}
	lipgloss.SetColorProfile(termenv.Ascii)
	lipgloss.SetHasDarkBackground(true)
	os.Exit(m.Run())
}
