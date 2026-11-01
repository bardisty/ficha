package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

// FirstReplyID reads past user lines, records it drops, lines it can't
// decode and replies without an ID, to the first reply that has one.
func TestFirstReplyID(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  string
	}{
		{"first reply", []string{
			`{"type":"user","message":{"role":"user","content":"p"}}`,
			`{"type":"file-history-snapshot","snapshot":{}}`,
			`not json`,
			`{"type":"assistant","message":{"model":"<synthetic>"}}`,
			`{"type":"assistant","requestId":"r1","message":{"id":"msg_1","model":"claude-opus-4-8"}}`,
			`{"type":"assistant","requestId":"r2","message":{"id":"msg_2","model":"claude-opus-4-8"}}`,
		}, "msg_1"},
		{"no reply", []string{`{"type":"user","message":{"role":"user","content":"p"}}`}, ""},
		{"empty", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s.jsonl")
			if err := os.WriteFile(path, []byte(strings.Join(tc.lines, "\n")), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := FirstReplyID(path)
			if err != nil || got != tc.want {
				t.Errorf("FirstReplyID = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	if _, err := FirstReplyID(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Error("a missing transcript should be an error")
	}
}

// The dedup set remembers which session kept each reply, so a later copy
// can name it.
func TestKeptByNamesTheSessionThatKeptAReply(t *testing.T) {
	msg := func(id, req string) models.JSONLMessage {
		return models.JSONLMessage{RequestID: req, Message: &models.AssistantMessage{ID: id}}
	}
	seen := map[DedupKey]string{}
	ExcludeSeenMessages([]models.JSONLMessage{msg("m1", "r1"), msg("m2", "r2")}, seen, "orig")
	kept := ExcludeSeenMessages([]models.JSONLMessage{msg("m1", "r1"), msg("m3", "r3")}, seen, "fork")
	if len(kept) != 1 || kept[0].Message.ID != "m3" {
		t.Fatalf("fork kept %v, want only m3", kept)
	}
	for _, tc := range []struct {
		msg  models.JSONLMessage
		want string
		ok   bool
	}{
		{msg("m1", "r1"), "orig", true},
		{msg("m3", "r3"), "fork", true},
		{msg("m9", "r9"), "", false},
		{models.JSONLMessage{}, "", false},
	} {
		if got, ok := KeptBy(tc.msg, seen); got != tc.want || ok != tc.ok {
			t.Errorf("KeptBy(%+v) = %q, %v; want %q, %v", tc.msg.Message, got, ok, tc.want, tc.ok)
		}
	}
}
