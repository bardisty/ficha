//go:build linux || darwin

package tui

import "golang.org/x/sys/unix"

// canSuspend reports whether ctrl+z can stop the process and get it back.
// tea.Suspend sends SIGTSTP to the process group and waits for SIGCONT. A
// shell with job control runs each job in its own group; without one (a
// tmux pane or split started with a command), the group is the session's
// own, the kernel discards the stop, and the program would wait forever on
// a blank screen.
func canSuspend() bool {
	sid, err := unix.Getsid(0)
	return err == nil && unix.Getpgrp() != sid
}
