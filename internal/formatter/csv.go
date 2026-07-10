package formatter

import (
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/bardisty/ficha/internal/models"
)

// FormatSessionCSV formats a session analysis as CSV. includeMessages switches
// granularity rather than stacking: false emits a single session-summary row,
// true emits one row per message. Either way the result is a single valid CSV
// table (one header, uniform column count) so single-table parsers never choke.
//
// Both `show` (one session) and `summary` (the aggregate) render through here,
// so the row carries skipped_sessions either way — always 0 for `show`, which
// analyzed exactly one session and would have errored had it failed.
func FormatSessionCSV(analysis *models.SessionAnalysis, includeMessages bool) (string, error) {
	if includeMessages {
		return formatMessagesCSV(analysis.Messages)
	}

	var sb strings.Builder
	w := csv.NewWriter(&sb)

	// Write header
	header := []string{
		"session_id",
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
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing CSV header: %w", err)
	}

	// Write session row
	row := []string{
		csvCell(analysis.SessionID),
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

// formatMessagesCSV formats individual messages as CSV. Parent rows come first
// (agent_id empty), then one block per agent sub-session; total_cost sums across
// every row to the session total. Model IDs are raw, not canonicalized.
func formatMessagesCSV(messages []models.MessageAnalysis) (string, error) {
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
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing messages CSV header: %w", err)
	}

	// Write message rows
	for _, msg := range messages {
		row := []string{
			csvCell(msg.AgentID),
			msg.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
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

// FormatSessionListCSV formats a list of sessions as CSV
func FormatSessionListCSV(entries []models.SessionEntry) (string, error) {
	var sb strings.Builder
	w := csv.NewWriter(&sb)

	// Write header
	header := []string{
		"session_id",
		"full_path",
		"message_count",
		"created",
		"modified",
		"agent_count",
		"agent_message_count",
		"skipped_sessions",
		"skipped_agents",
		"skipped_lines",
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing session list CSV header: %w", err)
	}

	// Write session rows
	for _, entry := range entries {
		row := []string{
			csvCell(entry.SessionID),
			csvCell(entry.FullPath),
			fmt.Sprintf("%d", entry.MessageCount),
			entry.Created.Format("2006-01-02T15:04:05Z07:00"),
			entry.Modified.Format("2006-01-02T15:04:05Z07:00"),
			fmt.Sprintf("%d", entry.AgentCount),
			fmt.Sprintf("%d", entry.AgentMessageCount),
			fmt.Sprintf("%d", entry.SkippedSessions),
			fmt.Sprintf("%d", entry.SkippedAgents),
			fmt.Sprintf("%d", entry.SkippedLines),
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
