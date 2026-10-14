package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// A waiting view says where it waits, and the first session to appear
// replaces it, pinned or not.
func TestWaitingModel(t *testing.T) {
	for _, follow := range []bool{true, false} {
		m := NewWaitingModel("/cfg/projects/-w-webapp", "~/work/webapp", nil, true, follow)
		m = sized(t, m, 80, 24)
		view := frameOf(m)
		for _, want := range []string{"webapp │ ● ", "waiting for a session", "Waiting for a Claude Code session in ~/work/webapp…"} {
			if !strings.Contains(view, want) {
				t.Errorf("follow=%v: waiting view missing %q:\n%s", follow, want, view)
			}
		}

		for _, k := range []string{"r", "j", "n", "-"} {
			if m = press(t, m, k); !m.waiting() || m.err != nil || m.loading {
				t.Fatalf("follow=%v: %q left the wait (waiting=%v err=%v loading=%v)", follow, k, m.waiting(), m.err, m.loading)
			}
		}

		m = send(t, m, sessionActivityMsg{path: "/cfg/projects/-w-webapp/" + sessA + ".jsonl", id: sessA})
		if m.sessionID != sessA {
			t.Fatalf("follow=%v: first session didn't replace the wait", follow)
		}
		if strings.Contains(frameOf(m), "- to go back") {
			t.Errorf("follow=%v: offers going back to nothing:\n%s", follow, frameOf(m))
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

// If the session watcher can't use fsnotify, the waiting view says so and r
// retries it, rather than offering a retry that does nothing.
func TestWaitingWatcherFailureRetries(t *testing.T) {
	failSessionWatch(t)
	m := NewWaitingModel(t.TempDir()+"/-w-webapp", "~/work/webapp", nil, true, true)
	m = sized(t, m, 80, 24)
	m = send(t, m, m.startSessionWatcher()())
	defer m.sessionWatcher.Stop()
	if !strings.Contains(frameOf(m), unavailableText) {
		t.Fatalf("watcher failure not shown:\n%s", frameOf(m))
	}
	_, cmd := m.Update(keyMsg(t, "r"))
	if cmd == nil {
		t.Fatal("r didn't retry the session watcher")
	}
	cmd()
	if _, ok := m.waitForNewSession()().(sessionWatcherStartedMsg); !ok {
		t.Fatal("r's retry wasn't made and reported")
	}
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
	m := NewWaitingModel(dir, "~/w", nil, true, true)
	m = send(t, m, sessionWatcherStartedMsg{watcher: sw})
	if m.sessionID != id {
		t.Errorf("session on disk at start not taken: sessionID = %q", m.sessionID)
	}
}

// The hint under the waiting message takes the longest form that fits and is
// dropped when none does: wrapped, it would push the frame out of shape.
func TestWaitingHintFits(t *testing.T) {
	long := "~/work/webapp has sessions: ficha watch -p ~/work/webapp"
	short := "~/work/webapp has sessions"
	hint := []string{long, short}
	// The last case's one form is wider than a 40-column panel.
	for _, tt := range []struct {
		w, h int
		want string
	}{
		{80, 24, long},
		{40, 15, short},
		{40, 15, ""},
	} {
		hint := hint
		if tt.want == "" {
			hint = []string{"~/projects/clients/acme/webapp has sessions"}
		}
		watch := sized(t, NewWaitingModel("/cfg/projects/-c-webapp", "~/clone/webapp", hint, true, true), tt.w, tt.h)
		updated, _ := NewWaitingBreakdownModel("/cfg/projects/-c-webapp", "~/clone/webapp", hint, true, true).Update(tea.WindowSizeMsg{Width: tt.w, Height: tt.h})
		for name, frame := range map[string]string{"watch": frameOf(watch), "breakdown": frameOf(updated)} {
			if frameHeight := strings.Count(frame, "\n") + 1; frameHeight > tt.h {
				t.Errorf("%s %dx%d: frame is %d rows", name, tt.w, tt.h, frameHeight)
			}
			for _, line := range strings.Split(frame, "\n") {
				if lipgloss.Width(line) > tt.w {
					t.Errorf("%s %dx%d: line wider than the terminal: %q", name, tt.w, tt.h, line)
				}
			}
			switch {
			case tt.want == "" && strings.Contains(frame, "has sessions"):
				t.Errorf("%s %dx%d: hint shown with no room:\n%s", name, tt.w, tt.h, frame)
			case tt.want != "" && !containsLine(frame, tt.want):
				t.Errorf("%s %dx%d: want the line %q:\n%s", name, tt.w, tt.h, tt.want, frame)
			}
		}
	}
}

// containsLine reports whether a line of frame, trimmed, is exactly want.
func containsLine(frame, want string) bool {
	for _, line := range strings.Split(frame, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
