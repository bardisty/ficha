// Package styles is the look every report and live view shares: the color
// palette, with a dark and a light value for each color, the lipgloss styles
// built from it, and the glyph set (Unicode box drawing, or ASCII under
// --ascii).
//
// Every lipgloss style lives here, including the few only the live views use.
// internal/tui/styles.go gives them short local names and adds the live
// views' spinner, marker and footnote helpers.
//
// The light-or-dark choice and the glyph set are process-wide. The terminal
// background is detected once, before anything renders, and cmd picks the
// glyph set at startup. Color on or off isn't stored here: callers pass
// noColor down and skip the styles.
package styles
