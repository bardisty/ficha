package analyzer

import (
	"github.com/bardisty/ccusage/internal/models"
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

	// Highest cost message - only include if >1.5x average
	if highestCost > insights.AverageCost*1.5 {
		insights.HighestCost = createSnapshot(messages[highestIdx], highestIdx+1) // 1-based index
	}

	// Calculate trend for sessions with 5+ messages
	if len(messages) >= 5 {
		// Average of first 3 messages
		var earlySum float64
		for i := 0; i < 3; i++ {
			earlySum += messages[i].Cost.TotalCost
		}
		insights.EarlyAvgCost = earlySum / 3

		// Average of last 3 messages
		var lateSum float64
		for i := len(messages) - 3; i < len(messages); i++ {
			lateSum += messages[i].Cost.TotalCost
		}
		insights.LateAvgCost = lateSum / 3

		// Determine trend direction (20% threshold for stability)
		if insights.EarlyAvgCost > 0 {
			change := (insights.LateAvgCost - insights.EarlyAvgCost) / insights.EarlyAvgCost
			if change > 0.2 {
				insights.CostTrend = models.TrendIncreasing
			} else if change < -0.2 {
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
