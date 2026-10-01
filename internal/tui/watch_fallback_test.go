package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/fsnotify/fsnotify"
)

// failWatcherCreation makes every watcher creation fail until the test ends,
// the way an exhausted inotify limit does, and counts the attempts.
func failWatcherCreation(t *testing.T) *atomic.Int32 {
	t.Helper()
	var attempts atomic.Int32
	orig := newFileWatcher
	newFileWatcher = func(string) (*fsnotify.Watcher, error) {
		attempts.Add(1)
		return nil, fmt.Errorf("inotify_init: %w", syscall.EMFILE)
	}
	t.Cleanup(func() { newFileWatcher = orig })
	return &attempts
}

// fallbackHarness drives watch and breakdown through the same scenarios.
type fallbackHarness struct {
	name     string
	open     func(sessionPath, projectDir, sessionID string) tea.Model
	fallback func(tea.Model) watchFallback
	watcher  func(tea.Model) *fsnotify.Watcher
	loading  func(tea.Model) bool
	notify   func(tea.Model) string
	loaded   tea.Msg // a completed reload
}

var fallbackHarnesses = []fallbackHarness{
	{
		name: "watch",
		open: func(sessionPath, projectDir, sessionID string) tea.Model {
			return NewModel(sessionPath, sessionID, true, projectDir, true)
		},
		fallback: func(m tea.Model) watchFallback { return m.(Model).fallback },
		watcher:  func(m tea.Model) *fsnotify.Watcher { return m.(Model).watcher },
		loading:  func(m tea.Model) bool { return m.(Model).loading },
		notify:   func(m tea.Model) string { return m.(Model).renderNotifyRow(80) },
		loaded:   analysisMsg{},
	},
	{
		name: "breakdown",
		open: func(sessionPath, projectDir, sessionID string) tea.Model {
			return NewBreakdownModel(sessionPath, sessionID, true, projectDir, true)
		},
		fallback: func(m tea.Model) watchFallback { return m.(BreakdownModel).fallback },
		watcher:  func(m tea.Model) *fsnotify.Watcher { return m.(BreakdownModel).watcher },
		loading:  func(m tea.Model) bool { return m.(BreakdownModel).loading },
		notify:   func(m tea.Model) string { return m.(BreakdownModel).renderNotifyRow() },
		loaded:   breakdownMsgsMsg{},
	},
}

const unavailableText = "file watch unavailable"

func TestWatcherFailurePollsUntilRecovery(t *testing.T) {
	for _, h := range fallbackHarnesses {
		t.Run(h.name, func(t *testing.T) {
			sessionPath, projectDir, sessionID, _ := watchFixture(t)
			failWatcherCreation(t)
			m := h.open(sessionPath, projectDir, sessionID)

			failed := watchFileCmd(sessionPath)
			if _, ok := failed.(watcherFailedMsg); !ok {
				t.Fatalf("watchFileCmd = %T, want watcherFailedMsg", failed)
			}
			m, cmd := m.Update(failed)
			if cmd == nil {
				t.Fatal("a failed watcher scheduled no retry")
			}
			if got := h.notify(m); !strings.Contains(got, unavailableText) || !strings.Contains(got, "r to retry") {
				t.Fatalf("notify row = %q, want the unavailable notice", got)
			}

			// A successful reload must not clear it: the view is still
			// polling.
			m, _ = m.Update(h.loaded)
			if got := h.notify(m); !strings.Contains(got, unavailableText) {
				t.Fatalf("a reload cleared the notice; notify row = %q", got)
			}

			// A write that landed between the initial load and the failure
			// is caught by the first poll.
			growFile(t, sessionPath)
			m, _ = pollOnce(t, m)
			if !h.loading(m) {
				t.Fatal("first poll after the failure missed a write from before it")
			}
			m, _ = m.Update(h.loaded)

			m, _ = pollOnce(t, m)
			if h.loading(m) {
				t.Fatal("poll reloaded an unchanged file")
			}

			growFile(t, sessionPath)
			m, _ = pollOnce(t, m)
			if !h.loading(m) {
				t.Fatal("poll missed an append to the session file")
			}
			m, _ = m.Update(h.loaded)
			if got := h.notify(m); !strings.Contains(got, unavailableText) {
				t.Fatalf("a poll reload cleared the notice; notify row = %q", got)
			}

			// The limit frees up: the next retry starts a watcher and the
			// notice goes. A write since the last poll predates the watcher,
			// so its start reloads once to catch it.
			growFile(t, sessionPath)
			newFileWatcher = newSessionFileWatcher
			m, cmd = m.Update(watchRetryMsg{gen: h.fallback(m).gen})
			if cmd == nil {
				t.Fatal("the retry timer did not retry")
			}
			started, ok := cmd().(watcherStartedMsg)
			if !ok {
				t.Fatal("retry did not start a watcher")
			}
			t.Cleanup(func() { started.watcher.Close() })
			m, _ = m.Update(started)
			if h.watcher(m) != started.watcher {
				t.Fatal("the retried watcher was not installed")
			}
			if got := h.notify(m); strings.Contains(got, unavailableText) {
				t.Fatalf("notice survived recovery: %q", got)
			}
			if !h.loading(m) {
				t.Fatal("recovery did not reload for writes the new watcher never saw")
			}
			m, _ = m.Update(h.loaded)

			// That reload read the write, so the poll has nothing to add.
			m, _ = pollOnce(t, m)
			if h.loading(m) {
				t.Fatal("poll reloaded for a write the recovery reload already read")
			}
		})
	}
}

func TestWatcherFailureRetryIsOneTimer(t *testing.T) {
	for _, h := range fallbackHarnesses {
		t.Run(h.name, func(t *testing.T) {
			sessionPath, projectDir, sessionID, _ := watchFixture(t)
			failWatcherCreation(t)
			m := h.open(sessionPath, projectDir, sessionID)

			m, _ = m.Update(watchFileCmd(sessionPath))
			gen := h.fallback(m).gen

			// A second failure while the timer runs (r failing too) must not
			// start another timer chain.
			m, cmd := m.Update(watchFileCmd(sessionPath))
			if cmd != nil {
				t.Fatal("a failure while a retry is pending scheduled another")
			}
			if _, cmd = m.Update(watchRetryMsg{gen: gen - 1}); cmd != nil {
				t.Fatal("a superseded retry timer retried")
			}
			m, cmd = m.Update(watchRetryMsg{gen: gen})
			if cmd == nil {
				t.Fatal("the live retry timer did not retry")
			}
			if _, cmd = m.Update(watchRetryMsg{gen: gen}); cmd != nil {
				t.Fatal("one timer retried twice")
			}
			if _, cmd = m.Update(watchFileCmd(sessionPath)); cmd == nil {
				t.Fatal("a failed retry scheduled no further retry")
			}
		})
	}
}

func TestWatcherFailureRKeyRetriesNow(t *testing.T) {
	for _, h := range fallbackHarnesses {
		t.Run(h.name, func(t *testing.T) {
			sessionPath, projectDir, sessionID, _ := watchFixture(t)
			attempts := failWatcherCreation(t)
			m := h.open(sessionPath, projectDir, sessionID)
			m, _ = m.Update(watchFileCmd(sessionPath))
			attempts.Store(0)

			_, cmd := m.Update(keyMsg(t, "r"))
			runBatch(cmd)
			if attempts.Load() != 1 {
				t.Fatalf("r made %d watcher attempts, want 1", attempts.Load())
			}
		})
	}
}

// runBatch runs cmd and, for a batch, each command in it.
func runBatch(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			runBatch(c)
		}
	}
}

func TestWatcherResultsForALeftSessionAreDropped(t *testing.T) {
	for _, h := range fallbackHarnesses {
		t.Run(h.name, func(t *testing.T) {
			sessionPath, projectDir, sessionID, _ := watchFixture(t)
			failWatcherCreation(t)
			m := h.open(sessionPath, projectDir, sessionID)
			m, _ = m.Update(watchFileCmd(sessionPath))
			oldGen := h.fallback(m).gen

			newPath := filepath.Join(projectDir, "sess-2.jsonl")
			writeSessionFile(t, newPath)
			m, _ = m.Update(sessionActivityMsg{path: newPath, id: "sess-2", created: true})
			if h.fallback(m).active() {
				t.Fatal("the old session's watcher failure survived the switch")
			}

			if _, cmd := m.Update(watchRetryMsg{gen: oldGen}); cmd != nil {
				t.Fatal("the old session's retry timer retried")
			}
			m, _ = m.Update(watcherFailedMsg{err: syscall.EMFILE, sessionPath: sessionPath})
			if h.fallback(m).active() {
				t.Fatal("the old session's watcher failure applied to the new one")
			}

			stale := newTestWatcher(t, sessionPath)
			m, _ = m.Update(watcherStartedMsg{watcher: stale, sessionPath: sessionPath})
			if h.watcher(m) != nil {
				t.Fatal("a watcher for the old session was installed")
			}
		})
	}
}

func TestWatchFallbackBackoff(t *testing.T) {
	var f watchFallback
	var nexts []time.Duration
	for range 6 {
		f.fail(syscall.EMFILE)
		nexts = append(nexts, f.delay)
		f.retryDue(watchRetryMsg{gen: f.gen})
	}
	want := []time.Duration{4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second}
	for i := range want {
		if nexts[i] != want[i] {
			t.Fatalf("delays after each retry = %v, want %v", nexts, want)
		}
	}

	f.reset()
	if f.active() || f.delay != 0 {
		t.Fatalf("reset left %+v", f)
	}
}

func TestDescribeWatchUnavailable(t *testing.T) {
	got := describeErr(watchUnavailableError{err: fmt.Errorf("inotify_init: %w", syscall.EMFILE)})
	if !strings.HasPrefix(got, unavailableText) || !strings.HasSuffix(got, "polling every 2s") {
		t.Errorf("describeErr = %q", got)
	}
	if runtime.GOOS == "linux" && !strings.Contains(got, "(inotify limit)") {
		t.Errorf("describeErr = %q, want the inotify limit named", got)
	}
	if got := describeErr(watchUnavailableError{err: errors.New("boom")}); !strings.Contains(got, "(boom)") {
		t.Errorf("describeErr = %q, want the raw reason", got)
	}
}

func TestWatchFallbackNoticeKeepsRetryHintWhenNarrow(t *testing.T) {
	var f watchFallback
	f.fail(errors.New("no watches left"))
	if got := f.notice(76); !strings.Contains(got, "(no watches left)") || !strings.HasSuffix(got, "r to retry") {
		t.Errorf("notice(76) = %q, want the reason and the retry hint", got)
	}
	got := f.notice(58)
	if strings.Contains(got, "no watches") || !strings.HasSuffix(got, "r to retry") || lipgloss.Width(got) > 58 {
		t.Errorf("notice(58) = %q, want the reason dropped for the retry hint", got)
	}
}
