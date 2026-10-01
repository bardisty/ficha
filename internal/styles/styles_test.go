package styles

import (
	"image/color"
	"math"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/muesli/termenv"
)

func TestGetAgentColor(t *testing.T) {
	tests := []struct {
		agentID  string
		expected color.Color
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
		expected  color.Color
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
		expected color.Color
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
		expected color.Color
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
		color pair
		min   float64
	}{
		{"Primary", palette.primary, text},
		{"Secondary", palette.secondary, text},
		{"Success", palette.success, text},
		{"Info", palette.info, text},
		{"Warning", palette.warning, text},
		{"Accent", palette.accent, text},
		{"Error", palette.err, text},
		{"Orange", palette.orange, text},
		{"Blue", palette.blue, text},
		{"Neutral", palette.neutral, text},
		{"SoftText", palette.softText, text},
		{"Note", palette.note, text},
		{"Highlight", palette.highlight, text},
		{"Fable", palette.fable, text},
		{"AgentOlive", palette.agentOlive, text},
		{"ContextFree", palette.contextFree, graphic},
		{"Separator", palette.separator, graphic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contrastOnWhite(t, tt.color.light); got < tt.min {
				t.Errorf("%s light value %d has contrast %.2f on white, want at least %.1f", tt.name, tt.color.light, got, tt.min)
			}
		})
	}
}

// SetDark swaps every color, the ones built from another included, and the
// styles drawn with them.
func TestSetDarkRebuildsThePalette(t *testing.T) {
	t.Cleanup(func() { SetDark(true) })
	for _, dark := range []bool{false, true} {
		SetDark(dark)
		if Dark() != dark {
			t.Errorf("Dark() = %v after SetDark(%v)", Dark(), dark)
		}
		want := lipgloss.Color("92")
		if dark {
			want = lipgloss.Color("99")
		}
		for name, got := range map[string]color.Color{
			"PrimaryColor": PrimaryColor,
			"OpusColor":    OpusColor,
			"SpinnerStyle": SpinnerStyle.GetForeground(),
		} {
			if got != want {
				t.Errorf("dark=%v: %s is %v, want %v", dark, name, got, want)
			}
		}
		olive := lipgloss.Color("58")
		if dark {
			olive = lipgloss.Color("221")
		}
		if got := AgentColors[2]; got != olive {
			t.Errorf("dark=%v: AgentColors[2] is %v, want %v", dark, got, olive)
		}
	}
}

// On a 16-color terminal each color is the nearest basic one. lipgloss's own
// table would turn the yellows, the pink and the cyan into reds and greens.
func TestSetBasicKeepsEachColorsHue(t *testing.T) {
	SetBasic(true)
	t.Cleanup(func() { SetBasic(false) })
	for name, c := range map[string]struct {
		got  color.Color
		want string
	}{
		"Warning (bright yellow)":   {WarningColor, "11"},
		"Highlight (bright yellow)": {HighlightColor, "11"},
		"Orange (bright yellow)":    {OrangeColor, "11"},
		"Accent (bright magenta)":   {AccentColor, "13"},
		"Info (bright cyan)":        {InfoColor, "14"},
		"Success (bright green)":    {SuccessColor, "10"},
		"Error (bright red)":        {ErrorColor, "9"},
		"HeroCostStyle":             {HeroCostStyle.GetForeground(), "10"},
	} {
		if c.got != lipgloss.Color(c.want) {
			t.Errorf("%s is %v, want basic color %s", name, c.got, c.want)
		}
	}
}

// contrastOnWhite returns the WCAG contrast ratio of an xterm-256 index
// against white.
func contrastOnWhite(t *testing.T, n uint8) float64 {
	t.Helper()
	if n < 16 {
		// 0-15 are the theme's own colors, so no ratio holds for them.
		t.Fatalf("light value %d must be an xterm-256 index from 16 to 255", n)
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
