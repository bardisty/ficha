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
		{"sonnet 4.6 exact", "claude-sonnet-4-6", 3.00, 15.00},
		{"sonnet 4.6 versioned", "claude-sonnet-4-6-20260101", 3.00, 15.00},
		{"sonnet 4.5 exact", "claude-sonnet-4-5", 3.00, 15.00},
		{"sonnet 4.5 versioned", "claude-sonnet-4-5-20251101", 3.00, 15.00},
		{"sonnet 4 exact", "claude-sonnet-4", 3.00, 15.00},
		{"sonnet 4 versioned", "claude-sonnet-4-20250514", 3.00, 15.00},
		{"sonnet 3.7 exact", "claude-sonnet-3-7", 3.00, 15.00},
		{"sonnet 3.7 versioned", "claude-sonnet-3-7-20250219", 3.00, 15.00},
		{"sonnet 3.7 alt format", "claude-3-7-sonnet", 3.00, 15.00},
		{"sonnet 3.7 alt versioned", "claude-3-7-sonnet-20250219", 3.00, 15.00},
		{"haiku 4.5 exact", "claude-haiku-4-5", 1.00, 5.00},
		{"haiku 4.5 versioned", "claude-haiku-4-5-20250101", 1.00, 5.00},
		{"claude 3.5 sonnet", "claude-3-5-sonnet", 3.00, 15.00},
		{"claude 3.5 sonnet versioned", "claude-3-5-sonnet-20241022", 3.00, 15.00},
		{"claude 3.5 haiku", "claude-3-5-haiku", 0.80, 4.00},
		{"claude 3 opus", "claude-3-opus", 15.00, 75.00},
		{"claude 3 opus versioned", "claude-3-opus-20240229", 15.00, 75.00},
		{"claude 3 sonnet", "claude-3-sonnet", 3.00, 15.00},
		{"claude 3 haiku", "claude-3-haiku", 0.25, 1.25},
		{"unknown model uses default", "unknown-model", 3.00, 15.00},
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
		{"claude-opus-4-10-20260601", "claude-opus-4"},
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

		// Versioned variants that match a broader prefix (F5 boundary guard)
		{"claude-opus-4-10-20260601", true},

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
