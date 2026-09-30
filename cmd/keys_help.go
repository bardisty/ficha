package cmd

import (
	"strings"

	"github.com/bardisty/ficha/internal/tui"
)

// keysHelp is the Keys: block in watch's and breakdown's --help, after the
// examples, built from the view's key table so it can't drift from what the view does.
func keysHelp(keys []tui.Key) string {
	width := 0
	for _, k := range keys {
		width = max(width, len(k.Keys))
	}
	var sb strings.Builder
	sb.WriteString("Keys:")
	for _, k := range keys {
		sb.WriteString("\n  " + k.Keys + strings.Repeat(" ", width-len(k.Keys)) + "  " + k.Does)
		if k.Note != "" {
			sb.WriteString("; " + k.Note)
		}
	}
	return sb.String()
}
