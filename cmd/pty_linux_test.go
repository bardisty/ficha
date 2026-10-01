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

// ptyOpts is the terminal a pty test plays.
type ptyOpts struct {
	rows, cols uint16
	// answer makes the terminal reply to the background query with white.
	// Without it the terminal stays silent, like a pty with nothing behind
	// it.
	answer bool
	// env is added to the child's environment.
	env []string
	// quitOn makes the terminal press q once ficha has written this text,
	// which is how a live view ends.
	quitOn string
}

// screen is an ordinary terminal that doesn't answer queries.
var screen = ptyOpts{rows: 40, cols: 100}

// ptyRun runs ficha on a pseudo-terminal, which is the only place it sends
// the background query or draws a live view, and returns everything it
// wrote and how long it ran.
//
// The child is this test binary: TestMain runs ficha when ptyArgsEnv is set.
// It gets its own session with the pty as controlling terminal, so it is the
// foreground process termenv insists on before it asks.
func ptyRun(t *testing.T, configDir string, o ptyOpts, args ...string) (output string, took time.Duration) {
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
	if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: o.rows, Col: o.cols}); err != nil {
		t.Fatalf("size pty: %v", err)
	}
	tty, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}

	child := exec.Command(os.Args[0]) //nolint:gosec // the test binary starting itself
	child.Stdin, child.Stdout, child.Stderr = tty, tty, tty
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	child.Env = append([]string{
		ptyArgsEnv + "=" + strings.Join(args, ptyArgsSep),
		"CLAUDE_CONFIG_DIR=" + configDir,
		"HOME=" + configDir,
		"TERM=xterm-256color",
		"TZ=UTC",
	}, o.env...)
	start := time.Now()
	if err := child.Start(); err != nil {
		t.Fatalf("start ficha: %v", err)
	}
	_ = tty.Close()

	// Reading the master ends with an error once the child, the last
	// holder of the other end, exits.
	read := make(chan string, 1)
	go func() {
		var out bytes.Buffer
		answered, quit := false, false
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			out.Write(buf[:n])
			if o.answer && !answered && bytes.Contains(out.Bytes(), []byte(cursorQuery)) {
				answered = true
				_, _ = master.WriteString(whiteBackground + cursorAt)
			}
			if o.quitOn != "" && !quit && bytes.Contains(out.Bytes(), []byte(o.quitOn)) {
				quit = true
				_, _ = master.WriteString("q")
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
		<-done
		t.Fatalf("ficha %v was still running after 30s. It wrote:\n%q", args, <-read)
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
		{"show", projFlag, e2eAlphaID, "-f", "json"},
		{"show", projFlag, e2eAlphaID, "-f", "csv"},
		{"show", projFlag, e2eAlphaID, "--no-color"},
		{"show", projFlag, "no-such-session"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, took := ptyRun(t, root, screen, args...)
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
	out, _ := ptyRun(t, root, ptyOpts{rows: 40, cols: 100, answer: true}, "show", projFlag, e2eAlphaID)
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
	out, took := ptyRun(t, root, screen, "show", projFlag, e2eAlphaID)
	if got := strings.Count(out, backgroundQuery); got != 1 {
		t.Fatalf("asked for the background %d times, want once:\n%q", got, out)
	}
	// One query can only time out once, so the count above is the limit on
	// the wait. This is that the terminal really went unanswered.
	if took < 4*time.Second {
		t.Errorf("took %v, want termenv's 5-second wait", took)
	}
	if !strings.Contains(out, "38;5;245m") || strings.Contains(out, "38;5;242m") {
		t.Errorf("no answer should get the dark palette:\n%q", out)
	}
}

// A live view draws in the run's colors: none under CI, where Bubble Tea
// left to itself would draw them, and the palette's otherwise.
func TestLiveViewTakesTheRunsColorProfile(t *testing.T) {
	root := setupE2EFixture(t)
	live := ptyOpts{rows: 40, cols: 100, answer: true, quitOn: "FOLLOWING"}

	out, _ := ptyRun(t, root, live, "watch", projFlag)
	if !strings.Contains(out, "38;5;") {
		t.Errorf("watch on a color terminal drew no color:\n%q", out)
	}

	live.env = []string{"CI=1"}
	out, _ = ptyRun(t, root, live, "watch", projFlag)
	if !strings.Contains(out, "FOLLOWING") || strings.Contains(out, "38;5;") || strings.Contains(out, "\x1b[1m") {
		t.Errorf("watch under CI should draw plain:\n%q", out)
	}
	if strings.Contains(out, backgroundQuery) {
		t.Errorf("watch under CI asked for the background:\n%q", out)
	}
}

// A pty that reports no size still gets a frame, in both views.
func TestLiveViewDrawsOnATerminalWithNoSize(t *testing.T) {
	root := setupE2EFixture(t)
	for _, tt := range []struct {
		view       string
		rows, cols uint16
	}{
		{"watch", 0, 0}, {"watch", 0, 100}, {"watch", 40, 0}, {"breakdown", 0, 0},
	} {
		o := ptyOpts{rows: tt.rows, cols: tt.cols, answer: true, quitOn: "FOLLOWING"}
		if out, _ := ptyRun(t, root, o, tt.view, projFlag); !strings.Contains(out, "FOLLOWING") {
			t.Errorf("%s at %dx%d drew no header:\n%q", tt.view, tt.rows, tt.cols, out)
		}
	}
}
