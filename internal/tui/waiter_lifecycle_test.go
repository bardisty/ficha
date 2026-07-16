package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// The file-change waiter lifecycle invariant (WATCH-01): at most one waiter
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
	m := NewModel(sessionPath, sessionID, false, true, projectDir, false)

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
		mm, cmd = m.Update(subagentPollMsg(time.Now()))
		m = mm.(Model)
		if cmd == nil {
			t.Fatalf("cycle %d: poll tick with changed signature did not trigger a reload", i)
		}
		if !m.loading {
			t.Fatalf("cycle %d: poll-triggered reload did not set loading", i)
		}
		mm, cmd = m.Update(analysisMsg(nil))
		m = mm.(Model)
		if cmd != nil {
			t.Fatalf("cycle %d: poll-triggered reload completion armed an extra waiter", i)
		}
	}

	// A failed reload must not arm one either
	mm, cmd = m.Update(errorMsg(errors.New("parse failed")))
	m = mm.(Model)
	if cmd != nil {
		t.Fatal("reload error armed an extra waiter")
	}

	// A file-triggered reload means the waiter exited: exactly one re-arm on
	// its completion, and only one
	mm, _ = m.Update(fileChangedMsg{})
	m = mm.(Model)
	mm, cmd = m.Update(analysisMsg(nil))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("file-triggered reload completion did not re-arm the waiter")
	}
	mm, cmd = m.Update(analysisMsg(nil))
	m = mm.(Model)
	if cmd != nil {
		t.Fatal("second completion after re-arm armed an extra waiter")
	}

	// A waiter error also means the waiter exited: re-arm a replacement
	mm, cmd = m.Update(fileWatchErrMsg{err: errors.New("watch failed")})
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("waiter error did not arm a replacement waiter")
	}
	if m.err == nil {
		t.Fatal("waiter error was not surfaced on the model")
	}
	if _, cmd = m.Update(analysisMsg(nil)); cmd != nil {
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
		mm, cmd = m.Update(subagentPollMsg(time.Now()))
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

	mm, cmd = m.Update(breakdownErrorMsg(errors.New("parse failed")))
	m = mm.(BreakdownModel)
	if cmd != nil {
		t.Fatal("reload error armed an extra waiter")
	}

	mm, _ = m.Update(fileChangedMsg{})
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

	mm, cmd = m.Update(fileWatchErrMsg{err: errors.New("watch failed")})
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
	m := NewModel(sessionPath, sessionID, false, true, projectDir, false)

	mm, cmd := m.Update(watcherStartedMsg{watcher: newTestWatcher(t, sessionPath)})
	m = mm.(Model)
	runArmedWaiter(cmd)

	// Mixed reload history: poll-triggered completions...
	for range 5 {
		growFile(t, agentPath)
		mm, _ = m.Update(subagentPollMsg(time.Now()))
		m = mm.(Model)
		mm, cmd = m.Update(analysisMsg(nil))
		m = mm.(Model)
		runArmedWaiter(cmd)
	}
	// ...and a file-triggered cycle
	mm, _ = m.Update(fileChangedMsg{})
	m = mm.(Model)
	mm, cmd = m.Update(analysisMsg(nil))
	m = mm.(Model)
	runArmedWaiter(cmd)

	quitDone := make(chan struct{})
	go func() {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
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
		mm, _ = m.Update(subagentPollMsg(time.Now()))
		m = mm.(BreakdownModel)
		mm, cmd = m.Update(breakdownMsgsMsg{})
		m = mm.(BreakdownModel)
		runArmedWaiter(cmd)
	}
	mm, _ = m.Update(fileChangedMsg{})
	m = mm.(BreakdownModel)
	mm, cmd = m.Update(breakdownMsgsMsg{})
	m = mm.(BreakdownModel)
	runArmedWaiter(cmd)

	quitDone := make(chan struct{})
	go func() {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
		close(quitDone)
	}()
	select {
	case <-quitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("quit hung in wg.Wait after mixed poll+file reloads")
	}
}
