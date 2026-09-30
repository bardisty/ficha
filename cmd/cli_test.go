package cmd

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

// executeCLI runs a fresh root command with args, capturing cobra-managed
// output. A new tree per call means no flag state leaks between tests, so no
// cleanup is needed (that re-entrancy is the point of the config refactor).
func executeCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	root := newRootCmd()
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
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
	want := versionLine() + "\n"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestResolveVersion(t *testing.T) {
	stamped := func(v string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: "github.com/bardisty/ficha", Version: v}}
	}
	tests := []struct {
		name    string
		ldflags string
		info    *debug.BuildInfo
		ok      bool
		want    string
	}{
		{"ldflags win over build info", "0.24.0", stamped("v0.23.0"), true, "0.24.0"},
		{"go install tag, v stripped", "dev", stamped("v0.24.0"), true, "0.24.0"},
		{"pseudo-version kept", "dev", stamped("v0.24.1-0.20260928120000-abcdef123456"), true, "0.24.1-0.20260928120000-abcdef123456"},
		{"devel falls back", "dev", stamped("(devel)"), true, "dev"},
		{"empty falls back", "dev", stamped(""), true, "dev"},
		{"no build info", "dev", nil, false, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.ldflags, tt.info, tt.ok); got != tt.want {
				t.Errorf("resolveVersion(%q, %v, %v) = %q, want %q", tt.ldflags, tt.info, tt.ok, got, tt.want)
			}
		})
	}
}

// version prints fixed text, so a global flag given to it would be silently
// ignored; it's rejected as a usage error instead.
func TestVersionSubcommandRejectsGlobalFlags(t *testing.T) {
	_, err := executeCLI(t, "version", "-f", "xml")
	if err == nil {
		t.Fatal("version -f xml should error")
	}
	if want := "ficha version doesn't take --format"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
	if got := exitCode(err); got != exitUsage {
		t.Errorf("exit %d, want %d", got, exitUsage)
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
	_, err := executeCLI(t, "global", "--top=-1")
	if err == nil {
		t.Fatal("global --top=-1 should error")
	}
	if !strings.Contains(err.Error(), "must be >= 0") {
		t.Errorf("expected --top validation error, got: %v", err)
	}
}

// `ficha help output` is a help topic: the glossary prints, and root help
// points at it.
func TestOutputHelpTopic(t *testing.T) {
	out, _, err := executeCLISplit(t, "help", "output")
	if err != nil {
		t.Fatal(err)
	}
	for _, term := range []string{"API-equivalent estimate", "5m TTL", "Savings", "Messages and turns", "Context"} {
		if !strings.Contains(out, term) {
			t.Errorf("glossary missing %q", term)
		}
	}
	root, _, err := executeCLISplit(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(root, "ficha help output") {
		t.Errorf("root help should point at the glossary:\n%s", root)
	}
}
