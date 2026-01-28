package tui

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
)

// uuidPattern matches UUID-formatted session IDs (with or without .jsonl extension)
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$`)

// SessionWatcher watches a project directory for new session files
type SessionWatcher struct {
	projectDir     string
	currentSession string
	sessionMu      sync.RWMutex // Protects currentSession
	watcher        *fsnotify.Watcher
	done           chan struct{}
	closing        *atomic.Bool
	// restartCh signals waiters to restart (used when session changes)
	restartCh chan struct{}
	restartMu sync.Mutex
}

// NewSessionWatcher creates a new session watcher
func NewSessionWatcher(projectDir, currentSession string) *SessionWatcher {
	return &SessionWatcher{
		projectDir:     filepath.Clean(projectDir),
		currentSession: currentSession,
		done:           make(chan struct{}),
		closing:        &atomic.Bool{},
		restartCh:      make(chan struct{}),
	}
}

// SetCurrentSession updates the current session ID being watched
// This also signals any waiting goroutine to restart with the new session
func (sw *SessionWatcher) SetCurrentSession(sessionID string) {
	// Don't modify channels during shutdown - prevents double-close panic
	if sw.closing.Load() {
		return
	}

	sw.sessionMu.Lock()
	sw.currentSession = sessionID
	sw.sessionMu.Unlock()

	// Signal any waiting goroutine to restart
	sw.restartMu.Lock()
	// Re-check closing under lock to prevent race with Stop()
	if !sw.closing.Load() {
		close(sw.restartCh)
		sw.restartCh = make(chan struct{})
	}
	sw.restartMu.Unlock()
}

// Start begins watching the project directory for new session files
func (sw *SessionWatcher) Start() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	sw.watcher = watcher

	err = watcher.Add(sw.projectDir)
	if err != nil {
		watcher.Close()
		return err
	}

	return nil
}

// Stop stops the session watcher
func (sw *SessionWatcher) Stop() {
	if sw.closing.CompareAndSwap(false, true) {
		// Acquire restartMu to ensure no concurrent SetCurrentSession is modifying restartCh
		sw.restartMu.Lock()
		close(sw.done)
		sw.restartMu.Unlock()
		if sw.watcher != nil {
			sw.watcher.Close()
		}
	}
}

// sessionRestartedMsg is a sentinel value indicating the waiter should restart
const sessionRestartedPath = "\x00RESTART\x00"

// WaitForNewSession blocks until a new or different session is detected or shutdown
// Returns the new session path and ID, or empty strings if shutdown
// Returns sessionRestartedPath if the session was changed externally (caller should re-call)
// Detects both:
// - New session files being created (user started fresh session)
// - Writes to existing session files that aren't the current one (user switched back)
func (sw *SessionWatcher) WaitForNewSession() (string, string) {
	// Check closing flag before starting to avoid unnecessary work
	if sw.watcher == nil || sw.closing.Load() {
		return "", ""
	}

	// Get current restart channel under lock
	sw.restartMu.Lock()
	restartCh := sw.restartCh
	sw.restartMu.Unlock()

	for {
		select {
		case <-sw.done:
			return "", ""
		case <-restartCh:
			// Session was changed externally, signal caller to restart
			return sessionRestartedPath, ""
		case event, ok := <-sw.watcher.Events:
			if !ok {
				return "", ""
			}
			// Handle Create events (new session) or Write events (switched to existing session)
			if event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Write == fsnotify.Write {
				path, id := sw.handleSessionFile(event.Name)
				if path != "" {
					return path, id
				}
			}
		case _, ok := <-sw.watcher.Errors:
			if !ok {
				return "", ""
			}
			// Ignore errors, continue watching
		}
	}
}

// handleSessionFile processes a file event to see if it indicates a session switch
// Returns the path and session ID if we should switch, empty strings otherwise
func (sw *SessionWatcher) handleSessionFile(filePath string) (string, string) {
	// Get just the filename
	filename := filepath.Base(filePath)

	// Must be a .jsonl file at the project root (not in a subdirectory)
	// Session files are directly in projectDir, agent files are in subdirectories
	if filepath.Clean(filepath.Dir(filePath)) != sw.projectDir {
		return "", ""
	}

	// Must match UUID pattern
	if !uuidPattern.MatchString(filename) {
		return "", ""
	}

	// Extract session ID (remove .jsonl extension)
	sessionID := strings.TrimSuffix(filename, ".jsonl")

	// Skip if this is our current session (we don't need to switch to ourselves)
	sw.sessionMu.RLock()
	current := sw.currentSession
	sw.sessionMu.RUnlock()

	if sessionID == current {
		return "", ""
	}

	// This is a different session that's being written to or created
	// That means the user has switched to it - we should follow
	return filePath, sessionID
}
