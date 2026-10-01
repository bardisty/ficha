package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bardisty/ficha/internal/parser"
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

// errSessionFileGone reports that the watched session file was removed or
// renamed away. Without it the view would keep showing stale totals as live.
var errSessionFileGone = errors.New("session file removed")

// goneGrace is how long a removed session file may take to reappear before
// it counts as gone, so a delete-and-rewrite reads as a change, not an error.
const goneGrace = 250 * time.Millisecond

// awaitSessionFileChange blocks until sessionPath is written or (re)created,
// the done channel closes, or the watcher fails. A Remove or Rename of the
// path returns errSessionFileGone unless the file is re-created within
// goneGrace, which reports as a change. The watcher observes the
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
			if filepath.Clean(event.Name) == target && event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				back, stop := awaitRecreate(watcher, done, isTargetChange)
				if stop {
					return false, nil
				}
				if !back {
					return false, errSessionFileGone
				}
			} else if !isTargetChange(event) {
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

// awaitRecreate waits up to goneGrace for an event matching isBack. stop
// reports shutdown or a closed watcher.
func awaitRecreate(watcher *fsnotify.Watcher, done chan struct{}, isBack func(fsnotify.Event) bool) (back, stop bool) {
	timer := time.NewTimer(goneGrace)
	defer timer.Stop()
	for {
		select {
		case <-done:
			return false, true
		case <-timer.C:
			return false, false
		case ev, ok := <-watcher.Events:
			if !ok {
				return false, true
			}
			if isBack(ev) {
				return true, false
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return false, true
			}
		}
	}
}

// subagentPollInterval paces the fallback poll that catches subagent and
// workflow transcript writes. The fsnotify watch covers only the parent
// session file's directory (inotify is non-recursive), and during a workflow
// run the parent file can go silent for many minutes while agent files under
// {sessionID}/subagents/workflows/ accumulate tokens — without the poll those
// costs would surface only on the next parent-file write.
//
// The same tick checks the session file itself. A watcher can start without
// error and never deliver an event: fsnotify on WSL's 9p and drvfs mounts
// does that, and network filesystems may too.
const subagentPollInterval = 2 * time.Second

// subagentPollMsg signals a subagent-tree poll tick. The next tick is
// scheduled when this one's pollResultMsg arrives, so a poll slower than the
// interval can't have a second one start behind it.
type subagentPollMsg time.Time

// subagentPollCmd schedules the next subagent-tree poll tick.
func subagentPollCmd() tea.Cmd {
	return tea.Tick(subagentPollInterval, func(t time.Time) tea.Msg {
		return subagentPollMsg(t)
	})
}

// pollResultMsg carries what one poll measured, and the session and load
// count it was sent under. The stats run in a command, not in Update, so a
// slow filesystem delays the reload and never a key press.
type pollResultMsg struct {
	sessionPath string
	loadGen     int
	treeSig     string
	fileSig     string
}

// treeSignature is subagentTreeSignature, replaceable in tests.
var treeSignature = subagentTreeSignature

// pollSessionCmd fingerprints the session file and its subagent tree. The
// tree comes first: it is the slow one, and the file's fingerprint becomes
// the baseline of the load a change starts, so it is taken last.
func pollSessionCmd(sessionPath, sessionID string, loadGen int) tea.Cmd {
	return func() tea.Msg {
		return pollResultMsg{
			sessionPath: sessionPath,
			loadGen:     loadGen,
			treeSig:     treeSignature(filepath.Dir(sessionPath), sessionID),
			fileSig:     sessionFileSig(sessionPath),
		}
	}
}

// stale reports whether the view switched sessions or started a load after
// the poll was sent. That load took its own baseline, and the poll may have
// measured before it or after, so comparing the two could reload twice for
// one write. The next poll compares against the new baseline instead.
func (r pollResultMsg) stale(sessionPath string, loadGen int) bool {
	return r.sessionPath != sessionPath || r.loadGen != loadGen
}

// sessionFileSig fingerprints the session file by size and mtime. Appends
// always grow it, so a coarse mtime clock can't hide one. The views take it
// as a load starts, before the parse, so a write the load already covers
// doesn't trigger a second reload on the next poll.
func sessionFileSig(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	return fmt.Sprintf("%d|%d", info.Size(), info.ModTime().UnixNano())
}

// subagentTreeSignature fingerprints the session's subagent transcripts plus
// the top-level workflow metadata files, so the poll detects changes with
// stats only — no file reads. The transcript list comes from the same
// parser.DiscoverAgentSessions the analyzers load from, so poll coverage
// cannot diverge from discovery coverage — a naive directory walk here would
// miss the symlinked workflow run dirs discovery follows (WalkDir lstat-visits
// symlinks and never descends them), silently freezing reloads for exactly
// those agents. Missing dirs and stat errors contribute nothing; a session
// without subagents yields "".
func subagentTreeSignature(projectDir, sessionID string) string {
	var parts []string

	addFile := func(path string, info fs.FileInfo) {
		parts = append(parts, fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano()))
	}

	agentPaths, unreadableDirs := parser.DiscoverAgentSessions(projectDir, sessionID)
	for _, path := range agentPaths {
		if info, err := os.Stat(path); err == nil {
			addFile(path, info)
		}
	}
	// A directory turning (un)readable changes the surfaces' skipped-agent
	// accounting even when no discovered path does, so it flips the signature.
	if unreadableDirs > 0 {
		parts = append(parts, fmt.Sprintf("unreadable|%d", unreadableDirs))
	}

	sessionDir := filepath.Join(projectDir, sessionID)

	// Workflow run metadata: catches status flips (running → completed)
	// without any transcript write.
	if entries, err := os.ReadDir(filepath.Join(sessionDir, "workflows")); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if info, err := e.Info(); err == nil {
				addFile(e.Name(), info)
			}
		}
	}

	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

// The watch and breakdown models run the same file/session-watching state
// machine and differ only in their error-message type, so the session watcher
// takes an onErr constructor to wrap failures in each model's own message.

// watchFileCmd creates the file watcher for sessionPath, returning a
// watcherStartedMsg on success or a watcherFailedMsg, which starts the poll
// fallback (see watchFallback), on failure.
func watchFileCmd(sessionPath string) tea.Msg {
	watcher, err := newFileWatcher(sessionPath)
	if err != nil {
		return watcherFailedMsg{err: err, sessionPath: sessionPath}
	}
	return watcherStartedMsg{watcher: watcher, sessionPath: sessionPath}
}

// fileWatchErrMsg reports an error from an in-flight file-change waiter. It is
// deliberately distinct from the models' load-error messages: on receiving it
// the waiter is known to have exited, so Update clears the in-flight flag and
// re-arms — whereas a load error leaves the waiter blocked, and re-arming
// there would leak a goroutine (see armFileWaiter). Carries the waiter's
// watcher for the same staleness check as fileChangedMsg.
type fileWatchErrMsg struct {
	err     error
	watcher *fsnotify.Watcher
}

// waitForFileChangeCmd waits for the session file to change (or for shutdown).
// wg.Add happens here, before the command is returned, not inside the returned
// goroutine: the quit handler calls wg.Wait, and an Add that races Wait can be
// missed, so shutdown wouldn't actually wait for this goroutine to drain.
// Arm only through the models' armFileWaiter methods, which guarantee at most
// one waiter is blocked on the watcher at a time.
func waitForFileChangeCmd(wg *sync.WaitGroup, closing *atomic.Bool, watcher *fsnotify.Watcher, done chan struct{}, sessionPath string) tea.Cmd {
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
			return fileWatchErrMsg{err: err, watcher: watcher}
		}
		if !changed {
			return nil
		}
		return fileChangedMsg{watcher: watcher}
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

// sessionActivityMsg reports another session in the project changing: a
// newly created session file, or a write to an existing one.
type sessionActivityMsg struct {
	path    string
	id      string
	created bool
}

// waitForSessionEventCmd blocks until the session watcher reports activity
// in another session (sessionActivityMsg), a same-session restart, or
// shutdown. Returns a nil command when there is no watcher. wg.Add is before
// the return for the same reason as waitForFileChangeCmd.
func waitForSessionEventCmd(wg *sync.WaitGroup, sw *SessionWatcher) tea.Cmd {
	if sw == nil {
		return nil
	}
	wg.Add(1)
	return func() tea.Msg {
		defer wg.Done()

		ev := sw.WaitForSessionEvent()
		switch ev.path {
		case "":
			return nil // Shutdown or error
		case sessionRestartedPath:
			return sessionWatcherRestartMsg{}
		}
		return sessionActivityMsg(ev)
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
