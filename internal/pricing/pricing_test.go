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
		{"opus 4.5 exact", "claude-opus-4-5", 5.00, 25.00},
		{"opus 4.5 versioned", "claude-opus-4-5-20251101", 5.00, 25.00},
		{"opus 4.1 exact", "claude-opus-4-1", 15.00, 75.00},
		{"opus 4.1 versioned", "claude-opus-4-1-20250414", 15.00, 75.00},
		{"opus 4 exact", "claude-opus-4", 15.00, 75.00},
		{"opus 4 versioned", "claude-opus-4-20250514", 15.00, 75.00},
		{"sonnet 4.5 exact", "claude-sonnet-4-5", 3.00, 15.00},
		{"sonnet 4.5 versioned", "claude-sonnet-4-5-20251101", 3.00, 15.00},
		{"sonnet 4 exact", "claude-sonnet-4", 3.00, 15.00},
		{"sonnet 4 versioned", "claude-sonnet-4-20250514", 3.00, 15.00},
		{"sonnet 3.7 exact", "claude-sonnet-3-7", 3.00, 15.00},
		{"sonnet 3.7 versioned", "claude-sonnet-3-7-20250219", 3.00, 15.00},
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
		{"claude-opus-4-5", "Opus 4.5"},
		{"claude-opus-4-5-20251101", "Opus 4.5"},
		{"claude-opus-4-1", "Opus 4.1"},
		{"claude-opus-4-1-20250414", "Opus 4.1"},
		{"claude-opus-4", "Opus 4"},
		{"claude-opus-4-20250514", "Opus 4"},
		{"claude-sonnet-4-5", "Sonnet 4.5"},
		{"claude-sonnet-4", "Sonnet 4"},
		{"claude-sonnet-4-20250514", "Sonnet 4"},
		{"claude-sonnet-3-7", "Sonnet 3.7"},
		{"claude-sonnet-3-7-20250219", "Sonnet 3.7"},
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
		{"claude-opus-4-5-20251101", "claude-opus-4-5"},
		{"claude-opus-4-5", "claude-opus-4-5"},
		{"claude-opus-4-1-20250414", "claude-opus-4-1"},
		{"claude-opus-4-1", "claude-opus-4-1"},
		{"claude-opus-4-20250514", "claude-opus-4"},
		{"claude-opus-4", "claude-opus-4"},
		{"claude-sonnet-4-20250514", "claude-sonnet-4"},
		{"claude-sonnet-4", "claude-sonnet-4"},
		{"claude-sonnet-3-7-20250219", "claude-sonnet-3-7"},
		{"claude-sonnet-3-7", "claude-sonnet-3-7"},
		{"claude-3-5-sonnet-20241022", "claude-3-5-sonnet"},
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

func TestContextWindowFunctions(t *testing.T) {
	pricing := ModelPricing{
		InputRate:        5.00,
		OutputRate:       25.00,
		MaxContextTokens: 200000,
	}

	// Test autocompact buffer (22.5% of 200k = 45k)
	autocompact := GetAutocompactBuffer(pricing)
	if autocompact != 45000 {
		t.Errorf("GetAutocompactBuffer: got %d, want 45000", autocompact)
	}

	// Test context percentage
	contextPct := GetContextPercentage(pricing, 100000) // 50% usage
	if contextPct < 49.9 || contextPct > 50.1 {
		t.Errorf("GetContextPercentage: got %f, want 50.0", contextPct)
	}

	// Test free space (200k - 100k usage - 45k buffer = 55k)
	freeSpace := GetFreeSpace(pricing, 100000)
	if freeSpace != 55000 {
		t.Errorf("GetFreeSpace: got %d, want 55000", freeSpace)
	}

	// Test free space when usage exceeds available (should return 0, not negative)
	freeSpaceExceeded := GetFreeSpace(pricing, 180000) // 180k usage, only 155k available
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
