package formatter

import (
	"fmt"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
)

// Chart display constants for summary sparkline
const (
	summaryChartHeight = 6 // 6 rows = 24 braille levels (matching watch command)
)

// renderCostChart renders a sparkline chart showing session costs over time
// Uses ntcharts braille rendering for high-resolution visualization (24 vertical levels)
func renderCostChart(costs []float64, dates []time.Time, width int, noColor bool) string {
	if len(costs) == 0 {
		return ""
	}

	var sb strings.Builder

	// Find min/max for labels
	var minCost, maxCost float64
	minCost = costs[0]
	maxCost = costs[0]
	for _, c := range costs {
		if c < minCost {
			minCost = c
		}
		if c > maxCost {
			maxCost = c
		}
	}

	// Calculate chart width (leave room for indent and some padding)
	indent := "    " // 4-space indent to match section content
	chartWidth := width - 8
	if chartWidth < 20 {
		chartWidth = 20
	}
	if chartWidth > 68 {
		chartWidth = 68 // Cap at reasonable width
	}

	// Create sparkline chart with appropriate styling
	var chart sparkline.Model
	if !noColor {
		chartStyle := lipgloss.NewStyle().Foreground(styles.SuccessColor)
		chart = sparkline.New(chartWidth, summaryChartHeight, sparkline.WithStyle(chartStyle))
	} else {
		chart = sparkline.New(chartWidth, summaryChartHeight)
	}

	// Push all cost data
	chart.PushAll(costs)

	// Render with appropriate mode
	if !noColor {
		chart.DrawBraille()
	} else {
		chart.Draw()
	}

	// Render the chart with proper indentation
	chartLines := strings.Split(chart.View(), "\n")
	for _, line := range chartLines {
		if line != "" {
			sb.WriteString(indent + line + "\n")
		}
	}

	// Add scale labels below the chart
	scaleInfo := fmt.Sprintf("min: %s  max: %s  (%d sessions)",
		formatChartCost(minCost), formatChartCost(maxCost), len(costs))
	if noColor {
		sb.WriteString(indent + scaleInfo + "\n")
	} else {
		sb.WriteString(indent + dimStyle.Render(scaleInfo) + "\n")
	}

	// Add date labels
	if len(dates) >= 2 {
		startDate := dates[0].Format("02 JAN")
		endDate := dates[len(dates)-1].Format("02 JAN")

		// Position dates at start and end of chart area
		padding := chartWidth - len(startDate) - len(endDate)
		if padding < 1 {
			padding = 1
		}
		dateLine := fmt.Sprintf("%s%s%s", startDate, strings.Repeat(" ", padding), endDate)
		if noColor {
			sb.WriteString(indent + dateLine + "\n")
		} else {
			sb.WriteString(indent + dimStyle.Render(dateLine) + "\n")
		}
	}

	return sb.String()
}

// formatChartCost formats a cost value compactly for chart Y-axis labels
func formatChartCost(cost float64) string {
	if cost >= 100 {
		return fmt.Sprintf("$%.0f", cost)
	} else if cost >= 10 {
		return fmt.Sprintf("$%.1f", cost)
	} else if cost >= 1 {
		return fmt.Sprintf("$%.2f", cost)
	}
	return fmt.Sprintf("$%.3f", cost)
}
