package formatter

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ccusage/internal/models"
)

// Helper function to create a sample analysis for testing
func sampleAnalysis() *models.SessionAnalysis {
	return &models.SessionAnalysis{
		SessionID:    "test-session-123",
		ProjectPath:  "/path/to/project",
		StartTime:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		EndTime:      time.Date(2024, 1, 1, 11, 30, 0, 0, time.UTC),
		Duration:     models.Duration(90 * time.Minute),
		MessageCount: 10,
		TotalUsage: models.TokenUsage{
			InputTokens:              5000,
			OutputTokens:             2000,
			CacheCreationInputTokens: 1000,
			CacheReadInputTokens:     3000,
		},
		TotalCost: models.CostBreakdown{
			InputCost:        0.015,
			OutputCost:       0.03,
			CacheWrite5mCost: 0.00375,
			CacheWrite1hCost: 0.0,
			CacheReadCost:    0.0003,
			TotalCost:        0.04905,
			CacheSavings:     0.0087,
		},
		CostByModel: map[string]models.CostBreakdown{
			"claude-sonnet-4-20250514": {
				InputCost:  0.015,
				OutputCost: 0.03,
				TotalCost:  0.04905,
			},
		},
	}
}

// Helper function to create sample session entries
func sampleSessionEntries() []models.SessionEntry {
	return []models.SessionEntry{
		{
			SessionID:    "session-001",
			FullPath:     "/path/to/session-001.jsonl",
			MessageCount: 5,
			Created:      time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			Modified:     time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			SessionID:    "session-002",
			FullPath:     "/path/to/session-002.jsonl",
			MessageCount: 10,
			Created:      time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC),
			Modified:     time.Date(2024, 1, 2, 14, 0, 0, 0, time.UTC),
			AgentCount:   2,
		},
	}
}

// === CSV Formatter Tests ===

func TestFormatSessionCSV(t *testing.T) {
	analysis := sampleAnalysis()

	output, err := FormatSessionCSV(analysis, false)
	if err != nil {
		t.Fatalf("FormatSessionCSV returned error: %v", err)
	}

	// Check header is present
	if !strings.Contains(output, "session_id,input_cost,output_cost") {
		t.Error("CSV output missing header")
	}

	// Check session ID is present
	if !strings.Contains(output, "test-session-123") {
		t.Error("CSV output missing session ID")
	}

	// Check costs are formatted correctly (6 decimal places)
	if !strings.Contains(output, "0.015000") {
		t.Error("CSV output missing properly formatted input cost")
	}
}

func TestFormatSessionCSV_WithMessages(t *testing.T) {
	analysis := sampleAnalysis()
	analysis.Messages = []models.MessageAnalysis{
		{
			Timestamp: time.Date(2024, 1, 1, 10, 5, 0, 0, time.UTC),
			Model:     "claude-sonnet-4-20250514",
			Usage: models.TokenUsage{
				InputTokens:  500,
				OutputTokens: 200,
			},
			Cost: models.CostBreakdown{
				InputCost:  0.0015,
				OutputCost: 0.003,
				TotalCost:  0.0045,
			},
		},
	}

	output, err := FormatSessionCSV(analysis, true)
	if err != nil {
		t.Fatalf("FormatSessionCSV with messages returned error: %v", err)
	}

	// Check messages section is present
	if !strings.Contains(output, "timestamp,model,input_tokens") {
		t.Error("CSV output missing messages header")
	}

	// Check message data is present
	if !strings.Contains(output, "claude-sonnet-4-20250514") {
		t.Error("CSV output missing message model")
	}
}

func TestFormatSessionListCSV(t *testing.T) {
	entries := sampleSessionEntries()

	output, err := FormatSessionListCSV(entries)
	if err != nil {
		t.Fatalf("FormatSessionListCSV returned error: %v", err)
	}

	// Check header
	if !strings.Contains(output, "session_id,full_path,message_count") {
		t.Error("Session list CSV missing header")
	}

	// Check entries
	if !strings.Contains(output, "session-001") {
		t.Error("Session list CSV missing first session")
	}
	if !strings.Contains(output, "session-002") {
		t.Error("Session list CSV missing second session")
	}
}

func TestFormatSessionCSV_EmptyAnalysis(t *testing.T) {
	analysis := &models.SessionAnalysis{
		SessionID: "empty-session",
	}

	output, err := FormatSessionCSV(analysis, false)
	if err != nil {
		t.Fatalf("FormatSessionCSV with empty analysis returned error: %v", err)
	}

	if !strings.Contains(output, "empty-session") {
		t.Error("CSV output missing session ID for empty analysis")
	}
}

// === JSON Formatter Tests ===

func TestFormatSessionJSON(t *testing.T) {
	analysis := sampleAnalysis()

	output, err := FormatSessionJSON(analysis, false)
	if err != nil {
		t.Fatalf("FormatSessionJSON returned error: %v", err)
	}

	// Verify it's valid JSON
	var parsed models.SessionAnalysis
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("FormatSessionJSON output is not valid JSON: %v", err)
	}

	// Verify key fields
	if parsed.SessionID != "test-session-123" {
		t.Errorf("Session ID mismatch: got %q, want %q", parsed.SessionID, "test-session-123")
	}
	if parsed.MessageCount != 10 {
		t.Errorf("Message count mismatch: got %d, want %d", parsed.MessageCount, 10)
	}
}

func TestFormatSessionJSON_Pretty(t *testing.T) {
	analysis := sampleAnalysis()

	output, err := FormatSessionJSON(analysis, true)
	if err != nil {
		t.Fatalf("FormatSessionJSON (pretty) returned error: %v", err)
	}

	// Pretty output should have newlines and indentation
	if !strings.Contains(output, "\n") {
		t.Error("Pretty JSON output missing newlines")
	}
	if !strings.Contains(output, "  ") {
		t.Error("Pretty JSON output missing indentation")
	}
}

func TestFormatSessionListJSON(t *testing.T) {
	entries := sampleSessionEntries()

	output, err := FormatSessionListJSON(entries, true)
	if err != nil {
		t.Fatalf("FormatSessionListJSON returned error: %v", err)
	}

	// Verify it's valid JSON array
	var parsed []models.SessionEntry
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("FormatSessionListJSON output is not valid JSON: %v", err)
	}

	if len(parsed) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(parsed))
	}
}

// === Table Formatter Tests ===

func TestFormatSessionTable_Plain(t *testing.T) {
	analysis := sampleAnalysis()

	output := FormatSessionTable(analysis, true) // noColor = true

	// Check basic structure
	if !strings.Contains(output, "Session: test-session-123") {
		t.Error("Table output missing session header")
	}
	if !strings.Contains(output, "TOTAL") {
		t.Error("Table output missing TOTAL row")
	}
	if !strings.Contains(output, "Messages: 10") {
		t.Error("Table output missing message count")
	}
}

func TestFormatSessionTable_WithColor(t *testing.T) {
	analysis := sampleAnalysis()

	output := FormatSessionTable(analysis, false) // noColor = false

	// Colored output should still contain the data
	if !strings.Contains(output, "test-session-123") {
		t.Error("Colored table output missing session ID")
	}
	if !strings.Contains(output, "TOTAL") {
		t.Error("Colored table output missing TOTAL row")
	}
}

func TestFormatSessionTable_WithCacheSavings(t *testing.T) {
	analysis := sampleAnalysis()

	output := FormatSessionTable(analysis, true)

	if !strings.Contains(output, "Savings") {
		t.Error("Table output missing savings row")
	}
}

func TestFormatSessionTable_WithAgents(t *testing.T) {
	analysis := sampleAnalysis()
	analysis.HasAgents = true
	analysis.AgentCount = 2
	analysis.ParentCost = models.CostBreakdown{TotalCost: 0.03}
	analysis.AgentsCost = models.CostBreakdown{TotalCost: 0.01905}
	analysis.Agents = []models.AgentAnalysis{
		{
			AgentID:      "agent-1",
			MessageCount: 3,
			TotalCost:    models.CostBreakdown{TotalCost: 0.01},
		},
		{
			AgentID:      "agent-2",
			MessageCount: 2,
			TotalCost:    models.CostBreakdown{TotalCost: 0.00905},
		},
	}

	output := FormatSessionTable(analysis, true)

	if !strings.Contains(output, "AGENT SUB-SESSIONS") {
		t.Error("Table output missing agent breakdown section")
	}
	if !strings.Contains(output, "Parent session") {
		t.Error("Table output missing parent session cost")
	}
	if !strings.Contains(output, "agent-1") {
		t.Error("Table output missing first agent")
	}
}

func TestFormatSessionListTable(t *testing.T) {
	entries := sampleSessionEntries()

	output := FormatSessionListTable(entries, true)

	// Check header
	if !strings.Contains(output, "Session ID") {
		t.Error("Session list table missing header")
	}
	if !strings.Contains(output, "Messages") {
		t.Error("Session list table missing Messages column")
	}

	// Check entries
	if !strings.Contains(output, "session-001") {
		t.Error("Session list table missing first session")
	}
	if !strings.Contains(output, "session-002") {
		t.Error("Session list table missing second session")
	}
}

// === Helper Function Tests ===

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{1000000, "1.00M"},
		{2500000, "2.50M"},
	}

	for _, tc := range tests {
		result := formatNumber(tc.input)
		if result != tc.expected {
			t.Errorf("formatNumber(%d) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m 30s"},
		{65 * time.Minute, "1h 5m"},
		{2*time.Hour + 30*time.Minute, "2h 30m"},
	}

	for _, tc := range tests {
		result := formatDuration(tc.input)
		if result != tc.expected {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestTruncateID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"short-id", "short-id"},
		{"exactly-40-characters-long-id-goes-here!", "exactly-40-characters-long-id-goes-here!"},
		{"this-is-a-very-long-session-id-that-exceeds-40-chars", "this-is-a-very-long-session-id-that-e..."},
	}

	for _, tc := range tests {
		result := truncateID(tc.input)
		if result != tc.expected {
			t.Errorf("truncateID(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		input    float64
		expected string
	}{
		{0.0, "$0.000000"},
		{0.123456, "$0.123456"},
		{1.5, "$1.500000"},
		{123.456789, "$123.456789"},
	}

	for _, tc := range tests {
		result := formatCost(tc.input)
		if result != tc.expected {
			t.Errorf("formatCost(%f) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}
