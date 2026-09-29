package analyzer

import (
	"sort"

	"github.com/bardisty/ficha/internal/models"
)

// agentOrder returns the positions of agents in the order every view lists
// them: plain agents first, then each workflow run's agents together, with
// runs ordered by their first agent's start. Within a group agents go by
// first message, so a row keeps its place as a live session grows and a new
// agent lands at the bottom of its group. An agent with no timestamped
// message sorts last; ties keep discovery order.
//
// The formatters draw a run's heading where its WorkflowID changes, so each
// run's agents must stay contiguous.
func agentOrder(agents []models.AgentAnalysis) []int {
	byStart := make([]int, len(agents))
	for i := range byStart {
		byStart[i] = i
	}
	sort.SliceStable(byStart, func(i, j int) bool {
		return startsBefore(agents[byStart[i]], agents[byStart[j]])
	})

	// Walking in start order, a run is first seen at its earliest agent, so
	// runs come out ordered by when they started.
	var plain []int
	runs := map[string][]int{}
	var runOrder []string
	for _, i := range byStart {
		id := agents[i].WorkflowID
		if id == "" {
			plain = append(plain, i)
			continue
		}
		if _, seen := runs[id]; !seen {
			runOrder = append(runOrder, id)
		}
		runs[id] = append(runs[id], i)
	}
	order := plain
	for _, id := range runOrder {
		order = append(order, runs[id]...)
	}
	return order
}

// startsBefore is agentOrder's order for two agents: by first message, with
// an agent that has none after one that has.
func startsBefore(x, y models.AgentAnalysis) bool {
	if x.StartTime.IsZero() || y.StartTime.IsZero() {
		return !x.StartTime.IsZero() && y.StartTime.IsZero()
	}
	return x.StartTime.Before(y.StartTime)
}
