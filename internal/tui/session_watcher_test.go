package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
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
	const (
		current = "current-session-id-1234-5678-9012-3456"
		other   = "550e8400-e29b-41d4-a716-446655440000"
		known   = "660e8400-e29b-41d4-a716-446655440000"
	)
	tests := []struct {
		name        string
		filePath    string
		op          fsnotify.Op
		wantID      string
		wantCreated bool
	}{
		{"new session at project root", "/project/dir/" + other + ".jsonl", fsnotify.Create, other, true},
		{"write to another existing session", "/project/dir/" + known + ".jsonl", fsnotify.Write, known, false},
		// An atomic replace shows up as a Create of a name already there
		{"create of a known session", "/project/dir/" + known + ".jsonl", fsnotify.Create, known, false},
		{"current session (skip)", "/project/dir/" + current + ".jsonl", fsnotify.Write, "", false},
		{"subdirectory file (skip)", "/project/dir/subdir/" + other + ".jsonl", fsnotify.Create, "", false},
		{"non-UUID file (skip)", "/project/dir/notes.jsonl", fsnotify.Create, "", false},
		{"non-jsonl file (skip)", "/project/dir/" + other + ".txt", fsnotify.Create, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sw := NewSessionWatcher("/project/dir", current)
			sw.known[known] = true
			ev := sw.handleSessionFile(tt.filePath, tt.op)
			if ev.id != tt.wantID || ev.created != tt.wantCreated {
				t.Errorf("got id=%q created=%v, want id=%q created=%v", ev.id, ev.created, tt.wantID, tt.wantCreated)
			}
			if tt.wantID != "" && ev.path != tt.filePath {
				t.Errorf("path = %q, want %q", ev.path, tt.filePath)
			}
		})
	}
}

// Only the first Create of a session is new: once seen, later events for it
// are activity, so a session can't re-trigger a follow switch.
func TestHandleSessionFileCreateOnce(t *testing.T) {
	sw := NewSessionWatcher("/project/dir", "cur")
	path := "/project/dir/550e8400-e29b-41d4-a716-446655440000.jsonl"
	if ev := sw.handleSessionFile(path, fsnotify.Create); !ev.created {
		t.Fatal("first Create: want created")
	}
	if ev := sw.handleSessionFile(path, fsnotify.Create); ev.created {
		t.Error("second Create: want activity, got created")
	}
}

// Start snapshots the sessions already on disk, so writes to them never
// count as new sessions.
func TestSessionWatcherStartKnowsExistingSessions(t *testing.T) {
	dir := t.TempDir()
	existing := "550e8400-e29b-41d4-a716-446655440000"
	if err := os.WriteFile(filepath.Join(dir, existing+".jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	sw := NewSessionWatcher(dir, "cur")
	if err := sw.Start(); err != nil {
		t.Fatal(err)
	}
	defer sw.Stop()
	if ev := sw.handleSessionFile(filepath.Join(dir, existing+".jsonl"), fsnotify.Create); ev.created {
		t.Error("existing session reported as created")
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
