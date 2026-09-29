package models

import (
	"encoding/json"
	"strings"
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
		{
			name: "CacheCreation only, no flat field",
			usage: TokenUsage{
				InputTokens:          100,
				CacheReadInputTokens: 200,
				CacheCreation: &CacheCreation{
					Ephemeral5mInputTokens: 300,
					Ephemeral1hInputTokens: 400,
				},
			},
			expected: 100 + 700 + 200, // CC sum (700) > flat (0)
		},
		{
			name: "CacheCreation sum > flat field",
			usage: TokenUsage{
				InputTokens:              100,
				CacheCreationInputTokens: 500,
				CacheReadInputTokens:     200,
				CacheCreation: &CacheCreation{
					Ephemeral5mInputTokens: 400,
					Ephemeral1hInputTokens: 400,
				},
			},
			expected: 100 + 800 + 200, // CC sum (800) > flat (500)
		},
		{
			name: "flat field > CacheCreation sum",
			usage: TokenUsage{
				InputTokens:              100,
				CacheCreationInputTokens: 1000,
				CacheReadInputTokens:     200,
				CacheCreation: &CacheCreation{
					Ephemeral5mInputTokens: 300,
					Ephemeral1hInputTokens: 400,
				},
			},
			expected: 100 + 1000 + 200, // flat (1000) > CC sum (700)
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

func TestTrendDirectionJSONUsesNames(t *testing.T) {
	for _, d := range []TrendDirection{TrendStable, TrendIncreasing, TrendDecreasing} {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if want := `"` + d.String() + `"`; string(b) != want {
			t.Errorf("%v marshals to %s, want %s", d, b, want)
		}
		var back TrendDirection
		if err := json.Unmarshal(b, &back); err != nil || back != d {
			t.Errorf("%s unmarshals to %v (err %v), want %v", b, back, err, d)
		}
	}
	var d TrendDirection
	if err := json.Unmarshal([]byte(`"sideways"`), &d); err == nil {
		t.Error("an unknown trend name should fail to unmarshal")
	}
}

// Below MinMessagesForTrend no trend is computed, and a zero-valued
// cost_trend would read as "stable". A computed stable trend, which is also
// the zero value, must still be written.
func TestInsightsJSONOmitsUncomputedTrend(t *testing.T) {
	keys := func(i MessageInsights) map[string]any {
		t.Helper()
		b, err := json.Marshal(&i)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	short := keys(MessageInsights{MessageCount: MinMessagesForTrend - 1, AverageCost: 0.5})
	for _, k := range []string{"cost_trend", "recent_avg_cost", "trend_window"} {
		if _, ok := short[k]; ok {
			t.Errorf("%d messages: %s should be absent, got %v", MinMessagesForTrend-1, k, short[k])
		}
	}
	if short["average_cost"] != 0.5 || short["message_count"] != float64(MinMessagesForTrend-1) {
		t.Errorf("the other insights should stay: %v", short)
	}

	stable := keys(MessageInsights{MessageCount: MinMessagesForTrend, CostTrend: TrendStable, TrendWindow: 3})
	if stable["cost_trend"] != "stable" || stable["trend_window"] != float64(3) {
		t.Errorf("a computed stable trend should be written: %v", stable)
	}
	if v, ok := stable["recent_avg_cost"]; !ok || v != float64(0) {
		t.Errorf("recent_avg_cost should be written with a trend, even at 0: %v", stable)
	}
}

// Every time json writes is UTC at whole seconds with a Z, whatever zone and
// precision the value carries inside the program.
func TestJSONTimesAreUTCSeconds(t *testing.T) {
	ny := time.FixedZone("EDT", -4*3600)
	local := time.Date(2026, 9, 29, 13, 18, 43, 123456789, ny)
	const want = "2026-09-29T17:18:43Z"

	for name, v := range map[string]any{
		"session":  SessionAnalysis{StartTime: local, EndTime: local, Window: &TimeWindow{Since: local, Until: local}},
		"agent":    AgentAnalysis{StartTime: local, EndTime: local},
		"message":  MessageAnalysis{Timestamp: local},
		"snapshot": MessageSnapshot{Timestamp: local},
		"insights": MessageInsights{FirstMessage: &MessageSnapshot{Timestamp: local}},
		"global":   GlobalAnalysis{FirstActive: local, LastActive: local, Projects: []ProjectAnalysis{{FirstActive: local, LastActive: local}}},
	} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var times int
		var walk func(any)
		walk = func(v any) {
			switch v := v.(type) {
			case map[string]any:
				for _, child := range v {
					walk(child)
				}
			case []any:
				for _, child := range v {
					walk(child)
				}
			case string:
				if strings.HasPrefix(v, "2026-") || strings.HasPrefix(v, "0001-") {
					times++
					if v != want && v != "0001-01-01T00:00:00Z" {
						t.Errorf("%s: time written as %q, want %q", name, v, want)
					}
				}
			}
		}
		var decoded any
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatal(err)
		}
		walk(decoded)
		if times == 0 {
			t.Errorf("%s: no times in %s", name, b)
		}
	}

	// The internal value keeps its zone and nanoseconds.
	a := AgentAnalysis{StartTime: local}
	if _, err := json.Marshal(a); err != nil || !a.StartTime.Equal(local) || a.StartTime.Location() != ny {
		t.Errorf("marshaling changed the value: %v", a.StartTime)
	}

	// A window leaves an open bound out, and decodes back.
	b, err := json.Marshal(TimeWindow{Since: local})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"since":"`+want+`"}` {
		t.Errorf("window = %s", b)
	}
	var w TimeWindow
	if err := json.Unmarshal(b, &w); err != nil || !w.Since.Equal(local.Truncate(time.Second)) || !w.Until.IsZero() {
		t.Errorf("window decodes to %+v (err %v)", w, err)
	}
}
