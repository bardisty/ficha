package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/bardisty/ccusage/internal/render"
	"github.com/bardisty/ccusage/internal/styles"
	"github.com/charmbracelet/lipgloss"
)

// sessionIDDisplayLen is how many leading characters of a session ID the
// compact live header shows. The static `show` header has room for the full
// UUID; the live panel is width-constrained, so it shows a short prefix.
const sessionIDDisplayLen = 8

// liveHeaderParams carries the per-frame state the shared live header needs.
// Both Model (watch) and BreakdownModel (breakdown) populate it identically,
// so this one renderer keeps the two live headers from drifting apart
// (audit finding TUI-5: the headers had already diverged on their dead
// minimum-width fallback, 72 vs 76).
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

	// Minimum width for content (dead in practice: callers clamp width to
	// >= 40 via panelWidthFor, per audit D9). Canonicalized to 76.
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
		content := fmt.Sprintf("%s  %s  %s  %s  %s", sessionPart, sep, livePart, sep, statusPart)
		// Padding from rendered width — len() overcounts multi-byte UTF-8 chars like ● (3 bytes, 1 column)
		padding := max(innerWidth-lipgloss.Width(content), 0)

		// Top border
		sb.WriteString("  ")
		sb.WriteString(styles.BoxTopLeft)
		sb.WriteString(strings.Repeat(styles.BoxHorizontal, width-2))
		sb.WriteString(styles.BoxTopRight)
		sb.WriteString("\n")

		// Content line
		sb.WriteString("  ")
		sb.WriteString(styles.BoxVertical)
		sb.WriteString("  ")
		sb.WriteString(content)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString("  ")
		sb.WriteString(styles.BoxVertical)
		sb.WriteString("\n")

		// Bottom border
		sb.WriteString("  ")
		sb.WriteString(styles.BoxBottomLeft)
		sb.WriteString(strings.Repeat(styles.BoxHorizontal, width-2))
		sb.WriteString(styles.BoxBottomRight)
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
