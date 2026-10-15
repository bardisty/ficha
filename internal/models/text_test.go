package models

import "testing"

// StripControl turns control characters into spaces, drops bidi
// formatting characters, and leaves the joiners and selectors that hold
// emoji together.
func TestStripControl(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"plain", "plain"},
		{"a\x1b[31mred\x07", "a [31mred "},
		{"Fix \u202eparser tests", "Fix parser tests"},
		{"\u05e9\u05dc\u05d5\u05dd\u200f, \u200eleft", "\u05e9\u05dc\u05d5\u05dd, left"},
		{"\u061c\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069", ""},
		{"\U0001F469\u200d\U0001F4BB", "\U0001F469\u200d\U0001F4BB"},
		{"☀\ufe0f", "☀\ufe0f"},
		{"zero\u200bwidth", "zero\u200bwidth"},
	} {
		if got := StripControl(tt.in); got != tt.want {
			t.Errorf("StripControl(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// OneLine collapses what StripControl leaves into single spaces.
func TestOneLine(t *testing.T) {
	if got, want := OneLine("Fix \u202e parser\n\ttests "), "Fix parser tests"; got != want {
		t.Errorf("OneLine = %q, want %q", got, want)
	}
}
