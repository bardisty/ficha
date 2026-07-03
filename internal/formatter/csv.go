package formatter

import (
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/bardisty/ccusage/internal/models"
)

// FormatSessionCSV formats a session analysis as CSV. includeMessages switches
// granularity rather than stacking: false emits a single session-summary row,
// true emits one row per message. Either way the result is a single valid CSV
// table (one header, uniform column count) so single-table parsers never choke.
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
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing CSV header: %w", err)
	}

	// Write session row
	row := []string{
		analysis.SessionID,
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

// formatMessagesCSV formats individual messages as CSV
func formatMessagesCSV(messages []models.MessageAnalysis) (string, error) {
	var sb strings.Builder
	w := csv.NewWriter(&sb)

	// Write header
	header := []string{
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
			msg.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			msg.Model,
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
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing session list CSV header: %w", err)
	}

	// Write session rows
	for _, entry := range entries {
		row := []string{
			entry.SessionID,
			entry.FullPath,
			fmt.Sprintf("%d", entry.MessageCount),
			entry.Created.Format("2006-01-02T15:04:05Z07:00"),
			entry.Modified.Format("2006-01-02T15:04:05Z07:00"),
			fmt.Sprintf("%d", entry.AgentCount),
			fmt.Sprintf("%d", entry.AgentMessageCount),
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
