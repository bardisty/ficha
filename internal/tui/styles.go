package tui

import "github.com/bah/ccusage/internal/styles"

// Local aliases for frequently used styles
var (
	titleStyle         = styles.TitleStyle
	headerStyle        = styles.HeaderStyle
	tableBorderStyle   = styles.BorderStyle
	labelStyle         = styles.LabelStyle
	valueStyle         = styles.ValueStyle
	totalLabelStyle    = styles.TotalLabelStyle
	totalValueStyle    = styles.TotalValueStyle
	savingsLabelStyle  = styles.SavingsLabelStyle
	savingsValueStyle  = styles.SavingsValueStyle
	footerStyle        = styles.FooterStyle // No margin, just color
	statusBarStyle     = styles.StatusBarStyle
	liveIndicatorStyle = styles.LiveIndicatorStyle
	helpStyle          = styles.HelpStyle
	spinnerStyle       = styles.SpinnerStyle

	// Highlight styles for changed values
	highlightStyle        = styles.HighlightStyle
	highlightTotalStyle   = styles.HighlightTotalStyle
	highlightSavingsStyle = styles.HighlightSavingsStyle

	// Dim style for extra decimal precision
	dimStyle = styles.DimStyle
)
