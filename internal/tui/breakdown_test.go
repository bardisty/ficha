package tui

import (
	"testing"
	"time"

	"github.com/bardisty/ccusage/internal/models"
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

func TestFormatCompactNumber(t *testing.T) {
	tests := []struct {
		name     string
		n        int
		expected string
	}{
		{"zero", 0, "0"},
		{"small number", 42, "42"},
		{"hundreds", 999, "999"},
		{"one thousand", 1000, "1.0K"},
		{"thousands", 1234, "1.2K"},
		{"large thousands", 89300, "89.3K"},
		{"one million", 1000000, "1.0M"},
		{"millions", 1500000, "1.5M"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatCompactNumber(tt.n)
			if result != tt.expected {
				t.Errorf("formatCompactNumber(%d) = %q, want %q", tt.n, result, tt.expected)
			}
		})
	}
}

func TestBreakdownModel_DetectNewMessages(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	// Initial load with 3 messages
	initialMessages := []models.BreakdownMessage{
		{Index: 1, Timestamp: time.Now()},
		{Index: 2, Timestamp: time.Now()},
		{Index: 3, Timestamp: time.Now()},
	}
	m.messages = initialMessages

	// Simulate receiving 5 messages (2 new)
	newMessages := []models.BreakdownMessage{
		{Index: 1, Timestamp: time.Now()},
		{Index: 2, Timestamp: time.Now()},
		{Index: 3, Timestamp: time.Now()},
		{Index: 4, Timestamp: time.Now()},
		{Index: 5, Timestamp: time.Now()},
	}

	m.detectNewMessages(newMessages)

	// Should have marked indices 4 and 5 as new
	if _, exists := m.newMsgIndices[4]; !exists {
		t.Error("expected index 4 to be marked as new")
	}
	if _, exists := m.newMsgIndices[5]; !exists {
		t.Error("expected index 5 to be marked as new")
	}
	if _, exists := m.newMsgIndices[1]; exists {
		t.Error("index 1 should not be marked as new")
	}
	if _, exists := m.newMsgIndices[3]; exists {
		t.Error("index 3 should not be marked as new")
	}
}

func TestBreakdownModel_IsNewMessage(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	// Mark a message as new
	m.newMsgIndices[5] = time.Now()

	// Should be highlighted
	if !m.isNewMessage(5) {
		t.Error("expected message 5 to be highlighted")
	}

	// Unknown message should not be highlighted
	if m.isNewMessage(10) {
		t.Error("expected message 10 to not be highlighted")
	}
}

func TestBreakdownModel_IsNewMessage_NoColor(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", true, "", false) // noColor = true

	// Mark a message as new
	m.newMsgIndices[5] = time.Now()

	// Should NOT be highlighted when noColor is true
	if m.isNewMessage(5) {
		t.Error("expected no highlight in noColor mode")
	}
}

func TestBreakdownModel_CleanupExpiredHighlights(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	// Add an expired highlight (older than highlightDuration)
	m.newMsgIndices[1] = time.Now().Add(-3 * time.Second) // highlightDuration is 2s

	// Add a fresh highlight
	m.newMsgIndices[2] = time.Now()

	m.cleanupExpiredHighlights()

	// Expired one should be removed
	if _, exists := m.newMsgIndices[1]; exists {
		t.Error("expected expired highlight to be cleaned up")
	}

	// Fresh one should remain
	if _, exists := m.newMsgIndices[2]; !exists {
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
			InputTokens:         1234,
			OutputTokens:        56,
			CacheReadInputTokens: 89300,
		},
		Cost: models.CostBreakdown{
			TotalCost: 0.0512,
		},
	}

	row := m.renderRow(msg, false, 0.0, true) // prevCost=0, isFirst=true

	// Check that the row contains expected values
	if !containsSubstring(row, "42") {
		t.Error("row should contain index 42")
	}
	if !containsSubstring(row, "14:30:45") {
		t.Error("row should contain timestamp")
	}
	if !containsSubstring(row, "Sonnet 4") {
		t.Error("row should contain model name 'Sonnet 4'")
	}
	if !containsSubstring(row, "$0.051200") {
		t.Error("row should contain cost with 6 decimal places")
	}
	if !containsSubstring(row, "1.2K") {
		t.Error("row should contain input tokens")
	}
	// Check trend indicator (first message, should be stable ·)
	if !containsSubstring(row, "·") {
		t.Error("row should contain stable trend indicator · for first message")
	}
}

func TestBreakdownModel_RenderRow_WithAgent(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", true, "", false) // noColor

	msg := models.BreakdownMessage{
		Index:     10,
		AgentID:   "1",
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

	// Check agent marker is present
	if !containsSubstring(row, "[A1]") {
		t.Error("row should contain agent marker [A1]")
	}
	// Check model name is present (agent using Haiku)
	if !containsSubstring(row, "Haiku 4.5") {
		t.Error("row should contain model name 'Haiku 4.5'")
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
			name:          "stable cost (within 10%)",
			currentCost:   0.055,
			previousCost:  0.05,
			isFirst:       false,
			wantSymbol:    "·",
			wantDirection: models.TrendStable,
		},
		{
			name:          "increasing cost (>10%)",
			currentCost:   0.06,
			previousCost:  0.05,
			isFirst:       false,
			wantSymbol:    "↑",
			wantDirection: models.TrendIncreasing,
		},
		{
			name:          "decreasing cost (>10%)",
			currentCost:   0.04,
			previousCost:  0.05,
			isFirst:       false,
			wantSymbol:    "↓",
			wantDirection: models.TrendDecreasing,
		},
		{
			name:          "exactly 10% increase (not significant)",
			currentCost:   0.055,
			previousCost:  0.05,
			isFirst:       false,
			wantSymbol:    "·",
			wantDirection: models.TrendStable,
		},
		{
			name:          "just over 10% increase",
			currentCost:   0.0551,
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

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && containsSubstringHelper(s, substr)))
}

func containsSubstringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
