package styles

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestGetAgentColor(t *testing.T) {
	tests := []struct {
		agentID  string
		expected lipgloss.Color
	}{
		{"", SecondaryColor},      // Empty returns secondary
		{"1", AgentColors[0]},     // A1 = pink
		{"2", AgentColors[1]},     // A2 = orange
		{"3", AgentColors[2]},     // A3 = yellow
		{"4", AgentColors[3]},     // A4 = blue
		{"5", AgentColors[4]},     // A5 = cyan
		{"6", AgentColors[0]},     // A6 = wraps to pink
		{"10", AgentColors[4]},    // A10 = wraps to cyan
		{"invalid", AgentColors[0]}, // Non-numeric defaults to 1
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
		expected  lipgloss.Color
	}{
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

func TestGetCostGradientColor(t *testing.T) {
	// Test the gradient: 0-50%=neutral, 50-75%=yellow, 75-90%=orange, 90%+=red
	tests := []struct {
		name     string
		cost     float64
		minCost  float64
		maxCost  float64
		expected lipgloss.Color
	}{
		{"minimum cost (0%)", 0.01, 0.01, 0.10, lipgloss.Color("252")},       // Neutral white
		{"low cost (20%)", 0.028, 0.01, 0.10, lipgloss.Color("252")},         // Neutral white (< 50%)
		{"below mid (45%)", 0.0505, 0.01, 0.10, lipgloss.Color("252")},       // Neutral white (< 50%)
		{"mid cost (55%)", 0.0595, 0.01, 0.10, WarningColor},                 // Yellow (50-75%)
		{"medium-high cost (70%)", 0.073, 0.01, 0.10, WarningColor},          // Yellow (50-75%)
		{"high cost (80%)", 0.082, 0.01, 0.10, lipgloss.Color("214")},        // Orange (75-90%)
		{"very high cost (92%)", 0.0928, 0.01, 0.10, ErrorColor},             // Red (>90%)
		{"maximum cost (100%)", 0.10, 0.01, 0.10, ErrorColor},                // Red (>90%)
		{"single value", 0.05, 0.05, 0.05, lipgloss.Color("252")},            // Edge case: equal min/max
		{"negative range", 0.03, 0.05, 0.01, lipgloss.Color("252")},          // Edge case: invalid range
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
