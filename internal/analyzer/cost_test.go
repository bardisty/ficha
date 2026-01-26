package analyzer

import (
	"math"
	"testing"

	"github.com/bah/ccusage/internal/models"
)

func almostEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		name          string
		usage         models.TokenUsage
		modelID       string
		expectedTotal float64
	}{
		{
			name: "basic input/output",
			usage: models.TokenUsage{
				InputTokens:  1_000_000, // 1M tokens
				OutputTokens: 1_000_000, // 1M tokens
			},
			modelID:       "claude-sonnet-4-5",
			expectedTotal: 3.00 + 15.00, // $3 input + $15 output = $18
		},
		{
			name: "with cache read",
			usage: models.TokenUsage{
				InputTokens:         500_000,
				OutputTokens:        100_000,
				CacheReadInputTokens: 500_000,
			},
			modelID:       "claude-sonnet-4-5",
			expectedTotal: 1.5 + 1.5 + 0.15, // input + output + cache_read
		},
		{
			name: "with cache write 5m",
			usage: models.TokenUsage{
				InputTokens:              500_000,
				OutputTokens:             100_000,
				CacheCreationInputTokens: 500_000,
			},
			modelID:       "claude-sonnet-4-5",
			expectedTotal: 1.5 + 1.5 + 1.875, // input + output + cache_write_5m (1.25x)
		},
		{
			name: "with detailed cache creation",
			usage: models.TokenUsage{
				InputTokens:  500_000,
				OutputTokens: 100_000,
				CacheCreation: &models.CacheCreation{
					Ephemeral5mInputTokens: 250_000,
					Ephemeral1hInputTokens: 250_000,
				},
			},
			modelID:       "claude-sonnet-4-5",
			expectedTotal: 1.5 + 1.5 + 0.9375 + 1.5, // input + output + 5m + 1h
		},
		{
			name: "zero tokens",
			usage: models.TokenUsage{
				InputTokens:  0,
				OutputTokens: 0,
			},
			modelID:       "claude-opus-4-5",
			expectedTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := CalculateCost(tt.usage, tt.modelID)
			if !almostEqual(cost.TotalCost, tt.expectedTotal, 0.001) {
				t.Errorf("TotalCost: got %f, want %f", cost.TotalCost, tt.expectedTotal)
			}
		})
	}
}

func TestCalculateCostCacheSavings(t *testing.T) {
	// Cache read tokens should show 90% savings compared to full input price
	usage := models.TokenUsage{
		InputTokens:         0,
		CacheReadInputTokens: 1_000_000, // 1M tokens cached
	}

	cost := CalculateCost(usage, "claude-sonnet-4-5")

	// Full price would be $3 (1M * $3/M)
	// Cache read price is $0.30 (1M * $0.30/M)
	// Savings should be $2.70 (90% of $3)
	expectedSavings := 2.70

	if !almostEqual(cost.CacheSavings, expectedSavings, 0.001) {
		t.Errorf("CacheSavings: got %f, want %f", cost.CacheSavings, expectedSavings)
	}
}

func TestCalculateMessageCost(t *testing.T) {
	msg := &models.MessageAnalysis{
		Model: "claude-sonnet-4-5",
		Usage: models.TokenUsage{
			InputTokens:  1_000_000,
			OutputTokens: 500_000,
		},
	}

	CalculateMessageCost(msg)

	// $3 input + $7.50 output = $10.50
	expectedTotal := 10.50
	if !almostEqual(msg.Cost.TotalCost, expectedTotal, 0.001) {
		t.Errorf("TotalCost: got %f, want %f", msg.Cost.TotalCost, expectedTotal)
	}
}

func TestAggregateCosts(t *testing.T) {
	costs := []models.CostBreakdown{
		{InputCost: 1.0, OutputCost: 2.0, TotalCost: 3.0, CacheSavings: 0.5},
		{InputCost: 1.5, OutputCost: 2.5, TotalCost: 4.0, CacheSavings: 0.7},
		{InputCost: 0.5, OutputCost: 1.0, TotalCost: 1.5, CacheSavings: 0.3},
	}

	total := AggregateCosts(costs)

	if !almostEqual(total.InputCost, 3.0, 0.001) {
		t.Errorf("InputCost: got %f, want 3.0", total.InputCost)
	}
	if !almostEqual(total.OutputCost, 5.5, 0.001) {
		t.Errorf("OutputCost: got %f, want 5.5", total.OutputCost)
	}
	if !almostEqual(total.TotalCost, 8.5, 0.001) {
		t.Errorf("TotalCost: got %f, want 8.5", total.TotalCost)
	}
	if !almostEqual(total.CacheSavings, 1.5, 0.001) {
		t.Errorf("CacheSavings: got %f, want 1.5", total.CacheSavings)
	}
}

func TestAggregateUsage(t *testing.T) {
	usages := []models.TokenUsage{
		{InputTokens: 100, OutputTokens: 50, CacheReadInputTokens: 10},
		{InputTokens: 200, OutputTokens: 100, CacheReadInputTokens: 20},
		{InputTokens: 150, OutputTokens: 75, CacheReadInputTokens: 15},
	}

	total := AggregateUsage(usages)

	if total.InputTokens != 450 {
		t.Errorf("InputTokens: got %d, want 450", total.InputTokens)
	}
	if total.OutputTokens != 225 {
		t.Errorf("OutputTokens: got %d, want 225", total.OutputTokens)
	}
	if total.CacheReadInputTokens != 45 {
		t.Errorf("CacheReadInputTokens: got %d, want 45", total.CacheReadInputTokens)
	}
}
