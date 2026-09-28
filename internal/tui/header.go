package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
)

// sessionIDDisplayLen is how many leading characters of a session ID the
// compact live header shows; the width-constrained live panel can't fit the
// full UUID that the static `show` header prints.
const sessionIDDisplayLen = 8

// liveHeaderParams is the per-frame state the shared live header renders. Both
// the watch and breakdown models fill it the same way so their headers stay
// identical.
type liveHeaderParams struct {
	sessionID     string
	prevSessionID string
	loading       bool
	err           error
	lastUpdated   time.Time
	spinnerView   string // m.spinner.View(); only shown while loading
	noColor       bool
	width         int
}

// renderLiveHeaderPanel renders the boxed live header shared by the watch and
// breakdown TUIs:
//
//	╔══════════════════════════════════════════════════════════╗
//	║  Session: xxx (prev: yyy)  │  ● LIVE  │  Updated: HH:MM:SS ║
//	╚══════════════════════════════════════════════════════════╝
func renderLiveHeaderPanel(p liveHeaderParams) string {
	var sb strings.Builder

	// Fallback for an absurdly small width; callers clamp to >= 40 via
	// panelWidthFor, so this rarely fires.
	width := p.width
	if width < 40 {
		width = 76
	}

	// Inner width (accounting for box borders and padding)
	innerWidth := width - 6 // 2 for borders, 2 for left padding, 2 for right padding

	// Fit the content to innerWidth *before* computing padding: full content
	// wider than innerWidth would clamp padding to 0 and push the right border
	// out of column on narrow terminals. Elide in order of least value
	// — drop the "(prev: …)" clause, then shorten "Updated: HH:MM:SS" to the
	// bare time — and hard-clip via lipgloss MaxWidth as a final guarantee.
	showPrev := p.prevSessionID != ""
	timeOnly := false
	content := buildLiveHeaderContent(p, showPrev, timeOnly)
	if lipgloss.Width(content) > innerWidth && showPrev {
		showPrev = false
		content = buildLiveHeaderContent(p, showPrev, timeOnly)
	}
	if lipgloss.Width(content) > innerWidth {
		timeOnly = true
		content = buildLiveHeaderContent(p, showPrev, timeOnly)
	}
	if lipgloss.Width(content) > innerWidth {
		content = lipgloss.NewStyle().MaxWidth(innerWidth).Render(content)
	}
	// Padding from rendered width — len() overcounts multi-byte UTF-8 chars like
	// ● (3 bytes, 1 column) and elided/styled content of varying display width.
	padding := max(innerWidth-lipgloss.Width(content), 0)

	if p.noColor {
		// Top border
		sb.WriteString("  ")
		sb.WriteString(styles.AsciiCorner)
		sb.WriteString(strings.Repeat(styles.AsciiHorizontal, width-2))
		sb.WriteString(styles.AsciiCorner)
		sb.WriteString("\n")

		// Content line
		sb.WriteString("  ")
		sb.WriteString(styles.AsciiVertical)
		sb.WriteString("  ")
		sb.WriteString(content)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString("  ")
		sb.WriteString(styles.AsciiVertical)
		sb.WriteString("\n")

		// Bottom border
		sb.WriteString("  ")
		sb.WriteString(styles.AsciiCorner)
		sb.WriteString(strings.Repeat(styles.AsciiHorizontal, width-2))
		sb.WriteString(styles.AsciiCorner)
	} else {
		// Top border
		sb.WriteString("  ")
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxTopRight))
		sb.WriteString("\n")

		// Content line
		sb.WriteString("  ")
		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("  ")
		sb.WriteString(content)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString("  ")
		sb.WriteString(panelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("\n")

		// Bottom border
		sb.WriteString("  ")
		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomLeft))
		sb.WriteString(panelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(panelBorderStyle.Render(styles.BoxBottomRight))
	}

	return sb.String()
}

// buildLiveHeaderContent assembles the header's single content line honoring the
// elision flags (showPrev, timeOnly) and p.noColor styling. The "●" bullet is
// kept even in noColor mode (it renders one column; lipgloss.Width, not len,
// measures it for padding).
func buildLiveHeaderContent(p liveHeaderParams, showPrev, timeOnly bool) string {
	sessionDisplay := render.TruncateID(p.sessionID, sessionIDDisplayLen)
	if showPrev && p.prevSessionID != "" {
		sessionDisplay = fmt.Sprintf("%s (prev: %s)",
			render.TruncateID(p.sessionID, sessionIDDisplayLen),
			render.TruncateID(p.prevSessionID, sessionIDDisplayLen))
	}
	livePart := "● LIVE"

	sep := styles.BoxVerticalSep
	if p.noColor {
		sep = styles.AsciiVertical
	}

	if p.noColor {
		var statusPart string
		switch {
		case p.loading:
			statusPart = "Loading..."
		case p.err != nil:
			statusPart = "Error"
		case timeOnly:
			statusPart = p.lastUpdated.Format("15:04:05")
		default:
			statusPart = fmt.Sprintf("Updated: %s", p.lastUpdated.Format("15:04:05"))
		}
		sessionPart := fmt.Sprintf("Session: %s", sessionDisplay)
		return fmt.Sprintf("%s  %s  %s  %s  %s", sessionPart, sep, livePart, sep, statusPart)
	}

	sessionStyled := fmt.Sprintf("%s %s", sectionHeaderStyle.Render("Session:"), sessionDisplay)
	liveStyled := liveIndicatorStyle.Render(livePart)
	var statusStyled string
	switch {
	case p.loading:
		// The spinner is wider than plain "Loading..."; padding is measured off
		// the rendered width so it can't push the right border out of alignment.
		statusStyled = p.spinnerView + " Loading..."
	case p.err != nil:
		statusStyled = lipgloss.NewStyle().Foreground(styles.ErrorColor).Render("Error")
	case timeOnly:
		statusStyled = p.lastUpdated.Format("15:04:05")
	default:
		statusStyled = fmt.Sprintf("Updated: %s", p.lastUpdated.Format("15:04:05"))
	}
	sepStyled := panelBorderStyle.Render(sep)
	return sessionStyled + "  " + sepStyled + "  " + liveStyled + "  " + sepStyled + "  " + statusStyled
}
