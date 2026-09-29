package cmd

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// forceColor makes lipgloss emit escapes even though tests have no TTY, so
// a test can tell whether ficha itself turned color off.
func forceColor(t *testing.T) {
	t.Helper()
	r := lipgloss.DefaultRenderer()
	orig := r.ColorProfile()
	r.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { r.SetColorProfile(orig) })
}

func TestNoColorEnvMatchesFlag(t *testing.T) {
	setupE2EFixture(t)
	forceColor(t)

	colored, _, err := executeCLISplit(t, "show", projFlag, e2eAlphaID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colored, "\x1b[") {
		t.Fatal("forced color profile should produce escapes without NO_COLOR")
	}

	flag, _, err := executeCLISplit(t, "show", projFlag, e2eAlphaID, "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NO_COLOR", "1")
	env, _, err := executeCLISplit(t, "show", projFlag, e2eAlphaID)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(flag, "\x1b[") || strings.Contains(env, "\x1b[") {
		t.Error("--no-color and NO_COLOR must both emit no escapes")
	}
	if flag != env {
		t.Errorf("NO_COLOR output differs from --no-color:\n--no-color:\n%s\nNO_COLOR:\n%s", flag, env)
	}
	// Color off keeps the Unicode frames
	if !strings.Contains(env, "╔") {
		t.Errorf("no-color should keep Unicode frames:\n%s", env)
	}
}

// NO_COLOR set to the empty string doesn't count (no-color.org).
func TestEmptyNoColorEnvKeepsColor(t *testing.T) {
	setupE2EFixture(t)
	forceColor(t)
	t.Setenv("NO_COLOR", "")
	out, _, err := executeCLISplit(t, "show", projFlag, e2eAlphaID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Error("an empty NO_COLOR must not disable color")
	}
}

func TestASCIIFlag(t *testing.T) {
	setupE2EFixture(t)
	forceColor(t)

	out, _, err := executeCLISplit(t, "show", projFlag, e2eAlphaID, "--ascii")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "==========") || strings.Contains(out, "═") {
		t.Errorf("--ascii should draw ASCII frames:\n%s", out)
	}
	for _, r := range out {
		if r > 0x7e {
			t.Errorf("--ascii output contains non-ASCII rune %q:\n%s", r, out)
			break
		}
	}
	// --ascii alone keeps color
	if !strings.Contains(out, "\x1b[") {
		t.Error("--ascii should not turn color off")
	}

	// The glyph set is process-wide; the next run must not inherit it.
	out, _, err = executeCLISplit(t, "show", projFlag, e2eAlphaID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "╔") {
		t.Errorf("a run without --ascii must draw Unicode frames again:\n%s", out)
	}
}

func TestASCIIWithNoColor(t *testing.T) {
	setupE2EFixture(t)
	forceColor(t)
	out, _, err := executeCLISplit(t, "summary", projFlag, "-d", "--expand-agents", "--ascii", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range out {
		if r > 0x7e && r != '\n' {
			t.Errorf("--ascii --no-color output contains non-ASCII rune %q:\n%s", r, out)
			break
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("--no-color must emit no escapes alongside --ascii")
	}
}

// An explicit --no-color=false beats NO_COLOR (no-color.org: flags override).
func TestExplicitColorFlagOverridesNoColorEnv(t *testing.T) {
	setupE2EFixture(t)
	forceColor(t)
	t.Setenv("NO_COLOR", "1")
	out, _, err := executeCLISplit(t, "show", projFlag, e2eAlphaID, "--no-color=false")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Error("--no-color=false should keep color despite NO_COLOR")
	}
}
