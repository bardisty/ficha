package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	pollCurrent = "11111111-1111-1111-1111-111111111111"
	pollOther   = "22222222-2222-2222-2222-222222222222"
	pollNew     = "33333333-3333-3333-3333-333333333333"
	pollNewer   = "44444444-4444-4444-4444-444444444444"
)

// startSilent starts a session watcher on dir, then swaps its fsnotify
// watch for one that watches nothing, as fsnotify on WSL's 9p mounts
// behaves: started without error, and never an event.
func startSilent(t *testing.T, dir, current string) *SessionWatcher {
	t.Helper()
	sw := NewSessionWatcher(dir, current)
	if err := sw.Start(); err != nil {
		t.Fatal(err)
	}
	sw.watcher.Close()
	silent, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	sw.watcher = silent
	sw.pollEvery = 10 * time.Millisecond
	t.Cleanup(sw.Stop)
	return sw
}

func pollPath(dir, id string) string { return filepath.Join(dir, id+".jsonl") }

func writeSession(t *testing.T, dir, id string) {
	t.Helper()
	if err := os.WriteFile(pollPath(dir, id), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// waitEvent runs WaitForSessionEvent with a deadline.
func waitEvent(t *testing.T, sw *SessionWatcher) sessionEvent {
	t.Helper()
	got := make(chan sessionEvent, 1)
	go func() { got <- sw.WaitForSessionEvent() }()
	select {
	case ev := <-got:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("WaitForSessionEvent never returned")
		return sessionEvent{}
	}
}

// pollProject is a project with the current session and one other, both
// already seen by a first poll.
func pollProject(t *testing.T) (string, *SessionWatcher) {
	t.Helper()
	dir := t.TempDir()
	writeSession(t, dir, pollCurrent)
	writeSession(t, dir, pollOther)
	sw := startSilent(t, dir, pollCurrent)
	if ev := sw.poll(); ev.path != "" {
		t.Fatalf("first poll = %+v, want nothing", ev)
	}
	return dir, sw
}

// A new session file the watch never reported comes from the poll.
func TestPollReportsANewSessionASilentWatchMissed(t *testing.T) {
	dir, sw := pollProject(t)
	writeSession(t, dir, pollNew)
	ev := waitEvent(t, sw)
	if ev.id != pollNew || !ev.created {
		t.Fatalf("event = %+v, want %s created", ev, pollNew)
	}
	if ev := sw.poll(); ev.path != "" {
		t.Fatalf("next poll = %+v, want nothing", ev)
	}
}

// A write to another session is reported once, and a session with nothing
// new is never reported.
func TestPollReportsEachWriteOnce(t *testing.T) {
	dir, sw := pollProject(t)
	for range 3 {
		if ev := sw.poll(); ev.path != "" {
			t.Fatalf("quiet poll = %+v, want nothing", ev)
		}
	}
	growFile(t, pollPath(dir, pollOther))
	if ev := sw.poll(); ev.id != pollOther || ev.created {
		t.Fatalf("poll after a write = %+v, want activity in %s", ev, pollOther)
	}
	if ev := sw.poll(); ev.path != "" {
		t.Fatalf("poll after that = %+v, want nothing", ev)
	}
}

// The current session's writes are the view's own, and those the poll saw
// don't read as activity once the view leaves the session.
func TestPollIgnoresTheCurrentSession(t *testing.T) {
	dir, sw := pollProject(t)
	growFile(t, pollPath(dir, pollCurrent))
	if ev := sw.poll(); ev.path != "" {
		t.Fatalf("poll after a write to the current session = %+v, want nothing", ev)
	}
	sw.SetCurrentSession(pollOther)
	if ev := sw.poll(); ev.path != "" {
		t.Fatalf("poll after switching away = %+v, want nothing", ev)
	}
}

// The current session's writes since the last poll are the view's own too:
// on a silent mount no watch event records them, and a switch away mustn't
// turn them into a hint about the session just left. A write after the
// switch is news.
func TestPollIgnoresWritesBeforeASwitch(t *testing.T) {
	dir, sw := pollProject(t)
	growFile(t, pollPath(dir, pollCurrent))
	sw.SetCurrentSession(pollOther)
	if ev := sw.poll(); ev.path != "" {
		t.Fatalf("poll after switching away = %+v, want nothing", ev)
	}
	growFile(t, pollPath(dir, pollCurrent))
	if ev := sw.poll(); ev.id != pollCurrent || ev.created {
		t.Fatalf("poll after a write to the session left = %+v, want activity in %s", ev, pollCurrent)
	}
}

// With a working watch, what fsnotify reported doesn't come back from the
// poll.
func TestPollSkipsWhatTheWatchReported(t *testing.T) {
	dir, sw := pollProject(t)

	writeSession(t, dir, pollNew)
	if ev := sw.handleSessionFile(pollPath(dir, pollNew), fsnotify.Create); !ev.created {
		t.Fatalf("Create = %+v, want created", ev)
	}
	growFile(t, pollPath(dir, pollOther))
	if ev := sw.handleSessionFile(pollPath(dir, pollOther), fsnotify.Write); ev.id != pollOther {
		t.Fatalf("Write = %+v, want activity in %s", ev, pollOther)
	}
	if ev := sw.poll(); ev.path != "" {
		t.Fatalf("poll = %+v, want nothing", ev)
	}
}

// Between full scans the poll stats only recently active sessions, so a
// write to one idle longer than idleAfter waits for the next full scan.
func TestPollStatsIdleSessionsOnlyOnFullScans(t *testing.T) {
	dir, sw := pollProject(t)
	path := pollPath(dir, pollOther)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	// A full scan sees the backdating, then the session is idle.
	sw.polls = 0
	sw.poll()

	growFile(t, path)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	for range fullScanEvery - 1 {
		if ev := sw.poll(); ev.path != "" {
			t.Fatalf("poll between full scans = %+v, want nothing", ev)
		}
	}
	if ev := sw.poll(); ev.id != pollOther {
		t.Fatalf("full scan = %+v, want activity in %s", ev, pollOther)
	}
}

// A project Claude Code hadn't run in when the view started gets its
// directory later. When the watch never reports it, the poll adopts it: its
// first session is new, and the rest are known rather than reported as new
// one poll at a time.
func TestPollAdoptsAProjectDirectoryTheWatchMissed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project")
	sw := startSilent(t, dir, "")
	if !sw.dirPending {
		t.Fatal("Start didn't note the missing directory")
	}
	if ev := sw.poll(); ev.path != "" {
		t.Fatalf("poll before the directory exists = %+v, want nothing", ev)
	}

	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSession(t, dir, pollNew)
	writeSession(t, dir, pollNewer)
	ev := waitEvent(t, sw)
	if !ev.created || (ev.id != pollNew && ev.id != pollNewer) {
		t.Fatalf("event = %+v, want one of the new sessions, created", ev)
	}
	sw.SetCurrentSession(ev.id)
	if ev := sw.poll(); ev.created {
		t.Fatalf("next poll = %+v, want the other session known", ev)
	}
}

// The poll's timer keeps WaitForSessionEvent waiting on shutdown and on a
// session switch like before.
func TestPollingWaiterExitsOnStopAndRestart(t *testing.T) {
	_, sw := pollProject(t)
	restarted := make(chan sessionEvent, 1)
	go func() { restarted <- sw.WaitForSessionEvent() }()
	time.Sleep(50 * time.Millisecond)
	sw.SetCurrentSession(pollOther)
	select {
	case ev := <-restarted:
		if ev.path != sessionRestartedPath {
			t.Fatalf("after a switch = %+v, want a restart", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter didn't restart")
	}

	stopped := make(chan sessionEvent, 1)
	go func() { stopped <- sw.WaitForSessionEvent() }()
	time.Sleep(50 * time.Millisecond)
	sw.Stop()
	select {
	case ev := <-stopped:
		if ev.path != "" {
			t.Fatalf("after Stop = %+v, want nothing", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter didn't exit on Stop")
	}
}

// A session writing on every poll doesn't keep the poll from finding a new
// session, or from finding a write to an idle one on a full scan.
func TestPollSeesPastABusySession(t *testing.T) {
	dir, sw := pollProject(t)
	idle := pollPath(dir, pollNewer)
	writeSession(t, dir, pollNewer)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(idle, old, old); err != nil {
		t.Fatal(err)
	}
	growFile(t, pollPath(dir, pollOther))
	if ev := sw.poll(); ev.id != pollNewer || !ev.created {
		t.Fatalf("poll = %+v, want %s created", ev, pollNewer)
	}

	writeSession(t, dir, pollNew)
	growFile(t, pollPath(dir, pollOther))
	if ev := sw.poll(); ev.id != pollNew || !ev.created {
		t.Fatalf("poll with a busy session = %+v, want %s created", ev, pollNew)
	}

	growFile(t, idle)
	if err := os.Chtimes(idle, old, old); err != nil {
		t.Fatal(err)
	}
	sw.polls = 0
	growFile(t, pollPath(dir, pollOther))
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(pollPath(dir, pollOther), later, later); err != nil {
		t.Fatal(err)
	}
	if ev := sw.poll(); ev.id != pollOther {
		t.Fatalf("full scan = %+v, want the most recent write, in %s", ev, pollOther)
	}
	sw.sessionMu.RLock()
	got := sw.sigs[pollNewer]
	sw.sessionMu.RUnlock()
	if info, err := os.Stat(idle); err != nil || got.size != info.Size() {
		t.Fatalf("full scan stopped before the idle session: recorded size %d", got.size)
	}
}
