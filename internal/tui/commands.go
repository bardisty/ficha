package tui

import (
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/fsnotify/fsnotify"
)

// Init initializes the TUI
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{clockCmd(time.Second, 0), subagentPollCmd()}
	if !m.waiting() {
		cmds = append(cmds, m.spinner.Tick, m.loadAnalysis, func() tea.Msg { return m.watchFile() })
	}

	// The session watcher runs in pinned mode too, to hint at newer sessions
	if m.projectDir != "" {
		cmds = append(cmds, m.startSessionWatcher())
	}

	return tea.Batch(cmds...)
}

// tickCmd returns a command that sends a tick every 100ms for animation
func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// clockMsg is watch's slow redraw tick for time-based text. gen names the
// chain it belongs to.
type clockMsg struct{ gen int }

// clockCmd schedules the next clockMsg of chain gen.
func clockCmd(d time.Duration, gen int) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clockMsg{gen: gen} })
}

// clockInterval paces clockMsg for a header that ages lastActivity: every
// second while the age counts seconds, and every 15s once it reads in
// minutes or more.
func clockInterval(lastActivity, now time.Time) time.Duration {
	if !lastActivity.IsZero() && now.Sub(lastActivity) < time.Minute {
		return time.Second
	}
	return 15 * time.Second
}

// Commands

func (m Model) loadAnalysis() tea.Msg {
	if m.closing != nil && m.closing.Load() {
		return nil
	}
	// The cost trend chart needs every message. The agent cache skips
	// re-parsing unchanged agent sub-sessions on each reload. AllMessages so
	// the chart's minutes count agent spend too; rebuildCostChart buckets
	// them by timestamp on every reload, whatever order the analyzer keeps.
	// The first load also looks for what the file shares with other
	// sessions, beside the analysis rather than after it, so the note comes
	// in the same frame as the body and never pushes it down later.
	var copies chan copiesResult
	if m.copiesFor != m.sessionPath {
		copies = make(chan copiesResult, 1)
		go func() {
			var r copiesResult
			r.copies, r.checked = analyzer.FindCopies(m.sessionPath, m.sessionID)
			copies <- r
		}()
	}
	analysis, err := analyzer.AnalyzeSessionWithCache(m.sessionPath, m.sessionID, analyzer.AllMessages, m.agentCache)
	if err != nil {
		return errorMsg{err: err, sessionPath: m.sessionPath}
	}
	msg := analysisMsg{analysis: analysis, sessionPath: m.sessionPath}
	if copies != nil {
		r := <-copies
		// A session with no reply yet has nothing to compare, so a later
		// load looks again.
		msg.copiesFound = r.checked || analysis.ParentMessageCount > 0
		msg.copies = r.copies
	}
	if info, err := os.Stat(m.sessionPath); err == nil {
		msg.modTime = info.ModTime()
	}
	return msg
}

// copiesResult is FindCopies' answer for one load.
type copiesResult struct {
	copies  *models.CopiedReplies
	checked bool
}

// watcherStartedMsg is sent when the watcher is successfully created.
// sessionPath drops a watcher that comes up after a session switch; an empty
// path applies, as in analysisMsg.
type watcherStartedMsg struct {
	watcher     *fsnotify.Watcher
	sessionPath string
}

// sessionWatcherStartedMsg is sent when the session watcher is ready, and
// after each RetryWatch. err says why it runs on its poll alone, and blind
// why even that can't list the project directory; both are nil for a
// watcher with fsnotify.
type sessionWatcherStartedMsg struct {
	watcher *SessionWatcher
	err     error
	blind   error
}

// sessionWatcherRestartMsg signals that the watcher should restart waiting
type sessionWatcherRestartMsg struct{}

func (m Model) watchFile() tea.Msg { return watchFileCmd(m.sessionPath) }

// retryWatchNow is r's watcher retry while polling for want of one; nil
// otherwise. A failure leaves the backoff timer as it was.
func (m Model) retryWatchNow() tea.Cmd {
	if !m.fallback.active() || m.watcher != nil {
		return nil
	}
	return m.watchFile
}

// armFileWaiter starts a file-change waiter unless one is already blocked on
// the watcher. Every reload — file-triggered, poll-triggered, manual "r",
// session switch — ends in the analysisMsg/errorMsg handlers, but only
// fileChangedMsg or fileWatchErrMsg means the previous waiter exited; arming
// unconditionally there would leak one blocked goroutine per poll-triggered
// reload. Returns nil when a waiter is already in flight.
func (m *Model) armFileWaiter() tea.Cmd {
	if m.watcher == nil || m.fileWaiterActive {
		return nil
	}
	m.fileWaiterActive = true
	return waitForFileChangeCmd(m.wg, m.closing, m.watcher, m.done, m.sessionPath)
}

func (m Model) startSessionWatcher() tea.Cmd {
	return startSessionWatcherCmd(m.projectDir, m.sessionID)
}

// retrySessionWatch is the session watcher's retry while it polls for want
// of fsnotify, for r and the backoff timer; nil otherwise. The outcome
// comes back as a sessionWatcherStartedMsg.
func (m Model) retrySessionWatch() tea.Cmd {
	return retrySessionWatchCmd(m.sessionWatcher, m.sessionFallback)
}

// retrySessionWatchCmd is both views' retrySessionWatch.
func retrySessionWatchCmd(sw *SessionWatcher, f watchFallback) tea.Cmd {
	if sw == nil || !f.active() {
		return nil
	}
	return func() tea.Msg {
		sw.RetryWatch()
		return nil
	}
}

func (m Model) waitForNewSession() tea.Cmd {
	return waitForSessionEventCmd(m.wg, m.sessionWatcher)
}
