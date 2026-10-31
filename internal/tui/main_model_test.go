package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bardisty/ficha/internal/models"
)

// mainModelFrame renders watch on a at width by height, on the golden clock.
func mainModelFrame(t *testing.T, a *models.SessionAnalysis, width, height int) string {
	t.Helper()
	m := NewModel("/fixture/sess.jsonl", "sess", true, "", true)
	m.now = func() time.Time { return goldenTime(11, 30, 12) }
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	updated, _ = updated.Update(analysisMsg{analysis: a})
	return frameOf(updated.(Model))
}

// lineWith is the first line of frame containing s.
func lineWith(frame, s string) string {
	for line := range strings.SplitSeq(frame, "\n") {
		if strings.Contains(line, s) {
			return line
		}
	}
	return ""
}

// column is the display column s starts at in line, or -1.
func column(line, s string) int {
	i := strings.Index(line, s)
	if i < 0 {
		return -1
	}
	return lipgloss.Width(line[:i])
}

// After a switch to another model, watch names the main conversation's
// current model: in its row of the agent table, under the agents' models,
// or, in a session without agents, in the note under the gauge.
func TestWatchNamesMainConversationModel(t *testing.T) {
	a := goldenViewAnalysis()
	a.LastMessageModel = "claude-sonnet-5" // most of the cost is still Opus 4.8

	frame := mainModelFrame(t, a, 80, 70)
	main, agent := lineWith(frame, "Main conversation"), lineWith(frame, "[Af6e5d4c]")
	if col := column(main, "Sonnet 5"); col < 0 || col != column(agent, "Sonnet 5") {
		t.Errorf("main row names its model at column %d, the agent rows at %d:\n%s\n%s", col, column(agent, "Sonnet 5"), main, agent)
	}
	if column(main, "$") != column(agent, "$") {
		t.Errorf("costs out of line:\n%s\n%s", main, agent)
	}
	if note := lineWith(frame, "to ~compaction"); strings.Contains(note, "Sonnet") {
		t.Errorf("with agents the note shouldn't name the model too: %q", note)
	}

	// Without a task column the model can't sit under the agents' models,
	// left of the label's end, so it follows the label.
	frame = mainModelFrame(t, a, 50, 70)
	if main := lineWith(frame, "Main conversation"); !strings.Contains(main, "Main conversation  Sonnet 5 ") {
		t.Errorf("width 50: want the model after the label: %q", main)
	}

	a.HasAgents, a.Agents, a.AgentCount = false, nil, 0
	for _, tt := range []struct {
		width int
		want  string
	}{
		{80, "(Sonnet 5, main, last request)"},
		{60, "(Sonnet 5, last request)"},
		{70, "(Sonnet 5, main, last request)"},
		{40, "to ~compaction"},
	} {
		note := strings.TrimRight(lineWith(mainModelFrame(t, a, tt.width, 40), "to ~compaction"), " ")
		if !strings.HasSuffix(note, tt.want) {
			t.Errorf("width %d, no agents: note %q, want it to end %q", tt.width, note, tt.want)
		}
	}

	// A window ficha doesn't know already names the model, once.
	a.LastMessageModel = "claude-mystery-9"
	frame = mainModelFrame(t, a, 80, 40)
	if note := lineWith(frame, "window unknown"); strings.Count(note, "mystery") != 1 {
		t.Errorf("want the model named once: %q", note)
	}
}
