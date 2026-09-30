package tui

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"

	"github.com/bardisty/ficha/internal/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Retry bounds for a session-file watcher that couldn't be created. The
// usual cause is an exhausted inotify limit, which frees up when some other
// process exits, so retries keep going at the cap rather than giving up.
const (
	watchRetryMin = 2 * time.Second
	watchRetryMax = 30 * time.Second
)

// newFileWatcher creates the session-file watcher; tests swap it to inject a
// creation failure.
var newFileWatcher = newSessionFileWatcher

// watcherFailedMsg reports that the session file's watcher couldn't be
// created. sessionPath drops a failure that lands after a session switch.
type watcherFailedMsg struct {
	err         error
	sessionPath string
}

// watchRetryMsg is a backoff timer firing; gen drops one that a switch or a
// successful start has superseded.
type watchRetryMsg struct{ gen int }

// watchUnavailableError marks a watcher-creation failure, so describeErr can
// say what it means for the view rather than print the raw syscall error.
type watchUnavailableError struct{ err error }

func (e watchUnavailableError) Error() string { return e.err.Error() }
func (e watchUnavailableError) Unwrap() error { return e.err }

// describeWatchUnavailable is the notify-row text for a watcher that couldn't
// be created: why, and that updates still arrive, only slower. An empty
// reason leaves the why out.
func describeWatchUnavailable(err error) string {
	return watchUnavailableText(watchFailureReason(err))
}

func watchUnavailableText(reason string) string {
	if reason != "" {
		reason = " (" + reason + ")"
	}
	return fmt.Sprintf("file watch unavailable%s, polling every %s", reason, subagentPollInterval)
}

// watchFailureReason names the cause of a watcher-creation failure.
func watchFailureReason(err error) string {
	if runtime.GOOS == "linux" && (errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENOSPC)) {
		// EMFILE from inotify_init is max_user_instances; ENOSPC from
		// inotify_add_watch is max_user_watches. Neither message says so.
		// EMFILE can also be the process's open-file limit, which ficha,
		// holding a handful of descriptors, is far from.
		return "inotify limit"
	}
	return err.Error()
}

// watchFallback carries the view while its session file has no watcher: the
// failure the notify row shows and the retry backoff. The poll that reloads
// in the meantime runs whether or not a watcher is up; see sessionFileSig.
// The failure stays up through reloads; only a watcher that starts, or a
// switch to another session, clears it.
type watchFallback struct {
	err     error         // watchUnavailableError; nil while a watcher is up
	gen     int           // identifies the live retry timer
	pending bool          // a retry timer is scheduled
	delay   time.Duration // the next retry's wait; 0 means watchRetryMin
}

// notice is the notify-row text while polling. A row too narrow for all of
// it drops the reason before the retry hint, which says what to do.
func (f watchFallback) notice(width int) string {
	tail := " " + styles.Bullet + " r to retry"
	text := styles.Warning + " " + describeErr(f.err) + tail
	if lipgloss.Width(text) > width {
		text = styles.Warning + " " + watchUnavailableText("") + tail
	}
	return text
}

// active reports whether the view is polling for want of a watcher.
func (f watchFallback) active() bool { return f.err != nil }

// fail records a creation failure and returns the retry timer, unless one is
// already scheduled (a manual retry failing while the timer runs).
func (f *watchFallback) fail(err error) tea.Cmd {
	f.err = watchUnavailableError{err: err}
	if f.pending {
		return nil
	}
	d := f.delay
	if d == 0 {
		d = watchRetryMin
	}
	f.delay = min(2*d, watchRetryMax)
	f.pending = true
	f.gen++
	gen := f.gen
	return tea.Tick(d, func(time.Time) tea.Msg { return watchRetryMsg{gen: gen} })
}

// reset clears the fallback, for a watcher that started or a new session.
// The generation moves on so a timer still in flight is ignored.
func (f *watchFallback) reset() { *f = watchFallback{gen: f.gen + 1} }

// retryDue reports whether msg is the live retry timer, consuming it.
func (f *watchFallback) retryDue(msg watchRetryMsg) bool {
	if msg.gen != f.gen || !f.pending {
		return false
	}
	f.pending = false
	return true
}
