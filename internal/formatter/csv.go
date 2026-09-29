package formatter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bardisty/ficha/internal/models"
)

// FormatSessionCSV formats a session analysis as CSV. includeMessages switches
// granularity rather than stacking: false emits a single session-summary row,
// true emits one row per message. Either way the result is a single valid CSV
// table (one header, uniform column count) so single-table parsers never choke.
//
// Both `show` (one session) and `summary` (the aggregate) render through here,
// so the row carries skipped_sessions and session_count either way — for
// `show` they are always 0 and 1: it analyzed exactly one session and would
// have errored had it failed.
func FormatSessionCSV(analysis *models.SessionAnalysis, includeMessages bool) (string, error) {
	if includeMessages {
		return formatMessagesCSV(analysis)
	}

	var sb strings.Builder
	w := csv.NewWriter(&sb)

	// Write header
	header := []string{
		"session_id",
		"project_path",
		"session_file",
		"input_cost",
		"output_cost",
		"cache_write_5m_cost",
		"cache_write_1h_cost",
		"cache_read_cost",
		"total_cost",
		"cache_savings",
		"message_count",
		"duration_seconds",
		"agent_count",
		"agent_message_count",
		"parent_cost",
		"agents_cost",
		"workflow_count",
		"skipped_sessions",
		"skipped_agents",
		"skipped_lines",
		"estimated_cost_messages",
		"session_count",
		"unpriced_models",
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing CSV header: %w", err)
	}

	// Write session row
	row := []string{
		csvCell(analysis.SessionID),
		csvCell(analysis.ProjectPath),
		csvCell(analysis.SessionFile),
		fmt.Sprintf("%.6f", analysis.TotalCost.InputCost),
		fmt.Sprintf("%.6f", analysis.TotalCost.OutputCost),
		fmt.Sprintf("%.6f", analysis.TotalCost.CacheWrite5mCost),
		fmt.Sprintf("%.6f", analysis.TotalCost.CacheWrite1hCost),
		fmt.Sprintf("%.6f", analysis.TotalCost.CacheReadCost),
		fmt.Sprintf("%.6f", analysis.TotalCost.TotalCost),
		fmt.Sprintf("%.6f", analysis.TotalCost.CacheSavings),
		fmt.Sprintf("%d", analysis.MessageCount),
		fmt.Sprintf("%.0f", analysis.Duration.Duration().Seconds()),
		fmt.Sprintf("%d", analysis.AgentCount),
		fmt.Sprintf("%d", analysis.AgentMessageCount),
		fmt.Sprintf("%.6f", analysis.ParentCost.TotalCost),
		fmt.Sprintf("%.6f", analysis.AgentsCost.TotalCost),
		fmt.Sprintf("%d", analysis.WorkflowCount),
		fmt.Sprintf("%d", analysis.SkippedSessions),
		fmt.Sprintf("%d", analysis.SkippedAgents),
		fmt.Sprintf("%d", analysis.SkippedLines),
		fmt.Sprintf("%d", analysis.EstimatedCostMessages),
		fmt.Sprintf("%d", analysis.SessionCount),
		unpricedModelsCell(analysis.UnpricedModels),
	}
	if err := w.Write(row); err != nil {
		return "", fmt.Errorf("writing CSV row: %w", err)
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("flushing CSV: %w", err)
	}

	return sb.String(), nil
}

// unpricedModelsCell writes model IDs as a json array, "[]" when there are
// none. An unknown ID is raw transcript text, so it can be empty or hold any
// separator; a json array keeps every ID intact, and a cell opening with "["
// can't be read as a formula.
func unpricedModelsCell(ids []string) string {
	if ids == nil {
		ids = []string{}
	}
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(ids) // a []string always encodes
	return strings.TrimSuffix(sb.String(), "\n")
}

// formatMessagesCSV formats individual messages as CSV. Parent rows come first
// (agent_id empty), then one block per agent sub-session; total_cost sums across
// every row to the session total. Model IDs are raw, not canonicalized.
//
// The trailing three columns are session-level accounting repeated verbatim on
// every row (standard denormalized CSV): without them a consumer summing rows
// could not tell the rows are incomplete (skipped agents/lines) or estimated —
// the json --messages surface carries the same counters on the enclosing
// session object. A session with zero messages emits a header-only table, so
// its counters appear nowhere but stderr (the pre-existing empty-table shape).
func formatMessagesCSV(analysis *models.SessionAnalysis) (string, error) {
	var sb strings.Builder
	w := csv.NewWriter(&sb)

	// Write header
	header := []string{
		"agent_id",
		"timestamp",
		"model",
		"input_tokens",
		"output_tokens",
		"cache_write_tokens",
		"cache_read_tokens",
		"input_cost",
		"output_cost",
		"cache_write_5m_cost",
		"cache_write_1h_cost",
		"cache_read_cost",
		"total_cost",
		"skipped_agents",
		"skipped_lines",
		"estimated_cost_messages",
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing messages CSV header: %w", err)
	}

	skippedAgents := fmt.Sprintf("%d", analysis.SkippedAgents)
	skippedLines := fmt.Sprintf("%d", analysis.SkippedLines)
	estimatedCostMessages := fmt.Sprintf("%d", analysis.EstimatedCostMessages)

	// Write message rows
	for _, msg := range analysis.Messages {
		row := []string{
			csvCell(msg.AgentID),
			models.MachineTime(msg.Timestamp),
			csvCell(msg.Model),
			fmt.Sprintf("%d", msg.Usage.InputTokens),
			fmt.Sprintf("%d", msg.Usage.OutputTokens),
			fmt.Sprintf("%d", msg.Usage.CacheCreationInputTokens),
			fmt.Sprintf("%d", msg.Usage.CacheReadInputTokens),
			fmt.Sprintf("%.6f", msg.Cost.InputCost),
			fmt.Sprintf("%.6f", msg.Cost.OutputCost),
			fmt.Sprintf("%.6f", msg.Cost.CacheWrite5mCost),
			fmt.Sprintf("%.6f", msg.Cost.CacheWrite1hCost),
			fmt.Sprintf("%.6f", msg.Cost.CacheReadCost),
			fmt.Sprintf("%.6f", msg.Cost.TotalCost),
			skippedAgents,
			skippedLines,
			estimatedCostMessages,
		}
		if err := w.Write(row); err != nil {
			return "", fmt.Errorf("writing message row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("flushing messages CSV: %w", err)
	}
	return sb.String(), nil
}

// FormatSessionListCSV formats `list` output as CSV, with the fields of
// FormatSessionListJSON. A session that failed to parse leaves the analysis
// columns (start_time through title) empty.
func FormatSessionListCSV(results []models.SessionResult, originalPath string) (string, error) {
	var sb strings.Builder
	w := csv.NewWriter(&sb)

	// Write header
	header := []string{
		"session_id",
		"full_path",
		"message_count",
		"modified",
		"agent_count",
		"agent_message_count",
		"skipped_sessions",
		"skipped_agents",
		"skipped_lines",
		"project_path",
		"original_path",
		"start_time",
		"duration_seconds",
		"model",
		"total_cost",
		"title",
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing session list CSV header: %w", err)
	}

	// Write session rows
	for _, r := range results {
		entry := r.Entry
		row := []string{
			csvCell(entry.SessionID),
			csvCell(entry.FullPath),
			fmt.Sprintf("%d", entry.MessageCount),
			models.MachineTime(entry.Modified),
			fmt.Sprintf("%d", entry.AgentCount),
			fmt.Sprintf("%d", entry.AgentMessageCount),
			fmt.Sprintf("%d", entry.SkippedSessions),
			fmt.Sprintf("%d", entry.SkippedAgents),
			fmt.Sprintf("%d", entry.SkippedLines),
			csvCell(filepath.Dir(entry.FullPath)),
			csvCell(originalPath),
		}
		if a := r.Analysis; a != nil {
			start := ""
			if !a.StartTime.IsZero() {
				start = models.MachineTime(a.StartTime)
			}
			row = append(row,
				start,
				fmt.Sprintf("%.0f", a.Duration.Seconds()),
				csvCell(primarySessionModelID(a)),
				fmt.Sprintf("%.6f", a.TotalCost.TotalCost),
				csvCell(a.Title),
			)
		} else {
			row = append(row, "", "", "", "", "")
		}
		if err := w.Write(row); err != nil {
			return "", fmt.Errorf("writing session entry row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("flushing session list CSV: %w", err)
	}
	return sb.String(), nil
}
