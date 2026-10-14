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
	CacheReadRate    float64 // Cost per million cache-read tokens; 0 = InputRate * CacheReadMultiplier
	MaxContextTokens int     // Maximum context window size in tokens
	CompactAtTokens  int     // Where Claude Code auto-compacts by default; see ContextZoneOf

	// A request whose prompt is longer than LongPromptTokens bills every token,
	// output included, at the long-prompt rates. 0 means one rate card.
	LongPromptTokens     int64
	LongPromptInputRate  float64
	LongPromptOutputRate float64
}

// ForPrompt returns the rate card that bills a request whose prompt is
// promptTokens long: input plus cache writes plus cache reads, which is what
// models.TokenUsage.ContextWindowSize counts. Cache rates follow the input
// rate, so a row with a long-prompt card must not set CacheReadRate.
func (p ModelPricing) ForPrompt(promptTokens int64) ModelPricing {
	if p.LongPromptTokens == 0 || promptTokens <= p.LongPromptTokens {
		return p
	}
	p.InputRate, p.OutputRate = p.LongPromptInputRate, p.LongPromptOutputRate
	return p
}

// ForContext returns the card a context reading of tokens is measured
// against. Claude Code writes a model ID without its "[1m]" marker, so a
// transcript doesn't say whether Opus 4.6 or Sonnet 4.6 ran on the 1M window.
// A reading past their 200K mark says it did, since Claude Code would have
// compacted it there otherwise, so the mark moves to the end of the window.
func (p ModelPricing) ForContext(tokens int64) ModelPricing {
	if tokens > int64(p.CompactAtTokens) && p.CompactAtTokens < min(p.MaxContextTokens, nativeLongContextCompactAt) {
		p.CompactAtTokens = p.MaxContextTokens
	}
	return p
}

// Cache multipliers (relative to input rate). Cache reads are the only bucket
// a catalog row may override with an absolute rate (see ModelInfo.CacheReadRate).
const (
	CacheWrite5mMultiplier = 1.25 // 5-minute TTL cache write
	CacheWrite1hMultiplier = 2.0  // 1-hour TTL cache write
	CacheReadMultiplier    = 0.1  // Cache read default
)

// ModelInfo is one row of the model catalog: the canonical ID (also the
// prefix that dated variants like "claude-opus-4-5-20251101" resolve
// against), display name, rates, and context window.
type ModelInfo struct {
	ID               string
	DisplayName      string
	InputRate        float64 // Cost per million input tokens
	OutputRate       float64 // Cost per million output tokens
	CacheReadRate    float64 // Absolute cache-read rate; 0 = InputRate * CacheReadMultiplier
	MaxContextTokens int
	// Where Claude Code auto-compacts the model by default. 0 = derived: a
	// native 1M window compacts at nativeLongContextCompactAt, any other
	// window when it fills.
	CompactAtTokens int

	// Rates for prompts over LongPromptTokens; see ModelPricing.ForPrompt
	LongPromptTokens     int64
	LongPromptInputRate  float64
	LongPromptOutputRate float64
}

// modelCatalog is the single source of truth for known models. The pricing
// map, display-name map, and prefix patterns are derived from it at init —
// adding a model means adding one row here.
var modelCatalog = []ModelInfo{
	// Claude 5 family
	// Fable 5.1 / Mythos 5.1 cache reads are $0.25/MTok (0.025x), not the 0.1x default.
	{ID: "claude-fable-5-1", DisplayName: "Fable 5.1", InputRate: 10.00, OutputRate: 50.00, CacheReadRate: 0.25, MaxContextTokens: 1000000},
	{ID: "claude-mythos-5-1", DisplayName: "Mythos 5.1", InputRate: 10.00, OutputRate: 50.00, CacheReadRate: 0.25, MaxContextTokens: 1000000},
	{ID: "claude-fable-5", DisplayName: "Fable 5", InputRate: 10.00, OutputRate: 50.00, MaxContextTokens: 1000000},
	{ID: "claude-mythos-5", DisplayName: "Mythos 5", InputRate: 10.00, OutputRate: 50.00, MaxContextTokens: 1000000},
	// Opus 5.5 and Sonnet 5.5 cache reads are 0.05x input ($0.20 and
	// $0.10/MTok), not the 0.1x default.
	{ID: "claude-opus-5-5", DisplayName: "Opus 5.5", InputRate: 4.00, OutputRate: 20.00, CacheReadRate: 0.20, MaxContextTokens: 1000000},
	{ID: "claude-opus-5", DisplayName: "Opus 5", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 1000000},
	{ID: "claude-sonnet-5-5", DisplayName: "Sonnet 5.5", InputRate: 2.00, OutputRate: 10.00, CacheReadRate: 0.10, MaxContextTokens: 1000000},
	{ID: "claude-sonnet-5", DisplayName: "Sonnet 5", InputRate: 2.00, OutputRate: 10.00, MaxContextTokens: 1000000},
	// Haiku 5.5 prompts over 100K tokens bill at $0.50/$2.50, five times the base card.
	{ID: "claude-haiku-5-5", DisplayName: "Haiku 5.5", InputRate: 0.10, OutputRate: 0.50, MaxContextTokens: 1000000,
		LongPromptTokens: 100000, LongPromptInputRate: 0.50, LongPromptOutputRate: 2.50},
	// Opus 4.x
	{ID: "claude-opus-4-8", DisplayName: "Opus 4.8", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 1000000},
	{ID: "claude-opus-4-7", DisplayName: "Opus 4.7", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 1000000},
	// Opus 4.6 and Sonnet 4.6 reach 1M only through "[1m]", so Claude Code
	// compacts them at 200K without it; see ModelPricing.ForContext.
	{ID: "claude-opus-4-6", DisplayName: "Opus 4.6", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 1000000, CompactAtTokens: 200000},
	{ID: "claude-opus-4-5", DisplayName: "Opus 4.5", InputRate: 5.00, OutputRate: 25.00, MaxContextTokens: 200000},
	{ID: "claude-opus-4-1", DisplayName: "Opus 4.1", InputRate: 15.00, OutputRate: 75.00, MaxContextTokens: 200000},
	// "claude-opus-4-0" / "claude-sonnet-4-0" are the official aliases for
	// Opus 4 / Sonnet 4; listed explicitly because their "-0" suffix would
	// otherwise be rejected as a version segment by NormalizeModelID
	{ID: "claude-opus-4-0", DisplayName: "Opus 4", InputRate: 15.00, OutputRate: 75.00, MaxContextTokens: 200000},
	{ID: "claude-opus-4", DisplayName: "Opus 4", InputRate: 15.00, OutputRate: 75.00, MaxContextTokens: 200000},
	// Sonnet 4.x
	{ID: "claude-sonnet-4-6", DisplayName: "Sonnet 4.6", InputRate: 3.00, OutputRate: 15.00, MaxContextTokens: 1000000, CompactAtTokens: 200000},
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
			CacheReadRate:    m.CacheReadRate,
			MaxContextTokens: m.MaxContextTokens,
			CompactAtTokens:  compactAt(m),

			LongPromptTokens:     m.LongPromptTokens,
			LongPromptInputRate:  m.LongPromptInputRate,
			LongPromptOutputRate: m.LongPromptOutputRate,
		}
		displayNames[m.ID] = m.DisplayName
		prefixPatterns = append(prefixPatterns, m.ID)
	}
	sort.Slice(prefixPatterns, func(i, j int) bool {
		return len(prefixPatterns[i]) > len(prefixPatterns[j])
	})
}

// nativeLongContextCompactAt is where Claude Code auto-compacts a model that
// runs with a 1M window by default: about 967K tokens, per
// https://code.claude.com/docs/en/model-config#default-auto-compact-thresholds.
// Every other model compacts when its window fills.
const nativeLongContextCompactAt = 967000

// compactAt is a catalog row's compaction point: its own when set, else
// derived from its window.
func compactAt(m ModelInfo) int {
	switch {
	case m.CompactAtTokens > 0:
		return m.CompactAtTokens
	case m.MaxContextTokens >= longContextTokens:
		return nativeLongContextCompactAt
	default:
		return m.MaxContextTokens
	}
}

// Default pricing for unknown models: the Sonnet 4.x rates. The real rate can
// be off in either direction, which is why unknown models carry a warning.
// Claude Code compacts a model it doesn't recognize when the window it
// assumes fills, so the mark sits at the end of the fallback window.
var defaultPricing = ModelPricing{
	InputRate:        3.00,
	OutputRate:       15.00,
	MaxContextTokens: 200000,
	CompactAtTokens:  200000,
}

// SyntheticModel is the model name Claude Code writes on lines it records
// itself rather than receiving from the API, such as an API error. No request
// stands behind them and they carry no tokens, so the name is known and priced
// at zero instead of being treated as an unknown model at the fallback rates.
const SyntheticModel = "<synthetic>"

// syntheticDisplayName drops the brackets so the name fits a 10-column MODEL
// cell without being cut.
const syntheticDisplayName = "synthetic"

// syntheticPricing keeps the fallback context window rather than a zero one,
// so a synthetic line that ever did carry tokens can't divide by zero or
// read as a full window.
var syntheticPricing = ModelPricing{MaxContextTokens: defaultPricing.MaxContextTokens, CompactAtTokens: defaultPricing.CompactAtTokens}

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
	if modelID == SyntheticModel {
		return syntheticPricing
	}
	id, longContext, known := canonicalModelID(modelID)

	pricing := defaultPricing
	if known {
		pricing = modelPricing[id]
	}
	if longContext && pricing.MaxContextTokens < longContextTokens {
		pricing.MaxContextTokens = longContextTokens
	}
	// A model that reaches 1M only through the beta isn't a native 1M model,
	// so Claude Code compacts it when the widened window fills. A native one
	// keeps its earlier point.
	if longContext && pricing.CompactAtTokens < nativeLongContextCompactAt {
		pricing.CompactAtTokens = longContextTokens
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
	if modelID == SyntheticModel {
		return syntheticDisplayName
	}
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

// GetCacheReadRate returns the cache read rate: the row's absolute override
// when set, otherwise the standard 0.1x of the input rate.
func GetCacheReadRate(pricing ModelPricing) float64 {
	if pricing.CacheReadRate > 0 {
		return pricing.CacheReadRate
	}
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

// ContextZone places a context reading against its model's compaction point.
type ContextZone int

const (
	ContextClear ContextZone = iota // short of the warning band
	ContextNear                     // in the warning band, short of the mark
	ContextPast                     // at the mark or past it
)

// warningBandShare is where the warning band starts, as a share of the
// compaction point. It scales with the mark, so a 1M window doesn't warn
// 300K early and a 200K one still gets a couple of turns' notice (30K).
const warningBandShare = 0.85

// ContextZoneOf reports where tokens sit against compactAt, the point
// ModelPricing.CompactAtTokens gives. Everything that colors or words a
// context reading asks this rather than comparing percentages, since the
// mark's share of the window differs from model to model. A reading with no
// mark (compactAt 0) is clear.
func ContextZoneOf(tokens int64, compactAt int) ContextZone {
	switch {
	case compactAt <= 0:
		return ContextClear
	case tokens >= int64(compactAt):
		return ContextPast
	case float64(tokens) >= warningBandShare*float64(compactAt):
		return ContextNear
	default:
		return ContextClear
	}
}

// IsKnownModel returns true if the model ID is recognized
// (i.e., has explicit pricing rather than falling back to defaults)
func IsKnownModel(modelID string) bool {
	if modelID == SyntheticModel {
		return true
	}
	_, _, known := canonicalModelID(modelID)
	return known
}
