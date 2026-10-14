package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// failSessionWatch makes the session watcher's fsnotify fail until the test
// ends, the way an exhausted inotify limit does.
func failSessionWatch(t *testing.T) {
	t.Helper()
	orig := newFsnotifyWatcher
	newFsnotifyWatcher = func() (*fsnotify.Watcher, error) {
		return nil, fmt.Errorf("inotify_init1: %w", syscall.EMFILE)
	}
	t.Cleanup(func() { newFsnotifyWatcher = orig })
}

// pollView drives watch and breakdown through the same session watcher
// scenarios.
type pollView struct {
	name     string
	open     func(projectDir, sessionID string, follow bool) tea.Model
	waiting  func(projectDir string) tea.Model
	start    func(tea.Model) tea.Cmd
	wait     func(tea.Model) tea.Cmd
	watcher  func(tea.Model) *SessionWatcher
	fallback func(tea.Model) watchFallback
	id       func(tea.Model) string
	notify   func(tea.Model) string
	loaded   tea.Msg
}

var pollViews = []pollView{
	{
		name: "watch",
		open: func(projectDir, sessionID string, follow bool) tea.Model {
			m, _ := NewModel(filepath.Join(projectDir, sessionID+".jsonl"), sessionID, true, projectDir, follow).Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			return m
		},
		waiting: func(projectDir string) tea.Model {
			m, _ := NewWaitingModel(projectDir, "~/w", nil, true, true).Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			return m
		},
		start:    func(m tea.Model) tea.Cmd { return m.(Model).startSessionWatcher() },
		wait:     func(m tea.Model) tea.Cmd { return m.(Model).waitForNewSession() },
		watcher:  func(m tea.Model) *SessionWatcher { return m.(Model).sessionWatcher },
		fallback: func(m tea.Model) watchFallback { return m.(Model).sessionFallback },
		id:       func(m tea.Model) string { return m.(Model).sessionID },
		notify:   func(m tea.Model) string { return m.(Model).renderNotifyRow(80) },
		loaded:   analysisMsg{},
	},
	{
		name: "breakdown",
		open: func(projectDir, sessionID string, follow bool) tea.Model {
			m, _ := NewBreakdownModel(filepath.Join(projectDir, sessionID+".jsonl"), sessionID, true, projectDir, follow).Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			return m
		},
		waiting: func(projectDir string) tea.Model {
			m, _ := NewWaitingBreakdownModel(projectDir, "~/w", nil, true, true).Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			return m
		},
		start:    func(m tea.Model) tea.Cmd { return m.(BreakdownModel).startSessionWatcher() },
		wait:     func(m tea.Model) tea.Cmd { return m.(BreakdownModel).waitForNewSession() },
		watcher:  func(m tea.Model) *SessionWatcher { return m.(BreakdownModel).sessionWatcher },
		fallback: func(m tea.Model) watchFallback { return m.(BreakdownModel).sessionFallback },
		id:       func(m tea.Model) string { return m.(BreakdownModel).sessionID },
		notify:   func(m tea.Model) string { return m.(BreakdownModel).renderNotifyRow() },
		loaded:   breakdownMsgsMsg{},
	},
}

// startPolling starts v's session watcher with fsnotify failing and checks
// that it came up polling, with the notice and the header saying so.
func startPolling(t *testing.T, v pollView, m tea.Model, follow bool) tea.Model {
	t.Helper()
	m, retry := m.Update(v.start(m)())
	t.Cleanup(func() { v.watcher(m).Stop() })
	if retry == nil {
		t.Fatal("a session watcher without fsnotify scheduled no retry")
	}
	if got := v.notify(m); !strings.Contains(got, unavailableText) || !strings.Contains(got, "r to retry") {
		t.Fatalf("notify row = %q, want the unavailable notice", got)
	}
	if want := "● FOLLOWING"; follow && !strings.Contains(frameOf(m), want) {
		t.Errorf("header lacks %q while the poll runs:\n%s", want, frameOf(m))
	}
	// The notice outlives a load: the watcher is still polling.
	m, _ = m.Update(v.loaded)
	if got := v.notify(m); !strings.Contains(got, unavailableText) {
		t.Fatalf("a load cleared the notice; notify row = %q", got)
	}
	return m
}

// Without fsnotify the session watcher runs on its poll: a following view
// still takes a new session, and a pinned one hints at it.
func TestSessionWatcherWithoutFsnotifyPolls(t *testing.T) {
	for _, v := range pollViews {
		for _, follow := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s follow=%v", v.name, follow), func(t *testing.T) {
				failSessionWatch(t)
				dir := t.TempDir()
				writeSessionFile(t, filepath.Join(dir, sessA+".jsonl"))
				m := startPolling(t, v, v.open(dir, sessA, follow), follow)

				writeSessionFile(t, filepath.Join(dir, sessB+".jsonl"))
				m, _ = m.Update(v.wait(m)())
				switch {
				case follow && v.id(m) != sessB:
					t.Errorf("a following view stayed on %s", v.id(m))
				case !follow && v.id(m) != sessA:
					t.Errorf("a pinned view switched to %s", v.id(m))
				case !follow && !strings.Contains(v.notify(m), "new session bbbbbbbb started • n to switch"):
					t.Errorf("notify row = %q, want the new session hint", v.notify(m))
				}
			})
		}
	}
}

// A waiting view takes the first session from the poll, in a project
// directory that appears only after the watcher started.
func TestWaitingWithoutFsnotifyTakesFirstSession(t *testing.T) {
	for _, v := range pollViews {
		t.Run(v.name, func(t *testing.T) {
			failSessionWatch(t)
			dir := filepath.Join(t.TempDir(), "-w-webapp")
			m := startPolling(t, v, v.waiting(dir), true)

			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeSessionFile(t, filepath.Join(dir, sessB+".jsonl"))
			if m, _ = m.Update(v.wait(m)()); v.id(m) != sessB {
				t.Errorf("waiting view didn't take the first session (id %q)", v.id(m))
			}
		})
	}
}

// r and the retry timer ask the watcher to try fsnotify again, and its
// waiter reports how that went: still polling, or watching once the limit
// frees up.
func TestSessionWatcherRetryTakesUpFsnotify(t *testing.T) {
	for _, v := range pollViews {
		t.Run(v.name, func(t *testing.T) {
			failSessionWatch(t)
			dir := t.TempDir()
			writeSessionFile(t, filepath.Join(dir, sessA+".jsonl"))
			m := startPolling(t, v, v.open(dir, sessA, true), true)
			sw := v.watcher(m)

			gen := v.fallback(m).gen
			_, cmd := m.Update(keyMsg(t, "r"))
			runBatch(cmd)
			m, _ = m.Update(v.wait(m)())
			if v.watcher(m) != sw || sw.watcher != nil {
				t.Fatal("a failed retry changed the watcher")
			}
			if v.fallback(m).gen != gen {
				t.Error("a failed r retry started a second timer")
			}
			if got := v.notify(m); !strings.Contains(got, unavailableText) {
				t.Fatalf("notify row = %q after a failed retry, want the notice", got)
			}

			newFsnotifyWatcher = fsnotify.NewWatcher
			m, cmd = m.Update(watchRetryMsg{gen: v.fallback(m).gen, sessions: true})
			if cmd == nil {
				t.Fatal("the retry timer did not retry")
			}
			runBatch(cmd)
			if m, _ = m.Update(v.wait(m)()); sw.watcher == nil {
				t.Fatal("the retry didn't take up fsnotify")
			}
			if got := v.notify(m); strings.Contains(got, unavailableText) {
				t.Errorf("notice survived recovery: %q", got)
			}
			writeSessionFile(t, filepath.Join(dir, sessB+".jsonl"))
			if m, _ = m.Update(v.wait(m)()); v.id(m) != sessB {
				t.Errorf("a new session after recovery wasn't followed (id %q)", v.id(m))
			}
		})
	}
}

// When even the poll can't list the project directory, nothing will see a
// new session, and the header's follow mode carries a warning.
func TestSessionWatcherBlindHeader(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the process can't list")
	}
	for _, v := range pollViews {
		t.Run(v.name, func(t *testing.T) {
			failSessionWatch(t)
			dir := t.TempDir()
			writeSessionFile(t, filepath.Join(dir, sessA+".jsonl"))
			if err := os.Chmod(dir, 0o300); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			m := v.open(dir, sessA, true)
			m, _ = m.Update(v.start(m)())
			t.Cleanup(func() { v.watcher(m).Stop() })
			if got, want := v.notify(m), "can't watch for new sessions (permission denied) • r to retry"; !strings.Contains(got, want) {
				t.Errorf("notify row = %q, want %q", got, want)
			}
			// At the boxed header's narrowest the mode still fits whole.
			for _, w := range []int{80, 40} {
				m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
				frame := frameOf(m)
				if strings.Contains(frame, "● FOLLOWING") || !strings.Contains(frame, "⚠ FOLLOWING │") {
					t.Errorf("%d columns: header doesn't warn that it can't follow:\n%s", w, frame)
				}
			}
		})
	}
}
