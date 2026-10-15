package models

import "testing"

// StripControl turns control characters and bidi formatting characters
// into spaces, and leaves the joiners and selectors that hold emoji
// together.
func TestStripControl(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"plain", "plain"},
		{"a\x1b[31mred\x07", "a [31mred "},
		{"Fix ‮parser tests", "Fix  parser tests"},
		{"‎left‏right", " left right"},
		{"‪‫‬‭‮", "     "},
		{"⁦⁧⁨⁩", "    "},
		{"\U0001F469‍\U0001F4BB", "\U0001F469‍\U0001F4BB"},
		{"☀️", "☀️"},
		{"zero​width", "zero​width"},
	} {
		if got := StripControl(tt.in); got != tt.want {
			t.Errorf("StripControl(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// OneLine collapses what StripControl leaves into single spaces.
func TestOneLine(t *testing.T) {
	if got, want := OneLine("Fix ‮ parser\n\ttests "), "Fix parser tests"; got != want {
		t.Errorf("OneLine = %q, want %q", got, want)
	}
}
