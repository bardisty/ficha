package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// The file-change waiter lifecycle invariant: at most one waiter
// blocks on the watcher at a time. A reload's completion message re-arms only
// when the waiter actually exited (fileChangedMsg / fileWatchErrMsg); reloads
// triggered by the subagent poll, manual refresh, or session switch land in
// the same completion handlers and must NOT stack extra waiters. The arm is
// observable without instrumentation: the handlers return a non-nil command
// exactly when a waiter was armed.

// watchFixture creates a session file plus a subagents tree whose agent file
// can be grown to flip subagentTreeSignature between poll ticks.
func watchFixture(t *testing.T) (sessionPath, projectDir, sessionID, agentPath string) {
	t.Helper()
	projectDir = t.TempDir()
	sessionID = "sess-waiter"
	sessionPath = filepath.Join(projectDir, sessionID+".jsonl")
	writeSessionFile(t, sessionPath)

	subagentsDir := filepath.Join(projectDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentPath = filepath.Join(subagentsDir, "agent-a1.jsonl")
	if err := os.WriteFile(agentPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return sessionPath, projectDir, sessionID, agentPath
}

func growFile(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func newTestWatcher(t *testing.T, sessionPath string) *fsnotify.Watcher {
	t.Helper()
	watcher, err := newSessionFileWatcher(sessionPath)
	if err != nil {
		t.Fatalf("newSessionFileWatcher: %v", err)
	}
	t.Cleanup(func() { watcher.Close() })
	return watcher
}

func TestWatchPollReloadsDoNotArmExtraWaiters(t *testing.T) {
	sessionPath, projectDir, sessionID, agentPath := watchFixture(t)
	m := NewModel(sessionPath, sessionID, true, projectDir, false)

	// Watcher creation arms the one long-lived waiter
	mm, cmd := m.Update(watcherStartedMsg{watcher: newTestWatcher(t, sessionPath)})
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("watcherStartedMsg did not arm the file-change waiter")
	}

	// N poll-triggered reload cycles: grow the agent file so the signature
	// flips, deliver the poll tick, then the reload completion. The waiter is
	// still blocked on the watcher, so no cycle may arm another.
	for i := range 10 {
		growFile(t, agentPath)
		mm, cmd = pollOnce(t, m)
		m = mm.(Model)
		if cmd == nil {
			t.Fatalf("cycle %d: poll tick with changed signature did not trigger a reload", i)
		}
		if !m.loading {
			t.Fatalf("cycle %d: poll-triggered reload did not set loading", i)
		}
		mm, cmd = m.Update(analysisMsg{})
		m = mm.(Model)
		if cmd != nil {
			t.Fatalf("cycle %d: poll-triggered reload completion armed an extra waiter", i)
		}
	}

	// A failed reload must not arm one either
	mm, cmd = m.Update(errorMsg{err: errors.New("parse failed")})
	m = mm.(Model)
	if cmd != nil {
		t.Fatal("reload error armed an extra waiter")
	}

	// A file-triggered reload means the waiter exited: exactly one re-arm on
	// its completion, and only one
	mm, _ = m.Update(fileChangedMsg{watcher: m.watcher})
	m = mm.(Model)
	mm, cmd = m.Update(analysisMsg{})
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("file-triggered reload completion did not re-arm the waiter")
	}
	mm, cmd = m.Update(analysisMsg{})
	m = mm.(Model)
	if cmd != nil {
		t.Fatal("second completion after re-arm armed an extra waiter")
	}

	// A waiter error also means the waiter exited: re-arm a replacement
	mm, cmd = m.Update(fileWatchErrMsg{err: errors.New("watch failed"), watcher: m.watcher})
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("waiter error did not arm a replacement waiter")
	}
	if m.err == nil {
		t.Fatal("waiter error was not surfaced on the model")
	}
	if _, cmd = m.Update(analysisMsg{}); cmd != nil {
		t.Fatal("completion after waiter-error re-arm armed an extra waiter")
	}
}

func TestBreakdownPollReloadsDoNotArmExtraWaiters(t *testing.T) {
	sessionPath, projectDir, sessionID, agentPath := watchFixture(t)
	m := NewBreakdownModel(sessionPath, sessionID, true, projectDir, false)

	mm, cmd := m.Update(watcherStartedMsg{watcher: newTestWatcher(t, sessionPath)})
	m = mm.(BreakdownModel)
	if cmd == nil {
		t.Fatal("watcherStartedMsg did not arm the file-change waiter")
	}

	for i := range 10 {
		growFile(t, agentPath)
		mm, cmd = pollOnce(t, m)
		m = mm.(BreakdownModel)
		if cmd == nil {
			t.Fatalf("cycle %d: poll tick with changed signature did not trigger a reload", i)
		}
		mm, cmd = m.Update(breakdownMsgsMsg{})
		m = mm.(BreakdownModel)
		if cmd != nil {
			t.Fatalf("cycle %d: poll-triggered reload completion armed an extra waiter", i)
		}
	}

	mm, cmd = m.Update(breakdownErrorMsg{err: errors.New("parse failed")})
	m = mm.(BreakdownModel)
	if cmd != nil {
		t.Fatal("reload error armed an extra waiter")
	}

	mm, _ = m.Update(fileChangedMsg{watcher: m.watcher})
	m = mm.(BreakdownModel)
	mm, cmd = m.Update(breakdownMsgsMsg{})
	m = mm.(BreakdownModel)
	if cmd == nil {
		t.Fatal("file-triggered reload completion did not re-arm the waiter")
	}
	mm, cmd = m.Update(breakdownMsgsMsg{})
	m = mm.(BreakdownModel)
	if cmd != nil {
		t.Fatal("second completion after re-arm armed an extra waiter")
	}

	mm, cmd = m.Update(fileWatchErrMsg{err: errors.New("watch failed"), watcher: m.watcher})
	m = mm.(BreakdownModel)
	if cmd == nil {
		t.Fatal("waiter error did not arm a replacement waiter")
	}
	if m.err == nil {
		t.Fatal("waiter error was not surfaced on the model")
	}
	if _, cmd = m.Update(breakdownMsgsMsg{}); cmd != nil {
		t.Fatal("completion after waiter-error re-arm armed an extra waiter")
	}
}

// runArmedWaiters executes any non-nil commands so their goroutines really
// block on the watcher; quit must drain them via wg.Wait.
func runArmedWaiter(cmd tea.Cmd) {
	if cmd != nil {
		go cmd()
	}
}

func TestWatchQuitCompletesAfterMixedReloads(t *testing.T) {
	sessionPath, projectDir, sessionID, agentPath := watchFixture(t)
	m := NewModel(sessionPath, sessionID, true, projectDir, false)

	mm, cmd := m.Update(watcherStartedMsg{watcher: newTestWatcher(t, sessionPath)})
	m = mm.(Model)
	runArmedWaiter(cmd)

	// Mixed reload history: poll-triggered completions...
	for range 5 {
		growFile(t, agentPath)
		mm, _ = pollOnce(t, m)
		m = mm.(Model)
		mm, cmd = m.Update(analysisMsg{})
		m = mm.(Model)
		runArmedWaiter(cmd)
	}
	// ...and a file-triggered cycle
	mm, _ = m.Update(fileChangedMsg{watcher: m.watcher})
	m = mm.(Model)
	mm, cmd = m.Update(analysisMsg{})
	m = mm.(Model)
	runArmedWaiter(cmd)

	quitDone := make(chan struct{})
	go func() {
		m.Update(keyMsg(t, "q"))
		close(quitDone)
	}()
	select {
	case <-quitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("quit hung in wg.Wait after mixed poll+file reloads")
	}
}

func TestBreakdownQuitCompletesAfterMixedReloads(t *testing.T) {
	sessionPath, projectDir, sessionID, agentPath := watchFixture(t)
	m := NewBreakdownModel(sessionPath, sessionID, true, projectDir, false)

	mm, cmd := m.Update(watcherStartedMsg{watcher: newTestWatcher(t, sessionPath)})
	m = mm.(BreakdownModel)
	runArmedWaiter(cmd)

	for range 5 {
		growFile(t, agentPath)
		mm, _ = pollOnce(t, m)
		m = mm.(BreakdownModel)
		mm, cmd = m.Update(breakdownMsgsMsg{})
		m = mm.(BreakdownModel)
		runArmedWaiter(cmd)
	}
	mm, _ = m.Update(fileChangedMsg{watcher: m.watcher})
	m = mm.(BreakdownModel)
	mm, cmd = m.Update(breakdownMsgsMsg{})
	m = mm.(BreakdownModel)
	runArmedWaiter(cmd)

	quitDone := make(chan struct{})
	go func() {
		m.Update(keyMsg(t, "q"))
		close(quitDone)
	}()
	select {
	case <-quitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("quit hung in wg.Wait after mixed poll+file reloads")
	}
}

func TestWatchSessionSwitchResetsWaiterAccounting(t *testing.T) {
	sessionPath, projectDir, sessionID, agentPath := watchFixture(t)
	m := NewModel(sessionPath, sessionID, true, projectDir, true)

	mm, cmd := m.Update(watcherStartedMsg{watcher: newTestWatcher(t, sessionPath)})
	m = mm.(Model)
	runArmedWaiter(cmd)

	// Leave a poll-triggered reload completed so the flag machinery has state
	growFile(t, agentPath)
	mm, _ = pollOnce(t, m)
	m = mm.(Model)
	mm, _ = m.Update(analysisMsg{})
	m = mm.(Model)

	// Switch sessions: the old watcher closes (its waiter exits silently), so
	// the switch must reset the accounting for the new watcher's waiter
	newPath := filepath.Join(projectDir, "sess-2.jsonl")
	writeSessionFile(t, newPath)
	mm, _ = m.Update(sessionActivityMsg{path: newPath, id: "sess-2", created: true})
	m = mm.(Model)
	if m.fileWaiterActive {
		t.Fatal("session switch did not reset fileWaiterActive")
	}

	mm, cmd = m.Update(watcherStartedMsg{watcher: newTestWatcher(t, newPath)})
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("new watcher after session switch did not arm a waiter")
	}
	if _, c := m.Update(analysisMsg{}); c != nil {
		t.Fatal("completion after switch re-arm armed an extra waiter")
	}
}

func TestBreakdownSessionSwitchResetsWaiterAccounting(t *testing.T) {
	sessionPath, projectDir, sessionID, _ := watchFixture(t)
	m := NewBreakdownModel(sessionPath, sessionID, true, projectDir, true)

	mm, cmd := m.Update(watcherStartedMsg{watcher: newTestWatcher(t, sessionPath)})
	m = mm.(BreakdownModel)
	runArmedWaiter(cmd)

	newPath := filepath.Join(projectDir, "sess-2.jsonl")
	writeSessionFile(t, newPath)
	mm, _ = m.Update(sessionActivityMsg{path: newPath, id: "sess-2", created: true})
	m = mm.(BreakdownModel)
	if m.fileWaiterActive {
		t.Fatal("session switch did not reset fileWaiterActive")
	}

	mm, cmd = m.Update(watcherStartedMsg{watcher: newTestWatcher(t, newPath)})
	m = mm.(BreakdownModel)
	if cmd == nil {
		t.Fatal("new watcher after session switch did not arm a waiter")
	}
	if _, c := m.Update(breakdownMsgsMsg{}); c != nil {
		t.Fatal("completion after switch re-arm armed an extra waiter")
	}
}

func TestWatchStaleWatcherMessagesIgnored(t *testing.T) {
	// A waiter message from a superseded watcher must not clear the in-flight
	// flag (that would let a reload completion arm a second waiter on the
	// current watcher) nor stamp its error over the current session.
	sessionPath, projectDir, sessionID, _ := watchFixture(t)
	m := NewModel(sessionPath, sessionID, true, projectDir, false)

	mm, cmd := m.Update(watcherStartedMsg{watcher: newTestWatcher(t, sessionPath)})
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("watcherStartedMsg did not arm the file-change waiter")
	}
	// Complete the initial load so loading is false before the stale message
	mm, _ = m.Update(analysisMsg{})
	m = mm.(Model)

	stale := newTestWatcher(t, sessionPath)
	mm, cmd = m.Update(fileChangedMsg{watcher: stale})
	m = mm.(Model)
	if cmd != nil {
		t.Fatal("stale fileChangedMsg triggered a reload")
	}
	if m.loading {
		t.Fatal("stale fileChangedMsg set loading")
	}
	if !m.fileWaiterActive {
		t.Fatal("stale fileChangedMsg cleared the in-flight flag")
	}

	mm, cmd = m.Update(fileWatchErrMsg{err: errors.New("stale"), watcher: stale})
	m = mm.(Model)
	if cmd != nil {
		t.Fatal("stale fileWatchErrMsg armed a waiter")
	}
	if m.err != nil {
		t.Fatal("stale fileWatchErrMsg stamped its error on the model")
	}
	if !m.fileWaiterActive {
		t.Fatal("stale fileWatchErrMsg cleared the in-flight flag")
	}
}

func TestBreakdownStaleWatcherMessagesIgnored(t *testing.T) {
	sessionPath, projectDir, sessionID, _ := watchFixture(t)
	m := NewBreakdownModel(sessionPath, sessionID, true, projectDir, false)

	mm, cmd := m.Update(watcherStartedMsg{watcher: newTestWatcher(t, sessionPath)})
	m = mm.(BreakdownModel)
	if cmd == nil {
		t.Fatal("watcherStartedMsg did not arm the file-change waiter")
	}
	// Complete the initial load so loading is false before the stale message
	mm, _ = m.Update(breakdownMsgsMsg{})
	m = mm.(BreakdownModel)

	stale := newTestWatcher(t, sessionPath)
	mm, cmd = m.Update(fileChangedMsg{watcher: stale})
	m = mm.(BreakdownModel)
	if cmd != nil || m.loading || !m.fileWaiterActive {
		t.Fatal("stale fileChangedMsg was not ignored")
	}
	mm, cmd = m.Update(fileWatchErrMsg{err: errors.New("stale"), watcher: stale})
	m = mm.(BreakdownModel)
	if cmd != nil || m.err != nil || !m.fileWaiterActive {
		t.Fatal("stale fileWatchErrMsg was not ignored")
	}
}

func TestWatchReplacementWatcherClosesSuperseded(t *testing.T) {
	// Two watchFile calls racing (rapid session switches) deliver two
	// watcherStartedMsgs with no waiter exit between them: the second must
	// close the first watcher (so its waiter drains) and arm on the new one.
	sessionPath, projectDir, sessionID, _ := watchFixture(t)
	m := NewModel(sessionPath, sessionID, true, projectDir, false)

	w1 := newTestWatcher(t, sessionPath)
	mm, cmd := m.Update(watcherStartedMsg{watcher: w1})
	m = mm.(Model)
	runArmedWaiter(cmd)

	w2 := newTestWatcher(t, sessionPath)
	mm, cmd = m.Update(watcherStartedMsg{watcher: w2})
	m = mm.(Model)
	if m.watcher != w2 {
		t.Fatal("replacement watcher was not stored")
	}
	if cmd == nil {
		t.Fatal("replacement watcher did not arm a waiter")
	}
	// The superseded watcher must be closed so its waiter goroutine exits
	select {
	case _, ok := <-w1.Events:
		if ok {
			t.Fatal("superseded watcher delivered an event instead of closing")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("superseded watcher was not closed")
	}
	if _, c := m.Update(analysisMsg{}); c != nil {
		t.Fatal("completion after replacement armed an extra waiter")
	}
}

func TestWatchErrTriggersReload(t *testing.T) {
	// A watcher error (e.g. event-queue overflow) means changes may have been
	// dropped: the handler must reload, not just re-arm.
	sessionPath, projectDir, sessionID, _ := watchFixture(t)
	m := NewModel(sessionPath, sessionID, true, projectDir, false)

	mm, _ := m.Update(watcherStartedMsg{watcher: newTestWatcher(t, sessionPath)})
	m = mm.(Model)
	mm, cmd := m.Update(fileWatchErrMsg{err: errors.New("overflow"), watcher: m.watcher})
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("watch error returned no command")
	}
	if !m.loading {
		t.Fatal("watch error did not trigger a reload")
	}
	if m.err == nil {
		t.Fatal("watch error was not surfaced on the model")
	}
}

func TestBreakdownWatchErrTriggersReload(t *testing.T) {
	sessionPath, projectDir, sessionID, _ := watchFixture(t)
	m := NewBreakdownModel(sessionPath, sessionID, true, projectDir, false)

	mm, _ := m.Update(watcherStartedMsg{watcher: newTestWatcher(t, sessionPath)})
	m = mm.(BreakdownModel)
	mm, cmd := m.Update(fileWatchErrMsg{err: errors.New("overflow"), watcher: m.watcher})
	m = mm.(BreakdownModel)
	if cmd == nil {
		t.Fatal("watch error returned no command")
	}
	if !m.loading {
		t.Fatal("watch error did not trigger a reload")
	}
	if m.err == nil {
		t.Fatal("watch error was not surfaced on the model")
	}
}
