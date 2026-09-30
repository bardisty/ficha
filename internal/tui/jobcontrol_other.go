//go:build !linux && !darwin

package tui

// canSuspend is false where ficha has no job-control check; see the unix
// version.
var canSuspend = func() bool { return false }
