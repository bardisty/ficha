package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ccusage/internal/models"
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

	count := countMessagesInFile(testFile)
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

func TestPathWithinDir(t *testing.T) {
	tests := []struct {
		name string
		path string
		dir  string
		want bool
	}{
		{"file inside dir", "/proj/session.jsonl", "/proj", true},
		{"file in subdir", "/proj/sub/agent.jsonl", "/proj", true},
		{"file outside dir", "/other/session.jsonl", "/proj", false},
		{"traversal escape", "/proj/../other/session.jsonl", "/proj", false},
		{"sibling with shared prefix", "/proj-evil/session.jsonl", "/proj", false},
		{"path equals dir", "/proj", "/proj", false},
		{"empty path", "", "/proj", false},
		{"empty dir", "/proj/session.jsonl", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathWithinDir(tt.path, tt.dir); got != tt.want {
				t.Errorf("pathWithinDir(%q, %q) = %v, want %v", tt.path, tt.dir, got, tt.want)
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

	count := countMessagesInFile(testFile)
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

	count := countMessagesInFile(testFile)
	if count != 4 {
		t.Errorf("expected 4 (2 distinct ids + 2 id-less lines), got %d", count)
	}
}
