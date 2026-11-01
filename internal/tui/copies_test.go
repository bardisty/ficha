package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
			if !ok || !first.copiesFound || first.family == nil || first.family.Copies == nil {
				t.Fatalf("the first load should come with the copies: found %v, %+v", first.copiesFound, first.family)
			}
			updated, _ := m.Update(first)
			m = updated.(Model)
			body := stripANSI(m.renderAnalysis())
			if n := strings.Count(body, copiesNoteStart); n != 1 {
				t.Fatalf("note appears %d times, want once:\n%s", n, body)
			}
			if !strings.Contains(strings.Join(strings.Fields(body), " "), "are also in session 66666666. list and summary count them there.") {
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

// The note reads only what the first load found, so as the session grows
// it keeps its lines, and the body under it stays where it was: here a
// fork that added nothing gets replies that take its own cost past $1.
func TestWatchCopiesNoteKeepsItsLinesAsTheSessionGrows(t *testing.T) {
	for _, width := range []int{45, 50, 70, 77, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			projectDir, forkPath := copiesProject(t)
			data, err := os.ReadFile(forkPath)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.SplitAfter(string(data), "\n")
			if err := os.WriteFile(forkPath, []byte(strings.Join(lines[:3], "")), 0o644); err != nil {
				t.Fatal(err)
			}
			m := sized(t, NewModel(forkPath, "77777777-0000-4000-8000-000000000007", true, projectDir, false), width, 40)
			m = loadFrom(t, m)
			contextLine := func() int {
				for i, l := range strings.Split(stripANSI(m.renderAnalysis()), "\n") {
					if strings.Contains(l, "Context") {
						return i
					}
				}
				t.Fatalf("no context line:\n%s", stripANSI(m.renderAnalysis()))
				return 0
			}
			before := contextLine()
			grown := strings.Join(lines[:3], "")
			for i := range 80 {
				grown += copiesLine(time.Date(2026, 10, 31, 9, 0, i, 0, time.UTC), fmt.Sprintf("g%d", i))
			}
			if err := os.WriteFile(forkPath, []byte(grown), 0o644); err != nil {
				t.Fatal(err)
			}
			m = loadFrom(t, m)
			if m.analysis.TotalCost.TotalCost-m.copies.Cost < 1 {
				t.Fatalf("the fork's own cost %v should pass $1", m.analysis.TotalCost.TotalCost-m.copies.Cost)
			}
			if got := contextLine(); got != before {
				t.Errorf("the context gauge moved from line %d to %d", before, got)
			}
		})
	}
}

// copiesFromCmd runs cmd, batches and all, and returns the copiesMsg that
// ends the recheck it starts, if it starts one. A batch can hold a clock tick that sleeps for seconds, so
// its commands run side by side, and the first copiesMsg ends the wait.
func copiesFromCmd(cmd tea.Cmd) (copiesMsg, bool) {
	if cmd == nil {
		return copiesMsg{}, false
	}
	switch msg := cmd().(type) {
	case copiesMsg:
		return msg, true
	case tea.BatchMsg:
		found := make(chan copiesMsg, len(msg))
		var wg sync.WaitGroup
		for _, c := range msg {
			wg.Go(func() {
				if m, ok := copiesFromCmd(c); ok {
					found <- m
				}
			})
		}
		go func() { wg.Wait(); close(found) }()
		m, ok := <-found
		return m, ok
	}
	return copiesMsg{}, false
}

// Without sessions-index.json, credit for a fork's copies goes by file
// mtime, so resuming the original while watch runs on the fork moves the
// copies to the fork, and the fork's next write moves them back. watch
// follows both without a restart, and the reader's line holds as the note
// goes and comes. With an index, writes move nothing.
func TestWatchRechecksWhoKeepsTheCopies(t *testing.T) {
	const origID = "66666666-ffff-4fff-8fff-000000000006"
	for _, indexed := range []bool{false, true} {
		t.Run(fmt.Sprintf("indexed=%v", indexed), func(t *testing.T) {
			projectDir, forkPath := copiesProject(t)
			origPath := filepath.Join(projectDir, origID+".jsonl")
			if indexed {
				index := fmt.Sprintf(`{"version":1,"entries":[{"sessionId":%q,"fullPath":%q,"created":"2026-10-25T09:00:00Z"},{"sessionId":"77777777-0000-4000-8000-000000000007","fullPath":%q,"created":"2026-10-31T09:00:00Z"}]}`, origID, origPath, forkPath)
				if err := os.WriteFile(filepath.Join(projectDir, "sessions-index.json"), []byte(index), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			m := loadFrom(t, sized(t, NewModel(forkPath, "77777777-0000-4000-8000-000000000007", true, projectDir, false), 80, 8))
			if !strings.Contains(stripANSI(m.renderAnalysis()), copiesNoteStart) {
				t.Fatal("the fork should open with a note")
			}
			m = scrollToLine(t, m, "Context")
			top := topLine(m)

			// A write to an unrelated session rechecks nothing.
			if _, ok := copiesFromCmd(func() tea.Cmd {
				_, cmd := m.Update(sessionActivityMsg{path: filepath.Join(projectDir, "x.jsonl"), id: "x"})
				return cmd
			}()); ok {
				t.Error("a write outside the family rechecked the copies")
			}

			later := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
			if err := os.Chtimes(origPath, later, later); err != nil {
				t.Fatal(err)
			}
			updated, cmd := m.Update(sessionActivityMsg{path: origPath, id: origID})
			m = updated.(Model)
			found, ok := copiesFromCmd(cmd)
			if !ok {
				t.Fatal("the original's write didn't recheck the copies")
			}
			if indexed {
				if found.family != nil {
					t.Errorf("with an index the original's write moved the copies: %+v", found.family.Copies)
				}
				return
			}
			if found.family == nil {
				t.Fatal("the original's write didn't move the copies")
			}
			updated, _ = m.Update(found)
			m = updated.(Model)
			if body := stripANSI(m.renderAnalysis()); strings.Contains(body, "also in") {
				t.Errorf("the fork keeps its replies now, yet has a note:\n%s", body)
			}
			if got := topLine(m); got != top {
				t.Errorf("the note going moved the reader from %q to %q", top, got)
			}

			// The fork writes next, so it's the later file again, and the
			// reload that write starts rechecks.
			if err := os.Chtimes(forkPath, later.Add(time.Hour), later.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			reload, ok := m.loadAnalysis().(analysisMsg)
			if !ok {
				t.Fatal("load didn't return an analysis")
			}
			updated, cmd = m.Update(reload)
			m = updated.(Model)
			if found, ok = copiesFromCmd(cmd); !ok || found.family == nil {
				t.Fatal("the fork's reload didn't move the copies back")
			}
			updated, _ = m.Update(found)
			m = updated.(Model)
			if !strings.Contains(stripANSI(m.renderAnalysis()), copiesNoteStart) {
				t.Errorf("the fork's write didn't bring the note back:\n%s", stripANSI(m.renderAnalysis()))
			}
			if got := topLine(m); got != top {
				t.Errorf("the note coming back moved the reader from %q to %q", top, got)
			}
		})
	}
}

// A recheck that parses the family takes longer than one that only stats,
// so a write during it waits for its result and rechecks from there,
// rather than race it and be undone. A recheck for a session watch has left
// changes nothing, and doesn't hold up the next session's.
func TestWatchRechecksOneAtATime(t *testing.T) {
	const origID = "66666666-ffff-4fff-8fff-000000000006"
	projectDir, forkPath := copiesProject(t)
	origPath := filepath.Join(projectDir, origID+".jsonl")
	m := loadFrom(t, sized(t, NewModel(forkPath, "77777777-0000-4000-8000-000000000007", true, projectDir, false), 80, 24))

	later := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(origPath, later, later); err != nil {
		t.Fatal(err)
	}
	updated, cmd := m.Update(sessionActivityMsg{path: origPath, id: origID})
	m = updated.(Model)
	first, ok := copiesFromCmd(cmd)
	if !ok || first.family == nil {
		t.Fatal("the original's write didn't move the copies")
	}
	// The fork writes while that recheck is still out.
	if err := os.Chtimes(forkPath, later.Add(time.Hour), later.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	reload, ok := m.loadAnalysis().(analysisMsg)
	if !ok {
		t.Fatal("load didn't return an analysis")
	}
	updated, cmd = m.Update(reload)
	m = updated.(Model)
	if _, ok := copiesFromCmd(cmd); ok {
		t.Fatal("a second recheck started while the first was out")
	}
	updated, cmd = m.Update(first)
	m = updated.(Model)
	again, ok := copiesFromCmd(cmd)
	if !ok || again.family == nil {
		t.Fatal("the fork's write during the recheck wasn't rechecked after it")
	}
	updated, _ = m.Update(again)
	m = updated.(Model)
	if !strings.Contains(stripANSI(m.renderAnalysis()), copiesNoteStart) {
		t.Errorf("the fork wrote last, yet its note is gone:\n%s", stripANSI(m.renderAnalysis()))
	}

	// Switch to the original with a recheck still out.
	updated, _ = m.Update(sessionActivityMsg{path: origPath, id: origID})
	switched, _ := updated.(Model).switchTo(origPath, origID, false)
	m = loadFrom(t, switched.(Model))
	updated, _ = m.Update(again)
	m = updated.(Model)
	if m.copies != nil {
		t.Errorf("a recheck for the fork changed the original's copies: %+v", m.copies)
	}
	_, cmd = m.Update(sessionActivityMsg{path: forkPath, id: "77777777-0000-4000-8000-000000000007"})
	if _, ok := copiesFromCmd(cmd); !ok {
		t.Error("the original never rechecks: the fork's recheck left behind still counts as out")
	}
}
