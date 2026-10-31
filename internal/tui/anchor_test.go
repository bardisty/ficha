package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// anchorAnalysis is foldAnalysis with a long COST BY MODEL section below
// the agents, Opus 4.8 at its head, and extra running agents started just
// before foldNow.
func anchorAnalysis(extra int) *models.SessionAnalysis {
	a := foldAnalysis()
	a.CostByModel = map[string]models.CostBreakdown{"claude-opus-4-8": {TotalCost: 20}}
	for i := range 24 {
		a.CostByModel[fmt.Sprintf("model-%02d", i)] = models.CostBreakdown{TotalCost: 1}
	}
	for i := range extra {
		a.Agents = append(a.Agents, foldAgent(fmt.Sprintf("e%d000000000", i), "", foldNow.Add(-20*time.Second), foldNow.Add(-10*time.Second), 3, 0.1))
	}
	a.AgentCount = len(a.Agents)
	return a
}

// anchorModel is watch at width x height on anchorAnalysis, its clock at
// foldNow.
func anchorModel(t *testing.T, width, height, extra int) Model {
	t.Helper()
	m := NewModel("/fixture/sess.jsonl", "sess", true, "", false)
	m.now = func() time.Time { return foldNow }
	return load(t, sized(t, m, width, height), anchorAnalysis(extra))
}

// topLine is the body's first line on screen, without escapes or padding.
func topLine(m Model) string {
	line, _, _ := strings.Cut(m.viewport.View(), "\n")
	return strings.TrimSpace(stripANSI(line))
}

// scrollToLine presses j until the body's top line contains text.
func scrollToLine(t *testing.T, m Model, text string) Model {
	t.Helper()
	for range 200 {
		if strings.Contains(topLine(m), text) {
			return m
		}
		m = press(t, m, "j")
	}
	t.Fatalf("no line with %q", text)
	return m
}

// Agents starting above the reader push the body down, and the offset with
// it: the line at the top stays at the top, the hidden count above grows by
// the rows added, and a resize that clamps the view gives the line back. A
// compact layout, which drops every blank line, keeps it the same way.
func TestWatchKeepsTheReadersLineWhenAgentsStart(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {40, 15}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := scrollToLine(t, anchorModel(t, size[0], size[1], 0), "Opus 4.8")
			before := m.viewport.YOffset()
			m = load(t, m, anchorAnalysis(3))
			if got := topLine(m); !strings.Contains(got, "Opus 4.8") {
				t.Fatalf("three agents starting moved the top line to %q", got)
			}
			if got := m.viewport.YOffset() - before; got != 3 {
				t.Errorf("offset moved %d, want the 3 rows added", got)
			}
			if rule := stripANSI(m.renderFooterRule(panelWidthFor(m.width))); !strings.Contains(rule, fmt.Sprintf("↑ %d more", m.viewport.YOffset())) {
				t.Errorf("footer rule doesn't count the new offset %d: %q", m.viewport.YOffset(), rule)
			}
			m = sized(t, sized(t, m, size[0], 200), size[0], size[1])
			if got := topLine(m); !strings.Contains(got, "Opus 4.8") {
				t.Errorf("after a tall resize and back the top line is %q", got)
			}
		})
	}
}

// Running agents going quiet fold into one row on the clock alone, and the
// body comes up by the rows folded, under the reader's line.
func TestWatchKeepsTheReadersLineWhenAgentsFold(t *testing.T) {
	m := scrollToLine(t, anchorModel(t, 80, 24, 3), "Opus 4.8")
	before := m.viewport.YOffset()
	m.now = func() time.Time { return foldNow.Add(idleAfter + time.Minute) }
	m = send(t, m, clockMsg{gen: m.clockGen})
	if got := topLine(m); !strings.Contains(got, "Opus 4.8") {
		t.Fatalf("agents folding moved the top line to %q", got)
	}
	if m.viewport.YOffset() >= before {
		t.Errorf("offset %d after the fold, want it under %d", m.viewport.YOffset(), before)
	}
}

// A reader on the blank line above a heading is reading the section below
// it, and the blank line moves with the heading.
func TestWatchKeepsTheLineAboveAHeading(t *testing.T) {
	m := press(t, scrollToLine(t, anchorModel(t, 80, 24, 0), "COST BY MODEL"), "k")
	if topLine(m) != "" {
		t.Fatalf("setup: top line %q, want the blank above the heading", topLine(m))
	}
	heading := func(m Model) string {
		lines := strings.Split(m.viewport.View(), "\n")
		return strings.TrimSpace(stripANSI(lines[1]))
	}
	started := load(t, m, anchorAnalysis(3))
	if got := heading(started); !strings.Contains(got, "COST BY MODEL") {
		t.Errorf("agents starting put %q under the blank line", got)
	}
	started.now = func() time.Time { return foldNow.Add(idleAfter + time.Minute) }
	folded := send(t, started, clockMsg{gen: started.clockGen})
	if got := heading(folded); !strings.Contains(got, "COST BY MODEL") {
		t.Errorf("agents folding put %q under the blank line", got)
	}
}

// At the top, and inside the agent list, the offset stays as it was: the
// line above the change is the reader's.
func TestWatchOffsetHoldsAboveTheChange(t *testing.T) {
	top := anchorModel(t, 80, 24, 0)
	if top = load(t, top, anchorAnalysis(3)); top.viewport.YOffset() != 0 {
		t.Errorf("at the top, agents starting moved the offset to %d", top.viewport.YOffset())
	}
	inList := scrollToLine(t, anchorModel(t, 80, 24, 0), "AGENT SUB-SESSIONS")
	inList = press(t, inList, "j")
	before := inList.viewport.YOffset()
	if inList = load(t, inList, anchorAnalysis(3)); inList.viewport.YOffset() != before {
		t.Errorf("inside the agent list the offset moved from %d to %d", before, inList.viewport.YOffset())
	}
}

// fitBody's line map follows what it drops, in both layouts.
func TestFitBodyMapsLines(t *testing.T) {
	body := "\n  a\n\n  b\n  \n  c\n\n"
	for _, tt := range []struct {
		compact bool
		want    string
		at      map[int]int
	}{
		{false, "  a\n\n  b\n  \n  c", map[int]int{0: 0, 1: 0, 3: 2, 5: 4}},
		{true, "  a\n  b\n  c", map[int]int{1: 0, 3: 1, 5: 2}},
	} {
		got, at := fitBody(body, tt.compact)
		if got != tt.want {
			t.Errorf("compact %v: body %q, want %q", tt.compact, got, tt.want)
		}
		for in, want := range tt.at {
			if out := at(in); out != want {
				t.Errorf("compact %v: line %d lands on %d, want %d", tt.compact, in, out, want)
			}
		}
	}
}
