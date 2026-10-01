package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// startWatching opens a view on a fixture session with a real watcher
// installed, the way Init leaves it once the watcher is up.
func startWatching(t *testing.T, h fallbackHarness) (tea.Model, string) {
	t.Helper()
	sessionPath, projectDir, sessionID, _ := watchFixture(t)
	m := h.open(sessionPath, projectDir, sessionID)
	m, _ = m.Update(watcherStartedMsg{watcher: newTestWatcher(t, sessionPath)})
	m, _ = m.Update(h.loaded)
	return m, sessionPath
}

// A watcher can start without error and never deliver an event, as fsnotify
// does on WSL's 9p mounts. The poll catches the write, and the view doesn't
// call it a watcher failure.
func TestPollCatchesWritesASilentWatcherMissed(t *testing.T) {
	for _, h := range fallbackHarnesses {
		t.Run(h.name, func(t *testing.T) {
			m, sessionPath := startWatching(t, h)

			m, _ = pollOnce(t, m)
			if h.loading(m) {
				t.Fatal("poll reloaded an unchanged file")
			}

			growFile(t, sessionPath)
			m, _ = pollOnce(t, m)
			if !h.loading(m) {
				t.Fatal("poll missed a write the watcher never reported")
			}
			if h.fallback(m).active() {
				t.Fatal("a silent watcher turned the fallback on")
			}
			if got := h.notify(m); strings.Contains(got, unavailableText) {
				t.Fatalf("notify row = %q, want no watcher warning", got)
			}
			m, _ = m.Update(h.loaded)

			m, _ = pollOnce(t, m)
			if h.loading(m) {
				t.Fatal("poll reloaded again for a write it already caught")
			}
		})
	}
}

// With a working watcher every write already reloads; the poll must not
// reload for the same write a second time.
func TestPollDoesNotRepeatAWatcherReload(t *testing.T) {
	for _, h := range fallbackHarnesses {
		t.Run(h.name, func(t *testing.T) {
			m, sessionPath := startWatching(t, h)

			growFile(t, sessionPath)
			m, _ = m.Update(fileChangedMsg{watcher: h.watcher(m)})
			if !h.loading(m) {
				t.Fatal("watcher event did not reload")
			}
			m, _ = m.Update(h.loaded)
			m, _ = pollOnce(t, m)
			if h.loading(m) {
				t.Fatal("poll reloaded after a watcher reload of the same write")
			}
		})
	}
}

// A write that lands while a load parses may or may not be in it. When the
// watcher never reports it, the next poll reloads for it, and the poll after
// that doesn't.
func TestPollReloadsOnceForAWriteDuringALoad(t *testing.T) {
	for _, h := range fallbackHarnesses {
		t.Run(h.name, func(t *testing.T) {
			m, sessionPath := startWatching(t, h)

			growFile(t, sessionPath)
			m, _ = m.Update(fileChangedMsg{watcher: h.watcher(m)})
			growFile(t, sessionPath)
			m, _ = m.Update(h.loaded)

			m, _ = pollOnce(t, m)
			if !h.loading(m) {
				t.Fatal("poll missed a write made during the load")
			}
			m, _ = m.Update(h.loaded)
			m, _ = pollOnce(t, m)
			if h.loading(m) {
				t.Fatal("poll reloaded twice for one write")
			}
		})
	}
}
