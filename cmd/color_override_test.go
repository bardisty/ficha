package cmd

import (
	"io"
	"strings"
	"testing"
)

// fakeTerminal makes ficha treat stdout as a terminal, which a test buffer
// never is.
func fakeTerminal(t *testing.T) {
	t.Helper()
	orig := stdoutIsTerminal
	stdoutIsTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() {
		stdoutIsTerminal = orig
		// An override's profile must not outlive the test that made it.
		restoreColorProfile()
		restoreColorProfile = func() {}
	})
	// termenv picks a terminal's profile from TERM; pin one with color.
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "")
}

func showOut(t *testing.T, args ...string) string {
	t.Helper()
	out, _, err := executeCLISplit(t, append([]string{"show", projFlag, e2eAlphaID}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// An explicit --no-color=false on a terminal beats every variable that turns
// color off (no-color.org: flags override). TestMain pins the profile to no
// color, which stands in for what lipgloss would detect from these variables.
func TestExplicitColorFlagOverridesEnv(t *testing.T) {
	setupE2EFixture(t)
	fakeTerminal(t)
	for _, env := range []string{"NO_COLOR", "CLICOLOR", "CI"} {
		t.Run(env, func(t *testing.T) {
			value := "1"
			if env == "CLICOLOR" {
				value = "0"
			}
			t.Setenv(env, value)
			if out := showOut(t); strings.Contains(out, "\x1b[") {
				t.Fatalf("%s=%s alone should leave color off", env, value)
			}
			if out := showOut(t, "--no-color=false"); !strings.Contains(out, "\x1b[") {
				t.Errorf("--no-color=false should turn color on over %s=%s", env, value)
			}
		})
	}
}

// The override changes the process-wide renderer. The next run in the same
// process must get its own profile back. CI, unlike NO_COLOR, leaves ficha's
// own noColor false, so only the renderer's profile keeps that run plain.
func TestColorOverrideDoesNotLeak(t *testing.T) {
	setupE2EFixture(t)
	fakeTerminal(t)
	t.Setenv("CI", "1")
	if out := showOut(t, "--no-color=false"); !strings.Contains(out, "\x1b[") {
		t.Fatal("--no-color=false should turn color on over CI")
	}
	if out := showOut(t); strings.Contains(out, "\x1b[") {
		t.Error("a run after an override must not inherit its color")
	}
}

// Piped output stays plain even with --no-color=false.
func TestColorOverrideNeedsTerminal(t *testing.T) {
	setupE2EFixture(t)
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "1")
	if out := showOut(t, "--no-color=false"); strings.Contains(out, "\x1b[") {
		t.Error("--no-color=false must not color output that isn't a terminal")
	}
}

// CLICOLOR=0 asks for no color the way NO_COLOR does, so it takes the same
// no-color layout, unless CLICOLOR_FORCE says otherwise.
func TestClicolorZeroMatchesNoColor(t *testing.T) {
	setupE2EFixture(t)
	forceColor(t)
	flag := showOut(t, "--no-color")
	t.Setenv("CLICOLOR", "0")
	env := showOut(t)
	if env != flag {
		t.Errorf("CLICOLOR=0 output differs from --no-color:\n--no-color:\n%s\nCLICOLOR=0:\n%s", flag, env)
	}
	t.Setenv("CLICOLOR_FORCE", "1")
	if out := showOut(t); !strings.Contains(out, "\x1b[") {
		t.Error("CLICOLOR_FORCE=1 should keep color over CLICOLOR=0")
	}
}

// The flag asks for color, so it never takes away what CLICOLOR_FORCE gives a
// terminal whose TERM names no colors.
func TestColorOverrideKeepsClicolorForce(t *testing.T) {
	setupE2EFixture(t)
	fakeTerminal(t)
	t.Setenv("TERM", "dumb")
	t.Setenv("CLICOLOR_FORCE", "1")
	if out := showOut(t, "--no-color=false"); !strings.Contains(out, "\x1b[") {
		t.Error("--no-color=false should keep CLICOLOR_FORCE's color on a dumb terminal")
	}
}
