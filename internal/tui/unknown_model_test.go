package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/models"
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
	return watchViewWithUnknownModelAt(t, noColor, 80)
}

// watchViewWithUnknownModelAt is watchViewWithUnknownModel at width columns.
func watchViewWithUnknownModelAt(t *testing.T, noColor bool, width int) string {
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
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 60})
	m = updated.(Model)
	updated, _ = m.Update(analysisMsg{analysis: a})
	m = updated.(Model)
	m.lastUpdated = goldenTime(11, 30, 0)
	return frameOf(m)
}

func TestWatchMarksUnknownModel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		noColor bool
	}{
		{"no-color", true},
		{"color", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
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

			// The footer explains it. Naming two long IDs leaves no room in
			// the panel's 76 columns for the pointer to ficha show, which goes
			// whole rather than split across rows.
			full := unknownModelFootnote([]string{unknownModelID, "local-llm-7"}, "0a1b2c3d-4e5f-6789-abcd-ef0123456789")
			if want := strings.TrimSuffix(full, unknownModelPointer("0a1b2c3d")); !strings.Contains(out, want) {
				t.Errorf("footer missing %q:\n%s", want, out)
			}
			if strings.Contains(out, "see ficha") {
				t.Errorf("the pointer doesn't fit and should be gone:\n%s", out)
			}
		})
	}
}

// A wide terminal's panel has room for the pointer to ficha show that the
// 76-column one drops.
func TestWatchUnknownModelPointerFitsAWidePanel(t *testing.T) {
	out := stripANSI(watchViewWithUnknownModelAt(t, true, 160))
	full := unknownModelFootnote([]string{unknownModelID, "local-llm-7"}, "0a1b2c3d-4e5f-6789-abcd-ef0123456789")
	if !strings.Contains(out, full) {
		t.Errorf("footer missing %q:\n%s", full, out)
	}
}

// A catalog-only session must gain neither the marker nor the footnote.
func TestWatchOmitsUnknownModelFootnoteWhenAllKnown(t *testing.T) {
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
		unknownModels: []string{unknownModelID},
	})
	m = updated.(BreakdownModel)
	m.lastUpdated = goldenTime(11, 30, 0)
	return frameOf(m)
}

func TestBreakdownMarksUnknownModel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		noColor bool
	}{
		{"no-color", true},
		{"color", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := stripANSI(breakdownViewWithUnknownModel(t, tc.noColor))

			markedRow, plainRow := findRow(t, out, unknownModelPrefix), findRow(t, out, "Opus 4.8")
			if !strings.Contains(markedRow, unknownModelMarker) {
				t.Errorf("unknown-model row lacks the %q marker:\n%q", unknownModelMarker, markedRow)
			}
			if strings.Contains(plainRow, unknownModelMarker) {
				t.Errorf("catalog-model row must not be marked:\n%q", plainRow)
			}
			if !strings.Contains(out, unknownModelFootnote([]string{unknownModelID}, "0a1b2c3d-4e5f-6789-abcd-ef0123456789")) {
				t.Errorf("footer missing %q:\n%s", unknownModelFootnote([]string{unknownModelID}, "0a1b2c3d-4e5f-6789-abcd-ef0123456789"), out)
			}
		})
	}
}

// The marker is appended inside the MODEL column, not after it, so a marked row
// stays the same width as an unmarked one. Measured in display columns, not
// bytes: a clamped label carries a 3-byte, 1-column ellipsis.
func TestUnknownModelMarkerKeepsColumnWidth(t *testing.T) {
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
		{[]string{"claude-nova-9"}, "⚠ * claude-nova-9: fallback pricing, see ficha show 0a1b2c3d"},
		{[]string{"a", "b"}, "⚠ * a, b: fallback pricing, see ficha show 0a1b2c3d"},
		{[]string{"a", "b", "c", "d"}, "⚠ * a, b +2 more: fallback pricing, see ficha show 0a1b2c3d"},
	}
	// The pointer names the view's session, which ficha show on its own
	// might not open.
	for _, tt := range tests {
		if got := unknownModelFootnote(tt.ids, "0a1b2c3d-4e5f-6789-abcd-ef0123456789"); got != tt.want {
			t.Errorf("unknownModelFootnote(%v) = %q, want %q", tt.ids, got, tt.want)
		}
	}
}

// breakdown's stats line shortens the fallback-pricing note by whole pieces
// rather than clip it: the pointer to ficha show goes first, then the words
// after the IDs, then the IDs, for what the marker means. Short of room for
// any form, the note goes, separator and all.
func TestBreakdownUnknownNoteFitsWidth(t *testing.T) {
	const long = "claude-nova-6-20991231-preview"
	for _, tc := range []struct {
		width int
		id    string
		want  string // the note's form, "" for none
	}{
		{120, "claude-nova-6", "⚠ * claude-nova-6: fallback pricing, see ficha show 0a1b2c3d"},
		{100, "claude-nova-6", "⚠ * claude-nova-6: fallback pricing"},
		{80, "claude-nova-6", "⚠ * claude-nova-6"},
		{80, long, "⚠ * = fallback pricing"},
		{60, "claude-nova-6", ""},
	} {
		for _, noColor := range []bool{true, false} {
			m := loadedBreakdown(t, tc.width, 24, chromeRows(goldenTime(10, 0, 0), 3))
			m.noColor = noColor
			m.skippedLines = 1
			m.unknownModels = []string{tc.id}
			line := stripANSI(m.renderStatsTotals())
			want := "  Messages: 3 │ Total: $0.0000 │ ⚠ 1 skipped line(s)"
			if tc.want != "" {
				want += " │ " + tc.want
			}
			if line != want {
				t.Errorf("width %d, %s, noColor %v:\n got %q\nwant %q", tc.width, tc.id, noColor, line, want)
			}
			if w := lipgloss.Width(line); w > tc.width {
				t.Errorf("width %d: line is %d wide", tc.width, w)
			}
		}
	}
}

// windowUnknownAnalysis is the golden session with its last request at
// 680.4K on a model ficha doesn't know, and one agent's reading on that
// model too.
func windowUnknownAnalysis() *models.SessionAnalysis {
	a := goldenViewAnalysis()
	a.LastMessageModel = "claude-mystery-9"
	a.CostByModel["claude-mystery-9"] = models.CostBreakdown{TotalCost: 1.20}
	a.LastMessageUsage = models.TokenUsage{InputTokens: 400, CacheReadInputTokens: 680_000}
	a.Agents[1].CostByModel = map[string]models.CostBreakdown{"claude-mystery-9": {TotalCost: 0.42}}
	a.Agents[1].Context = &models.ContextUsage{Tokens: 680_400}
	return a
}

// Without a window, the gauge gives the tokens and says why, and the footer
// gives the tokens too: no percentage, bar or compaction note at any width.
func TestGoldenWatchWindowUnknown(t *testing.T) {
	for _, size := range [][2]int{{40, 15}, {60, 20}, {80, 24}} {
		name := fmt.Sprintf("watch_window_unknown_%dx%d", size[0], size[1])
		frame := goldenWatchViewOf(t, windowUnknownAnalysis(), true, size[0], size[1])
		checkGolden(t, name, frame)
		if strings.Contains(frame, "compaction") || !strings.Contains(frame, "ctx 680.4K") {
			t.Errorf("%s: want no compaction note and a ctx 680.4K footer:\n%s", name, frame)
		}
	}
	checkGolden(t, "watch_window_unknown_120x40_color", goldenWatchViewOf(t, windowUnknownAnalysis(), false, 120, 40))
}

// taggedUnknownBreakdown is breakdown at width on the fixture's 618 rows,
// alternating between an audit-codebase run and a review-changes run, one
// of them on a model ficha can't price.
func taggedUnknownBreakdown(t *testing.T, width int, noColor bool) BreakdownModel {
	t.Helper()
	return taggedUnknownBreakdownFrom(t, width, noColor, 0)
}

// taggedUnknownBreakdownFrom is taggedUnknownBreakdown with the first
// untagged rows the main conversation's.
func taggedUnknownBreakdownFrom(t *testing.T, width int, noColor bool, untagged int) BreakdownModel {
	t.Helper()
	rows := chromeRows(goldenTime(1, 0, 0), 618)
	for i := range rows[untagged:] {
		i += untagged
		rows[i].AgentID, rows[i].WorkflowID = "a1111111111111111", "wf_a"
		if i%2 == 1 {
			rows[i].AgentID, rows[i].WorkflowID = "a2222222222222222", "wf_b"
		}
	}
	rows[610].Model = "claude-zeta-7-20261001"
	m := NewBreakdownModel("/fixture/sess.jsonl", "bdd640fb", noColor, "", false)
	m.now = func() time.Time { return goldenTime(12, 0, 0) }
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
	updated, _ = updated.Update(breakdownMsgsMsg{
		messages: rows, totalCost: 73.98, insights: &models.MessageInsights{}, hasAgents: true,
		unknownModels: []string{"claude-zeta-7-20261001"},
		workflows:     []models.WorkflowMeta{{RunID: "wf_a", Name: "audit-codebase"}, {RunID: "wf_b", Name: "review-changes"}},
	})
	return updated.(BreakdownModel)
}

// The run-tag key for the rows on screen outranks the model IDs in the
// fallback note, which shrinks for it, but not what the asterisk means: at
// 80 columns the note's plainest form leaves room for one entry.
func TestBreakdownRunTagKeyOutranksUnknownModelIDs(t *testing.T) {
	const totals = "  Messages: 618 │ Total: $73.98 │ "
	for _, tc := range []struct {
		width int
		want  string
	}{
		{80, "⚠ * = fallback pricing │ ac = audit-codebase"},
		{100, "⚠ * = fallback pricing │ ac = audit-codebase, rc = review-changes"},
		{120, "⚠ * claude-zeta-7-20261001 │ ac = audit-codebase, rc = review-changes"},
		{150, "⚠ * claude-zeta-7-20261001: fallback pricing, see ficha show bdd640fb │ ac = audit-codebase, rc = review-changes"},
	} {
		for _, noColor := range []bool{true, false} {
			m := taggedUnknownBreakdown(t, tc.width, noColor)
			if !m.table.runTags {
				t.Fatalf("width %d: setup: the table should draw run tags", tc.width)
			}
			line := stripANSI(m.renderStatsLine(m.table))
			if line != totals+tc.want {
				t.Errorf("width %d, noColor %v:\n got %q\nwant %q", tc.width, noColor, line, totals+tc.want)
			}
			if w := lipgloss.Width(line); w > tc.width {
				t.Errorf("width %d: line is %d wide", tc.width, w)
			}
		}
	}
}

// The ? list names every run tag in the scope's rows, on screen or not,
// after the hidden-columns line, and none when the table draws no tags.
func TestBreakdownKeyListNamesRunTags(t *testing.T) {
	m := taggedUnknownBreakdown(t, 80, true)
	m = press(t, m, "?")
	frame := frameOf(m)
	if !strings.Contains(frame, "\n  ac = audit-codebase, rc = review-changes ") {
		t.Errorf("? list at 80 lacks the run-tag key:\n%s", frame)
	}
	if strings.Index(frame, "rc = review-changes") < strings.Index(frame, "hidden") {
		t.Errorf("the run-tag key should come after the hidden-columns line:\n%s", frame)
	}
	m = press(t, press(t, m, "?"), "a")
	if m = press(t, m, "?"); strings.Contains(frameOf(m), "rc = review-changes") {
		t.Errorf("scoped to the audit-codebase agent, the ? list names review-changes:\n%s", frameOf(m))
	}
	plain := press(t, loadedBreakdown(t, 80, 24, chromeRows(goldenTime(10, 0, 0), 10)), "?")
	if strings.Contains(frameOf(plain), " = ") {
		t.Errorf("? list names run tags for a session with none:\n%s", frameOf(plain))
	}
}

// The ? list's run-tag key wraps between whole entries, and only a line
// that goes on ends in a comma.
func TestBreakdownRunTagKeyWraps(t *testing.T) {
	m := taggedUnknownBreakdown(t, 80, true)
	for _, tc := range []struct {
		width int
		want  string
	}{
		{42, "ac = audit-codebase, rc = review-changes"},
		{41, "ac = audit-codebase,\nrc = review-changes"},
	} {
		// The layout stays the 80-column one, which draws the tags.
		m.width = tc.width
		if got := m.runTagKeyLines(); got != tc.want {
			t.Errorf("at %d columns: %q, want %q", tc.width, got, tc.want)
		}
	}
}

// The fallback note's form depends on the width and the scope's tags, not
// on the rows on screen, so it holds while the reader scrolls past rows
// with no tag; the key itself names only the tags on screen.
func TestBreakdownUnknownNoteHoldsWhileScrolling(t *testing.T) {
	m := press(t, taggedUnknownBreakdownFrom(t, 80, true, 300), "g")
	if got := stripANSI(m.renderStatsLine(m.table)); got != "  Messages: 618 │ Total: $73.98 │ ⚠ * = fallback pricing" {
		t.Errorf("at the top, with no tag on screen: %q", got)
	}
	if m = press(t, m, "G"); !strings.HasSuffix(stripANSI(m.renderStatsLine(m.table)), "│ ⚠ * = fallback pricing │ ac = audit-codebase") {
		t.Errorf("at the bottom, with tags on screen: %q", stripANSI(m.renderStatsLine(m.table)))
	}
}

// A run whose key entry can't fit beside even the plainest note doesn't
// cost another run's entry its room, nor does a short pair of entries cost
// a wide single one: with one run's rows on screen, the note leaves room
// for its entry whenever the plainest note does.
func TestBreakdownUnknownNoteLeavesRoomForTheTagOnScreen(t *testing.T) {
	const wide = "audit-the-whole-codebase-for-security"
	for _, tc := range []struct {
		width int
		names []string
		want  string
	}{
		{80, []string{wide, "review-changes"}, "⚠ * = fallback pricing │ rc = review-changes"},
		{104, []string{"audit", "review", wide}, "⚠ * = fallback pricing │ at = " + wide},
	} {
		rows := chromeRows(goldenTime(1, 0, 0), 618)
		var runs []models.WorkflowMeta
		for i, name := range tc.names {
			runs = append(runs, models.WorkflowMeta{RunID: fmt.Sprintf("wf_%d", i), Name: name})
		}
		for i := range rows {
			// Each run has an equal share of the rows, in run order.
			run := i * len(runs) / len(rows)
			rows[i].AgentID, rows[i].WorkflowID = fmt.Sprintf("a%016d", run), runs[run].RunID
		}
		rows[610].Model = "claude-zeta-7-20261001"
		m := NewBreakdownModel("/fixture/sess.jsonl", "bdd640fb", true, "", false)
		m.now = func() time.Time { return goldenTime(12, 0, 0) }
		updated, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 24})
		updated, _ = updated.Update(breakdownMsgsMsg{
			messages: rows, totalCost: 73.98, insights: &models.MessageInsights{}, hasAgents: true,
			unknownModels: []string{"claude-zeta-7-20261001"}, workflows: runs,
		})
		m = updated.(BreakdownModel)
		if got, want := stripANSI(m.renderStatsLine(m.table)), "  Messages: 618 │ Total: $73.98 │ "+tc.want; got != want {
			t.Errorf("%d runs at %d columns:\n got %q\nwant %q", len(runs), tc.width, got, want)
		}
	}
}
