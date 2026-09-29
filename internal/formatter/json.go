package formatter

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// FormatSessionJSON formats a session analysis as JSON
func FormatSessionJSON(analysis *models.SessionAnalysis, pretty bool) (string, error) {
	var data []byte
	var err error

	if pretty {
		data, err = json.MarshalIndent(analysis, "", "  ")
	} else {
		data, err = json.Marshal(analysis)
	}

	if err != nil {
		return "", err
	}

	return string(data), nil
}

// sessionListRecord is one session in `list -f json`: the same fields as
// the csv, in snake_case like every other json output. SessionEntry's own
// tags are camelCase because they also read sessions-index.json.
//
// project_path is the transcript's directory, derived the way the analyzer
// derives show's, so the two join. original_path is the working directory
// the project stands for, "" when unknown.
//
// The analysis fields are those of the session's summary -d record: model,
// start_time, duration_seconds and total_cost leave out messages repeated
// from an earlier session, and title is the transcript's latest, as on show.
// They are absent on a session that failed to parse, which skipped_sessions
// marks, and start_time and model on one with nothing of its own. total_cost is the same breakdown
// object show and summary carry, so .total_cost.total_cost reads alike.
type sessionListRecord struct {
	SessionID         string                `json:"session_id"`
	FullPath          string                `json:"full_path"`
	ProjectPath       string                `json:"project_path"`
	OriginalPath      string                `json:"original_path"`
	Title             string                `json:"title,omitempty"`
	Model             string                `json:"model,omitempty"`
	StartTime         time.Time             `json:"start_time,omitzero"`
	Modified          time.Time             `json:"modified"`
	DurationSeconds   *float64              `json:"duration_seconds,omitempty"`
	MessageCount      int                   `json:"message_count"`
	TotalCost         *models.CostBreakdown `json:"total_cost,omitempty"`
	AgentPaths        []string              `json:"agent_paths,omitempty"`
	AgentCount        int                   `json:"agent_count"`
	AgentMessageCount int                   `json:"agent_message_count"`
	SkippedSessions   int                   `json:"skipped_sessions,omitempty"`
	SkippedAgents     int                   `json:"skipped_agents,omitempty"`
	SkippedLines      int                   `json:"skipped_lines,omitempty"`
}

// FormatSessionListJSON formats `list` output. Counts and skip counters come
// from each entry's discovery scan, so they match show's; costs and the rest
// come from the analysis, which prices cross-session duplicates once, so a
// session's cost matches its summary -d row.
func FormatSessionListJSON(results []models.SessionResult, originalPath string, pretty bool) (string, error) {
	records := make([]sessionListRecord, len(results))
	for i, r := range results {
		e := r.Entry
		records[i] = sessionListRecord{
			SessionID:         e.SessionID,
			FullPath:          e.FullPath,
			ProjectPath:       filepath.Dir(e.FullPath),
			OriginalPath:      originalPath,
			Modified:          e.Modified,
			MessageCount:      e.MessageCount,
			AgentPaths:        e.AgentPaths,
			AgentCount:        e.AgentCount,
			AgentMessageCount: e.AgentMessageCount,
			SkippedSessions:   e.SkippedSessions,
			SkippedAgents:     e.SkippedAgents,
			SkippedLines:      e.SkippedLines,
		}
		if a := r.Analysis; a != nil {
			seconds := a.Duration.Seconds()
			cost := a.TotalCost
			records[i].Title = a.Title
			records[i].Model = primarySessionModelID(a)
			records[i].StartTime = a.StartTime
			records[i].DurationSeconds = &seconds
			records[i].TotalCost = &cost
		}
	}

	var data []byte
	var err error
	if pretty {
		data, err = json.MarshalIndent(records, "", "  ")
	} else {
		data, err = json.Marshal(records)
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}
