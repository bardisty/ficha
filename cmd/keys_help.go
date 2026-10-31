package cmd

import (
	"strings"

	"github.com/bardisty/ficha/internal/tui"
)

// helpWidth is the widest a help line gets, so a page fits an 80-column
// terminal with a column to spare on each side.
const helpWidth = 78

// keysHelp is the Keys: block in watch's and breakdown's --help, after the
// examples, built from the view's key table so it can't drift from what the
// view does. A row too long for helpWidth puts its note on the lines after,
// under the text after its keys, wrapped between words.
func keysHelp(keys []tui.Key) string {
	width := 0
	for _, k := range keys {
		width = max(width, len(k.Keys))
	}
	indent := strings.Repeat(" ", 2+width+2)
	var sb strings.Builder
	sb.WriteString("Keys:")
	for _, k := range keys {
		line := "  " + k.Keys + strings.Repeat(" ", width-len(k.Keys)) + "  " + k.Does
		if k.Note == "" {
			sb.WriteString("\n" + line)
			continue
		}
		if len(line)+len("; ")+len(k.Note) <= helpWidth {
			sb.WriteString("\n" + line + "; " + k.Note)
			continue
		}
		sb.WriteString("\n" + line + ";")
		line = indent
		for _, word := range strings.Fields(k.Note) {
			if len(line)+1+len(word) > helpWidth && line != indent {
				sb.WriteString("\n" + line)
				line = indent
			}
			if line != indent {
				line += " "
			}
			line += word
		}
		sb.WriteString("\n" + line)
	}
	return sb.String()
}
