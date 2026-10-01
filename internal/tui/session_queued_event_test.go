package tui

import (
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// hintAfter delivers ev to a view pinned to current, the way the session
// waiter would, and returns its notify row.
func hintAfter(t *testing.T, sw *SessionWatcher, dir, current string, ev sessionEvent) string {
	t.Helper()
	m := NewModel(pollPath(dir, current), current, true, "", false)
	m.sessionWatcher = sw
	if ev.path == "" {
		return m.renderNotifyRow(80)
	}
	updated, _ := m.Update(sessionActivityMsg(ev))
	return updated.(Model).renderNotifyRow(80)
}

// The watch can hand over a write to the current session after the view has
// left it: the event was queued when the switch happened. That write was the
// view's own, so it must not come back as a hint about the session just
// left. A write made after the switch is news, and so is every one after it.
func TestWatchEventQueuedBeforeASwitchIsNotNews(t *testing.T) {
	dir, sw := pollProject(t)
	left := pollPath(dir, pollCurrent)

	growFile(t, left)
	sw.SetCurrentSession(pollOther)
	ev := sw.handleSessionFile(left, fsnotify.Write)
	if row := hintAfter(t, sw, dir, pollOther, ev); strings.Contains(row, "newer activity in") {
		t.Fatalf("a write from before the switch hinted about the session just left: %q", row)
	}
	if ev.path != "" {
		t.Fatalf("event queued before the switch = %+v, want nothing", ev)
	}

	for range 2 {
		growFile(t, left)
		ev = sw.handleSessionFile(left, fsnotify.Write)
		if ev.id != pollCurrent || ev.created {
			t.Fatalf("a write after the switch = %+v, want activity in %s", ev, pollCurrent)
		}
		if row := hintAfter(t, sw, dir, pollOther, ev); !strings.Contains(row, "newer activity in 11111111") {
			t.Fatalf("a write after the switch gave no hint: %q", row)
		}
	}
}

// Two switches in a row leave two sessions with events that may still be
// queued; neither is news.
func TestWatchEventsQueuedBeforeTwoSwitchesAreNotNews(t *testing.T) {
	dir, sw := pollProject(t)
	writeSession(t, dir, pollNew)

	growFile(t, pollPath(dir, pollCurrent))
	sw.SetCurrentSession(pollOther)
	growFile(t, pollPath(dir, pollOther))
	sw.SetCurrentSession(pollNew)

	for _, id := range []string{pollCurrent, pollOther} {
		if ev := sw.handleSessionFile(pollPath(dir, id), fsnotify.Write); ev.path != "" {
			t.Errorf("event for %s queued before the switches = %+v, want nothing", id, ev)
		}
	}
}

// The same through a real watch: the event for the session just left is in
// the watcher's queue ahead of another session's write, and the waiter
// reports only the other session.
func TestWaiterSkipsTheLeftSessionsQueuedWrite(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{pollCurrent, pollOther, pollNew} {
		writeSession(t, dir, id)
	}
	sw := NewSessionWatcher(dir, pollCurrent)
	if err := sw.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sw.Stop)

	growFile(t, pollPath(dir, pollCurrent))
	sw.SetCurrentSession(pollOther)
	growFile(t, pollPath(dir, pollNew))

	ev := waitEvent(t, sw)
	for ev.path == sessionRestartedPath {
		ev = waitEvent(t, sw)
	}
	if ev.id != pollNew {
		t.Fatalf("first event after the switch = %+v, want activity in %s", ev, pollNew)
	}
}
