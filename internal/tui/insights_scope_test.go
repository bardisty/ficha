package tui

import (
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

// TestWatchInsights_TrendGate pins the trend gate on the watch surface:
// renderInsights gates the trend row on HasTrend, so no fabricated row appears
// one message below models.MinMessagesForTrend.
func TestWatchInsights_TrendGate(t *testing.T) {
	below := &models.SessionAnalysis{
		Insights: &models.MessageInsights{MessageCount: models.MinMessagesForTrend - 1},
	}
	m := Model{analysis: below, noColor: true}
	if out := m.renderInsightsContent(); strings.Contains(out, "Trend") {
		t.Errorf("watch trend row rendered below threshold:\n%s", out)
	}

	at := &models.SessionAnalysis{
		Insights: &models.MessageInsights{
			MessageCount: models.MinMessagesForTrend,
			CostTrend:    models.TrendIncreasing,
			EarlyAvgCost: 0.10,
			LateAvgCost:  0.25,
		},
	}
	m = Model{analysis: at, noColor: true}
	if out := m.renderInsightsContent(); !strings.Contains(out, "Trend") {
		t.Errorf("watch trend row missing at threshold:\n%s", out)
	}
}

// TestWatchInsights_ScopeLabel pins the scope label on the watch surface.
func TestWatchInsights_ScopeLabel(t *testing.T) {
	insights := &models.MessageInsights{
		MessageCount: models.MinMessagesForTrend,
		FirstMessage: &models.MessageSnapshot{Index: 1},
	}
	withAgents := Model{analysis: &models.SessionAnalysis{Insights: insights, HasAgents: true}, noColor: true}
	if out := withAgents.renderInsightsContent(); !strings.Contains(out, "scope: parent transcript") {
		t.Errorf("watch: expected scope label when agents present:\n%s", out)
	}
	noAgents := Model{analysis: &models.SessionAnalysis{Insights: insights, HasAgents: false}, noColor: true}
	if out := noAgents.renderInsightsContent(); strings.Contains(out, "scope:") {
		t.Errorf("watch: did not expect scope label when no agents:\n%s", out)
	}
}

// TestBreakdownInsights_TrendGate pins the trend gate on the breakdown surface.
func TestBreakdownInsights_TrendGate(t *testing.T) {
	below := BreakdownModel{
		noColor:  true,
		messages: []models.BreakdownMessage{{Index: 1}},
		insights: &models.MessageInsights{
			MessageCount: models.MinMessagesForTrend - 1,
			HighestCost:  &models.MessageSnapshot{Index: 1, Cost: 1.0},
			AverageCost:  0.5,
		},
	}
	if out := below.renderCompactInsights(); strings.Contains(out, "Trend") {
		t.Errorf("breakdown trend rendered below threshold:\n%s", out)
	}

	at := below
	at.insights = &models.MessageInsights{
		MessageCount: models.MinMessagesForTrend,
		CostTrend:    models.TrendIncreasing,
		HighestCost:  &models.MessageSnapshot{Index: 1, Cost: 1.0},
		AverageCost:  0.5,
	}
	if out := at.renderCompactInsights(); !strings.Contains(out, "Trend") {
		t.Errorf("breakdown trend missing at threshold:\n%s", out)
	}
}

// TestBreakdownInsights_ScopeLabel pins the scope label on the breakdown
// surface: its insights cover the merged parent+agent messages, so the scope is
// labeled when agents are present and left off when they are not.
func TestBreakdownInsights_ScopeLabel(t *testing.T) {
	base := BreakdownModel{
		noColor:  true,
		messages: []models.BreakdownMessage{{Index: 1}},
		insights: &models.MessageInsights{
			MessageCount: models.MinMessagesForTrend,
			HighestCost:  &models.MessageSnapshot{Index: 1, Cost: 1.0},
			AverageCost:  0.5,
		},
	}
	withAgents := base
	withAgents.hasAgents = true
	if out := withAgents.renderCompactInsights(); !strings.Contains(out, "scope: parent + agents") {
		t.Errorf("breakdown: expected scope label when agents present:\n%s", out)
	}
	noAgents := base
	noAgents.hasAgents = false
	if out := noAgents.renderCompactInsights(); strings.Contains(out, "scope:") {
		t.Errorf("breakdown: did not expect scope label when no agents:\n%s", out)
	}
}
