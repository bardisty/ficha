package pricing

import (
	"regexp"
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
	// Sticker rates; the time-boxed intro pricing ($2/$10 through 2026-08-31) is not modeled.
	{ID: "claude-sonnet-5", DisplayName: "Sonnet 5", InputRate: 3.00, OutputRate: 15.00, MaxContextTokens: 1000000},
	// Opus 4.x
	{ID: "claude-opus-4-8", DisplayName: "Opus 4.8", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 1000000},
	{ID: "claude-opus-4-7", DisplayName: "Opus 4.7", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 1000000},
	{ID: "claude-opus-4-6", DisplayName: "Opus 4.6", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 1000000},
	{ID: "claude-opus-4-5", DisplayName: "Opus 4.5", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 200000},
	{ID: "claude-opus-4-1", DisplayName: "Opus 4.1", InputRate: 15.00, OutputRate: 75.00, MaxContextTokens: 200000},
	// "claude-opus-4-0" / "claude-sonnet-4-0" are the official aliases for
	// Opus 4 / Sonnet 4; listed explicitly because their "-0" suffix would
	// otherwise be rejected as a version segment by NormalizeModelID
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

// longContextMarker is the suffix Claude Code appends to a model ID when the
// 1M-context beta is active, e.g. "claude-opus-4-8[1m]".
const longContextMarker = "[1m]"

// longContextTokens is the window size the 1M-context beta unlocks. Requests
// above the base window are billed at premium long-context rates, which this
// tool does not model — only the window size is adjusted.
const longContextTokens = 1000000

// Amazon Bedrock wraps the vendor-neutral ID in an inference-profile prefix and
// a version suffix: "us.anthropic.claude-opus-4-5-20251101-v1:0". Stripping both
// leaves an ID the catalog prefixes can match.
var (
	bedrockPrefixRe = regexp.MustCompile(`^([a-z0-9-]+\.)?anthropic\.`)
	bedrockSuffixRe = regexp.MustCompile(`-v\d+:\d+$`)
)

// GetModelPricing returns the pricing for a model ID, resolving versioned,
// provider-decorated, and 1M-context-beta IDs onto their catalog row.
func GetModelPricing(modelID string) ModelPricing {
	id, longContext, known := canonicalModelID(modelID)

	pricing := defaultPricing
	if known {
		pricing = modelPricing[id]
	}
	if longContext && pricing.MaxContextTokens < longContextTokens {
		pricing.MaxContextTokens = longContextTokens
	}
	return pricing
}

// canonicalModelID resolves a raw model ID onto the catalog. It reports the
// catalog ID (or modelID unchanged when nothing matches), whether the
// 1M-context beta marker was present, and whether the catalog knows the model.
func canonicalModelID(modelID string) (id string, longContext, known bool) {
	base, longContext := stripDecorations(modelID)

	if _, ok := modelPricing[base]; ok {
		return base, longContext, true
	}
	if pattern, ok := matchCatalogPrefix(base); ok {
		return pattern, longContext, true
	}
	return modelID, longContext, false
}

// stripDecorations removes provider-specific and beta decoration so that the
// vendor-neutral catalog prefixes can match:
//
//	"us.anthropic.claude-opus-4-5-20251101-v1:0" -> "claude-opus-4-5-20251101"
//	"claude-opus-4-8[1m]"                        -> "claude-opus-4-8", longContext
//
// Vertex IDs ("claude-opus-4-5@20251101") need no stripping — isBoundary treats
// '@' as a segment delimiter, so the date suffix is matched like a '-' one.
func stripDecorations(modelID string) (base string, longContext bool) {
	base = modelID
	if trimmed, ok := strings.CutSuffix(base, longContextMarker); ok {
		base, longContext = trimmed, true
	}
	// Only Bedrock IDs carry a '.' (profile prefix) or ':' (version suffix).
	// GetModelPricing runs once per message, so skip the regexes otherwise.
	if strings.IndexByte(base, '.') >= 0 {
		base = bedrockPrefixRe.ReplaceAllString(base, "")
	}
	if strings.IndexByte(base, ':') >= 0 {
		base = bedrockSuffixRe.ReplaceAllString(base, "")
	}
	return base, longContext
}

// matchCatalogPrefix resolves a stripped model ID (e.g. "claude-opus-4-5-20251101")
// to its canonical catalog ID. A prefix match only counts when the suffix is a
// date or alias variant of the same model — a short numeric segment right after
// the prefix ("claude-opus-4-9") denotes a different model in the family and
// must not inherit the prefix's pricing.
func matchCatalogPrefix(modelID string) (string, bool) {
	for _, pattern := range prefixPatterns {
		if !strings.HasPrefix(modelID, pattern) {
			continue
		}
		rest := modelID[len(pattern):]
		if rest == "" {
			return pattern, true
		}
		seg := firstSegment(rest[1:])
		if !isBoundary(rest[0]) || isVersionSegment(seg) || isServingTierSegment(seg) {
			continue
		}
		return pattern, true
	}

	return "", false
}

// NormalizeModelID resolves a model ID to its canonical catalog ID, returning
// modelID unchanged when the catalog does not know it. Aggregation keys on this
// so dated snapshots of one model ("claude-sonnet-4-5-20250929" and
// "-20251119") collapse to the single row they render as; an unknown ID keeps
// its raw form so nothing ficha cannot price is silently merged.
//
// The 1M-context marker is part of the decoration this strips, so
// "claude-opus-4-8[1m]" and "claude-opus-4-8" share a key. Their rates are
// identical (long-context premium pricing is unmodeled — see the catalog
// comments), so only the beta attribution is lost, not any cost.
func NormalizeModelID(modelID string) string {
	id, _, _ := canonicalModelID(modelID)
	return id
}

// isBoundary reports whether b separates a model ID's canonical prefix from its
// suffix: Anthropic delimits with '-', Vertex with '@'. A bracket marker is not
// a boundary — stripDecorations removes the one marker we understand ("[1m]"),
// so anything still bracketed here is an unrecognized decoration and must not
// resolve to a catalog row.
func isBoundary(b byte) bool {
	return b == '-' || b == '@'
}

// firstSegment returns s up to (excluding) the first boundary byte.
func firstSegment(s string) string {
	for i := 0; i < len(s); i++ {
		if isBoundary(s[i]) {
			return s[:i]
		}
	}
	return s
}

// isVersionSegment reports whether seg names a different model in the prefix's
// family rather than a date or alias variant of it. A leading digit means a
// version bump (the "9" in "claude-opus-4-9") unless the whole segment is an
// 8-digit release date; named aliases ("latest", the "v2" in
// "claude-3-5-sonnet-v2-20241022") start with a letter.
//
// A segment carrying an unrecognized decorator ("8[2m]") keeps its leading
// digit and so reads as a version bump. That is deliberate: an ID whose suffix
// form this code does not understand falls back to default pricing and an
// unknown-model warning, rather than silently inheriting a shorter prefix's
// rates — the failure mode that made "claude-opus-4-5@20251101" bill at Opus 4.
func isVersionSegment(seg string) bool {
	if len(seg) == 0 || !isDigit(seg[0]) {
		return false
	}
	return !isDateSegment(seg)
}

// servingTierSegments names suffix segments that denote a distinct,
// non-price-neutral serving tier of a model rather than a date or same-priced
// alias of it. The retired fast-mode IDs "claude-opus-4-6-fast" /
// "claude-opus-4-7-fast" billed at a premium tier, but their "-fast" suffix is
// letter-led, so isVersionSegment would otherwise wave them through as an alias
// like "latest" — inheriting the base row's standard rates, a known-model
// verdict (no unknown-model warning), and the base aggregation key. Listing the
// segment here forces such IDs to fail safe: they fall through to unknown ->
// raw aggregation key + default pricing + the existing unknown-model warning,
// exactly like an unrecognized decorator.
var servingTierSegments = map[string]bool{
	"fast": true,
}

// isServingTierSegment reports whether seg is a known serving-tier variant name
// (see servingTierSegments) that must not inherit its prefix's pricing.
func isServingTierSegment(seg string) bool {
	return servingTierSegments[seg]
}

// isDateSegment reports whether seg is an 8-digit release date ("20250514").
func isDateSegment(seg string) bool {
	if len(seg) != 8 {
		return false
	}
	for i := 0; i < len(seg); i++ {
		if !isDigit(seg[i]) {
			return false
		}
	}
	return true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// GetModelDisplayName returns a human-readable name for a model ID
func GetModelDisplayName(modelID string) string {
	if id, _, known := canonicalModelID(modelID); known {
		return displayNames[id]
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
	_, _, known := canonicalModelID(modelID)
	return known
}
