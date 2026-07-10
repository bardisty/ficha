package pricing

import (
	"testing"
)

func TestGetModelPricing(t *testing.T) {
	tests := []struct {
		name           string
		modelID        string
		expectedInput  float64
		expectedOutput float64
	}{
		{"fable 5 exact", "claude-fable-5", 10.00, 50.00},
		{"fable 5 versioned", "claude-fable-5-20260301", 10.00, 50.00},
		{"mythos 5 exact", "claude-mythos-5", 10.00, 50.00},
		{"mythos 5 versioned", "claude-mythos-5-20260601", 10.00, 50.00},
		{"sonnet 5 exact", "claude-sonnet-5", 3.00, 15.00},
		{"sonnet 5 versioned", "claude-sonnet-5-20260401", 3.00, 15.00},
		{"opus 4.8 exact", "claude-opus-4-8", 5.00, 25.00},
		{"opus 4.8 versioned", "claude-opus-4-8-20260601", 5.00, 25.00},
		{"opus 4.7 exact", "claude-opus-4-7", 5.00, 25.00},
		{"opus 4.7 versioned", "claude-opus-4-7-20260301", 5.00, 25.00},
		{"opus 4.6 exact", "claude-opus-4-6", 5.00, 25.00},
		{"opus 4.6 versioned", "claude-opus-4-6-20260101", 5.00, 25.00},
		{"opus 4.5 exact", "claude-opus-4-5", 5.00, 25.00},
		{"opus 4.5 versioned", "claude-opus-4-5-20251101", 5.00, 25.00},
		{"opus 4.1 exact", "claude-opus-4-1", 15.00, 75.00},
		{"opus 4.1 versioned", "claude-opus-4-1-20250414", 15.00, 75.00},
		{"opus 4 exact", "claude-opus-4", 15.00, 75.00},
		{"opus 4 versioned", "claude-opus-4-20250514", 15.00, 75.00},
		{"opus 4 alias", "claude-opus-4-0", 15.00, 75.00},
		{"sonnet 4.6 exact", "claude-sonnet-4-6", 3.00, 15.00},
		{"sonnet 4.6 versioned", "claude-sonnet-4-6-20260101", 3.00, 15.00},
		{"sonnet 4.5 exact", "claude-sonnet-4-5", 3.00, 15.00},
		{"sonnet 4.5 versioned", "claude-sonnet-4-5-20251101", 3.00, 15.00},
		{"sonnet 4 exact", "claude-sonnet-4", 3.00, 15.00},
		{"sonnet 4 versioned", "claude-sonnet-4-20250514", 3.00, 15.00},
		{"sonnet 4 alias", "claude-sonnet-4-0", 3.00, 15.00},
		{"sonnet 3.7 exact", "claude-sonnet-3-7", 3.00, 15.00},
		{"sonnet 3.7 versioned", "claude-sonnet-3-7-20250219", 3.00, 15.00},
		{"sonnet 3.7 alt format", "claude-3-7-sonnet", 3.00, 15.00},
		{"sonnet 3.7 alt versioned", "claude-3-7-sonnet-20250219", 3.00, 15.00},
		{"haiku 4.5 exact", "claude-haiku-4-5", 1.00, 5.00},
		{"haiku 4.5 versioned", "claude-haiku-4-5-20250101", 1.00, 5.00},
		{"claude 3.5 sonnet", "claude-3-5-sonnet", 3.00, 15.00},
		{"claude 3.5 sonnet versioned", "claude-3-5-sonnet-20241022", 3.00, 15.00},
		{"claude 3.5 haiku", "claude-3-5-haiku", 0.80, 4.00},
		{"claude 3.5 haiku latest alias", "claude-3-5-haiku-latest", 0.80, 4.00},
		{"claude 3 opus", "claude-3-opus", 15.00, 75.00},
		{"claude 3 opus versioned", "claude-3-opus-20240229", 15.00, 75.00},
		{"claude 3 sonnet", "claude-3-sonnet", 3.00, 15.00},
		{"claude 3 haiku", "claude-3-haiku", 0.25, 1.25},
		{"unknown model uses default", "unknown-model", 3.00, 15.00},
		// Unlisted family versions must NOT inherit a shorter prefix's pricing:
		// claude-opus-4-9 is not Opus 4 ($15/$75) — it falls to default until cataloged
		{"unlisted opus 4.9 uses default", "claude-opus-4-9", 3.00, 15.00},
		{"unlisted opus 4.9 dated uses default", "claude-opus-4-9-20260101", 3.00, 15.00},
		{"unlisted opus 4.10 dated uses default", "claude-opus-4-10-20260601", 3.00, 15.00},
		{"unlisted fable 5.5 uses default", "claude-fable-5-5", 3.00, 15.00},

		// Vertex '@date' IDs. Before the segment-boundary fix these matched the
		// shorter "claude-opus-4" prefix and billed at $15/$75.
		{"vertex opus 4.5", "claude-opus-4-5@20251101", 5.00, 25.00},
		{"vertex opus 4.1", "claude-opus-4-1@20250805", 15.00, 75.00},
		{"vertex opus 4", "claude-opus-4@20250514", 15.00, 75.00},
		{"vertex sonnet 4.5", "claude-sonnet-4-5@20250929", 3.00, 15.00},
		{"vertex haiku 4.5", "claude-haiku-4-5@20251001", 1.00, 5.00},
		{"vertex sonnet 3.5 v2", "claude-3-5-sonnet-v2@20241022", 3.00, 15.00},

		// Bedrock IDs. Before the prefix/suffix strip every one of these fell to
		// the Sonnet default: Opus 4.1 under-reported 5x, Haiku 4.5 over 3x.
		{"bedrock opus 4.5 us profile", "us.anthropic.claude-opus-4-5-20251101-v1:0", 5.00, 25.00},
		{"bedrock opus 4.1 us profile", "us.anthropic.claude-opus-4-1-20250805-v1:0", 15.00, 75.00},
		{"bedrock haiku 4.5 global profile", "global.anthropic.claude-haiku-4-5-20251001-v1:0", 1.00, 5.00},
		{"bedrock sonnet 3.5 no region", "anthropic.claude-3-5-sonnet-20241022-v2:0", 3.00, 15.00},
		{"bedrock foundation model form", "claude-3-5-sonnet-20241022-v2:0", 3.00, 15.00},

		// 1M-context beta marker: base rates, never the shorter prefix's.
		{"1m beta opus 4.8", "claude-opus-4-8[1m]", 5.00, 25.00},
		{"1m beta sonnet 4.5 dated", "claude-sonnet-4-5-20250929[1m]", 3.00, 15.00},

		// Unlisted family versions stay on default through every decoration.
		{"vertex unlisted opus 4.9 uses default", "claude-opus-4-9@20251101", 3.00, 15.00},
		{"bedrock unlisted opus 4.9 uses default", "us.anthropic.claude-opus-4-9-20251101-v1:0", 3.00, 15.00},
		{"bedrock fake model uses default", "anthropic.claude-9-fake-v1:0", 3.00, 15.00},
		{"1m beta unlisted opus 4.9 uses default", "claude-opus-4-9[1m]", 3.00, 15.00},

		// An unrecognized decorator falls to default rather than a shorter prefix.
		{"unknown decorator uses default", "claude-opus-4-5{2m}", 3.00, 15.00},
		{"unknown beta marker uses default", "claude-opus-4-8[2m]", 3.00, 15.00},
		{"letter-leading marker uses default", "claude-opus-4-8[foo]", 3.00, 15.00},
		{"unterminated marker uses default", "claude-opus-4-8[1m", 3.00, 15.00},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pricing := GetModelPricing(tt.modelID)
			if pricing.InputRate != tt.expectedInput {
				t.Errorf("InputRate: got %f, want %f", pricing.InputRate, tt.expectedInput)
			}
			if pricing.OutputRate != tt.expectedOutput {
				t.Errorf("OutputRate: got %f, want %f", pricing.OutputRate, tt.expectedOutput)
			}
		})
	}
}

func TestGetModelDisplayName(t *testing.T) {
	tests := []struct {
		modelID      string
		expectedName string
	}{
		{"claude-fable-5", "Fable 5"},
		{"claude-fable-5-20260301", "Fable 5"},
		{"claude-mythos-5", "Mythos 5"},
		{"claude-mythos-5-20260601", "Mythos 5"},
		{"claude-sonnet-5", "Sonnet 5"},
		{"claude-sonnet-5-20260401", "Sonnet 5"},
		{"claude-opus-4-8", "Opus 4.8"},
		{"claude-opus-4-8-20260601", "Opus 4.8"},
		{"claude-opus-4-7", "Opus 4.7"},
		{"claude-opus-4-7-20260301", "Opus 4.7"},
		{"claude-opus-4-6", "Opus 4.6"},
		{"claude-opus-4-6-20260101", "Opus 4.6"},
		{"claude-opus-4-5", "Opus 4.5"},
		{"claude-opus-4-5-20251101", "Opus 4.5"},
		{"claude-opus-4-1", "Opus 4.1"},
		{"claude-opus-4-1-20250414", "Opus 4.1"},
		{"claude-opus-4", "Opus 4"},
		{"claude-opus-4-20250514", "Opus 4"},
		{"claude-opus-4-0", "Opus 4"},
		{"claude-sonnet-4-0", "Sonnet 4"},
		{"claude-opus-4-9", "claude-opus-4-9"},
		{"claude-sonnet-4-6", "Sonnet 4.6"},
		{"claude-sonnet-4-6-20260101", "Sonnet 4.6"},
		{"claude-sonnet-4-5", "Sonnet 4.5"},
		{"claude-sonnet-4", "Sonnet 4"},
		{"claude-sonnet-4-20250514", "Sonnet 4"},
		{"claude-sonnet-3-7", "Sonnet 3.7"},
		{"claude-sonnet-3-7-20250219", "Sonnet 3.7"},
		{"claude-3-7-sonnet", "Sonnet 3.7"},
		{"claude-3-7-sonnet-20250219", "Sonnet 3.7"},
		{"claude-haiku-4-5", "Haiku 4.5"},
		{"claude-3-5-sonnet", "Sonnet 3.5"},
		{"claude-3-5-haiku", "Haiku 3.5"},
		{"claude-3-opus", "Opus 3"},
		{"claude-3-sonnet", "Sonnet 3"},
		{"claude-3-haiku", "Haiku 3"},
		{"unknown-model", "unknown-model"},

		// Vertex ('@date'), Bedrock ('[region.]anthropic.' + '-vN:M'), and the
		// 1M-context beta marker all resolve to the base model's name.
		{"claude-opus-4-5@20251101", "Opus 4.5"},
		{"claude-sonnet-4-5@20250929", "Sonnet 4.5"},
		{"claude-3-5-sonnet-v2@20241022", "Sonnet 3.5"},
		{"us.anthropic.claude-opus-4-5-20251101-v1:0", "Opus 4.5"},
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", "Sonnet 3.5"},
		{"claude-opus-4-8[1m]", "Opus 4.8"},
		{"claude-sonnet-4-5-20250929[1m]", "Sonnet 4.5"},

		// Unknown models keep their raw ID, decoration and all.
		{"claude-opus-4-9@20251101", "claude-opus-4-9@20251101"},
		{"anthropic.claude-9-fake-v1:0", "anthropic.claude-9-fake-v1:0"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			name := GetModelDisplayName(tt.modelID)
			if name != tt.expectedName {
				t.Errorf("got %s, want %s", name, tt.expectedName)
			}
		})
	}
}

func TestCacheRates(t *testing.T) {
	pricing := ModelPricing{InputRate: 3.00, OutputRate: 15.00}

	// Cache write 5m should be 1.25x input rate
	cw5m := GetCacheWrite5mRate(pricing)
	if cw5m < 3.74 || cw5m > 3.76 {
		t.Errorf("CacheWrite5mRate: got %f, want ~3.75", cw5m)
	}

	// Cache write 1h should be 2x input rate
	cw1h := GetCacheWrite1hRate(pricing)
	if cw1h < 5.99 || cw1h > 6.01 {
		t.Errorf("CacheWrite1hRate: got %f, want ~6.00", cw1h)
	}

	// Cache read should be 0.1x input rate
	cr := GetCacheReadRate(pricing)
	if cr < 0.29 || cr > 0.31 {
		t.Errorf("CacheReadRate: got %f, want ~0.30", cr)
	}
}

func TestNormalizeModelID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-fable-5-20260301", "claude-fable-5"},
		{"claude-fable-5", "claude-fable-5"},
		{"claude-mythos-5-20260601", "claude-mythos-5"},
		{"claude-mythos-5", "claude-mythos-5"},
		{"claude-sonnet-5-20260401", "claude-sonnet-5"},
		{"claude-sonnet-5", "claude-sonnet-5"},
		{"claude-opus-4-8-20260601", "claude-opus-4-8"},
		{"claude-opus-4-8", "claude-opus-4-8"},
		{"claude-opus-4-7-20260301", "claude-opus-4-7"},
		{"claude-opus-4-7", "claude-opus-4-7"},
		{"claude-opus-4-6-20260101", "claude-opus-4-6"},
		{"claude-opus-4-6", "claude-opus-4-6"},
		{"claude-opus-4-5-20251101", "claude-opus-4-5"},
		{"claude-opus-4-5", "claude-opus-4-5"},
		{"claude-opus-4-1-20250414", "claude-opus-4-1"},
		{"claude-opus-4-1", "claude-opus-4-1"},
		{"claude-opus-4-20250514", "claude-opus-4"},
		{"claude-opus-4", "claude-opus-4"},
		{"claude-sonnet-4-6-20260101", "claude-sonnet-4-6"},
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"claude-sonnet-4-20250514", "claude-sonnet-4"},
		{"claude-sonnet-4", "claude-sonnet-4"},
		{"claude-sonnet-3-7-20250219", "claude-sonnet-3-7"},
		{"claude-sonnet-3-7", "claude-sonnet-3-7"},
		{"claude-3-7-sonnet-20250219", "claude-3-7-sonnet"},
		{"claude-3-7-sonnet", "claude-3-7-sonnet"},
		{"claude-3-5-sonnet-20241022", "claude-3-5-sonnet"},
		{"claude-3-5-sonnet-latest", "claude-3-5-sonnet"},
		{"claude-opus-4-0", "claude-opus-4-0"},
		{"claude-sonnet-4-0", "claude-sonnet-4-0"},
		// Short numeric segments are version bumps (different models), not
		// variants of the prefix — they must not normalize to it.
		{"claude-opus-4-9", "claude-opus-4-9"},
		{"claude-opus-4-9-20260101", "claude-opus-4-9-20260101"},
		{"claude-opus-4-10-20260601", "claude-opus-4-10-20260601"},
		{"claude-sonnet-4-9", "claude-sonnet-4-9"},
		{"claude-fable-5-5", "claude-fable-5-5"},
		{"some-other-model", "some-other-model"},

		// Vertex separates the release date with '@'. Treating it as a segment
		// boundary keeps the longest prefix winning; without it "claude-opus-4-5"
		// was rejected and "claude-opus-4" silently inherited (3x mispricing).
		{"claude-opus-4-5@20251101", "claude-opus-4-5"},
		{"claude-sonnet-4-5@20250929", "claude-sonnet-4-5"},
		{"claude-opus-4-1@20250805", "claude-opus-4-1"},
		{"claude-opus-4@20250514", "claude-opus-4"},
		{"claude-haiku-4-5@20251001", "claude-haiku-4-5"},
		{"claude-3-5-sonnet-v2@20241022", "claude-3-5-sonnet"},

		// Bedrock wraps the ID in an inference-profile prefix and a version suffix.
		{"us.anthropic.claude-opus-4-5-20251101-v1:0", "claude-opus-4-5"},
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", "claude-3-5-sonnet"},
		{"eu.anthropic.claude-sonnet-4-5-20250929-v1:0", "claude-sonnet-4-5"},
		{"global.anthropic.claude-haiku-4-5-20251001-v1:0", "claude-haiku-4-5"},
		{"anthropic.claude-opus-4-8-v1:0", "claude-opus-4-8"},
		{"claude-3-5-sonnet-20241022-v2:0", "claude-3-5-sonnet"}, // bare foundation-model form

		// The 1M-context beta marker is stripped before matching. Claude Code
		// emits the bare form ("claude-opus-4-8[1m]"); the dated form is the
		// shape the API documents.
		{"claude-opus-4-8[1m]", "claude-opus-4-8"},
		{"claude-sonnet-4-5[1m]", "claude-sonnet-4-5"},
		{"claude-sonnet-4-5-20250929[1m]", "claude-sonnet-4-5"},

		// The version-segment rule survives every decoration: an unlisted family
		// member never inherits a shorter prefix's pricing.
		{"claude-opus-4-9@20251101", "claude-opus-4-9@20251101"},
		{"claude-opus-4-9[1m]", "claude-opus-4-9[1m]"},
		{"us.anthropic.claude-opus-4-9-20251101-v1:0", "us.anthropic.claude-opus-4-9-20251101-v1:0"},
		{"anthropic.claude-9-fake-v1:0", "anthropic.claude-9-fake-v1:0"},

		// An unrecognized decorator must fail safe (default pricing + warning)
		// rather than fall back to a shorter prefix.
		{"claude-opus-4-5{2m}", "claude-opus-4-5{2m}"},
		{"claude-opus-4-8[2m]", "claude-opus-4-8[2m]"},
		{"claude-opus-4-8[foo]", "claude-opus-4-8[foo]"},
		{"claude-3-5-sonnet[beta]", "claude-3-5-sonnet[beta]"},
		{"claude-opus-4-8[1m", "claude-opus-4-8[1m"}, // unterminated marker
		{"claude-opus-4-123456789", "claude-opus-4-123456789"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeModelID(tt.input)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestIsKnownModel(t *testing.T) {
	tests := []struct {
		modelID  string
		expected bool
	}{
		// All known models - exact matches
		{"claude-fable-5", true},
		{"claude-mythos-5", true},
		{"claude-sonnet-5", true},
		{"claude-opus-4-0", true},
		{"claude-sonnet-4-0", true},
		{"claude-opus-4-8", true},
		{"claude-opus-4-7", true},
		{"claude-opus-4-6", true},
		{"claude-opus-4-5", true},
		{"claude-opus-4-1", true},
		{"claude-opus-4", true},
		{"claude-sonnet-4-6", true},
		{"claude-sonnet-4-5", true},
		{"claude-sonnet-4", true},
		{"claude-sonnet-3-7", true},
		{"claude-3-7-sonnet", true},
		{"claude-haiku-4-5", true},
		{"claude-3-5-sonnet", true},
		{"claude-3-5-haiku", true},
		{"claude-3-opus", true},
		{"claude-3-sonnet", true},
		{"claude-3-haiku", true},

		// All known models - versioned variants
		{"claude-fable-5-20260301", true},
		{"claude-mythos-5-20260601", true},
		{"claude-sonnet-5-20260401", true},
		{"claude-3-5-sonnet-latest", true},
		{"claude-opus-4-8-20260601", true},
		{"claude-opus-4-7-20260301", true},
		{"claude-opus-4-6-20260101", true},
		{"claude-opus-4-5-20251101", true},
		{"claude-opus-4-1-20250414", true},
		{"claude-opus-4-20250514", true},
		{"claude-sonnet-4-6-20260101", true},
		{"claude-sonnet-4-5-20251022", true},
		{"claude-sonnet-4-20250514", true},
		{"claude-sonnet-3-7-20250219", true},
		{"claude-3-7-sonnet-20250219", true},
		{"claude-haiku-4-5-20250101", true},
		{"claude-3-5-sonnet-20241022", true},
		{"claude-3-5-haiku-20241022", true},
		{"claude-3-opus-20240229", true},
		{"claude-3-sonnet-20240229", true},
		{"claude-3-haiku-20240307", true},

		// Unlisted family versions are unknown — a short numeric segment after
		// a catalog prefix is a different model, not a variant of it.
		{"claude-opus-4-9", false},
		{"claude-opus-4-9-20260101", false},
		{"claude-opus-4-10-20260601", false},
		{"claude-sonnet-4-9", false},
		{"claude-fable-5-5", false},

		// Vertex, Bedrock, and 1M-beta forms of catalogued models are known.
		{"claude-opus-4-5@20251101", true},
		{"claude-opus-4@20250514", true},
		{"claude-haiku-4-5@20251001", true},
		{"claude-3-5-sonnet-v2@20241022", true},
		{"us.anthropic.claude-opus-4-5-20251101-v1:0", true},
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", true},
		{"claude-3-5-sonnet-20241022-v2:0", true},
		{"claude-opus-4-8[1m]", true},
		{"claude-sonnet-4-5-20250929[1m]", true},

		// Decorated unlisted family versions stay unknown, so the unknown-model
		// warning still fires instead of billing them at a shorter prefix's rate.
		{"claude-opus-4-9@20251101", false},
		{"claude-opus-4-9[1m]", false},
		{"us.anthropic.claude-opus-4-9-20251101-v1:0", false},
		{"anthropic.claude-9-fake-v1:0", false},
		{"claude-opus-4-5{2m}", false},
		{"claude-opus-4-8[2m]", false},
		{"claude-opus-4-8[foo]", false},
		{"claude-opus-4-8[1m", false},

		// Unknown models - should return false
		{"unknown-model", false},
		{"gpt-4", false},
		{"gemini-pro", false},
		{"claude", false},
		{"claude-2", false},
		{"anthropic.", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := IsKnownModel(tt.modelID)
			if result != tt.expected {
				t.Errorf("IsKnownModel(%q): got %v, want %v", tt.modelID, result, tt.expected)
			}
		})
	}
}

func TestContextWindowFunctions(t *testing.T) {
	pricing := ModelPricing{
		InputRate:        5.00,
		OutputRate:       25.00,
		MaxContextTokens: 200000,
	}

	// Test context percentage
	contextPct := GetContextPercentage(pricing, 100000) // 50% usage
	if contextPct < 49.9 || contextPct > 50.1 {
		t.Errorf("GetContextPercentage: got %f, want 50.0", contextPct)
	}

	// Test free space (200k - 100k usage = 100k)
	freeSpace := GetFreeSpace(pricing, 100000)
	if freeSpace != 100000 {
		t.Errorf("GetFreeSpace: got %d, want 100000", freeSpace)
	}

	// Test free space when usage exceeds max (should return 0, not negative)
	freeSpaceExceeded := GetFreeSpace(pricing, 250000)
	if freeSpaceExceeded != 0 {
		t.Errorf("GetFreeSpace (exceeded): got %d, want 0", freeSpaceExceeded)
	}

	// Test context percentage with zero max context
	zeroPricing := ModelPricing{MaxContextTokens: 0}
	zeroPct := GetContextPercentage(zeroPricing, 100000)
	if zeroPct != 0 {
		t.Errorf("GetContextPercentage (zero max): got %f, want 0", zeroPct)
	}
}

func TestModelCatalogIntegrity(t *testing.T) {
	seen := make(map[string]bool, len(modelCatalog))
	for _, m := range modelCatalog {
		if m.ID == "" {
			t.Error("catalog entry with empty ID")
		}
		if seen[m.ID] {
			t.Errorf("duplicate catalog ID %q", m.ID)
		}
		seen[m.ID] = true
		if m.DisplayName == "" {
			t.Errorf("%s: empty DisplayName", m.ID)
		}
		if m.InputRate <= 0 || m.OutputRate <= 0 {
			t.Errorf("%s: non-positive rates (%f/%f)", m.ID, m.InputRate, m.OutputRate)
		}
		if m.MaxContextTokens <= 0 {
			t.Errorf("%s: non-positive MaxContextTokens (%d)", m.ID, m.MaxContextTokens)
		}
	}

	// Derived structures must mirror the catalog 1:1
	if len(modelPricing) != len(modelCatalog) {
		t.Errorf("modelPricing has %d entries, catalog has %d", len(modelPricing), len(modelCatalog))
	}
	if len(displayNames) != len(modelCatalog) {
		t.Errorf("displayNames has %d entries, catalog has %d", len(displayNames), len(modelCatalog))
	}
	if len(prefixPatterns) != len(modelCatalog) {
		t.Errorf("prefixPatterns has %d entries, catalog has %d", len(prefixPatterns), len(modelCatalog))
	}
	for _, m := range modelCatalog {
		p, ok := modelPricing[m.ID]
		if !ok {
			t.Errorf("%s: missing from modelPricing", m.ID)
			continue
		}
		if p.InputRate != m.InputRate || p.OutputRate != m.OutputRate || p.MaxContextTokens != m.MaxContextTokens {
			t.Errorf("%s: modelPricing %+v does not match catalog %+v", m.ID, p, m)
		}
		if displayNames[m.ID] != m.DisplayName {
			t.Errorf("%s: displayNames %q does not match catalog %q", m.ID, displayNames[m.ID], m.DisplayName)
		}
	}

	// Longest-first ordering is what lets "claude-opus-4-5" win over "claude-opus-4"
	for i := 1; i < len(prefixPatterns); i++ {
		if len(prefixPatterns[i-1]) < len(prefixPatterns[i]) {
			t.Errorf("prefixPatterns not sorted longest-first: %q before %q", prefixPatterns[i-1], prefixPatterns[i])
		}
	}

	// Every catalog ID must resolve to itself through the public API
	for _, m := range modelCatalog {
		if !IsKnownModel(m.ID) {
			t.Errorf("IsKnownModel(%q) = false for catalog entry", m.ID)
		}
		if got := normalizeModelID(m.ID); got != m.ID {
			t.Errorf("normalizeModelID(%q) = %q, want identity", m.ID, got)
		}
	}
}

func TestGetModelPricingContextWindow(t *testing.T) {
	tests := []struct {
		modelID  string
		expected int
	}{
		{"claude-fable-5", 1000000},
		{"claude-mythos-5", 1000000},
		{"claude-sonnet-5", 1000000},
		{"claude-sonnet-5-20260401", 1000000},
		{"claude-opus-4-8", 1000000},
		{"claude-sonnet-4-6", 1000000},
		{"claude-opus-4-5", 200000},
		{"claude-haiku-4-5", 200000},
		{"claude-opus-4-9", 200000}, // unknown -> default
		{"unknown-model", 200000},   // unknown -> default

		// The 1M-context beta widens a 200K base model's window. Without this,
		// `watch` measures a genuine 1M session against 200K and the context bar
		// reads overfull at ~20% of the real window.
		{"claude-sonnet-4-5[1m]", 1000000},
		{"claude-sonnet-4-5-20250929[1m]", 1000000},
		{"claude-opus-4-5@20251101", 200000},
		{"claude-haiku-4-5[1m]", 1000000},

		// Already-1M models are unaffected by the marker.
		{"claude-opus-4-8[1m]", 1000000},
		{"claude-opus-4-8", 1000000},

		// The marker widens the window even when the model itself is unknown.
		{"claude-opus-4-9[1m]", 1000000},

		// Bedrock/Vertex forms inherit their base model's window.
		{"us.anthropic.claude-opus-4-8-20260601-v1:0", 1000000},
		{"us.anthropic.claude-haiku-4-5-20251001-v1:0", 200000},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := GetModelPricing(tt.modelID).MaxContextTokens
			if got != tt.expected {
				t.Errorf("MaxContextTokens: got %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestStripDecorations(t *testing.T) {
	tests := []struct {
		modelID         string
		wantBase        string
		wantLongContext bool
	}{
		// Bedrock inference profiles and the bare foundation-model form
		{"us.anthropic.claude-opus-4-5-20251101-v1:0", "claude-opus-4-5-20251101", false},
		{"eu.anthropic.claude-sonnet-4-5-20250929-v1:0", "claude-sonnet-4-5-20250929", false},
		{"apac.anthropic.claude-haiku-4-5-v1:0", "claude-haiku-4-5", false},
		{"global.anthropic.claude-opus-4-8-v12:34", "claude-opus-4-8", false},
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", "claude-3-5-sonnet-20241022", false},
		{"claude-3-5-sonnet-20241022-v2:0", "claude-3-5-sonnet-20241022", false},

		// 1M-context beta marker
		{"claude-opus-4-8[1m]", "claude-opus-4-8", true},
		{"claude-sonnet-4-5-20250929[1m]", "claude-sonnet-4-5-20250929", true},
		{"us.anthropic.claude-sonnet-4-5-20250929-v1:0[1m]", "claude-sonnet-4-5-20250929", true},

		// Vertex '@date' is handled by segment-boundary matching, not stripping
		{"claude-opus-4-5@20251101", "claude-opus-4-5@20251101", false},

		// Undecorated IDs pass through untouched
		{"claude-opus-4-5", "claude-opus-4-5", false},
		{"", "", false},

		// Near-misses must not be stripped
		{"anthropic-claude-opus-4-5", "anthropic-claude-opus-4-5", false}, // hyphen, not dot
		{"my.own.anthropic.claude-opus-4-5", "my.own.anthropic.claude-opus-4-5", false},
		{"claude-opus-4-5-v1", "claude-opus-4-5-v1", false},               // no ':N'
		{"claude-opus-4-5-vX:0", "claude-opus-4-5-vX:0", false},           // non-numeric version
		{"claude-opus-4-8[1m]-extra", "claude-opus-4-8[1m]-extra", false}, // marker not a suffix
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			base, longContext := stripDecorations(tt.modelID)
			if base != tt.wantBase {
				t.Errorf("base: got %q, want %q", base, tt.wantBase)
			}
			if longContext != tt.wantLongContext {
				t.Errorf("longContext: got %v, want %v", longContext, tt.wantLongContext)
			}
		})
	}
}

// A model ID that resolves to a catalog row must never be priced off a *different*
// row than the one its own base ID names. Inheriting a shorter prefix's rates is
// silent: IsKnownModel reports true, so the unknown-model warning never fires and
// a 3x error goes unreported.
func TestKnownModelsNeverInheritForeignPricing(t *testing.T) {
	decorate := []struct {
		name string
		fn   func(base, date string) string
	}{
		{"plain", func(b, _ string) string { return b }},
		{"dated", func(b, d string) string { return b + "-" + d }},
		{"vertex", func(b, d string) string { return b + "@" + d }},
		{"bedrock", func(b, d string) string { return "us.anthropic." + b + "-" + d + "-v1:0" }},
		{"1m beta", func(b, _ string) string { return b + "[1m]" }},
		{"bedrock 1m beta", func(b, d string) string { return "anthropic." + b + "-" + d + "-v1:0[1m]" }},
	}

	for _, m := range modelCatalog {
		for _, d := range decorate {
			id := d.fn(m.ID, "20250514")
			t.Run(d.name+"/"+m.ID, func(t *testing.T) {
				if !IsKnownModel(id) {
					t.Fatalf("IsKnownModel(%q) = false, want true", id)
				}
				got := GetModelPricing(id)
				if got.InputRate != m.InputRate || got.OutputRate != m.OutputRate {
					t.Errorf("%q priced at $%g/$%g, want catalog rates $%g/$%g",
						id, got.InputRate, got.OutputRate, m.InputRate, m.OutputRate)
				}
				if name := GetModelDisplayName(id); name != m.DisplayName {
					t.Errorf("%q displays as %q, want %q", id, name, m.DisplayName)
				}
			})
		}
	}
}

// The converse invariant: an unlisted family member (a short numeric segment
// after a catalog prefix) must stay unknown under every decoration, so it falls
// to default pricing *and* trips the unknown-model warning rather than silently
// inheriting the shorter prefix's rates.
func TestUnlistedFamilyVersionsNeverResolve(t *testing.T) {
	ids := []string{
		"claude-opus-4-9", "claude-opus-4-9-20260101", "claude-opus-4-9@20260101",
		"claude-opus-4-9[1m]", "us.anthropic.claude-opus-4-9-20260101-v1:0",
		"claude-sonnet-4-9", "claude-sonnet-4-9@20260101", "claude-fable-5-5",
		"claude-opus-4-10-20260601", "anthropic.claude-9-fake-v1:0",
		"claude-opus-4-5{2m}", "claude-opus-4-8[2m]", "claude-opus-4-8[foo]",
		"claude-3-5-sonnet[beta]", "claude-opus-4-8[1m", "claude-opus-4-81m]",
	}

	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			if IsKnownModel(id) {
				t.Errorf("IsKnownModel(%q) = true, want false", id)
			}
			if got := GetModelPricing(id); got.InputRate != defaultPricing.InputRate || got.OutputRate != defaultPricing.OutputRate {
				t.Errorf("%q priced at $%g/$%g, want default $%g/$%g",
					id, got.InputRate, got.OutputRate, defaultPricing.InputRate, defaultPricing.OutputRate)
			}
			if name := GetModelDisplayName(id); name != id {
				t.Errorf("%q displays as %q, want the raw ID", id, name)
			}
		})
	}
}
