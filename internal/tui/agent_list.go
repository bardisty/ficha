package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// agentListCap is how many agents watch lists one row each. Above it,
// finished agents fold so a big workflow session still leaves the rest of
// the dashboard on screen; breakdown keeps per-message detail for all of them.
const agentListCap = 8

// runningStatus is a workflow status that says the run is in progress. A run
// that says so never folds, even while its agents are all quiet on long tool
// calls. Claude Code may write no status file until a run ends, which reads
// as "" and leaves the call to the agents.
const runningStatus = "running"

// runEnded reports whether a workflow run's recorded status says it's over.
// That outranks its agents' activity: a run that just completed has agents
// that wrote seconds ago, and a dot under "(completed)" would contradict it.
// An unreadable status ("") leaves the call to the agents.
func runEnded(a *models.SessionAnalysis, runID string) bool {
	status := a.WorkflowByID(runID).Status
	return status != "" && status != runningStatus
}

// agentRowKind tells the agent list's row types apart.
type agentRowKind int

const (
	agentRowAgent    agentRowKind = iota // one agent
	agentRowRun                          // a workflow run's heading
	agentRowFinished                     // plain agents folded into one line
)

// agentRow is one line of watch's agent list.
type agentRow struct {
	kind    agentRowKind
	agent   models.AgentAnalysis // agentRowAgent
	running bool                 // agentRowAgent: wrote within idleAfter
	runID   string               // agentRowRun
	folded  bool                 // agentRowRun: its agent rows are hidden
	count   int                  // agentRowFinished: agents folded
	msgs    int                  // agentRowFinished: their messages
	cost    float64              // agentRowFinished: their cost
}

// agentRunning reports whether an agent wrote within idleAfter of now, the
// same window the header uses before it calls the session idle.
func agentRunning(agent models.AgentAnalysis, now time.Time) bool {
	return !agent.EndTime.IsZero() && now.Sub(agent.EndTime) < idleAfter
}

// agentFinished reports whether an agent has gone quiet. One with no reply
// yet has only just started, so it isn't finished, and it isn't running
// either: one aborted before its first reply would keep the dot forever.
func agentFinished(agent models.AgentAnalysis, now time.Time) bool {
	return !agent.EndTime.IsZero() && !agentRunning(agent, now)
}

// runningAgentsKey names the agents running at now, so a clock tick can tell
// whether the list's dots or folds changed without re-rendering it.
func runningAgentsKey(a *models.SessionAnalysis, now time.Time) string {
	if a == nil {
		return ""
	}
	var ids []string
	for _, agent := range a.Agents {
		if agentRunning(agent, now) && (agent.WorkflowID == "" || !runEnded(a, agent.WorkflowID)) {
			ids = append(ids, agent.AgentID)
		}
	}
	return strings.Join(ids, ",")
}

// sortByStart orders agents by their first message, so rows keep their
// place across reloads and a new agent lands at the bottom of its group. An
// agent with no timestamped message yet sorts last; ties keep discovery
// order.
func sortByStart(agents []models.AgentAnalysis) {
	sort.SliceStable(agents, func(i, j int) bool { return startsBefore(agents[i], agents[j]) })
}

// startsBefore is sortByStart's order for two agents.
func startsBefore(x, y models.AgentAnalysis) bool {
	if x.StartTime.IsZero() || y.StartTime.IsZero() {
		return !x.StartTime.IsZero() && y.StartTime.IsZero()
	}
	return x.StartTime.Before(y.StartTime)
}

// agentRows lays out the agent list: plain agents, then each workflow run
// under its heading, all in first-message order. Above agentListCap agents,
// two or more finished plain agents fold into one line ahead of the rest,
// and a run that has ended folds to its heading, which carries the run's
// subtotal. A run whose status can't be read has ended once all its agents
// have finished. Running and just-started agents never fold.
func agentRows(a *models.SessionAnalysis, now time.Time) []agentRow {
	var plain []models.AgentAnalysis
	runs := map[string][]models.AgentAnalysis{}
	var runOrder []string
	for _, agent := range a.Agents {
		if agent.WorkflowID == "" {
			plain = append(plain, agent)
			continue
		}
		if _, seen := runs[agent.WorkflowID]; !seen {
			runOrder = append(runOrder, agent.WorkflowID)
		}
		runs[agent.WorkflowID] = append(runs[agent.WorkflowID], agent)
	}
	sortByStart(plain)
	for _, id := range runOrder {
		sortByStart(runs[id])
	}
	// A run's first agent stands for it: a run sorts by when it started.
	sort.SliceStable(runOrder, func(i, j int) bool {
		return startsBefore(runs[runOrder[i]][0], runs[runOrder[j]][0])
	})

	fold := len(a.Agents) > agentListCap
	var rows []agentRow
	finished := agentRow{kind: agentRowFinished}
	for _, agent := range plain {
		if agentFinished(agent, now) {
			finished.count++
			finished.msgs += agent.MessageCount
			finished.cost += agent.TotalCost.TotalCost
		}
	}
	foldPlain := fold && finished.count >= 2
	if foldPlain {
		rows = append(rows, finished)
	}
	for _, agent := range plain {
		if foldPlain && agentFinished(agent, now) {
			continue
		}
		rows = append(rows, agentRow{kind: agentRowAgent, agent: agent, running: agentRunning(agent, now)})
	}

	for _, id := range runOrder {
		agents := runs[id]
		ended := runEnded(a, id)
		status := a.WorkflowByID(id).Status
		folded := fold && (ended || status == "" && allFinished(agents, now))
		rows = append(rows, agentRow{kind: agentRowRun, runID: id, folded: folded})
		if folded {
			continue
		}
		for _, agent := range agents {
			rows = append(rows, agentRow{kind: agentRowAgent, agent: agent, running: !ended && agentRunning(agent, now)})
		}
	}
	return rows
}

func allFinished(agents []models.AgentAnalysis, now time.Time) bool {
	for _, agent := range agents {
		if !agentFinished(agent, now) {
			return false
		}
	}
	return true
}
