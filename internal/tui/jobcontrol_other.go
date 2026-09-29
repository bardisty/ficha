//go:build !linux && !darwin

package tui

// canSuspend is false where ficha has no job-control check; see the unix
// version.
func canSuspend() bool { return false }
