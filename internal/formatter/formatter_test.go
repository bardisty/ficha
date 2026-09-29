package formatter

import (
	"encoding/csv"
	"encoding/json"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"strconv"
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
		SessionCount: 1,
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
		"session_id", "project_path", "session_file", "input_cost", "output_cost", "cache_write_5m_cost",
		"cache_write_1h_cost", "cache_read_cost", "total_cost", "cache_savings",
		"message_count", "duration_seconds", "agent_count", "agent_message_count",
		"parent_cost", "agents_cost", "workflow_count",
		"skipped_sessions", "skipped_agents", "skipped_lines",
		"estimated_cost_messages", "session_count", "unpriced_models",
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
	if data[1] != "/path/to/project" {
		t.Errorf("project_path (col 1): got %q, want %q", data[1], "/path/to/project")
	}
	if data[2] != "" {
		t.Errorf("session_file (col 2): got %q, want empty", data[2])
	}
	if data[3] != "0.015000" {
		t.Errorf("input_cost (col 3): got %q, want %q", data[3], "0.015000")
	}
	if data[4] != "0.030000" {
		t.Errorf("output_cost (col 4): got %q, want %q", data[4], "0.030000")
	}
	if data[5] != "0.003750" {
		t.Errorf("cache_write_5m_cost (col 5): got %q, want %q", data[5], "0.003750")
	}
	if data[6] != "0.000000" {
		t.Errorf("cache_write_1h_cost (col 6): got %q, want %q", data[6], "0.000000")
	}
	if data[7] != "0.000300" {
		t.Errorf("cache_read_cost (col 7): got %q, want %q", data[7], "0.000300")
	}
	if data[8] != "0.049050" {
		t.Errorf("total_cost (col 8): got %q, want %q", data[8], "0.049050")
	}
	if data[9] != "0.008700" {
		t.Errorf("cache_savings (col 9): got %q, want %q", data[9], "0.008700")
	}
	if data[10] != "10" {
		t.Errorf("message_count (col 10): got %q, want %q", data[10], "10")
	}
	if data[11] != "5400" {
		t.Errorf("duration_seconds (col 11): got %q, want %q", data[11], "5400")
	}
	if data[12] != "0" {
		t.Errorf("agent_count (col 12): got %q, want %q", data[12], "0")
	}
	if data[13] != "0" {
		t.Errorf("agent_message_count (col 13): got %q, want %q", data[13], "0")
	}
	if data[14] != "0.000000" {
		t.Errorf("parent_cost (col 14): got %q, want %q", data[14], "0.000000")
	}
	if data[15] != "0.000000" {
		t.Errorf("agents_cost (col 15): got %q, want %q", data[15], "0.000000")
	}
	if data[21] != "1" {
		t.Errorf("session_count (col 21): got %q, want %q", data[21], "1")
	}
}

func TestFormatSessionCSV_WithMessages(t *testing.T) {
	analysis := sampleAnalysis()
	analysis.SkippedAgents = 2
	analysis.SkippedLines = 3
	analysis.EstimatedCostMessages = 1
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
		"agent_id", "timestamp", "model", "input_tokens", "output_tokens",
		"cache_write_tokens", "cache_read_tokens",
		"input_cost", "output_cost", "cache_write_5m_cost",
		"cache_write_1h_cost", "cache_read_cost", "total_cost",
		"skipped_agents", "skipped_lines", "estimated_cost_messages",
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
	for name, want := range map[string]string{
		"agent_id":            "", // parent row
		"timestamp":           "2024-01-01T10:05:00Z",
		"model":               "claude-sonnet-4-20250514",
		"input_tokens":        "500",
		"output_tokens":       "200",
		"cache_write_tokens":  "0",
		"cache_read_tokens":   "0",
		"input_cost":          "0.001500",
		"output_cost":         "0.003000",
		"cache_write_5m_cost": "0.000000",
		"cache_write_1h_cost": "0.000000",
		"cache_read_cost":     "0.000000",
		"total_cost":          "0.004500",
		// Session-level accounting repeated on every message row.
		"skipped_agents":          "2",
		"skipped_lines":           "3",
		"estimated_cost_messages": "1",
	} {
		col := slices.Index(msgHeader, name)
		if col == -1 {
			t.Fatalf("column %q missing from messages header", name)
		}
		if msgData[col] != want {
			t.Errorf("%s: got %q, want %q", name, msgData[col], want)
		}
	}
}

// TestFormatSessionCSV_AgentMessageRows pins the agent discriminator: agent rows
// carry their agent_id, parent rows leave it empty, and every row is part of the
// same table so total_cost sums across all of them.
func TestFormatSessionCSV_AgentMessageRows(t *testing.T) {
	analysis := sampleAnalysis()
	analysis.Messages = []models.MessageAnalysis{
		{Timestamp: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC), Model: "claude-opus-4-8", Cost: models.CostBreakdown{TotalCost: 0.01}},
		{AgentID: "g1", Timestamp: time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC), Model: "claude-sonnet-5", Cost: models.CostBreakdown{TotalCost: 0.02}},
		{AgentID: "w1", Timestamp: time.Date(2024, 1, 1, 10, 2, 0, 0, time.UTC), Model: "claude-sonnet-5", Cost: models.CostBreakdown{TotalCost: 0.03}},
	}

	output, err := FormatSessionCSV(analysis, true)
	if err != nil {
		t.Fatalf("FormatSessionCSV with messages returned error: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(output)).ReadAll()
	if err != nil {
		t.Fatalf("Failed to parse messages CSV: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("rows: got %d, want 4 (header + 3 messages)", len(records))
	}

	agentCol := slices.Index(records[0], "agent_id")
	costCol := slices.Index(records[0], "total_cost")
	if agentCol == -1 || costCol == -1 {
		t.Fatalf("agent_id/total_cost missing from header %v", records[0])
	}

	wantAgents := []string{"", "g1", "w1"}
	var sum float64
	for i, want := range wantAgents {
		row := records[i+1]
		if row[agentCol] != want {
			t.Errorf("row %d agent_id: got %q, want %q", i, row[agentCol], want)
		}
		cost, err := strconv.ParseFloat(row[costCol], 64)
		if err != nil {
			t.Fatalf("row %d total_cost %q: %v", i, row[costCol], err)
		}
		sum += cost
	}
	if !almostEqual(sum, 0.06, 1e-9) {
		t.Errorf("sum of row total_cost: got %f, want 0.06", sum)
	}
}

// sampleListResults is list's input: session-001 analyzed, session-002
// unreadable.
func sampleListResults() ([]models.SessionResult, string) {
	entries := sampleSessionEntries()
	entries[1].SkippedSessions = 1
	analysis := &models.SessionAnalysis{
		SessionID:         "session-001",
		Title:             "Fix the parser",
		StartTime:         time.Date(2024, 1, 1, 10, 30, 0, 0, time.UTC),
		Duration:          models.Duration(90 * time.Minute),
		TotalCost:         models.CostBreakdown{InputCost: 1, TotalCost: 1.5},
		ParentCostByModel: map[string]models.CostBreakdown{"claude-opus-4-8": {TotalCost: 1.5}},
	}
	return []models.SessionResult{{Entry: entries[0], Analysis: analysis}, {Entry: entries[1]}},
		"/home/u/work/proj"
}

func TestFormatSessionListCSV(t *testing.T) {
	results, originalPath := sampleListResults()
	// The transcript's own directory, spelled the way this OS spells it.
	projectPath := filepath.Dir(results[0].Entry.FullPath)

	output, err := FormatSessionListCSV(results, originalPath)
	if err != nil {
		t.Fatalf("FormatSessionListCSV returned error: %v", err)
	}

	records, err := csv.NewReader(strings.NewReader(output)).ReadAll()
	if err != nil {
		t.Fatalf("Failed to parse CSV output: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("Expected 3 rows (header + 2 data), got %d", len(records))
	}

	expectedHeader := []string{
		"session_id", "full_path", "message_count",
		"modified", "agent_count", "agent_message_count",
		"skipped_sessions", "skipped_agents", "skipped_lines",
		"project_path", "original_path", "start_time", "duration_seconds",
		"model", "total_cost", "title",
	}
	if !slices.Equal(records[0], expectedHeader) {
		t.Fatalf("header:\n got %q\nwant %q", records[0], expectedHeader)
	}
	row := func(r []string) map[string]string {
		m := map[string]string{}
		for i, col := range expectedHeader {
			m[col] = r[i]
		}
		return m
	}

	want1 := map[string]string{
		"session_id": "session-001", "full_path": "/path/to/session-001.jsonl", "message_count": "5",
		"modified": "2024-01-01T12:00:00Z", "agent_count": "0", "agent_message_count": "0",
		"skipped_sessions": "0", "skipped_agents": "0", "skipped_lines": "0",
		"project_path": projectPath, "original_path": "/home/u/work/proj",
		"start_time": "2024-01-01T10:30:00Z", "duration_seconds": "5400",
		"model": "claude-opus-4-8", "total_cost": "1.500000", "title": "Fix the parser",
	}
	if got := row(records[1]); !maps.Equal(got, want1) {
		t.Errorf("row 1:\n got %v\nwant %v", got, want1)
	}

	// The unreadable session keeps its scan fields and leaves the analysis
	// columns empty.
	want2 := map[string]string{
		"session_id": "session-002", "full_path": "/path/to/session-002.jsonl", "message_count": "10",
		"modified": "2024-01-02T14:00:00Z", "agent_count": "2", "agent_message_count": "0",
		"skipped_sessions": "1", "skipped_agents": "0", "skipped_lines": "0",
		"project_path": projectPath, "original_path": "/home/u/work/proj",
		"start_time": "", "duration_seconds": "", "model": "", "total_cost": "", "title": "",
	}
	if got := row(records[2]); !maps.Equal(got, want2) {
		t.Errorf("row 2:\n got %v\nwant %v", got, want2)
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

	// project_path and session_file are empty on an empty analysis
	for _, c := range []struct {
		col  int
		name string
	}{{1, "project_path"}, {2, "session_file"}} {
		if data[c.col] != "" {
			t.Errorf("%s (col %d): got %q, want empty", c.name, c.col, data[c.col])
		}
	}

	// Verify all cost fields are zero
	zeroCols := map[int]string{
		3: "input_cost", 4: "output_cost", 5: "cache_write_5m_cost",
		6: "cache_write_1h_cost", 7: "cache_read_cost", 8: "total_cost",
		9: "cache_savings", 14: "parent_cost", 15: "agents_cost",
	}
	for col, name := range zeroCols {
		if data[col] != "0.000000" {
			t.Errorf("%s (col %d): got %q, want %q", name, col, data[col], "0.000000")
		}
	}

	// Verify count fields are zero
	intZeroCols := map[int]string{
		10: "message_count", 12: "agent_count", 13: "agent_message_count",
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
	if parsed.SessionCount != 1 {
		t.Errorf("SessionCount mismatch: got %d, want 1 (session_count must be exported)", parsed.SessionCount)
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
	results, originalPath := sampleListResults()

	output, err := FormatSessionListJSON(results, originalPath, true)
	if err != nil {
		t.Fatalf("FormatSessionListJSON returned error: %v", err)
	}

	var parsed []map[string]any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("FormatSessionListJSON output is not valid JSON: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(parsed))
	}

	analyzed := parsed[0]
	for k, want := range map[string]any{
		"project_path":     filepath.Dir(results[0].Entry.FullPath),
		"original_path":    originalPath,
		"title":            "Fix the parser",
		"model":            "claude-opus-4-8",
		"start_time":       "2024-01-01T10:30:00Z",
		"duration_seconds": 5400.0,
	} {
		if analyzed[k] != want {
			t.Errorf("%s: got %v, want %v", k, analyzed[k], want)
		}
	}
	if cost, _ := analyzed["total_cost"].(map[string]any); cost["total_cost"] != 1.5 {
		t.Errorf("total_cost should be the breakdown object: %v", analyzed["total_cost"])
	}
	if _, ok := analyzed["created"]; ok {
		t.Error("created is gone: it was the file's mtime, not a creation time")
	}

	unreadable := parsed[1]
	for _, k := range []string{"title", "model", "start_time", "duration_seconds", "total_cost"} {
		if v, ok := unreadable[k]; ok {
			t.Errorf("unreadable session: %s = %v, want it absent", k, v)
		}
	}
	if unreadable["skipped_sessions"] != 1.0 || unreadable["project_path"] != filepath.Dir(results[1].Entry.FullPath) {
		t.Errorf("unreadable session keeps its scan fields: %v", unreadable)
	}
}

// === Table Formatter Tests ===

func TestFormatSessionTable_Plain(t *testing.T) {
	analysis := sampleAnalysis()

	output := FormatSessionTable(analysis, true, 0) // noColor = true

	// Check basic structure
	if !strings.Contains(output, "Session: test-ses") {
		t.Error("Table output missing session header")
	}
	if !strings.Contains(output, "API-equivalent estimate") {
		t.Error("Table output missing the hero total")
	}
	if !strings.Contains(output, "Messages: 10") {
		t.Error("Table output missing message count")
	}
}

func TestFormatSessionTable_WithColor(t *testing.T) {
	analysis := sampleAnalysis()

	output := FormatSessionTable(analysis, false, 0) // noColor = false

	// Colored output should still contain the data
	if !strings.Contains(output, "test-ses") {
		t.Error("Colored table output missing session ID")
	}
	if !strings.Contains(output, "API-equivalent estimate") {
		t.Error("Colored table output missing the hero total")
	}
}

func TestFormatSessionTable_WithCacheSavings(t *testing.T) {
	analysis := sampleAnalysis()

	output := FormatSessionTable(analysis, true, 0)

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

	output := FormatSessionTable(analysis, true, 0)

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
	var results []models.SessionResult
	for _, e := range sampleSessionEntries() {
		results = append(results, models.SessionResult{Entry: e, Analysis: &models.SessionAnalysis{MessageCount: 1}})
	}
	output := FormatSessionListTable(results, true, ListTableOptions{})

	for _, col := range []string{"ID", "WHEN", "LENGTH", "MODEL", "AGENTS", "COST", "TITLE"} {
		if !strings.Contains(output, col) {
			t.Errorf("list table missing %s column:\n%s", col, output)
		}
	}
	if !strings.Contains(output, "session-") {
		t.Error("list table missing sessions")
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
	colored := FormatSessionTable(analysis, false, 0)
	if !strings.Contains(colored, "\x1b[") {
		t.Error("colored output should contain ANSI escape codes")
	}

	// noColor=true → no ANSI
	plain := FormatSessionTable(analysis, true, 0)
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
				FirstActive:  time.Date(2024, 1, 2, 9, 0, 0, 0, time.UTC),
				LastActive:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			},
			{
				ProjectInfo:  models.ProjectInfo{DisplayName: "project-beta"},
				TotalCost:    models.CostBreakdown{TotalCost: 2.5, InputCost: 1.0, OutputCost: 1.5},
				SessionCount: 2,
				MessageCount: 15,
				FirstActive:  time.Date(2024, 1, 3, 9, 0, 0, 0, time.UTC),
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

	// Verify 17-column header
	if len(records[0]) != 17 {
		t.Fatalf("expected 17 columns, got %d", len(records[0]))
	}
	if records[0][0] != "project" {
		t.Errorf("first header column: got %q, want %q", records[0][0], "project")
	}
	if records[0][10] != "first_active" || records[0][11] != "last_active" {
		t.Errorf("activity columns: got %q, %q, want first_active, last_active", records[0][10], records[0][11])
	}
	if records[0][15] != "estimated_cost_messages" {
		t.Errorf("last header column: got %q, want %q", records[0][15], "estimated_cost_messages")
	}

	// Verify first data row
	if records[1][0] != "project-alpha" {
		t.Errorf("first project name: got %q, want %q", records[1][0], "project-alpha")
	}
	if records[1][1] != "3" {
		t.Errorf("first project sessions: got %q, want %q", records[1][1], "3")
	}
	if records[1][10] != "2024-01-02T09:00:00Z" {
		t.Errorf("first_active: got %q, want %q", records[1][10], "2024-01-02T09:00:00Z")
	}
	if records[1][11] != "2024-01-15T10:00:00Z" {
		t.Errorf("last_active: got %q, want %q", records[1][11], "2024-01-15T10:00:00Z")
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
	output := FormatGlobalTable(analysis, true, GlobalTableOptions{TopN: 10})

	// Verify key strings present in plain output
	if !strings.Contains(output, "project-alpha") {
		t.Error("output should contain 'project-alpha'")
	}
	if !strings.Contains(output, "project-beta") {
		t.Error("output should contain 'project-beta'")
	}
	if !strings.Contains(output, "API-equivalent estimate") {
		t.Error("output should contain the hero total")
	}
}

// A session nobody has replied to prints one line, not a report of empty
// sections.
func TestSessionTableEmptySession(t *testing.T) {
	out := FormatSessionTable(&models.SessionAnalysis{SessionID: "68994c84-0840-3234-39ed-0800317979f9"}, true, 0)
	if out != "No assistant messages in session 68994c84 yet." {
		t.Errorf("got %q", out)
	}
}
