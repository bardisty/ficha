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

// csvTimeFormat matches the timestamp layout used by the other CSV formatters.
const csvTimeFormat = "2006-01-02T15:04:05Z07:00"

// sortedSuccessfulResults returns the results that parsed, ordered by modified
// time to match the SESSION BREAKDOWN table. Failed sessions (nil Analysis) are
// dropped: machine formats export only sessions that could be analyzed, and the
// aggregate's skipped_sessions plus the stderr warning account for the rest.
func sortedSuccessfulResults(results []models.SessionResult) []models.SessionResult {
	sorted := make([]models.SessionResult, 0, len(results))
	for _, r := range results {
		if r.Analysis != nil {
			sorted = append(sorted, r)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Entry.Modified.Before(sorted[j].Entry.Modified)
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
// full agent sub-sessions are included.
func FormatSummaryDetailJSON(summary *models.SessionAnalysis, results []models.SessionResult, expandAgents, pretty bool) (string, error) {
	sorted := sortedSuccessfulResults(results)
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
// expandAgents is set. Session rows carry the session total (message_count,
// costs and skipped_lines include agents); agent rows break out each agent — do
// not sum across row types. cumulative_cost is the running session total in
// modified order and is empty on agent rows. agent_id holds the agent's real ID
// — the same key `show --messages` and the json agents[] array use, so the
// exports join. skipped_agents says how many agents a session row's agent_count
// does not include, and is empty on agent rows.
func FormatSummaryDetailCSV(results []models.SessionResult, expandAgents bool) (string, error) {
	var sb strings.Builder
	w := csv.NewWriter(&sb)

	header := []string{
		"row_type",
		"session_id",
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
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing summary detail CSV header: %w", err)
	}

	var cumulative float64
	for _, r := range sortedSuccessfulResults(results) {
		a := r.Analysis
		cumulative += a.TotalCost.TotalCost

		sessionRow := []string{
			"session",
			csvCell(r.Entry.SessionID),
			"",
			r.Entry.Modified.Format(csvTimeFormat),
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
