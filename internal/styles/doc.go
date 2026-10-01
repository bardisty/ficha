// Package styles is the look every report and live view shares: the color
// palette, with a dark and a light value for each color, the lipgloss styles
// built from it, and the glyph set (Unicode box drawing, or ASCII under
// --ascii).
//
// Every lipgloss style lives here, including the few only the live views use.
// internal/tui/styles.go adds the live views' spinner, marker and footnote
// helpers.
//
// The glyph set and the palette are process-wide. cmd picks the glyph set at
// startup, with SetASCII, and the palette right before a report or live view
// draws in color, with SetDark and SetBasic. Those two rebuild the colors
// and styles, so callers read them from this package each time and keep no
// copies. Color on or off isn't stored here: callers pass noColor down and
// skip the styles.
package styles
