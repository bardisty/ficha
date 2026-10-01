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
