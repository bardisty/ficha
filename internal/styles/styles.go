package styles

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Shared color palette
var (
	PrimaryColor   = lipgloss.Color("99")  // Purple
	SecondaryColor = lipgloss.Color("245") // Gray (dimmed but readable)
	SuccessColor   = lipgloss.Color("42")  // Green
	InfoColor      = lipgloss.Color("43")  // Cyan
	WarningColor   = lipgloss.Color("221") // Yellow
	AccentColor    = lipgloss.Color("212") // Pink
	ErrorColor     = lipgloss.Color("196") // Red
)

// Header styles for titles and table headers
var (
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(PrimaryColor)

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(AccentColor).
			MarginBottom(1)
)

// Table styles for borders and cells
// Note: Width/Padding removed - use fmt.Sprintf for consistent formatting
var (
	BorderStyle = lipgloss.NewStyle().
			Foreground(SecondaryColor)

	LabelStyle = lipgloss.NewStyle()

	ValueStyle = lipgloss.NewStyle()
)

// Total row styles for summary lines
var (
	TotalLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(SuccessColor)

	TotalValueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(SuccessColor)
)

// Savings styles for cache savings display
var (
	SavingsLabelStyle = lipgloss.NewStyle().
				Foreground(InfoColor)

	SavingsValueStyle = lipgloss.NewStyle().
				Foreground(InfoColor)
)

// Status and footer styles
var (
	FooterStyle = lipgloss.NewStyle().
			Foreground(SecondaryColor)

	StatusBarStyle = lipgloss.NewStyle().
			Foreground(SecondaryColor).
			MarginTop(1)
)

// Live mode styles
var (
	LiveIndicatorStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ErrorColor).
				Background(lipgloss.Color("52")). // Dark red bg
				Padding(0, 1)
)

// Help and spinner styles
var (
	HelpStyle = lipgloss.NewStyle().
			Foreground(SecondaryColor).
			MarginTop(1)

	SpinnerStyle = lipgloss.NewStyle().
			Foreground(PrimaryColor)
)

// Session ID style
var SessionIDStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(AccentColor)

// Highlight style for recently changed values
var (
	HighlightColor = lipgloss.Color("220") // Bright yellow/gold

	HighlightStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(HighlightColor)

	HighlightTotalStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(HighlightColor)

	HighlightSavingsStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(HighlightColor)
)

// Dim style for extra decimal precision in costs
var DimStyle = lipgloss.NewStyle().
	Foreground(SecondaryColor)

// Breakdown view styles
var (
	// BreakdownNewRowStyle highlights newly added rows in the breakdown view
	BreakdownNewRowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(HighlightColor)
)

// Model-specific colors (by tier)
var (
	OpusColor   = lipgloss.Color("99")  // Purple - premium tier
	SonnetColor = lipgloss.Color("75")  // Blue - mid tier
	HaikuColor  = lipgloss.Color("43")  // Cyan/Teal - lightweight tier
)

// Token type colors
var (
	OutputTokenColor     = lipgloss.Color("75")  // Light blue
	CacheWriteTokenColor = lipgloss.Color("214") // Warm orange - cost investment
	CacheReadTokenColor  = lipgloss.Color("43")  // Cyan - efficiency/savings
)

// Cost magnitude colors (for relative cost shading)
var (
	CostHighColor   = lipgloss.Color("255") // Bright white - high proportion
	CostMediumColor = lipgloss.Color("250") // Normal white - medium proportion
	CostLowColor    = lipgloss.Color("245") // Dimmed - low proportion
)

// Context usage level colors (thresholds based on ~75-78% compaction trigger)
var (
	ContextLowColor      = SuccessColor          // Green - 0-65%
	ContextHighColor     = lipgloss.Color("214") // Orange - 65-75% (approaching compaction)
	ContextCriticalColor = ErrorColor            // Red - 75%+ (compaction territory)
	ContextFreeColor     = lipgloss.Color("252") // Bright gray - clearly visible free space
	ContextBufferColor   = lipgloss.Color("236") // Dark gray - reserved buffer (distinct from free)
)

// GetContextUsageColor returns the appropriate color based on context usage percentage.
// Thresholds aligned with Claude Code's ~75-78% auto-compaction trigger.
func GetContextUsageColor(usagePct float64) lipgloss.Color {
	switch {
	case usagePct >= 75:
		return ContextCriticalColor // Red - compaction territory
	case usagePct >= 65:
		return ContextHighColor // Orange - approaching compaction
	default:
		return ContextLowColor // Green - comfortable
	}
}

// Agent marker colors - cycling palette for distinguishing sub-agents
var AgentColors = []lipgloss.Color{
	lipgloss.Color("212"), // Pink - A1
	lipgloss.Color("214"), // Orange - A2
	lipgloss.Color("221"), // Yellow - A3
	lipgloss.Color("75"),  // Blue - A4
	lipgloss.Color("43"),  // Cyan - A5
}

// GetAgentColor returns a color for the given agent ID (cycles through palette)
func GetAgentColor(agentID string) lipgloss.Color {
	if agentID == "" {
		return SecondaryColor
	}
	// Parse agent number from ID (e.g., "1", "2", "3")
	var num int
	_, _ = fmt.Sscanf(agentID, "%d", &num)
	if num < 1 {
		num = 1
	}
	// Cycle through the palette (0-indexed)
	return AgentColors[(num-1)%len(AgentColors)]
}

// GetModelColor returns the tier-appropriate color for a model display name
func GetModelColor(modelName string) lipgloss.Color {
	// Check for tier keywords in the model name
	switch {
	case contains(modelName, "Opus"):
		return OpusColor
	case contains(modelName, "Sonnet"):
		return SonnetColor
	case contains(modelName, "Haiku"):
		return HaikuColor
	default:
		return SecondaryColor
	}
}

// contains checks if s contains substr (case-insensitive)
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// GetCostGradientColor returns a color based on cost position in the session's range
// Neutral (cheap) -> Yellow -> Orange -> Red (expensive)
// Only expensive items "heat up" - cheap items stay unobtrusive
func GetCostGradientColor(cost, minCost, maxCost float64) lipgloss.Color {
	// Handle edge cases
	if maxCost <= minCost {
		return lipgloss.Color("252") // Single value - neutral white
	}

	// Normalize to 0.0-1.0 range
	normalized := (cost - minCost) / (maxCost - minCost)

	// Clamp to valid range
	if normalized < 0 {
		normalized = 0
	}
	if normalized > 1 {
		normalized = 1
	}

	// Neutral -> Warm gradient (only expensive items draw attention)
	// 0-50%: neutral white (blends in)
	// 50-75%: yellow (starting to warm up)
	// 75-90%: orange (getting hot)
	// 90%+: red (expensive!)
	if normalized < 0.5 {
		return lipgloss.Color("252") // Neutral white - cheap, unobtrusive
	}
	if normalized < 0.75 {
		return WarningColor // Yellow (221)
	}
	if normalized < 0.9 {
		return lipgloss.Color("214") // Orange
	}
	return ErrorColor // Red (196)
}
