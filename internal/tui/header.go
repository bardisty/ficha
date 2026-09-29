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

// idleAfter is how long without a new message before the header calls the
// session idle.
const idleAfter = 5 * time.Minute

// liveHeaderParams is the per-frame state the shared live header renders.
//
// With mode set, the header reads
//
//	webapp │ 68994c84 │ ● FOLLOWING │ last msg 12s ago
//
// and turns to "○ FOLLOWING │ idle 7m" once no message has landed for
// idleAfter. Without it, the header keeps the older
// "Session: … │ ● LIVE │ Updated: …" form, which breakdown still uses.
type liveHeaderParams struct {
	sessionID     string
	prevSessionID string
	loading       bool
	err           error
	lastUpdated   time.Time
	spinnerView   string // m.spinner.View(); only shown while loading
	noColor       bool
	width         int

	project      string    // project directory name; omitted when ""
	mode         string    // "FOLLOWING" or "PINNED"; "" selects the older form
	lastActivity time.Time // newest message timestamp; zero when none yet
	noMessages   bool      // lastActivity is the file's mtime: no message yet
	now          time.Time
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
	var content string
	if p.mode != "" {
		content = fitStatusHeader(p, innerWidth)
	} else {
		showPrev := p.prevSessionID != ""
		timeOnly := false
		content = buildLiveHeaderContent(p, showPrev, timeOnly)
		if lipgloss.Width(content) > innerWidth && showPrev {
			showPrev = false
			content = buildLiveHeaderContent(p, showPrev, timeOnly)
		}
		if lipgloss.Width(content) > innerWidth {
			timeOnly = true
			content = buildLiveHeaderContent(p, showPrev, timeOnly)
		}
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
// elision flags (showPrev, timeOnly) and p.noColor styling. The live dot is
// one column in both glyph sets; lipgloss.Width, not len, measures it for
// padding.
func buildLiveHeaderContent(p liveHeaderParams, showPrev, timeOnly bool) string {
	sessionDisplay := render.TruncateID(p.sessionID, sessionIDDisplayLen)
	if showPrev && p.prevSessionID != "" {
		sessionDisplay = fmt.Sprintf("%s (prev: %s)",
			render.TruncateID(p.sessionID, sessionIDDisplayLen),
			render.TruncateID(p.prevSessionID, sessionIDDisplayLen))
	}
	livePart := styles.LiveDot + " LIVE"

	sep := styles.BoxVerticalSep

	if p.noColor {
		var statusPart string
		switch {
		case p.loading:
			statusPart = "Loading..."
		case p.err != nil:
			statusPart = "Error"
		case timeOnly:
			statusPart = render.Clock(p.lastUpdated)
		default:
			statusPart = fmt.Sprintf("Updated: %s", render.Clock(p.lastUpdated))
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
		statusStyled = render.Clock(p.lastUpdated)
	default:
		statusStyled = fmt.Sprintf("Updated: %s", render.Clock(p.lastUpdated))
	}
	sepStyled := panelBorderStyle.Render(sep)
	return sessionStyled + "  " + sepStyled + "  " + liveStyled + "  " + sepStyled + "  " + statusStyled
}

// fitStatusHeader builds the mode-style header content and fits it to width,
// giving up detail in order of least value: the "last msg" prefix, then
// project name characters, then the project name altogether.
func fitStatusHeader(p liveHeaderParams, width int) string {
	short := false
	project := p.project
	content := buildStatusHeader(p, project, short)
	if lipgloss.Width(content) > width {
		short = true
		content = buildStatusHeader(p, project, short)
	}
	if over := lipgloss.Width(content) - width; over > 0 && project != "" {
		// Keep at least a few characters: a stub is still recognizable
		// next to the other panes' headers.
		if keep := lipgloss.Width(project) - over; keep >= 4 {
			project = withEllipsis(project, keep)
		} else {
			project = ""
		}
		content = buildStatusHeader(p, project, short)
	}
	return content
}

// buildStatusHeader renders "project │ id │ ● MODE │ status". short drops
// the "last msg" prefix from the status.
func buildStatusHeader(p liveHeaderParams, project string, short bool) string {
	style := func(st lipgloss.Style, s string) string {
		if p.noColor {
			return s
		}
		return st.Render(s)
	}

	idle := !p.lastActivity.IsZero() && p.now.Sub(p.lastActivity) >= idleAfter

	dot := styles.LiveDot
	modeStyle := liveIndicatorStyle
	switch {
	case p.err != nil:
		modeStyle = lipgloss.NewStyle().Bold(true).Foreground(styles.ErrorColor)
	case idle:
		dot = styles.IdleDot
		modeStyle = dimStyle
	}

	var status string
	switch {
	case p.loading:
		status = "Loading..."
		if !p.noColor {
			status = p.spinnerView + " Loading..."
		}
	case p.lastActivity.IsZero(), p.noMessages && !idle:
		status = "no messages yet"
	case idle:
		status = style(dimStyle, "idle "+strings.TrimSuffix(render.Ago(p.lastActivity, p.now), " ago"))
	case short:
		status = messageAge(p.lastActivity, p.now)
	default:
		status = "last msg " + messageAge(p.lastActivity, p.now)
	}

	var segs []string
	if project != "" {
		segs = append(segs, style(sectionHeaderStyle, project))
	}
	segs = append(segs,
		render.TruncateID(p.sessionID, sessionIDDisplayLen),
		style(modeStyle, dot+" "+p.mode),
		status)
	return strings.Join(segs, style(panelBorderStyle, " "+styles.BoxVerticalSep+" "))
}

// messageAge is render.Ago with seconds under a minute: in a live view,
// "12s ago" against "50s ago" is the difference between busy and stalling,
// where Ago says "just now" for both.
func messageAge(t, now time.Time) string {
	if d := now.Sub(t); d < time.Minute {
		return fmt.Sprintf("%ds ago", max(int(d/time.Second), 0))
	}
	return render.Ago(t, now)
}
