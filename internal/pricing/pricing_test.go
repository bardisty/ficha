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

		// Unknown models - should return false
		{"unknown-model", false},
		{"gpt-4", false},
		{"gemini-pro", false},
		{"claude", false},
		{"claude-2", false},
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
