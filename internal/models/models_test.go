package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTokenUsageAdd(t *testing.T) {
	tests := []struct {
		name     string
		initial  TokenUsage
		other    TokenUsage
		expected TokenUsage
	}{
		{
			name:    "add basic tokens",
			initial: TokenUsage{InputTokens: 100, OutputTokens: 50},
			other:   TokenUsage{InputTokens: 200, OutputTokens: 100},
			expected: TokenUsage{
				InputTokens:  300,
				OutputTokens: 150,
			},
		},
		{
			name:    "add with cache tokens",
			initial: TokenUsage{InputTokens: 100, CacheCreationInputTokens: 50, CacheReadInputTokens: 25},
			other:   TokenUsage{InputTokens: 100, CacheCreationInputTokens: 50, CacheReadInputTokens: 25},
			expected: TokenUsage{
				InputTokens:              200,
				CacheCreationInputTokens: 100,
				CacheReadInputTokens:     50,
			},
		},
		{
			name:    "add with cache creation details",
			initial: TokenUsage{CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 10, Ephemeral1hInputTokens: 20}},
			other:   TokenUsage{CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 30, Ephemeral1hInputTokens: 40}},
			expected: TokenUsage{
				CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 40, Ephemeral1hInputTokens: 60},
			},
		},
		{
			name:     "add nil cache creation to existing",
			initial:  TokenUsage{CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 10}},
			other:    TokenUsage{},
			expected: TokenUsage{CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 10}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.initial.Add(tt.other)
			if tt.initial.InputTokens != tt.expected.InputTokens {
				t.Errorf("InputTokens: got %d, want %d", tt.initial.InputTokens, tt.expected.InputTokens)
			}
			if tt.initial.OutputTokens != tt.expected.OutputTokens {
				t.Errorf("OutputTokens: got %d, want %d", tt.initial.OutputTokens, tt.expected.OutputTokens)
			}
			if tt.initial.CacheCreationInputTokens != tt.expected.CacheCreationInputTokens {
				t.Errorf("CacheCreationInputTokens: got %d, want %d", tt.initial.CacheCreationInputTokens, tt.expected.CacheCreationInputTokens)
			}
			if tt.initial.CacheReadInputTokens != tt.expected.CacheReadInputTokens {
				t.Errorf("CacheReadInputTokens: got %d, want %d", tt.initial.CacheReadInputTokens, tt.expected.CacheReadInputTokens)
			}
			if tt.expected.CacheCreation != nil {
				if tt.initial.CacheCreation == nil {
					t.Error("CacheCreation: expected non-nil")
				} else {
					if tt.initial.CacheCreation.Ephemeral5mInputTokens != tt.expected.CacheCreation.Ephemeral5mInputTokens {
						t.Errorf("CacheCreation.Ephemeral5mInputTokens: got %d, want %d",
							tt.initial.CacheCreation.Ephemeral5mInputTokens, tt.expected.CacheCreation.Ephemeral5mInputTokens)
					}
					if tt.initial.CacheCreation.Ephemeral1hInputTokens != tt.expected.CacheCreation.Ephemeral1hInputTokens {
						t.Errorf("CacheCreation.Ephemeral1hInputTokens: got %d, want %d",
							tt.initial.CacheCreation.Ephemeral1hInputTokens, tt.expected.CacheCreation.Ephemeral1hInputTokens)
					}
				}
			}
		})
	}
}

func TestCostBreakdownAdd(t *testing.T) {
	initial := CostBreakdown{
		InputCost:        1.0,
		OutputCost:       2.0,
		CacheWrite5mCost: 0.5,
		CacheWrite1hCost: 1.0,
		CacheReadCost:    0.1,
		TotalCost:        4.6,
		CacheSavings:     0.5,
	}

	other := CostBreakdown{
		InputCost:        1.0,
		OutputCost:       2.0,
		CacheWrite5mCost: 0.5,
		CacheWrite1hCost: 1.0,
		CacheReadCost:    0.1,
		TotalCost:        4.6,
		CacheSavings:     0.5,
	}

	initial.Add(other)

	if initial.InputCost != 2.0 {
		t.Errorf("InputCost: got %f, want 2.0", initial.InputCost)
	}
	if initial.OutputCost != 4.0 {
		t.Errorf("OutputCost: got %f, want 4.0", initial.OutputCost)
	}
	if initial.TotalCost != 9.2 {
		t.Errorf("TotalCost: got %f, want 9.2", initial.TotalCost)
	}
	if initial.CacheSavings != 1.0 {
		t.Errorf("CacheSavings: got %f, want 1.0", initial.CacheSavings)
	}
}

func TestDurationMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		expected string
	}{
		{"zero", Duration(0), `"0s"`},
		{"seconds", Duration(30 * time.Second), `"30s"`},
		{"minutes", Duration(5 * time.Minute), `"5m0s"`},
		{"hours", Duration(2 * time.Hour), `"2h0m0s"`},
		{"complex", Duration(1*time.Hour + 30*time.Minute + 45*time.Second), `"1h30m45s"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.duration)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			if string(data) != tt.expected {
				t.Errorf("got %s, want %s", string(data), tt.expected)
			}
		})
	}
}

func TestDurationUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Duration
	}{
		{"string format", `"1h30m"`, Duration(1*time.Hour + 30*time.Minute)},
		{"string seconds", `"45s"`, Duration(45 * time.Second)},
		{"numeric nanoseconds", `1000000000`, Duration(time.Second)},
		{"zero string", `"0s"`, Duration(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := json.Unmarshal([]byte(tt.input), &d)
			if err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			if d != tt.expected {
				t.Errorf("got %v, want %v", d, tt.expected)
			}
		})
	}
}

func TestDurationMethod(t *testing.T) {
	d := Duration(5 * time.Minute)
	if d.Duration() != 5*time.Minute {
		t.Errorf("Duration() returned incorrect value")
	}
}

func TestContextWindowSize(t *testing.T) {
	tests := []struct {
		name     string
		usage    TokenUsage
		expected int64
	}{
		{
			name:     "all zero",
			usage:    TokenUsage{},
			expected: 0,
		},
		{
			name: "input only",
			usage: TokenUsage{
				InputTokens: 100,
			},
			expected: 100,
		},
		{
			name: "all token types",
			usage: TokenUsage{
				InputTokens:              18,
				CacheCreationInputTokens: 21900,
				CacheReadInputTokens:     20700,
			},
			expected: 42618,
		},
		{
			name: "typical session message",
			usage: TokenUsage{
				InputTokens:              500,
				OutputTokens:             1000, // Output tokens are NOT included
				CacheCreationInputTokens: 5000,
				CacheReadInputTokens:     30000,
			},
			expected: 35500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.usage.ContextWindowSize()
			if result != tt.expected {
				t.Errorf("ContextWindowSize(): got %d, want %d", result, tt.expected)
			}
		})
	}
}
