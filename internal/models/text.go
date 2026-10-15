package models

import (
	"strings"
	"unicode"
)

// StripControl replaces control characters with spaces. Titles, project
// paths and agent descriptions come from transcripts, so one could otherwise
// move the cursor or recolor the terminal, and throw off the width of the
// line it's in. Bidirectional formatting characters go too, since an
// override turns the rest of the line around, ficha's own columns included.
// They take no room, so they're dropped rather than spaced: right-to-left
// text sets a mark beside punctuation, where a space would show. Other
// format characters stay, since the zero-width joiner holds emoji sequences
// together.
func StripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if isBidiControl(r) {
			return -1
		}
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// isBidiControl reports whether r is a bidirectional mark (ALM, LRM, RLM),
// an embedding or override (U+202A to U+202E) or an isolate (U+2066 to
// U+2069).
func isBidiControl(r rune) bool {
	return r == '\u061c' || r == '\u200e' || r == '\u200f' || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069')
}

// OneLine makes transcript text safe to print on one line: control
// characters and newlines become spaces, and runs of whitespace collapse to
// one.
func OneLine(s string) string {
	return strings.Join(strings.Fields(StripControl(s)), " ")
}
