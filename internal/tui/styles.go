package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"

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
// under the ASCII glyph set, colored unless color is off.
func newSpinner(noColor bool) spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	if styles.ASCII() {
		s.Spinner = spinner.Line
	}
	if !noColor {
		s.Style = spinnerStyle
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

// Local aliases for frequently used styles
var (
	headerStyle        = styles.HeaderStyle
	heroCostStyle      = styles.HeroCostStyle
	tableBorderStyle   = styles.BorderStyle
	savingsLabelStyle  = styles.SavingsLabelStyle
	footerStyle        = styles.FooterStyle // No margin, just color
	liveIndicatorStyle = styles.LiveIndicatorStyle
	spinnerStyle       = styles.SpinnerStyle

	// Highlight styles for changed values
	highlightStyle = styles.HighlightStyle

	// Dim style for extra decimal precision
	dimStyle = styles.DimStyle

	// Section header style for bracketed section headers
	sectionHeaderStyle = styles.SectionHeaderStyle

	// Panel border style for header panel
	panelBorderStyle = styles.PanelBorderStyle
)
