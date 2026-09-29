package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// uuidPattern matches UUID-formatted session IDs (with or without .jsonl extension)
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$`)

// SessionWatcher watches a project directory for other sessions: new
// session files, and writes to existing ones.
type SessionWatcher struct {
	projectDir     string
	currentSession string
	// known holds the session IDs present when watching started plus every
	// one created since. Only a Create of an ID outside it is a new session;
	// a Create of a known ID is an atomic replace, which is just activity.
	known     map[string]bool
	sessionMu sync.RWMutex // Protects currentSession and known
	watcher   *fsnotify.Watcher
	done      chan struct{}
	// restartCh signals waiters to restart (used when session changes)
	restartCh chan struct{}
	restartMu sync.Mutex
	closeOnce sync.Once // Ensures Stop() logic runs exactly once
}

// NewSessionWatcher creates a new session watcher
func NewSessionWatcher(projectDir, currentSession string) *SessionWatcher {
	return &SessionWatcher{
		projectDir:     filepath.Clean(projectDir),
		currentSession: currentSession,
		known:          make(map[string]bool),
		done:           make(chan struct{}),
		restartCh:      make(chan struct{}),
	}
}

// SetCurrentSession updates the current session ID being watched
// This also signals any waiting goroutine to restart with the new session
func (sw *SessionWatcher) SetCurrentSession(sessionID string) {
	sw.sessionMu.Lock()
	sw.currentSession = sessionID
	sw.sessionMu.Unlock()

	// Signal any waiting goroutine to restart
	// The restartMu lock ensures this doesn't race with Stop()
	sw.restartMu.Lock()
	defer sw.restartMu.Unlock()

	// Check if done channel is closed (indicates shutdown)
	select {
	case <-sw.done:
		// Already stopped, don't touch restartCh
		return
	default:
		// Safe to close and recreate restartCh
		close(sw.restartCh)
		sw.restartCh = make(chan struct{})
	}
}

// Start begins watching the project directory for new session files
func (sw *SessionWatcher) Start() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	sw.watcher = watcher

	// A project Claude Code hasn't run in yet has no directory: watch the
	// projects directory for it instead, and switch over when it appears
	// (see adoptProjectDir).
	target := sw.projectDir
	if _, statErr := os.Stat(target); os.IsNotExist(statErr) {
		target = filepath.Dir(target)
	}
	err = watcher.Add(target)
	if err != nil {
		watcher.Close()
		return err
	}
	// The directory may have appeared between the Stat and the Add, whose
	// Create event would then be missed: watch it now if it exists.
	if target != sw.projectDir {
		if _, statErr := os.Stat(sw.projectDir); statErr == nil {
			_ = watcher.Add(sw.projectDir)
		}
	}

	// Snapshot after Add so no session slips between the two unseen. One
	// created in that instant lands in the snapshot and its Create then
	// reads as activity, so it is hinted rather than followed.
	if entries, err := os.ReadDir(sw.projectDir); err == nil {
		sw.sessionMu.Lock()
		for _, e := range entries {
			if uuidPattern.MatchString(e.Name()) {
				sw.known[strings.TrimSuffix(e.Name(), ".jsonl")] = true
			}
		}
		sw.sessionMu.Unlock()
	}

	return nil
}

// Stop stops the session watcher
func (sw *SessionWatcher) Stop() {
	sw.closeOnce.Do(func() {
		// Acquire restartMu to ensure no concurrent SetCurrentSession is modifying restartCh
		sw.restartMu.Lock()
		close(sw.done)
		sw.restartMu.Unlock()
		if sw.watcher != nil {
			sw.watcher.Close()
		}
	})
}

// sessionRestartedPath is a sentinel path telling the waiter to restart
const sessionRestartedPath = "\x00RESTART\x00"

// NewestSession returns the most recently modified session file already in
// the project directory, or empty strings when there is none.
func (sw *SessionWatcher) NewestSession() (string, string) {
	entries, err := os.ReadDir(sw.projectDir)
	if err != nil {
		return "", ""
	}
	var path, id string
	var newest int64
	for _, e := range entries {
		if !uuidPattern.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if mod := info.ModTime().UnixNano(); path == "" || mod > newest {
			path, id, newest = filepath.Join(sw.projectDir, e.Name()), strings.TrimSuffix(e.Name(), ".jsonl"), mod
		}
	}
	return path, id
}

// sessionEvent is another session in the project changing. created marks a
// brand-new session file; otherwise an existing session was written to.
// path is "" on shutdown and sessionRestartedPath when the current session
// was changed externally (the caller should wait again).
type sessionEvent struct {
	path    string
	id      string
	created bool
}

// WaitForNewSession blocks until a new session file is created, returning
// its path and ID, or empty strings on shutdown, or sessionRestartedPath.
// Writes to other existing sessions are skipped: with two sessions active in
// one project, following writes would flip between them on every message.
func (sw *SessionWatcher) WaitForNewSession() (string, string) {
	for {
		ev := sw.WaitForSessionEvent()
		if ev.path == "" || ev.path == sessionRestartedPath || ev.created {
			return ev.path, ev.id
		}
	}
}

// WaitForSessionEvent blocks until another session in the project is created
// or written to, the current session is changed externally, or shutdown.
func (sw *SessionWatcher) WaitForSessionEvent() sessionEvent {
	// Check done channel before starting to avoid unnecessary work
	select {
	case <-sw.done:
		return sessionEvent{}
	default:
	}
	if sw.watcher == nil {
		return sessionEvent{}
	}

	// Get current restart channel under lock
	sw.restartMu.Lock()
	restartCh := sw.restartCh
	sw.restartMu.Unlock()

	for {
		select {
		case <-sw.done:
			return sessionEvent{}
		case <-restartCh:
			// Session was changed externally, signal caller to restart
			return sessionEvent{path: sessionRestartedPath}
		case event, ok := <-sw.watcher.Events:
			if !ok {
				return sessionEvent{}
			}
			if event.Op&fsnotify.Create != 0 && filepath.Clean(event.Name) == sw.projectDir {
				if ev := sw.adoptProjectDir(); ev.path != "" {
					return ev
				}
				continue
			}
			if event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
				if ev := sw.handleSessionFile(event.Name, event.Op); ev.path != "" {
					return ev
				}
			}
		case _, ok := <-sw.watcher.Errors:
			if !ok {
				return sessionEvent{}
			}
			// Ignore errors, continue watching
		}
	}
}

// adoptProjectDir starts watching the project directory once it exists. A
// session file can land in it before the watch does, so any already there
// count as created now.
func (sw *SessionWatcher) adoptProjectDir() sessionEvent {
	if err := sw.watcher.Add(sw.projectDir); err != nil {
		return sessionEvent{}
	}
	entries, err := os.ReadDir(sw.projectDir)
	if err != nil {
		return sessionEvent{}
	}
	for _, e := range entries {
		if ev := sw.handleSessionFile(filepath.Join(sw.projectDir, e.Name()), fsnotify.Create); ev.path != "" {
			return ev
		}
	}
	return sessionEvent{}
}

// handleSessionFile classifies a file event: another session's file at the
// project root yields an event, anything else (the current session, agent
// files in subdirectories, non-session files) a zero one.
func (sw *SessionWatcher) handleSessionFile(filePath string, op fsnotify.Op) sessionEvent {
	// Session files sit directly in projectDir; agent files are in subdirectories
	if filepath.Clean(filepath.Dir(filePath)) != sw.projectDir {
		return sessionEvent{}
	}
	filename := filepath.Base(filePath)
	if !uuidPattern.MatchString(filename) {
		return sessionEvent{}
	}
	sessionID := strings.TrimSuffix(filename, ".jsonl")

	sw.sessionMu.Lock()
	defer sw.sessionMu.Unlock()
	created := op&fsnotify.Create != 0 && !sw.known[sessionID]
	sw.known[sessionID] = true
	if sessionID == sw.currentSession {
		return sessionEvent{}
	}
	return sessionEvent{path: filePath, id: sessionID, created: created}
}
