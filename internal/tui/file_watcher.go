package tui

import (
	"path/filepath"
	"time"

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
