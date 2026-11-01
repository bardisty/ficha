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

// unrepliedFork is copiesProject with the fork cut back to the copy it
// started as, before its first reply of its own.
func unrepliedFork(t *testing.T) (projectDir, origPath, forkPath string) {
	t.Helper()
	projectDir, forkPath = copiesProject(t)
	origPath = filepath.Join(projectDir, "66666666-ffff-4fff-8fff-000000000006.jsonl")
	data, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(forkPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return projectDir, origPath, forkPath
}

func appendFile(t *testing.T, path, data string) {
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

// reloadRecheck runs the reload a write to the watched file starts, and
// returns the model and the copiesMsg of the recheck it starts.
func reloadRecheck(t *testing.T, m Model) (Model, copiesMsg, bool) {
	t.Helper()
	reload, ok := m.loadAnalysis().(analysisMsg)
	if !ok {
		t.Fatal("load didn't return an analysis")
	}
	updated, cmd := m.Update(reload)
	found, ok := copiesFromCmd(cmd)
	return updated.(Model), found, ok
}

// Without sessions-index.json, the session that diverged from the shared
// history last keeps it. While a fork hasn't replied, neither session has
// diverged, and the original keeps the copies by its ID. Its first reply
// hands them to the fork, and watch on the original follows that without a
// restart, keeping the reader's line as the note comes. The fork's reply
// after that moves nothing, and mtimes never do. With an index, writes move
// nothing.
func TestWatchRechecksWhoKeepsTheCopies(t *testing.T) {
	const origID, forkID = "66666666-ffff-4fff-8fff-000000000006", "77777777-0000-4000-8000-000000000007"
	for _, indexed := range []bool{false, true} {
		t.Run(fmt.Sprintf("indexed=%v", indexed), func(t *testing.T) {
			projectDir, origPath, forkPath := unrepliedFork(t)
			if indexed {
				index := fmt.Sprintf(`{"version":1,"entries":[{"sessionId":%q,"fullPath":%q,"created":"2026-10-25T09:00:00Z"},{"sessionId":%q,"fullPath":%q,"created":"2026-10-31T09:00:00Z"}]}`, origID, origPath, forkID, forkPath)
				if err := os.WriteFile(filepath.Join(projectDir, "sessions-index.json"), []byte(index), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			m := loadFrom(t, sized(t, NewModel(origPath, origID, true, projectDir, false), 80, 8))
			if strings.Contains(stripANSI(m.renderAnalysis()), "also in") {
				t.Fatal("the original keeps the shared replies, so it should open without a note")
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

			// The fork's file is written last, but holds nothing new.
			later := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
			if err := os.Chtimes(forkPath, later, later); err != nil {
				t.Fatal(err)
			}
			updated, cmd := m.Update(sessionActivityMsg{path: forkPath, id: forkID})
			m = updated.(Model)
			found, ok := copiesFromCmd(cmd)
			if !ok {
				t.Fatal("the fork's write didn't recheck the copies")
			}
			if found.family != nil {
				t.Fatalf("the fork's mtime moved the copies: %+v", found.family.Copies)
			}
			updated, _ = m.Update(found)
			m = updated.(Model)

			// The original replies first, so the fork keeps them.
			appendFile(t, origPath, copiesLine(time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC), "o3"))
			m, found, ok = reloadRecheck(t, m)
			if !ok {
				t.Fatal("the original's reload didn't recheck the copies")
			}
			if indexed {
				if found.family != nil {
					t.Errorf("with an index the original's reply moved the copies: %+v", found.family.Copies)
				}
				return
			}
			if found.family == nil || found.family.Copies == nil {
				t.Fatal("the original's reply didn't hand the copies to the fork")
			}
			updated, _ = m.Update(found)
			m = updated.(Model)
			if !strings.Contains(stripANSI(m.renderAnalysis()), copiesNoteStart) {
				t.Errorf("the fork keeps the copies now, yet the original has no note:\n%s", stripANSI(m.renderAnalysis()))
			}
			if got := topLine(m); got != top {
				t.Errorf("the note coming moved the reader from %q to %q", top, got)
			}

			// The fork replies later, and keeps them: the note stays.
			appendFile(t, forkPath, copiesLine(time.Date(2026, 10, 31, 11, 0, 0, 0, time.UTC), "f0"))
			updated, cmd = m.Update(sessionActivityMsg{path: forkPath, id: forkID})
			m = updated.(Model)
			if found, ok = copiesFromCmd(cmd); ok {
				updated, _ = m.Update(found)
				m = updated.(Model)
			}
			if !strings.Contains(stripANSI(m.renderAnalysis()), copiesNoteStart) {
				t.Errorf("the fork's reply took the note away:\n%s", stripANSI(m.renderAnalysis()))
			}
			if got := topLine(m); got != top {
				t.Errorf("the fork's reply moved the reader from %q to %q", top, got)
			}

			// Both have replied, so no write moves them again.
			appendFile(t, origPath, copiesLine(time.Date(2026, 10, 31, 13, 0, 0, 0, time.UTC), "o4"))
			if _, found, ok = reloadRecheck(t, m); !ok || found.family != nil {
				t.Errorf("a write after both replied moved the copies: %+v", found.family)
			}
		})
	}
}

// A recheck that parses the family takes longer than one that only stats,
// so a write during it waits for its result and rechecks from there,
// rather than race it and be undone. A recheck for a session watch has left
// changes nothing, and doesn't hold up the next session's.
func TestWatchRechecksOneAtATime(t *testing.T) {
	const origID, forkID = "66666666-ffff-4fff-8fff-000000000006", "77777777-0000-4000-8000-000000000007"
	projectDir, origPath, forkPath := unrepliedFork(t)
	m := loadFrom(t, sized(t, NewModel(origPath, origID, true, projectDir, false), 80, 24))

	appendFile(t, origPath, copiesLine(time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC), "o3"))
	m, first, ok := reloadRecheck(t, m)
	if !ok || first.family == nil {
		t.Fatal("the original's reply didn't move the copies")
	}
	// The fork replies while that recheck is still out.
	appendFile(t, forkPath, copiesLine(time.Date(2026, 10, 31, 11, 0, 0, 0, time.UTC), "f0"))
	updated, cmd := m.Update(sessionActivityMsg{path: forkPath, id: forkID})
	m = updated.(Model)
	if _, ok := copiesFromCmd(cmd); ok {
		t.Fatal("a second recheck started while the first was out")
	}
	updated, cmd = m.Update(first)
	m = updated.(Model)
	if !strings.Contains(stripANSI(m.renderAnalysis()), copiesNoteStart) {
		t.Errorf("the fork keeps the copies, yet the original has no note:\n%s", stripANSI(m.renderAnalysis()))
	}
	again, ok := copiesFromCmd(cmd)
	if !ok || again.family == nil || again.family.Copies == nil {
		t.Fatal("the fork's reply during the recheck wasn't rechecked after it")
	}

	// Switch to the fork with that recheck still out. The fork keeps its
	// replies, and the original's copies mustn't land on it.
	switched, _ := m.switchTo(forkPath, forkID, false)
	m = loadFrom(t, switched.(Model))
	updated, _ = m.Update(again)
	m = updated.(Model)
	if m.copies != nil {
		t.Errorf("a recheck for the original changed the fork's copies: %+v", m.copies)
	}
	_, cmd = m.Update(sessionActivityMsg{path: origPath, id: origID})
	if _, ok := copiesFromCmd(cmd); !ok {
		t.Error("the fork never rechecks: the original's recheck left behind still counts as out")
	}
}
