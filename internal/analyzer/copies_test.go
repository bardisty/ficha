package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// copiesReply is an assistant reply as a transcript holds it.
func copiesReply(ts, id string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"req_%s","message":{"id":"msg_%s","model":"claude-opus-4-8","usage":{"input_tokens":100,"output_tokens":300}}}`+"\n", ts, id, id)
}

// FindCopies compares only sessions that begin with this one's first reply,
// so an unrelated session, one that holds a copied reply further in, and a
// directory named like a transcript change nothing; what's outside the
// projects directory, or has no reply yet, has nothing to compare.
func TestFindCopies(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	dir := filepath.Join(root, "projects", "-work-api")
	if err := os.MkdirAll(filepath.Join(dir, "broken.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC)
	write := func(name, data string) string {
		t.Helper()
		path := filepath.Join(dir, name+".jsonl")
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		mtime = mtime.Add(time.Hour)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return path
	}
	a1, a2 := copiesReply("2026-02-01T10:00:00Z", "a1"), copiesReply("2026-02-01T10:01:00Z", "a2")
	orig := write("aaaaaaaa", a1+a2)
	fork := write("ffffffff", a1+a2+copiesReply("2026-02-03T10:00:00Z", "f1"))
	write("bbbbbbbb", copiesReply("2026-02-02T10:00:00Z", "b1")+a2)
	empty := write("eeeeeeee", `{"type":"user","message":{"role":"user","content":"p"}}`+"\n")

	// The fork's file is the later one, so the original keeps a1 and a2.
	c, checked := FindCopies(fork, "ffffffff")
	if !checked || c == nil || c.Messages != 2 || c.SessionID != "aaaaaaaa" || c.Sessions != 1 {
		t.Fatalf("fork: %+v, checked %v; want 2 copies kept by aaaaaaaa", c, checked)
	}
	want := 2 * CalculateCost(models.TokenUsage{InputTokens: 100, OutputTokens: 300}, "claude-opus-4-8").TotalCost
	if !almostEqual(c.Cost, want, 1e-12) {
		t.Errorf("copies cost %v, want %v", c.Cost, want)
	}
	if c, checked := FindCopies(orig, "aaaaaaaa"); c != nil || !checked {
		t.Errorf("original: %+v, checked %v; want none", c, checked)
	}
	if c, checked := FindCopies(empty, "eeeeeeee"); c != nil || checked {
		t.Errorf("no reply yet: %+v, checked %v; want none, unchecked", c, checked)
	}

	outside := filepath.Join(t.TempDir(), "ffffffff.jsonl")
	data, _ := os.ReadFile(fork)
	if err := os.WriteFile(outside, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if c, checked := FindCopies(outside, "ffffffff"); c != nil || !checked {
		t.Errorf("outside the projects directory: %+v, checked %v; want none", c, checked)
	}
}
