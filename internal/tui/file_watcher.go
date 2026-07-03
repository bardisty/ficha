package tui

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// newSessionFileWatcher creates a watcher for changes to sessionPath. It
// watches the parent directory rather than the file itself: inotify watches
// follow the inode, so a watch on the file goes silently stale when the file
// is renamed, deleted, or atomically replaced. Callers filter events by name
// (see awaitSessionFileChange).
func newSessionFileWatcher(sessionPath string) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := watcher.Add(filepath.Dir(sessionPath)); err != nil {
		watcher.Close()
		return nil, err
	}

	return watcher, nil
}

// awaitSessionFileChange blocks until sessionPath is written or (re)created,
// the done channel closes, or the watcher fails. The watcher observes the
// parent directory, so sibling-file events are filtered out by name. Create
// counts as a change alongside Write because it covers atomic replace (rename
// onto the path) and delete+recreate. Coalesces rapid events with a debounce
// so a burst of writes triggers a single reload.
// Returns (false, nil) on shutdown or watcher-channel close.
func awaitSessionFileChange(watcher *fsnotify.Watcher, done chan struct{}, sessionPath string) (bool, error) {
	target := filepath.Clean(sessionPath)
	isTargetChange := func(ev fsnotify.Event) bool {
		return filepath.Clean(ev.Name) == target && ev.Op&(fsnotify.Write|fsnotify.Create) != 0
	}

	for {
		select {
		case <-done:
			return false, nil
		case event, ok := <-watcher.Events:
			if !ok {
				return false, nil
			}
			if !isTargetChange(event) {
				continue
			}
			// Debounce: wait for writes to settle before signaling reload
			timer := time.NewTimer(watchDebounce)
			defer timer.Stop()
		drain:
			for {
				select {
				case <-done:
					return false, nil
				case <-timer.C:
					break drain
				case ev, ok := <-watcher.Events:
					if !ok {
						return false, nil
					}
					if isTargetChange(ev) {
						if !timer.Stop() {
							select {
							case <-timer.C:
							default:
							}
						}
						timer.Reset(watchDebounce)
					}
				case _, ok := <-watcher.Errors:
					if !ok {
						return false, nil
					}
				}
			}
			return true, nil
		case err, ok := <-watcher.Errors:
			if !ok {
				return false, nil
			}
			return false, err
		}
	}
}

// The watch and breakdown models run the same file/session-watching state
// machine and differ only in their error-message type, so each passes an onErr
// constructor to wrap failures in its own message.

// watchFileCmd creates the file watcher for sessionPath, returning a
// watcherStartedMsg on success or onErr(err) on failure.
func watchFileCmd(sessionPath string, onErr func(error) tea.Msg) tea.Msg {
	watcher, err := newSessionFileWatcher(sessionPath)
	if err != nil {
		return onErr(err)
	}
	return watcherStartedMsg{watcher: watcher}
}

// waitForFileChangeCmd waits for the session file to change (or for shutdown).
// wg.Add happens here, before the command is returned, not inside the returned
// goroutine: the quit handler calls wg.Wait, and an Add that races Wait can be
// missed, so shutdown wouldn't actually wait for this goroutine to drain.
func waitForFileChangeCmd(wg *sync.WaitGroup, closing *atomic.Bool, watcher *fsnotify.Watcher, done chan struct{}, sessionPath string, onErr func(error) tea.Msg) tea.Cmd {
	wg.Add(1)
	return func() tea.Msg {
		defer wg.Done()
		if closing != nil && closing.Load() {
			return nil
		}
		if watcher == nil || done == nil {
			return nil
		}

		changed, err := awaitSessionFileChange(watcher, done, sessionPath)
		if err != nil {
			return onErr(err)
		}
		if !changed {
			return nil
		}
		return fileChangedMsg{}
	}
}

// startSessionWatcherCmd starts the auto-follow session watcher for the project.
func startSessionWatcherCmd(projectDir, sessionID string, onErr func(error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		watcher := NewSessionWatcher(projectDir, sessionID)
		if err := watcher.Start(); err != nil {
			return onErr(err)
		}
		return sessionWatcherStartedMsg{watcher: watcher}
	}
}

// waitForNewSessionCmd blocks until the session watcher reports a new session
// (switch), a same-session restart, or shutdown. Returns a nil command when
// there is no watcher. wg.Add is before the return for the same reason as
// waitForFileChangeCmd.
func waitForNewSessionCmd(wg *sync.WaitGroup, sw *SessionWatcher) tea.Cmd {
	if sw == nil {
		return nil
	}
	wg.Add(1)
	return func() tea.Msg {
		defer wg.Done()

		path, id := sw.WaitForNewSession()
		if path == "" {
			return nil // Shutdown or error
		}
		if path == sessionRestartedPath {
			// Session was updated externally, just restart waiting
			return sessionWatcherRestartMsg{}
		}
		return sessionSwitchedMsg{
			newSessionPath: path,
			newSessionID:   id,
		}
	}
}

// shutdownWatchers performs the one-time teardown of the file and session
// watchers, then waits for in-flight watcher goroutines to drain. Shared by
// both models' quit handlers.
func shutdownWatchers(once *sync.Once, closing *atomic.Bool, done chan struct{}, watcher *fsnotify.Watcher, sw *SessionWatcher, wg *sync.WaitGroup) {
	once.Do(func() {
		closing.Store(true)
		if done != nil {
			close(done)
		}
		if watcher != nil {
			watcher.Close()
		}
		if sw != nil {
			sw.Stop()
		}
	})
	wg.Wait()
}
