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

	// Build content parts
	sessionDisplay := render.TruncateID(p.sessionID, sessionIDDisplayLen)
	if p.prevSessionID != "" {
		sessionDisplay = fmt.Sprintf("%s (prev: %s)",
			render.TruncateID(p.sessionID, sessionIDDisplayLen),
			render.TruncateID(p.prevSessionID, sessionIDDisplayLen))
	}

	sessionPart := fmt.Sprintf("Session: %s", sessionDisplay)
	livePart := "● LIVE"
	var statusPart string
	if p.loading {
		statusPart = "Loading..."
	} else if p.err != nil {
		statusPart = "Error"
	} else {
		statusPart = fmt.Sprintf("Updated: %s", p.lastUpdated.Format("15:04:05"))
	}

	sep := styles.BoxVerticalSep
	if p.noColor {
		sep = styles.AsciiVertical
	}

	if p.noColor {
		content := fmt.Sprintf("%s  %s  %s  %s  %s", sessionPart, sep, livePart, sep, statusPart)
		// Padding from rendered width — len() overcounts multi-byte UTF-8 chars like ● (3 bytes, 1 column)
		padding := max(innerWidth-lipgloss.Width(content), 0)

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
		// Build styled content
		sessionStyled := fmt.Sprintf("%s %s",
			sectionHeaderStyle.Render("Session:"),
			sessionDisplay)
		liveStyled := liveIndicatorStyle.Render(livePart)
		var statusStyled string
		if p.loading {
			statusStyled = p.spinnerView + " Loading..."
		} else if p.err != nil {
			statusStyled = lipgloss.NewStyle().Foreground(styles.ErrorColor).Render("Error")
		} else {
			statusStyled = fmt.Sprintf("Updated: %s", p.lastUpdated.Format("15:04:05"))
		}

		sepStyled := panelBorderStyle.Render(sep)

		// Padding from rendered width so styled parts of varying display width
		// (e.g. the spinner shown next to "Loading...") can't push the right
		// border out of alignment
		content := sessionStyled + "  " + sepStyled + "  " + liveStyled + "  " + sepStyled + "  " + statusStyled
		padding := max(innerWidth-lipgloss.Width(content), 0)

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
