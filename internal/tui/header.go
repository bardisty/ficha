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

// liveHeaderParams is the per-frame state the shared live header renders:
//
//	webapp │ 68994c84 │ ● FOLLOWING │ last msg 12s ago
//
// turning to "○ FOLLOWING │ idle 7m" once no message has landed for
// idleAfter.
type liveHeaderParams struct {
	sessionID   string
	loading     bool
	err         error
	spinnerView string // m.spinner.View(); only shown while loading
	noColor     bool
	width       int

	project      string    // project directory name; omitted when ""
	mode         string    // "FOLLOWING" or "PINNED"
	lastActivity time.Time // newest message timestamp; zero when none yet
	noMessages   bool      // lastActivity is the file's mtime: no message yet
	waiting      bool      // no session yet: no ID, and the status says so
	now          time.Time
}

// renderLiveHeaderPanel renders the boxed live header shared by the watch and
// breakdown TUIs:
//
//	╔══════════════════════════════════════════════════════════╗
//	║  webapp │ 68994c84 │ ● FOLLOWING │ last msg 12s ago       ║
//	╚══════════════════════════════════════════════════════════╝
func renderLiveHeaderPanel(p liveHeaderParams) string {
	var sb strings.Builder

	// Fallback for an absurdly small width; callers clamp to >=
	// minPanelWidth via panelWidthFor, so this rarely fires.
	width := p.width
	if width < minPanelWidth {
		width = defaultPanelWidth
	}

	// Inner width: the box's borders and a 2-column margin inside each. The
	// widest everyday content, ID, mode and "idle 11mo", is 34 columns, so
	// the two boxes narrower than 40 give up a column of each margin rather
	// than cut the status: "idle 12m" without its "m" would misread. It goes
	// by width, not content, so the text doesn't shift as the status grows.
	margin := "  "
	if width < minPanelWidth+2 {
		margin = " "
	}
	innerWidth := width - 2 - 2*len(margin)

	// Fit the content to innerWidth *before* computing padding: full content
	// wider than innerWidth would clamp padding to 0 and push the right border
	// out of column on narrow terminals. fitStatusHeader elides in order of
	// least value; MaxWidth hard-clips as a final guarantee.
	content := fitStatusHeader(p, innerWidth)
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
		sb.WriteString(margin)
		sb.WriteString(content)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString(margin)
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
		sb.WriteString(styles.PanelBorderStyle.Render(styles.BoxTopLeft))
		sb.WriteString(styles.PanelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(styles.PanelBorderStyle.Render(styles.BoxTopRight))
		sb.WriteString("\n")

		// Content line
		sb.WriteString("  ")
		sb.WriteString(styles.PanelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString(margin)
		sb.WriteString(content)
		sb.WriteString(strings.Repeat(" ", padding))
		sb.WriteString(margin)
		sb.WriteString(styles.PanelBorderStyle.Render(styles.BoxVertical))
		sb.WriteString("\n")

		// Bottom border
		sb.WriteString("  ")
		sb.WriteString(styles.PanelBorderStyle.Render(styles.BoxBottomLeft))
		sb.WriteString(styles.PanelBorderStyle.Render(strings.Repeat(styles.BoxHorizontal, width-2)))
		sb.WriteString(styles.PanelBorderStyle.Render(styles.BoxBottomRight))
	}

	return sb.String()
}

// statusForm is how much room the header's status may take.
type statusForm int

const (
	statusFull  statusForm = iota // "last msg 12s ago", "no messages yet"
	statusShort                   // "12s ago", "no msgs"
)

// fitStatusHeader builds the mode-style header content and fits it to width,
// giving up detail in order of least value: the status's spare words, then
// project name characters, then the project name altogether. The status goes
// first so that the project doesn't come and go with the session's state:
// "12s ago", "no msgs" and "idle 7m" are one width, where "no messages yet"
// would cost an empty session its project 8 columns before an idle one. The
// ID and the mode stay whole, since they tell one pane from the next, and a
// status loses words, never letters: "no messag" reads as a glitch where
// "no msgs" doesn't.
func fitStatusHeader(p liveHeaderParams, width int) string {
	form := statusFull
	project := p.project
	content := buildStatusHeader(p, project, form)
	if lipgloss.Width(content) > width {
		form = statusShort
		content = buildStatusHeader(p, project, form)
	}
	if over := lipgloss.Width(content) - width; over > 0 && project != "" {
		// Keep at least a few characters: a stub is still recognizable
		// next to the other panes' headers.
		if keep := lipgloss.Width(project) - over; keep >= 4 {
			project = withEllipsis(project, keep)
		} else {
			project = ""
		}
		content = buildStatusHeader(p, project, form)
		// A dropped project frees up to 6 columns, enough for the dots of
		// "loading..." or the article of "waiting for a session".
		if full := buildStatusHeader(p, "", statusFull); project == "" && lipgloss.Width(full) <= width {
			content = full
		}
	}
	return content
}

// buildStatusHeader renders "project │ id │ ● MODE │ status", with the
// status in the given form.
func buildStatusHeader(p liveHeaderParams, project string, form statusForm) string {
	style := func(st lipgloss.Style, s string) string {
		if p.noColor {
			return s
		}
		return st.Render(s)
	}

	idle := !p.lastActivity.IsZero() && p.now.Sub(p.lastActivity) >= idleAfter

	dot := styles.LiveDot
	modeStyle := styles.LiveIndicatorStyle
	switch {
	case p.err != nil:
		modeStyle = lipgloss.NewStyle().Bold(true).Foreground(styles.ErrorColor)
	case idle:
		dot = styles.IdleDot
		modeStyle = styles.DimStyle
	}

	short := form == statusShort
	var status string
	switch {
	case p.waiting && short:
		status = "waiting for session"
	case p.waiting:
		status = "waiting for a session"
	case p.loading:
		status = "loading..."
		spin := p.spinnerView
		if short {
			// The dot spinner's frames end in a space of their own.
			status, spin = "loading", strings.TrimRight(spin, " ")
		}
		if !p.noColor {
			status = spin + " " + status
		}
	case (p.lastActivity.IsZero() || p.noMessages && !idle) && short:
		status = "no msgs"
	case p.lastActivity.IsZero(), p.noMessages && !idle:
		status = "no messages yet"
	case idle:
		status = style(styles.DimStyle, "idle "+strings.TrimSuffix(render.Ago(p.lastActivity, p.now), " ago"))
	case short:
		status = messageAge(p.lastActivity, p.now)
	default:
		status = "last msg " + messageAge(p.lastActivity, p.now)
	}

	var segs []string
	if project != "" {
		segs = append(segs, style(styles.SectionHeaderStyle, project))
	}
	if p.sessionID != "" {
		segs = append(segs, render.TruncateID(p.sessionID, sessionIDDisplayLen))
	}
	segs = append(segs, style(modeStyle, dot+" "+p.mode), status)
	return strings.Join(segs, style(styles.PanelBorderStyle, " "+styles.BoxVerticalSep+" "))
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
