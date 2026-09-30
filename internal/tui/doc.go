// Package tui is the two live views, watch (Model) and breakdown
// (BreakdownModel). Each is a Bubble Tea program that loads a session through
// internal/analyzer, redraws when the session changes, and notices new
// sessions in the project.
//
// The package also owns the change detection behind that: fsnotify watchers
// for writes to the session file and for new sessions in the project, a poll
// of the session's agent transcripts, and polling in place of a watcher that
// can't start. Strings the static reports print the same way come from
// internal/render; the rest of each frame is drawn here.
package tui
