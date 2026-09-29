package styles

import (
	"math"
	"strconv"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestGetAgentColor(t *testing.T) {
	tests := []struct {
		agentID  string
		expected lipgloss.AdaptiveColor
	}{
		{"", SecondaryColor},   // Empty returns secondary
		{"1", AgentColors[0]},  // A1 = pink
		{"2", AgentColors[1]},  // A2 = orange
		{"3", AgentColors[2]},  // A3 = yellow
		{"4", AgentColors[3]},  // A4 = blue
		{"5", AgentColors[4]},  // A5 = cyan
		{"6", AgentColors[0]},  // A6 = wraps to pink
		{"10", AgentColors[4]}, // A10 = wraps to cyan
		// Non-numeric IDs hash (FNV-1a % len). Expectations are pinned
		// per-ID because with 5 palette entries "distinct IDs get distinct
		// colors" is not an invariant — only stability is.
		{"invalid", AgentColors[0]},
		{"abc123", AgentColors[3]},
		{"g7h8i9j0k1l2", AgentColors[2]}, // the breakdown golden's fixture ID
		// Digit-prefixed hex must take the hash path, not parse as ordinal 3
		// (which would be AgentColors[2])
		{"3f2a1b", AgentColors[0]},
		// Atoi succeeds but ordinals start at 1, so "0" hashes too
		// (ordinal arithmetic on it would index AgentColors[-1])
		{"0", AgentColors[3]},
	}

	for _, tt := range tests {
		t.Run("agent_"+tt.agentID, func(t *testing.T) {
			got := GetAgentColor(tt.agentID)
			if got != tt.expected {
				t.Errorf("GetAgentColor(%q) = %v, want %v", tt.agentID, got, tt.expected)
			}
		})
	}

}

func TestGetModelColor(t *testing.T) {
	tests := []struct {
		modelName string
		expected  lipgloss.AdaptiveColor
	}{
		{"Fable 5", FableColor},
		{"claude-fable-5", FableColor},
		{"Mythos 5", FableColor},
		{"claude-mythos-5", FableColor},
		{"Opus 4.5", OpusColor},
		{"opus 4", OpusColor},
		{"Sonnet 4", SonnetColor},
		{"SONNET 4.5", SonnetColor},
		{"Haiku 4.5", HaikuColor},
		{"haiku 4", HaikuColor},
		{"Unknown", SecondaryColor},
		{"", SecondaryColor},
	}

	for _, tt := range tests {
		t.Run(tt.modelName, func(t *testing.T) {
			got := GetModelColor(tt.modelName)
			if got != tt.expected {
				t.Errorf("GetModelColor(%q) = %v, want %v", tt.modelName, got, tt.expected)
			}
		})
	}
}

func TestGetContextUsageColor(t *testing.T) {
	tests := []struct {
		name     string
		pct      float64
		expected lipgloss.AdaptiveColor
	}{
		{"0% → green", 0, ContextLowColor},
		{"64.9% → green", 64.9, ContextLowColor},
		{"65.0% → orange", 65.0, ContextHighColor},
		{"74.9% → orange", 74.9, ContextHighColor},
		{"75.0% → red", 75.0, ContextCriticalColor},
		{"100% → red", 100.0, ContextCriticalColor},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetContextUsageColor(tt.pct)
			if got != tt.expected {
				t.Errorf("GetContextUsageColor(%v) = %v, want %v", tt.pct, got, tt.expected)
			}
		})
	}
}

func TestGetCostGradientColor(t *testing.T) {
	// Test the gradient: 0-50%=neutral, 50-75%=yellow, 75-90%=orange, 90%+=red
	tests := []struct {
		name     string
		cost     float64
		minCost  float64
		maxCost  float64
		expected lipgloss.AdaptiveColor
	}{
		{"minimum cost (0%)", 0.01, 0.01, 0.10, NeutralColor},       // Neutral white
		{"low cost (20%)", 0.028, 0.01, 0.10, NeutralColor},         // Neutral white (< 50%)
		{"below mid (45%)", 0.0505, 0.01, 0.10, NeutralColor},       // Neutral white (< 50%)
		{"mid cost (55%)", 0.0595, 0.01, 0.10, WarningColor},        // Yellow (50-75%)
		{"medium-high cost (70%)", 0.073, 0.01, 0.10, WarningColor}, // Yellow (50-75%)
		{"high cost (80%)", 0.082, 0.01, 0.10, OrangeColor},         // Orange (75-90%)
		{"very high cost (92%)", 0.0928, 0.01, 0.10, ErrorColor},    // Red (>90%)
		{"maximum cost (100%)", 0.10, 0.01, 0.10, ErrorColor},       // Red (>90%)
		{"single value", 0.05, 0.05, 0.05, NeutralColor},            // Edge case: equal min/max
		{"negative range", 0.03, 0.05, 0.01, NeutralColor},          // Edge case: invalid range
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetCostGradientColor(tt.cost, tt.minCost, tt.maxCost)
			if got != tt.expected {
				t.Errorf("GetCostGradientColor(%v, %v, %v) = %v, want %v",
					tt.cost, tt.minCost, tt.maxCost, got, tt.expected)
			}
		})
	}
}

// The light values must stay readable on a white background: WCAG's 4.5:1
// for text, and 3:1 for bars and separators, which aren't read letter by
// letter.
func TestLightPaletteContrast(t *testing.T) {
	const text, graphic = 4.5, 3.0
	tests := []struct {
		name  string
		color lipgloss.AdaptiveColor
		min   float64
	}{
		{"Primary", PrimaryColor, text},
		{"Secondary", SecondaryColor, text},
		{"Success", SuccessColor, text},
		{"Info", InfoColor, text},
		{"Warning", WarningColor, text},
		{"Accent", AccentColor, text},
		{"Error", ErrorColor, text},
		{"Orange", OrangeColor, text},
		{"Blue", BlueColor, text},
		{"Neutral", NeutralColor, text},
		{"SoftText", SoftTextColor, text},
		{"Note", NoteColor, text},
		{"Highlight", HighlightColor, text},
		{"Fable", FableColor, text},
		{"ContextFree", ContextFreeColor, graphic},
		{"Separator", SeparatorColor, graphic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contrastOnWhite(t, tt.color.Light); got < tt.min {
				t.Errorf("%s light value %s has contrast %.2f on white, want at least %.1f", tt.name, tt.color.Light, got, tt.min)
			}
		})
	}
}

// contrastOnWhite returns the WCAG contrast ratio of an xterm-256 index
// against white.
func contrastOnWhite(t *testing.T, index string) float64 {
	t.Helper()
	n, err := strconv.Atoi(index)
	if err != nil || n < 16 || n > 255 {
		// 0-15 are the theme's own colors, so no ratio holds for them.
		t.Fatalf("light value %q must be an xterm-256 index from 16 to 255", index)
	}
	c := termenv.ConvertToRGB(termenv.ANSI256Color(n))
	linear := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	lum := 0.2126*linear(c.R) + 0.7152*linear(c.G) + 0.0722*linear(c.B)
	return 1.05 / (lum + 0.05)
}
