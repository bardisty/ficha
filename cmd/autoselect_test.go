package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupEmptyNewestSession adds a session to the e2e project that has a user
// prompt and a title but no reply yet, and makes it the newest by mtime.
func setupEmptyNewestSession(t *testing.T) (emptyID string) {
	t.Helper()
	root := setupE2EFixture(t)
	dir := filepath.Join(root, "projects", e2eProjDir)
	old := time.Now().Add(-2 * time.Hour)
	for i, id := range []string{e2eAlphaID, e2eBetaID} {
		mtime := old.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(filepath.Join(dir, id+".jsonl"), mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	emptyID = "eeeeeeee-0000-4000-8000-000000000000"
	content := `{"type":"user","timestamp":"2026-02-05T10:00:00Z","message":{"role":"user","content":"hi"}}` + "\n" +
		`{"type":"ai-title","aiTitle":"Say hello","sessionId":"` + emptyID + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, emptyID+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return emptyID
}

// Bare `show` passes over a newer session nobody has replied to yet, reports
// the one before it, and says what it skipped.
func TestShowSkipsNewerSessionWithoutReplies(t *testing.T) {
	emptyID := setupEmptyNewestSession(t)

	stdout, stderr, err := executeCLISplit(t, "show", projFlag, "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Session: "+e2eBetaID) {
		t.Errorf("want the newest session with replies (beta):\n%s", stdout)
	}
	if want := "(skipped 1 newer session with no replies yet: " + emptyID[:8] + ")"; !strings.Contains(stderr, want) {
		t.Errorf("stderr missing %q:\n%s", want, stderr)
	}

	// Asked for by ID, the empty session gets one line.
	stdout, _, err = executeCLISplit(t, "show", projFlag, emptyID[:8])
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != "No assistant messages in session "+emptyID[:8]+" yet." {
		t.Errorf("explicit empty session: got %q", stdout)
	}

	// list keeps it, with its title.
	stdout, _, err = executeCLISplit(t, "list", projFlag, "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, emptyID[:8]) || !strings.Contains(stdout, "Say hello") {
		t.Errorf("list should keep the empty session and its title:\n%s", stdout)
	}
}
