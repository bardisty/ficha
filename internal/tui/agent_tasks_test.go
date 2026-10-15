package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	for _, i := range []int{0, 2, 4} { // a3, a4 and b2: folded
		a.Agents[i].Description = long
	}
	frame := watchAt(t, a, 80)
	if strings.Contains(frame, "Check every") {
		t.Errorf("a folded agent's task shows:\n%s", frame)
	}
	for _, want := range []string{"22 msgs", "46 msgs"} {
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
	if !strings.Contains(frame, "2 finished agents") || !strings.Contains(frame, "46 msgs") {
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

// Every row's cost lines up, the "N finished agents" fold's included, even
// where a very short task takes the count's place and leaves the fold less
// room than its label and count need.
func TestWatchFoldStaysAlignedBesideShortTasks(t *testing.T) {
	// The cost's decimal point, in display columns: the cells align it.
	decimal := func(line string) int {
		i := strings.LastIndex(line, "$")
		if i < 0 {
			return -1
		}
		return lipgloss.Width(line[:i+strings.IndexByte(line[i:], '.')])
	}
	for _, finished := range []int{3, 12} {
		for taskWidth := 1; taskWidth <= 4; taskWidth++ {
			for width := 53; width <= 57; width++ {
				a := foldAnalysis()
				for i := 3; i < finished; i++ { // more finished plain agents, ahead of the running one
					extra := foldAgent(fmt.Sprintf("e%010d", i), "", foldNow.Add(-5*time.Hour), foldNow.Add(-4*time.Hour), 3, 0.10)
					a.Agents = append([]models.AgentAnalysis{extra}, a.Agents...)
				}
				for i := range a.Agents {
					if a.Agents[i].AgentID == "a2000000000" {
						a.Agents[i].AgentType = strings.Repeat("Q", taskWidth)
					}
				}
				frame := watchAt(t, a, width)
				want := -1
				for _, line := range strings.Split(frame, "\n") {
					if !strings.Contains(line, "$") || !(strings.Contains(line, "[A") || strings.Contains(line, "finished agents") ||
						strings.Contains(line, "Parent session") || strings.Contains(line, "Agents subtotal")) {
						continue
					}
					if want < 0 {
						want = decimal(line)
					} else if got := decimal(line); got != want {
						t.Errorf("%d finished, %d-column task, width %d: cost at %d, want %d:\n%s", finished, taskWidth, width, got, want, line)
					}
				}
			}
		}
	}
}
