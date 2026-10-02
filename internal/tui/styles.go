package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"

	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// unknownModelMarker tags a model whose cost came from the fallback pricing
// table rather than the catalog. show/summary/global say so on stderr; a TUI
// owns the screen, so the marker plus the footnote below is its only channel.
// One ASCII cell in both modes, so the fixed-width MODEL columns stay aligned.
const unknownModelMarker = "*"

// unknownModelFootnote explains unknownModelMarker in a footer, naming the
// models: "⚠ * claude-nova-9: fallback pricing, see ficha show 0a1b2c3d".
// The ID is what a pricing update needs, and a TUI owns the screen, so the
// stderr warning that names it elsewhere never shows here. ficha show prints
// that warning: the version that lacks the price, and where newer releases
// are. The footer has no room for a URL. Past two IDs the rest are counted.
func unknownModelFootnote(ids []string, sessionID string) string {
	if len(ids) == 0 {
		return styles.Warning + " " + unknownModelMarker + " = fallback pricing"
	}
	named := strings.Join(ids[:min(2, len(ids))], ", ")
	if len(ids) > 2 {
		named += fmt.Sprintf(" +%d more", len(ids)-2)
	}
	return styles.Warning + " " + unknownModelMarker + " " + named + ": fallback pricing" + unknownModelPointer(sessionID)
}

// unknownModelPointer ends unknownModelFootnote. It names the view's session:
// ficha show on its own opens the newest one, which a pinned view may not be
// on. The ID's prefix works from the project's directory, where the view was
// started. A line with no room for it drops it rather than clip it to "see
// ficha".
func unknownModelPointer(sessionID string) string {
	return ", see ficha show " + render.TruncateID(sessionID, sessionIDDisplayLen)
}

// newSpinner returns the loading spinner: braille dots, or a spinning line
// under the ASCII glyph set, colored unless color is off. Its frames are the
// glyph alone. The dot frames come padded with a space, and the spinner's
// style wraps a frame whole, so a caller couldn't trim it from the view. The
// header puts the gap after the spinner, the same for both sets.
func newSpinner(noColor bool) spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	if styles.ASCII() {
		// The line without its upright frame: that one is the header's
		// ASCII separator, and "| * FOLLOWING | | loading" reads as an
		// empty cell.
		s.Spinner = spinner.Spinner{Frames: []string{"/", "-", `\`}, FPS: spinner.Line.FPS}
	}
	// A new slice: the frames belong to the spinner package.
	frames := make([]string, len(s.Spinner.Frames))
	for i, f := range s.Spinner.Frames {
		frames[i] = strings.TrimRight(f, " ")
	}
	s.Spinner.Frames = frames
	if !noColor {
		s.Style = styles.SpinnerStyle
	}
	return s
}

// accountingFootnote reports what a TUI's totals could not account for
// exactly — skipped inputs (missing from the totals) and estimated costs
// (included, but priced on the 5m cache-write assumption) — returning ""
// when there is nothing to report. All counts share one footer segment
// rather than claiming a separator each: the footer is a single line, and it
// already carries the fallback-pricing footnote.
func accountingFootnote(skippedAgents, skippedLines, estimatedCosts int) string {
	if skippedAgents == 0 && skippedLines == 0 && estimatedCosts == 0 {
		return ""
	}
	var parts []string
	if skippedAgents > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped agent(s)", skippedAgents))
	}
	if skippedLines > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped line(s)", skippedLines))
	}
	if estimatedCosts > 0 {
		parts = append(parts, fmt.Sprintf("%d estimated cost(s)", estimatedCosts))
	}
	return styles.Warning + " " + strings.Join(parts, ", ")
}
