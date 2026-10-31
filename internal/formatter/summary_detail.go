package formatter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
)

// The orders a session list and summary's per-session breakdown take, as
// --sort-by names them.
const (
	// SortByTime is by last message: newest first in the tables, oldest
	// first in summary's json and csv.
	SortByTime = "time"
	// SortByCost is costliest first everywhere (see SortSessionsByCost).
	SortByCost = "cost"
)

// SortSessionsByCost orders results costliest first. A session with nothing
// to price, no replies or an unreadable transcript, goes last. Equal costs
// go newest first, then by ID, so the order never depends on the input's.
func SortSessionsByCost(results []models.SessionResult) {
	priced := func(r models.SessionResult) bool { return r.Analysis != nil && r.Analysis.MessageCount > 0 }
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i], results[j]
		if priced(a) != priced(b) {
			return priced(a)
		}
		if priced(a) && a.Analysis.TotalCost.TotalCost != b.Analysis.TotalCost.TotalCost {
			return a.Analysis.TotalCost.TotalCost > b.Analysis.TotalCost.TotalCost
		}
		if !a.LastActive().Equal(b.LastActive()) {
			return a.LastActive().After(b.LastActive())
		}
		return a.Entry.SessionID < b.Entry.SessionID
	})
}

// sortedSuccessfulResults returns the results that parsed, in the SESSION
// BREAKDOWN table's order: oldest first by last message, the table read from
// the bottom, or costliest first under SortByCost, as the table reads.
// Failed sessions (nil Analysis) are dropped: machine formats export only
// sessions that could be analyzed, and the aggregate's skipped_sessions plus
// the stderr warning account for the rest.
func sortedSuccessfulResults(results []models.SessionResult, sortBy string) []models.SessionResult {
	sorted := make([]models.SessionResult, 0, len(results))
	for _, r := range results {
		if r.Analysis != nil {
			sorted = append(sorted, r)
		}
	}
	if sortBy == SortByCost {
		SortSessionsByCost(sorted)
		return sorted
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].LastActive().Before(sorted[j].LastActive())
	})
	return sorted
}

// primaryModelID returns the raw model ID with the highest cost, or "" when the
// map is empty. Machine formats use raw IDs (like cost_by_model keys), not the
// table's display names.
func primaryModelID(costByModel map[string]models.CostBreakdown) string {
	ordered := render.OrderModelsByCost(costByModel)
	if len(ordered) == 0 {
		return ""
	}
	return ordered[0]
}

// primarySessionModelID picks the dominant parent-session model, falling back to
// the combined map when parent-only costs are unavailable.
func primarySessionModelID(a *models.SessionAnalysis) string {
	if len(a.ParentCostByModel) > 0 {
		return primaryModelID(a.ParentCostByModel)
	}
	return primaryModelID(a.CostByModel)
}

// FormatSummaryDetailJSON renders the summary aggregate plus a per-session
// breakdown. When expandAgents is false each session's nested agents array is
// omitted (its agent_count/agents_cost still convey the rollup); when true the
// full agent sub-sessions are included. sortBy orders the sessions, as
// sortedSuccessfulResults says.
func FormatSummaryDetailJSON(summary *models.SessionAnalysis, results []models.SessionResult, expandAgents, pretty bool, sortBy string) (string, error) {
	sorted := sortedSuccessfulResults(results, sortBy)
	sessions := make([]models.SessionAnalysis, 0, len(sorted))
	for _, r := range sorted {
		// Shallow copy so nil-ing the agents slice does not mutate the caller's
		// analysis; the remaining reference fields are only read during marshal.
		s := *r.Analysis
		if !expandAgents {
			s.Agents = nil
		}
		sessions = append(sessions, s)
	}

	detail := models.SummaryDetail{Summary: summary, Sessions: sessions}

	var data []byte
	var err error
	if pretty {
		data, err = json.MarshalIndent(detail, "", "  ")
	} else {
		data, err = json.Marshal(detail)
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FormatSummaryDetailCSV renders one row per session as a single flat table. A
// row_type column discriminates "session" rows from the "agent" rows added when
// expandAgents is set. project_path (the project dir, shared with the summary
// aggregate for joining) and session_file (the transcript path) ride the session
// rows only — join agent rows back through session_id. Session rows carry the
// session total (message_count, costs and skipped_lines include agents); agent
// rows break out each agent — do not sum across row types: sum session rows for
// totals, agent rows for agent spend. On session rows parent_cost + agents_cost
// = total_cost, so the file reconciles against itself; agent rows leave both
// empty. cumulative_cost is the running session total in the rows' order,
// which sortBy sets as sortedSuccessfulResults says, and is empty on agent
// rows. agent_id holds the agent's real
// ID — the same key `show --messages` and the json agents[] array use, so the
// exports join.
// skipped_agents says how many agents a session row's agent_count does not
// include, and is empty on agent rows. agent_type and description are the
// agent's meta file's, raw, and empty on session rows.
func FormatSummaryDetailCSV(results []models.SessionResult, expandAgents bool, sortBy string) (string, error) {
	var sb strings.Builder
	w := csv.NewWriter(&sb)

	header := []string{
		"row_type",
		"session_id",
		"project_path",
		"session_file",
		"agent_id",
		"modified",
		"model",
		"message_count",
		"agent_count",
		"input_cost",
		"output_cost",
		"cache_write_5m_cost",
		"cache_write_1h_cost",
		"cache_read_cost",
		"total_cost",
		"cache_savings",
		"cumulative_cost",
		"workflow_id",
		"skipped_agents",
		"skipped_lines",
		"estimated_cost_messages",
		"parent_cost",
		"agents_cost",
		"unpriced_models",
		"agent_type",
		"description",
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing summary detail CSV header: %w", err)
	}

	var cumulative float64
	for _, r := range sortedSuccessfulResults(results, sortBy) {
		a := r.Analysis
		cumulative += a.TotalCost.TotalCost

		sessionRow := []string{
			"session",
			csvCell(r.Entry.SessionID),
			csvCell(a.ProjectPath),
			csvCell(a.SessionFile),
			"",
			models.MachineTime(r.Entry.Modified),
			csvCell(primarySessionModelID(a)),
			fmt.Sprintf("%d", a.MessageCount),
			fmt.Sprintf("%d", a.AgentCount),
			fmt.Sprintf("%.6f", a.TotalCost.InputCost),
			fmt.Sprintf("%.6f", a.TotalCost.OutputCost),
			fmt.Sprintf("%.6f", a.TotalCost.CacheWrite5mCost),
			fmt.Sprintf("%.6f", a.TotalCost.CacheWrite1hCost),
			fmt.Sprintf("%.6f", a.TotalCost.CacheReadCost),
			fmt.Sprintf("%.6f", a.TotalCost.TotalCost),
			fmt.Sprintf("%.6f", a.TotalCost.CacheSavings),
			fmt.Sprintf("%.6f", cumulative),
			"",
			fmt.Sprintf("%d", a.SkippedAgents),
			fmt.Sprintf("%d", a.SkippedLines),
			fmt.Sprintf("%d", a.EstimatedCostMessages),
			fmt.Sprintf("%.6f", a.ParentCost.TotalCost),
			fmt.Sprintf("%.6f", a.AgentsCost.TotalCost),
			unpricedModelsCell(a.UnpricedModels),
			"", // agent_type and description name an agent
			"",
		}
		if err := w.Write(sessionRow); err != nil {
			return "", fmt.Errorf("writing summary detail session row: %w", err)
		}

		if !expandAgents {
			continue
		}
		for _, agent := range a.Agents {
			agentRow := []string{
				"agent",
				csvCell(r.Entry.SessionID),
				"", // project_path is a session-row concept; join via session_id
				"", // session_file is a session-row concept
				csvCell(agent.AgentID),
				"",
				csvCell(primaryModelID(agent.CostByModel)),
				fmt.Sprintf("%d", agent.MessageCount),
				"",
				fmt.Sprintf("%.6f", agent.TotalCost.InputCost),
				fmt.Sprintf("%.6f", agent.TotalCost.OutputCost),
				fmt.Sprintf("%.6f", agent.TotalCost.CacheWrite5mCost),
				fmt.Sprintf("%.6f", agent.TotalCost.CacheWrite1hCost),
				fmt.Sprintf("%.6f", agent.TotalCost.CacheReadCost),
				fmt.Sprintf("%.6f", agent.TotalCost.TotalCost),
				fmt.Sprintf("%.6f", agent.TotalCost.CacheSavings),
				"",
				csvCell(agent.WorkflowID),
				"", // skipped_agents is a session-row concept
				fmt.Sprintf("%d", agent.SkippedLines),
				fmt.Sprintf("%d", agent.EstimatedCostMessages),
				"", // parent_cost and agents_cost split a session row's total
				"",
				unpricedModelsCell(agent.UnpricedModels),
				csvCell(agent.AgentType),
				csvCell(agent.Description),
			}
			if err := w.Write(agentRow); err != nil {
				return "", fmt.Errorf("writing summary detail agent row: %w", err)
			}
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("flushing summary detail CSV: %w", err)
	}
	return sb.String(), nil
}
