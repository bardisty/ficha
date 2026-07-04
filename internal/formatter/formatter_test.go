package formatter

import (
	"encoding/csv"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// almostEqual checks if two float64 values are within a given tolerance
func almostEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}

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

	records, err := csv.NewReader(strings.NewReader(output)).ReadAll()
	if err != nil {
		t.Fatalf("Failed to parse CSV output: %v", err)
	}

	// Verify row count: 1 header + 1 data row
	if len(records) != 2 {
		t.Fatalf("Expected 2 rows (header + data), got %d", len(records))
	}

	// Verify header columns
	expectedHeader := []string{
		"session_id", "input_cost", "output_cost", "cache_write_5m_cost",
		"cache_write_1h_cost", "cache_read_cost", "total_cost", "cache_savings",
		"message_count", "duration_seconds", "agent_count", "agent_message_count",
		"parent_cost", "agents_cost",
	}
	header := records[0]
	if len(header) != len(expectedHeader) {
		t.Fatalf("Header column count: got %d, want %d", len(header), len(expectedHeader))
	}
	for i, col := range expectedHeader {
		if header[i] != col {
			t.Errorf("Header column %d: got %q, want %q", i, header[i], col)
		}
	}

	// Verify data row values at specific column positions
	data := records[1]
	if data[0] != "test-session-123" {
		t.Errorf("session_id (col 0): got %q, want %q", data[0], "test-session-123")
	}
	if data[1] != "0.015000" {
		t.Errorf("input_cost (col 1): got %q, want %q", data[1], "0.015000")
	}
	if data[2] != "0.030000" {
		t.Errorf("output_cost (col 2): got %q, want %q", data[2], "0.030000")
	}
	if data[3] != "0.003750" {
		t.Errorf("cache_write_5m_cost (col 3): got %q, want %q", data[3], "0.003750")
	}
	if data[4] != "0.000000" {
		t.Errorf("cache_write_1h_cost (col 4): got %q, want %q", data[4], "0.000000")
	}
	if data[5] != "0.000300" {
		t.Errorf("cache_read_cost (col 5): got %q, want %q", data[5], "0.000300")
	}
	if data[6] != "0.049050" {
		t.Errorf("total_cost (col 6): got %q, want %q", data[6], "0.049050")
	}
	if data[7] != "0.008700" {
		t.Errorf("cache_savings (col 7): got %q, want %q", data[7], "0.008700")
	}
	if data[8] != "10" {
		t.Errorf("message_count (col 8): got %q, want %q", data[8], "10")
	}
	if data[9] != "5400" {
		t.Errorf("duration_seconds (col 9): got %q, want %q", data[9], "5400")
	}
	if data[10] != "0" {
		t.Errorf("agent_count (col 10): got %q, want %q", data[10], "0")
	}
	if data[11] != "0" {
		t.Errorf("agent_message_count (col 11): got %q, want %q", data[11], "0")
	}
	if data[12] != "0.000000" {
		t.Errorf("parent_cost (col 12): got %q, want %q", data[12], "0.000000")
	}
	if data[13] != "0.000000" {
		t.Errorf("agents_cost (col 13): got %q, want %q", data[13], "0.000000")
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

	// includeMessages switches granularity: the output is a single per-message
	// CSV table (no stacked session row), so single-table parsers stay happy.
	output, err := FormatSessionCSV(analysis, true)
	if err != nil {
		t.Fatalf("FormatSessionCSV with messages returned error: %v", err)
	}

	// The entire output is one valid CSV table — parse it whole.
	msgRecords, err := csv.NewReader(strings.NewReader(output)).ReadAll()
	if err != nil {
		t.Fatalf("Failed to parse messages CSV: %v", err)
	}

	// Verify messages header
	expectedMsgHeader := []string{
		"timestamp", "model", "input_tokens", "output_tokens",
		"cache_write_tokens", "cache_read_tokens",
		"input_cost", "output_cost", "cache_write_5m_cost",
		"cache_write_1h_cost", "cache_read_cost", "total_cost",
	}
	if len(msgRecords) < 2 {
		t.Fatalf("Messages CSV: expected at least 2 rows (header + data), got %d", len(msgRecords))
	}
	msgHeader := msgRecords[0]
	if len(msgHeader) != len(expectedMsgHeader) {
		t.Fatalf("Messages header column count: got %d, want %d", len(msgHeader), len(expectedMsgHeader))
	}
	for i, col := range expectedMsgHeader {
		if msgHeader[i] != col {
			t.Errorf("Messages header column %d: got %q, want %q", i, msgHeader[i], col)
		}
	}

	// Verify 1 message data row
	if len(msgRecords) != 2 {
		t.Fatalf("Messages section: expected 2 rows (header + 1 data), got %d", len(msgRecords))
	}

	msgData := msgRecords[1]
	if msgData[0] != "2024-01-01T10:05:00Z" {
		t.Errorf("timestamp (col 0): got %q, want %q", msgData[0], "2024-01-01T10:05:00Z")
	}
	if msgData[1] != "claude-sonnet-4-20250514" {
		t.Errorf("model (col 1): got %q, want %q", msgData[1], "claude-sonnet-4-20250514")
	}
	if msgData[2] != "500" {
		t.Errorf("input_tokens (col 2): got %q, want %q", msgData[2], "500")
	}
	if msgData[3] != "200" {
		t.Errorf("output_tokens (col 3): got %q, want %q", msgData[3], "200")
	}
	if msgData[4] != "0" {
		t.Errorf("cache_write_tokens (col 4): got %q, want %q", msgData[4], "0")
	}
	if msgData[5] != "0" {
		t.Errorf("cache_read_tokens (col 5): got %q, want %q", msgData[5], "0")
	}
	if msgData[6] != "0.001500" {
		t.Errorf("input_cost (col 6): got %q, want %q", msgData[6], "0.001500")
	}
	if msgData[7] != "0.003000" {
		t.Errorf("output_cost (col 7): got %q, want %q", msgData[7], "0.003000")
	}
	if msgData[8] != "0.000000" {
		t.Errorf("cache_write_5m_cost (col 8): got %q, want %q", msgData[8], "0.000000")
	}
	if msgData[9] != "0.000000" {
		t.Errorf("cache_write_1h_cost (col 9): got %q, want %q", msgData[9], "0.000000")
	}
	if msgData[10] != "0.000000" {
		t.Errorf("cache_read_cost (col 10): got %q, want %q", msgData[10], "0.000000")
	}
	if msgData[11] != "0.004500" {
		t.Errorf("total_cost (col 11): got %q, want %q", msgData[11], "0.004500")
	}
}

func TestFormatSessionListCSV(t *testing.T) {
	entries := sampleSessionEntries()

	output, err := FormatSessionListCSV(entries)
	if err != nil {
		t.Fatalf("FormatSessionListCSV returned error: %v", err)
	}

	records, err := csv.NewReader(strings.NewReader(output)).ReadAll()
	if err != nil {
		t.Fatalf("Failed to parse CSV output: %v", err)
	}

	// Verify row count: 1 header + 2 data rows
	if len(records) != 3 {
		t.Fatalf("Expected 3 rows (header + 2 data), got %d", len(records))
	}

	// Verify header columns
	expectedHeader := []string{
		"session_id", "full_path", "message_count", "created",
		"modified", "agent_count", "agent_message_count",
	}
	header := records[0]
	if len(header) != len(expectedHeader) {
		t.Fatalf("Header column count: got %d, want %d", len(header), len(expectedHeader))
	}
	for i, col := range expectedHeader {
		if header[i] != col {
			t.Errorf("Header column %d: got %q, want %q", i, header[i], col)
		}
	}

	// Verify first data row
	row1 := records[1]
	if row1[0] != "session-001" {
		t.Errorf("Row 1 session_id (col 0): got %q, want %q", row1[0], "session-001")
	}
	if row1[1] != "/path/to/session-001.jsonl" {
		t.Errorf("Row 1 full_path (col 1): got %q, want %q", row1[1], "/path/to/session-001.jsonl")
	}
	if row1[2] != "5" {
		t.Errorf("Row 1 message_count (col 2): got %q, want %q", row1[2], "5")
	}
	if row1[3] != "2024-01-01T10:00:00Z" {
		t.Errorf("Row 1 created (col 3): got %q, want %q", row1[3], "2024-01-01T10:00:00Z")
	}
	if row1[4] != "2024-01-01T12:00:00Z" {
		t.Errorf("Row 1 modified (col 4): got %q, want %q", row1[4], "2024-01-01T12:00:00Z")
	}
	if row1[5] != "0" {
		t.Errorf("Row 1 agent_count (col 5): got %q, want %q", row1[5], "0")
	}
	if row1[6] != "0" {
		t.Errorf("Row 1 agent_message_count (col 6): got %q, want %q", row1[6], "0")
	}

	// Verify second data row
	row2 := records[2]
	if row2[0] != "session-002" {
		t.Errorf("Row 2 session_id (col 0): got %q, want %q", row2[0], "session-002")
	}
	if row2[1] != "/path/to/session-002.jsonl" {
		t.Errorf("Row 2 full_path (col 1): got %q, want %q", row2[1], "/path/to/session-002.jsonl")
	}
	if row2[2] != "10" {
		t.Errorf("Row 2 message_count (col 2): got %q, want %q", row2[2], "10")
	}
	if row2[3] != "2024-01-02T10:00:00Z" {
		t.Errorf("Row 2 created (col 3): got %q, want %q", row2[3], "2024-01-02T10:00:00Z")
	}
	if row2[4] != "2024-01-02T14:00:00Z" {
		t.Errorf("Row 2 modified (col 4): got %q, want %q", row2[4], "2024-01-02T14:00:00Z")
	}
	if row2[5] != "2" {
		t.Errorf("Row 2 agent_count (col 5): got %q, want %q", row2[5], "2")
	}
	if row2[6] != "0" {
		t.Errorf("Row 2 agent_message_count (col 6): got %q, want %q", row2[6], "0")
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

	records, err := csv.NewReader(strings.NewReader(output)).ReadAll()
	if err != nil {
		t.Fatalf("Failed to parse CSV output: %v", err)
	}

	// Verify row count: 1 header + 1 data row
	if len(records) != 2 {
		t.Fatalf("Expected 2 rows (header + data), got %d", len(records))
	}

	// Verify session_id
	data := records[1]
	if data[0] != "empty-session" {
		t.Errorf("session_id (col 0): got %q, want %q", data[0], "empty-session")
	}

	// Verify all cost fields are zero
	zeroCols := map[int]string{
		1: "input_cost", 2: "output_cost", 3: "cache_write_5m_cost",
		4: "cache_write_1h_cost", 5: "cache_read_cost", 6: "total_cost",
		7: "cache_savings", 12: "parent_cost", 13: "agents_cost",
	}
	for col, name := range zeroCols {
		if data[col] != "0.000000" {
			t.Errorf("%s (col %d): got %q, want %q", name, col, data[col], "0.000000")
		}
	}

	// Verify count fields are zero
	intZeroCols := map[int]string{
		8: "message_count", 10: "agent_count", 11: "agent_message_count",
	}
	for col, name := range intZeroCols {
		if data[col] != "0" {
			t.Errorf("%s (col %d): got %q, want %q", name, col, data[col], "0")
		}
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

	// Verify cost-related fields
	if !almostEqual(parsed.TotalCost.InputCost, 0.015, 1e-9) {
		t.Errorf("TotalCost.InputCost: got %f, want 0.015", parsed.TotalCost.InputCost)
	}
	if !almostEqual(parsed.TotalCost.OutputCost, 0.03, 1e-9) {
		t.Errorf("TotalCost.OutputCost: got %f, want 0.03", parsed.TotalCost.OutputCost)
	}
	if !almostEqual(parsed.TotalCost.TotalCost, 0.04905, 1e-9) {
		t.Errorf("TotalCost.TotalCost: got %f, want 0.04905", parsed.TotalCost.TotalCost)
	}
	if !almostEqual(parsed.TotalCost.CacheSavings, 0.0087, 1e-9) {
		t.Errorf("TotalCost.CacheSavings: got %f, want 0.0087", parsed.TotalCost.CacheSavings)
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

// === ANSI Color Tests ===

func TestFormatSessionTable_ANSICodes(t *testing.T) {
	analysis := sampleAnalysis()

	// Force lipgloss default renderer to use ANSI256 so it emits escape codes in non-TTY
	r := lipgloss.DefaultRenderer()
	origProfile := r.ColorProfile()
	r.SetColorProfile(termenv.ANSI256)
	defer r.SetColorProfile(origProfile)

	// noColor=false → output contains ANSI escape codes
	colored := FormatSessionTable(analysis, false)
	if !strings.Contains(colored, "\x1b[") {
		t.Error("colored output should contain ANSI escape codes")
	}

	// noColor=true → no ANSI
	plain := FormatSessionTable(analysis, true)
	if strings.Contains(plain, "\x1b[") {
		t.Error("plain output should not contain ANSI escape codes")
	}
}

// === Global Formatter Tests ===

func sampleGlobalAnalysis() *models.GlobalAnalysis {
	return &models.GlobalAnalysis{
		Projects: []models.ProjectAnalysis{
			{
				ProjectInfo:  models.ProjectInfo{DisplayName: "project-alpha"},
				TotalCost:    models.CostBreakdown{TotalCost: 5.0, InputCost: 2.0, OutputCost: 3.0},
				SessionCount: 3,
				MessageCount: 30,
				LastActive:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			},
			{
				ProjectInfo:  models.ProjectInfo{DisplayName: "project-beta"},
				TotalCost:    models.CostBreakdown{TotalCost: 2.5, InputCost: 1.0, OutputCost: 1.5},
				SessionCount: 2,
				MessageCount: 15,
				LastActive:   time.Date(2024, 1, 14, 10, 0, 0, 0, time.UTC),
			},
		},
		TotalCost:    models.CostBreakdown{TotalCost: 7.5, InputCost: 3.0, OutputCost: 4.5},
		TotalUsage:   models.TokenUsage{InputTokens: 50000, OutputTokens: 20000},
		CostByModel:  map[string]models.CostBreakdown{"claude-sonnet-4-20250514": {TotalCost: 7.5}},
		ProjectCount: 2,
		SessionCount: 5,
		MessageCount: 45,
	}
}

func TestFormatGlobalCSV(t *testing.T) {
	analysis := sampleGlobalAnalysis()
	output, err := FormatGlobalCSV(analysis)
	if err != nil {
		t.Fatalf("FormatGlobalCSV returned error: %v", err)
	}

	records, err := csv.NewReader(strings.NewReader(output)).ReadAll()
	if err != nil {
		t.Fatalf("Failed to parse CSV: %v", err)
	}

	// Header + 2 project rows
	if len(records) != 3 {
		t.Fatalf("expected 3 rows (header + 2 data), got %d", len(records))
	}

	// Verify 11-column header
	if len(records[0]) != 11 {
		t.Fatalf("expected 11 columns, got %d", len(records[0]))
	}
	if records[0][0] != "project" {
		t.Errorf("first header column: got %q, want %q", records[0][0], "project")
	}
	if records[0][10] != "last_active" {
		t.Errorf("last header column: got %q, want %q", records[0][10], "last_active")
	}

	// Verify first data row
	if records[1][0] != "project-alpha" {
		t.Errorf("first project name: got %q, want %q", records[1][0], "project-alpha")
	}
	if records[1][1] != "3" {
		t.Errorf("first project sessions: got %q, want %q", records[1][1], "3")
	}
}

func TestFormatGlobalJSON(t *testing.T) {
	analysis := sampleGlobalAnalysis()
	output, err := FormatGlobalJSON(analysis, true)
	if err != nil {
		t.Fatalf("FormatGlobalJSON returned error: %v", err)
	}

	var parsed models.GlobalAnalysis
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if parsed.ProjectCount != 2 {
		t.Errorf("ProjectCount: got %d, want %d", parsed.ProjectCount, 2)
	}
	if parsed.SessionCount != 5 {
		t.Errorf("SessionCount: got %d, want %d", parsed.SessionCount, 5)
	}
	if !almostEqual(parsed.TotalCost.TotalCost, 7.5, 1e-9) {
		t.Errorf("TotalCost: got %f, want 7.5", parsed.TotalCost.TotalCost)
	}
}

func TestFormatGlobalTable(t *testing.T) {
	analysis := sampleGlobalAnalysis()
	output := FormatGlobalTable(analysis, true, 10, false)

	// Verify key strings present in plain output
	if !strings.Contains(output, "project-alpha") {
		t.Error("output should contain 'project-alpha'")
	}
	if !strings.Contains(output, "project-beta") {
		t.Error("output should contain 'project-beta'")
	}
	if !strings.Contains(output, "TOTAL") {
		t.Error("output should contain 'TOTAL'")
	}
}
