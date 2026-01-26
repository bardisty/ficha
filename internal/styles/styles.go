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
