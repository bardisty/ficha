package analyzer

import (
	"testing"
	"time"

	"github.com/bardisty/ccusage/internal/models"
)

func TestBuildSessionAnalysisEmpty(t *testing.T) {
	analysis := buildSessionAnalysis("test-session", "/path/to/session.jsonl", nil, false)

	if analysis.SessionID != "test-session" {
		t.Errorf("SessionID: got %s, want test-session", analysis.SessionID)
	}
	if analysis.MessageCount != 0 {
		t.Errorf("MessageCount: got %d, want 0", analysis.MessageCount)
	}
	if analysis.TotalCost.TotalCost != 0 {
		t.Errorf("TotalCost: got %f, want 0", analysis.TotalCost.TotalCost)
	}
}

func TestBuildSessionAnalysisWithMessages(t *testing.T) {
	t1 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC)

	messages := []models.MessageAnalysis{
		{
			Timestamp: t1,
			Model:     "claude-opus-4-5",
			Usage:     models.TokenUsage{InputTokens: 1000, OutputTokens: 500},
			Cost:      models.CostBreakdown{TotalCost: 1.50},
		},
		{
			Timestamp: t2,
			Model:     "claude-opus-4-5",
			Usage:     models.TokenUsage{InputTokens: 2000, OutputTokens: 1000},
			Cost:      models.CostBreakdown{TotalCost: 3.00},
		},
		{
			Timestamp: t3,
			Model:     "claude-sonnet-4-5",
			Usage:     models.TokenUsage{InputTokens: 500, OutputTokens: 200},
			Cost:      models.CostBreakdown{TotalCost: 0.50},
		},
	}

	analysis := buildSessionAnalysis("test-session", "/path", messages, false)

	if analysis.MessageCount != 3 {
		t.Errorf("MessageCount: got %d, want 3", analysis.MessageCount)
	}
	if analysis.TotalUsage.InputTokens != 3500 {
		t.Errorf("TotalUsage.InputTokens: got %d, want 3500", analysis.TotalUsage.InputTokens)
	}
	if analysis.TotalUsage.OutputTokens != 1700 {
		t.Errorf("TotalUsage.OutputTokens: got %d, want 1700", analysis.TotalUsage.OutputTokens)
	}
	if !almostEqual(analysis.TotalCost.TotalCost, 5.0, 0.001) {
		t.Errorf("TotalCost: got %f, want 5.0", analysis.TotalCost.TotalCost)
	}

	// Check time range
	if !analysis.StartTime.Equal(t1) {
		t.Errorf("StartTime: got %v, want %v", analysis.StartTime, t1)
	}
	if !analysis.EndTime.Equal(t3) {
		t.Errorf("EndTime: got %v, want %v", analysis.EndTime, t3)
	}
	expectedDuration := time.Hour
	if analysis.Duration.Duration() != expectedDuration {
		t.Errorf("Duration: got %v, want %v", analysis.Duration.Duration(), expectedDuration)
	}

	// Check cost by model
	if len(analysis.CostByModel) != 2 {
		t.Errorf("CostByModel count: got %d, want 2", len(analysis.CostByModel))
	}
	if opusCost, ok := analysis.CostByModel["claude-opus-4-5"]; !ok {
		t.Error("CostByModel missing claude-opus-4-5")
	} else if !almostEqual(opusCost.TotalCost, 4.5, 0.001) {
		t.Errorf("CostByModel[claude-opus-4-5]: got %f, want 4.5", opusCost.TotalCost)
	}
}

func TestBuildSessionAnalysisIncludeMessages(t *testing.T) {
	messages := []models.MessageAnalysis{
		{Model: "claude-opus-4-5"},
		{Model: "claude-sonnet-4-5"},
	}

	// Without include messages
	analysis1 := buildSessionAnalysis("test", "/path", messages, false)
	if len(analysis1.Messages) != 0 {
		t.Errorf("Messages should be empty when includeMessages=false, got %d", len(analysis1.Messages))
	}

	// With include messages
	analysis2 := buildSessionAnalysis("test", "/path", messages, true)
	if len(analysis2.Messages) != 2 {
		t.Errorf("Messages should be included when includeMessages=true, got %d", len(analysis2.Messages))
	}
}

func TestAnalyzeSessionFromMessages(t *testing.T) {
	jsonlMessages := []models.JSONLMessage{
		{
			Type:      "assistant",
			Timestamp: time.Now(),
			Message: &models.AssistantMessage{
				Model: "claude-sonnet-4-5",
				Usage: models.TokenUsage{InputTokens: 1000, OutputTokens: 500},
			},
		},
	}

	analysis := AnalyzeSessionFromMessages("test-session", "/path", jsonlMessages, false)

	if analysis.SessionID != "test-session" {
		t.Errorf("SessionID: got %s, want test-session", analysis.SessionID)
	}
	if analysis.MessageCount != 1 {
		t.Errorf("MessageCount: got %d, want 1", analysis.MessageCount)
	}
	// Cost should be calculated
	if analysis.TotalCost.TotalCost == 0 {
		t.Error("TotalCost should not be zero")
	}
}
