package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// executeCLI runs the root command with args, capturing cobra-managed output.
// Note: versionCmd prints via cmd.OutOrStdout(), so it lands in the buffer too.
func executeCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		format = "table"
	})
	err := rootCmd.Execute()
	return buf.String(), err
}

// These commands take no positional args; without cobra.NoArgs they silently
// ignore stray ones instead of erroring.
func TestSubcommandsRejectExtraArgs(t *testing.T) {
	for _, name := range []string{"list", "summary", "global", "version"} {
		t.Run(name, func(t *testing.T) {
			_, err := executeCLI(t, name, "stray-arg")
			if err == nil {
				t.Fatalf("%s with extra arg should error", name)
			}
			if !strings.Contains(err.Error(), "stray-arg") {
				t.Errorf("error should mention the stray arg, got: %v", err)
			}
		})
	}
}

// cobra only registers a --version flag when rootCmd.Version is set.
func TestVersionFlag(t *testing.T) {
	out, err := executeCLI(t, "--version")
	if err != nil {
		t.Fatalf("--version failed: %v", err)
	}
	want := "ccusage " + Version + "\n"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// The version command prints plain text and is exempt from --format validation,
// so an otherwise-invalid -f must not error.
func TestVersionSubcommandIgnoresFormat(t *testing.T) {
	out, err := executeCLI(t, "version", "-f", "xml")
	if err != nil {
		t.Fatalf("version -f xml should not error (format is ignored): %v", err)
	}
	want := "ccusage " + Version + "\n"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// Guard: format validation still applies to every other command.
func TestInvalidFormatStillRejected(t *testing.T) {
	_, err := executeCLI(t, "list", "-f", "xml")
	if err == nil {
		t.Fatal("list -f xml should error")
	}
	if !strings.Contains(err.Error(), "invalid format") {
		t.Errorf("expected invalid format error, got: %v", err)
	}
}

// A negative --top must be rejected before it reaches renderProjectsTable,
// where it would index projects[-1] and panic.
func TestGlobalRejectsNegativeTop(t *testing.T) {
	t.Cleanup(func() { globalTopN = 10 })
	_, err := executeCLI(t, "global", "--top=-1")
	if err == nil {
		t.Fatal("global --top=-1 should error")
	}
	if !strings.Contains(err.Error(), "must be >= 0") {
		t.Errorf("expected --top validation error, got: %v", err)
	}
}
