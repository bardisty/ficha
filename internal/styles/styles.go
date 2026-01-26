package styles

import "github.com/charmbracelet/lipgloss"

// Shared color palette
var (
	PrimaryColor   = lipgloss.Color("99")  // Purple
	SecondaryColor = lipgloss.Color("240") // Gray
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
	CostLowColor    = lipgloss.Color("240") // Dimmed - low proportion
)

// Context usage level colors
var (
	ContextLowColor      = SuccessColor          // Green - 0-50%
	ContextMediumColor   = WarningColor          // Yellow - 50-75%
	ContextHighColor     = lipgloss.Color("214") // Orange - 75-90%
	ContextCriticalColor = ErrorColor            // Red - 90%+
	ContextFreeColor     = lipgloss.Color("252") // Bright gray - clearly visible free space
	ContextBufferColor   = lipgloss.Color("236") // Dark gray - reserved buffer (distinct from free)
)

// GetContextUsageColor returns the appropriate color based on context usage percentage
func GetContextUsageColor(usagePct float64) lipgloss.Color {
	switch {
	case usagePct >= 90:
		return ContextCriticalColor
	case usagePct >= 75:
		return ContextHighColor
	case usagePct >= 50:
		return ContextMediumColor
	default:
		return ContextLowColor
	}
}
