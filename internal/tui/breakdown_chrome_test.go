package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bardisty/ficha/internal/models"
)

// chromeRows builds n parent messages a minute apart from start.
func chromeRows(start time.Time, n int) []models.BreakdownMessage {
	msgs := make([]models.BreakdownMessage, n)
	for i := range msgs {
		msgs[i] = models.BreakdownMessage{
			Index: i + 1, Timestamp: start.Add(time.Duration(i) * time.Minute), Model: "claude-opus-4-8",
			Cost: models.CostBreakdown{TotalCost: 0.05},
		}
	}
	return msgs
}

func loadedBreakdown(t *testing.T, width, height int, msgs []models.BreakdownMessage) BreakdownModel {
	t.Helper()
	m := NewBreakdownModel("/fixture/sess.jsonl", "0a1b2c3d", true, "", false)
	m.now = func() time.Time { return goldenTime(12, 0, 0) }
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = updated.(BreakdownModel)
	updated, _ = m.Update(breakdownMsgsMsg{messages: msgs, insights: &models.MessageInsights{}})
	return updated.(BreakdownModel)
}

// The frame is exactly as tall as the terminal at every size: a frame one
// row too tall scrolls the header off, one too short leaves a dead row.
func TestBreakdownView_FillsTerminalExactly(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{100, 40}, {80, 24}, {120, 10}, {60, 20}, {40, 15}, {39, 7}, {200, 60}} {
		m := loadedBreakdown(t, sz.w, sz.h, chromeRows(goldenTime(10, 0, 0), 100))
		lines := strings.Split(frameOf(m), "\n")
		if len(lines) != sz.h {
			t.Errorf("%dx%d: frame is %d rows", sz.w, sz.h, len(lines))
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > sz.w {
				t.Errorf("%dx%d: row %d is %d wide: %q", sz.w, sz.h, i, w, l)
			}
		}
		// The rows the table shows are the rows the viewport was given.
		if got, want := m.viewport.Height(), sz.h-m.headerRows()-breakdownFooterRows; got != want {
			t.Errorf("%dx%d: viewport has %d rows, want %d", sz.w, sz.h, got, want)
		}
	}
}

func TestBreakdownView_CompactAndTooSmall(t *testing.T) {
	m := loadedBreakdown(t, 120, 10, chromeRows(goldenTime(10, 0, 0), 5))
	if out := frameOf(m); strings.Contains(out, "╔") || !strings.Contains(out, "0a1b2c3d") {
		t.Errorf("a 10-row terminal should get the unboxed header:\n%s", out)
	}
	m = loadedBreakdown(t, 120, 40, chromeRows(goldenTime(10, 0, 0), 5))
	if out := frameOf(m); !strings.Contains(out, "╔") {
		t.Errorf("a 40-row terminal should keep the boxed header:\n%s", out)
	}
	// The narrowest layout (#, TIME, MODEL, COST) is 39 columns; any less
	// would clip the digits off the costs.
	for _, sz := range []struct {
		w, h int
		need string
	}{{38, 10, "need 39 cols"}, {25, 10, "need 39 cols"}, {80, 6, "need 7 rows"}} {
		m = loadedBreakdown(t, sz.w, sz.h, chromeRows(goldenTime(10, 0, 0), 5))
		out := frameOf(m)
		for _, want := range []string{"terminal too small", sz.need, "q to quit"} {
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d should say %q, got:\n%s", sz.w, sz.h, want, out)
			}
		}
	}
	m = loadedBreakdown(t, 39, 10, chromeRows(goldenTime(10, 0, 0), 5))
	if out := frameOf(m); !strings.Contains(out, "$0.0500") {
		t.Errorf("at 39 columns every cost should show whole:\n%s", out)
	}
}

// A load error shows in words, in both frames, and without color.
func TestBreakdownView_ShowsLoadError(t *testing.T) {
	for _, h := range []int{24, 12} {
		m := loadedBreakdown(t, 100, h, chromeRows(goldenTime(10, 0, 0), 5))
		updated, _ := m.Update(breakdownErrorMsg{err: errors.New("boom")})
		m = updated.(BreakdownModel)
		if out := frameOf(m); !strings.Contains(out, "boom • r to retry") {
			t.Errorf("height %d: the error should be on screen:\n%s", h, out)
		}
	}
	m := NewBreakdownModel("/fixture/sess.jsonl", "0a1b2c3d", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(BreakdownModel)
	updated, _ = m.Update(breakdownErrorMsg{err: errors.New("boom")})
	m = updated.(BreakdownModel)
	if out := frameOf(m); !strings.Contains(out, "boom") || strings.Contains(out, emptyStateText) {
		t.Errorf("a failed first load should show the error, not the empty state:\n%s", out)
	}
}

// Scrolling before the first load lands must not count the whole session
// as rows that arrived while scrolled up.
func TestBreakdownPosition_ScrollBeforeFirstLoad(t *testing.T) {
	m := NewBreakdownModel("/fixture/sess.jsonl", "0a1b2c3d", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m = updated.(BreakdownModel)
	m = press(t, m, "g")
	updated, _ = m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(10, 0, 0), 40), insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if got := m.positionText(); strings.Contains(got, "new") {
		t.Errorf("the first load's rows aren't new: %q", got)
	}
	updated, _ = m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(10, 0, 0), 43), insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if got := m.positionText(); !strings.Contains(got, "3 new") {
		t.Errorf("rows after the first load are: %q", got)
	}
}

// The position counts message rows: a day divider in view is not a row.
func TestBreakdownPosition_SkipsDayDividers(t *testing.T) {
	// 20 rows, the day changing after row 10: the divider is content line 10.
	msgs := chromeRows(time.Date(2026, 1, 15, 23, 50, 0, 0, time.UTC), 20)
	m := loadedBreakdown(t, 100, 20, msgs) // 10 viewport rows
	if m.viewport.Height() != 10 {
		t.Fatalf("viewport height = %d, want 10", m.viewport.Height())
	}
	m.setFollow(false)
	m.viewport.SetYOffset(10) // the divider is the top line
	if got := m.positionText(); got != "rows 11-19 of 20" {
		t.Errorf("position with a divider at the top = %q, want %q", got, "rows 11-19 of 20")
	}
	m.viewport.SetYOffset(5) // the divider is mid-view
	if got := m.positionText(); got != "rows 6-14 of 20" {
		t.Errorf("position with a divider mid-view = %q, want %q", got, "rows 6-14 of 20")
	}
}

// Rows that arrive while scrolled up are counted until the reader follows
// again. A table that fits has no position to report.
func TestBreakdownPosition_CountsNewRowsWhileScrolledUp(t *testing.T) {
	m := loadedBreakdown(t, 100, 20, chromeRows(goldenTime(10, 0, 0), 30))
	if got := m.positionText(); got != "rows 21-30 of 30" {
		t.Errorf("following: %q", got)
	}
	m = press(t, m, "g")
	updated, _ := m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(10, 0, 0), 35), insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if got, want := m.positionText(), "rows 1-10 of 35 • 5 new ↓ (G)"; got != want {
		t.Errorf("scrolled up: %q, want %q", got, want)
	}
	if rule := m.renderFooterRule(m.panelWidth()); !strings.Contains(rule, "[ rows 1-10 of 35 • 5 new ↓ (G) ]") {
		t.Errorf("the footer rule should carry the position: %q", rule)
	}
	m = press(t, m, "G")
	if got := m.positionText(); got != "rows 26-35 of 35" {
		t.Errorf("after G: %q", got)
	}

	short := loadedBreakdown(t, 100, 40, chromeRows(goldenTime(10, 0, 0), 5))
	if got := short.positionText(); got != "" {
		t.Errorf("a table that fits should report no position, got %q", got)
	}
}

// Following a new session drops the old session's scroll: the new one opens
// following its newest row.
func TestBreakdownSessionSwitch_ResetsScroll(t *testing.T) {
	m := loadedBreakdown(t, 100, 20, chromeRows(goldenTime(10, 0, 0), 50))
	m = press(t, m, "g")
	if m.autoScroll || m.viewport.YOffset() != 0 {
		t.Fatalf("setup: want scrolled to top, manual")
	}
	m.followMode = true
	updated, _ := m.Update(sessionActivityMsg{path: "/fixture/new.jsonl", id: "new", created: true})
	m = updated.(BreakdownModel)
	if !m.autoScroll {
		t.Error("a switch should turn auto-scroll back on")
	}
	updated, _ = m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(11, 0, 0), 31), sessionPath: "/fixture/new.jsonl", insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if !m.viewport.AtBottom() {
		t.Errorf("the new session should open at its newest row (offset %d)", m.viewport.YOffset())
	}
	if got := m.positionText(); strings.Contains(got, "new") {
		t.Errorf("the old session's pause must not count the new session's rows as new: %q", got)
	}
}

// Once a load finds no replies, the table says so; while loading it doesn't.
func TestBreakdownEmptyState(t *testing.T) {
	m := NewBreakdownModel("/fixture/sess.jsonl", "0a1b2c3d", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(BreakdownModel)
	if strings.Contains(frameOf(m), emptyStateText) {
		t.Error("the empty state should wait for the first load")
	}
	updated, _ = m.Update(breakdownMsgsMsg{})
	m = updated.(BreakdownModel)
	if !strings.Contains(frameOf(m), emptyStateText) {
		t.Errorf("an empty session should show the empty state:\n%s", frameOf(m))
	}
	updated, _ = m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(10, 0, 0), 1)})
	m = updated.(BreakdownModel)
	if strings.Contains(frameOf(m), emptyStateText) {
		t.Error("the empty state should go once a row arrives")
	}
}

// A downward key resumes following even when the view is already at the
// bottom and can't move: on a short table, or after p lands on a last-page
// row.
func TestBreakdownDownKeyAtBottomResumesFollowing(t *testing.T) {
	for _, k := range []string{"j", "down", "space"} {
		// Short table: g turns following off without moving anything
		m := press(t, loadedBreakdown(t, 100, 30, chromeRows(goldenTime(10, 0, 0), 10)), "g", k)
		if !m.autoScroll {
			t.Errorf("%s at the bottom of a short table should resume following", k)
		}
		updated, _ := m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(10, 0, 0), 60), insights: &models.MessageInsights{}})
		if m = updated.(BreakdownModel); !m.viewport.AtBottom() {
			t.Errorf("%s: new rows should keep the view at the bottom", k)
		}

		// p on a last-page row clamps to the bottom with following off
		rows := chromeRows(goldenTime(10, 0, 0), 40)
		rows[39].Cost.TotalCost = 9
		m = press(t, loadedBreakdown(t, 100, 20, rows), "p")
		if m.autoScroll || !m.viewport.AtBottom() {
			t.Fatalf("setup: p should stop following at the bottom")
		}
		if m = press(t, m, k); !m.autoScroll {
			t.Errorf("%s after p at the bottom should resume following", k)
		}
	}
}

// Pinned, a new session leaves the view where it is; f follows, as in watch.
func TestBreakdownFollowToggle(t *testing.T) {
	m := loadedBreakdown(t, 100, 24, chromeRows(goldenTime(10, 0, 0), 5))
	updated, _ := m.Update(sessionActivityMsg{path: "/fixture/new.jsonl", id: "new", created: true})
	if m = updated.(BreakdownModel); m.sessionPath != "/fixture/sess.jsonl" {
		t.Fatalf("pinned breakdown switched to %s", m.sessionPath)
	}
	if m = press(t, m, "f"); !m.followMode || !strings.Contains(frameOf(m), "FOLLOWING") {
		t.Fatalf("f should turn following on and show it:\n%s", frameOf(m))
	}
	updated, _ = m.Update(sessionActivityMsg{path: "/fixture/new.jsonl", id: "new", created: true})
	if m = updated.(BreakdownModel); m.sessionPath != "/fixture/new.jsonl" {
		t.Errorf("following breakdown stayed on %s", m.sessionPath)
	}
}

// With no session yet, breakdown waits and says where, then opens the first
// session to appear, pinned or not.
func TestBreakdownWaitingForFirstSession(t *testing.T) {
	m := NewWaitingBreakdownModel("/projects/-work-webapp", "~/work/webapp", true, false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(BreakdownModel)
	out := frameOf(m)
	for _, want := range []string{"Waiting for a Claude Code session in ~/work/webapp", "webapp", "waiting for a session"} {
		if !strings.Contains(out, want) {
			t.Errorf("waiting view lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, emptyStateText) {
		t.Errorf("waiting isn't the empty state:\n%s", out)
	}
	// Keys that act on rows or a session are harmless with neither
	m = press(t, m, "p", "-", "r", "j", "g", "G")
	updated, _ = m.Update(sessionActivityMsg{path: "/projects/-work-webapp/first.jsonl", id: "first", created: true})
	if m = updated.(BreakdownModel); m.sessionPath != "/projects/-work-webapp/first.jsonl" || !m.loading {
		t.Errorf("a waiting breakdown should open the first session, pinned or not (path %q)", m.sessionPath)
	}
	if got := m.renderNotifyRow(); strings.Contains(got, "go back") {
		t.Errorf("offers going back to nothing: %q", got)
	}
}

// ctrl+z suspends only where the shell can resume it; without job control
// (a tmux pane started with a command) it would leave a blank screen.
func TestBreakdownCtrlZSuspends(t *testing.T) {
	m := loadedBreakdown(t, 100, 24, chromeRows(goldenTime(10, 0, 0), 5))
	_, cmd := m.Update(keyMsg(t, "ctrl+z"))
	if !canSuspend() {
		if cmd != nil {
			t.Error("ctrl+z suspended without job control")
		}
		return
	}
	if cmd == nil {
		t.Fatal("ctrl+z returned no command")
	} else if _, ok := cmd().(tea.SuspendMsg); !ok {
		t.Error("ctrl+z didn't suspend")
	}
}

// - goes back to the session open before the last switch and pins there, as
// in watch. p stays peak and never changes session.
func TestBreakdownGoBack(t *testing.T) {
	m := loadedBreakdown(t, 100, 24, chromeRows(goldenTime(10, 0, 0), 5))
	if m = press(t, m, "-"); m.sessionPath != "/fixture/sess.jsonl" {
		t.Fatalf("- with no previous session switched to %s", m.sessionPath)
	}

	m.followMode = true
	updated, _ := m.Update(sessionActivityMsg{path: "/fixture/new.jsonl", id: "new", created: true})
	m = updated.(BreakdownModel)
	if got := m.renderNotifyRow(); !strings.Contains(got, "→ new session new (previous 0a1b2c3d: $0.2500) • - to go back") {
		t.Errorf("switch notice = %q, want it to offer going back", got)
	}
	if m = press(t, m, "p"); m.sessionPath != "/fixture/new.jsonl" {
		t.Fatalf("p switched sessions to %s", m.sessionPath)
	}

	m = press(t, m, "-")
	if m.sessionPath != "/fixture/sess.jsonl" || m.followMode {
		t.Fatalf("after -: session %s follow=%v, want /fixture/sess.jsonl pinned", m.sessionPath, m.followMode)
	}
	if got := m.renderNotifyRow(); !strings.Contains(got, "→ switched to 0a1b2c3d • - to go back") {
		t.Errorf("go-back notice = %q", got)
	}
	if !strings.Contains(frameOf(m), "PINNED") {
		t.Errorf("header doesn't say PINNED after going back:\n%s", frameOf(m))
	}
}

// The rows tableView draws are the whole table's lines at the viewport's
// offset, day dividers included, wherever it's scrolled.
func TestBreakdownTableViewMatchesTableAtEveryOffset(t *testing.T) {
	rows := chromeRows(goldenTime(22, 0, 0), 60)
	// A day divider partway down.
	for i := 30; i < len(rows); i++ {
		rows[i].Timestamp = rows[i].Timestamp.Add(24 * time.Hour)
	}
	m := loadedBreakdown(t, 100, 20, rows)
	all := strings.Split(tableContent(m), "\n")
	if len(all) != len(rows)+1 {
		t.Fatalf("table has %d lines, want %d rows and a divider", len(all), len(rows)+1)
	}
	for _, offset := range []int{0, 1, 25, 29, 30, len(all) - m.viewport.Height()} {
		m.viewport.SetYOffset(offset)
		got := strings.Split(m.tableView(), "\n")
		want := all[offset:min(offset+m.viewport.Height(), len(all))]
		if len(got) != m.viewport.Height() {
			t.Fatalf("offset %d: %d lines, want the viewport's %d", offset, len(got), m.viewport.Height())
		}
		for i, line := range want {
			if strings.TrimRight(got[i], " ") != strings.TrimRight(line, " ") {
				t.Errorf("offset %d line %d: %q, want %q", offset, i, got[i], line)
			}
		}
	}
}
