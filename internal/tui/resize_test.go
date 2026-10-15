package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/bardisty/ficha/internal/models"
)

// A pty can report 0 rows (bare `script`, some CI shells). The viewport's
// height must clamp: handed a negative height, its line math panics inside
// GotoBottom. Both TUIs share the failure mode, so both get the same probe:
// zero-height WindowSizeMsg, then a data message that triggers
// SetContent + GotoBottom.

func TestWatchZeroHeightTerminalDoesNotPanic(t *testing.T) {
	m := NewModel("/test/path", "test-session", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 0})
	m = updated.(Model)

	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	analysis := &models.SessionAnalysis{
		SessionID:    "test-session",
		MessageCount: 12,
	}
	for i := 0; i < 12; i++ {
		analysis.Messages = append(analysis.Messages,
			chartMsg(base.Add(time.Duration(i)*time.Minute), "", 0.1))
	}

	// Panics here without the viewportHeight clamp.
	updated, _ = m.Update(analysisMsg{analysis: analysis})
	_ = frameOf(updated.(Model))
}

func TestBreakdownZeroHeightTerminalDoesNotPanic(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 0})
	m = updated.(BreakdownModel)

	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	var msgs []models.BreakdownMessage
	for i := 0; i < 12; i++ {
		msgs = append(msgs, models.BreakdownMessage{
			Index:     i + 1,
			Timestamp: base.Add(time.Duration(i) * time.Minute),
			Model:     "claude-sonnet-4",
			Cost:      models.CostBreakdown{TotalCost: 0.1},
		})
	}

	// Panics here without the viewportHeight clamp.
	updated, _ = m.Update(breakdownMsgsMsg{messages: msgs, totalCost: 1.2, minCost: 0.1, maxCost: 0.1})
	_ = frameOf(updated.(BreakdownModel))
}

// resizeBreakdown sends a WindowSizeMsg to breakdown.
func resizeBreakdown(t *testing.T, m BreakdownModel, w, h int) BreakdownModel {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(BreakdownModel)
}

// Growing the window clamps the view to the content, but shrinking it back,
// as a zoomed tmux pane does, returns to the line the reader chose. A view
// at watch's top stays at the top.
func TestWatchKeepsChosenLineThroughZoom(t *testing.T) {
	m := load(t, sized(t, NewModel("/fixture/sess.jsonl", "sess", true, "", false), 80, 24), tallAnalysis(1))
	if m = sized(t, sized(t, m, 200, 60), 80, 24); m.viewport.YOffset() != 0 {
		t.Errorf("at the top, grow and shrink moved the view to %d", m.viewport.YOffset())
	}
	m = press(t, m, "g", "j", "j", "j", "j", "j", "j", "j", "j")
	chosen := m.viewport.YOffset()
	if m = sized(t, m, 200, 60); m.viewport.YOffset() >= chosen {
		t.Fatalf("setup: growing to 200x60 should clamp offset %d, got %d", chosen, m.viewport.YOffset())
	}
	if m = sized(t, m, 80, 24); m.viewport.YOffset() != chosen {
		t.Errorf("after grow and shrink the view is at %d, want %d", m.viewport.YOffset(), chosen)
	}
}

// A body that shrinks on a reload, as when agents fold, clamps the view, and
// one that grows back returns it to the reader's line.
func TestWatchKeepsChosenLineWhenBodyShrinks(t *testing.T) {
	m := load(t, sized(t, NewModel("/fixture/sess.jsonl", "sess", true, "", false), 80, 24), tallAnalysis(1))
	m = press(t, m, "g", "j", "j", "j", "j", "j", "j", "j", "j")
	chosen := m.viewport.YOffset()
	short := tallAnalysis(1)
	short.Agents = short.Agents[:1]
	if m = load(t, m, short); m.viewport.YOffset() >= chosen {
		t.Fatalf("setup: the short body should clamp offset %d, got %d", chosen, m.viewport.YOffset())
	}
	if m = load(t, m, tallAnalysis(1)); m.viewport.YOffset() != chosen {
		t.Errorf("after the body grew back the view is at %d, want %d", m.viewport.YOffset(), chosen)
	}
}

// breakdown scrolled up into the table comes back to the same first row
// after a grow and shrink; following, it stays at the bottom.
func TestBreakdownKeepsChosenLineThroughZoom(t *testing.T) {
	m := loadedBreakdown(t, 80, 24, chromeRows(goldenTime(10, 0, 0), 100))
	if m = resizeBreakdown(t, resizeBreakdown(t, m, 200, 60), 80, 24); !m.autoScroll || !m.viewport.AtBottom() {
		t.Errorf("following, grow and shrink: following %v, at bottom %v", m.autoScroll, m.viewport.AtBottom())
	}
	for range 20 {
		m = press(t, m, "k")
	}
	chosen := m.viewport.YOffset()
	if m = resizeBreakdown(t, m, 200, 60); m.viewport.YOffset() >= chosen {
		t.Fatalf("setup: growing to 200x60 should clamp offset %d, got %d", chosen, m.viewport.YOffset())
	}
	if m = resizeBreakdown(t, m, 80, 24); m.viewport.YOffset() != chosen || m.autoScroll {
		t.Errorf("after grow and shrink the view is at %d, want %d; following %v", m.viewport.YOffset(), chosen, m.autoScroll)
	}
	// A reload at the grown size clamps too, and still gives the line back.
	m = resizeBreakdown(t, m, 200, 60)
	updated, _ := m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(10, 0, 0), 100), insights: &models.MessageInsights{}})
	if m = resizeBreakdown(t, updated.(BreakdownModel), 80, 24); m.viewport.YOffset() != chosen {
		t.Errorf("after a reload while grown the view is at %d, want %d", m.viewport.YOffset(), chosen)
	}
}

// Sorted by cost, a reload while grown keeps the reader's line too, and so
// does leaving cost order while grown.
func TestBreakdownSortKeepsChosenLineThroughZoom(t *testing.T) {
	rows := chromeRows(goldenTime(10, 0, 0), 100)
	for i := range rows {
		rows[i].Cost.TotalCost = float64(200-i) / 1000
	}
	m := press(t, loadedBreakdown(t, 80, 24, rows), "s", "G", "k", "k", "k", "k", "k")
	chosen := m.viewport.YOffset()
	m = resizeBreakdown(t, m, 200, 60)
	updated, _ := m.Update(breakdownMsgsMsg{messages: rows, insights: &models.MessageInsights{}})
	if m = resizeBreakdown(t, updated.(BreakdownModel), 80, 24); m.viewport.YOffset() != chosen {
		t.Errorf("sorted, after a reload while grown the view is at %d, want %d", m.viewport.YOffset(), chosen)
	}

	m = loadedBreakdown(t, 80, 24, rows)
	for range 5 {
		m = press(t, m, "k")
	}
	chosen = m.viewport.YOffset()
	m = resizeBreakdown(t, press(t, m, "s"), 200, 60)
	if m = resizeBreakdown(t, press(t, m, "s"), 80, 24); m.viewport.YOffset() != chosen {
		t.Errorf("after leaving cost order while grown the view is at %d, want %d", m.viewport.YOffset(), chosen)
	}
}
