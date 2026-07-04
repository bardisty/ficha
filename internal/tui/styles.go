package tui

import "github.com/bardisty/ficha/internal/styles"

// Local aliases for frequently used styles
var (
	headerStyle        = styles.HeaderStyle
	heroCostStyle      = styles.HeroCostStyle
	tableBorderStyle   = styles.BorderStyle
	savingsLabelStyle  = styles.SavingsLabelStyle
	footerStyle        = styles.FooterStyle // No margin, just color
	liveIndicatorStyle = styles.LiveIndicatorStyle
	spinnerStyle       = styles.SpinnerStyle

	// Highlight styles for changed values
	highlightStyle = styles.HighlightStyle

	// Dim style for extra decimal precision
	dimStyle = styles.DimStyle

	// Section header style for bracketed section headers
	sectionHeaderStyle = styles.SectionHeaderStyle

	// Panel border style for header panel
	panelBorderStyle = styles.PanelBorderStyle
)
