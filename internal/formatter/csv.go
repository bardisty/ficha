package formatter

import (
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/bah/ccusage/internal/models"
)

// FormatSessionCSV formats a session analysis as CSV
func FormatSessionCSV(analysis *models.SessionAnalysis, includeMessages bool) string {
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
	}
	w.Write(header)

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
	}
	w.Write(row)

	w.Flush()

	// Optionally include individual messages
	if includeMessages && len(analysis.Messages) > 0 {
		sb.WriteString("\n")
		sb.WriteString(formatMessagesCSV(analysis.Messages))
	}

	return sb.String()
}

// formatMessagesCSV formats individual messages as CSV
func formatMessagesCSV(messages []models.MessageAnalysis) string {
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
	w.Write(header)

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
		w.Write(row)
	}

	w.Flush()
	return sb.String()
}

// FormatSessionListCSV formats a list of sessions as CSV
func FormatSessionListCSV(entries []models.SessionEntry) string {
	var sb strings.Builder
	w := csv.NewWriter(&sb)

	// Write header
	header := []string{
		"session_id",
		"full_path",
		"message_count",
		"created",
		"modified",
	}
	w.Write(header)

	// Write session rows
	for _, entry := range entries {
		row := []string{
			entry.SessionID,
			entry.FullPath,
			fmt.Sprintf("%d", entry.MessageCount),
			entry.Created.Format("2006-01-02T15:04:05Z07:00"),
			entry.Modified.Format("2006-01-02T15:04:05Z07:00"),
		}
		w.Write(row)
	}

	w.Flush()
	return sb.String()
}
