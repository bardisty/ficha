package pricing

import "strings"

// ModelPricing contains pricing information for a model (per million tokens)
type ModelPricing struct {
	InputRate        float64 // Cost per million input tokens
	OutputRate       float64 // Cost per million output tokens
	MaxContextTokens int     // Maximum context window size in tokens
}

// Cache multipliers (relative to input rate)
const (
	CacheWrite5mMultiplier = 1.25 // 5-minute TTL cache write
	CacheWrite1hMultiplier = 2.0  // 1-hour TTL cache write
	CacheReadMultiplier    = 0.1  // Cache read
)

// Context window constants
const (
	AutocompactBufferRatio = 0.225 // 22.5% of max context reserved for autocompact
)

// Model pricing constants (per million tokens)
var modelPricing = map[string]ModelPricing{
	// Opus 4.6
	"claude-opus-4-6": {
		InputRate:        5.00,
		OutputRate:       25.00,
		MaxContextTokens: 200000,
	},
	// Opus 4.5
	"claude-opus-4-5": {
		InputRate:        5.00,
		OutputRate:       25.00,
		MaxContextTokens: 200000,
	},
	// Opus 4.1
	"claude-opus-4-1": {
		InputRate:        15.00,
		OutputRate:       75.00,
		MaxContextTokens: 200000,
	},
	// Opus 4
	"claude-opus-4": {
		InputRate:        15.00,
		OutputRate:       75.00,
		MaxContextTokens: 200000,
	},
	// Sonnet 4.5
	"claude-sonnet-4-5": {
		InputRate:        3.00,
		OutputRate:       15.00,
		MaxContextTokens: 200000,
	},
	// Sonnet 4
	"claude-sonnet-4": {
		InputRate:        3.00,
		OutputRate:       15.00,
		MaxContextTokens: 200000,
	},
	// Haiku 4.5
	"claude-haiku-4-5": {
		InputRate:        1.00,
		OutputRate:       5.00,
		MaxContextTokens: 200000,
	},
	// Legacy/fallback models
	"claude-sonnet-3-7": {
		InputRate:        3.00,
		OutputRate:       15.00,
		MaxContextTokens: 200000,
	},
	"claude-3-7-sonnet": {
		InputRate:        3.00,
		OutputRate:       15.00,
		MaxContextTokens: 200000,
	},
	"claude-3-5-sonnet": {
		InputRate:        3.00,
		OutputRate:       15.00,
		MaxContextTokens: 200000,
	},
	"claude-3-5-haiku": {
		InputRate:        0.80,
		OutputRate:       4.00,
		MaxContextTokens: 200000,
	},
	"claude-3-opus": {
		InputRate:        15.00,
		OutputRate:       75.00,
		MaxContextTokens: 200000,
	},
	"claude-3-sonnet": {
		InputRate:        3.00,
		OutputRate:       15.00,
		MaxContextTokens: 200000,
	},
	"claude-3-haiku": {
		InputRate:        0.25,
		OutputRate:       1.25,
		MaxContextTokens: 200000,
	},
}

// Default pricing for unknown models (use Sonnet pricing as safe default)
var defaultPricing = ModelPricing{
	InputRate:        3.00,
	OutputRate:       15.00,
	MaxContextTokens: 200000,
}

// GetModelPricing returns the pricing for a model ID
// Uses pattern matching to handle versioned model IDs like "claude-opus-4-5-20251101"
func GetModelPricing(modelID string) ModelPricing {
	// First try exact match
	if pricing, ok := modelPricing[modelID]; ok {
		return pricing
	}

	// Try pattern matching by removing date suffix
	// e.g., "claude-opus-4-5-20251101" -> "claude-opus-4-5"
	normalized := normalizeModelID(modelID)
	if pricing, ok := modelPricing[normalized]; ok {
		return pricing
	}

	// Return default pricing
	return defaultPricing
}

// normalizeModelID removes date suffixes and other version info from model IDs
func normalizeModelID(modelID string) string {
	// Handle common patterns (order matters - longer prefixes first)
	patterns := []string{
		"claude-opus-4-6",
		"claude-opus-4-5",
		"claude-opus-4-1",
		"claude-opus-4",
		"claude-sonnet-4-5",
		"claude-sonnet-4",
		"claude-sonnet-3-7",
		"claude-haiku-4-5",
		"claude-3-7-sonnet",
		"claude-3-5-sonnet",
		"claude-3-5-haiku",
		"claude-3-opus",
		"claude-3-sonnet",
		"claude-3-haiku",
	}

	for _, pattern := range patterns {
		if strings.HasPrefix(modelID, pattern) {
			rest := modelID[len(pattern):]
			if rest == "" || rest[0] == '-' {
				return pattern
			}
		}
	}

	return modelID
}

// GetModelDisplayName returns a human-readable name for a model ID
func GetModelDisplayName(modelID string) string {
	normalized := normalizeModelID(modelID)

	displayNames := map[string]string{
		"claude-opus-4-6":   "Opus 4.6",
		"claude-opus-4-5":   "Opus 4.5",
		"claude-opus-4-1":   "Opus 4.1",
		"claude-opus-4":     "Opus 4",
		"claude-sonnet-4-5": "Sonnet 4.5",
		"claude-sonnet-4":   "Sonnet 4",
		"claude-sonnet-3-7": "Sonnet 3.7",
		"claude-3-7-sonnet": "Sonnet 3.7",
		"claude-haiku-4-5":  "Haiku 4.5",
		"claude-3-5-sonnet": "Sonnet 3.5",
		"claude-3-5-haiku":  "Haiku 3.5",
		"claude-3-opus":     "Opus 3",
		"claude-3-sonnet":   "Sonnet 3",
		"claude-3-haiku":    "Haiku 3",
	}

	if name, ok := displayNames[normalized]; ok {
		return name
	}

	return modelID
}

// GetCacheWrite5mRate returns the cache write rate for 5-minute TTL
func GetCacheWrite5mRate(pricing ModelPricing) float64 {
	return pricing.InputRate * CacheWrite5mMultiplier
}

// GetCacheWrite1hRate returns the cache write rate for 1-hour TTL
func GetCacheWrite1hRate(pricing ModelPricing) float64 {
	return pricing.InputRate * CacheWrite1hMultiplier
}

// GetCacheReadRate returns the cache read rate
func GetCacheReadRate(pricing ModelPricing) float64 {
	return pricing.InputRate * CacheReadMultiplier
}

// GetAutocompactBuffer returns the autocompact buffer size in tokens for a model
func GetAutocompactBuffer(pricing ModelPricing) int64 {
	return int64(float64(pricing.MaxContextTokens) * AutocompactBufferRatio)
}

// GetFreeSpace returns the free space in tokens given current context usage
// Free space = max context - current usage - autocompact buffer
func GetFreeSpace(pricing ModelPricing, currentUsage int64) int64 {
	autocompact := GetAutocompactBuffer(pricing)
	free := int64(pricing.MaxContextTokens) - currentUsage - autocompact
	if free < 0 {
		return 0
	}
	return free
}

// GetContextPercentage returns the percentage of context used (0-100)
func GetContextPercentage(pricing ModelPricing, currentUsage int64) float64 {
	if pricing.MaxContextTokens == 0 {
		return 0
	}
	return float64(currentUsage) / float64(pricing.MaxContextTokens) * 100
}

// IsKnownModel returns true if the model ID is recognized
// (i.e., has explicit pricing rather than falling back to defaults)
func IsKnownModel(modelID string) bool {
	if _, ok := modelPricing[modelID]; ok {
		return true
	}
	normalized := normalizeModelID(modelID)
	_, ok := modelPricing[normalized]
	return ok
}
