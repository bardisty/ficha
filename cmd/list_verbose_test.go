package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// appendBrokenLine adds a malformed line to the end of a transcript.
func appendBrokenLine(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("{broken\n"); err != nil {
		t.Fatal(err)
	}
}

// list -v names the same files and lines in every format.
// Only stderr grows: stdout is the same with or without -v.
func TestListVerboseNamesSkippedFilesInEveryFormat(t *testing.T) {
	root := setupE2EFixture(t)
	proj := filepath.Join(root, "projects", e2eProjDir)
	parent := filepath.Join(proj, e2eBetaID+".jsonl")
	agent := filepath.Join(proj, e2eBetaID, "subagents", "agent-g1.jsonl")
	appendBrokenLine(t, parent)
	appendBrokenLine(t, agent)

	files := "    session bbbbbbbb: skipped line 3 (malformed)\n      " + parent + "\n" +
		"    agent g1: skipped line 2 (malformed)\n      " + agent + "\n"
	for _, format := range []string{"json", "csv", "table"} {
		t.Run(format, func(t *testing.T) {
			stdout, stderr, err := executeCLISplit(t, "list", projFlag, "-f", format, "-v")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stderr, "  bbbbbbbb: 2 lines\n"+files) {
				t.Errorf("-v should name both files under the session:\n%s", stderr)
			}

			quietOut, quiet, err := executeCLISplit(t, "list", projFlag, "-f", format)
			if err != nil {
				t.Fatal(err)
			}
			if stdout != quietOut {
				t.Errorf("stdout changed with -v:\n got %q\nwant %q", stdout, quietOut)
			}
			counts := "costs"
			if format != "table" {
				counts = "message counts and costs"
			}
			want := "Warning: 2 unparseable line(s) skipped; " + counts + " may be undercounted\n" +
				"  Run with -v to list the affected sessions.\n"
			if quiet != want {
				t.Errorf("without -v:\n got %q\nwant %q", quiet, want)
			}
		})
	}
}
