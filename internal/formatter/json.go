package formatter

import (
	"encoding/json"

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
