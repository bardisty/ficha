package cmd

import (
	"io"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/styles"
)

// fakeTerminal makes ficha treat stdout as a 256-color terminal, which a
// test buffer never is. The buffer still can't answer a query, so the
// background reads as dark unless a test sets COLORFGBG and a TERM under
// which termenv goes by it.
func fakeTerminal(t *testing.T) {
	t.Helper()
	orig := stdoutIsTerminal
	stdoutIsTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() {
		stdoutIsTerminal = orig
		// The palette a run picked must not outlive the test that ran it.
		styles.SetBasic(false)
		styles.SetDark(true)
	})
	// termenv picks a terminal's profile from TERM; pin one with color.
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "")
	t.Setenv("COLORFGBG", "")
	// CI turns color off on a terminal, and GitHub Actions sets it.
	t.Setenv("CI", "")
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
// color off (no-color.org: flags override).
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

// Each run decides its own profile. CI, unlike NO_COLOR, leaves ficha's own
// noColor false, so only the profile keeps a run under CI plain, and the run
// after an override must not get the override's.
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
	fakeTerminal(t)
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
