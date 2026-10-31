package tui

import (
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
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

// agentLive reports whether agent counts as running in a's list, the rows
// that carry the dot: it wrote within idleAfter of now, and it isn't in a
// workflow run that has ended.
func agentLive(a *models.SessionAnalysis, agent models.AgentAnalysis, now time.Time) bool {
	return agentRunning(agent, now) && (agent.WorkflowID == "" || !runEnded(a, agent.WorkflowID))
}

// runningAgentsKey names the agents running at now, so a clock tick can tell
// whether the list's dots or folds changed without re-rendering it.
func runningAgentsKey(a *models.SessionAnalysis, now time.Time) string {
	if a == nil {
		return ""
	}
	var ids []string
	for _, agent := range a.Agents {
		if agentLive(a, agent, now) {
			ids = append(ids, agent.AgentID)
		}
	}
	return strings.Join(ids, ",")
}

// nearestAgent is the running agent nearest ~compaction, when it's in its
// warning band or past its mark and nearer its own mark than main, the
// session's reading, is to its own. Each reading measures against its own
// model's mark, as tokens over the mark, so an agent at 190K of a 200K mark
// (95%) outranks a session at 900K of a 967K one (93%). A main reading with
// no mark counts as far from one: ficha can't place it, and an agent near
// its own mark is a warning by itself. nil when no agent qualifies.
func nearestAgent(a *models.SessionAnalysis, main *models.ContextUsage, now time.Time) *models.AgentAnalysis {
	var nearest *models.AgentAnalysis
	best := markShare(main)
	for i, agent := range a.Agents {
		c := agent.Context
		if c == nil || !agentLive(a, agent, now) || pricing.ContextZoneOf(c.Tokens, c.CompactAt) == pricing.ContextClear {
			continue
		}
		if share := markShare(c); share > best {
			nearest, best = &a.Agents[i], share
		}
	}
	return nearest
}

// markShare is how far along to its ~compaction mark a reading is, 1 at the
// mark, or 0 for a reading with no mark.
func markShare(c *models.ContextUsage) float64 {
	if c == nil || c.CompactAt <= 0 {
		return 0
	}
	return float64(c.Tokens) / float64(c.CompactAt)
}

// agentRows lays out the agent list: plain agents, then each workflow run
// under its heading, in the order the analyzer lists them, which is by first
// message. Above agentListCap agents, finished plain agents fold into one
// line ahead of the rest, and a run that has ended folds to its heading,
// which carries the run's subtotal. A run whose status can't be read has
// ended once all its agents have finished. Running and just-started agents
// never fold.
//
// The costliest finished plain agent stays out of the fold, listed above it,
// so the agent that spent the most is never hidden behind a count. A fold
// needs at least two agents in it, so plain agents fold only once three have
// finished. A finished agent's cost stops changing, so the kept-out row moves
// only when a newly finished agent costs more; comparing against running
// agents, which keep getting dearer, would fold it back in mid-session.
//
// open lists every agent on its own row, folds or no folds: the a key's
// way to the agents a fold hides.
func agentRows(a *models.SessionAnalysis, now time.Time, open bool) []agentRow {
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

	fold := !open && len(a.Agents) > agentListCap
	var rows []agentRow
	// Of equal costs, the one that finished first stays out, so an agent
	// finishing later at the same cost can't move it.
	costliest, finishedCount := -1, 0
	for i, agent := range plain {
		if !agentFinished(agent, now) {
			continue
		}
		finishedCount++
		if costliest < 0 || costlierFinish(agent, plain[costliest]) {
			costliest = i
		}
	}
	foldPlain := fold && finishedCount >= 3
	if foldPlain {
		finished := agentRow{kind: agentRowFinished}
		for i, agent := range plain {
			if i != costliest && agentFinished(agent, now) {
				finished.count++
				finished.msgs += agent.MessageCount
				finished.cost += agent.TotalCost.TotalCost
			}
		}
		rows = append(rows, agentRow{kind: agentRowAgent, agent: plain[costliest]}, finished)
	}
	for _, agent := range plain {
		if foldPlain && agentFinished(agent, now) {
			continue
		}
		rows = append(rows, agentRow{kind: agentRowAgent, agent: agent, running: agentLive(a, agent, now)})
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
			rows = append(rows, agentRow{kind: agentRowAgent, agent: agent, running: agentLive(a, agent, now)})
		}
	}
	return rows
}

// costlierFinish reports whether finished agent a outranks b for the row
// kept out of the fold: it cost more, or the same and finished first.
func costlierFinish(a, b models.AgentAnalysis) bool {
	if a.TotalCost.TotalCost != b.TotalCost.TotalCost {
		return a.TotalCost.TotalCost > b.TotalCost.TotalCost
	}
	return a.EndTime.Before(b.EndTime)
}

func allFinished(agents []models.AgentAnalysis, now time.Time) bool {
	for _, agent := range agents {
		if !agentFinished(agent, now) {
			return false
		}
	}
	return true
}
