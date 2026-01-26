package formatter

import (
	"encoding/json"

	"github.com/bah/ccusage/internal/models"
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

// FormatSessionListJSON formats a list of session entries as JSON
func FormatSessionListJSON(entries []models.SessionEntry, pretty bool) (string, error) {
	var data []byte
	var err error

	if pretty {
		data, err = json.MarshalIndent(entries, "", "  ")
	} else {
		data, err = json.Marshal(entries)
	}

	if err != nil {
		return "", err
	}

	return string(data), nil
}

// SessionListWithCosts represents session entries with their costs
type SessionListWithCosts struct {
	Sessions []SessionWithCost `json:"sessions"`
	Total    CostSummary       `json:"total"`
}

// SessionWithCost represents a session entry with its calculated cost
type SessionWithCost struct {
	SessionID    string  `json:"session_id"`
	MessageCount int     `json:"message_count"`
	Created      string  `json:"created"`
	Modified     string  `json:"modified"`
	TotalCost    float64 `json:"total_cost"`
}

// CostSummary represents a summary of costs
type CostSummary struct {
	TotalCost    float64 `json:"total_cost"`
	CacheSavings float64 `json:"cache_savings"`
	SessionCount int     `json:"session_count"`
	MessageCount int     `json:"message_count"`
}
