//go:build linux

package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The query a terminal answers with its background color (OSC 11), and the
// cursor position request termenv sends after it: a terminal that doesn't
// know OSC 11 still answers that one, which ends the wait.
const (
	backgroundQuery = "\x1b]11;?"
	cursorQuery     = "\x1b[6n"
	whiteBackground = "\x1b]11;rgb:ffff/ffff/ffff\x1b\\"
	cursorAt        = "\x1b[1;1R"
)

// ptyRun runs ficha on a pseudo-terminal, which is the only place it sends
// the background query, and returns everything it wrote and how long it
// ran. With answer set, the test plays a terminal with a white background.
// Without it the test stays silent, like a pty with nothing behind it.
//
// The child is this test binary: TestMain runs ficha when ptyArgsEnv is set.
// It gets its own session with the pty as controlling terminal, so it is the
// foreground process termenv insists on before it asks.
func ptyRun(t *testing.T, configDir string, answer bool, args ...string) (output string, took time.Duration) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty to run on: %v", err)
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
	if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: 40, Col: 100}); err != nil {
		t.Fatalf("size pty: %v", err)
	}
	tty, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}

	child := exec.Command(os.Args[0]) //nolint:gosec // the test binary starting itself
	child.Stdin, child.Stdout, child.Stderr = tty, tty, tty
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	child.Env = []string{
		ptyArgsEnv + "=" + strings.Join(args, ptyArgsSep),
		"CLAUDE_CONFIG_DIR=" + configDir,
		"HOME=" + configDir,
		"TERM=xterm-256color",
		"TZ=UTC",
	}
	start := time.Now()
	if err := child.Start(); err != nil {
		t.Fatalf("start ficha: %v", err)
	}
	_ = tty.Close()

	// Reading the master ends with an error once the child, the last
	// holder of the other end, exits.
	read := make(chan string)
	go func() {
		var out bytes.Buffer
		answered := false
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			out.Write(buf[:n])
			if answer && !answered && bytes.Contains(out.Bytes(), []byte(cursorQuery)) {
				answered = true
				_, _ = master.WriteString(whiteBackground + cursorAt)
			}
			if err != nil {
				read <- out.String()
				return
			}
		}
	}()

	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case err := <-done:
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			t.Fatalf("ficha %v: %v", args, err)
		}
	case <-time.After(30 * time.Second):
		_ = child.Process.Kill()
		t.Fatalf("ficha %v was still running after 30s", args)
	}
	took = time.Since(start)
	return <-read, took
}

// A run that prints no color sends the terminal no background query, so it
// costs nothing over a slow link and nothing on a pty that never answers.
// The test never answers, and each run must end long before the 5 seconds
// termenv would wait.
func TestPlainRunsSendNoBackgroundQuery(t *testing.T) {
	root := setupE2EFixture(t)
	for _, args := range [][]string{
		{"version"},
		{"--help"},
		{"help", "output"},
		{"show", projFlag, e2eAlphaID, "-f", "json"},
		{"show", projFlag, e2eAlphaID, "-f", "csv"},
		{"show", projFlag, e2eAlphaID, "--no-color"},
		{"list", projFlag, "-f", "json"},
		{"show", projFlag, "no-such-session"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, took := ptyRun(t, root, false, args...)
			if strings.Contains(out, backgroundQuery) {
				t.Errorf("asked the terminal for its background:\n%q", out)
			}
			if took > 4*time.Second {
				t.Errorf("took %v on a terminal that never answers", took)
			}
			if out == "" {
				t.Error("printed nothing, so the run proves nothing")
			}
		})
	}
}

// A table in color asks once, and draws with the palette for the answer.
func TestColoredTableAsksForTheBackgroundOnce(t *testing.T) {
	root := setupE2EFixture(t)
	out, _ := ptyRun(t, root, true, "show", projFlag, e2eAlphaID)
	if got := strings.Count(out, backgroundQuery); got != 1 {
		t.Fatalf("asked for the background %d times, want once:\n%q", got, out)
	}
	// The frame's gray: 242 on a light background, 245 on a dark one.
	if !strings.Contains(out, "38;5;242m") || strings.Contains(out, "38;5;245m") {
		t.Errorf("a white background should get the light palette:\n%q", out)
	}
}

// On a terminal that never answers, a table in color waits once, then draws
// the dark palette.
func TestSilentTerminalCostsOneWait(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out termenv's 5-second timeout")
	}
	root := setupE2EFixture(t)
	out, took := ptyRun(t, root, false, "show", projFlag, e2eAlphaID)
	if got := strings.Count(out, backgroundQuery); got != 1 {
		t.Fatalf("asked for the background %d times, want once:\n%q", got, out)
	}
	if took < 4*time.Second || took > 9*time.Second {
		t.Errorf("took %v, want one 5-second wait", took)
	}
	if !strings.Contains(out, "38;5;245m") || strings.Contains(out, "38;5;242m") {
		t.Errorf("no answer should get the dark palette:\n%q", out)
	}
}
