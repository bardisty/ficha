package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// unknownModelID is not in the catalog, so it is priced from the fallback table.
// show/summary/global say so on stderr; the TUIs own the screen and must say so
// on the screen.
const unknownModelID = "claude-opus-4-9-20260101"

// unknownModelPrefix survives the clamp on every surface: the raw ID is cut to
// 9 columns in the breakdown row and 11 in the watch COST BY MODEL row, and
// each reserves the last one for the marker.
const unknownModelPrefix = "opus-4-9"

// watchViewWithUnknownModel renders a watch frame whose COST BY MODEL section
// carries one catalog model and one fallback-priced model.
func watchViewWithUnknownModel(t *testing.T, noColor bool) string {
	t.Helper()
	a := goldenViewAnalysis()
	a.CostByModel = map[string]models.CostBreakdown{
		"claude-opus-4-8": {TotalCost: 7.90},
		unknownModelID:    {TotalCost: 2.00},
		// 11 chars: the tightest fit against the 12-column label + 1-column
		// marker.
		"local-llm-7": {TotalCost: 0.0001},
	}
	a.SkippedLines = 0

	m := NewModel("/fixture/sess.jsonl", "0a1b2c3d-4e5f-6789-abcd-ef0123456789", noColor, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	m = updated.(Model)
	updated, _ = m.Update(analysisMsg{analysis: a})
	m = updated.(Model)
	m.lastUpdated = goldenTime(11, 30, 0)
	return m.View()
}

func TestWatchMarksUnknownModel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		noColor bool
		profile termenv.Profile
	}{
		{"no-color", true, termenv.Ascii},
		{"color", false, termenv.ANSI256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forceProfile(t, tc.profile)
			out := stripANSI(watchViewWithUnknownModel(t, tc.noColor))

			// The unknown model's row carries the marker; the catalog model's does not.
			markedRow, plainRow := findRow(t, out, unknownModelPrefix), findRow(t, out, "Opus 4.8")
			if !strings.Contains(markedRow, unknownModelMarker) {
				t.Errorf("unknown-model row lacks the %q marker:\n%q", unknownModelMarker, markedRow)
			}
			if strings.Contains(plainRow, unknownModelMarker) {
				t.Errorf("catalog-model row must not be marked:\n%q", plainRow)
			}

			// A marked row must not push its cost out of the column. Measured in
			// display columns: the clamp's ellipsis is 3 bytes but 1 column.
			rows := costByModelRows(out)
			if len(rows) != 3 {
				t.Fatalf("expected 3 COST BY MODEL rows, got %d: %q", len(rows), rows)
			}
			for i, row := range rows {
				got := lipgloss.Width(row[:strings.Index(row, "$")])
				want := lipgloss.Width(rows[0][:strings.Index(rows[0], "$")])
				if got != want {
					t.Errorf("row %d puts the cost at column %d, row 0 at %d:\n%q", i, got, want, row)
				}
			}

			// The footer explains it.
			if want := unknownModelFootnote([]string{unknownModelID, "local-llm-7"}); !strings.Contains(out, want) {
				t.Errorf("footer missing %q:\n%s", want, out)
			}
		})
	}
}

// A catalog-only session must gain neither the marker nor the footnote.
func TestWatchOmitsUnknownModelFootnoteWhenAllKnown(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	out := stripANSI(goldenWatchView(t, true))
	if strings.Contains(out, "fallback pricing") {
		t.Errorf("footnote must not render when every model is priced from the catalog:\n%s", out)
	}
	for _, row := range costByModelRows(out) {
		if strings.Contains(row, unknownModelMarker) {
			t.Errorf("catalog-only row is marked: %q", row)
		}
	}
}

// breakdownViewWithUnknownModel renders a breakdown frame whose message rows mix
// a catalog model with a fallback-priced one.
func breakdownViewWithUnknownModel(t *testing.T, noColor bool) string {
	t.Helper()
	msgs := goldenBreakdownMessages()
	msgs[1].Model = unknownModelID

	m := NewBreakdownModel("/fixture/sess.jsonl", "0a1b2c3d-4e5f-6789-abcd-ef0123456789", noColor, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(BreakdownModel)
	updated, _ = m.Update(breakdownMsgsMsg{
		messages: msgs, totalCost: 3.34, minCost: 0.01, maxCost: 1.87,
		hasUnknown: true,
	})
	m = updated.(BreakdownModel)
	m.lastUpdated = goldenTime(11, 30, 0)
	return m.View()
}

func TestBreakdownMarksUnknownModel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		noColor bool
		profile termenv.Profile
	}{
		{"no-color", true, termenv.Ascii},
		{"color", false, termenv.ANSI256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forceProfile(t, tc.profile)
			out := stripANSI(breakdownViewWithUnknownModel(t, tc.noColor))

			markedRow, plainRow := findRow(t, out, unknownModelPrefix), findRow(t, out, "Opus 4.8")
			if !strings.Contains(markedRow, unknownModelMarker) {
				t.Errorf("unknown-model row lacks the %q marker:\n%q", unknownModelMarker, markedRow)
			}
			if strings.Contains(plainRow, unknownModelMarker) {
				t.Errorf("catalog-model row must not be marked:\n%q", plainRow)
			}
			if !strings.Contains(out, unknownModelFootnote([]string{unknownModelID})) {
				t.Errorf("footer missing %q:\n%s", unknownModelFootnote([]string{unknownModelID}), out)
			}
		})
	}
}

// The marker is appended inside the MODEL column, not after it, so a marked row
// stays the same width as an unmarked one. Measured in display columns, not
// bytes: a clamped label carries a 3-byte, 1-column ellipsis.
func TestUnknownModelMarkerKeepsColumnWidth(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	out := stripANSI(breakdownViewWithUnknownModel(t, true))

	marked, plain := findRow(t, out, unknownModelPrefix), findRow(t, out, "Opus 4.8")
	costCol := func(row string) int { return lipgloss.Width(row[:strings.Index(row, "$")]) }
	if a, b := costCol(marked), costCol(plain); a != b {
		t.Errorf("cost column at %d on the marked row vs %d on the plain row:\n%q\n%q", a, b, marked, plain)
	}
	if lipgloss.Width(marked) != lipgloss.Width(plain) {
		t.Errorf("marked row is %d columns, plain row is %d", lipgloss.Width(marked), lipgloss.Width(plain))
	}
}

// --- helpers ---

// ansiRe matches the SGR escape sequences lipgloss emits.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes escape sequences so assertions can match on plain text in
// both the color and no-color paths.
func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// findRow returns the first line containing needle.
func findRow(t *testing.T, out, needle string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line containing %q in:\n%s", needle, out)
	return ""
}

// costByModelRows returns the model rows of a watch frame's COST BY MODEL section.
func costByModelRows(out string) []string {
	var rows []string
	inSection := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "COST BY MODEL"):
			inSection = true
		case inSection && strings.Contains(line, "["):
			return rows
		case inSection && strings.Contains(line, "$"):
			rows = append(rows, line)
		}
	}
	return rows
}

// With room, breakdown shows an unknown model's full ID, the one a pricing
// update needs, and the footnote names it.
func TestBreakdownShowsFullUnknownModelID(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	out := stripANSI(breakdownViewWithUnknownModel(t, true))
	if row := findRow(t, out, unknownModelPrefix); !strings.Contains(row, unknownModelID+unknownModelMarker) {
		t.Errorf("at 120 columns the row should carry the full ID:\n%q", row)
	}
	if !strings.Contains(out, unknownModelMarker+" "+unknownModelID+": fallback pricing") {
		t.Errorf("the footnote should name the model:\n%s", out)
	}
}

func TestUnknownModelFootnote(t *testing.T) {
	tests := []struct {
		ids  []string
		want string
	}{
		{nil, "⚠ * = fallback pricing"},
		{[]string{"claude-nova-9"}, "⚠ * claude-nova-9: fallback pricing"},
		{[]string{"a", "b"}, "⚠ * a, b: fallback pricing"},
		{[]string{"a", "b", "c", "d"}, "⚠ * a, b +2 more: fallback pricing"},
	}
	for _, tt := range tests {
		if got := unknownModelFootnote(tt.ids); got != tt.want {
			t.Errorf("unknownModelFootnote(%v) = %q, want %q", tt.ids, got, tt.want)
		}
	}
}
