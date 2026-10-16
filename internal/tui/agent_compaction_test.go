package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/styles"
)

// nearerAgentAnalysis is the golden session, Opus 4.8 at 380K of a 967K
// mark, with its running agent f6e5d4c3 past its own mark at 980K. The
// golden's other agents are finished, and one of them sits at its mark too.
func nearerAgentAnalysis() *models.SessionAnalysis {
	a := goldenViewAnalysis()
	a.Agents[0].Context = &models.ContextUsage{Tokens: 980_000, Window: 1_000_000, Percent: 98, CompactAt: 967_000}
	return a
}

// A running agent nearer ~compaction than the main conversation shows on the
// first screen: the note under the gauge names it, and the footer's ctx
// reading carries it. Each sheds whole as the terminal narrows, before the
// main reading does.
func TestGoldenWatchAgentNearerCompaction(t *testing.T) {
	for _, size := range [][2]int{{40, 15}, {60, 20}, {80, 24}, {120, 40}} {
		name := fmt.Sprintf("watch_agent_nearer_%dx%d", size[0], size[1])
		frame := goldenWatchViewOf(t, nearerAgentAnalysis(), true, size[0], size[1])
		checkGolden(t, name, frame)
		if !strings.Contains(frame, "ctx 38%") {
			t.Errorf("%s: the main reading must stay in the footer:\n%s", name, frame)
		}
	}
	checkGolden(t, "watch_agent_nearer_80x24_color", goldenWatchViewOf(t, nearerAgentAnalysis(), false, 80, 24))
}

// The note and footer each name the agent at 80 columns, and the note's
// agent part goes before the main conversation's headroom at 40. --ascii
// swaps the note's separator for its own.
func TestWatchAgentNearerCompactionText(t *testing.T) {
	frame := goldenWatchViewOf(t, nearerAgentAnalysis(), true, 80, 24)
	for _, want := range []string{
		"587.0K to ~compaction · agent Af6e5d4c 13.0K past ~compaction",
		"ctx 38% (agent 98%)",
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("80x24 frame lacks %q:\n%s", want, frame)
		}
	}
	styles.SetASCII(true)
	t.Cleanup(func() { styles.SetASCII(false) })
	ascii := goldenWatchViewOf(t, nearerAgentAnalysis(), true, 80, 24)
	styles.SetASCII(false)
	if want := "587.0K to ~compaction | agent Af6e5d4c 13.0K past ~compaction"; !strings.Contains(ascii, want) {
		t.Errorf("--ascii 80x24 frame lacks %q:\n%s", want, ascii)
	}
	narrow := goldenWatchViewOf(t, nearerAgentAnalysis(), true, 40, 15)
	if !strings.Contains(narrow, "587.0K to ~compaction") {
		t.Errorf("40x15 frame lost the main headroom:\n%s", narrow)
	}
}

// Only a running agent in its warning band or past its mark, and nearer its
// mark than the main conversation is to its own, counts.
func TestWatchAgentNearerCompactionOnlyWhenItShould(t *testing.T) {
	tests := []struct {
		name   string
		change func(a *models.SessionAnalysis)
	}{
		// The golden's own agents: the running one is clear, and the
		// finished ones are at or near their marks.
		{"finished agents past their marks", func(*models.SessionAnalysis) {}},
		{"running agent below its band", func(a *models.SessionAnalysis) {
			a.Agents[0].Context = &models.ContextUsage{Tokens: 800_000, Window: 1_000_000, Percent: 80, CompactAt: 967_000}
		}},
		{"running agent in its band but further than main", func(a *models.SessionAnalysis) {
			a.LastMessageUsage = models.TokenUsage{InputTokens: 960_000}
			a.Agents[0].Context = &models.ContextUsage{Tokens: 900_000, Window: 1_000_000, Percent: 90, CompactAt: 967_000}
		}},
		{"running agent without a window", func(a *models.SessionAnalysis) {
			a.Agents[0].Context = &models.ContextUsage{Tokens: 1_500_000}
		}},
		{"running agent in an ended workflow run", func(a *models.SessionAnalysis) {
			a.Agents[3].EndTime = a.Agents[0].EndTime
			a.Agents[3].Context = &models.ContextUsage{Tokens: 980_000, Window: 1_000_000, Percent: 98, CompactAt: 967_000}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := goldenViewAnalysis()
			tt.change(a)
			frame := goldenWatchViewOf(t, a, true, 120, 40)
			if strings.Contains(frame, "agent A") || strings.Contains(frame, "(agent ") {
				t.Errorf("frame names an agent it shouldn't:\n%s", frame)
			}
		})
	}
}

// With two running agents in their bands, the one nearer its own mark is
// named: 190K of a 200K mark (95%) beats 900K of a 967K mark (93%), though
// the second holds far more tokens. A reading past its mark beats both.
func TestNearestAgentComparesEachAgainstItsOwnMark(t *testing.T) {
	a := goldenViewAnalysis()
	now := goldenTime(11, 30, 12)
	main := &models.ContextUsage{Tokens: 380_000, Window: 1_000_000, Percent: 38, CompactAt: 967_000}
	a.Agents[0].Context = &models.ContextUsage{Tokens: 900_000, Window: 1_000_000, Percent: 90, CompactAt: 967_000}
	a.Agents[1].EndTime = a.Agents[0].EndTime
	a.Agents[1].Context = &models.ContextUsage{Tokens: 190_000, Window: 200_000, Percent: 95, CompactAt: 200_000}
	if got := nearestAgent(a, main, now); got == nil || got.AgentID != a.Agents[1].AgentID {
		t.Fatalf("nearestAgent = %v, want the 200K agent at 95%% of its mark", got)
	}
	a.Agents[0].Context = &models.ContextUsage{Tokens: 970_000, Window: 1_000_000, Percent: 97, CompactAt: 967_000}
	if got := nearestAgent(a, main, now); got == nil || got.AgentID != a.Agents[0].AgentID {
		t.Fatalf("nearestAgent = %v, want the 1M agent past its mark", got)
	}
}
