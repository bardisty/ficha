package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bardisty/ficha/internal/models"
)

// watchAt renders watch on a at width, at the fold fixture's clock.
func watchAt(t *testing.T, a *models.SessionAnalysis, width int) string {
	t.Helper()
	m := NewModel("/fixture/sess.jsonl", "sess", true, "", true)
	m.now = func() time.Time { return foldNow }
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 80})
	updated, _ = updated.Update(analysisMsg{analysis: a})
	return frameOf(updated.(Model))
}

// Only the rows watch draws size the task column: a folded agent's long task
// leaves the visible rows, and their counts, as they are.
func TestWatchTasksOnlyFromDrawnRows(t *testing.T) {
	a := foldAnalysis()
	long := "Check every query in the billing module for rounding"
	for _, i := range []int{0, 1, 4} { // a3, a1 and b2: folded
		a.Agents[i].Description = long
	}
	frame := watchAt(t, a, 80)
	if strings.Contains(frame, "Check every") {
		t.Errorf("a folded agent's task shows:\n%s", frame)
	}
	for _, want := range []string{"22 msgs", "71 msgs"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame lacks %q, a count the hidden tasks took:\n%s", want, frame)
		}
	}
}

// Where the task takes the count's place, the "N finished agents" fold, which
// has no task, keeps its count in the room.
func TestWatchFoldKeepsItsCountBesideTasks(t *testing.T) {
	a := foldAnalysis()
	a.Agents[3].Description = "Check every query in the billing module for rounding" // a2, running
	frame := watchAt(t, a, 80)
	if !strings.Contains(frame, "3 finished agents") || !strings.Contains(frame, "71 msgs") {
		t.Errorf("the fold lost its count:\n%s", frame)
	}
	if strings.Contains(frame, "22 msgs") {
		t.Errorf("the agent rows kept their counts beside a task that needed the room:\n%s", frame)
	}
}

// A short task at a narrow width takes the count's place and leaves the
// context reading, which goes only after the count.
func TestWatchShortTaskKeepsTheReading(t *testing.T) {
	a := goldenViewAnalysis()
	for i := range a.Agents {
		a.Agents[i].Description, a.Agents[i].AgentType = "", ""
	}
	a.Agents[0].AgentType = "QA"
	for _, width := range []int{54, 55} {
		m := NewModel("/fixture/sess.jsonl", "sess", true, "", true)
		m.now = func() time.Time { return goldenTime(11, 30, 12) }
		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 80})
		updated, _ = updated.Update(analysisMsg{analysis: a})
		frame := frameOf(updated.(Model))
		if !strings.Contains(frame, "38% ctx") {
			t.Errorf("width %d: the reading went before it had to:\n%s", width, frame)
		}
	}
}

// The poll reloads when an agent's meta file appears on its own, so its task
// shows while the session is quiet.
func TestSubagentSignatureSeesMetaFile(t *testing.T) {
	dir := t.TempDir()
	const sessionID = "sess"
	agentPath := filepath.Join(dir, sessionID, "subagents", "agent-a1.jsonl")
	if err := os.MkdirAll(filepath.Dir(agentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := subagentTreeSignature(dir, sessionID)
	if err := os.WriteFile(filepath.Join(filepath.Dir(agentPath), "agent-a1.meta.json"), []byte(`{"description":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if subagentTreeSignature(dir, sessionID) == before {
		t.Error("signature unchanged after the meta file appeared")
	}
}
