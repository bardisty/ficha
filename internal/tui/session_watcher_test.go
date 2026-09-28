package tui

import (
	"path/filepath"
	"testing"
)

func TestUuidPattern(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		matches bool
	}{
		{"valid UUID.jsonl", "550e8400-e29b-41d4-a716-446655440000.jsonl", true},
		{"missing extension", "550e8400-e29b-41d4-a716-446655440000", false},
		{"wrong hex (G)", "550g8400-e29b-41d4-a716-446655440000.jsonl", false},
		{"empty string", "", false},
		{"too short", "abc.jsonl", false},
		{"uppercase hex", "550E8400-E29B-41D4-A716-446655440000.jsonl", false}, // pattern uses [0-9a-f] only
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uuidPattern.MatchString(tt.input)
			if got != tt.matches {
				t.Errorf("uuidPattern.MatchString(%q) = %v, want %v", tt.input, got, tt.matches)
			}
		})
	}
}

func TestNewSessionWatcher(t *testing.T) {
	sw := NewSessionWatcher("/tmp/test/../test/project", "session-123")
	// filepath.Clean should normalize the path
	expected := filepath.Clean("/tmp/test/../test/project")
	if sw.projectDir != expected {
		t.Errorf("projectDir = %q, want %q", sw.projectDir, expected)
	}
	if sw.currentSession != "session-123" {
		t.Errorf("currentSession = %q, want %q", sw.currentSession, "session-123")
	}
	if sw.done == nil {
		t.Error("done channel should be initialized")
	}
	if sw.restartCh == nil {
		t.Error("restartCh channel should be initialized")
	}
}

func TestHandleSessionFile(t *testing.T) {
	sw := NewSessionWatcher("/project/dir", "current-session-id-1234-5678-9012-3456")

	tests := []struct {
		name     string
		filePath string
		wantPath string
		wantID   string
	}{
		{
			"new session at project root",
			"/project/dir/550e8400-e29b-41d4-a716-446655440000.jsonl",
			"/project/dir/550e8400-e29b-41d4-a716-446655440000.jsonl",
			"550e8400-e29b-41d4-a716-446655440000",
		},
		{
			"current session (skip)",
			"/project/dir/current-session-id-1234-5678-9012-3456.jsonl",
			"",
			"",
		},
		{
			"subdirectory file (skip)",
			"/project/dir/subdir/550e8400-e29b-41d4-a716-446655440000.jsonl",
			"",
			"",
		},
		{
			"non-UUID file (skip)",
			"/project/dir/notes.jsonl",
			"",
			"",
		},
		{
			"non-jsonl file (skip)",
			"/project/dir/550e8400-e29b-41d4-a716-446655440000.txt",
			"",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotID := sw.handleSessionFile(tt.filePath)
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			if gotID != tt.wantID {
				t.Errorf("id = %q, want %q", gotID, tt.wantID)
			}
		})
	}
}

func TestSetCurrentSession(t *testing.T) {
	sw := NewSessionWatcher("/project/dir", "old-session")
	sw.SetCurrentSession("new-session")

	sw.sessionMu.RLock()
	got := sw.currentSession
	sw.sessionMu.RUnlock()

	if got != "new-session" {
		t.Errorf("currentSession = %q, want %q", got, "new-session")
	}
}
