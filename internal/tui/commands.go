package tui

import (
	"os"
	"time"

	"github.com/bardisty/ficha/internal/analyzer"
	tea "github.com/charmbracelet/bubbletea"
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

// Commands

func (m Model) loadAnalysis() tea.Msg {
	if m.closing != nil && m.closing.Load() {
		return nil
	}
	// Always include messages for the cost trend chart
	// The verbose flag controls additional output details, but we need messages
	// for the live chart regardless. The agent cache skips re-parsing unchanged
	// agent sub-sessions on each reload. AllMessages so the chart covers agent
	// spend too; the analyzer keeps parent-block-then-agent-blocks order and
	// updateCostChart merges chronologically on every reload.
	analysis, err := analyzer.AnalyzeSessionWithCache(m.sessionPath, m.sessionID, analyzer.AllMessages, m.agentCache)
	if err != nil {
		return errorMsg{err: err, sessionPath: m.sessionPath}
	}
	msg := analysisMsg{analysis: analysis, sessionPath: m.sessionPath}
	if info, err := os.Stat(m.sessionPath); err == nil {
		msg.modTime = info.ModTime()
	}
	return msg
}

// watcherStartedMsg is sent when the watcher is successfully created
type watcherStartedMsg struct {
	watcher *fsnotify.Watcher
}

// sessionWatcherStartedMsg is sent when the session watcher is ready
type sessionWatcherStartedMsg struct {
	watcher *SessionWatcher
}

// sessionWatcherRestartMsg signals that the watcher should restart waiting
type sessionWatcherRestartMsg struct{}

// wrapErr lets the shared watcher commands report failures as this model's
// error message.
func (m Model) wrapErr(err error) tea.Msg { return errorMsg{err: err, sessionPath: m.sessionPath} }

func (m Model) watchFile() tea.Msg { return watchFileCmd(m.sessionPath, m.wrapErr) }

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
	return startSessionWatcherCmd(m.projectDir, m.sessionID, m.wrapErr)
}

func (m Model) waitForNewSession() tea.Cmd {
	return waitForSessionEventCmd(m.wg, m.sessionWatcher)
}
