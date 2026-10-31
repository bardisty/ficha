package tui

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// Session following, shared by watch and breakdown. Both wait on the session
// watcher for activity in the project's other sessions and decide the same
// way what to do with it; only how each view draws the result differs.

// switchNotice describes the last session switch for the notify row.
type switchNotice struct {
	auto      bool    // followed a new session, rather than a key press
	prevTotal float64 // the previous session's total when it was left
	hadTotal  bool    // prevTotal is known (the previous session had loaded)
}

// sessionHint is activity in another session the view didn't switch to.
type sessionHint struct {
	path    string
	id      string
	created bool
	at      time.Time
}

// activityOutcome is what a view does with another session's activity.
type activityOutcome int

const (
	activityIgnored activityOutcome = iota // nothing to show; keep waiting
	activitySwitch                         // open the other session
	activityHint                           // show it as a hint; n switches
)

// onSessionActivity decides what a view does with activity in another
// session. A view with no session yet takes any. A following view takes only
// a session that just started, never a write to an older one: with two
// sessions live in one project, following writes would flip between them on
// every message. Anything else becomes the hint, returned as the view's new
// one. A new session's first writes follow its Create, so a hint keeps
// calling it new.
func onSessionActivity(msg sessionActivityMsg, currentID string, waiting, following bool, hint *sessionHint, now time.Time) (activityOutcome, *sessionHint) {
	switch {
	case msg.id == currentID:
		// Reported before a key switch landed on this session: stale.
		return activityIgnored, hint
	case waiting, msg.created && following:
		return activitySwitch, hint
	}
	created := msg.created || (hint != nil && hint.id == msg.id && hint.created)
	return activityHint, &sessionHint{path: msg.path, id: msg.id, created: created, at: now}
}

// sessionsSeen is when each other session in the project last wrote, as a
// view saw it. It decides what's news on the notify row: the switch notice
// and breakdown's p selection give way only to a hint about a session that
// wasn't already writing. The view's one hint can't tell: it names only the
// latest session to write, and empties when the view opens that session.
type sessionsSeen map[string]time.Time

// news records a write to another session and reports whether it's news:
// the session just started, or hadn't written within idleAfter, the time a
// hint takes to fade. A live session writes every few hundred milliseconds,
// so counting each write would take the row before it could be read.
func (s *sessionsSeen) news(msg sessionActivityMsg, now time.Time) bool {
	last, ok := (*s)[msg.id]
	s.saw(msg.id, now)
	return msg.created || !ok || now.Sub(last) >= idleAfter
}

// left records the session a view leaves in a switch at its newest write:
// lastMsg, its newest priced message, or fileWrite, when its file last
// changed, whichever is later. A prompt or a tool result is a write too, and
// adds no priced message. If that was recent, the session's next write isn't
// news: the switch notice names it and the key back to it. One quiet for
// idleAfter or longer is news when it wakes, like any other. left never moves
// an entry back, since news may have seen the session write since then.
func (s *sessionsSeen) left(id string, lastMsg, fileWrite, now time.Time) {
	last := lastMsg
	if fileWrite.After(last) {
		last = fileWrite
	}
	if now.Sub(last) < idleAfter && last.After((*s)[id]) {
		s.saw(id, last)
	}
}

// saw records that session id was writing at now, and forgets the sessions
// quiet for longer than news looks back.
func (s *sessionsSeen) saw(id string, now time.Time) {
	if id == "" {
		return
	}
	if *s == nil {
		*s = sessionsSeen{}
	}
	for other, at := range *s {
		if now.Sub(at) >= idleAfter {
			delete(*s, other)
		}
	}
	(*s)[id] = now
}

// hintShowing reports whether a hint is still on screen: it fades once the
// other session has been quiet for idleAfter, and n stops acting on it.
func hintShowing(h *sessionHint, now time.Time) bool {
	return h != nil && now.Sub(h.at) < idleAfter
}

// followTarget is the session f switches to as it turns following on: the
// hinted one, if it started while the view was pinned. Following means
// taking sessions as they start, so a hint about a write to an older session
// doesn't count. nil means stay put.
func followTarget(h *sessionHint, now time.Time) *sessionHint {
	if hintShowing(h, now) && h.created {
		return h
	}
	return nil
}

// switchNoticeText is the notify row after a switch, fitted to width: the
// session now open, the total of the one left behind, and the key back to
// it. The key is what the row is for, so a narrow row gives up the old
// session's ID, then its total, then the lead's words, and keeps the key
// to the last. The total goes after the ID because nothing else on screen
// still shows it.
func switchNoticeText(n switchNotice, id, prevID string, canGoBack bool, width int) string {
	short := render.TruncateID(id, sessionIDDisplayLen)
	lead := styles.Arrow + " switched to " + short
	if n.auto {
		lead = styles.Arrow + " new session " + short
	}
	forms := []string{lead, styles.Arrow + " " + short}
	if n.hadTotal {
		total := render.Cost(n.prevTotal)
		forms = append([]string{
			lead + fmt.Sprintf(" (previous %s: %s)", render.TruncateID(prevID, sessionIDDisplayLen), total),
			lead + " (previous " + total + ")",
		}, forms...)
	}
	tail := ""
	if canGoBack {
		tail = " " + styles.Bullet + " - to go back"
	}
	return fitBeforeTail(forms, tail, width)
}

// hintText is the notify row for a hint, fitted to width. As in
// switchNoticeText, the words go before the key does.
func hintText(h sessionHint, width int) string {
	id := render.TruncateID(h.id, sessionIDDisplayLen)
	// "activity" outlasts "newer": a bare ID would read the same as a
	// new session's.
	forms := []string{"newer activity in " + id, "activity in " + id, id}
	if h.created {
		forms = []string{"new session " + id + " started", "new session " + id, id}
	}
	return fitBeforeTail(forms, " "+styles.Bullet+" n to switch", width)
}

// fitBeforeTail is the first of forms, fullest first, that fits width with
// tail after it. When none does, the last is cut to leave tail whole, as
// errNotice does; the caller's withinWidth handles a row too narrow even
// for the tail.
func fitBeforeTail(forms []string, tail string, width int) string {
	for _, form := range forms {
		if lipgloss.Width(form+tail) <= width {
			return form + tail
		}
	}
	last := forms[len(forms)-1]
	return withEllipsis(last, max(width-lipgloss.Width(tail), 1)) + tail
}
