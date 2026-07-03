package pricing

import (
	"sort"
	"strings"
)

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

// ModelInfo is one row of the model catalog: the canonical ID (also the
// prefix that dated variants like "claude-opus-4-5-20251101" resolve
// against), display name, rates, and context window.
type ModelInfo struct {
	ID               string
	DisplayName      string
	InputRate        float64 // Cost per million input tokens
	OutputRate       float64 // Cost per million output tokens
	MaxContextTokens int
}

// modelCatalog is the single source of truth for known models. The pricing
// map, display-name map, and prefix patterns are derived from it at init —
// adding a model means adding one row here.
var modelCatalog = []ModelInfo{
	// Claude 5 family
	{ID: "claude-fable-5", DisplayName: "Fable 5", InputRate: 10.00, OutputRate: 50.00, MaxContextTokens: 1000000},
	{ID: "claude-mythos-5", DisplayName: "Mythos 5", InputRate: 10.00, OutputRate: 50.00, MaxContextTokens: 1000000},
	// Sticker rates; intro pricing ($2/$10 through 2026-08-31) deliberately not modeled (decision D2)
	{ID: "claude-sonnet-5", DisplayName: "Sonnet 5", InputRate: 3.00, OutputRate: 15.00, MaxContextTokens: 1000000},
	// Opus 4.x
	{ID: "claude-opus-4-8", DisplayName: "Opus 4.8", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 1000000},
	{ID: "claude-opus-4-7", DisplayName: "Opus 4.7", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 1000000},
	{ID: "claude-opus-4-6", DisplayName: "Opus 4.6", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 1000000},
	{ID: "claude-opus-4-5", DisplayName: "Opus 4.5", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 200000},
	{ID: "claude-opus-4-1", DisplayName: "Opus 4.1", InputRate: 15.00, OutputRate: 75.00, MaxContextTokens: 200000},
	// "claude-opus-4-0" / "claude-sonnet-4-0" are the official aliases for
	// Opus 4 / Sonnet 4; listed explicitly because their "-0" suffix would
	// otherwise be rejected as a version segment by normalizeModelID
	{ID: "claude-opus-4-0", DisplayName: "Opus 4", InputRate: 15.00, OutputRate: 75.00, MaxContextTokens: 200000},
	{ID: "claude-opus-4", DisplayName: "Opus 4", InputRate: 15.00, OutputRate: 75.00, MaxContextTokens: 200000},
	// Sonnet 4.x
	{ID: "claude-sonnet-4-6", DisplayName: "Sonnet 4.6", InputRate: 3.00, OutputRate: 15.00, MaxContextTokens: 1000000},
	{ID: "claude-sonnet-4-5", DisplayName: "Sonnet 4.5", InputRate: 3.00, OutputRate: 15.00, MaxContextTokens: 200000},
	{ID: "claude-sonnet-4-0", DisplayName: "Sonnet 4", InputRate: 3.00, OutputRate: 15.00, MaxContextTokens: 200000},
	{ID: "claude-sonnet-4", DisplayName: "Sonnet 4", InputRate: 3.00, OutputRate: 15.00, MaxContextTokens: 200000},
	// Haiku 4.x
	{ID: "claude-haiku-4-5", DisplayName: "Haiku 4.5", InputRate: 1.00, OutputRate: 5.00, MaxContextTokens: 200000},
	// Legacy models
	{ID: "claude-sonnet-3-7", DisplayName: "Sonnet 3.7", InputRate: 3.00, OutputRate: 15.00, MaxContextTokens: 200000},
	{ID: "claude-3-7-sonnet", DisplayName: "Sonnet 3.7", InputRate: 3.00, OutputRate: 15.00, MaxContextTokens: 200000},
	{ID: "claude-3-5-sonnet", DisplayName: "Sonnet 3.5", InputRate: 3.00, OutputRate: 15.00, MaxContextTokens: 200000},
	{ID: "claude-3-5-haiku", DisplayName: "Haiku 3.5", InputRate: 0.80, OutputRate: 4.00, MaxContextTokens: 200000},
	{ID: "claude-3-opus", DisplayName: "Opus 3", InputRate: 15.00, OutputRate: 75.00, MaxContextTokens: 200000},
	{ID: "claude-3-sonnet", DisplayName: "Sonnet 3", InputRate: 3.00, OutputRate: 15.00, MaxContextTokens: 200000},
	{ID: "claude-3-haiku", DisplayName: "Haiku 3", InputRate: 0.25, OutputRate: 1.25, MaxContextTokens: 200000},
}

// Derived from modelCatalog at init
var (
	modelPricing   = make(map[string]ModelPricing, len(modelCatalog))
	displayNames   = make(map[string]string, len(modelCatalog))
	prefixPatterns = make([]string, 0, len(modelCatalog)) // catalog IDs, longest first
)

func init() {
	for _, m := range modelCatalog {
		modelPricing[m.ID] = ModelPricing{
			InputRate:        m.InputRate,
			OutputRate:       m.OutputRate,
			MaxContextTokens: m.MaxContextTokens,
		}
		displayNames[m.ID] = m.DisplayName
		prefixPatterns = append(prefixPatterns, m.ID)
	}
	sort.Slice(prefixPatterns, func(i, j int) bool {
		return len(prefixPatterns[i]) > len(prefixPatterns[j])
	})
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

// normalizeModelID resolves a versioned model ID (e.g. "claude-opus-4-5-20251101")
// to its canonical catalog ID. A prefix match only counts when the suffix is a
// date or alias variant of the same model — a short numeric segment right after
// the prefix ("claude-opus-4-9") denotes a different model in the family and
// must not inherit the prefix's pricing.
func normalizeModelID(modelID string) string {
	for _, pattern := range prefixPatterns {
		if !strings.HasPrefix(modelID, pattern) {
			continue
		}
		rest := modelID[len(pattern):]
		if rest == "" {
			return pattern
		}
		if rest[0] != '-' || isVersionSegment(firstSegment(rest[1:])) {
			continue
		}
		return pattern
	}

	return modelID
}

// firstSegment returns s up to (excluding) the first '-'.
func firstSegment(s string) string {
	seg, _, _ := strings.Cut(s, "-")
	return seg
}

// isVersionSegment reports whether seg looks like a model version number
// (1-7 digits, e.g. the "9" in "claude-opus-4-9") as opposed to a date
// ("20250514", 8 digits) or a named alias ("latest").
func isVersionSegment(seg string) bool {
	if len(seg) == 0 || len(seg) >= 8 {
		return false
	}
	for i := 0; i < len(seg); i++ {
		if seg[i] < '0' || seg[i] > '9' {
			return false
		}
	}
	return true
}

// GetModelDisplayName returns a human-readable name for a model ID
func GetModelDisplayName(modelID string) string {
	normalized := normalizeModelID(modelID)

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

// GetFreeSpace returns the free space in tokens given current context usage
func GetFreeSpace(pricing ModelPricing, currentUsage int64) int64 {
	free := int64(pricing.MaxContextTokens) - currentUsage
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
