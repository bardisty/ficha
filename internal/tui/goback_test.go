package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/bardisty/ficha/internal/models"
)

// goBackProject writes empty session files for ids into a temp project
// directory, each a minute newer than the one before, so a test can make an
// older one the latest written. It returns the directory and each session's
// path.
func goBackProject(t *testing.T, ids ...string) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, len(ids))
	base := time.Now().Add(-time.Hour)
	for i, id := range ids {
		paths[i] = filepath.Join(dir, id+".jsonl")
		if err := os.WriteFile(paths[i], nil, 0o600); err != nil {
			t.Fatal(err)
		}
		at := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(paths[i], at, at); err != nil {
			t.Fatal(err)
		}
	}
	return dir, paths
}

// goBackBreakdown is breakdown at 80x24 on 40 rows of session A, following,
// with a session watcher on dir.
func goBackBreakdown(t *testing.T, dir, path string) BreakdownModel {
	t.Helper()
	m := NewBreakdownModel(path, sessA, true, "", true)
	m.now = func() time.Time { return goldenTime(12, 0, 0) }
	m.sessionWatcher = NewSessionWatcher(dir, sessA)
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = model.Update(breakdownMsgsMsg{messages: sortRows(40), insights: &models.MessageInsights{}})
	return model.(BreakdownModel)
}

// awayAndBack follows a new session B, then presses - and lets A's first
// load land with rows, as A's file would have them by then.
func awayAndBack(t *testing.T, m BreakdownModel, pathB string, rows []models.BreakdownMessage) BreakdownModel {
	t.Helper()
	updated, _ := m.Update(sessionActivityMsg{path: pathB, id: sessB, created: true})
	m = updated.(BreakdownModel)
	if m.sessionID != sessB {
		t.Fatalf("setup: following didn't switch to B")
	}
	updated, _ = m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(12, 0, 0), 3), insights: &models.MessageInsights{}})
	m = press(t, updated.(BreakdownModel), "-")
	updated, _ = m.Update(breakdownMsgsMsg{messages: rows, insights: &models.MessageInsights{}})
	return updated.(BreakdownModel)
}

// Scrolled up and switched away by follow, - brings back the same first
// row on screen, and rows that landed meanwhile count as new.
func TestBreakdownGoBackKeepsTheRow(t *testing.T) {
	dir, paths := goBackProject(t, sessA, sessB)
	m := press(t, goBackBreakdown(t, dir, paths[0]), "g", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j")
	before := m.positionText()
	if !strings.HasPrefix(before, "rows 11-") {
		t.Fatalf("setup: position %q", before)
	}
	m = awayAndBack(t, m, paths[1], sortRows(43))
	if m.sessionID != sessA || m.followMode {
		t.Fatalf("after -: session %s following %v, want A pinned", m.sessionID, m.followMode)
	}
	if got, want := m.positionText(), strings.TrimSuffix(before, " of 40")+" of 43 • 3 new ↓ (G)"; got != want {
		t.Errorf("position %q, want %q", got, want)
	}
}

// Following at the bottom, - comes back on the newest row and stays there
// as rows arrive.
func TestBreakdownGoBackKeepsFollowingTheBottom(t *testing.T) {
	dir, paths := goBackProject(t, sessA, sessB)
	m := awayAndBack(t, goBackBreakdown(t, dir, paths[0]), paths[1], sortRows(43))
	if !m.autoScroll || !m.viewport.AtBottom() {
		t.Fatalf("after -: autoScroll %v at bottom %v", m.autoScroll, m.viewport.AtBottom())
	}
	updated, _ := m.Update(breakdownMsgsMsg{messages: sortRows(45), insights: &models.MessageInsights{}})
	if m = updated.(BreakdownModel); !m.viewport.AtBottom() || strings.Contains(m.positionText(), "new") {
		t.Errorf("a row arriving after - left the bottom: %q", m.positionText())
	}
}

// Sorted with p on the second costliest row, - comes back sorted, on the
// same ranks, with the same row marked, and the next p goes on to the third.
// s still returns to the time-order line the reader left.
func TestBreakdownGoBackKeepsSortAndSelection(t *testing.T) {
	dir, paths := goBackProject(t, sessA, sessB)
	m := press(t, goBackBreakdown(t, dir, paths[0]), "g", "j", "j", "j", "j", "s", "p", "p")
	rank, key, position := m.selectedRank(m.topByCost()), m.selectedKey, m.positionText()
	if rank != 1 {
		t.Fatalf("setup: rank %d", rank)
	}
	m = awayAndBack(t, m, paths[1], sortRows(40))
	if !m.sortByCost || m.selectedKey != key || m.positionText() != position {
		t.Fatalf("after -: sorted %v, selection kept %v, position %q, want %q", m.sortByCost, m.selectedKey == key, m.positionText(), position)
	}
	if !strings.Contains(tableContent(m), "▸") {
		t.Error("the selected row lost its mark")
	}
	if m = press(t, m, "p"); m.selectedRank(m.topByCost()) != 2 {
		t.Errorf("p after - went to rank %d, want 2", m.selectedRank(m.topByCost()))
	}
	if m = press(t, m, "s"); !strings.HasPrefix(m.positionText(), "rows 5-") {
		t.Errorf("s after - returned to %q, want the line left at rows 5-", m.positionText())
	}
}

// A selected row that's gone when - comes back drops the selection; the
// view keeps the rest.
func TestBreakdownGoBackDropsAGoneSelection(t *testing.T) {
	dir, paths := goBackProject(t, sessA, sessB)
	m := press(t, goBackBreakdown(t, dir, paths[0]), "s", "p")
	rows := sortRows(40)
	for i := range rows {
		rows[i].Cost.TotalCost += 0.001 // every key changes
	}
	m = awayAndBack(t, m, paths[1], rows)
	if !m.sortByCost || m.selectedKey != "" {
		t.Errorf("after -: sorted %v, selection %q, want sorted with none", m.sortByCost, m.selectedKey)
	}
}

// A switch to a session, rather than back to one, opens at its newest row
// in time order, whatever the reader had in the session left.
func TestBreakdownSwitchToOpensAtTheNewest(t *testing.T) {
	dir, paths := goBackProject(t, sessA, sessB)
	m := press(t, goBackBreakdown(t, dir, paths[0]), "s", "p")
	updated, _ := m.Update(sessionActivityMsg{path: paths[1], id: sessB, created: true})
	updated, _ = updated.Update(breakdownMsgsMsg{messages: sortRows(40), insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if m.sortByCost || m.selectedKey != "" || !m.autoScroll {
		t.Errorf("switched to B: sorted %v, selection %q, following rows %v", m.sortByCost, m.selectedKey, m.autoScroll)
	}
}

// In both views, following, - pins on the older session, and - again back
// on the newest follows again, so the next new session is followed. A
// session left pinned comes back pinned.
func TestGoBackRestoresFollowOnTheNewest(t *testing.T) {
	type view struct {
		open   func(t *testing.T, dir, path string, follow bool) tea.Model
		id     func(tea.Model) string
		follow func(tea.Model) bool
	}
	views := map[string]view{
		"watch": {
			open: func(t *testing.T, dir, path string, follow bool) tea.Model {
				m := NewModel(path, sessA, true, "", follow)
				m.now = func() time.Time { return goldenTime(10, 2, 0) }
				m.sessionWatcher = NewSessionWatcher(dir, sessA)
				return load(t, sized(t, m, 80, 24), tallAnalysis(30.05))
			},
			id:     func(m tea.Model) string { return m.(Model).sessionID },
			follow: func(m tea.Model) bool { return m.(Model).followMode },
		},
		"breakdown": {
			open: func(t *testing.T, dir, path string, follow bool) tea.Model {
				m := goBackBreakdown(t, dir, path)
				m.followMode = follow
				return m
			},
			id:     func(m tea.Model) string { return m.(BreakdownModel).sessionID },
			follow: func(m tea.Model) bool { return m.(BreakdownModel).followMode },
		},
	}
	const sessC = "cccccccc-0000-0000-0000-000000000000"
	for name, v := range views {
		t.Run(name, func(t *testing.T) {
			dir, paths := goBackProject(t, sessA, sessB)
			m, _ := v.open(t, dir, paths[0], true).Update(sessionActivityMsg{path: paths[1], id: sessB, created: true})
			if m = press(t, m, "-"); v.id(m) != sessA || v.follow(m) {
				t.Fatalf("first -: session %s following %v, want A pinned", v.id(m), v.follow(m))
			}
			if m = press(t, m, "-"); v.id(m) != sessB || !v.follow(m) {
				t.Fatalf("second -: session %s following %v, want B following", v.id(m), v.follow(m))
			}
			pathC := filepath.Join(dir, sessC+".jsonl")
			if m, _ = m.Update(sessionActivityMsg{path: pathC, id: sessC, created: true}); v.id(m) != sessC {
				t.Errorf("a new session after - - wasn't followed: on %s", v.id(m))
			}

			// Pinned on A, n to B, then - and - again: B was left pinned.
			dir, paths = goBackProject(t, sessA, sessB)
			m, _ = v.open(t, dir, paths[0], false).Update(sessionActivityMsg{path: paths[1], id: sessB, created: true})
			m = press(t, m, "n", "-", "-")
			if v.id(m) != sessB || v.follow(m) {
				t.Errorf("pinned round trip: session %s following %v, want B pinned", v.id(m), v.follow(m))
			}
		})
	}
}

// In a dated session, whose day dividers come back with its first load, -
// lands on the row left, not a line off it, and does again after a second
// round trip through B.
func TestBreakdownGoBackKeepsTheRowWhenDated(t *testing.T) {
	dir, paths := goBackProject(t, sessA, sessB)
	m := NewBreakdownModel(paths[0], sessA, true, "", true)
	m.now = func() time.Time { return goldenTime(12, 0, 0).AddDate(0, 0, 2) }
	m.sessionWatcher = NewSessionWatcher(dir, sessA)
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = model.Update(breakdownMsgsMsg{messages: twoDayRows(), insights: &models.MessageInsights{}})
	m = model.(BreakdownModel)
	m = press(t, m, "g", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j")
	before := m.positionText()
	if !strings.Contains(before, "Thu 15 Jan • rows 20-") {
		t.Fatalf("setup: position %q", before)
	}
	m = awayAndBack(t, m, paths[1], twoDayRows())
	if got := m.positionText(); got != before {
		t.Fatalf("after -: position %q, want %q", got, before)
	}
	m = press(t, m, "-")
	updated, _ := m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(12, 0, 0), 3), insights: &models.MessageInsights{}})
	m = press(t, updated.(BreakdownModel), "-")
	updated, _ = m.Update(breakdownMsgsMsg{messages: twoDayRows(), insights: &models.MessageInsights{}})
	if got := updated.(BreakdownModel).positionText(); got != before {
		t.Errorf("after a second round trip: position %q, want %q", got, before)
	}
}

// Rows already new when the reader left stay new after -, along with those
// that landed while away.
func TestBreakdownGoBackKeepsTheNewCount(t *testing.T) {
	dir, paths := goBackProject(t, sessA, sessB)
	m := press(t, goBackBreakdown(t, dir, paths[0]), "g", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j")
	updated, _ := m.Update(breakdownMsgsMsg{messages: sortRows(45), insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if !strings.HasSuffix(m.positionText(), "5 new ↓ (G)") {
		t.Fatalf("setup: position %q", m.positionText())
	}
	if m = awayAndBack(t, m, paths[1], sortRows(47)); !strings.HasSuffix(m.positionText(), "of 47 • 7 new ↓ (G)") {
		t.Errorf("after -: position %q, want 7 new", m.positionText())
	}
}

// Following goes by which session started last, not which was written last:
// the older session writing meanwhile doesn't cost the followed one its
// follow mode on the way back.
func TestGoBackFollowsTheLastStartedSession(t *testing.T) {
	dir, paths := goBackProject(t, sessA, sessB)
	updated, _ := goBackBreakdown(t, dir, paths[0]).Update(sessionActivityMsg{path: paths[1], id: sessB, created: true})
	m := press(t, updated.(BreakdownModel), "-")
	now := time.Now()
	if err := os.Chtimes(paths[0], now, now); err != nil {
		t.Fatal(err)
	}
	if m = press(t, m, "-"); m.sessionID != sessB || !m.followMode {
		t.Errorf("back on B after A wrote: session %s following %v, want B following", m.sessionID, m.followMode)
	}
}

// - pressed again before the session's first load lands keeps the view it
// was bringing back for the next time.
func TestBreakdownGoBackTwiceBeforeTheLoad(t *testing.T) {
	dir, paths := goBackProject(t, sessA, sessB)
	m := press(t, goBackBreakdown(t, dir, paths[0]), "g", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j")
	before := m.positionText()
	updated, _ := m.Update(sessionActivityMsg{path: paths[1], id: sessB, created: true})
	updated, _ = updated.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(12, 0, 0), 3), insights: &models.MessageInsights{}})
	m = press(t, updated.(BreakdownModel), "-", "-")
	updated, _ = m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(12, 0, 0), 3), insights: &models.MessageInsights{}})
	m = press(t, updated.(BreakdownModel), "-")
	updated, _ = m.Update(breakdownMsgsMsg{messages: sortRows(40), insights: &models.MessageInsights{}})
	if got := updated.(BreakdownModel).positionText(); got != before {
		t.Errorf("position %q, want %q", got, before)
	}
}

// With no session seen start, the one the view opened on is the one
// following is on: n to an older session that wrote, then - comes back
// following, and - again to the older one pins.
func TestGoBackFollowsTheOpenedSessionBeforeAnyStart(t *testing.T) {
	views := map[string]func(t *testing.T, dir, path string) tea.Model{
		"watch": func(t *testing.T, dir, path string) tea.Model {
			m := NewModel(path, sessA, true, "", true)
			m.now = func() time.Time { return goldenTime(10, 2, 0) }
			m.sessionWatcher = NewSessionWatcher(dir, sessA)
			return load(t, sized(t, m, 80, 24), tallAnalysis(30.05))
		},
		"breakdown": func(t *testing.T, dir, path string) tea.Model { return goBackBreakdown(t, dir, path) },
	}
	follow := func(m tea.Model) (string, bool) {
		if w, ok := m.(Model); ok {
			return w.sessionID, w.followMode
		}
		b := m.(BreakdownModel)
		return b.sessionID, b.followMode
	}
	for name, open := range views {
		t.Run(name, func(t *testing.T) {
			dir, paths := goBackProject(t, sessB, sessA) // A is newest
			m := open(t, dir, paths[1])
			now := time.Now()
			if err := os.Chtimes(paths[0], now, now); err != nil {
				t.Fatal(err)
			}
			m, _ = m.Update(sessionActivityMsg{path: paths[0], id: sessB})
			if id, _ := follow(press(t, m, "n")); id != sessB {
				t.Fatalf("setup: n went to %s", id)
			}
			if id, on := follow(press(t, m, "n", "-")); id != sessA || !on {
				t.Errorf("back on A: session %s following %v, want A following", id, on)
			}
			if id, on := follow(press(t, m, "n", "-", "-")); id != sessB || on {
				t.Errorf("back on B: session %s following %v, want B pinned", id, on)
			}
		})
	}
}
