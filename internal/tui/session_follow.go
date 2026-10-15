package tui

import (
	"fmt"
	"time"

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

// left records the session a view leaves in a switch at last, its newest
// write. If that was recent, the session's next write isn't news: the switch
// notice names it and the key back to it. One quiet for idleAfter or longer
// is news when it wakes, like any other.
func (s *sessionsSeen) left(id string, last, now time.Time) {
	if now.Sub(last) < idleAfter {
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

// switchNoticeText is the notify row after a switch: the session now open,
// the total of the one left behind, and the key back to it.
func switchNoticeText(n switchNotice, id, prevID string, canGoBack bool) string {
	lead := "switched to " + render.TruncateID(id, sessionIDDisplayLen)
	if n.auto {
		lead = "new session " + render.TruncateID(id, sessionIDDisplayLen)
	}
	text := styles.Arrow + " " + lead
	if n.hadTotal {
		text += fmt.Sprintf(" (previous %s: %s)",
			render.TruncateID(prevID, sessionIDDisplayLen), render.Cost(n.prevTotal))
	}
	if canGoBack {
		text += " " + styles.Bullet + " - to go back"
	}
	return text
}

// hintText is the notify row for a hint.
func hintText(h sessionHint) string {
	id := render.TruncateID(h.id, sessionIDDisplayLen)
	text := "newer activity in " + id
	if h.created {
		text = "new session " + id + " started"
	}
	return text + " " + styles.Bullet + " n to switch"
}
