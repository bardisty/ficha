package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

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
	known map[string]bool
	// sigs holds each session file's size and mtime as last seen, so the
	// poll can tell a write from nothing new.
	sigs map[string]sessionSig
	// switches counts SetCurrentSession calls, so observe can tell that one
	// landed between its stat and its record.
	switches  int
	sessionMu sync.RWMutex // Protects currentSession, known, sigs and switches
	watcher   *fsnotify.Watcher
	done      chan struct{}
	// restartCh signals waiters to restart (used when session changes)
	restartCh chan struct{}
	restartMu sync.Mutex
	closeOnce sync.Once // Ensures Stop() logic runs exactly once

	// The poll's state, touched only by the goroutine in WaitForSessionEvent.
	pollEvery  time.Duration
	nextPoll   time.Time
	polls      int
	dirPending bool // the project directory hadn't appeared at Start
}

// sessionSig is a session file's size and mtime.
type sessionSig struct {
	size int64
	mod  time.Time
}

// fullScanEvery is how many polls pass between stats of every session. The
// polls between stat only sessions written to within idleAfter: a stat over
// WSL's 9p mounts takes milliseconds, and a project can hold hundreds of
// sessions. A write to an older session is noticed within 30 seconds.
const fullScanEvery = 15

// NewSessionWatcher creates a new session watcher
func NewSessionWatcher(projectDir, currentSession string) *SessionWatcher {
	return &SessionWatcher{
		projectDir:     filepath.Clean(projectDir),
		currentSession: currentSession,
		known:          make(map[string]bool),
		sigs:           make(map[string]sessionSig),
		pollEvery:      subagentPollInterval,
		done:           make(chan struct{}),
		restartCh:      make(chan struct{}),
	}
}

// SetCurrentSession updates the current session ID being watched
// This also signals any waiting goroutine to restart with the new session
func (sw *SessionWatcher) SetCurrentSession(sessionID string) {
	sw.sessionMu.Lock()
	// Writes the session being left made since the poll last looked were the
	// view's own; a silent watch reported none of them. Record them before
	// it stops being current, under the same lock, so the next poll can't
	// take them for news. A file that can't be stated keeps its last
	// signature.
	if left := sw.currentSession; left != "" && left != sessionID {
		if sig, ok := statSig(filepath.Join(sw.projectDir, left+".jsonl")); ok {
			sw.sigs[left] = sig
		}
	}
	sw.currentSession = sessionID
	sw.switches++
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
		} else {
			sw.dirPending = true
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

// WaitForSessionEvent blocks until another session in the project is created
// or written to, the current session is changed externally, or shutdown.
// Alongside the fsnotify watch it polls the project directory, since a watch
// can start without error and never deliver an event, as on WSL's 9p mounts.
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

	// The schedule outlives this call: another session's steady writes
	// return from it every few hundred milliseconds, and a timer started
	// afresh each time would never fire.
	pollTimer := time.NewTimer(time.Until(sw.nextPoll))
	defer pollTimer.Stop()

	for {
		select {
		case <-pollTimer.C:
			ev := sw.poll()
			sw.nextPoll = time.Now().Add(sw.pollEvery)
			if ev.path != "" {
				return ev
			}
			pollTimer.Reset(sw.pollEvery)
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
	sw.dirPending = false
	if err := sw.watcher.Add(sw.projectDir); err != nil {
		return sessionEvent{}
	}
	entries, err := os.ReadDir(sw.projectDir)
	if err != nil {
		return sessionEvent{}
	}
	// Every session goes into known, not just the one reported, or the next
	// poll would report the rest as new.
	var first sessionEvent
	for _, e := range entries {
		if ev := sw.handleSessionFile(filepath.Join(sw.projectDir, e.Name()), fsnotify.Create); first.path == "" {
			first = ev
		}
	}
	return first
}

// poll lists the project directory for what the watch missed: a session
// file not seen before is a new session, and a new size or mtime on another
// is activity, the most recent reported when several moved. New sessions are
// found by name alone, before any stat, so a busy session can't hide one; see
// fullScanEvery for which sessions it stats.
func (sw *SessionWatcher) poll() sessionEvent {
	entries, err := os.ReadDir(sw.projectDir)
	if err != nil {
		return sessionEvent{}
	}
	if sw.dirPending {
		return sw.adoptProjectDir()
	}
	var ids []string
	for _, e := range entries {
		if uuidPattern.MatchString(e.Name()) {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".jsonl"))
		}
	}
	sw.sessionMu.RLock()
	for _, id := range ids {
		if !sw.known[id] {
			sw.sessionMu.RUnlock()
			return sw.handleSessionFile(filepath.Join(sw.projectDir, id+".jsonl"), fsnotify.Create)
		}
	}
	sw.sessionMu.RUnlock()

	full := sw.polls%fullScanEvery == 0
	sw.polls++
	now := time.Now()
	var ev sessionEvent
	var newest time.Time
	for _, id := range ids {
		if full {
			select {
			case <-sw.done:
				return sessionEvent{}
			default:
			}
		}
		sw.sessionMu.RLock()
		current := id == sw.currentSession
		prev, seen := sw.sigs[id]
		sw.sessionMu.RUnlock()
		// The current session's writes are the view's own, but tracking them
		// keeps most of them from reading as activity once the view leaves it.
		if !current && !full && (!seen || now.Sub(prev.mod) >= idleAfter) {
			continue
		}
		path := filepath.Join(sw.projectDir, id+".jsonl")
		if sig, changed := sw.observe(id, path); changed && !current && sig.mod.After(newest) {
			ev, newest = sessionEvent{path: path, id: id}, sig.mod
		}
	}
	return ev
}

// observe records a session file's size and mtime, and reports whether
// either moved since the last time. The first sighting doesn't count.
func (sw *SessionWatcher) observe(id, path string) (sessionSig, bool) {
	sw.sessionMu.RLock()
	switches := sw.switches
	sw.sessionMu.RUnlock()
	sig, ok := statSig(path)
	if !ok {
		return sessionSig{}, false
	}
	return sw.record(id, sig, switches)
}

// record is observe's second half, for a sig taken when switches
// SetCurrentSession calls had been made. A switch since then may have
// recorded a newer sig for the session it left, which this one must not
// overwrite, so it's dropped; the next poll stats the file again.
func (sw *SessionWatcher) record(id string, sig sessionSig, switches int) (sessionSig, bool) {
	sw.sessionMu.Lock()
	defer sw.sessionMu.Unlock()
	if sw.switches != switches {
		return sig, false
	}
	prev, seen := sw.sigs[id]
	sw.sigs[id] = sig
	return sig, seen && (prev.size != sig.size || !prev.mod.Equal(sig.mod))
}

// statSig is a file's size and mtime, and false when it can't be stated.
func statSig(path string) (sessionSig, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return sessionSig{}, false
	}
	return sessionSig{size: info.Size(), mod: info.ModTime()}, true
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
	// The poll then finds nothing new in a write reported here.
	sw.observe(sessionID, filePath)

	sw.sessionMu.Lock()
	defer sw.sessionMu.Unlock()
	created := op&fsnotify.Create != 0 && !sw.known[sessionID]
	sw.known[sessionID] = true
	if sessionID == sw.currentSession {
		return sessionEvent{}
	}
	return sessionEvent{path: filePath, id: sessionID, created: created}
}
