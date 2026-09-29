package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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
	forceProfile(t, termenv.Ascii)
	for _, sz := range []struct{ w, h int }{{100, 40}, {80, 24}, {120, 10}, {60, 20}, {40, 15}, {39, 7}, {200, 60}} {
		m := loadedBreakdown(t, sz.w, sz.h, chromeRows(goldenTime(10, 0, 0), 100))
		lines := strings.Split(m.View(), "\n")
		if len(lines) != sz.h {
			t.Errorf("%dx%d: frame is %d rows", sz.w, sz.h, len(lines))
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > sz.w {
				t.Errorf("%dx%d: row %d is %d wide: %q", sz.w, sz.h, i, w, l)
			}
		}
		// The rows the table shows are the rows the viewport was given.
		if got, want := m.viewport.Height, sz.h-m.headerRows()-breakdownFooterRows; got != want {
			t.Errorf("%dx%d: viewport has %d rows, want %d", sz.w, sz.h, got, want)
		}
	}
}

func TestBreakdownView_CompactAndTooSmall(t *testing.T) {
	m := loadedBreakdown(t, 120, 10, chromeRows(goldenTime(10, 0, 0), 5))
	if out := m.View(); strings.Contains(out, "╔") || !strings.Contains(out, "0a1b2c3d") {
		t.Errorf("a 10-row terminal should get the unboxed header:\n%s", out)
	}
	m = loadedBreakdown(t, 120, 40, chromeRows(goldenTime(10, 0, 0), 5))
	if out := m.View(); !strings.Contains(out, "╔") {
		t.Errorf("a 40-row terminal should keep the boxed header:\n%s", out)
	}
	// The narrowest layout (#, TIME, MODEL, COST) is 39 columns; any less
	// would clip the digits off the costs.
	for _, sz := range []struct{ w, h int }{{38, 10}, {25, 10}, {80, 6}} {
		m = loadedBreakdown(t, sz.w, sz.h, chromeRows(goldenTime(10, 0, 0), 5))
		if out := m.View(); strings.TrimSpace(out) != "terminal too small" {
			t.Errorf("%dx%d should say the terminal is too small, got:\n%s", sz.w, sz.h, out)
		}
	}
	m = loadedBreakdown(t, 39, 10, chromeRows(goldenTime(10, 0, 0), 5))
	if out := m.View(); !strings.Contains(out, "$0.0500") {
		t.Errorf("at 39 columns every cost should show whole:\n%s", out)
	}
}

// A load error shows in words, in both frames, and without color.
func TestBreakdownView_ShowsLoadError(t *testing.T) {
	for _, h := range []int{24, 12} {
		m := loadedBreakdown(t, 100, h, chromeRows(goldenTime(10, 0, 0), 5))
		updated, _ := m.Update(breakdownErrorMsg{err: errors.New("boom")})
		m = updated.(BreakdownModel)
		if out := m.View(); !strings.Contains(out, "boom, showing last data") {
			t.Errorf("height %d: the error should be on screen:\n%s", h, out)
		}
	}
	m := NewBreakdownModel("/fixture/sess.jsonl", "0a1b2c3d", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(BreakdownModel)
	updated, _ = m.Update(breakdownErrorMsg{err: errors.New("boom")})
	m = updated.(BreakdownModel)
	if out := m.View(); !strings.Contains(out, "boom") || strings.Contains(out, emptyStateText) {
		t.Errorf("a failed first load should show the error, not the empty state:\n%s", out)
	}
}

// Scrolling before the first load lands must not count the whole session
// as rows that arrived while scrolled up.
func TestBreakdownPosition_ScrollBeforeFirstLoad(t *testing.T) {
	m := NewBreakdownModel("/fixture/sess.jsonl", "0a1b2c3d", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m = updated.(BreakdownModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(BreakdownModel)
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
	if m.viewport.Height != 10 {
		t.Fatalf("viewport height = %d, want 10", m.viewport.Height)
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
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	m = updated.(BreakdownModel)
	updated, _ = m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(10, 0, 0), 35), insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if got, want := m.positionText(), "rows 1-10 of 35 • 5 new ↓ (G)"; got != want {
		t.Errorf("scrolled up: %q, want %q", got, want)
	}
	if rule := m.renderFooterRule(m.panelWidth()); !strings.Contains(rule, "[ rows 1-10 of 35 • 5 new ↓ (G) ]") {
		t.Errorf("the footer rule should carry the position: %q", rule)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	m = updated.(BreakdownModel)
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
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	m = updated.(BreakdownModel)
	if m.autoScroll || m.viewport.YOffset != 0 {
		t.Fatalf("setup: want scrolled to top, manual")
	}
	updated, _ = m.Update(sessionSwitchedMsg{newSessionPath: "/fixture/new.jsonl", newSessionID: "new"})
	m = updated.(BreakdownModel)
	if !m.autoScroll {
		t.Error("a switch should turn auto-scroll back on")
	}
	updated, _ = m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(11, 0, 0), 31), sessionPath: "/fixture/new.jsonl", insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if !m.viewport.AtBottom() {
		t.Errorf("the new session should open at its newest row (offset %d)", m.viewport.YOffset)
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
	if strings.Contains(m.View(), emptyStateText) {
		t.Error("the empty state should wait for the first load")
	}
	updated, _ = m.Update(breakdownMsgsMsg{})
	m = updated.(BreakdownModel)
	if !strings.Contains(m.View(), emptyStateText) {
		t.Errorf("an empty session should show the empty state:\n%s", m.View())
	}
	updated, _ = m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(10, 0, 0), 1)})
	m = updated.(BreakdownModel)
	if strings.Contains(m.View(), emptyStateText) {
		t.Error("the empty state should go once a row arrives")
	}
}
