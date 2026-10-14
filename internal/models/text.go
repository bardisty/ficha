package models

import (
	"strings"
	"unicode"
)

// StripControl replaces control characters with spaces. Titles, project
// paths and agent descriptions come from transcripts, so one could otherwise
// move the cursor or recolor the terminal, and throw off the width of the
// line it's in.
func StripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// OneLine makes transcript text safe to print on one line: control
// characters and newlines become spaces, and runs of whitespace collapse to
// one.
func OneLine(s string) string {
	return strings.Join(strings.Fields(StripControl(s)), " ")
}
