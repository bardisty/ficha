package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/bardisty/ficha/internal/styles"
)

// Key is one row of a live view's key table: keys that do one thing, as a
// user types them, and what they do. The help line, the ? list, each view's
// --help and the README are all built from these tables, so they can't
// disagree.
type Key struct {
	Keys string
	Does string
	// Note is a caveat for the docs. The ? list leaves it off: it lists
	// ctrl+z only where ctrl+z works.
	Note string

	hint    string // the key's help-line form; "" keeps it off the line
	suspend bool   // listed in ? only where canSuspend
}

// The rows both views share, in the same words.
var (
	quitKey   = Key{Keys: "q, ctrl+c", Does: "quit", hint: "q quit"}
	listKey   = Key{Keys: "?", Does: "key list on/off; esc closes"}
	lineKey   = Key{Keys: "j/k, up/down", Does: "scroll a line", hint: "j/k scroll"}
	pageKey   = Key{Keys: "space/b, pgdn/pgup", Does: "scroll a page", hint: "space/b page"}
	halfKey   = Key{Keys: "d/u, ctrl+d/u", Does: "scroll half a page"}
	endsKey   = Key{Keys: "g/G, home/end", Does: "go to the top or bottom", hint: "g/G top/bottom"}
	followKey = Key{Keys: "f", Does: "follow new sessions on/off", hint: "f follow"}
	hintedKey = Key{Keys: "n", Does: "switch to the newer session"}
	backKey   = Key{Keys: "-", Does: "back to the previous session"}
	reloadKey = Key{Keys: "r", Does: "reload or retry"}
	stopKey   = Key{Keys: "ctrl+z", Does: "suspend", Note: "needs a shell with job control, so not on Windows", suspend: true}
)

// The help line lists the hinted rows in table order, dropping from the
// right on a narrow terminal (see helpLine), then ends with keysHint, which
// it drops last: it's the way to the rest. closeHint takes its place while the list is open,
// and both states leave room for the wider, so opening the list never
// changes which hints show.
const (
	keysHint  = "? keys"
	closeHint = "? close"
)

// WatchKeys is watch's key table, in the order the ? list shows it.
func WatchKeys() []Key {
	return []Key{quitKey, listKey, lineKey, pageKey, halfKey, endsKey,
		followKey, hintedKey, backKey, reloadKey, stopKey}
}

// BreakdownKeys is breakdown's key table: watch's, plus p and s. Its p row
// carries the help-line hint for both.
func BreakdownKeys() []Key {
	return []Key{quitKey, listKey, lineKey, pageKey, halfKey, endsKey,
		{Keys: "p", Does: "go to the next costliest row", hint: "p/s peak/sort"},
		{Keys: "s", Does: "sort by cost on/off"},
		followKey, hintedKey, backKey, reloadKey, stopKey}
}

// helpLine is a view's key-hint row, indented and fitted to the terminal.
// A hint that doesn't fit goes whole, not cut mid-word, the last in the
// table first. While pinned, f follow is the key that resumes following, so
// it takes g/G top/bottom's place in that order and g/G drops first: the
// page keys get to the ends too. The hints that stay keep their table order.
func helpLine(keys []Key, width int, listOpen, pinned, noColor bool) string {
	var hints, order []string
	for _, k := range keys {
		if k.hint == "" {
			continue
		}
		hints = append(hints, k.hint)
		switch {
		case !pinned:
			order = append(order, k.hint)
		case k.hint == endsKey.hint:
			order = append(order, followKey.hint)
		case k.hint != followKey.hint:
			order = append(order, k.hint)
		}
	}
	if pinned {
		order = append(order, endsKey.hint)
	}
	sep := " " + styles.Bullet + " "
	tail := keysHint
	if listOpen {
		tail = closeHint
	}
	room := width - 2 - lipgloss.Width(sep) - max(lipgloss.Width(keysHint), lipgloss.Width(closeHint))
	if width <= 0 {
		room = 0
	}
	// The first hint stays even past room; q quit is short enough that it
	// never pushes the tail off a frame wide enough to draw.
	kept := map[string]bool{}
	shown := func() []string {
		var out []string
		for _, h := range hints {
			if kept[h] {
				out = append(out, h)
			}
		}
		return out
	}
	for _, h := range order {
		kept[h] = true
		if room > 0 && len(kept) > 1 && lipgloss.Width(strings.Join(shown(), sep)) > room {
			delete(kept, h)
			break
		}
	}
	text := strings.Join(shown(), sep) + sep + tail
	if !noColor {
		text = lipgloss.NewStyle().Foreground(styles.SecondaryColor).Render(text)
	}
	return "  " + text
}

// keyList draws the ? list in place of a view's frame above the help line,
// exactly rows lines tall so the help line stays at the bottom. It drops
// its title when the rows are short, and past that cuts the list with a
// pointer to --help, which has all of it. A terminal too narrow for every
// row whole gets each row's first keys only, and --help has the others.
func keyList(view string, keys []Key, width, rows int, noColor bool) []string {
	var shown []Key
	for _, k := range keys {
		if !k.suspend || canSuspend() {
			shown = append(shown, k)
		}
	}
	keyWidth, doesWidth := 0, 0
	for _, k := range shown {
		keyWidth = max(keyWidth, lipgloss.Width(k.Keys))
		doesWidth = max(doesWidth, lipgloss.Width(k.Does))
	}
	if width > 0 && 2+keyWidth+2+doesWidth > width {
		keyWidth = 0
		for i, k := range shown {
			shown[i].Keys, _, _ = strings.Cut(k.Keys, ", ")
			keyWidth = max(keyWidth, lipgloss.Width(shown[i].Keys))
		}
	}
	style := func(st lipgloss.Style, s string) string {
		if noColor {
			return s
		}
		return st.Render(s)
	}
	var entries []string
	for _, k := range shown {
		pad := strings.Repeat(" ", keyWidth-lipgloss.Width(k.Keys))
		entries = append(entries, "  "+style(sectionHeaderStyle, k.Keys)+pad+"  "+k.Does)
	}

	var lines []string
	switch {
	case len(entries) < rows:
		lines = append([]string{"  " + style(headerStyle, "Keys")}, entries...)
	case len(entries) <= rows:
		lines = entries
	default:
		keep := max(rows-1, 0)
		more := fmt.Sprintf("  %s %d more in ficha %s --help", styles.Ellipsis, len(entries)-keep, view)
		lines = append(entries[:keep:keep], style(dimStyle, more))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return lines[:max(rows, 0)]
}

// toggleKeyList handles a key for the ? list and returns whether the list
// is open after it. ? opens the list. While it's open, any key closes it: ?
// and esc do nothing else, and done says so. Every other key goes on to do
// what it always does, so a key read off the list works the first time.
// ctrl+z leaves the list open, to come back to after the resume.
func toggleKeyList(open bool, msg tea.KeyMsg) (nowOpen, done bool) {
	switch s := msg.String(); {
	case !open:
		return s == "?", s == "?"
	case s == "ctrl+z":
		return true, false
	default:
		return false, s == "?" || s == "esc"
	}
}
