package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/styles"
)

const (
	sessA = "aaaaaaaa-0000-0000-0000-000000000000"
	sessB = "bbbbbbbb-0000-0000-0000-000000000000"
)

func followModel(t *testing.T, follow bool) Model {
	t.Helper()
	m := NewModel("/p/"+sessA+".jsonl", sessA, true, "", follow)
	m = sized(t, m, 80, 24)
	return load(t, m, tallAnalysis(30.05))
}

func send(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func TestSessionActivityFollowAndHint(t *testing.T) {
	tests := []struct {
		name       string
		follow     bool
		created    bool
		wantSwitch bool
		wantHint   string
	}{
		{"following: a new session switches", true, true, true, ""},
		{"following: a write to another session only hints", true, false, false, "newer activity in bbbbbbbb " + "• n to switch"},
		{"pinned: a new session only hints", false, true, false, "new session bbbbbbbb started • n to switch"},
		{"pinned: a write only hints", false, false, false, "newer activity in bbbbbbbb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := followModel(t, tt.follow)
			m = send(t, m, sessionActivityMsg{path: "/p/" + sessB + ".jsonl", id: sessB, created: tt.created})
			if switched := m.sessionID == sessB; switched != tt.wantSwitch {
				t.Fatalf("switched = %v, want %v", switched, tt.wantSwitch)
			}
			if tt.wantHint != "" && !strings.Contains(frameOf(m), tt.wantHint) {
				t.Errorf("notify row missing %q:\n%s", tt.wantHint, frameOf(m))
			}
		})
	}
}

// The switch notice names the old total and stays until a key is pressed;
// - goes back and pins.
func TestSwitchNoticeAndGoBack(t *testing.T) {
	m := followModel(t, true)
	m = send(t, m, sessionActivityMsg{path: "/p/" + sessB + ".jsonl", id: sessB, created: true})

	want := "→ new session bbbbbbbb (previous aaaaaaaa: $30.05) • - to go back"
	if !strings.Contains(frameOf(m), want) {
		t.Fatalf("missing switch notice %q:\n%s", want, frameOf(m))
	}
	m.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	if !strings.Contains(frameOf(m), want) {
		t.Error("switch notice expired without a keypress")
	}

	m = load(t, m, tallAnalysis(0.04))
	m = press(t, m, "-")
	if m.sessionID != sessA || m.followMode {
		t.Fatalf("after -: session %s follow=%v, want %s pinned", m.sessionID, m.followMode, sessA)
	}
	if !strings.Contains(frameOf(m), "switched to aaaaaaaa (previous bbbbbbbb: $0.0400)") {
		t.Errorf("go-back notice missing:\n%s", frameOf(m))
	}
	if !strings.Contains(frameOf(m), "PINNED") {
		t.Errorf("header doesn't say PINNED after going back:\n%s", frameOf(m))
	}

	m = press(t, m, "j")
	if m.switched != nil {
		t.Error("a keypress didn't clear the switch notice")
	}
	m = press(t, m, "f")
	if !m.followMode || !strings.Contains(frameOf(m), "FOLLOWING") {
		t.Errorf("f didn't resume following:\n%s", frameOf(m))
	}
}

// p is peak in breakdown, so in watch it must not change session.
func TestPDoesNotGoBack(t *testing.T) {
	m := followModel(t, true)
	m = send(t, m, sessionActivityMsg{path: "/p/" + sessB + ".jsonl", id: sessB, created: true})
	if m = press(t, m, "p"); m.sessionID != sessB || !m.followMode {
		t.Fatalf("after p: session %s follow=%v, want %s still following", m.sessionID, m.followMode, sessB)
	}
}

// n switches to the session a hint names.
func TestHintSwitchKey(t *testing.T) {
	m := followModel(t, false)
	m = press(t, m, "n")
	if m.sessionID != sessA {
		t.Fatal("n without a hint switched sessions")
	}
	m = send(t, m, sessionActivityMsg{path: "/p/" + sessB + ".jsonl", id: sessB, created: true})
	m = send(t, m, sessionActivityMsg{path: "/p/" + sessB + ".jsonl", id: sessB})
	if !strings.Contains(frameOf(m), "new session bbbbbbbb started") {
		t.Errorf("a new session's first write turned it into mere activity:\n%s", frameOf(m))
	}
	m = press(t, m, "n")
	if m.sessionID != sessB || m.hint != nil {
		t.Errorf("after n: session %s hint %v, want %s and no hint", m.sessionID, m.hint, sessB)
	}
}

func TestWatchHeaderStates(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	base := liveHeaderParams{
		sessionID: sessA, noColor: true, width: 76, project: "webapp", mode: "FOLLOWING", now: now,
	}
	tests := []struct {
		name string
		edit func(*liveHeaderParams)
		want string
	}{
		{"live", func(p *liveHeaderParams) { p.lastActivity = now.Add(-12 * time.Second) },
			"webapp │ aaaaaaaa │ ● FOLLOWING │ last msg 12s ago"},
		{"idle after 5m", func(p *liveHeaderParams) { p.lastActivity = now.Add(-7 * time.Minute) },
			"webapp │ aaaaaaaa │ ○ FOLLOWING │ idle 7m"},
		{"pinned", func(p *liveHeaderParams) { p.mode = "PINNED"; p.lastActivity = now.Add(-3 * time.Hour) },
			"webapp │ aaaaaaaa │ ○ PINNED │ idle 3h"},
		{"no messages yet", func(p *liveHeaderParams) {}, "webapp │ aaaaaaaa │ ● FOLLOWING │ no messages yet"},
		{"empty, recently touched", func(p *liveHeaderParams) { p.noMessages = true; p.lastActivity = now.Add(-time.Minute) },
			"webapp │ aaaaaaaa │ ● FOLLOWING │ no messages yet"},
		{"empty and long dead", func(p *liveHeaderParams) { p.noMessages = true; p.lastActivity = now.Add(-72 * time.Hour) },
			"webapp │ aaaaaaaa │ ○ FOLLOWING │ idle 3d"},
		{"loading", func(p *liveHeaderParams) { p.loading = true }, "webapp │ aaaaaaaa │ ● FOLLOWING │ loading..."},
		{"no project", func(p *liveHeaderParams) { p.project = ""; p.lastActivity = now }, "aaaaaaaa │ ● FOLLOWING │ last msg 0s ago"},
		{"narrow drops the prefix, then shortens the project", func(p *liveHeaderParams) {
			p.width = 54
			p.project = "a-very-long-project-name"
			p.lastActivity = now.Add(-12 * time.Second)
		}, "a-very-long-… │ aaaaaaaa │ ● FOLLOWING │ 12s ago"},
		{"narrower drops the project", func(p *liveHeaderParams) {
			p.width = 44
			p.project = "a-very-long-project-name"
			p.lastActivity = now.Add(-12 * time.Second)
		}, "║  aaaaaaaa │ ● FOLLOWING │ 12s ago"},
		{"narrowest gives up margin before the status", func(p *liveHeaderParams) {
			p.width = minPanelWidth
			p.lastActivity = now.Add(-12 * time.Minute)
		}, "║ aaaaaaaa │ ○ FOLLOWING │ idle 12m  ║"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := base
			tt.edit(&p)
			got := renderLiveHeaderPanel(p)
			if !strings.Contains(got, tt.want) {
				t.Errorf("header missing %q:\n%s", tt.want, got)
			}
			assertPanelLinesAligned(t, got, tt.name)
		})
	}
}

// A removed session file is an error in the notify row; the last data stays
// on screen and no reload is attempted.
func TestWatchSessionFileRemoved(t *testing.T) {
	m := followModel(t, true)
	w := newTestWatcher(t, t.TempDir()+"/s.jsonl")
	m = send(t, m, watcherStartedMsg{watcher: w})

	updated, cmd := m.Update(fileWatchErrMsg{err: errSessionFileGone, watcher: w})
	m = updated.(Model)
	if m.loading {
		t.Error("removed file triggered a reload")
	}
	if cmd == nil {
		t.Error("removed file didn't re-arm the waiter (a re-create must still be seen)")
	}
	view := frameOf(m)
	if !strings.Contains(view, "! session file removed • r to retry") &&
		!strings.Contains(view, "⚠ session file removed • r to retry") {
		t.Errorf("notify row missing the removal:\n%s", view)
	}
	if !strings.Contains(view, "$30.05 TOTAL") {
		t.Errorf("last data dropped:\n%s", view)
	}
	if strings.Contains(view, "Error") {
		t.Errorf("header still shows a bare Error:\n%s", view)
	}
}

func TestDescribeErr(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{errSessionFileGone, "session file removed"},
		{fmt.Errorf("opening: %w", fs.ErrNotExist), "session file removed"},
		{fmt.Errorf("opening: %w", fs.ErrPermission), "can't read session file (permission denied)"},
		{errors.New("event queue overflow"), "event queue overflow"},
	}
	for _, tt := range tests {
		if got := describeErr(tt.err); got != tt.want {
			t.Errorf("describeErr(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

// The window title carries the project and total once a session has loaded,
// and breakdown sets none.
func TestWatchWindowTitle(t *testing.T) {
	m := NewModel("/p/"+sessA+".jsonl", sessA, true, "", true)
	m.project = "webapp"
	m = sized(t, m, 80, 24)
	if got := m.View().WindowTitle; got != "" {
		t.Errorf("title before the first load = %q, want none", got)
	}
	m = load(t, m, tallAnalysis(30.05))
	if got, want := m.View().WindowTitle, "ficha • webapp • $30.05"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got := NewBreakdownModel("/p/"+sessA+".jsonl", sessA, true, "", true).View().WindowTitle; got != "" {
		t.Errorf("breakdown title = %q, want none", got)
	}
}

// Both views draw on the alternate screen, so quitting gives the terminal
// back as it was.
func TestViewsUseTheAlternateScreen(t *testing.T) {
	for name, m := range map[string]tea.Model{
		"watch":     NewModel("/p/s.jsonl", "s", true, "", false),
		"breakdown": NewBreakdownModel("/p/s.jsonl", "s", true, "", false),
	} {
		if !m.View().AltScreen {
			t.Errorf("%s doesn't ask for the alternate screen", name)
		}
	}
}

func TestLastActivity(t *testing.T) {
	end := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	mod := end.Add(-time.Hour)
	a := &models.SessionAnalysis{EndTime: end, Messages: []models.MessageAnalysis{{Timestamp: end.Add(time.Minute)}}}
	if got, fromFile := lastActivity(a, mod); !got.Equal(end.Add(time.Minute)) || fromFile {
		t.Errorf("got %v, fromFile %v; want the newest message, not from the file", got, fromFile)
	}
	if got, fromFile := lastActivity(&models.SessionAnalysis{}, mod); !got.Equal(mod) || !fromFile {
		t.Errorf("got %v, fromFile %v; want the file mtime for a session with no messages", got, fromFile)
	}
}

// A transcript whose mtime equals its last message's timestamp (a coarse
// filesystem clock, a copied or restored file) still has messages. Drive the
// real load so the flag is checked where the mtime enters.
func TestHeaderWhenMtimeEqualsLastMessage(t *testing.T) {
	stamp := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), sessA+".jsonl")
	line := `{"type":"assistant","timestamp":"2026-02-01T10:00:00Z","requestId":"r1","message":{"id":"m1","model":"claude-opus-4-8","usage":{"input_tokens":100,"output_tokens":50}}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	m := NewModel(path, sessA, true, "", false)
	m.now = func() time.Time { return stamp.Add(12 * time.Second) }
	m = sized(t, m, 80, 24)
	msg, ok := m.loadAnalysis().(analysisMsg)
	if !ok {
		t.Fatalf("loadAnalysis returned %T, want analysisMsg", m.loadAnalysis())
	}
	if !msg.modTime.Equal(stamp) {
		t.Skipf("filesystem stored mtime %v, want %v", msg.modTime, stamp)
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if header := m.renderHeaderPanel(80); !strings.Contains(header, "last msg 12s ago") {
		t.Errorf("header should read the message's age:\n%s", header)
	}
}

// After following a new session, the view must keep watching for the next
// one: the waiter that reported the switch has exited, so the switch has to
// start another.
func TestFollowKeepsWatchingAfterSwitch(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, sessA+".jsonl")
	if err := os.WriteFile(pathA, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	sw := NewSessionWatcher(dir, sessA)
	if err := sw.Start(); err != nil {
		t.Fatal(err)
	}
	m := NewModel(pathA, sessA, true, "", true)
	m.sessionWatcher = sw
	t.Cleanup(func() { shutdownWatchers(m.closeOnce, m.closing, m.done, m.watcher, sw, m.wg) })

	pathB := filepath.Join(dir, sessB+".jsonl")
	_, cmd := m.Update(sessionActivityMsg{path: pathB, id: sessB, created: true})

	msgs := make(chan tea.Msg, 8)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, sub := range batch {
					run(sub)
				}
				return
			}
			msgs <- msg
		}()
	}
	run(cmd)

	sessC := "cccccccc-0000-0000-0000-000000000000"
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, sessC+".jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg := <-msgs:
			if ev, ok := msg.(sessionActivityMsg); ok && ev.id == sessC && ev.created {
				return
			}
			if w, ok := msg.(watcherStartedMsg); ok {
				w.watcher.Close()
			}
		case <-deadline:
			t.Fatal("no session event for a session created after the switch")
		}
	}
}

func TestMessageAge(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		ago  time.Duration
		want string
	}{
		{-5 * time.Second, "0s ago"}, // clock skew
		{12 * time.Second, "12s ago"},
		{59 * time.Second, "59s ago"},
		{7 * time.Minute, "7m ago"},
		{3 * time.Hour, "3h ago"},
	}
	for _, tt := range tests {
		if got := messageAge(now.Add(-tt.ago), now); got != tt.want {
			t.Errorf("messageAge(-%v) = %q, want %q", tt.ago, got, tt.want)
		}
	}
}

// A write to B reported just before a key switch to B lands after it; it
// must not hint at the session already on screen or clobber the go-back.
func TestStaleActivityForCurrentSessionIgnored(t *testing.T) {
	m := followModel(t, false)
	m = send(t, m, sessionActivityMsg{path: "/p/" + sessB + ".jsonl", id: sessB})
	m = press(t, m, "n")
	m = send(t, m, sessionActivityMsg{path: "/p/" + sessB + ".jsonl", id: sessB})
	if m.hint != nil {
		t.Errorf("hint names the current session: %+v", m.hint)
	}
	if m.prevSessionID != sessA {
		t.Errorf("go-back target = %s, want %s", m.prevSessionID, sessA)
	}
}

// Once a hint fades from the screen, n no longer acts on it.
func TestExpiredHintIgnoredByN(t *testing.T) {
	m := followModel(t, false)
	m = send(t, m, sessionActivityMsg{path: "/p/" + sessB + ".jsonl", id: sessB})
	m.now = func() time.Time { return time.Now().Add(idleAfter + time.Minute) }
	if strings.Contains(frameOf(m), "n to switch") {
		t.Fatal("expired hint still shown")
	}
	m = press(t, m, "n")
	if m.sessionID != sessA {
		t.Error("n switched to a hint no longer on screen")
	}
}

// followView drives watch and breakdown through the same session-following
// cases. Both open on sessA at 80x24 with a total of $30.05, its last write
// a minute before their pinned clock.
type followView struct {
	name   string
	open   func(t *testing.T, follow bool) tea.Model
	id     func(tea.Model) string
	follow func(tea.Model) bool
	notify func(tea.Model) string
}

var followViews = []followView{
	{
		name: "watch",
		open: func(t *testing.T, follow bool) tea.Model {
			t.Helper()
			m := NewModel("/p/"+sessA+".jsonl", sessA, true, "", follow)
			m.now = func() time.Time { return goldenTime(10, 2, 0) }
			a := tallAnalysis(30.05)
			a.EndTime = goldenTime(10, 1, 0)
			return load(t, sized(t, m, 80, 24), a)
		},
		id:     func(m tea.Model) string { return m.(Model).sessionID },
		follow: func(m tea.Model) bool { return m.(Model).followMode },
		notify: func(m tea.Model) string { return m.(Model).renderNotifyRow(80) },
	},
	{
		name: "breakdown",
		open: func(t *testing.T, follow bool) tea.Model {
			t.Helper()
			m := NewBreakdownModel("/p/"+sessA+".jsonl", sessA, true, "", follow)
			m.now = func() time.Time { return goldenTime(10, 2, 0) }
			rows := chromeRows(goldenTime(10, 0, 0), 2)
			rows[0].Cost.TotalCost, rows[1].Cost.TotalCost = 30, 0.05
			var model tea.Model = m
			model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			model, _ = model.Update(breakdownMsgsMsg{messages: rows, insights: &models.MessageInsights{}})
			return model
		},
		id:     func(m tea.Model) string { return m.(BreakdownModel).sessionID },
		follow: func(m tea.Model) bool { return m.(BreakdownModel).followMode },
		notify: func(m tea.Model) string { return m.(BreakdownModel).renderNotifyRow() },
	},
}

var (
	createdB  = sessionActivityMsg{path: "/p/" + sessB + ".jsonl", id: sessB, created: true}
	activityB = sessionActivityMsg{path: "/p/" + sessB + ".jsonl", id: sessB}
)

// Pinned, both views hint at a new session, and n switches to it with the
// same notice.
func TestPinnedViewsHintAndSwitchWithN(t *testing.T) {
	for _, v := range followViews {
		t.Run(v.name, func(t *testing.T) {
			m, _ := v.open(t, false).Update(createdB)
			if v.id(m) != sessA {
				t.Fatal("a pinned view switched to a new session")
			}
			if got, want := v.notify(m), "new session bbbbbbbb started • n to switch"; !strings.Contains(got, want) {
				t.Fatalf("notify row = %q, want %q", got, want)
			}
			m = press(t, m, "n")
			if v.id(m) != sessB || v.follow(m) {
				t.Fatalf("after n: session %s following %v, want %s pinned", v.id(m), v.follow(m), sessB)
			}
			if got, want := v.notify(m), "→ switched to bbbbbbbb (previous aaaaaaaa: $30.05) • - to go back"; !strings.Contains(got, want) {
				t.Errorf("notify row = %q, want %q", got, want)
			}
		})
	}
}

// Following, both views take a session that starts, with the same notice,
// and never jump to an older session that's written to.
func TestFollowingViewsTakeNewSessionsOnly(t *testing.T) {
	for _, v := range followViews {
		t.Run(v.name, func(t *testing.T) {
			m, _ := v.open(t, true).Update(activityB)
			if v.id(m) != sessA {
				t.Fatal("a following view jumped to an older session that was written to")
			}
			if got, want := v.notify(m), "newer activity in bbbbbbbb • n to switch"; !strings.Contains(got, want) {
				t.Errorf("notify row = %q, want %q", got, want)
			}

			m, _ = v.open(t, true).Update(createdB)
			if v.id(m) != sessB {
				t.Fatal("a following view didn't take a new session")
			}
			if got, want := v.notify(m), "→ new session bbbbbbbb (previous aaaaaaaa: $30.05) • - to go back"; !strings.Contains(got, want) {
				t.Errorf("notify row = %q, want %q", got, want)
			}
		})
	}
}

// f turning following on takes a session that started while pinned, but not
// one that was only written to: following never takes those.
func TestFollowKeyOverHint(t *testing.T) {
	for _, v := range followViews {
		t.Run(v.name, func(t *testing.T) {
			m, _ := v.open(t, false).Update(createdB)
			m = press(t, m, "f")
			if v.id(m) != sessB || !v.follow(m) {
				t.Errorf("f over a new-session hint: session %s following %v, want %s following", v.id(m), v.follow(m), sessB)
			}

			m, _ = v.open(t, false).Update(activityB)
			m = press(t, m, "f")
			if v.id(m) != sessA || !v.follow(m) {
				t.Errorf("f over an activity hint: session %s following %v, want %s following", v.id(m), v.follow(m), sessA)
			}
		})
	}
}

const sessC = "cccccccc-0000-0000-0000-000000000000"

var (
	createdC  = sessionActivityMsg{path: "/p/" + sessC + ".jsonl", id: sessC, created: true}
	activityC = sessionActivityMsg{path: "/p/" + sessC + ".jsonl", id: sessC}
)

// In both views the switch notice stays until a key when nothing newer
// arrives: a clock tick and ? leave it, and any other key drops it.
func TestSwitchNoticeLastsUntilAKey(t *testing.T) {
	const notice = "→ new session bbbbbbbb (previous aaaaaaaa: $30.05) • - to go back"
	for _, v := range followViews {
		t.Run(v.name, func(t *testing.T) {
			m, _ := v.open(t, true).Update(createdB)
			if v.id(m) != sessB {
				t.Fatal("setup: a following view didn't take the new session")
			}
			m, _ = m.Update(clockMsg{})
			m = press(t, m, "?", "?")
			if got := v.notify(m); !strings.Contains(got, notice) {
				t.Errorf("with nothing newer the notice should stay: %q", got)
			}
			if m = press(t, m, "k"); v.notify(m) != "" {
				t.Errorf("a key should drop the notice: %q", v.notify(m))
			}
		})
	}
}

// In both views a hint that arrives after a switch takes the notify row at
// once: a session that starts, or another written to. Writes to the session
// just left aren't news, since the notice names it and the key back, and
// nor are more writes to a session hinted before the switch.
func TestHintAfterSwitchReplacesNotice(t *testing.T) {
	for _, v := range followViews {
		t.Run(v.name, func(t *testing.T) {
			m, _ := v.open(t, false).Update(createdB)
			m = press(t, m, "n")
			m, _ = m.Update(createdC)
			if got, want := v.notify(m), "new session cccccccc started • n to switch"; !strings.Contains(got, want) {
				t.Errorf("a new session after a switch: notify row %q, want %q", got, want)
			}

			m, _ = v.open(t, false).Update(createdB)
			m = press(t, m, "n")
			m, _ = m.Update(activityC)
			if got, want := v.notify(m), "newer activity in cccccccc • n to switch"; !strings.Contains(got, want) {
				t.Errorf("activity after a switch: notify row %q, want %q", got, want)
			}

			m, _ = v.open(t, false).Update(createdB)
			m = press(t, m, "n")
			for range 2 {
				m, _ = m.Update(sessionActivityMsg{path: "/p/" + sessA + ".jsonl", id: sessA})
				if got, want := v.notify(m), "→ switched to bbbbbbbb"; !strings.Contains(got, want) {
					t.Errorf("a write to the session just left took the row: %q", got)
				}
			}

			m, _ = v.open(t, true).Update(activityC)
			m, _ = m.Update(createdB)
			if v.id(m) != sessB {
				t.Fatal("setup: a following view didn't take the new session")
			}
			// The session left and the one hinted before both stay live.
			for _, msg := range []sessionActivityMsg{activityC, {path: "/p/" + sessA + ".jsonl", id: sessA}, activityC} {
				m, _ = m.Update(msg)
				if got, want := v.notify(m), "→ new session bbbbbbbb"; !strings.Contains(got, want) {
					t.Errorf("a write to %s took the row: %q", msg.id[:8], got)
				}
			}
		})
	}
}

// A session already writing before a key switch isn't news after it, even
// though opening the hinted session empties the view's hint.
func TestLiveSessionsKeepNoticeAfterKeySwitch(t *testing.T) {
	const sessD = "dddddddd-0000-0000-0000-000000000000"
	activityA := sessionActivityMsg{path: "/p/" + sessA + ".jsonl", id: sessA}
	activityD := sessionActivityMsg{path: "/p/" + sessD + ".jsonl", id: sessD}
	for _, v := range followViews {
		t.Run(v.name, func(t *testing.T) {
			// Pinned, C writing, B starts, n to B.
			m, _ := v.open(t, false).Update(activityC)
			m, _ = m.Update(createdB)
			m = press(t, m, "n")
			m, _ = m.Update(activityC)
			if got := v.notify(m); !strings.Contains(got, "→ switched to bbbbbbbb") {
				t.Errorf("C, then n to B: %q", got)
			}

			// Pinned, C and D writing, n to C.
			m, _ = v.open(t, false).Update(activityD)
			m, _ = m.Update(activityC)
			m = press(t, m, "n")
			m, _ = m.Update(activityD)
			if got := v.notify(m); !strings.Contains(got, "→ switched to cccccccc") {
				t.Errorf("D and C, then n to C: %q", got)
			}

			// Following, C writing, B starts, A writes, - back to A.
			m, _ = v.open(t, true).Update(activityC)
			for _, msg := range []tea.Msg{createdB, activityC, activityA} {
				m, _ = m.Update(msg)
			}
			m = press(t, m, "-")
			m, _ = m.Update(activityC)
			if got := v.notify(m); !strings.Contains(got, "→ switched to aaaaaaaa") {
				t.Errorf("- back to A with C writing: %q", got)
			}
		})
	}
}

// A write is news when its session just started or hadn't written within
// idleAfter; the session a view leaves counts as writing.
func TestSessionsSeenNews(t *testing.T) {
	now := goldenTime(12, 0, 0)
	var seen sessionsSeen
	write := func(id string, created bool, at time.Time) bool {
		return seen.news(sessionActivityMsg{id: id, created: created}, at)
	}
	if !write("c", false, now) {
		t.Error("a session's first write should be news")
	}
	if write("c", false, now.Add(time.Second)) {
		t.Error("a second write shouldn't be")
	}
	if !write("c", false, now.Add(time.Second+idleAfter)) {
		t.Error("a write after idleAfter's quiet should be")
	}
	if !write("d", true, now) {
		t.Error("a session that just started should be")
	}
	seen.left("a", now.Add(-time.Minute), time.Time{}, now)
	if write("a", false, now.Add(time.Minute)) {
		t.Error("the session a view left while it was writing shouldn't be")
	}
	seen.left("b", now.Add(-time.Hour), time.Time{}, now)
	if !write("b", false, now.Add(time.Minute)) {
		t.Error("the session a view left after an hour's quiet should be when it wakes")
	}
}

// atClock pins a followView's clock to now.
func atClock(m tea.Model, now time.Time) tea.Model {
	switch v := m.(type) {
	case Model:
		v.now = func() time.Time { return now }
		return v
	case BreakdownModel:
		v.now = func() time.Time { return now }
		return v
	}
	return m
}

// The session a view leaves after hours of quiet is news when it wakes, as
// any other session would be.
func TestQuietLeftSessionIsNewsWhenItWakes(t *testing.T) {
	activityA := sessionActivityMsg{path: "/p/" + sessA + ".jsonl", id: sessA}
	for _, v := range followViews {
		t.Run(v.name, func(t *testing.T) {
			m, _ := atClock(v.open(t, true), goldenTime(12, 0, 0)).Update(createdB)
			if v.id(m) != sessB {
				t.Fatal("setup: a following view didn't take the new session")
			}
			m, _ = atClock(m, goldenTime(12, 1, 0)).Update(activityA)
			if got, want := v.notify(m), "newer activity in aaaaaaaa • n to switch"; !strings.Contains(got, want) {
				t.Errorf("notify row %q, want %q", got, want)
			}
		})
	}
}

// loadAt lands a load of a followView's current session whose newest priced
// message is at lastMsg and whose file last changed at mtime.
func loadAt(t *testing.T, m tea.Model, lastMsg, mtime time.Time) tea.Model {
	t.Helper()
	switch v := m.(type) {
	case Model:
		a := tallAnalysis(1)
		a.EndTime = lastMsg
		u, _ := v.Update(analysisMsg{analysis: a, modTime: mtime})
		return u
	case BreakdownModel:
		u, _ := v.Update(breakdownMsgsMsg{messages: chromeRows(lastMsg, 1), insights: &models.MessageInsights{}, modTime: mtime})
		return u
	}
	return m
}

// A prompt is a write with no priced message. Following on A, quiet six
// minutes until its user sends one, then B starts: A's reply 20s later isn't
// news.
func TestLeftSessionPromptCountsAsWrite(t *testing.T) {
	activityA := sessionActivityMsg{path: "/p/" + sessA + ".jsonl", id: sessA}
	for _, v := range followViews {
		t.Run(v.name, func(t *testing.T) {
			m := atClock(v.open(t, true), goldenTime(10, 7, 0))
			m = loadAt(t, m, goldenTime(10, 1, 0), goldenTime(10, 7, 0))
			if m, _ = m.Update(createdB); v.id(m) != sessB {
				t.Fatal("setup: a following view didn't take the new session")
			}
			m, _ = atClock(m, goldenTime(10, 7, 20)).Update(activityA)
			if got := v.notify(m); !strings.Contains(got, "→ new session bbbbbbbb") {
				t.Errorf("A wrote 20s before the switch, yet its next write took the row: %q", got)
			}
		})
	}
}

// Leaving a session never moves its last seen write back. Pinned on A, B
// writes a prompt (its last priced message is older); n to B, then - back:
// B's reply 40s after its prompt isn't news.
func TestLeftSessionKeepsNewerSeenWrite(t *testing.T) {
	for _, v := range followViews {
		t.Run(v.name, func(t *testing.T) {
			m, _ := atClock(v.open(t, false), goldenTime(10, 6, 0)).Update(activityB)
			if m = press(t, atClock(m, goldenTime(10, 6, 2)), "n"); v.id(m) != sessB {
				t.Fatal("setup: n didn't switch to B")
			}
			m = loadAt(t, m, goldenTime(10, 1, 30), time.Time{})
			if m = press(t, atClock(m, goldenTime(10, 6, 20)), "-"); v.id(m) != sessA {
				t.Fatal("setup: - didn't go back to A")
			}
			m, _ = atClock(m, goldenTime(10, 6, 40)).Update(activityB)
			if got := v.notify(m); !strings.Contains(got, "→ switched to aaaaaaaa") {
				t.Errorf("B was seen writing 40s ago, yet took the row: %q", got)
			}
		})
	}
}

// A switch drops the old session's file time: leaving B before its first
// load lands mustn't record B at A's, which would hide B's first write.
func TestLeftSessionBeforeItsLoad(t *testing.T) {
	for _, v := range followViews {
		t.Run(v.name, func(t *testing.T) {
			m := atClock(v.open(t, true), goldenTime(10, 7, 0))
			m = loadAt(t, m, goldenTime(10, 1, 0), goldenTime(10, 7, 0))
			if m, _ = m.Update(createdB); v.id(m) != sessB {
				t.Fatal("setup: a following view didn't take the new session")
			}
			if m = press(t, m, "-"); v.id(m) != sessA {
				t.Fatal("setup: - didn't go back to A")
			}
			m, _ = atClock(m, goldenTime(10, 7, 30)).Update(activityB)
			if got, want := v.notify(m), "newer activity in bbbbbbbb"; !strings.Contains(got, want) {
				t.Errorf("notify row %q, want %q", got, want)
			}
		})
	}
}

// A narrow notify row gives up the previous total, then the lead's words,
// before the key the row is for, in Unicode and ASCII alike.
func TestNotifyRowKeepsItsKeyWhenNarrow(t *testing.T) {
	notice := switchNotice{auto: true, hadTotal: true, prevTotal: 1234.56}
	created := sessionHint{id: sessB, created: true}
	for _, ascii := range []bool{false, true} {
		t.Run(fmt.Sprintf("ascii=%t", ascii), func(t *testing.T) {
			if ascii {
				useASCII(t)
			}
			arrow, bullet := styles.Arrow, styles.Bullet
			cases := []struct {
				name, got, want string
			}{
				{"notice at 80", switchNoticeText(notice, sessB, sessA, true, 80),
					arrow + " new session bbbbbbbb (previous aaaaaaaa: $1,234.56) " + bullet + " - to go back"},
				{"notice at 60", switchNoticeText(notice, sessB, sessA, true, 60),
					arrow + " new session bbbbbbbb (previous $1,234.56) " + bullet + " - to go back"},
				{"notice at 50", switchNoticeText(notice, sessB, sessA, true, 50),
					arrow + " new session bbbbbbbb " + bullet + " - to go back"},
				{"notice at 30", switchNoticeText(notice, sessB, sessA, true, 30),
					arrow + " bbbbbbbb " + bullet + " - to go back"},
				{"switched at 30", switchNoticeText(switchNotice{}, sessB, sessA, true, 30),
					arrow + " bbbbbbbb " + bullet + " - to go back"},
				{"nothing to go back to at 40", switchNoticeText(notice, sessB, sessA, false, 40),
					arrow + " new session bbbbbbbb"},
				{"hint at 40", hintText(created, 40), "new session bbbbbbbb " + bullet + " n to switch"},
				{"hint at 30", hintText(created, 30), "bbbbbbbb " + bullet + " n to switch"},
				{"activity hint at 36", hintText(sessionHint{id: sessB}, 36), "activity in bbbbbbbb " + bullet + " n to switch"},
				{"activity hint at 30", hintText(sessionHint{id: sessB}, 30), "bbbbbbbb " + bullet + " n to switch"},
			}
			for _, c := range cases {
				if c.got != c.want {
					t.Errorf("%s: %q, want %q", c.name, c.got, c.want)
				}
			}
			// Wherever the key fits, the row ends in it and fits, and the
			// total shows wherever its short form does.
			full := lipgloss.Width(arrow + " new session bbbbbbbb (previous $1,234.56) " + bullet + " - to go back")
			for width := 24; width <= 100; width++ {
				got := switchNoticeText(notice, sessB, sessA, true, width)
				if w := lipgloss.Width(got); w > width || !strings.HasSuffix(got, " - to go back") {
					t.Errorf("width %d: %q (%d columns)", width, got, w)
				}
				if hasTotal := strings.Contains(got, "$1,234.56"); hasTotal != (width >= full) {
					t.Errorf("width %d: total shown %v, its short form is %d columns", width, hasTotal, full)
				}
				if hint := hintText(created, width); lipgloss.Width(hint) > width || !strings.HasSuffix(hint, " n to switch") {
					t.Errorf("width %d: hint %q", width, hint)
				}
			}
		})
	}
}

// Both views fit the notice and the hint to their own width at 40 and 60
// columns.
func TestNotifyRowKeepsItsKeyInBothViews(t *testing.T) {
	rows := map[string]func(t *testing.T, width int, follow bool, msg tea.Msg) string{
		"watch": func(t *testing.T, width int, follow bool, msg tea.Msg) string {
			m := followModel(t, follow)
			return send(t, m, msg).renderNotifyRow(width)
		},
		"breakdown": func(t *testing.T, width int, follow bool, msg tea.Msg) string {
			// breakdown's row is the panel's width, two columns in.
			m := loadedBreakdown(t, width+2, 24, chromeRows(goldenTime(10, 0, 0), 5))
			m.followMode = follow
			updated, _ := m.Update(msg)
			return updated.(BreakdownModel).renderNotifyRow()
		},
	}
	hints := map[int]string{40: "new session bbbbbbbb • n to switch", 60: "new session bbbbbbbb started • n to switch"}
	for name, row := range rows {
		for _, width := range []int{40, 60} {
			if got := stripANSI(row(t, width, true, createdB)); !strings.HasSuffix(got, " • - to go back") || lipgloss.Width(strings.TrimSpace(got)) > width {
				t.Errorf("%s at %d: notice %q", name, width, got)
			}
			if got := stripANSI(row(t, width, false, createdB)); strings.TrimSpace(got) != hints[width] {
				t.Errorf("%s at %d: hint %q", name, width, got)
			}
		}
	}
}
