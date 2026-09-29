package analyzer

import (
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

func TestCalculateInsights_NoMessages(t *testing.T) {
	result := CalculateInsights([]models.MessageAnalysis{})
	if result != nil {
		t.Errorf("Expected nil for empty messages, got %+v", result)
	}
}

func TestCalculateInsights_SingleMessage(t *testing.T) {
	now := time.Now()
	messages := []models.MessageAnalysis{
		{
			Timestamp: now,
			Model:     "claude-sonnet-4-5",
			Cost: models.CostBreakdown{
				InputCost:  0.10,
				OutputCost: 0.05,
				TotalCost:  0.15,
			},
		},
	}

	result := CalculateInsights(messages)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// First and last should be the same
	if result.FirstMessage == nil {
		t.Error("FirstMessage should not be nil")
	}
	if result.LastMessage == nil {
		t.Error("LastMessage should not be nil")
	}
	if result.FirstMessage.Cost != result.LastMessage.Cost {
		t.Errorf("First and last should be equal for single message")
	}

	// No trend for single message
	if result.MessageCount != 1 {
		t.Errorf("Expected MessageCount=1, got %d", result.MessageCount)
	}

	// Average should equal the single message cost
	if !almostEqual(result.AverageCost, 0.15, 0.001) {
		t.Errorf("Expected AverageCost=0.15, got %f", result.AverageCost)
	}

	// No highest (not notable)
	if result.HighestCost != nil {
		t.Error("Expected no HighestCost for uniform single message")
	}
}

func TestCalculateInsights_TwoMessages(t *testing.T) {
	now := time.Now()
	messages := []models.MessageAnalysis{
		{
			Timestamp: now,
			Cost: models.CostBreakdown{
				TotalCost: 0.10,
			},
		},
		{
			Timestamp: now.Add(time.Minute),
			Cost: models.CostBreakdown{
				TotalCost: 0.05,
			},
		},
	}

	result := CalculateInsights(messages)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Check first and last
	if !almostEqual(result.FirstMessage.Cost, 0.10, 0.001) {
		t.Errorf("Expected FirstMessage.Cost=0.10, got %f", result.FirstMessage.Cost)
	}
	if !almostEqual(result.LastMessage.Cost, 0.05, 0.001) {
		t.Errorf("Expected LastMessage.Cost=0.05, got %f", result.LastMessage.Cost)
	}

	// No trend below MinMessagesForTrend
	if result.RecentAvgCost != 0 || result.TrendWindow != 0 {
		t.Error("Should not have trend data below the trend threshold")
	}
}

func TestCalculateInsights_SixMessages_DecreasingTrend(t *testing.T) {
	now := time.Now()
	// Decreasing costs: 0.30, 0.25, 0.20, 0.10, 0.05, 0.03
	messages := []models.MessageAnalysis{
		{Timestamp: now, Cost: models.CostBreakdown{TotalCost: 0.30}},
		{Timestamp: now.Add(time.Minute), Cost: models.CostBreakdown{TotalCost: 0.25}},
		{Timestamp: now.Add(2 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.20}},
		{Timestamp: now.Add(3 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.10}},
		{Timestamp: now.Add(4 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.05}},
		{Timestamp: now.Add(5 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.03}},
	}

	result := CalculateInsights(messages)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Six messages: the window is the later half, 3 messages
	if result.TrendWindow != 3 {
		t.Errorf("Expected TrendWindow=3, got %d", result.TrendWindow)
	}
	// Recent avg = (0.10 + 0.05 + 0.03) / 3 = 0.06, against a session avg of 0.155
	expectedRecentAvg := (0.10 + 0.05 + 0.03) / 3
	if !almostEqual(result.RecentAvgCost, expectedRecentAvg, 0.001) {
		t.Errorf("Expected RecentAvgCost=%f, got %f", expectedRecentAvg, result.RecentAvgCost)
	}

	// Should be decreasing (recent is well below the session average)
	if result.CostTrend != models.TrendDecreasing {
		t.Errorf("Expected TrendDecreasing, got %v", result.CostTrend)
	}
}

func TestCalculateInsights_SixMessages_IncreasingTrend(t *testing.T) {
	now := time.Now()
	// Increasing costs: 0.05, 0.10, 0.15, 0.25, 0.30, 0.35
	messages := []models.MessageAnalysis{
		{Timestamp: now, Cost: models.CostBreakdown{TotalCost: 0.05}},
		{Timestamp: now.Add(time.Minute), Cost: models.CostBreakdown{TotalCost: 0.10}},
		{Timestamp: now.Add(2 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.15}},
		{Timestamp: now.Add(3 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.25}},
		{Timestamp: now.Add(4 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.30}},
		{Timestamp: now.Add(5 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.35}},
	}

	result := CalculateInsights(messages)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Should be increasing
	if result.CostTrend != models.TrendIncreasing {
		t.Errorf("Expected TrendIncreasing, got %v", result.CostTrend)
	}
}

// TestMinMessagesForTrendMatchesModel binds the analyzer's compute gate to the
// render-facing threshold. Every trend renderer gates on
// MessageInsights.HasTrend (which uses models.MinMessagesForTrend); if that
// diverged from the count at which CalculateInsights actually populates the
// trend fields, a surface would either fabricate a trend from zeros or drop a
// real one. Keep the two locked together.
func TestMinMessagesForTrendMatchesModel(t *testing.T) {
	if minMessagesForTrend != models.MinMessagesForTrend {
		t.Fatalf("analyzer minMessagesForTrend (%d) != models.MinMessagesForTrend (%d): the compute and render gates have drifted",
			minMessagesForTrend, models.MinMessagesForTrend)
	}
}

func TestCalculateInsights_SixMessages_StableTrend(t *testing.T) {
	now := time.Now()
	// Stable costs (within 20% variance)
	messages := []models.MessageAnalysis{
		{Timestamp: now, Cost: models.CostBreakdown{TotalCost: 0.10}},
		{Timestamp: now.Add(time.Minute), Cost: models.CostBreakdown{TotalCost: 0.11}},
		{Timestamp: now.Add(2 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.09}},
		{Timestamp: now.Add(3 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.10}},
		{Timestamp: now.Add(4 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.11}},
		{Timestamp: now.Add(5 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.10}},
	}

	result := CalculateInsights(messages)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Should be stable
	if result.CostTrend != models.TrendStable {
		t.Errorf("Expected TrendStable, got %v", result.CostTrend)
	}
}

// Below MinMessagesForTrend no trend is computed, and the trend fields keep
// their zero values.
func TestCalculateInsights_FiveMessages_NoTrend(t *testing.T) {
	now := time.Now()
	messages := []models.MessageAnalysis{
		{Timestamp: now, Cost: models.CostBreakdown{TotalCost: 0.05}},
		{Timestamp: now.Add(time.Minute), Cost: models.CostBreakdown{TotalCost: 0.10}},
		{Timestamp: now.Add(2 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.15}},
		{Timestamp: now.Add(3 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.25}},
		{Timestamp: now.Add(4 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.30}},
	}

	result := CalculateInsights(messages)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.RecentAvgCost != 0 || result.TrendWindow != 0 {
		t.Errorf("trend fields should be zero at 5 messages, got recent=%f window=%d", result.RecentAvgCost, result.TrendWindow)
	}
	if result.CostTrend != models.TrendStable {
		t.Errorf("CostTrend should stay at zero value (stable), got %v", result.CostTrend)
	}
}

func TestCalculateInsights_HighestCostNotable(t *testing.T) {
	now := time.Now()
	// One message is >1.5x average
	messages := []models.MessageAnalysis{
		{Timestamp: now, Cost: models.CostBreakdown{TotalCost: 0.10}},
		{Timestamp: now.Add(time.Minute), Cost: models.CostBreakdown{TotalCost: 0.10}},
		{Timestamp: now.Add(2 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.50}}, // Outlier
		{Timestamp: now.Add(3 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.10}},
	}

	result := CalculateInsights(messages)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Average = (0.10 + 0.10 + 0.50 + 0.10) / 4 = 0.20
	// Highest = 0.50, which is 2.5x average
	if result.HighestCost == nil {
		t.Fatal("Expected HighestCost to be set")
	}

	if !almostEqual(result.HighestCost.Cost, 0.50, 0.001) {
		t.Errorf("Expected HighestCost.Cost=0.50, got %f", result.HighestCost.Cost)
	}

	multiplier := result.CostMultiplier()
	if !almostEqual(multiplier, 2.5, 0.01) {
		t.Errorf("Expected multiplier=2.5, got %f", multiplier)
	}
}

func TestCalculateInsights_HighestCostNotNotable(t *testing.T) {
	now := time.Now()
	// All costs uniform - highest is not notable
	messages := []models.MessageAnalysis{
		{Timestamp: now, Cost: models.CostBreakdown{TotalCost: 0.10}},
		{Timestamp: now.Add(time.Minute), Cost: models.CostBreakdown{TotalCost: 0.10}},
		{Timestamp: now.Add(2 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.12}}, // Slightly higher but not 1.5x
		{Timestamp: now.Add(3 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0.10}},
	}

	result := CalculateInsights(messages)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Average = 0.105, highest = 0.12, which is only 1.14x average
	if result.HighestCost != nil {
		t.Error("Expected HighestCost to be nil for uniform costs")
	}
}

func TestCalculateInsights_ZeroCosts(t *testing.T) {
	now := time.Now()
	// All zero costs
	messages := []models.MessageAnalysis{
		{Timestamp: now, Cost: models.CostBreakdown{TotalCost: 0}},
		{Timestamp: now.Add(time.Minute), Cost: models.CostBreakdown{TotalCost: 0}},
		{Timestamp: now.Add(2 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0}},
		{Timestamp: now.Add(3 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0}},
		{Timestamp: now.Add(4 * time.Minute), Cost: models.CostBreakdown{TotalCost: 0}},
	}

	result := CalculateInsights(messages)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.AverageCost != 0 {
		t.Errorf("Expected AverageCost=0, got %f", result.AverageCost)
	}

	// Trend should be stable for zero costs
	if result.CostTrend != models.TrendStable {
		t.Errorf("Expected TrendStable for zero costs, got %v", result.CostTrend)
	}
}

func TestGetMainCostComponent(t *testing.T) {
	tests := []struct {
		name          string
		cost          models.CostBreakdown
		expectedName  string
		expectedValue float64
	}{
		{
			name: "input highest",
			cost: models.CostBreakdown{
				InputCost:     0.50,
				OutputCost:    0.30,
				CacheReadCost: 0.10,
			},
			expectedName:  "input",
			expectedValue: 0.50,
		},
		{
			name: "output highest",
			cost: models.CostBreakdown{
				InputCost:  0.10,
				OutputCost: 0.80,
			},
			expectedName:  "output",
			expectedValue: 0.80,
		},
		{
			name: "cache_write_5m highest",
			cost: models.CostBreakdown{
				InputCost:        0.10,
				OutputCost:       0.20,
				CacheWrite5mCost: 0.50,
			},
			expectedName:  "cache_write_5m",
			expectedValue: 0.50,
		},
		{
			name: "cache_write_1h highest",
			cost: models.CostBreakdown{
				InputCost:        0.10,
				OutputCost:       0.20,
				CacheWrite1hCost: 0.60,
			},
			expectedName:  "cache_write_1h",
			expectedValue: 0.60,
		},
		{
			name: "cache_read highest",
			cost: models.CostBreakdown{
				InputCost:     0.01,
				OutputCost:    0.02,
				CacheReadCost: 0.50,
			},
			expectedName:  "cache_read",
			expectedValue: 0.50,
		},
		{
			name:          "all zero",
			cost:          models.CostBreakdown{},
			expectedName:  "input",
			expectedValue: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, value := GetMainCostComponent(tt.cost)
			if name != tt.expectedName {
				t.Errorf("Expected component name %q, got %q", tt.expectedName, name)
			}
			if !almostEqual(value, tt.expectedValue, 0.001) {
				t.Errorf("Expected value %f, got %f", tt.expectedValue, value)
			}
		})
	}
}

func TestTrendDirection_String(t *testing.T) {
	tests := []struct {
		trend    models.TrendDirection
		expected string
	}{
		{models.TrendStable, "stable"},
		{models.TrendIncreasing, "increasing"},
		{models.TrendDecreasing, "decreasing"},
	}

	for _, tt := range tests {
		result := tt.trend.String()
		if result != tt.expected {
			t.Errorf("TrendDirection.String(): got %q, want %q", result, tt.expected)
		}
	}
}

func TestMessageInsights_TrendDescription(t *testing.T) {
	// Below the trend threshold - no description, because CalculateInsights never
	// computed a trend (the fields would be zero values). This guards against the
	// old bug where a description was rendered from never-computed data.
	insights := &models.MessageInsights{
		MessageCount: 3,
		CostTrend:    models.TrendDecreasing,
	}
	if insights.TrendDescription() != "" {
		t.Errorf("Expected empty description below the trend threshold")
	}

	// Exactly 5 messages is still below MinMessagesForTrend (6), so no trend is
	// computed and none is described.
	insights.MessageCount = models.MinMessagesForTrend - 1
	if desc := insights.TrendDescription(); desc != "" {
		t.Errorf("Expected empty description at %d messages, got %q", models.MinMessagesForTrend-1, desc)
	}

	// At the threshold - should return a description
	insights.MessageCount = models.MinMessagesForTrend
	desc := insights.TrendDescription()
	if desc != "falling" {
		t.Errorf("Expected 'falling' for TrendDecreasing, got %q", desc)
	}

	insights.CostTrend = models.TrendIncreasing
	desc = insights.TrendDescription()
	if desc != "rising" {
		t.Errorf("Expected 'rising' for TrendIncreasing, got %q", desc)
	}

	insights.CostTrend = models.TrendStable
	desc = insights.TrendDescription()
	if desc != "flat" {
		t.Errorf("Expected 'flat' for TrendStable, got %q", desc)
	}
}

func TestMessageInsights_CostMultiplier(t *testing.T) {
	// No highest cost
	insights := &models.MessageInsights{
		AverageCost: 0.10,
		HighestCost: nil,
	}
	if insights.CostMultiplier() != 0 {
		t.Errorf("Expected 0 when HighestCost is nil")
	}

	// Zero average
	insights.HighestCost = &models.MessageSnapshot{Cost: 0.50}
	insights.AverageCost = 0
	if insights.CostMultiplier() != 0 {
		t.Errorf("Expected 0 when AverageCost is 0")
	}

	// Normal case
	insights.AverageCost = 0.10
	mult := insights.CostMultiplier()
	if !almostEqual(mult, 5.0, 0.001) {
		t.Errorf("Expected multiplier 5.0, got %f", mult)
	}
}

func TestCalculateInsights_ManyMessages(t *testing.T) {
	now := time.Now()
	// 10 messages with varying costs
	messages := make([]models.MessageAnalysis, 10)
	for i := 0; i < 10; i++ {
		cost := 0.10 + float64(i)*0.01 // Gradually increasing
		messages[i] = models.MessageAnalysis{
			Timestamp: now.Add(time.Duration(i) * time.Minute),
			Cost: models.CostBreakdown{
				InputCost:  cost * 0.6,
				OutputCost: cost * 0.4,
				TotalCost:  cost,
			},
		}
	}

	result := CalculateInsights(messages)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.MessageCount != 10 {
		t.Errorf("Expected MessageCount=10, got %d", result.MessageCount)
	}

	// First = 0.10, Last = 0.19
	if !almostEqual(result.FirstMessage.Cost, 0.10, 0.001) {
		t.Errorf("Expected FirstMessage.Cost=0.10, got %f", result.FirstMessage.Cost)
	}
	if !almostEqual(result.LastMessage.Cost, 0.19, 0.001) {
		t.Errorf("Expected LastMessage.Cost=0.19, got %f", result.LastMessage.Cost)
	}

	// The last 5 average $0.17 against a session average of $0.145: 17%
	// up, inside the 20% band, so flat
	if result.CostTrend != models.TrendStable {
		t.Errorf("Expected TrendStable, got %v", result.CostTrend)
	}
}

// A long session's trend averages its last 20 messages, so a few small
// replies at the end can't flip it.
func TestCalculateInsights_TrendHoldsAcrossAFewSmallMessages(t *testing.T) {
	now := time.Now()
	var messages []models.MessageAnalysis
	add := func(cost float64) {
		messages = append(messages, models.MessageAnalysis{
			Timestamp: now.Add(time.Duration(len(messages)) * time.Minute),
			Cost:      models.CostBreakdown{TotalCost: cost},
		})
	}
	// 200 messages around $0.04, then 40 around $0.09: a real rise
	for i := range 200 {
		add(0.04 + float64(i%3)*0.002)
	}
	for i := range 40 {
		add(0.09 + float64(i%3)*0.002)
	}
	result := CalculateInsights(messages)
	if result.TrendWindow != 20 || result.CostTrend != models.TrendIncreasing {
		t.Fatalf("window %d, trend %v; want 20, rising", result.TrendWindow, result.CostTrend)
	}

	// Three small replies: a window of three would read falling here
	for range 3 {
		add(0.02)
	}
	if result := CalculateInsights(messages); result.CostTrend != models.TrendIncreasing {
		t.Errorf("three small replies flipped the trend to %v", result.CostTrend)
	}
}
