package cmd

import (
	"runtime"
	"testing"

	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
)

// With CI set, --no-color=false reads the background from the terminal the
// way a run without CI does. TERM=screen makes termenv read COLORFGBG rather
// than query the terminal, which a test has none of to ask. termenv reads no
// background on Windows, so there every terminal gets the dark palette.
func TestColorOverrideReadsBackgroundUnderCI(t *testing.T) {
	setupE2EFixture(t)
	fakeTerminal(t)
	t.Setenv("TERM", "screen")
	t.Setenv("CI", "1")
	r := lipgloss.DefaultRenderer()

	t.Setenv("COLORFGBG", "0;15")
	showOut(t, "--no-color=false")
	if dark, want := r.HasDarkBackground(), runtime.GOOS == "windows"; dark != want {
		t.Errorf("COLORFGBG=0;15 on %s: dark background %v, want %v", runtime.GOOS, dark, want)
	}
	if styles.Dark() != r.HasDarkBackground() {
		t.Errorf("the palette is dark=%v on a background that is dark=%v", styles.Dark(), r.HasDarkBackground())
	}

	t.Setenv("COLORFGBG", "15;0")
	showOut(t, "--no-color=false")
	if !r.HasDarkBackground() {
		t.Error("COLORFGBG=15;0 should give the dark palette")
	}

	// json has no color, so it doesn't ask.
	t.Setenv("COLORFGBG", "0;15")
	showOut(t, "--no-color=false", "-f", "json")
	if !r.HasDarkBackground() {
		t.Error("json output shouldn't read the background")
	}

	// The next run in the process gets its own background back.
	t.Setenv("COLORFGBG", "0;15")
	showOut(t, "--no-color=false")
	showOut(t)
	if !r.HasDarkBackground() || !styles.Dark() {
		t.Error("a run after an override must not inherit its light background")
	}
}

// Without CI, the background the renderer already has stands. A run without
// CI asked the terminal at init, and asking again would query it twice.
func TestColorOverrideKeepsBackgroundWithoutCI(t *testing.T) {
	setupE2EFixture(t)
	fakeTerminal(t)
	t.Setenv("TERM", "screen")
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("COLORFGBG", "0;15")
	showOut(t, "--no-color=false")
	if !lipgloss.DefaultRenderer().HasDarkBackground() {
		t.Error("the override changed the background without CI")
	}
}
