package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFormatCompactCost(t *testing.T) {
	tests := []struct {
		name     string
		cost     float64
		expected string
	}{
		{"zero", 0, "$0.0000"},
		{"small cost", 0.0512, "$0.0512"},
		{"one dollar", 1.0, "$1.0000"},
		{"ten dollars", 10.5, "$10.500"},
		{"hundred dollars", 123.45, "$123.45"},
		{"large cost", 999.99, "$999.99"},
		{"very small", 0.0001, "$0.0001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatCompactCost(tt.cost)
			if result != tt.expected {
				t.Errorf("formatCompactCost(%f) = %q, want %q", tt.cost, result, tt.expected)
			}
		})
	}
}

// bdMsg builds a message with a fixed timestamp (minutes offset) and optional
// agent ID. Its identity — what highlight tracking keys on — is stable across
// reloads regardless of the row's Index, so tests can add rows and reorder them.
func bdMsg(index, minute int, agentID string) models.BreakdownMessage {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	return models.BreakdownMessage{
		Index:     index,
		AgentID:   agentID,
		Timestamp: base.Add(time.Duration(minute) * time.Minute),
	}
}

func TestBreakdownModel_DetectNewMessages(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	// Currently displayed: three messages at minutes 0, 1, 2.
	m.messages = []models.BreakdownMessage{bdMsg(1, 0, ""), bdMsg(2, 1, ""), bdMsg(3, 2, "")}

	// Reload appends two more (minutes 3, 4). Indices are irrelevant to detection.
	newMessages := []models.BreakdownMessage{
		bdMsg(1, 0, ""), bdMsg(2, 1, ""), bdMsg(3, 2, ""), bdMsg(4, 3, ""), bdMsg(5, 4, ""),
	}
	m.detectNewMessages(newMessages)

	if !m.isNewMessage(bdMsg(4, 3, "")) {
		t.Error("expected the minute-3 message to be marked new")
	}
	if !m.isNewMessage(bdMsg(5, 4, "")) {
		t.Error("expected the minute-4 message to be marked new")
	}
	if m.isNewMessage(bdMsg(1, 0, "")) {
		t.Error("the minute-0 message already existed and should not be new")
	}
	if m.isNewMessage(bdMsg(3, 2, "")) {
		t.Error("the minute-2 message already existed and should not be new")
	}
}

// A newly discovered agent message whose timestamp precedes existing
// rows inserts mid-list; the sort then renumbers the old tail rows past the old
// count. Identity-based detection must flag only the inserted agent row, not the
// shifted parent rows.
func TestBreakdownModel_DetectNewMessages_MidListInsertion(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	// Displayed: three parent rows at minutes 0, 2, 4 (indices 1..3).
	m.messages = []models.BreakdownMessage{bdMsg(1, 0, ""), bdMsg(2, 2, ""), bdMsg(3, 4, "")}

	// Reload: an agent row at minute 1 lands mid-list; after the timestamp sort
	// and 1..N reindex the parent rows shift to indices 3 and 4.
	agentRow := bdMsg(2, 1, "x")
	reloaded := []models.BreakdownMessage{
		bdMsg(1, 0, ""), agentRow, bdMsg(3, 2, ""), bdMsg(4, 4, ""),
	}
	m.detectNewMessages(reloaded)

	if !m.isNewMessage(agentRow) {
		t.Error("the inserted agent row should be flagged new")
	}
	// The parent row now at index 4 must NOT be flagged just because its index
	// exceeds the old count of 3 — that was the positional-key bug.
	if m.isNewMessage(bdMsg(4, 4, "")) {
		t.Error("a pre-existing parent row must not flash as new after a mid-list insertion shifted its index")
	}
	if m.isNewMessage(bdMsg(3, 2, "")) {
		t.Error("a pre-existing parent row must not be flagged after reindexing")
	}
}

func TestBreakdownModel_IsNewMessage(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	newRow := bdMsg(5, 5, "")
	m.newMsgKeys[breakdownMsgKey(newRow)] = time.Now()

	if !m.isNewMessage(newRow) {
		t.Error("expected the tracked message to be highlighted")
	}
	if m.isNewMessage(bdMsg(10, 10, "")) {
		t.Error("expected an untracked message to not be highlighted")
	}
}

func TestBreakdownModel_IsNewMessage_NoColor(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", true, "", false) // noColor = true

	newRow := bdMsg(5, 5, "")
	m.newMsgKeys[breakdownMsgKey(newRow)] = time.Now()

	if m.isNewMessage(newRow) {
		t.Error("expected no highlight in noColor mode")
	}
}

func TestBreakdownModel_CleanupExpiredHighlights(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	expired := bdMsg(1, 0, "")
	fresh := bdMsg(2, 1, "")
	m.newMsgKeys[breakdownMsgKey(expired)] = time.Now().Add(-3 * time.Second) // highlightDuration is 2s
	m.newMsgKeys[breakdownMsgKey(fresh)] = time.Now()

	m.cleanupExpiredHighlights()

	if _, exists := m.newMsgKeys[breakdownMsgKey(expired)]; exists {
		t.Error("expected expired highlight to be cleaned up")
	}
	if _, exists := m.newMsgKeys[breakdownMsgKey(fresh)]; !exists {
		t.Error("expected fresh highlight to remain")
	}
}

func TestBreakdownModel_RenderRow(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", true, "", false) // noColor for predictable output

	msg := models.BreakdownMessage{
		Index:     42,
		AgentID:   "",
		Timestamp: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC),
		Model:     "claude-sonnet-4",
		Usage: models.TokenUsage{
			InputTokens:          1234,
			OutputTokens:         56,
			CacheReadInputTokens: 89300,
		},
		Cost: models.CostBreakdown{
			TotalCost: 0.0512,
		},
	}

	row := m.renderRow(msg, false, 0.0, true) // prevCost=0, isFirst=true

	// Check that the row contains expected values
	if !strings.Contains(row, "42") {
		t.Error("row should contain index 42")
	}
	if !strings.Contains(row, "14:30:45") {
		t.Error("row should contain timestamp")
	}
	if !strings.Contains(row, "Sonnet 4") {
		t.Error("row should contain model name 'Sonnet 4'")
	}
	if !strings.Contains(row, "$0.051200") {
		t.Error("row should contain cost with 6 decimal places")
	}
	if !strings.Contains(row, "1.2K") {
		t.Error("row should contain input tokens")
	}
	// Check trend indicator (first message, should be stable ·)
	if !strings.Contains(row, "·") {
		t.Error("row should contain stable trend indicator · for first message")
	}
}

func TestBreakdownModel_RenderRow_WithAgent(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", true, "", false) // noColor

	msg := models.BreakdownMessage{
		Index:     10,
		AgentID:   "g7h8i9j0k1l2", // real 12-char ID — marker shows the first 7
		Timestamp: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC),
		Model:     "claude-haiku-4-5",
		Usage: models.TokenUsage{
			InputTokens:  100,
			OutputTokens: 50,
		},
		Cost: models.CostBreakdown{
			TotalCost: 0.001,
		},
	}

	row := m.renderRow(msg, false, 0.05, false) // prevCost=0.05, isFirst=false

	// Check agent marker shows the truncated real ID
	if !strings.Contains(row, "[Ag7h8i9j]") {
		t.Errorf("row should contain agent marker [Ag7h8i9j], got %q", row)
	}
	// Check model name is present (agent using Haiku)
	if !strings.Contains(row, "Haiku 4.5") {
		t.Error("row should contain model name 'Haiku 4.5'")
	}

	// A short ID is shown whole
	msg.AgentID = "w1"
	row = m.renderRow(msg, false, 0.05, false)
	if !strings.Contains(row, "[Aw1]") {
		t.Errorf("row should contain agent marker [Aw1], got %q", row)
	}
}

func TestBreakdownModel_RenderRow_ANSICodes(t *testing.T) {
	msg := models.BreakdownMessage{
		Index:     1,
		Timestamp: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC),
		Model:     "claude-sonnet-4",
		Usage:     models.TokenUsage{InputTokens: 100, OutputTokens: 50},
		Cost:      models.CostBreakdown{TotalCost: 0.05},
	}

	// noColor=false takes color code path
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)
	coloredRow := m.renderRow(msg, false, 0.0, true)

	// noColor=true takes plain code path - must not contain ANSI
	m2 := NewBreakdownModel("/test/path", "test-session", true, "", false)
	plainRow := m2.renderRow(msg, false, 0.0, true)

	if strings.Contains(plainRow, "\x1b[") {
		t.Error("noColor output should not contain ANSI escape codes")
	}

	// Both rows should contain essential data
	for _, row := range []string{coloredRow, plainRow} {
		if !strings.Contains(row, "14:30:45") {
			t.Errorf("row should contain timestamp: %q", row)
		}
		if !strings.Contains(row, "Sonnet 4") {
			t.Errorf("row should contain model name: %q", row)
		}
	}

	// In a TTY environment, colored output would contain ANSI codes;
	// in non-TTY test environments, lipgloss may strip them.
	// Verify the colored path was exercised by checking both produce valid output.
	if !strings.Contains(coloredRow, "14:30:45") {
		t.Errorf("colored row should contain timestamp: %q", coloredRow)
	}
}

func TestGetRowTrendIndicator(t *testing.T) {
	tests := []struct {
		name          string
		currentCost   float64
		previousCost  float64
		isFirst       bool
		wantSymbol    string
		wantDirection models.TrendDirection
	}{
		{
			name:          "first message",
			currentCost:   0.05,
			previousCost:  0.0,
			isFirst:       true,
			wantSymbol:    "·",
			wantDirection: models.TrendStable,
		},
		{
			name:          "stable cost (within 5%)",
			currentCost:   0.052,
			previousCost:  0.05,
			isFirst:       false,
			wantSymbol:    "·",
			wantDirection: models.TrendStable,
		},
		{
			name:          "increasing cost (>5%)",
			currentCost:   0.06,
			previousCost:  0.05,
			isFirst:       false,
			wantSymbol:    "↑",
			wantDirection: models.TrendIncreasing,
		},
		{
			name:          "decreasing cost (>5%)",
			currentCost:   0.04,
			previousCost:  0.05,
			isFirst:       false,
			wantSymbol:    "↓",
			wantDirection: models.TrendDecreasing,
		},
		{
			name:          "exactly 5% increase (not significant)",
			currentCost:   0.0525,
			previousCost:  0.05,
			isFirst:       false,
			wantSymbol:    "·",
			wantDirection: models.TrendStable,
		},
		{
			name:          "just over 5% increase",
			currentCost:   0.0526,
			previousCost:  0.05,
			isFirst:       false,
			wantSymbol:    "↑",
			wantDirection: models.TrendIncreasing,
		},
		{
			name:          "previous cost zero",
			currentCost:   0.05,
			previousCost:  0.0,
			isFirst:       false,
			wantSymbol:    "·",
			wantDirection: models.TrendStable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSymbol, gotDirection := getRowTrendIndicator(tt.currentCost, tt.previousCost, tt.isFirst)
			if gotSymbol != tt.wantSymbol {
				t.Errorf("getRowTrendIndicator() symbol = %q, want %q", gotSymbol, tt.wantSymbol)
			}
			if gotDirection != tt.wantDirection {
				t.Errorf("getRowTrendIndicator() direction = %v, want %v", gotDirection, tt.wantDirection)
			}
		})
	}
}

func TestBreakdownModel_DetectNewMessages_FirstLoad(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	// First load has nothing to diff against — flagging every row would flash
	// the whole table as "new"
	first := []models.BreakdownMessage{
		{Index: 1, Timestamp: time.Now()},
		{Index: 2, Timestamp: time.Now()},
		{Index: 3, Timestamp: time.Now()},
	}
	m.detectNewMessages(first)

	if len(m.newMsgKeys) != 0 {
		t.Errorf("first load flagged %d messages as new, want 0", len(m.newMsgKeys))
	}
}

// tickReadyBreakdownModel returns a model with an initialized viewport holding
// sentinel content, so tests can observe whether a tick re-rendered the table.
func tickReadyBreakdownModel() BreakdownModel {
	m := NewBreakdownModel("/test/path", "test-session", true, "", false)
	m.ready = true
	m.viewport = viewport.New(80, 10)
	m.messages = []models.BreakdownMessage{{Index: 1, Timestamp: time.Now()}}
	m.viewport.SetContent("SENTINEL")
	return m
}

func TestBreakdownModel_TickSkipsRenderWhenIdle(t *testing.T) {
	m := tickReadyBreakdownModel()

	updated, _ := m.Update(tickMsg(time.Now()))
	m = updated.(BreakdownModel)

	if !strings.Contains(m.viewport.View(), "SENTINEL") {
		t.Error("idle tick re-rendered the table; expected viewport content untouched")
	}
}

func TestBreakdownModel_TickRendersWhileHighlightsActive(t *testing.T) {
	m := tickReadyBreakdownModel()
	m.newMsgKeys[breakdownMsgKey(m.messages[0])] = time.Now()

	updated, _ := m.Update(tickMsg(time.Now()))
	m = updated.(BreakdownModel)

	if strings.Contains(m.viewport.View(), "SENTINEL") {
		t.Error("tick with active highlights should re-render the table")
	}
}

func TestBreakdownModel_TickRendersFinalFadeFrame(t *testing.T) {
	m := tickReadyBreakdownModel()
	// Expired highlight: this tick prunes it, but must still re-render once
	// so the row doesn't stay highlighted forever
	m.newMsgKeys[breakdownMsgKey(m.messages[0])] = time.Now().Add(-2 * highlightDuration)

	updated, _ := m.Update(tickMsg(time.Now()))
	m = updated.(BreakdownModel)

	if strings.Contains(m.viewport.View(), "SENTINEL") {
		t.Error("the tick that expires the last highlight must re-render once to un-highlight rows")
	}
	if len(m.newMsgKeys) != 0 {
		t.Errorf("expired highlight not pruned: %d entries remain", len(m.newMsgKeys))
	}

	// Subsequent ticks are idle again
	m.viewport.SetContent("SENTINEL")
	updated, _ = m.Update(tickMsg(time.Now()))
	m = updated.(BreakdownModel)
	if !strings.Contains(m.viewport.View(), "SENTINEL") {
		t.Error("tick after the fade frame should not re-render")
	}
}

func TestBreakdownViewportDoesNotWrapRowsOnNarrowTerminal(t *testing.T) {
	// Table rows are ~74 columns; on a narrower terminal the viewport
	// soft-wraps overlong lines into extra rows, shifting the whole table.
	// Content must be clipped before it reaches the viewport.
	m := NewBreakdownModel("/test/path", "test-session", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m = updated.(BreakdownModel)

	msgs := []models.BreakdownMessage{
		{Index: 1, Timestamp: time.Now(), Model: "claude-opus-4-8",
			Usage: models.TokenUsage{InputTokens: 3500, OutputTokens: 717, CacheCreationInputTokens: 6000, CacheReadInputTokens: 15500}},
		{Index: 2, Timestamp: time.Now(), Model: "claude-opus-4-8",
			Usage: models.TokenUsage{InputTokens: 2, OutputTokens: 421, CacheCreationInputTokens: 144800, CacheReadInputTokens: 21500}},
	}
	updated, _ = m.Update(breakdownMsgsMsg{messages: msgs})
	m = updated.(BreakdownModel)

	if got := m.viewport.TotalLineCount(); got != len(msgs) {
		t.Errorf("viewport holds %d lines for %d messages — overlong rows were wrapped, not clipped", got, len(msgs))
	}
}
