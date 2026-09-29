package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/bardisty/ficha/internal/styles"
)

// unknownModelMarker tags a model whose cost came from the fallback pricing
// table rather than the catalog. show/summary/global say so on stderr; a TUI
// owns the screen, so the marker plus the footnote below is its only channel.
// One ASCII cell in both modes, so the fixed-width MODEL columns stay aligned.
const unknownModelMarker = "*"

// unknownModelFootnote explains unknownModelMarker in a footer.
func unknownModelFootnote() string {
	return styles.Warning + " " + unknownModelMarker + " = fallback pricing"
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

// helpLine joins key hints with the glyph set's bullet.
func helpLine(hints ...string) string {
	return strings.Join(hints, " "+styles.Bullet+" ")
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
