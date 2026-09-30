// Package tui is the two live views, watch (Model) and breakdown
// (BreakdownModel). Each is a Bubble Tea program that loads a session through
// internal/analyzer, redraws when the session changes, and notices new
// sessions in the project.
//
// The package also owns the change detection behind that: fsnotify watchers
// for writes to the session file and for new sessions in the project, and a
// 2-second poll of the session file and its agent transcripts. The poll runs
// whether or not a watcher started, because some filesystems accept a watch
// and never report a write. Strings the static reports print the same way
// come from internal/render; the rest of each frame is drawn here.
package tui
