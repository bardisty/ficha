package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withStderrWidth makes ficha treat stderr as a terminal width columns wide.
func withStderrWidth(t *testing.T, width int) {
	t.Helper()
	orig := stderrWidth
	stderrWidth = func(io.Writer) int { return width }
	t.Cleanup(func() { stderrWidth = orig })
}

// noteLines returns stderr's lines from the first "Note: " line through its
// continuations, which are indented past the label.
func noteLines(t *testing.T, stderr string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "Note: ") {
			continue
		}
		j := i + 1
		for j < len(lines) && strings.HasPrefix(lines[j], "      ") {
			j++
		}
		return lines[i:j]
	}
	t.Fatalf("no note on stderr:\n%s", stderr)
	return nil
}

// checkWrapped fails unless the note wrapped to width with every line after
// the first hung past "Note: ", and no word was cut.
func checkWrapped(t *testing.T, stderr string, width int, words ...string) {
	t.Helper()
	lines := noteLines(t, stderr)
	if len(lines) < 2 {
		t.Errorf("note didn't wrap at %d columns:\n%s", width, stderr)
	}
	for _, line := range lines {
		if len(line) > width && len(strings.Fields(line)) > 1 && !strings.HasPrefix(line, "Note: ") {
			t.Errorf("line is %d columns wide at %d: %q", len(line), width, line)
		}
	}
	joined := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	for _, w := range words {
		if !strings.Contains(joined, w) {
			t.Errorf("note lost %q:\n%s", w, stderr)
		}
	}
}

// checkOneLine fails unless the note is a single line: stderr that isn't a
// terminal is left for logs and scripts to read line by line.
func checkOneLine(t *testing.T, stderr, want string) {
	t.Helper()
	if lines := noteLines(t, stderr); len(lines) != 1 || lines[0] != want {
		t.Errorf("note changed off a terminal:\n got: %q\nwant: %q", lines, want)
	}
}

func TestNoteSessionInAnotherProjectWraps(t *testing.T) {
	setupE2EFixture(t)
	args := []string{"show", projFlag, e2eGammaID, "-f", "json"}
	want := "Note: session cccccccc is in " + e2eOtherDir + "."

	_, stderr, err := executeCLISplit(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	checkOneLine(t, stderr, want)

	withStderrWidth(t, 30)
	_, stderr, err = executeCLISplit(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stderr, "Note: session cccccccc is in\n      "+e2eOtherDir+".\n"; got != want {
		t.Errorf("stderr:\n got: %q\nwant: %q", got, want)
	}
}

func TestNoteProjectMatchWraps(t *testing.T) {
	home := setupCwdFixture(t)
	elsewhere := filepath.Join(t.TempDir(), "webapp")
	args := []string{"show", "-p", elsewhere, "-f", "json"}
	real := filepath.Join(home, "work", "webapp")
	want := "Note: matched by name 'webapp' (original: " + real + ")"

	_, stderr, err := executeCLISplit(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	checkOneLine(t, stderr, want)

	withStderrWidth(t, 40)
	_, stderr, err = executeCLISplit(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	checkWrapped(t, stderr, 40, "'webapp'", real+")")
}

func TestNoteCopiedTranscriptWraps(t *testing.T) {
	root := setupE2EFixture(t)
	copyDir := t.TempDir()
	copied := filepath.Join(copyDir, e2eBetaID+".jsonl")
	copyFile(t, filepath.Join(root, "projects", e2eProjDir, e2eBetaID+".jsonl"), copied)
	folder := filepath.Join(copyDir, e2eBetaID) + string(filepath.Separator)

	withStderrWidth(t, 40)
	_, stderr, err := executeCLISplit(t, "show", copied, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	checkWrapped(t, stderr, 40, folder+",", "which isn't there.")
}

func TestNoteSessionsMissingFromIndexWraps(t *testing.T) {
	root := setupE2EFixture(t)
	index := filepath.Join(root, "projects", e2eProjDir, "sessions-index.json")
	if err := os.WriteFile(index, []byte(`{"entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"show", projFlag, e2eBetaID, "-v", "-f", "json"}

	_, stderr, err := executeCLISplit(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	checkOneLine(t, stderr, "Note: Found 2 session(s) not in sessions-index.json")

	withStderrWidth(t, 30)
	_, stderr, err = executeCLISplit(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	checkWrapped(t, stderr, 30, "sessions-index.json")
}
