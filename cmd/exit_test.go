package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestExitCode(t *testing.T) {
	usage := usageErrorf("unknown flag: --bogus")
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, 0},
		{"plain error", errors.New("session not found"), 1},
		{"usage error", usage, 2},
		{"wrapped usage error", fmt.Errorf("context: %w", usage), 2},
		{"silent status", &exitError{code: 130}, 130},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCode(tt.err); got != tt.want {
				t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
	if msg := (&exitError{code: 130}).Error(); msg != "" {
		t.Errorf("a status with no error should have no message, got %q", msg)
	}
}

// TestExitStatusByCommandLine runs each command line and checks the status
// Execute would exit with: 2 when ficha was called wrong, 1 when it was called
// right but couldn't produce the report.
func TestExitStatusByCommandLine(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"success", []string{"list", projFlag}, 0},

		{"unknown flag", []string{"list", "--bogus"}, 2},
		{"flag missing its value", []string{"list", "--project-dir"}, 2},
		{"bad int value", []string{"global", "--top", "abc"}, 2},
		{"bad bool value", []string{"global", "--details=maybe"}, 2},
		{"unknown command", []string{"lisst"}, 2},
		{"unknown non-hex word", []string{projFlag, "frobnicate"}, 2},
		{"stray arg on a no-arg command", []string{"list", "extra"}, 2},
		{"too many args to show", []string{"show", "a", "b"}, 2},
		{"too many args to watch", []string{"watch", "a", "b"}, 2},
		{"too many args to breakdown", []string{"breakdown", "a", "b"}, 2},
		{"invalid format", []string{"list", "-f", "xml"}, 2},
		{"json on a TUI", []string{"watch", "-f", "json"}, 2},
		{"-p with --project-dir", []string{"list", "-p", ".", projFlag}, 2},
		{"bad --since", []string{"summary", projFlag, "--since", "yesterday-ish"}, 2},
		{"bad --until", []string{"global", "--until", "soon"}, 2},
		{"window ends before it starts", []string{"summary", projFlag, "--since", "2026-09-02", "--until", "2026-09-01"}, 2},
		{"--expand-agents without --details", []string{"summary", projFlag, "--expand-agents"}, 2},
		{"bad --sort-by", []string{"global", "--sort-by", "x"}, 2},
		{"negative --top", []string{"global", "--top=-1"}, 2},
		{"unknown completion shell", []string{"completion", "tcsh"}, 2},
		{"stray arg to a completion shell", []string{"completion", "bash", "x"}, 2},

		{"session not found", []string{"show", projFlag, "abc123"}, 1},
		{"ambiguous session ID", []string{"show", projFlag, "aaaaaaaa"}, 1},
		{"no such project directory", []string{"list", "-p", filepath.Join("no", "such", "dir")}, 1},
		{"unknown project directory", []string{"list", "--project-dir=-home-test-nothing"}, 1},
		{"TUI without a terminal", []string{"watch", projFlag}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configDir := setupE2EFixture(t)
			// A second session sharing alpha's first block makes "aaaaaaaa"
			// ambiguous.
			twin := filepath.Join(configDir, "projects", e2eProjDir, "aaaaaaaa-9999-2222-3333-444444444444.jsonl")
			if err := os.WriteFile(twin, []byte(e2eMsg("2026-02-01T11:00:00Z", "t1", "claude-opus-4-8", 1, 1, 0, 0, 0)+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			// Built the way Execute builds it, so completion's own
			// argument checks are included.
			root := newRootCmd()
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			root.SetArgs(tt.args)
			initCompletion(root)
			err := root.Execute()
			if got := exitCode(err); got != tt.want {
				t.Errorf("ficha %v: exit %d, want %d (err: %v)", tt.args, got, tt.want, err)
			}
		})
	}
}
