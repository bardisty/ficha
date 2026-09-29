package analyzer

import (
	"github.com/bardisty/ficha/internal/models"
)

// Insight calculation thresholds
const (
	// highCostMultiplier defines how many times above average a message cost
	// must be to be flagged as notably high (e.g., 1.5 = 50% above average)
	highCostMultiplier = 1.5

	// trendChangeThreshold is the minimum relative difference (as a fraction)
	// between the recent and session averages for the trend to count as
	// rising or falling (0.2 = 20%)
	trendChangeThreshold = 0.2

	// trendWindow is how many of the latest messages the trend averages. A
	// window this wide moves by a twentieth of a message's difference from the
	// mean per message, so one large or small reply can't flip it, where a
	// window of three would flip on almost every message.
	trendWindow = 20

	// minMessagesForTrend is the minimum number of messages for a trend. It
	// equals models.MinMessagesForTrend (the gate every renderer consults via
	// MessageInsights.HasTrend); the two are bound by
	// TestMinMessagesForTrendMatchesModel so the compute and render thresholds
	// can never drift.
	minMessagesForTrend = 6
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

	// The trend compares the latest messages with the whole session. Short
	// sessions use their later half, so the window never is the whole session.
	if len(messages) >= minMessagesForTrend {
		window := min(trendWindow, len(messages)/2)
		var recentSum float64
		for _, msg := range messages[len(messages)-window:] {
			recentSum += msg.Cost.TotalCost
		}
		insights.TrendWindow = window
		insights.RecentAvgCost = recentSum / float64(window)

		switch {
		case insights.AverageCost > 0:
			change := (insights.RecentAvgCost - insights.AverageCost) / insights.AverageCost
			switch {
			case change > trendChangeThreshold:
				insights.CostTrend = models.TrendIncreasing
			case change < -trendChangeThreshold:
				insights.CostTrend = models.TrendDecreasing
			default:
				insights.CostTrend = models.TrendStable
			}
		default:
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
