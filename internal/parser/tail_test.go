package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tailLine(ts string, pad int) string {
	return `{"type":"user","timestamp":"` + ts + `","content":"` + strings.Repeat("x", pad) + `"}`
}

func TestNewestTailTimestamp(t *testing.T) {
	at := func(s string) time.Time {
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return ts
	}
	early, mid, late := "2024-01-15T10:00:00Z", "2024-01-15T11:00:00Z", "2024-01-15T12:00:00Z"
	// The exact length that puts the start of the last line on the tail's
	// first byte, so the byte before it has to be seen to keep the line.
	exact := tailLine(late, 0)
	exact = tailLine(late, tailSize-len(exact)-1)

	cases := []struct {
		name    string
		content string
		want    string // "" when the tail has no timestamp
	}{
		{"empty file", "", ""},
		{"short file", tailLine(early, 0) + "\n" + tailLine(mid, 0) + "\n", mid},
		{"newest isn't last", tailLine(late, 0) + "\n" + tailLine(early, 0) + "\n", late},
		{"no final newline", tailLine(early, 0) + "\n" + tailLine(mid, 0), mid},
		{"last line cut short", tailLine(early, 0) + "\n" + `{"type":"user","timestamp":"2024-01-15T12:00`, early},
		{"line before the tail is left out", tailLine(late, tailSize) + "\n" + tailLine(early, 0) + "\n", early},
		{"last line fills the tail exactly", tailLine(early, 0) + "\n" + exact + "\n", late},
		{"last line longer than the tail", tailLine(early, 0) + "\n" + tailLine(late, tailSize+10) + "\n", ""},
		{"no timestamps", `{"type":"summary"}` + "\n" + "{broken\n", ""},
		{"unparseable timestamp", `{"type":"user","timestamp":"yesterday"}` + "\n", ""},
		{"CRLF", tailLine(early, 0) + "\r\n" + tailLine(mid, 0) + "\r\n", mid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "t.jsonl")
			if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			got, ok := NewestTailTimestamp(f, int64(len(c.content)))
			if c.want == "" {
				if ok {
					t.Errorf("got %v, want no timestamp", got)
				}
				return
			}
			if !ok || !got.Equal(at(c.want)) {
				t.Errorf("got %v (ok=%v), want %s", got, ok, c.want)
			}
		})
	}
}

func replyLine(ts string) string {
	return `{"type":"assistant","timestamp":"` + ts + `","message":{"id":"m","role":"assistant","content":[]}}`
}

func TestNewestReplyTimestamp(t *testing.T) {
	early, mid, late := "2024-01-15T10:00:00Z", "2024-01-15T11:00:00Z", "2024-01-15T12:00:00Z"
	system := `{"type":"system","subtype":"informational","timestamp":"` + late + `","content":"x"}`
	cases := []struct {
		name    string
		content string
		want    string // "" when there's no reply
	}{
		{"empty file", "", ""},
		{"newest reply, not the newest line", replyLine(early) + "\n" + replyLine(mid) + "\n" + system + "\n" + tailLine(late, 0) + "\n", mid},
		{"newest reply isn't last", replyLine(mid) + "\n" + replyLine(early) + "\n", mid},
		{"no replies", tailLine(early, 0) + "\n" + system + "\n", ""},
		{"a reply with no message", `{"type":"assistant","timestamp":"` + late + `","message":null}` + "\n" + replyLine(early) + "\n", early},
		{"reply cut short", replyLine(early) + "\n" + `{"type":"assistant","timestamp":"` + late, early},
		// The tail holds no reply, so the file is read from the start.
		{"reply only before the tail", replyLine(mid) + "\n" + tailLine(late, tailSize) + "\n" + system + "\n", mid},
		{"reply only before a last line longer than the tail", replyLine(mid) + "\n" + tailLine(late, tailSize+10) + "\n", mid},
		{"CRLF", replyLine(early) + "\r\n" + replyLine(mid) + "\r\n", mid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "t.jsonl")
			if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
				t.Fatal(err)
			}
			got, ok := NewestReplyTimestamp(path)
			if c.want == "" {
				if ok {
					t.Errorf("got %v, want no reply", got)
				}
				return
			}
			if want, _ := time.Parse(time.RFC3339, c.want); !ok || !got.Equal(want) {
				t.Errorf("got %v (ok=%v), want %s", got, ok, c.want)
			}
		})
	}
	if _, ok := NewestReplyTimestamp(filepath.Join(t.TempDir(), "missing.jsonl")); ok {
		t.Error("a missing file has a reply")
	}
}
