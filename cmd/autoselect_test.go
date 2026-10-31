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
	if !strings.Contains(stdout, "Session: "+e2eBetaID[:8]) {
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

// The note is the one styled line that goes to stderr. It follows stdout's
// color: dim on a color terminal, plain when stdout is a pipe.
func TestSkippedNoteTakesStdoutsColor(t *testing.T) {
	emptyID := setupEmptyNewestSession(t)
	note := "(skipped 1 newer session with no replies yet: " + emptyID[:8] + ")"

	_, stderr, err := executeCLISplit(t, "show", projFlag)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, note) || strings.Contains(stderr, "\x1b[") {
		t.Errorf("piped: want the plain note, got %q", stderr)
	}

	fakeTerminal(t)
	_, stderr, err = executeCLISplit(t, "show", projFlag)
	if err != nil {
		t.Fatal(err)
	}
	if want := "\x1b[38;5;245m" + note + "\x1b[m"; !strings.Contains(stderr, want) {
		t.Errorf("on a terminal: want %q, got %q", want, stderr)
	}
}

// Scripts get the newest session as before: only the table skips it.
func TestShowJSONKeepsNewestSession(t *testing.T) {
	emptyID := setupEmptyNewestSession(t)
	stdout, stderr, err := executeCLISplit(t, "show", projFlag, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"session_id": "`+emptyID+`"`) || strings.Contains(stderr, "skipped") {
		t.Errorf("json should report the newest session unskipped:\nstdout %s\nstderr %s", stdout, stderr)
	}
}

// A newest session whose lines couldn't be parsed may hold replies, so it
// isn't passed over: its report and its skip warning show instead.
func TestShowDoesNotSkipUnreadableSession(t *testing.T) {
	emptyID := setupEmptyNewestSession(t)
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	bad := `{"type":"assistant","timestamp":"not-a-time","message":{"id":"m9"` + "\n"
	if err := os.WriteFile(filepath.Join(root, "projects", e2eProjDir, emptyID+".jsonl"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := executeCLISplit(t, "show", projFlag, "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr, "skipped 1 newer") || !strings.Contains(stderr, "unparseable line") {
		t.Errorf("want the unreadable session's warning, not a skip:\n%s", stderr)
	}
	if !strings.Contains(stdout, "No assistant messages in session "+emptyID[:8]) {
		t.Errorf("want the newest session reported:\n%s", stdout)
	}
}

// setupStaleSession makes alpha the newest transcript by mtime, though its
// last reply (Feb 1) is a day older than beta's (Feb 2): Claude Code wrote a
// system record to it long after that reply. It returns the project dir.
func setupStaleSession(t *testing.T) string {
	t.Helper()
	proj := filepath.Join(setupE2EFixture(t), "projects", e2eProjDir)
	alpha := filepath.Join(proj, e2eAlphaID+".jsonl")
	f, err := os.OpenFile(alpha, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"system","subtype":"informational","timestamp":"2026-02-09T10:00:00Z","content":"x"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	setMtime(t, alpha, now.Add(-time.Minute))
	setMtime(t, filepath.Join(proj, e2eBetaID+".jsonl"), now.Add(-time.Hour))
	return proj
}

func setMtime(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func writeSession(t *testing.T, path string, mtime time.Time, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setMtime(t, path, mtime)
}

// The latest session is the one with the newest reply, as list puts first,
// not the transcript written last.
func TestLatestSessionIsTheLastReplied(t *testing.T) {
	setupStaleSession(t)

	stdout, stderr, err := executeCLISplit(t, "show", projFlag, "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Session: "+e2eBetaID[:8]) || strings.Contains(stderr, "skipped") {
		t.Errorf("show: want beta with no note:\n%s\n%s", stdout, stderr)
	}
	stdout, _, err = executeCLISplit(t, "show", projFlag, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"session_id": "`+e2eBetaID+`"`) {
		t.Errorf("show -f json: want beta:\n%s", stdout)
	}

	got, _ := completeLines(t, "show", projFlag, "")
	if len(got) != 2 || !strings.HasPrefix(got[0], "bbbbbbbb\t") || !strings.HasPrefix(got[1], "aaaaaaaa\t") {
		t.Errorf("completion order: %q", got)
	}
}

// The ambiguous-ID list puts sessions in the same order.
func TestAmbiguousIDListsLastRepliedFirst(t *testing.T) {
	proj := setupStaleSession(t)
	extraID := "aaaabbbb-0000-0000-0000-000000000000"
	writeSession(t, filepath.Join(proj, extraID+".jsonl"), time.Now().Add(-3*time.Hour),
		e2eMsg("2026-02-03T10:00:00Z", "x1", "claude-opus-4-8", 100, 50, 0, 0, 0))

	_, _, err := executeCLISplit(t, "show", projFlag, "aaaa")
	if err == nil {
		t.Fatal("want an ambiguous-ID error")
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[1], "  "+extraID) || !strings.HasPrefix(lines[2], "  "+e2eAlphaID) {
		t.Errorf("got:\n%v", err)
	}
}

// A newest transcript with no reply yet is a session just starting: watch,
// breakdown and json still open it, and the table passes over it to the
// session with the newest reply, not the one written last.
func TestNewestWithoutRepliesStillComesFirst(t *testing.T) {
	proj := setupStaleSession(t)
	emptyID := "eeeeeeee-0000-4000-8000-000000000000"
	writeSession(t, filepath.Join(proj, emptyID+".jsonl"), time.Now(),
		`{"type":"user","timestamp":"2026-02-10T10:00:00Z","message":{"role":"user","content":"hi"}}`)

	stdout, _, err := executeCLISplit(t, "show", projFlag, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"session_id": "`+emptyID+`"`) {
		t.Errorf("show -f json: want the empty newest session:\n%s", stdout)
	}

	stdout, stderr, err := executeCLISplit(t, "show", projFlag, "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Session: "+e2eBetaID[:8]) {
		t.Errorf("show: want beta:\n%s", stdout)
	}
	if want := "(skipped 1 newer session with no replies yet: " + emptyID[:8] + ")"; !strings.Contains(stderr, want) {
		t.Errorf("stderr missing %q:\n%s", want, stderr)
	}
}

// The walk reads a transcript only while its mtime could still hide a later
// reply, so with nothing written to an old session after its last reply it
// reads one tail, as before. The price is a transcript whose mtime was moved
// back, as a copy can do: its later reply goes unseen.
func TestLatestSessionStopsAtOlderFiles(t *testing.T) {
	proj := filepath.Join(setupE2EFixture(t), "projects", e2eProjDir)
	// Each file was last written at its last reply.
	at := func(s string) time.Time {
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return ts
	}
	setMtime(t, filepath.Join(proj, e2eBetaID+".jsonl"), at("2026-02-02T09:10:00Z"))
	setMtime(t, filepath.Join(proj, e2eAlphaID+".jsonl"), at("2026-02-01T10:05:00Z"))
	copiedID := "cccccccc-0000-4000-8000-000000000000"
	writeSession(t, filepath.Join(proj, copiedID+".jsonl"), at("2026-01-01T10:00:00Z"),
		e2eMsg("2099-01-01T10:00:00Z", "x1", "claude-opus-4-8", 100, 50, 0, 0, 0))

	stdout, _, err := executeCLISplit(t, "show", projFlag, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"session_id": "`+e2eBetaID+`"`) {
		t.Errorf("want beta, the newest file, without reading past it:\n%s", stdout)
	}
}
