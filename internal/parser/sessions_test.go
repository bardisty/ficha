package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

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

	sessions, err := DiscoverSessionsFromDisk(tmpDir, true)
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

// TestDiscoverSessionsFromDisk_SkipsCounting verifies that countMessages=false
// leaves message counts at zero while still discovering agent sub-sessions.
// Analysis paths rely on this: they recompute counts from their own parse, so
// the discovery-time file scan would be wasted work.
func TestDiscoverSessionsFromDisk_SkipsCounting(t *testing.T) {
	tmpDir := t.TempDir()

	sessionID := "sess-count-skip"
	line := `{"type":"assistant","message":{"id":"m1","model":"test","usage":{}},"timestamp":"2024-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(tmpDir, sessionID+".jsonl"), []byte(line+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// An agent sub-session so we can confirm discovery still runs
	subDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "agent-a1.jsonl"), []byte(line+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	sessions, err := DiscoverSessionsFromDisk(tmpDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	s := sessions[0]
	// Counting skipped
	if s.MessageCount != 0 {
		t.Errorf("MessageCount: got %d, want 0 when counting disabled", s.MessageCount)
	}
	if s.AgentMessageCount != 0 {
		t.Errorf("AgentMessageCount: got %d, want 0 when counting disabled", s.AgentMessageCount)
	}
	// Agent discovery still ran (cheap directory listing, always performed)
	if s.AgentCount != 1 || len(s.AgentPaths) != 1 {
		t.Errorf("agent discovery: got AgentCount=%d AgentPaths=%v, want 1 agent", s.AgentCount, s.AgentPaths)
	}

	// Counting enabled must still work (parent + agent = 2)
	counted, err := DiscoverSessionsFromDisk(tmpDir, true)
	if err != nil {
		t.Fatal(err)
	}
	if counted[0].MessageCount != 2 {
		t.Errorf("MessageCount with counting: got %d, want 2", counted[0].MessageCount)
	}
}

func TestDiscoverEmptyDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sessions-empty-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sessions, err := DiscoverSessionsFromDisk(tmpDir, true)
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

	merged, orphanCount := MergeSessionSources(index, diskSessions, "/path", true)

	if orphanCount != 1 {
		t.Errorf("expected 1 orphan, got %d", orphanCount)
	}

	// Should have 2 sessions: 1 matched + 1 orphan (session-2 is a ghost — filtered)
	if len(merged) != 2 {
		t.Errorf("expected 2 merged sessions, got %d", len(merged))
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

	merged, orphanCount := MergeSessionSources(nil, diskSessions, "/path", true)

	if orphanCount != 3 {
		t.Errorf("expected 3 orphans, got %d", orphanCount)
	}

	if len(merged) != 3 {
		t.Errorf("expected 3 merged sessions, got %d", len(merged))
	}
}

func TestMergeIndexOnly(t *testing.T) {
	t1 := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)

	// Index has entries for files that no longer exist on disk
	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "deleted-1", FullPath: "/nonexistent/deleted-1.jsonl", Created: t1},
			{SessionID: "deleted-2", FullPath: "/nonexistent/deleted-2.jsonl", Created: t1},
		},
	}

	// No files on disk
	diskSessions := []models.SessionEntry{}

	merged, orphanCount := MergeSessionSources(index, diskSessions, "/path", true)

	if orphanCount != 0 {
		t.Errorf("expected 0 orphans (only index entries), got %d", orphanCount)
	}

	// Ghost sessions should be filtered out — files don't exist
	if len(merged) != 0 {
		t.Errorf("expected 0 merged sessions (ghost sessions filtered), got %d", len(merged))
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

	count, _ := countMessagesInFile(testFile)
	if count != 3 {
		t.Errorf("expected 3 assistant messages, got %d", count)
	}
}

func TestCountMessagesInFileNotFound(t *testing.T) {
	count, _ := countMessagesInFile("/nonexistent/path/file.jsonl")
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

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 0 {
		t.Fatalf("DiscoverAgentSessions reported %d unreadable dir(s), want 0", unreadable)
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

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 0 {
		t.Fatalf("DiscoverAgentSessions reported %d unreadable dir(s), want 0", unreadable)
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

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 0 {
		t.Fatalf("DiscoverAgentSessions reported %d unreadable dir(s), want 0", unreadable)
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
	paths, unreadable := DiscoverAgentSessions(tmpDir, "nonexistent-session")
	if unreadable != 0 {
		t.Fatalf("DiscoverAgentSessions reported %d unreadable dir(s), want 0", unreadable)
	}

	if paths != nil && len(paths) != 0 {
		t.Errorf("expected nil or empty paths for nonexistent session, got %v", paths)
	}
}

func TestMergeGhostSessionsFiltered(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ghost-sessions-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create one real file on disk
	realPath := filepath.Join(tmpDir, "real-session.jsonl")
	if err := os.WriteFile(realPath, []byte(`{"type":"assistant","message":{"model":"test","usage":{}}}`), 0644); err != nil {
		t.Fatal(err)
	}

	t1 := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)

	// Index has entries: one with a real file, two with nonexistent files
	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "real-session", FullPath: realPath, Created: t1},
			{SessionID: "ghost-1", FullPath: "/nonexistent/ghost-1.jsonl", Created: t1},
			{SessionID: "ghost-2", FullPath: "/nonexistent/ghost-2.jsonl", Created: t1},
		},
	}

	// Only ghost sessions remain in index (real-session matched disk)
	diskSessions := []models.SessionEntry{
		{SessionID: "real-session", FullPath: realPath, Modified: t1, MessageCount: 1},
	}

	merged, orphanCount := MergeSessionSources(index, diskSessions, tmpDir, true)

	if orphanCount != 0 {
		t.Errorf("expected 0 orphans, got %d", orphanCount)
	}

	// Should have 1: real-session matched. ghost-1 and ghost-2 filtered.
	if len(merged) != 1 {
		t.Errorf("expected 1 merged session (ghosts filtered), got %d", len(merged))
	}

	if len(merged) > 0 && merged[0].SessionID != "real-session" {
		t.Errorf("expected real-session, got %s", merged[0].SessionID)
	}
}

func TestMergeFullPathFromDisk(t *testing.T) {
	t1 := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)

	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "session-1", FullPath: "/old/path/session-1.jsonl", Created: t1},
		},
	}

	diskSessions := []models.SessionEntry{
		{SessionID: "session-1", FullPath: "/new/path/session-1.jsonl", Modified: t1, MessageCount: 5},
	}

	merged, _ := MergeSessionSources(index, diskSessions, "/new/path", true)

	if len(merged) != 1 {
		t.Fatalf("expected 1 merged session, got %d", len(merged))
	}

	// FullPath should come from disk, not the stale index
	if merged[0].FullPath != "/new/path/session-1.jsonl" {
		t.Errorf("expected disk FullPath, got %s", merged[0].FullPath)
	}

	// Created should still come from index
	if merged[0].Created.IsZero() {
		t.Error("expected Created time from index to be preserved")
	}
}

func TestCountMessagesInFile_OversizedLineMiddle(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "oversized.jsonl")
	validLine1 := `{"type":"assistant","requestId":"r1","message":{"id":"m1","model":"test","usage":{}}}`
	validLine2 := `{"type":"assistant","requestId":"r2","message":{"id":"m2","model":"test","usage":{}}}`

	// Oversized line in the middle: only that line is skipped; counting continues
	var content strings.Builder
	content.Grow(maxLineBytes + 256)
	content.WriteString(validLine1)
	content.WriteString("\n")
	content.WriteString(strings.Repeat("x", maxLineBytes+1))
	content.WriteString("\n")
	content.WriteString(validLine2)
	content.WriteString("\n")

	if err := os.WriteFile(testFile, []byte(content.String()), 0644); err != nil {
		t.Fatal(err)
	}

	count, _ := countMessagesInFile(testFile)
	if count != 2 {
		t.Errorf("expected 2 (messages after the oversized line must still count), got %d", count)
	}
}

func TestMergeIndexOnly_RebuildsFromDisk(t *testing.T) {
	// Index-only entries must not keep stale counts or miss agents.
	tmpDir := t.TempDir()

	sessionPath := filepath.Join(tmpDir, "idx-only.jsonl")
	parentContent := `{"type":"assistant","requestId":"r1","message":{"id":"m1","model":"test","usage":{}}}
{"type":"assistant","requestId":"r2","message":{"id":"m2","model":"test","usage":{}}}
`
	if err := os.WriteFile(sessionPath, []byte(parentContent), 0644); err != nil {
		t.Fatal(err)
	}

	subagentsDir := filepath.Join(tmpDir, "idx-only", "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentContent := `{"type":"assistant","requestId":"r3","message":{"id":"m3","model":"test","usage":{}}}
`
	if err := os.WriteFile(filepath.Join(subagentsDir, "agent-a1.jsonl"), []byte(agentContent), 0644); err != nil {
		t.Fatal(err)
	}

	t1 := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)
	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			// Stale MessageCount (99) and no agent info in the index
			{SessionID: "idx-only", FullPath: sessionPath, Created: t1, MessageCount: 99},
		},
	}

	// Disk scan missed the session (e.g. created after the scan)
	merged, orphanCount := MergeSessionSources(index, nil, tmpDir, true)

	if orphanCount != 0 {
		t.Errorf("expected 0 orphans, got %d", orphanCount)
	}
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged session, got %d", len(merged))
	}
	e := merged[0]
	if e.MessageCount != 3 {
		t.Errorf("MessageCount: got %d, want 3 (2 parent + 1 agent, recounted from disk)", e.MessageCount)
	}
	if e.AgentCount != 1 || len(e.AgentPaths) != 1 {
		t.Errorf("agent discovery not re-run: AgentCount=%d, AgentPaths=%v", e.AgentCount, e.AgentPaths)
	}
	if e.AgentMessageCount != 1 {
		t.Errorf("AgentMessageCount: got %d, want 1", e.AgentMessageCount)
	}
	if !e.Created.Equal(t1) {
		t.Errorf("Created should keep index metadata: got %v, want %v", e.Created, t1)
	}
	if e.Modified.IsZero() {
		t.Error("Modified should be set from the file mtime")
	}
}

// TestMergeIndexOnly_SkipsCounting verifies countMessages=false is forwarded to
// the index-only rebuild: agents are still discovered, but the message-count
// scan is skipped (analysis paths recompute counts themselves).
func TestMergeIndexOnly_SkipsCounting(t *testing.T) {
	tmpDir := t.TempDir()

	sessionPath := filepath.Join(tmpDir, "idx-only.jsonl")
	parentContent := `{"type":"assistant","requestId":"r1","message":{"id":"m1","model":"test","usage":{}}}
{"type":"assistant","requestId":"r2","message":{"id":"m2","model":"test","usage":{}}}
`
	if err := os.WriteFile(sessionPath, []byte(parentContent), 0644); err != nil {
		t.Fatal(err)
	}

	subagentsDir := filepath.Join(tmpDir, "idx-only", "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentContent := `{"type":"assistant","requestId":"r3","message":{"id":"m3","model":"test","usage":{}}}
`
	if err := os.WriteFile(filepath.Join(subagentsDir, "agent-a1.jsonl"), []byte(agentContent), 0644); err != nil {
		t.Fatal(err)
	}

	t1 := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)
	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "idx-only", FullPath: sessionPath, Created: t1, MessageCount: 99},
		},
	}

	merged, _ := MergeSessionSources(index, nil, tmpDir, false)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged session, got %d", len(merged))
	}
	e := merged[0]
	// Counting skipped: the stale index count (99) must not survive, and no
	// fresh count is computed either
	if e.MessageCount != 0 {
		t.Errorf("MessageCount: got %d, want 0 when counting disabled", e.MessageCount)
	}
	if e.AgentMessageCount != 0 {
		t.Errorf("AgentMessageCount: got %d, want 0 when counting disabled", e.AgentMessageCount)
	}
	// Agent discovery still runs
	if e.AgentCount != 1 || len(e.AgentPaths) != 1 {
		t.Errorf("agent discovery not re-run: AgentCount=%d, AgentPaths=%v", e.AgentCount, e.AgentPaths)
	}
}

func TestMergeIndexOnly_OutsideProjectDirDropped(t *testing.T) {
	// The index's FullPath is untrusted: entries pointing outside the project
	// directory are dropped even if the file exists.
	projectDir := t.TempDir()
	outsideDir := t.TempDir()

	outsidePath := filepath.Join(outsideDir, "escaped.jsonl")
	if err := os.WriteFile(outsidePath, []byte(`{"type":"assistant","message":{"model":"test","usage":{}}}`), 0644); err != nil {
		t.Fatal(err)
	}

	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "escaped", FullPath: outsidePath, Created: time.Now()},
		},
	}

	merged, _ := MergeSessionSources(index, nil, projectDir, true)
	if len(merged) != 0 {
		t.Errorf("expected entry outside projectDir to be dropped, got %d entries", len(merged))
	}
}

// An index entry whose sessionId disagrees with its fullPath's filename stem
// aliases a transcript the disk scan already produced under the stem's
// identity. Merging by SessionID alone never matches the two, so the alias
// used to survive as a second session row over the same file.
func TestMergeIndexOnly_AliasedStemDropped(t *testing.T) {
	projectDir := t.TempDir()

	sessionPath := filepath.Join(projectDir, "def.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"type":"assistant","requestId":"r1","message":{"id":"m1","model":"test","usage":{}}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	disk, err := DiscoverSessionsFromDisk(projectDir, true)
	if err != nil {
		t.Fatal(err)
	}
	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "abc", FullPath: sessionPath, Created: time.Now()},
		},
	}

	merged, _ := MergeSessionSources(index, disk, projectDir, true)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged session, got %d", len(merged))
	}
	if merged[0].SessionID != "def" {
		t.Errorf("SessionID: got %q, want %q (disk identity wins)", merged[0].SessionID, "def")
	}
}

// An index entry pointing at a nested agent file passes a containment-only
// check but is not a session transcript: it must not become a session row
// (its messages are already rolled into the parent session).
func TestMergeIndexOnly_NestedAgentPathDropped(t *testing.T) {
	projectDir := t.TempDir()

	parentPath := filepath.Join(projectDir, "parent.jsonl")
	if err := os.WriteFile(parentPath, []byte(`{"type":"assistant","requestId":"r1","message":{"id":"m1","model":"test","usage":{}}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	subagentsDir := filepath.Join(projectDir, "parent", "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(subagentsDir, "agent-x.jsonl")
	if err := os.WriteFile(agentPath, []byte(`{"type":"assistant","requestId":"r2","message":{"id":"m2","model":"test","usage":{}}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	disk, err := DiscoverSessionsFromDisk(projectDir, true)
	if err != nil {
		t.Fatal(err)
	}
	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "agent-x", FullPath: agentPath, Created: time.Now()},
		},
	}

	merged, _ := MergeSessionSources(index, disk, projectDir, true)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged session, got %d", len(merged))
	}
	if merged[0].SessionID != "parent" {
		t.Errorf("SessionID: got %q, want %q", merged[0].SessionID, "parent")
	}
}

func TestIsSessionFilePath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		dir       string
		sessionID string
		want      bool
	}{
		{"top-level session file", "/proj/abc.jsonl", "/proj", "abc", true},
		{"dot segments cleaned", "/proj/./abc.jsonl", "/proj", "abc", true},
		{"stem disagrees with sessionId", "/proj/def.jsonl", "/proj", "abc", false},
		{"nested agent file", "/proj/abc/subagents/agent-x.jsonl", "/proj", "agent-x", false},
		{"file outside dir", "/other/abc.jsonl", "/proj", "abc", false},
		{"traversal escape", "/proj/../other/abc.jsonl", "/proj", "abc", false},
		{"sibling with shared prefix", "/proj-evil/abc.jsonl", "/proj", "abc", false},
		{"missing .jsonl suffix", "/proj/abc", "/proj", "abc", false},
		{"path equals dir", "/proj", "/proj", "proj", false},
		{"empty path", "", "/proj", "abc", false},
		{"empty dir", "/proj/abc.jsonl", "", "abc", false},
		{"empty sessionId", "/proj/.jsonl", "/proj", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSessionFilePath(tt.path, tt.dir, tt.sessionID); got != tt.want {
				t.Errorf("isSessionFilePath(%q, %q, %q) = %v, want %v", tt.path, tt.dir, tt.sessionID, got, tt.want)
			}
		})
	}
}

func TestMergeDuplicateSessionIDsInIndex(t *testing.T) {
	// Index has 2 entries with same ID - last wins in Go map
	t1 := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)
	index := &models.SessionsIndex{
		Entries: []models.SessionEntry{
			{SessionID: "dup-id", FullPath: "/path/first.jsonl", Created: t1, MessageCount: 5},
			{SessionID: "dup-id", FullPath: "/path/second.jsonl", Created: t1, MessageCount: 10},
		},
	}
	diskSessions := []models.SessionEntry{
		{SessionID: "dup-id", FullPath: "/path/first.jsonl", Modified: t1, MessageCount: 5},
	}
	merged, _ := MergeSessionSources(index, diskSessions, "/path", true)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged session, got %d", len(merged))
	}
}

func TestMergeBothNilAndEmpty(t *testing.T) {
	// nil index + nil disk
	merged1, orphans1 := MergeSessionSources(nil, nil, "/path", true)
	if len(merged1) != 0 {
		t.Errorf("nil+nil: expected 0 merged, got %d", len(merged1))
	}
	if orphans1 != 0 {
		t.Errorf("nil+nil: expected 0 orphans, got %d", orphans1)
	}

	// nil index + empty disk
	merged2, orphans2 := MergeSessionSources(nil, []models.SessionEntry{}, "/path", true)
	if len(merged2) != 0 {
		t.Errorf("nil+empty: expected 0 merged, got %d", len(merged2))
	}
	if orphans2 != 0 {
		t.Errorf("nil+empty: expected 0 orphans, got %d", orphans2)
	}
}

func TestCountMessagesInFile_NullMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "null-message-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "null-msg.jsonl")
	content := `{"type":"assistant","message":null}
{"type":"assistant","message":{"model":"test","usage":{}}}
{"type":"assistant"}`

	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	count, _ := countMessagesInFile(testFile)
	// Only the second line has a non-null message
	if count != 1 {
		t.Errorf("expected 1 (only non-null message), got %d", count)
	}
}

func TestCountMessagesInFile_DeduplicatesStreamingLines(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.jsonl")
	// msg_A streamed across 3 lines, msg_B once, plus 2 lines without ids
	content := `{"type":"assistant","requestId":"req_A","message":{"id":"msg_A","model":"test","usage":{"output_tokens":5}}}
{"type":"assistant","requestId":"req_A","message":{"id":"msg_A","model":"test","usage":{"output_tokens":120}}}
{"type":"assistant","requestId":"req_A","message":{"id":"msg_A","model":"test","usage":{"output_tokens":394}}}
{"type":"assistant","requestId":"req_B","message":{"id":"msg_B","model":"test","usage":{"output_tokens":10}}}
{"type":"assistant","message":{"model":"test"}}
{"type":"assistant","message":{"model":"test"}}`

	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	count, _ := countMessagesInFile(testFile)
	if count != 4 {
		t.Errorf("expected 4 (2 distinct ids + 2 id-less lines), got %d", count)
	}
}

// writeWorkflowRun creates a workflow run dir with the given agent files plus
// the non-agent files a real run contains (journal, agent meta).
func writeWorkflowRun(t *testing.T, subagentsDir, runID string, agentFiles ...string) {
	t.Helper()
	runDir := filepath.Join(subagentsDir, "workflows", runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range agentFiles {
		if err := os.WriteFile(filepath.Join(runDir, f), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	extras := map[string]string{
		"journal.jsonl":     `{"type":"started","key":"v2:abc","agentId":"x"}`,
		"agent-x.meta.json": `{"agentType":"general-purpose","spawnDepth":1}`,
	}
	for name, content := range extras {
		if err := os.WriteFile(filepath.Join(runDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiscoverAgentSessions_WithWorkflows(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "session-wf"
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatal(err)
	}

	for _, f := range []string{"agent-r1.jsonl", "agent-r2.jsonl"} {
		if err := os.WriteFile(filepath.Join(subagentsDir, f), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeWorkflowRun(t, subagentsDir, "wf_run-a", "agent-w1.jsonl", "agent-w2.jsonl")
	writeWorkflowRun(t, subagentsDir, "wf_run-b", "agent-w3.jsonl")

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 0 {
		t.Fatalf("DiscoverAgentSessions reported %d unreadable dir(s), want 0", unreadable)
	}

	var bases []string
	for _, p := range paths {
		bases = append(bases, filepath.Base(p))
	}
	want := []string{"agent-r1.jsonl", "agent-r2.jsonl", "agent-w1.jsonl", "agent-w2.jsonl", "agent-w3.jsonl"}
	if len(bases) != len(want) {
		t.Fatalf("expected %d agent paths (regular first, then runs alphabetically), got %d: %v", len(want), len(bases), bases)
	}
	for i, w := range want {
		if bases[i] != w {
			t.Errorf("path %d: expected %s, got %s", i, w, bases[i])
		}
	}
}

func TestDiscoverAgentSessions_WorkflowsOnly(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "session-wf-only"
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeWorkflowRun(t, subagentsDir, "wf_solo", "agent-w1.jsonl")

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 0 {
		t.Fatalf("DiscoverAgentSessions reported %d unreadable dir(s), want 0", unreadable)
	}
	if len(paths) != 1 || filepath.Base(paths[0]) != "agent-w1.jsonl" {
		t.Errorf("expected only agent-w1.jsonl, got %v", paths)
	}
}

func TestExtractWorkflowRunID(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "workflow agent path",
			path: filepath.Join("proj", "sess", "subagents", "workflows", "wf_abc-123", "agent-x.jsonl"),
			want: "wf_abc-123",
		},
		{
			name: "regular subagent path",
			path: filepath.Join("proj", "sess", "subagents", "agent-x.jsonl"),
			want: "",
		},
		{
			name: "workflows dir without subagents parent",
			path: filepath.Join("proj", "sess", "workflows", "wf_abc", "agent-x.jsonl"),
			want: "",
		},
		{
			name: "bare filename",
			path: "agent-x.jsonl",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractWorkflowRunID(tt.path); got != tt.want {
				t.Errorf("ExtractWorkflowRunID(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestParseWorkflowMeta(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "session-meta"
	wfDir := filepath.Join(tmpDir, sessionID, "workflows")
	if err := os.MkdirAll(wfDir, 0755); err != nil {
		t.Fatal(err)
	}

	valid := `{"runId":"wf_ok","workflowName":"audit-codebase","status":"completed","script":"` +
		strings.Repeat("x", 4096) + `","agentCount":9,"totalTokens":638238}`
	if err := os.WriteFile(filepath.Join(wfDir, "wf_ok.json"), []byte(valid), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wfDir, "wf_bad.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}

	meta, ok := ParseWorkflowMeta(tmpDir, sessionID, "wf_ok")
	if !ok {
		t.Error("expected ok=true for valid metadata file")
	}
	if meta.RunID != "wf_ok" || meta.Name != "audit-codebase" || meta.Status != "completed" {
		t.Errorf("unexpected meta: %+v", meta)
	}

	meta, ok = ParseWorkflowMeta(tmpDir, sessionID, "wf_missing")
	if ok {
		t.Error("expected ok=false for missing metadata file")
	}
	if meta.RunID != "wf_missing" || meta.Name != "" || meta.Status != "" {
		t.Errorf("expected runID-only fallback, got %+v", meta)
	}

	meta, ok = ParseWorkflowMeta(tmpDir, sessionID, "wf_bad")
	if ok {
		t.Error("expected ok=false for corrupt metadata file")
	}
	if meta.RunID != "wf_bad" {
		t.Errorf("expected runID-only fallback, got %+v", meta)
	}
}

// === Skipped-input accounting (AGENT-04, DEDUP-01) ===

// makeUnreadableDir strips every permission bit from dir and restores them when
// the test ends. A chmod failure is fatal (the dir should exist); only an
// environment where chmod 000 still permits reads — root, or a filesystem
// without POSIX modes — skips the test.
func makeUnreadableDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0000); err != nil {
		t.Fatalf("chmod 000 %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("chmod 000 does not bar directory reads (running as root?)")
	}
}

// An unreadable subagents/ dir hides every agent in it. Reporting "no agents"
// would understate the session's cost in silence, so discovery counts it.
func TestDiscoverAgentSessions_UnreadableSubagentsDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagentsDir, "agent-a.jsonl"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	makeUnreadableDir(t, subagentsDir)

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 1 {
		t.Errorf("unreadableDirs: got %d, want 1", unreadable)
	}
	if len(paths) != 0 {
		t.Errorf("paths: got %v, want none (the dir could not be listed)", paths)
	}
}

// subagents/ is a regular file (ENOTDIR, not NotExist) — also a real error.
func TestDiscoverAgentSessions_SubagentsIsAFile(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	if err := os.MkdirAll(filepath.Join(tmpDir, sessionID), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, sessionID, "subagents"), []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 1 {
		t.Errorf("unreadableDirs: got %d, want 1", unreadable)
	}
	if len(paths) != 0 {
		t.Errorf("paths: got %v, want none", paths)
	}
}

// An unreadable workflows/ dir must not be mistaken for "this session ran no
// workflows" — the regular subagents beside it still resolve.
func TestDiscoverAgentSessions_UnreadableWorkflowsDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	workflowsDir := filepath.Join(subagentsDir, "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagentsDir, "agent-a.jsonl"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	makeUnreadableDir(t, workflowsDir)

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 1 {
		t.Errorf("unreadableDirs: got %d, want 1", unreadable)
	}
	if len(paths) != 1 || filepath.Base(paths[0]) != "agent-a.jsonl" {
		t.Errorf("paths: got %v, want the regular subagent", paths)
	}
}

// A single unreadable run dir counts once and leaves its siblings intact.
func TestDiscoverAgentSessions_UnreadableWorkflowRunDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	writeWorkflowRun(t, subagentsDir, "wf_bad", "agent-w1.jsonl")
	writeWorkflowRun(t, subagentsDir, "wf_ok", "agent-w2.jsonl")
	makeUnreadableDir(t, filepath.Join(subagentsDir, "workflows", "wf_bad"))

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 1 {
		t.Errorf("unreadableDirs: got %d, want 1", unreadable)
	}
	if len(paths) != 1 || filepath.Base(paths[0]) != "agent-w2.jsonl" {
		t.Errorf("paths: got %v, want only the readable run's agent", paths)
	}
}

// AGENT-01: a workflow run dir reached through a symlink (e.g. a bulky run
// relocated to another disk and linked back) must be discovered. fs.DirEntry's
// IsDir() is lstat-based and false for a symlink-to-dir, so without the os.Stat
// follow the run's agents would silently vanish from every cost surface.
func TestDiscoverAgentSessions_SymlinkedWorkflowRunDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	workflowsDir := filepath.Join(subagentsDir, "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// The real run dir lives outside the session tree; a symlink stands in for it
	// under workflows/.
	target := filepath.Join(tmpDir, "external-run")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "agent-w1.jsonl"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(workflowsDir, "wf_linked")); err != nil {
		t.Skipf("symlink creation not supported: %v", err)
	}

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 0 {
		t.Errorf("unreadableDirs: got %d, want 0", unreadable)
	}
	if len(paths) != 1 || filepath.Base(paths[0]) != "agent-w1.jsonl" {
		t.Errorf("paths: got %v, want the symlinked run's agent", paths)
	}
}

// AGENT-01: a broken symlink where a run dir might be hides a possible run, so
// it is disclosed via unreadableDirs (mapped to SkippedAgents upstream), not
// silently skipped like a stray file.
func TestDiscoverAgentSessions_BrokenSymlinkWorkflowRunDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	workflowsDir := filepath.Join(subagentsDir, "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(tmpDir, "does-not-exist"), filepath.Join(workflowsDir, "wf_broken")); err != nil {
		t.Skipf("symlink creation not supported: %v", err)
	}

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 1 {
		t.Errorf("unreadableDirs: got %d, want 1 (broken symlink disclosed)", unreadable)
	}
	if len(paths) != 0 {
		t.Errorf("paths: got %v, want none", paths)
	}
}

// A plain file (not a directory) in workflows/ is not a run dir and is skipped
// silently — no unreadable count, unlike a broken symlink.
func TestDiscoverAgentSessions_StrayFileInWorkflowsDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	workflowsDir := filepath.Join(subagentsDir, "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "stray.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 0 {
		t.Errorf("unreadableDirs: got %d, want 0 (stray file is not a run dir)", unreadable)
	}
	if len(paths) != 0 {
		t.Errorf("paths: got %v, want none", paths)
	}
}

// A symlink pointing at a plain file (not a directory) in workflows/ is not a
// run dir and is skipped silently — the target stats as a non-directory, so it
// is neither descended nor disclosed.
func TestDiscoverAgentSessions_SymlinkToFileInWorkflowsDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	workflowsDir := filepath.Join(tmpDir, sessionID, "subagents", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(tmpDir, "some-file")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(workflowsDir, "wf_file")); err != nil {
		t.Skipf("symlink creation not supported: %v", err)
	}

	paths, unreadable := DiscoverAgentSessions(tmpDir, sessionID)
	if unreadable != 0 {
		t.Errorf("unreadableDirs: got %d, want 0 (symlink→file is not a run dir)", unreadable)
	}
	if len(paths) != 0 {
		t.Errorf("paths: got %v, want none", paths)
	}
}

// A missing subagents/ dir is the common case and must stay silent.
func TestDiscoverAgentSessions_MissingDirIsNotUnreadable(t *testing.T) {
	tmpDir := t.TempDir()
	paths, unreadable := DiscoverAgentSessions(tmpDir, "no-such-session")
	if unreadable != 0 {
		t.Errorf("unreadableDirs: got %d, want 0", unreadable)
	}
	if len(paths) != 0 {
		t.Errorf("paths: got %v, want none", paths)
	}
}

// DEDUP-01: `list` counted lines the analysis parse rejects, because it decoded
// a laxer struct. A line is now counted iff the analysis would keep it.
func TestCountMessagesInFile_MatchesAnalysisParse(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.jsonl")
	// Line 2's timestamp and line 3's token count both fail the strict decode
	// of models.JSONLMessage; the old minimal struct accepted both.
	content := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","requestId":"r1","message":{"id":"m1","usage":{"input_tokens":10}}}
{"type":"assistant","timestamp":"not-a-time","requestId":"r2","message":{"id":"m2","usage":{"input_tokens":10}}}
{"type":"assistant","timestamp":"2024-01-15T10:02:00Z","requestId":"r3","message":{"id":"m3","usage":{"input_tokens":"10"}}}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	count, skipped := countMessagesInFile(testFile)
	if count != 1 {
		t.Errorf("count: got %d, want 1 (only the well-formed line)", count)
	}
	if skipped != 2 {
		t.Errorf("skippedLines: got %d, want 2", skipped)
	}

	result, err := ParseJSONLFileWithResult(testFile)
	if err != nil {
		t.Fatalf("ParseJSONLFileWithResult: %v", err)
	}
	if count != len(result.Messages) || skipped != result.SkippedLines {
		t.Errorf("list count (%d msgs, %d skipped) disagrees with analysis parse (%d msgs, %d skipped)",
			count, skipped, len(result.Messages), result.SkippedLines)
	}
}

// buildDiskEntry surfaces both skip sources `list` can see: an unreadable agent
// directory (found by discovery) and an unreadable agent file (found by the
// message-count scan).
func TestDiscoverSessionsFromDisk_SkippedAccounting(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	writeJSONL := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Parent: one good line, one malformed timestamp.
	writeJSONL(filepath.Join(tmpDir, sessionID+".jsonl"),
		`{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","requestId":"r1","message":{"id":"m1","usage":{"input_tokens":10}}}
{"type":"assistant","timestamp":"not-a-time","requestId":"r2","message":{"id":"m2","usage":{"input_tokens":10}}}
`)
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(filepath.Join(subagentsDir, "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	badAgent := filepath.Join(subagentsDir, "agent-bad.jsonl")
	writeJSONL(badAgent, "{}\n")
	if err := os.Chmod(badAgent, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badAgent, 0644) })
	if _, err := os.ReadFile(badAgent); err == nil {
		t.Skip("chmod 000 does not bar reads (running as root?)")
	}
	makeUnreadableDir(t, filepath.Join(subagentsDir, "workflows"))

	sessions, err := DiscoverSessionsFromDisk(tmpDir, true)
	if err != nil {
		t.Fatalf("DiscoverSessionsFromDisk: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions: got %d, want 1", len(sessions))
	}
	s := sessions[0]
	if s.MessageCount != 1 {
		t.Errorf("MessageCount: got %d, want 1", s.MessageCount)
	}
	if s.SkippedLines != 1 {
		t.Errorf("SkippedLines: got %d, want 1", s.SkippedLines)
	}
	// 1 unreadable workflows dir + 1 unreadable agent file
	if s.SkippedAgents != 2 {
		t.Errorf("SkippedAgents: got %d, want 2", s.SkippedAgents)
	}
	// The unreadable agent file is in SkippedAgents, so it must not also be in
	// AgentCount — `list` and `show` would otherwise report different totals,
	// and agent_count + skipped_agents would over-count what is on disk.
	if s.AgentCount != 0 {
		t.Errorf("AgentCount: got %d, want 0 (the only agent file was unreadable)", s.AgentCount)
	}
}

// A readable agent beside an unreadable one still counts, so AgentCount and
// SkippedAgents partition what discovery found.
func TestDiscoverSessionsFromDisk_AgentCountExcludesUnreadable(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "sess"
	line := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","requestId":"r1","message":{"id":"m1","usage":{"input_tokens":10}}}` + "\n"
	if err := os.WriteFile(filepath.Join(tmpDir, sessionID+".jsonl"), []byte(line), 0644); err != nil {
		t.Fatal(err)
	}
	subagentsDir := filepath.Join(tmpDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagentsDir, "agent-good.jsonl"), []byte(line), 0644); err != nil {
		t.Fatal(err)
	}
	badAgent := filepath.Join(subagentsDir, "agent-bad.jsonl")
	if err := os.WriteFile(badAgent, []byte(line), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(badAgent, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badAgent, 0644) })
	if _, err := os.ReadFile(badAgent); err == nil {
		t.Skip("chmod 000 does not bar reads (running as root?)")
	}

	sessions, err := DiscoverSessionsFromDisk(tmpDir, true)
	if err != nil {
		t.Fatalf("DiscoverSessionsFromDisk: %v", err)
	}
	s := sessions[0]
	if s.AgentCount != 1 || s.SkippedAgents != 1 {
		t.Errorf("AgentCount=%d SkippedAgents=%d, want 1/1", s.AgentCount, s.SkippedAgents)
	}
	if s.MessageCount != 2 {
		t.Errorf("MessageCount: got %d, want 2 (parent + the readable agent)", s.MessageCount)
	}
}

// An unreadable parent transcript must not read as an empty session: `list`
// shows a zero message count, and only SkippedSessions says why.
func TestDiscoverSessionsFromDisk_UnreadableParentIsCounted(t *testing.T) {
	tmpDir := t.TempDir()
	sessPath := filepath.Join(tmpDir, "sess.jsonl")
	line := `{"type":"assistant","timestamp":"2024-01-15T10:00:00Z","requestId":"r1","message":{"id":"m1","usage":{"input_tokens":10}}}` + "\n"
	if err := os.WriteFile(sessPath, []byte(line), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sessPath, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sessPath, 0644) })
	if _, err := os.ReadFile(sessPath); err == nil {
		t.Skip("chmod 000 does not bar reads (running as root?)")
	}

	sessions, err := DiscoverSessionsFromDisk(tmpDir, true)
	if err != nil {
		t.Fatalf("DiscoverSessionsFromDisk: %v", err)
	}
	s := sessions[0]
	if s.SkippedSessions != 1 {
		t.Errorf("SkippedSessions: got %d, want 1", s.SkippedSessions)
	}
	if s.MessageCount != 0 {
		t.Errorf("MessageCount: got %d, want 0", s.MessageCount)
	}
}

// readAgentDir is the single place discovery decides whether a directory hid
// something. A run dir that vanished between the parent listing and its read
// (the live TUIs rescan directories Claude Code is writing) hides nothing.
func TestReadAgentDir(t *testing.T) {
	tmpDir := t.TempDir()

	present := filepath.Join(tmpDir, "present")
	if err := os.MkdirAll(present, 0755); err != nil {
		t.Fatal(err)
	}
	if entries, unreadable := readAgentDir(present); unreadable != 0 || len(entries) != 0 {
		t.Errorf("readable empty dir: entries=%d unreadable=%d, want 0/0", len(entries), unreadable)
	}

	if entries, unreadable := readAgentDir(filepath.Join(tmpDir, "vanished")); unreadable != 0 || entries != nil {
		t.Errorf("missing dir: entries=%v unreadable=%d, want nil/0", entries, unreadable)
	}

	barred := filepath.Join(tmpDir, "barred")
	if err := os.MkdirAll(barred, 0755); err != nil {
		t.Fatal(err)
	}
	makeUnreadableDir(t, barred)
	if _, unreadable := readAgentDir(barred); unreadable != 1 {
		t.Errorf("unreadable dir: unreadable=%d, want 1", unreadable)
	}

	notADir := filepath.Join(tmpDir, "file")
	if err := os.WriteFile(notADir, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, unreadable := readAgentDir(notADir); unreadable != 1 {
		t.Errorf("ENOTDIR: unreadable=%d, want 1", unreadable)
	}
}
