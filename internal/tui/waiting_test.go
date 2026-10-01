package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// A waiting view says where it waits, and the first session to appear
// replaces it, pinned or not.
func TestWaitingModel(t *testing.T) {
	for _, follow := range []bool{true, false} {
		m := NewWaitingModel("/cfg/projects/-w-webapp", "~/work/webapp", true, follow)
		m = sized(t, m, 80, 24)
		view := m.View()
		for _, want := range []string{"webapp │ ● ", "waiting for a session", "Waiting for a Claude Code session in ~/work/webapp…"} {
			if !strings.Contains(view, want) {
				t.Errorf("follow=%v: waiting view missing %q:\n%s", follow, want, view)
			}
		}

		for _, k := range []string{"r", "j", "n", "-"} {
			if m = key(t, m, k); !m.waiting() || m.err != nil || m.loading {
				t.Fatalf("follow=%v: %q left the wait (waiting=%v err=%v loading=%v)", follow, k, m.waiting(), m.err, m.loading)
			}
		}

		m = send(t, m, sessionActivityMsg{path: "/cfg/projects/-w-webapp/" + sessA + ".jsonl", id: sessA})
		if m.sessionID != sessA {
			t.Fatalf("follow=%v: first session didn't replace the wait", follow)
		}
		if strings.Contains(m.View(), "- to go back") {
			t.Errorf("follow=%v: offers going back to nothing:\n%s", follow, m.View())
		}
	}
}

// The session watcher can start before the project directory exists and
// picks up the first session once Claude Code creates it.
func TestSessionWatcherAdoptsNewProjectDir(t *testing.T) {
	projects := t.TempDir()
	projectDir := filepath.Join(projects, "-w-webapp")
	sw := NewSessionWatcher(projectDir, "")
	if err := sw.Start(); err != nil {
		t.Fatalf("Start on a missing project dir: %v", err)
	}
	defer sw.Stop()

	got := make(chan sessionEvent, 1)
	go func() { got <- sw.WaitForSessionEvent() }()
	time.Sleep(50 * time.Millisecond)

	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "550e8400-e29b-41d4-a716-446655440000"
	if err := os.WriteFile(filepath.Join(projectDir, id+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-got:
		if ev.id != id || !ev.created {
			t.Errorf("event = %+v, want a created %s", ev, id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event for a session in a project dir created after start")
	}
}

// If the session watcher can't start, the waiting view says so and r
// retries it, rather than offering a retry that does nothing.
func TestWaitingWatcherFailureRetries(t *testing.T) {
	m := NewWaitingModel(t.TempDir()+"/-w-webapp", "~/work/webapp", true, true)
	m = sized(t, m, 80, 24)
	m = send(t, m, errorMsg{err: errors.New("too many open files")})
	if !strings.Contains(m.View(), "too many open files") {
		t.Fatalf("watcher failure not shown:\n%s", m.View())
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)
	if cmd == nil || m.err != nil {
		t.Fatal("r didn't retry the session watcher")
	}
	started, ok := cmd().(sessionWatcherStartedMsg)
	if !ok {
		t.Fatalf("retry produced %T, want sessionWatcherStartedMsg", cmd())
	}
	started.watcher.Stop()
}

// A session that lands between the decision to wait and the watcher's start
// is taken as soon as the watcher is up.
func TestWaitingTakesSessionCreatedBeforeWatcher(t *testing.T) {
	dir := t.TempDir()
	id := "550e8400-e29b-41d4-a716-446655440000"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sw := NewSessionWatcher(dir, "")
	if err := sw.Start(); err != nil {
		t.Fatal(err)
	}
	defer sw.Stop()
	m := NewWaitingModel(dir, "~/w", true, true)
	m = send(t, m, sessionWatcherStartedMsg{watcher: sw})
	if m.sessionID != id {
		t.Errorf("session on disk at start not taken: sessionID = %q", m.sessionID)
	}
}
