package parser

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bardisty/ccusage/internal/models"
)

func TestGetLatestSession(t *testing.T) {
	t1 := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 12, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		index      *models.SessionsIndex
		expectedID string
		isNil      bool
	}{
		{
			name:  "nil index",
			index: nil,
			isNil: true,
		},
		{
			name:  "empty entries",
			index: &models.SessionsIndex{Entries: []models.SessionEntry{}},
			isNil: true,
		},
		{
			name: "single entry",
			index: &models.SessionsIndex{
				Entries: []models.SessionEntry{
					{SessionID: "session-1", Modified: t1},
				},
			},
			expectedID: "session-1",
		},
		{
			name: "multiple entries returns most recent",
			index: &models.SessionsIndex{
				Entries: []models.SessionEntry{
					{SessionID: "session-1", Modified: t1},
					{SessionID: "session-2", Modified: t2}, // Most recent
					{SessionID: "session-3", Modified: t3},
				},
			},
			expectedID: "session-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetLatestSession(tt.index)
			if tt.isNil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("unexpected nil result")
			}
			if result.SessionID != tt.expectedID {
				t.Errorf("SessionID: got %s, want %s", result.SessionID, tt.expectedID)
			}
		})
	}
}

func TestGetSessionByID(t *testing.T) {
	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "abc123"},
			{SessionID: "def456"},
			{SessionID: "ghi789"},
		},
	}

	tests := []struct {
		name       string
		sessionID  string
		shouldFind bool
	}{
		{"exact match first", "abc123", true},
		{"exact match middle", "def456", true},
		{"exact match last", "ghi789", true},
		{"not found", "xyz999", false},
		{"partial id not found", "abc", false}, // GetSessionByID requires exact match
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSessionByID(index, tt.sessionID)
			if tt.shouldFind {
				if result == nil {
					t.Error("expected to find session, got nil")
				} else if result.SessionID != tt.sessionID {
					t.Errorf("SessionID: got %s, want %s", result.SessionID, tt.sessionID)
				}
			} else {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
			}
		})
	}
}

func TestGetSessionByIDNilIndex(t *testing.T) {
	result := GetSessionByID(nil, "any-id")
	if result != nil {
		t.Errorf("expected nil for nil index, got %v", result)
	}
}

func TestGetSessionsByModified(t *testing.T) {
	t1 := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 12, 12, 0, 0, 0, time.UTC)

	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "old", Modified: t1},
			{SessionID: "newest", Modified: t2},
			{SessionID: "middle", Modified: t3},
		},
	}

	result := GetSessionsByModified(index)

	if len(result) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(result))
	}

	// Should be sorted most recent first
	expectedOrder := []string{"newest", "middle", "old"}
	for i, expectedID := range expectedOrder {
		if result[i].SessionID != expectedID {
			t.Errorf("position %d: got %s, want %s", i, result[i].SessionID, expectedID)
		}
	}
}

func TestGetSessionsByCreated(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC)

	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "oldest", Created: t1},
			{SessionID: "newest", Created: t2},
			{SessionID: "middle", Created: t3},
		},
	}

	result := GetSessionsByCreated(index)

	if len(result) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(result))
	}

	// Should be sorted most recent first
	expectedOrder := []string{"newest", "middle", "oldest"}
	for i, expectedID := range expectedOrder {
		if result[i].SessionID != expectedID {
			t.Errorf("position %d: got %s, want %s", i, result[i].SessionID, expectedID)
		}
	}
}

func TestGetSessionsByModifiedEmpty(t *testing.T) {
	tests := []struct {
		name  string
		index *models.SessionsIndex
	}{
		{"nil index", nil},
		{"empty entries", &models.SessionsIndex{Entries: []models.SessionEntry{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSessionsByModified(tt.index)
			if result != nil {
				t.Errorf("expected nil, got %v", result)
			}
		})
	}
}

func TestDiscoverSessionsFromDisk(t *testing.T) {
	// Create a temp directory with test session files
	tmpDir, err := os.MkdirTemp("", "sessions-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test session files
	sessionFiles := []string{
		"session-1.jsonl",
		"session-2.jsonl",
		"session-3.jsonl",
	}

	for _, f := range sessionFiles {
		content := `{"type":"assistant","message":{"model":"test","usage":{}},"timestamp":"2024-01-01T00:00:00Z"}`
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create a non-jsonl file (should be ignored)
	if err := os.WriteFile(filepath.Join(tmpDir, "notes.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory (should be ignored)
	if err := os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	sessions, err := DiscoverSessionsFromDisk(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}

	// Check that session IDs are correct (without .jsonl extension)
	sessionIDs := make(map[string]bool)
	for _, s := range sessions {
		sessionIDs[s.SessionID] = true
	}

	for _, expected := range []string{"session-1", "session-2", "session-3"} {
		if !sessionIDs[expected] {
			t.Errorf("expected to find session %s", expected)
		}
	}

	// Check that message count was calculated
	for _, s := range sessions {
		if s.MessageCount != 1 {
			t.Errorf("expected message count 1, got %d for session %s", s.MessageCount, s.SessionID)
		}
	}
}

func TestDiscoverEmptyDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sessions-empty-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sessions, err := DiscoverSessionsFromDisk(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestMergeSessionSources(t *testing.T) {
	t1 := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "session-1", FullPath: "/path/session-1.jsonl", Created: t1, Modified: t2, MessageCount: 10},
			{SessionID: "session-2", FullPath: "/path/session-2.jsonl", Created: t1, Modified: t1, MessageCount: 5},
		},
	}

	diskSessions := []models.SessionEntry{
		{SessionID: "session-1", FullPath: "/path/session-1.jsonl", Modified: t2, MessageCount: 10},
		{SessionID: "session-3", FullPath: "/path/session-3.jsonl", Modified: t1, MessageCount: 3}, // Orphan
	}

	merged, orphanCount := MergeSessionSources(index, diskSessions)

	if orphanCount != 1 {
		t.Errorf("expected 1 orphan, got %d", orphanCount)
	}

	// Should have 3 sessions total: 2 from disk (1 matched, 1 orphan) + 1 from index only
	if len(merged) != 3 {
		t.Errorf("expected 3 merged sessions, got %d", len(merged))
	}

	// Check that session-1 has the index metadata (with Created time)
	for _, s := range merged {
		if s.SessionID == "session-1" && s.Created.IsZero() {
			t.Error("session-1 should have Created time from index")
		}
	}
}

func TestMergeOrphanDetection(t *testing.T) {
	// Empty index, all disk sessions are orphans
	diskSessions := []models.SessionEntry{
		{SessionID: "orphan-1"},
		{SessionID: "orphan-2"},
		{SessionID: "orphan-3"},
	}

	merged, orphanCount := MergeSessionSources(nil, diskSessions)

	if orphanCount != 3 {
		t.Errorf("expected 3 orphans, got %d", orphanCount)
	}

	if len(merged) != 3 {
		t.Errorf("expected 3 merged sessions, got %d", len(merged))
	}
}

func TestMergeIndexOnly(t *testing.T) {
	t1 := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)

	// Index has entries for files that no longer exist
	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "deleted-1", Created: t1},
			{SessionID: "deleted-2", Created: t1},
		},
	}

	// No files on disk
	diskSessions := []models.SessionEntry{}

	merged, orphanCount := MergeSessionSources(index, diskSessions)

	if orphanCount != 0 {
		t.Errorf("expected 0 orphans (only index entries), got %d", orphanCount)
	}

	// Should still include index entries
	if len(merged) != 2 {
		t.Errorf("expected 2 merged sessions from index, got %d", len(merged))
	}
}

func TestCountMessagesInFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messages-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.jsonl")
	content := `{"type":"user","message":"hello"}
{"type":"assistant","message":{"model":"test"}}
{"type":"user","message":"world"}
{"type":"assistant","message":{"model":"test"}}
{"type":"assistant","message":{"model":"test"}}`

	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	count := countMessagesInFile(testFile)
	if count != 3 {
		t.Errorf("expected 3 assistant messages, got %d", count)
	}
}

func TestCountMessagesInFileNotFound(t *testing.T) {
	count := countMessagesInFile("/nonexistent/path/file.jsonl")
	if count != -1 {
		t.Errorf("expected -1 for nonexistent file, got %d", count)
	}
}

// === ParseSessionsIndex Tests ===

func TestParseSessionsIndex(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "index-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	indexPath := filepath.Join(tmpDir, "sessions-index.json")
	content := `{
		"entries": [
			{
				"sessionId": "session-abc123",
				"fullPath": "/path/to/session-abc123.jsonl",
				"messageCount": 15,
				"created": "2024-01-10T10:00:00Z",
				"modified": "2024-01-10T12:00:00Z"
			},
			{
				"sessionId": "session-def456",
				"fullPath": "/path/to/session-def456.jsonl",
				"messageCount": 8,
				"created": "2024-01-11T10:00:00Z",
				"modified": "2024-01-11T14:00:00Z"
			}
		]
	}`

	if err := os.WriteFile(indexPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	index, err := ParseSessionsIndex(indexPath)
	if err != nil {
		t.Fatalf("ParseSessionsIndex returned error: %v", err)
	}

	if index == nil {
		t.Fatal("ParseSessionsIndex returned nil index")
	}

	if len(index.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(index.Entries))
	}

	// Verify first entry
	if index.Entries[0].SessionID != "session-abc123" {
		t.Errorf("first entry SessionID: got %q, want %q", index.Entries[0].SessionID, "session-abc123")
	}
	if index.Entries[0].MessageCount != 15 {
		t.Errorf("first entry MessageCount: got %d, want %d", index.Entries[0].MessageCount, 15)
	}
}

func TestParseSessionsIndex_FileNotFound(t *testing.T) {
	_, err := ParseSessionsIndex("/nonexistent/path/sessions-index.json")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestParseSessionsIndex_InvalidJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "index-invalid-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	indexPath := filepath.Join(tmpDir, "sessions-index.json")
	// Write invalid JSON
	if err := os.WriteFile(indexPath, []byte("{ invalid json }"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = ParseSessionsIndex(indexPath)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestParseSessionsIndex_EmptyFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "index-empty-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	indexPath := filepath.Join(tmpDir, "sessions-index.json")
	// Write empty JSON object
	if err := os.WriteFile(indexPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	index, err := ParseSessionsIndex(indexPath)
	if err != nil {
		t.Fatalf("ParseSessionsIndex returned error for empty object: %v", err)
	}

	if len(index.Entries) != 0 {
		t.Errorf("expected 0 entries for empty index, got %d", len(index.Entries))
	}
}

// === ExtractAgentID Tests ===

func TestExtractAgentID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard agent path",
			input:    "/path/to/project/session-123/subagents/agent-abc456.jsonl",
			expected: "abc456",
		},
		{
			name:     "just filename",
			input:    "agent-xyz789.jsonl",
			expected: "xyz789",
		},
		{
			name:     "uuid-style agent id",
			input:    "/path/agent-550e8400-e29b-41d4-a716-446655440000.jsonl",
			expected: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:     "short agent id",
			input:    "agent-1.jsonl",
			expected: "1",
		},
		{
			name:     "not an agent file - returns base",
			input:    "/path/to/regular-file.jsonl",
			expected: "regular-file.jsonl",
		},
		{
			name:     "agent prefix but wrong suffix",
			input:    "agent-test.txt",
			expected: "agent-test.txt",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ExtractAgentID(tc.input)
			if result != tc.expected {
				t.Errorf("ExtractAgentID(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

// === DiscoverAgentSessions Tests ===

func TestDiscoverAgentSessions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agents-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sessionID := "session-main"
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")

	// Create the subagents directory structure
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create agent files
	agentFiles := []string{
		"agent-001.jsonl",
		"agent-002.jsonl",
		"agent-abc.jsonl",
	}
	for _, f := range agentFiles {
		if err := os.WriteFile(filepath.Join(subagentsDir, f), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create a non-agent file (should be ignored)
	if err := os.WriteFile(filepath.Join(subagentsDir, "notes.txt"), []byte("notes"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a file that doesn't match agent-* pattern (should be ignored)
	if err := os.WriteFile(filepath.Join(subagentsDir, "other-file.jsonl"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	paths, err := DiscoverAgentSessions(tmpDir, sessionID)
	if err != nil {
		t.Fatalf("DiscoverAgentSessions returned error: %v", err)
	}

	if len(paths) != 3 {
		t.Errorf("expected 3 agent paths, got %d", len(paths))
	}

	// Verify the paths contain the expected files
	foundAgents := make(map[string]bool)
	for _, p := range paths {
		base := filepath.Base(p)
		foundAgents[base] = true
	}

	for _, expected := range agentFiles {
		if !foundAgents[expected] {
			t.Errorf("expected to find agent file %s", expected)
		}
	}

	// Verify non-agent files were not included
	if foundAgents["notes.txt"] {
		t.Error("notes.txt should not be included in agent paths")
	}
	if foundAgents["other-file.jsonl"] {
		t.Error("other-file.jsonl should not be included in agent paths")
	}
}

func TestDiscoverAgentSessions_NoSubagentsDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "no-agents-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Session directory exists but no subagents folder
	sessionID := "session-no-agents"
	if err := os.MkdirAll(filepath.Join(tmpDir, sessionID), 0755); err != nil {
		t.Fatal(err)
	}

	paths, err := DiscoverAgentSessions(tmpDir, sessionID)
	if err != nil {
		t.Fatalf("DiscoverAgentSessions should not error for missing subagents dir: %v", err)
	}

	if paths != nil && len(paths) != 0 {
		t.Errorf("expected nil or empty paths for missing subagents dir, got %v", paths)
	}
}

func TestDiscoverAgentSessions_EmptySubagentsDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "empty-agents-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sessionID := "session-empty"
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")

	// Create empty subagents directory
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatal(err)
	}

	paths, err := DiscoverAgentSessions(tmpDir, sessionID)
	if err != nil {
		t.Fatalf("DiscoverAgentSessions returned error: %v", err)
	}

	if len(paths) != 0 {
		t.Errorf("expected 0 paths for empty subagents dir, got %d", len(paths))
	}
}

func TestDiscoverAgentSessions_SessionDirNotExist(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nonexistent-session-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Session directory doesn't exist at all
	paths, err := DiscoverAgentSessions(tmpDir, "nonexistent-session")
	if err != nil {
		t.Fatalf("DiscoverAgentSessions should not error for nonexistent session: %v", err)
	}

	if paths != nil && len(paths) != 0 {
		t.Errorf("expected nil or empty paths for nonexistent session, got %v", paths)
	}
}
