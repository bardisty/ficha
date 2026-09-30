package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func agentCountOf(t *testing.T, stdout string) int {
	t.Helper()
	var s struct {
		AgentCount int `json:"agent_count"`
	}
	if err := json.Unmarshal([]byte(stdout), &s); err != nil {
		t.Fatalf("not json: %v\n%s", err, stdout)
	}
	return s.AgentCount
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A transcript copied out of the projects directory without its session
// folder shows the parent alone, and says so on stderr, where json and csv
// output stays clean.
func TestCopiedTranscriptNotesMissingAgents(t *testing.T) {
	root := setupE2EFixture(t)
	proj := filepath.Join(root, "projects", e2eProjDir)
	copyDir := t.TempDir()
	copied := filepath.Join(copyDir, e2eBetaID+".jsonl")
	copyFile(t, filepath.Join(proj, e2eBetaID+".jsonl"), copied)

	stdout, stderr, err := executeCLISplit(t, "show", copied, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if n := agentCountOf(t, stdout); n != 0 {
		t.Errorf("the copy found %d agents, want 0", n)
	}
	want := "Note: any agents this session ran aren't counted. ficha looks for them in " +
		filepath.Join(copyDir, e2eBetaID) + string(filepath.Separator) + ", which isn't there.\n"
	if stderr != want {
		t.Errorf("stderr:\n got: %q\nwant: %q", stderr, want)
	}

	// The note names the folder the way the path was given.
	t.Chdir(copyDir)
	_, stderr, err = executeCLISplit(t, "show", e2eBetaID+".jsonl", "-f", "csv")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, " in "+e2eBetaID+string(filepath.Separator)+",") {
		t.Errorf("a relative path should give a relative folder:\n%s", stderr)
	}
}

// No note where the agents are found, or where there are none to find.
func TestTranscriptWithReachableAgentsHasNoNote(t *testing.T) {
	root := setupE2EFixture(t)
	proj := filepath.Join(root, "projects", e2eProjDir)

	// A copy that took its session folder along.
	withFolder := t.TempDir()
	copyFile(t, filepath.Join(proj, e2eBetaID+".jsonl"), filepath.Join(withFolder, e2eBetaID+".jsonl"))
	if err := os.CopyFS(filepath.Join(withFolder, e2eBetaID), os.DirFS(filepath.Join(proj, e2eBetaID))); err != nil {
		t.Fatal(err)
	}
	want, _, err := executeCLISplit(t, "show", projFlag, e2eBetaID, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := executeCLISplit(t, "show", filepath.Join(withFolder, e2eBetaID+".jsonl"), "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if got, wantN := agentCountOf(t, stdout), agentCountOf(t, want); got != wantN || got == 0 {
		t.Errorf("a copy with its folder found %d agents, want %d", got, wantN)
	}
	if stderr != "" {
		t.Errorf("a copy with its folder wrote to stderr: %q", stderr)
	}

	// alpha ran no agents and has no folder, but it's in the projects directory.
	paths := []string{filepath.Join(proj, e2eAlphaID+".jsonl")}
	// So is a path through a link to it, which on macOS is every temp path:
	// /var links to /private/var.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(proj, link); err == nil {
		paths = append(paths, filepath.Join(link, e2eAlphaID+".jsonl"))
	} else {
		t.Logf("no symlink case: %v", err)
	}
	for _, p := range paths {
		_, stderr, err := executeCLISplit(t, "show", p, "-f", "json")
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if stderr != "" {
			t.Errorf("%s wrote to stderr: %q", p, stderr)
		}
	}
}

// A link to a transcript in the projects directory is still searched beside
// the link, where the agents aren't, so it gets the note.
func TestLinkedTranscriptNotesMissingAgents(t *testing.T) {
	root := setupE2EFixture(t)
	proj := filepath.Join(root, "projects", e2eProjDir)
	link := filepath.Join(t.TempDir(), e2eBetaID+".jsonl")
	if err := os.Symlink(filepath.Join(proj, e2eBetaID+".jsonl"), link); err != nil {
		t.Skipf("can't make a symlink: %v", err)
	}
	stdout, stderr, err := executeCLISplit(t, "show", link, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if n := agentCountOf(t, stdout); n != 0 {
		t.Fatalf("the link found %d agents; the note would be wrong", n)
	}
	if !strings.HasPrefix(stderr, "Note: any agents this session ran aren't counted.") {
		t.Errorf("a linked transcript should get the note, got %q", stderr)
	}
}
