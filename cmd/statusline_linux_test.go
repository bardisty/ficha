//go:build linux

package cmd

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// Run by hand, with stdin on a terminal, statusline fails at once instead of
// waiting for JSON nobody will type.
func TestStatuslineRefusesATerminalOnStdin(t *testing.T) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	defer func() { _ = master.Close() }()
	fd := int(master.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlock pty: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("pty number: %v", err)
	}
	tty, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}
	defer func() { _ = tty.Close() }()

	var out strings.Builder
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(tty)
	root.SetArgs([]string{"statusline"})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "stdin is a terminal") {
		t.Fatalf("error = %v, want one saying stdin is a terminal", err)
	}
	if code := exitCode(err); code != 2 {
		t.Errorf("exit status %d, want 2", code)
	}
	if out.String() != "" {
		t.Errorf("wrote %q", out.String())
	}
}
