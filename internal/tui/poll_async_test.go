package tui

import (
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// pollOnce delivers a poll tick the way the program does: Update hands back
// the command that stats the files, and its result goes back into Update.
func pollOnce(t *testing.T, m tea.Model) (tea.Model, tea.Cmd) {
	t.Helper()
	m, res := sendPoll(t, m)
	return m.Update(res)
}

// sendPoll delivers a poll tick and runs its command, returning the result
// undelivered so a test can put other messages ahead of it.
func sendPoll(t *testing.T, m tea.Model) (tea.Model, pollResultMsg) {
	t.Helper()
	m, cmd := m.Update(subagentPollMsg(time.Now()))
	if cmd == nil {
		t.Fatal("poll tick returned no command")
	}
	res, ok := cmd().(pollResultMsg)
	if !ok {
		t.Fatal("poll tick's command did not measure the session")
	}
	return m, res
}

// deliversPollTick runs cmd, and whatever it batches, and reports whether a
// poll tick comes out. The tick takes subagentPollInterval to arrive.
func deliversPollTick(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case subagentPollMsg:
		return true
	case tea.BatchMsg:
		for _, c := range msg {
			if deliversPollTick(c) {
				return true
			}
		}
	}
	return false
}

// The poll's stats run in the command, never in Update: on a slow filesystem
// they would hold up every key for as long as they take.
func TestPollStatsRunOutsideUpdate(t *testing.T) {
	for _, h := range fallbackHarnesses {
		t.Run(h.name, func(t *testing.T) {
			m, _ := startWatching(t, h)

			calls := 0
			real := treeSignature
			treeSignature = func(projectDir, sessionID string) string {
				calls++
				return real(projectDir, sessionID)
			}
			t.Cleanup(func() { treeSignature = real })

			m, cmd := m.Update(subagentPollMsg(time.Now()))
			if calls != 0 {
				t.Fatal("Update fingerprinted the agent tree itself")
			}
			res, ok := cmd().(pollResultMsg)
			if !ok {
				t.Fatal("poll tick's command did not measure the session")
			}
			if calls != 1 {
				t.Fatalf("the command fingerprinted the tree %d times, want 1", calls)
			}
			m.Update(res)
			if calls != 1 {
				t.Fatal("Update fingerprinted the agent tree when the result arrived")
			}
		})
	}
}

// One poll at a time: the tick doesn't schedule the next one, the result
// does, so a poll slower than the interval never has another queue behind
// it. Every kind of result schedules it, or the polls would stop.
func TestPollSchedulesTheNextOnlyWhenItsResultArrives(t *testing.T) {
	for _, h := range fallbackHarnesses {
		t.Run(h.name, func(t *testing.T) {
			t.Parallel()
			m, sessionPath := startWatching(t, h)

			// sendPoll fails unless the tick's command is the measurement
			// alone, with no timer batched beside it.
			m, res := sendPoll(t, m)
			_, unchanged := m.Update(res)

			growFile(t, sessionPath)
			m, res = sendPoll(t, m)
			m, reloaded := m.Update(res)
			if !h.loading(m) {
				t.Fatal("poll missed a write")
			}

			// Sent before that reload started, so it's stale.
			_, dropped := m.Update(res)

			cases := []struct {
				name string
				cmd  tea.Cmd
			}{
				{"an unchanged result", unchanged},
				{"a result that reloaded", reloaded},
				{"a dropped result", dropped},
			}
			found := make([]chan bool, len(cases))
			for i, c := range cases {
				found[i] = make(chan bool, 1)
				go func() { found[i] <- deliversPollTick(c.cmd) }()
			}
			for i, c := range cases {
				if !<-found[i] {
					t.Errorf("%s did not schedule the next poll", c.name)
				}
			}
		})
	}
}

// A result measured for the session the view has since left says nothing
// about the one it's on.
func TestPollResultForALeftSessionIsDropped(t *testing.T) {
	for _, h := range fallbackHarnesses {
		t.Run(h.name, func(t *testing.T) {
			m, sessionPath := startWatching(t, h)
			projectDir := filepath.Dir(sessionPath)

			growFile(t, sessionPath)
			m, res := sendPoll(t, m)

			newPath := filepath.Join(projectDir, "sess-2.jsonl")
			writeSessionFile(t, newPath)
			m, _ = m.Update(sessionActivityMsg{path: newPath, id: "sess-2", created: true})
			m, _ = m.Update(h.loaded)
			if h.loading(m) {
				t.Fatal("the switch's load did not land")
			}

			m, _ = m.Update(res)
			if h.loading(m) {
				t.Fatal("a poll of the previous session reloaded the new one")
			}

			// The new session's own polls still work.
			m, _ = pollOnce(t, m)
			if h.loading(m) {
				t.Fatal("poll reloaded an unchanged session after the switch")
			}
			growFile(t, newPath)
			m, _ = pollOnce(t, m)
			if !h.loading(m) {
				t.Fatal("poll missed a write to the new session")
			}
		})
	}
}

// r, or a watcher event, can start a load while a poll is out. That load
// takes its own baseline, so a result measured before it must not reload for
// a write the load read. A write after the load started still gets its one
// reload from the next poll.
func TestPollResultFromBeforeALoadIsDropped(t *testing.T) {
	for _, h := range fallbackHarnesses {
		t.Run(h.name, func(t *testing.T) {
			sessionPath, projectDir, sessionID, agentPath := watchFixture(t)
			m := h.open(sessionPath, projectDir, sessionID)
			m, _ = m.Update(h.loaded)

			m, res := sendPoll(t, m)

			growFile(t, sessionPath)
			m = press(t, m, "r")
			if !h.loading(m) {
				t.Fatal("r did not reload")
			}
			growFile(t, agentPath)
			m, _ = m.Update(h.loaded)

			m, _ = m.Update(res)
			if h.loading(m) {
				t.Fatal("a poll sent before r's load reloaded for the write that load read")
			}

			m, _ = pollOnce(t, m)
			if !h.loading(m) {
				t.Fatal("poll missed an agent write made during the load")
			}
			m, _ = m.Update(h.loaded)
			m, _ = pollOnce(t, m)
			if h.loading(m) {
				t.Fatal("poll reloaded twice for one write")
			}
		})
	}
}

// The reload a poll starts takes the poll's own measurement as its baseline,
// with no second stat in Update. So a write that lands after the measurement
// is newer than the baseline, and the next poll reloads for it.
func TestPollReloadUsesItsOwnMeasurementAsBaseline(t *testing.T) {
	for _, h := range fallbackHarnesses {
		t.Run(h.name, func(t *testing.T) {
			m, sessionPath := startWatching(t, h)

			growFile(t, sessionPath)
			m, res := sendPoll(t, m)
			growFile(t, sessionPath)
			m, _ = m.Update(res)
			if !h.loading(m) {
				t.Fatal("poll missed a write")
			}
			m, _ = m.Update(h.loaded)

			m, _ = pollOnce(t, m)
			if !h.loading(m) {
				t.Fatal("Update measured the file again when the result arrived")
			}
		})
	}
}

// A view with no session yet has nothing to measure, and keeps the poll
// going for the session it will open.
func TestWaitingViewKeepsPolling(t *testing.T) {
	for name, m := range map[string]tea.Model{
		"watch":     NewWaitingModel(t.TempDir(), "/work/webapp", true, true),
		"breakdown": NewWaitingBreakdownModel(t.TempDir(), "/work/webapp", true, true),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, cmd := m.Update(subagentPollMsg(time.Now()))
			if !deliversPollTick(cmd) {
				t.Fatal("a waiting view's poll tick did not schedule the next")
			}
		})
	}
}
