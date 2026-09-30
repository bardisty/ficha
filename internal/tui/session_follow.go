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
