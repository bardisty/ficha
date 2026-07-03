package analyzer

import (
	"github.com/bardisty/ccusage/internal/models"
)

// Insight calculation thresholds
const (
	// highCostMultiplier defines how many times above average a message cost
	// must be to be flagged as notably high (e.g., 1.5 = 50% above average)
	highCostMultiplier = 1.5

	// trendChangeThreshold defines the minimum relative change (as a fraction)
	// required to classify a trend as increasing or decreasing (0.2 = 20%)
	trendChangeThreshold = 0.2

	// trendSampleSize is the number of messages at each end used to calculate
	// early and late average costs for trend detection
	trendSampleSize = 3

	// minMessagesForTrend is the minimum number of messages required
	// to calculate meaningful cost trends; 2*trendSampleSize keeps the
	// early and late sample windows disjoint
	minMessagesForTrend = 2 * trendSampleSize
)

// CalculateInsights computes insights from a slice of message analyses
// Returns nil if there are no messages to analyze
func CalculateInsights(messages []models.MessageAnalysis) *models.MessageInsights {
	if len(messages) == 0 {
		return nil
	}

	insights := &models.MessageInsights{
		MessageCount: len(messages),
	}

	// First and last messages (1-based indices)
	first := messages[0]
	insights.FirstMessage = createSnapshot(first, 1)

	last := messages[len(messages)-1]
	insights.LastMessage = createSnapshot(last, len(messages))

	// Calculate total and average cost
	var totalCost float64
	var highestIdx int
	var highestCost float64

	for i, msg := range messages {
		totalCost += msg.Cost.TotalCost
		if msg.Cost.TotalCost > highestCost {
			highestCost = msg.Cost.TotalCost
			highestIdx = i
		}
	}

	insights.AverageCost = totalCost / float64(len(messages))

	// Highest cost message - only include if notably above average
	if highestCost > insights.AverageCost*highCostMultiplier {
		insights.HighestCost = createSnapshot(messages[highestIdx], highestIdx+1) // 1-based index
	}

	// Calculate trend for sessions with enough messages
	if len(messages) >= minMessagesForTrend {
		// Average of first N messages
		var earlySum float64
		for i := 0; i < trendSampleSize; i++ {
			earlySum += messages[i].Cost.TotalCost
		}
		insights.EarlyAvgCost = earlySum / float64(trendSampleSize)

		// Average of last N messages
		var lateSum float64
		for i := len(messages) - trendSampleSize; i < len(messages); i++ {
			lateSum += messages[i].Cost.TotalCost
		}
		insights.LateAvgCost = lateSum / float64(trendSampleSize)

		// Determine trend direction based on threshold
		if insights.EarlyAvgCost > 0 {
			change := (insights.LateAvgCost - insights.EarlyAvgCost) / insights.EarlyAvgCost
			if change > trendChangeThreshold {
				insights.CostTrend = models.TrendIncreasing
			} else if change < -trendChangeThreshold {
				insights.CostTrend = models.TrendDecreasing
			} else {
				insights.CostTrend = models.TrendStable
			}
		} else if insights.LateAvgCost > 0 {
			// Early was zero, late is positive = increasing
			insights.CostTrend = models.TrendIncreasing
		} else {
			insights.CostTrend = models.TrendStable
		}
	}

	return insights
}

// createSnapshot creates a MessageSnapshot from a MessageAnalysis
func createSnapshot(msg models.MessageAnalysis, index int) *models.MessageSnapshot {
	component, value := GetMainCostComponent(msg.Cost)
	return &models.MessageSnapshot{
		Index:             index,
		Timestamp:         msg.Timestamp,
		Cost:              msg.Cost.TotalCost,
		MainCostComponent: component,
		MainCostValue:     value,
	}
}

// GetMainCostComponent identifies the largest cost driver in a cost breakdown
func GetMainCostComponent(cost models.CostBreakdown) (string, float64) {
	type component struct {
		name  string
		value float64
	}

	components := []component{
		{"input", cost.InputCost},
		{"output", cost.OutputCost},
		{"cache_write_5m", cost.CacheWrite5mCost},
		{"cache_write_1h", cost.CacheWrite1hCost},
		{"cache_read", cost.CacheReadCost},
	}

	maxComponent := components[0]
	for _, c := range components[1:] {
		if c.value > maxComponent.value {
			maxComponent = c
		}
	}

	return maxComponent.name, maxComponent.value
}
