package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// copiesLine is an assistant reply as a transcript holds it.
func copiesLine(ts time.Time, id string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"req_%s","message":{"id":"msg_%s","model":"claude-sonnet-5-5","usage":{"input_tokens":5,"output_tokens":900,"cache_creation_input_tokens":3000,"cache_read_input_tokens":10000,"cache_creation":{"ephemeral_5m_input_tokens":3000,"ephemeral_1h_input_tokens":0}}}}`+"\n",
		ts.UTC().Format(time.RFC3339), id, id)
}

// copiesProject writes an original session and a fork of it that copied its
// three replies and added two, under a projects directory of its own. It
// returns the project directory and the fork's transcript.
func copiesProject(t *testing.T) (projectDir, forkPath string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	projectDir = filepath.Join(root, "projects", "-work-api")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	var original string
	for i := range 3 {
		original += copiesLine(start.Add(time.Duration(i)*time.Minute), fmt.Sprintf("o%d", i))
	}
	fork := original
	for i := range 2 {
		fork += copiesLine(start.AddDate(0, 0, 6).Add(time.Duration(i)*time.Minute), fmt.Sprintf("f%d", i))
	}
	write := func(id, data string, mtime time.Time) string {
		path := filepath.Join(projectDir, id+".jsonl")
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("66666666-ffff-4fff-8fff-000000000006", original, start.Add(time.Hour))
	forkPath = write("77777777-0000-4000-8000-000000000007", fork, start.AddDate(0, 0, 6).Add(time.Hour))
	return projectDir, forkPath
}

// loadFrom runs m's load command and applies what it returns.
func loadFrom(t *testing.T, m Model) Model {
	t.Helper()
	msg, ok := m.loadAnalysis().(analysisMsg)
	if !ok {
		t.Fatal("load didn't return an analysis")
	}
	updated, _ := m.Update(msg)
	return updated.(Model)
}

const copiesNoteStart = "3 messages here ("

// watch names a fork's copies under its cost rows from the first frame of
// the body, so the note never arrives later and pushes the body down. A
// reload doesn't look again, and keeps the note and every line where they
// were, the reader's too.
func TestWatchNamesAForksCopiesOnce(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {45, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			projectDir, forkPath := copiesProject(t)
			m := NewModel(forkPath, "77777777-0000-4000-8000-000000000007", true, projectDir, false)
			m = sized(t, m, size[0], size[1])

			first, ok := m.loadAnalysis().(analysisMsg)
			if !ok || !first.copiesFound || first.copies == nil {
				t.Fatalf("the first load should come with the copies: found %v, %+v", first.copiesFound, first.copies)
			}
			updated, _ := m.Update(first)
			m = updated.(Model)
			body := stripANSI(m.renderAnalysis())
			if n := strings.Count(body, copiesNoteStart); n != 1 {
				t.Fatalf("note appears %d times, want once:\n%s", n, body)
			}
			if !strings.Contains(strings.Join(strings.Fields(body), " "), "are also in session 66666666, and list and summary count them there.") {
				t.Errorf("the note should name the original:\n%s", body)
			}
			// It closes the totals, ahead of the context gauge.
			if note, gauge := strings.Index(body, copiesNoteStart), strings.Index(body, "Context"); note > gauge {
				t.Errorf("note after the context gauge:\n%s", body)
			}

			m = press(t, m, "j", "j")
			top := topLine(m)
			again, ok := m.loadAnalysis().(analysisMsg)
			if !ok || again.copiesFound {
				t.Fatalf("a reload looked for copies again")
			}
			updated, _ = m.Update(again)
			m = updated.(Model)
			if got := stripANSI(m.renderAnalysis()); got != body {
				t.Errorf("a reload moved the body:\n%s\nwas\n%s", got, body)
			}
			if got := topLine(m); got != top {
				t.Errorf("a reload moved the reader from %q to %q", top, got)
			}
		})
	}
}

// The original keeps its replies, so its body has no note, and switching
// to it from the fork drops the fork's.
func TestWatchOriginalHasNoCopiesNote(t *testing.T) {
	projectDir, forkPath := copiesProject(t)
	m := loadFrom(t, sized(t, NewModel(forkPath, "77777777-0000-4000-8000-000000000007", true, projectDir, false), 80, 24))
	if !strings.Contains(stripANSI(m.renderAnalysis()), copiesNoteStart) {
		t.Fatal("the fork should have a note")
	}
	orig := filepath.Join(projectDir, "66666666-ffff-4fff-8fff-000000000006.jsonl")
	switched, _ := m.switchTo(orig, "66666666-ffff-4fff-8fff-000000000006", false)
	m = loadFrom(t, switched.(Model))
	if body := stripANSI(m.renderAnalysis()); strings.Contains(body, "also in") {
		t.Errorf("the original has a note:\n%s", body)
	}
}

// A session nobody has replied to yet has nothing to compare, so the next
// load looks again, and finds the copies once they're written.
func TestWatchLooksForCopiesUntilTheSessionHasReplies(t *testing.T) {
	projectDir, forkPath := copiesProject(t)
	data, err := os.ReadFile(forkPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(forkPath, []byte(`{"type":"user","message":{"role":"user","content":"p"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := sized(t, NewModel(forkPath, "77777777-0000-4000-8000-000000000007", true, projectDir, false), 80, 24)
	m = loadFrom(t, m)
	if m.copiesFor != "" {
		t.Fatal("a session with no replies counted as looked at")
	}
	if err := os.WriteFile(forkPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	m = loadFrom(t, m)
	if !strings.Contains(stripANSI(m.renderAnalysis()), copiesNoteStart) {
		t.Errorf("the copies written later weren't found:\n%s", stripANSI(m.renderAnalysis()))
	}
}
