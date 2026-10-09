package analyzer

import (
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
)

// CalculateCost calculates the cost breakdown for a token usage with a specific model
func CalculateCost(usage models.TokenUsage, modelID string) models.CostBreakdown {
	// Validate token counts - clamp negatives to zero to prevent invalid costs.
	// Parser-derived usages arrive already sanitized and reconciled
	// (parser.ExtractUsageFromMessages); this repeat is a no-op there and a
	// safety net for hand-built usages.
	usage = usage.Sanitized()

	modelPricing := pricing.GetModelPricing(modelID).ForPrompt(usage.ContextWindowSize())

	// Calculate input cost (non-cached tokens)
	inputCost := float64(usage.InputTokens) / 1_000_000 * modelPricing.InputRate

	// Calculate output cost
	outputCost := float64(usage.OutputTokens) / 1_000_000 * modelPricing.OutputRate

	// Calculate cache write costs
	var cacheWrite5mCost, cacheWrite1hCost float64

	if usage.CacheCreation != nil {
		// Use detailed cache creation breakdown when available
		cacheWrite5mCost = float64(usage.CacheCreation.Ephemeral5mInputTokens) / 1_000_000 *
			pricing.GetCacheWrite5mRate(modelPricing)
		cacheWrite1hCost = float64(usage.CacheCreation.Ephemeral1hInputTokens) / 1_000_000 *
			pricing.GetCacheWrite1hRate(modelPricing)
	} else {
		// Fallback: assume all cache creation tokens are 5m TTL (1.25x multiplier).
		// This may slightly underestimate costs if 1h TTL tokens (2.0x multiplier)
		// were actually used. Parser-derived usages never reach this branch —
		// reconcileUsage materializes CacheCreation (folding unattributed write
		// tokens into the 5m bucket, counted in EstimatedCostMessages) — so it
		// only serves hand-built usages, where it applies the same assumption.
		cacheWrite5mCost = float64(usage.CacheCreationInputTokens) / 1_000_000 *
			pricing.GetCacheWrite5mRate(modelPricing)
	}

	// Calculate cache read cost
	cacheReadCost := float64(usage.CacheReadInputTokens) / 1_000_000 *
		pricing.GetCacheReadRate(modelPricing)

	// Calculate total cost
	totalCost := inputCost + outputCost + cacheWrite5mCost + cacheWrite1hCost + cacheReadCost

	// Calculate cache savings
	// Savings = what we would have paid at full input rate - what we paid at cache read rate
	fullPrice := float64(usage.CacheReadInputTokens) / 1_000_000 * modelPricing.InputRate
	cacheSavings := fullPrice - cacheReadCost

	return models.CostBreakdown{
		InputCost:        inputCost,
		OutputCost:       outputCost,
		CacheWrite5mCost: cacheWrite5mCost,
		CacheWrite1hCost: cacheWrite1hCost,
		CacheReadCost:    cacheReadCost,
		TotalCost:        totalCost,
		CacheSavings:     cacheSavings,
	}
}

// CalculateMessageCost calculates the cost for a message analysis
func CalculateMessageCost(msg *models.MessageAnalysis) {
	msg.Cost = CalculateCost(msg.Usage, msg.Model)
}

// AggregateCosts sums up multiple cost breakdowns
func AggregateCosts(costs []models.CostBreakdown) models.CostBreakdown {
	var total models.CostBreakdown

	for _, cost := range costs {
		total.Add(cost)
	}

	return total
}

// AggregateUsage sums up multiple token usages
func AggregateUsage(usages []models.TokenUsage) models.TokenUsage {
	var total models.TokenUsage

	for _, usage := range usages {
		total.Add(usage)
	}

	return total
}
