package formatter

import (
	"encoding/json"
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
type sessionListRecord struct {
	SessionID         string    `json:"session_id"`
	FullPath          string    `json:"full_path"`
	ProjectPath       string    `json:"project_path,omitempty"`
	MessageCount      int       `json:"message_count"`
	Created           time.Time `json:"created"`
	Modified          time.Time `json:"modified"`
	AgentPaths        []string  `json:"agent_paths,omitempty"`
	AgentCount        int       `json:"agent_count"`
	AgentMessageCount int       `json:"agent_message_count"`
	SkippedSessions   int       `json:"skipped_sessions,omitempty"`
	SkippedAgents     int       `json:"skipped_agents,omitempty"`
	SkippedLines      int       `json:"skipped_lines,omitempty"`
}

// FormatSessionListJSON formats a list of session entries as JSON
func FormatSessionListJSON(entries []models.SessionEntry, pretty bool) (string, error) {
	records := make([]sessionListRecord, len(entries))
	for i, e := range entries {
		records[i] = sessionListRecord{
			SessionID:         e.SessionID,
			FullPath:          e.FullPath,
			ProjectPath:       e.ProjectPath,
			MessageCount:      e.MessageCount,
			Created:           e.Created,
			Modified:          e.Modified,
			AgentPaths:        e.AgentPaths,
			AgentCount:        e.AgentCount,
			AgentMessageCount: e.AgentMessageCount,
			SkippedSessions:   e.SkippedSessions,
			SkippedAgents:     e.SkippedAgents,
			SkippedLines:      e.SkippedLines,
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
