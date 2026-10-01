package cmd

import (
	"regexp"
	"runtime"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/styles"
)

// lightTerminal fakes a terminal with a light background. Under a screen
// TERM termenv reads COLORFGBG and never queries the terminal, which a test
// has none of to ask. So a run that picks the palette ends up light, and one
// that doesn't stays dark: the palette says whether ficha asked.
func lightTerminal(t *testing.T) {
	t.Helper()
	fakeTerminal(t)
	t.Setenv("TERM", "screen-256color")
	t.Setenv("COLORFGBG", "0;15")
}

// A table drawn in color takes the terminal's background. termenv reads none
// on Windows, so there every terminal gets the dark palette.
func TestColoredTablePicksThePalette(t *testing.T) {
	setupE2EFixture(t)
	lightTerminal(t)

	showOut(t)
	if dark, want := styles.Dark(), runtime.GOOS == "windows"; dark != want {
		t.Errorf("COLORFGBG=0;15 on %s: dark palette %v, want %v", runtime.GOOS, dark, want)
	}

	t.Setenv("COLORFGBG", "15;0")
	showOut(t)
	if !styles.Dark() {
		t.Error("COLORFGBG=15;0 should give the dark palette")
	}

	// With CI set, --no-color=false reads the background the way a run
	// without CI does.
	t.Setenv("COLORFGBG", "0;15")
	t.Setenv("CI", "1")
	showOut(t, "--no-color=false")
	if dark, want := styles.Dark(), runtime.GOOS == "windows"; dark != want {
		t.Errorf("--no-color=false under CI on %s: dark palette %v, want %v", runtime.GOOS, dark, want)
	}
}

// A run that prints no color has no use for the background, and asking can
// cost a wait on a terminal that never answers. Each of these leaves the
// palette at the dark one every run starts with, the run after a light one
// included.
func TestPlainRunsDoNotAskForTheBackground(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("termenv reads no background on Windows, so no run can show that it asked")
	}
	setupE2EFixture(t)
	lightTerminal(t)

	for _, tt := range []struct {
		name string
		env  [2]string
		args []string
	}{
		{name: "json", args: []string{"show", projFlag, e2eAlphaID, "-f", "json"}},
		{name: "csv", args: []string{"show", projFlag, e2eAlphaID, "-f", "csv"}},
		{name: "--no-color", args: []string{"show", projFlag, e2eAlphaID, "--no-color"}},
		{name: "NO_COLOR", env: [2]string{"NO_COLOR", "1"}, args: []string{"show", projFlag, e2eAlphaID}},
		{name: "CLICOLOR=0", env: [2]string{"CLICOLOR", "0"}, args: []string{"show", projFlag, e2eAlphaID}},
		{name: "CI", env: [2]string{"CI", "1"}, args: []string{"show", projFlag, e2eAlphaID}},
		{name: "a dumb terminal", env: [2]string{"TERM", "dumb"}, args: []string{"show", projFlag, e2eAlphaID}},
		{name: "version", args: []string{"version"}},
		{name: "a help topic", args: []string{"help", "output"}},
		{name: "a command's help", args: []string{"help", "show"}},
		{name: "a session that doesn't exist", args: []string{"show", projFlag, "no-such-session"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			showOut(t)
			if styles.Dark() {
				t.Fatal("the colored table before this run should have picked the light palette")
			}
			if tt.env[0] != "" {
				t.Setenv(tt.env[0], tt.env[1])
			}
			_, _, _ = executeCLISplit(t, tt.args...)
			if !styles.Dark() {
				t.Error("the run asked for the background")
			}
		})
	}
}

var basicColor = regexp.MustCompile(`\x1b\[(?:1;)?(?:3|9)[0-7]m`)

// The run after one on a 16-color terminal gets its own palette back.
func TestBasicPaletteDoesNotLeak(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("termenv goes by the Windows version there, not TERM")
	}
	setupE2EFixture(t)
	fakeTerminal(t)
	t.Setenv("TERM", "xterm")
	if out := showOut(t); strings.Contains(out, "38;5;") {
		t.Fatalf("TERM=xterm should draw basic colors:\n%q", out)
	}
	// Every run starts from the 256-color palette, one that draws nothing
	// in color included.
	showOut(t, "--no-color")
	if got, want := styles.WarningColor, lipgloss.Color("221"); got != want {
		t.Errorf("after a plain run the warning color is %v, want %v", got, want)
	}
	t.Setenv("TERM", "xterm-256color")
	if out := showOut(t); !strings.Contains(out, "38;5;245m") {
		t.Errorf("a 256-color run after a 16-color one should draw the 256-color palette:\n%q", out)
	}
}

// A terminal with 16 colors, and a pipe that CLICOLOR_FORCE colors, get the
// basic colors and nothing a 16-color terminal can't draw.
func TestBasicTerminalGetsBasicColors(t *testing.T) {
	setupE2EFixture(t)
	for name, setup := range map[string]func(t *testing.T){
		"TERM=xterm": func(t *testing.T) {
			fakeTerminal(t)
			t.Setenv("TERM", "xterm")
		},
		"a pipe with CLICOLOR_FORCE": func(t *testing.T) {
			t.Setenv("TERM", "xterm-256color")
			t.Setenv("CI", "")
			t.Setenv("CLICOLOR_FORCE", "1")
		},
	} {
		t.Run(name, func(t *testing.T) {
			if name == "TERM=xterm" && runtime.GOOS == "windows" {
				t.Skip("termenv goes by the Windows version there, not TERM")
			}
			setup(t)
			t.Cleanup(func() { styles.SetBasic(false) })
			out := showOut(t)
			if !basicColor.MatchString(out) {
				t.Errorf("no basic-color escapes:\n%q", out)
			}
			if strings.Contains(out, "38;5;") || strings.Contains(out, "38;2;") {
				t.Errorf("256-color or truecolor escapes on a 16-color output:\n%q", out)
			}
			// The palette picks the basic colors itself. Left to the
			// writer, the yellow would come out bright red.
			if got, want := styles.WarningColor, lipgloss.Color("11"); got != want {
				t.Errorf("warning color is %v, want bright yellow (%v)", got, want)
			}
		})
	}
}
