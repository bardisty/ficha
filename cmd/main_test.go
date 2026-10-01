package cmd

import (
	"os"
	"strings"
	"testing"
)

// TestMain clears the color variables a developer's shell may set. NO_COLOR
// turns ficha's color off, and CLICOLOR_FORCE colors output the e2e tests
// expect plain. A test that needs one sets it with t.Setenv, and a test that
// needs color fakes a terminal: the buffer a test captures stdout in is a
// pipe to ficha, so its output is plain.
//
// With ptyArgsEnv set, the binary runs ficha with those arguments and no
// tests. The pty tests start it that way, to have a real ficha process on a
// terminal of their own.
func TestMain(m *testing.M) {
	if args, ok := os.LookupEnv(ptyArgsEnv); ok {
		os.Args = append([]string{"ficha"}, strings.Split(args, ptyArgsSep)...)
		Execute()
		return
	}
	for _, v := range []string{"NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE"} {
		_ = os.Unsetenv(v)
	}
	os.Exit(m.Run())
}

// ptyArgsEnv carries ficha's arguments to the test binary, joined with
// ptyArgsSep.
const (
	ptyArgsEnv = "FICHA_TEST_PTY_ARGS"
	ptyArgsSep = "\x1f"
)
