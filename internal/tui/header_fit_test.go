package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

// At every width watch draws, boxed or compact, the header ends in a whole
// status: a narrow one swaps a phrase for a shorter one and never cuts a
// word. The wide form comes back as soon as it fits.
func TestHeaderStatusIsNeverCutMidWord(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	// The view's own spinner, in its style, as the models hand it over. The
	// spinner forms below draw in color. Escapes take no columns, so the test
	// reads the lines without them.
	dots := newSpinner(false).View()
	forms := []struct {
		name string
		edit func(*liveHeaderParams)
		// whole lists the forms the status may take, widest first
		whole []string
	}{
		{"no messages", func(p *liveHeaderParams) {}, []string{"no messages yet", "no msgs"}},
		{"empty, recently touched", func(p *liveHeaderParams) { p.noMessages = true; p.lastActivity = now.Add(-time.Minute) },
			[]string{"no messages yet", "no msgs"}},
		{"active", func(p *liveHeaderParams) { p.lastActivity = now.Add(-12 * time.Second) }, []string{"last msg 12s ago", "12s ago"}},
		{"idle", func(p *liveHeaderParams) { p.lastActivity = now.Add(-7 * time.Minute) }, []string{"idle 7m"}},
		{"idle for months", func(p *liveHeaderParams) { p.lastActivity = now.Add(-340 * 24 * time.Hour) }, []string{"idle 11mo"}},
		{"loading", func(p *liveHeaderParams) { p.loading = true }, []string{"loading...", "loading"}},
		{"loading with a spinner", func(p *liveHeaderParams) { p.loading = true; p.noColor = false; p.spinnerView = dots },
			[]string{"⣾ loading...", "⣾ loading"}},
		{"loading with the ASCII spinner", func(p *liveHeaderParams) { p.loading = true; p.noColor = false; p.spinnerView = "|" },
			[]string{"| loading...", "| loading"}},
		{"error", func(p *liveHeaderParams) { p.err = errors.New("x"); p.lastActivity = now.Add(-12 * time.Second) },
			[]string{"last msg 12s ago", "12s ago"}},
		{"waiting", func(p *liveHeaderParams) { p.waiting = true; p.sessionID = "" }, []string{"waiting for a session", "waiting for session"}},
	}
	sep := " " + "│" + " "
	for _, mode := range []string{"FOLLOWING", "PINNED"} {
		for termWidth := minTermWidth; termWidth <= 60; termWidth++ {
			for i, f := range forms {
				project := []string{"webapp", "", "a-very-long-project-name", "日本語プロジェクト"}[(termWidth+i)%4]
				p := liveHeaderParams{sessionID: sessA, noColor: true, project: project, mode: mode, now: now}
				f.edit(&p)
				panel := panelWidthFor(termWidth)
				p.width = panel

				box := stripANSI(strings.Split(renderLiveHeaderPanel(p), "\n")[1])
				lines := map[string]string{
					"compact": "  " + stripANSI(fitStatusHeader(p, panel-2)),
					"boxed":   strings.TrimSuffix(strings.TrimRight(box, " "), "║"),
				}
				for kind, line := range lines {
					name := fmt.Sprintf("%s %s at %d, %s, project %q", mode, kind, termWidth, f.name, project)
					if w := lipgloss.Width(line); w > termWidth {
						t.Errorf("%s: %d columns: %q", name, w, line)
					}
					line = strings.TrimRight(line, " ")
					status := line[strings.LastIndex(line, sep)+len(sep):]
					found := false
					for _, want := range f.whole {
						if status == want {
							found = true
						}
					}
					if !found {
						t.Errorf("%s: status %q is none of %q: %q", name, status, f.whole, line)
					}
					if !strings.Contains(line, "aaaaaaaa") && p.sessionID != "" {
						t.Errorf("%s: the session ID gave way: %q", name, line)
					}
					if !strings.Contains(line, mode) {
						t.Errorf("%s: the mode gave way: %q", name, line)
					}
				}
			}
		}
	}
}

// The status gives up its words before the project gives up anything, so
// the project reads the same whether the session is empty, active or idle.
func TestHeaderShortensTheStatusBeforeTheProject(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	p := liveHeaderParams{sessionID: sessA, noColor: true, project: "webapp", mode: "FOLLOWING", now: now}
	for width, want := range map[int]string{
		49: "webapp │ aaaaaaaa │ ● FOLLOWING │ no messages yet",
		48: "webapp │ aaaaaaaa │ ● FOLLOWING │ no msgs",
		41: "webapp │ aaaaaaaa │ ● FOLLOWING │ no msgs",
		40: "weba… │ aaaaaaaa │ ● FOLLOWING │ no msgs",
		39: "web… │ aaaaaaaa │ ● FOLLOWING │ no msgs",
		38: "aaaaaaaa │ ● FOLLOWING │ no msgs",
		32: "aaaaaaaa │ ● FOLLOWING │ no msgs",
	} {
		if got := fitStatusHeader(p, width); got != want {
			t.Errorf("width %d: %q, want %q", width, got, want)
		}
	}

	active, idle := p, p
	active.lastActivity = now.Add(-12 * time.Second)
	idle.lastActivity = now.Add(-7 * time.Minute)
	project := func(p liveHeaderParams, width int) string {
		project, _, _ := strings.Cut(fitStatusHeader(p, width), "aaaaaaaa")
		return project
	}
	for width := 32; width <= 60; width++ {
		want := project(p, width)
		for name, q := range map[string]liveHeaderParams{"active": active, "idle": idle} {
			if got := project(q, width); got != want {
				t.Errorf("width %d: an %s session shows project %q, an empty one %q", width, name, got, want)
			}
		}
	}
}

// A project that is dropped leaves room behind it, and a status whose full
// form fits there takes it back.
func TestHeaderStatusTakesBackTheDroppedProjectsRoom(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	p := liveHeaderParams{sessionID: sessA, noColor: true, project: "webapp", mode: "FOLLOWING", now: now, loading: true}
	for width, want := range map[int]string{
		39: "web… │ aaaaaaaa │ ● FOLLOWING │ loading",
		38: "aaaaaaaa │ ● FOLLOWING │ loading...",
		35: "aaaaaaaa │ ● FOLLOWING │ loading...",
		34: "aaaaaaaa │ ● FOLLOWING │ loading",
	} {
		if got := fitStatusHeader(p, width); got != want {
			t.Errorf("loading at %d: %q, want %q", width, got, want)
		}
	}
	p.loading, p.waiting, p.sessionID = false, true, ""
	for width, want := range map[int]string{
		40: "web… │ ● FOLLOWING │ waiting for session",
		39: "● FOLLOWING │ waiting for a session",
		35: "● FOLLOWING │ waiting for a session",
		34: "● FOLLOWING │ waiting for session",
	} {
		if got := fitStatusHeader(p, width); got != want {
			t.Errorf("waiting at %d: %q, want %q", width, got, want)
		}
	}
}

// The header alone puts the gap after the spinner: one space in the full form
// and the short one, for the dots and for the ASCII line, in both views. The
// spinner view arrives styled, so a space inside a frame would sit between
// the escapes, where the header can't trim it.
func TestLoadingSpinnerIsOneSpaceFromTheStatus(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		glyph := "⣾"
		if ascii {
			glyph = "|"
		}
		t.Run(glyph, func(t *testing.T) {
			if ascii {
				useASCII(t)
			}
			for _, f := range newSpinner(false).Spinner.Frames {
				if strings.ContainsRune(f, ' ') {
					t.Errorf("frame %q carries a space of its own", f)
				}
			}

			watch := NewModel("/test/path", sessA, false, "", false)
			watch.loading = true
			breakdown := NewBreakdownModel("/test/path", sessA, false, "", false)
			breakdown.loading = true
			for view, p := range map[string]liveHeaderParams{
				"watch":     watch.headerParams(defaultPanelWidth),
				"breakdown": breakdown.headerParams(defaultPanelWidth),
			} {
				for form, want := range map[statusForm]string{
					statusFull:  glyph + " loading...",
					statusShort: glyph + " loading",
				} {
					got := stripANSI(buildStatusHeader(p, p.project, form))
					if !strings.HasSuffix(got, " "+want) || strings.HasSuffix(got, "  "+want) {
						t.Errorf("%s, form %d: %q, want one space before %q", view, form, got, want)
					}
				}
			}
		})
	}
}
