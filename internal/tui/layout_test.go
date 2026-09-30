package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The frame fills the terminal exactly at every size, with or without a
// notice in the notify row, so a notice never shifts the body.
func TestWatchFrameHeightIsStable(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	for _, size := range [][2]int{{80, 24}, {120, 40}, {80, 15}, {60, 10}, {40, 8}} {
		w, h := size[0], size[1]
		m := NewModel("/p/"+sessA+".jsonl", sessA, true, "", true)
		m = sized(t, m, w, h)
		m = load(t, m, tallAnalysis(1))
		plain := m.View()
		m.hint = &sessionHint{id: sessB, at: m.clock()}
		hinted := m.View()
		for name, v := range map[string]string{"plain": plain, "hinted": hinted} {
			if got := strings.Count(v, "\n") + 1; got != h {
				t.Errorf("%dx%d %s: %d rows, want %d", w, h, name, got, h)
			}
			for _, line := range strings.Split(v, "\n") {
				if lipgloss.Width(line) > w {
					t.Errorf("%dx%d %s: line wider than terminal: %q", w, h, name, line)
				}
			}
		}
		if lineOf(plain, "TOTAL ]") != lineOf(hinted, "TOTAL ]") {
			t.Errorf("%dx%d: the hint moved the body", w, h)
		}
	}
}

func TestWatchCompactAndTooSmall(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	m := NewModel("/p/"+sessA+".jsonl", sessA, true, "", true)
	m = load(t, sized(t, m, 80, 12), tallAnalysis(1))
	view := m.View()
	if strings.Contains(view, "╔") {
		t.Errorf("compact view kept the box:\n%s", view)
	}
	for _, want := range []string{"● FOLLOWING", "API est.", "q quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("compact view lost %q:\n%s", want, view)
		}
	}

	for _, tc := range []struct {
		w, h  int
		needs []string
	}{
		{39, 20, []string{"need 40 cols"}},
		{80, 7, []string{"need 8 rows"}},
		{20, 8, []string{"need 40 cols"}},
		{20, 4, []string{"need 40 cols", "need 8 rows"}},
	} {
		m = sized(t, m, tc.w, tc.h)
		v := m.View()
		var lines []string
		for _, line := range strings.Split(v, "\n") {
			lines = append(lines, strings.TrimSpace(line))
		}
		// Every line whole, not clipped, down to 20 columns.
		want := append(append([]string{"terminal too small"}, tc.needs...), "q to quit")
		if !slices.Equal(lines, want) {
			t.Errorf("%dx%d: got lines %q, want %q", tc.w, tc.h, lines, want)
		}
	}
}

// A help hint that doesn't fit is dropped whole, f first, never cut mid-word.
func TestWatchHelpLineDropsWholeHints(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	for _, tc := range []struct {
		w    int
		want string
	}{
		{80, "q quit • j/k scroll • space/b page • g/G top/bottom • f follow"},
		{60, "q quit • j/k scroll • space/b page • g/G top/bottom"},
		{50, "q quit • j/k scroll • space/b page"},
	} {
		m := NewModel("/p/"+sessA+".jsonl", sessA, true, "", true)
		m = load(t, sized(t, m, tc.w, 30), tallAnalysis(1))
		lines := strings.Split(m.View(), "\n")
		if got := strings.TrimSpace(lines[len(lines)-1]); got != tc.want {
			t.Errorf("width %d: help line = %q, want %q", tc.w, got, tc.want)
		}
	}
}

// Compact watch keeps its help row, so with two warning rows the footer takes
// five rows; at the smallest size that still leaves the body one.
func TestWatchCompactFrameFitsWithWarnings(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	a := tallAnalysis(1)
	a.SkippedLines = 2
	a.EstimatedCostMessages = 3
	a.CostByModel["claude-nova-6-unlisted-model"] = models.CostBreakdown{TotalCost: 0.5}
	for _, size := range [][2]int{{40, 8}, {60, 10}, {80, 15}} {
		w, h := size[0], size[1]
		m := NewModel("/fixture/sess.jsonl", "sess", true, "", false)
		m = load(t, sized(t, m, w, h), a)
		if rows := len(m.warningRows(panelWidthFor(w))); rows != maxWarningRows {
			t.Fatalf("%dx%d: %d warning rows, want %d for this test", w, h, rows, maxWarningRows)
		}
		v := m.View()
		if got := strings.Count(v, "\n") + 1; got != h {
			t.Errorf("%dx%d: %d rows, want %d:\n%s", w, h, got, h, v)
		}
		if m.viewport.Height < 1 {
			t.Errorf("%dx%d: viewport has %d rows", w, h, m.viewport.Height)
		}
		if !strings.Contains(v, "q quit") {
			t.Errorf("%dx%d: no help row:\n%s", w, h, v)
		}
	}
}

// A token delta sits in its own column after the TTL: nothing to its left
// moves while it shows.
func TestTokenDeltaColumn(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	m := NewModel("/p/s.jsonl", "s", true, "", false)
	m = sized(t, m, 100, 40)
	before := m.renderUnifiedCostRow("Cache write", 1.5, 50000, "c", "tok", nil, "5m TTL")
	m.deltaTokens["tok"] = 2700
	m.changedAt["tok"] = time.Now()
	after := m.renderUnifiedCostRow("Cache write", 1.5, 52700, "c", "tok", nil, "5m TTL")
	plainAfter := m.renderUnifiedCostRow("Input", 1.5, 52700, "c", "tok", nil, "")

	if !strings.HasPrefix(after, strings.TrimRight(strings.Replace(before, "50.0K", "52.7K", 1), "\n")) {
		t.Errorf("delta shifted the row:\nbefore %q\nafter  %q", before, after)
	}
	if strings.Index(after, "(+2.7K)") != strings.Index(plainAfter, "(+2.7K)") {
		t.Errorf("delta columns don't line up:\n%q\n%q", after, plainAfter)
	}
}

// The highlight tick runs only while something is highlighted, and a
// background reload doesn't bring "Loading..." back.
func TestWatchTickOnlyWhileHighlighted(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	m := NewModel("/p/s.jsonl", "s", true, "", false)
	m = sized(t, m, 80, 24)
	m = load(t, m, tallAnalysis(1))
	if m.ticking {
		t.Fatal("first load started the highlight tick")
	}

	updated, cmd := m.Update(tickMsg(time.Now()))
	m = updated.(Model)
	if cmd != nil {
		t.Error("tick with no highlights re-armed")
	}

	m.loading = true
	if !strings.Contains(m.View(), "last msg") && !strings.Contains(m.View(), "no messages") {
		t.Errorf("background reload showed Loading:\n%s", m.View())
	}
	m = load(t, m, tallAnalysis(2))
	if !m.ticking {
		t.Fatal("a change didn't start the highlight tick")
	}
	for k := range m.changedAt {
		m.changedAt[k] = time.Now().Add(-time.Hour)
	}
	updated, cmd = m.Update(tickMsg(time.Now()))
	m = updated.(Model)
	if cmd != nil || m.ticking {
		t.Error("tick kept running after the highlights expired")
	}
}

// lineOf returns the index of the first line containing sub, or -1.
func lineOf(s, sub string) int {
	for i, line := range strings.Split(s, "\n") {
		if strings.Contains(line, sub) {
			return i
		}
	}
	return -1
}

// On a terminal too narrow for the delta column, the delta moves next to
// its count rather than off the screen.
func TestTokenDeltaNarrowFallsBackInline(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	m := NewModel("/p/s.jsonl", "s", true, "", false)
	m = sized(t, m, 60, 20)
	m.deltaTokens["tok"] = 2700
	m.changedAt["tok"] = time.Now()
	row := strings.TrimRight(m.renderUnifiedCostRow("Cache write", 1.5, 52700, "c", "tok", nil, "5m TTL"), "\n")
	if !strings.Contains(row, "52.7K (+2.7K) tokens") {
		t.Errorf("narrow row = %q, want the delta inline", row)
	}
	// The delta must be on screen; the TTL label after it may clip, as the
	// frame clips any overlong row.
	if end := strings.Index(row, "(+2.7K)") + len("(+2.7K)"); end > 60 {
		t.Errorf("delta ends at column %d, off a 60-column screen", end)
	}
}

// After a failed first load stops the spinner, a retry that shows
// "Loading..." again must restart it rather than show a frozen frame.
func TestSpinnerRestartsAfterFailedLoad(t *testing.T) {
	m := NewModel("/nonexistent/s.jsonl", "s", false, "", false)
	m = sized(t, m, 80, 24)
	updated, _ := m.Update(errorMsg{err: errSessionFileGone})
	m = updated.(Model)
	if _, cmd := m.Update(spinner.TickMsg{}); cmd != nil {
		t.Fatal("spinner kept ticking with nothing loading")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)
	if !m.showLoading() {
		t.Fatal("retry doesn't show Loading")
	}
	if !batchHas(cmd, func(msg tea.Msg) bool { _, ok := msg.(spinner.TickMsg); return ok }) {
		t.Error("retry didn't restart the spinner")
	}
}

// A message after an idle stretch starts a fresh 1s clock chain, and the
// slow chain it replaces ends at its next tick.
func TestClockChainSpeedsUpAfterIdle(t *testing.T) {
	m := NewModel("/p/s.jsonl", "s", true, "", false)
	m = sized(t, m, 80, 24)
	now := time.Now()
	idle := tallAnalysis(1)
	idle.EndTime = now.Add(-10 * time.Minute)
	m = load(t, m, idle)
	if m.clockInterval() != 15*time.Second {
		t.Fatalf("idle interval = %v, want 15s", m.clockInterval())
	}
	oldGen := m.clockGen

	busy := tallAnalysis(2)
	busy.EndTime = now
	m = load(t, m, busy)
	if m.clockGen == oldGen {
		t.Fatal("a fresh message didn't start a faster clock chain")
	}
	if _, cmd := m.Update(clockMsg{gen: oldGen}); cmd != nil {
		t.Error("the superseded slow chain kept ticking")
	}
	if _, cmd := m.Update(clockMsg{gen: m.clockGen}); cmd == nil {
		t.Error("the live chain stopped")
	}
}

// batchHas runs cmd (unwrapping batches, skipping commands that block) and
// reports whether any resulting message satisfies want.
func batchHas(cmd tea.Cmd, want func(tea.Msg) bool) bool {
	if cmd == nil {
		return false
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if batchHas(c, want) {
					return true
				}
			}
			return false
		}
		return want(msg)
	case <-time.After(500 * time.Millisecond):
		return false
	}
}
