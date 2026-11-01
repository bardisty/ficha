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

// Without sessions-index.json the session that diverged from the shared
// history last keeps it, so the copies can move once at most: while neither
// session has a reply of its own the ID decides, and the first to reply
// goes behind. Recheck finds that move, and returns nil for any other
// write, mtime included. With an index, creation times decide and writes
// move nothing.
func TestForkFamilyRecheck(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		t.Run(fmt.Sprintf("indexed=%v", indexed), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", root)
			dir := filepath.Join(root, "projects", "-work-api")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			day := time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC)
			touch := func(path string, at time.Time) {
				t.Helper()
				if err := os.Chtimes(path, at, at); err != nil {
					t.Fatal(err)
				}
			}
			appendTo := func(path, data string) {
				t.Helper()
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if _, err := f.WriteString(data); err != nil {
					t.Fatal(err)
				}
			}
			shared := copiesReply("2026-02-01T10:00:00Z", "a1") + copiesReply("2026-02-01T10:01:00Z", "a2")
			orig := filepath.Join(dir, "aaaaaaaa.jsonl")
			fork := filepath.Join(dir, "ffffffff.jsonl")
			for _, path := range []string{orig, fork} {
				if err := os.WriteFile(path, []byte(shared), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if indexed {
				index := fmt.Sprintf(`{"version":1,"entries":[{"sessionId":"aaaaaaaa","fullPath":%q,"created":"2026-02-01T10:00:00Z"},{"sessionId":"ffffffff","fullPath":%q,"created":"2026-02-03T09:00:00Z"}]}`, orig, fork)
				if err := os.WriteFile(filepath.Join(dir, "sessions-index.json"), []byte(index), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			f, checked := FindFamily(fork, "ffffffff")
			if !checked || f == nil || f.Copies == nil || f.Copies.SessionID != "aaaaaaaa" {
				t.Fatalf("fork: %+v, checked %v; want copies kept by aaaaaaaa", f, checked)
			}
			if !f.Includes("aaaaaaaa") || !f.Includes("ffffffff") || f.Includes("bbbbbbbb") {
				t.Error("the family should be the original and the fork alone")
			}
			if next := f.Recheck(); next != nil {
				t.Fatalf("nothing was written, yet the copies moved: %+v", next.Copies)
			}
			touch(orig, day.Add(time.Hour))
			touch(fork, day)
			if next := f.Recheck(); next != nil {
				t.Fatalf("mtimes moved the copies: %+v", next.Copies)
			}
			// A prompt isn't a reply. The fork comes back at its new size,
			// so the next recheck doesn't parse it again.
			appendTo(fork, `{"type":"user","timestamp":"2026-02-03T09:59:00Z","message":{"role":"user","content":"go on"}}`+"\n")
			next := f.Recheck()
			if indexed && next != nil {
				t.Fatalf("with an index a prompt rechecked the family: %+v", next)
			}
			if !indexed {
				if next == nil || next.Copies == nil || next.Copies.SessionID != "aaaaaaaa" || next.Copies.Messages != f.Copies.Messages {
					t.Fatalf("a prompt moved the copies: %+v", next)
				}
				if again := next.Recheck(); again != nil {
					t.Fatalf("the fork's size didn't move with the prompt: %+v", again)
				}
				f = next
			}

			// The original replies first, so it diverged first, and the
			// fork, which hasn't yet, keeps the shared replies.
			appendTo(orig, copiesReply("2026-02-03T10:00:00Z", "a3"))
			next = f.Recheck()
			if indexed {
				if next != nil {
					t.Errorf("with an index a reply moved the copies: %+v", next.Copies)
				}
				return
			}
			if next == nil || next.Copies != nil {
				t.Fatalf("after the original's reply the fork should keep its replies: %+v", next)
			}
			o, _ := FindFamily(orig, "aaaaaaaa")
			if o == nil || o.Copies == nil || o.Copies.SessionID != "ffffffff" || o.Copies.Messages != 2 {
				t.Errorf("the original should now hold 2 copies the fork keeps: %+v", o)
			}

			// The fork's reply comes later, so it keeps them. That moves
			// nothing, and after it no write is even looked at.
			appendTo(fork, copiesReply("2026-02-03T11:00:00Z", "f1"))
			last := next.Recheck()
			if last == nil || last.Copies != nil {
				t.Fatalf("the fork replied last, so it keeps its replies: %+v", last)
			}
			if len(last.open) != 0 {
				t.Errorf("both have replied, yet %v can still move the order", last.open)
			}
			appendTo(orig, copiesReply("2026-02-04T10:00:00Z", "a4"))
			appendTo(fork, copiesReply("2026-02-04T11:00:00Z", "f2"))
			touch(orig, day.Add(3*time.Hour))
			if again := last.Recheck(); again != nil {
				t.Errorf("writes after both replied moved the copies: %+v", again.Copies)
			}
		})
	}
}
