package tui

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/styles"
)

// foldNow is the clock for the fold fixture: agents that ended within
// idleAfter of it are running.
var foldNow = goldenTime(12, 0, 0)

func foldAgent(id, run string, start, end time.Time, msgs int, cost float64) models.AgentAnalysis {
	return models.AgentAnalysis{
		AgentID:      id,
		WorkflowID:   run,
		MessageCount: msgs,
		StartTime:    start,
		EndTime:      end,
		TotalCost:    models.CostBreakdown{TotalCost: cost},
		CostByModel:  map[string]models.CostBreakdown{"claude-sonnet-5": {TotalCost: cost}},
	}
}

// foldAnalysis has ten agents, over the cap, in the analyzer's order:
// three finished plain agents and one running, a completed run, a run still
// in progress whose agents have all gone quiet, and a run with an unreadable
// status and one running agent.
func foldAnalysis() *models.SessionAnalysis {
	ago := func(d time.Duration) time.Time { return foldNow.Add(-d) }
	agents := []models.AgentAnalysis{
		foldAgent("a3000000000", "", ago(4*time.Hour), ago(3*time.Hour), 32, 0.68),
		foldAgent("a1000000000", "", ago(3*time.Hour), ago(2*time.Hour), 25, 1.04),
		foldAgent("a4000000000", "", ago(2*time.Hour), ago(time.Hour), 14, 0.44),
		foldAgent("a2000000000", "", ago(50*time.Minute), ago(40*time.Second), 22, 0.91),
		foldAgent("b2000000000", "wf_done", ago(95*time.Minute), ago(85*time.Minute), 17, 1.20),
		foldAgent("b1000000000", "wf_done", ago(90*time.Minute), ago(80*time.Minute), 21, 4.10),
		foldAgent("c1000000000", "wf_busy", ago(30*time.Minute), ago(10*time.Minute), 21, 1.82),
		foldAgent("c2000000000", "wf_busy", ago(25*time.Minute), ago(9*time.Minute), 9, 0.30),
		foldAgent("d1000000000", "wf_mixed", ago(20*time.Minute), ago(15*time.Minute), 8, 0.25),
		foldAgent("d2000000000", "wf_mixed", ago(18*time.Minute), ago(5*time.Second), 11, 0.60),
	}
	a := &models.SessionAnalysis{
		SessionID: "sess",
		Agents:    agents,
		HasAgents: true,
		Workflows: []models.WorkflowMeta{
			{RunID: "wf_done", Name: "review-changes", Status: "completed"},
			{RunID: "wf_busy", Name: "audit-codebase", Status: runningStatus},
			{RunID: "wf_mixed"},
		},
		ParentCost: models.CostBreakdown{TotalCost: 30},
	}
	for _, agent := range agents {
		a.AgentsCost.Add(agent.TotalCost)
	}
	a.AgentCount = len(agents)
	a.TotalCost.Add(a.ParentCost)
	a.TotalCost.Add(a.AgentsCost)
	for _, agent := range agents {
		a.MessageCount += agent.MessageCount
	}
	a.WorkflowCount = len(a.Workflows)
	return a
}

// rowSummary names each row for comparison: the agent ID with a trailing *
// when running, "run:<id>" or "run:<id>/folded", or "N finished".
func rowSummary(rows []agentRow) []string {
	var got []string
	for _, r := range rows {
		switch r.kind {
		case agentRowAgent:
			s := r.agent.AgentID
			if r.running {
				s += "*"
			}
			got = append(got, s)
		case agentRowRun:
			s := "run:" + r.runID
			if r.folded {
				s += "/folded"
			}
			got = append(got, s)
		case agentRowFinished:
			got = append(got, fmt.Sprintf("%d finished", r.count))
		}
	}
	return got
}

func TestAgentRowsFoldAboveCap(t *testing.T) {
	got := rowSummary(agentRows(foldAnalysis(), foldNow))
	want := []string{
		"3 finished", "a2000000000*",
		"run:wf_done/folded",
		// Still in progress per its status, so its quiet agents stay listed.
		"run:wf_busy", "c1000000000", "c2000000000",
		// No status to go by, and one agent is still writing.
		"run:wf_mixed", "d1000000000", "d2000000000*",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("rows:\n got %v\nwant %v", got, want)
	}
}

// At the cap nothing folds; every agent lists in first-message order.
func TestAgentRowsAtCapListEveryAgent(t *testing.T) {
	a := foldAnalysis()
	a.Agents = a.Agents[:agentListCap]
	got := rowSummary(agentRows(a, foldNow))
	want := []string{
		"a3000000000", "a1000000000", "a4000000000", "a2000000000*",
		"run:wf_done", "b2000000000", "b1000000000",
		"run:wf_busy", "c1000000000", "c2000000000",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("rows:\n got %v\nwant %v", got, want)
	}
}

// An agent with no reply yet has only just started: it stays listed, and
// keeps its run from folding, even when the run's status is unreadable.
func TestAgentRowsKeepAJustStartedAgent(t *testing.T) {
	a := foldAnalysis()
	a.Workflows[0].Status = ""
	a.Agents = append(a.Agents,
		models.AgentAnalysis{AgentID: "e0000000000"},
		models.AgentAnalysis{AgentID: "b3000000000", WorkflowID: "wf_done"})
	got := strings.Join(rowSummary(agentRows(a, foldNow)), " ")
	want := "3 finished a2000000000* e0000000000 run:wf_done b2000000000 b1000000000 b3000000000"
	if !strings.HasPrefix(got, want) {
		t.Errorf("rows:\n got %v\nwant prefix %v", got, want)
	}
}

// A run's recorded end outranks its agents' activity: an agent that wrote
// seconds before its run completed gets no dot, and the run folds at once.
func TestAgentRowsEndedRunOutranksActivity(t *testing.T) {
	a := foldAnalysis()
	a.Agents[4].EndTime = foldNow.Add(-5 * time.Second) // b1, in wf_done
	got := strings.Join(rowSummary(agentRows(a, foldNow)), " ")
	if !strings.Contains(got, "run:wf_done/folded") {
		t.Errorf("completed run didn't fold: %v", got)
	}
	a.Agents = a.Agents[:agentListCap]
	got = strings.Join(rowSummary(agentRows(a, foldNow)), " ")
	if !strings.Contains(got, "run:wf_done b2000000000 b1000000000 ") {
		t.Errorf("completed run's agent has a dot: %v", got)
	}
	if strings.Contains(runningAgentsKey(a, foldNow), "b1000000000") {
		t.Error("runningAgentsKey counts an agent of a completed run")
	}
}

// One finished plain agent would fold into a line of its own; list it.
func TestAgentRowsKeepALoneFinishedAgent(t *testing.T) {
	a := foldAnalysis()
	a.Agents[0].EndTime = foldNow
	a.Agents[2].EndTime = foldNow
	for _, r := range agentRows(a, foldNow) {
		if r.kind == agentRowFinished {
			t.Fatalf("folded %d agent(s) into a line", r.count)
		}
	}
}

// The analyzer orders agents; the list keeps that order rather than sorting
// again, so it agrees row for row with show and summary. Given agents out of
// start order, it lists them as given, plain agents first.
func TestAgentRowsKeepAnalyzerOrder(t *testing.T) {
	a := &models.SessionAnalysis{Agents: []models.AgentAnalysis{
		foldAgent("late", "", goldenTime(11, 0, 0), goldenTime(11, 5, 0), 1, 0),
		foldAgent("r1a", "wf_a", goldenTime(10, 40, 0), goldenTime(10, 45, 0), 1, 0),
		foldAgent("r1b", "wf_a", goldenTime(10, 20, 0), goldenTime(10, 25, 0), 1, 0),
		foldAgent("r2a", "wf_b", goldenTime(10, 30, 0), goldenTime(10, 35, 0), 1, 0),
		foldAgent("early", "", goldenTime(10, 0, 0), goldenTime(10, 5, 0), 1, 0),
	}}
	got := rowSummary(agentRows(a, foldNow))
	want := []string{"late", "early", "run:wf_a", "r1a", "r1b", "run:wf_b", "r2a"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("rows:\n got %v\nwant %v", got, want)
	}
}

// Folding hides rows, never spend: the fold line, the listed agents and the
// folded runs' subtotals add up to the agents subtotal.
func TestAgentRowsKeepTotals(t *testing.T) {
	a := foldAnalysis()
	var sum float64
	for _, r := range agentRows(a, foldNow) {
		switch {
		case r.kind == agentRowFinished:
			sum += r.cost
		case r.kind == agentRowAgent:
			sum += r.agent.TotalCost.TotalCost
		case r.folded:
			sum += workflowCost(a.Agents, r.runID)
		}
	}
	if math.Abs(sum-a.AgentsCost.TotalCost) > 1e-9 {
		t.Errorf("rows sum to %v, agents subtotal is %v", sum, a.AgentsCost.TotalCost)
	}
}

// The live dot ages out on a clock tick, without waiting for a reload, and
// the agent folds away with it.
func TestAgentDotAgesOutOnClockTick(t *testing.T) {
	clock := foldNow
	m := NewModel("/p/s.jsonl", "s", true, "", false)
	m.now = func() time.Time { return clock }
	m = load(t, sized(t, m, 80, 60), foldAnalysis())
	if !strings.Contains(m.viewport.View(), styles.LiveDot+" [A2000000]") {
		t.Fatalf("running agent has no live dot:\n%s", m.viewport.View())
	}
	clock = foldNow.Add(idleAfter)
	updated, _ := m.Update(clockMsg{gen: m.clockGen})
	m = updated.(Model)
	view := m.viewport.View()
	if strings.Contains(view, "[A2000000]") || !strings.Contains(view, "4 finished agents") {
		t.Errorf("agent still listed after going quiet:\n%s", view)
	}
}

func goldenAgentList(t *testing.T, noColor bool) string {
	t.Helper()
	m := NewModel("/fixture/sess.jsonl", "sess", noColor, "", false)
	m.now = func() time.Time { return foldNow }
	m.analysis = foldAnalysis()
	return m.renderAgentBreakdownContent()
}

func TestGoldenWatchAgentsFolded(t *testing.T) {
	checkGolden(t, "watch_agents_folded", goldenAgentList(t, true))
}

func TestGoldenWatchAgentsFoldedColor(t *testing.T) {
	checkGolden(t, "watch_agents_folded_color", goldenAgentList(t, false))
}

func TestGoldenWatchAgentsFoldedASCII(t *testing.T) {
	useASCII(t)
	checkGolden(t, "watch_agents_folded_ascii", goldenAgentList(t, true))
}

// A run name too long for its heading with a status drops every heading's
// status at full width too, not only on a narrow terminal, so the short
// name beside it can't be the only run that reads as having one.
func TestRunHeadingStatusesAllOrNone(t *testing.T) {
	for _, width := range []int{0, 100, 60} {
		m := NewModel("/fixture/sess.jsonl", "sess", true, "", false)
		m.now = func() time.Time { return foldNow }
		m.width = width
		m.analysis = foldAnalysis()
		m.analysis.Workflows[1].Name = "audit-codebase-nightly"
		got := m.renderAgentBreakdownContent()
		for _, want := range []string{"── workflow: review-changes  ", "── workflow: audit-codebase-nightly  "} {
			if !strings.Contains(got, want) {
				t.Errorf("width %d: no heading %q:\n%s", width, want, got)
			}
		}
		if strings.Contains(got, "(completed)") || strings.Contains(got, "(running)") {
			t.Errorf("width %d: a heading kept its status:\n%s", width, got)
		}
	}
}

// As the terminal narrows, an agent row gives up its gap, then its message
// count, then its context reading, and every cost in the section stays in
// one column, the folded rows' too.
func TestAgentColumnsDropCountBeforeReading(t *testing.T) {
	for _, tc := range []struct {
		width      int
		msgs, ctxs bool
	}{
		{58, true, true},
		{56, true, true},
		{55, false, true},
		{47, false, true},
		{46, false, false},
	} {
		for name, analysis := range map[string]*models.SessionAnalysis{"view": goldenViewAnalysis(), "folded": foldAnalysis()} {
			m := NewModel("/fixture/sess.jsonl", "sess", false, "", false)
			m.now = func() time.Time { return foldNow }
			m.width = tc.width
			m.analysis = analysis
			out := stripANSI(m.renderAgentBreakdownContent())

			// Every row ends in its cost cell, so equal widths mean
			// aligned costs.
			rowWidth := -1
			for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
				if !strings.Contains(line, "$") {
					t.Fatalf("width %d %s: row lost its cost: %q", tc.width, name, line)
				}
				if w := lipgloss.Width(line); rowWidth >= 0 && w != rowWidth {
					t.Errorf("width %d %s: row %q is %d wide, want %d:\n%s", tc.width, name, line, w, rowWidth, out)
				} else {
					rowWidth = w
				}
			}
			if got := strings.Contains(out, "msg"); got != tc.msgs {
				t.Errorf("width %d %s: message counts shown = %v, want %v:\n%s", tc.width, name, got, tc.msgs, out)
			}
			if name == "view" && strings.Contains(out, "% ctx") != tc.ctxs {
				t.Errorf("width %d: context readings shown = %v, want %v:\n%s", tc.width, !tc.ctxs, tc.ctxs, out)
			}
		}
	}
}
