package formatter

import (
	"fmt"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
)

// Chart display constants for summary sparkline
const (
	summaryChartHeight = 6 // 6 rows = 24 braille levels (matching watch command)
)

// renderCostChart renders a sparkline chart showing session costs over time
// Uses ntcharts braille rendering for high-resolution visualization (24 vertical levels)
//
// The sparkline's ring buffer holds exactly chartWidth points, so with more
// sessions than that only the newest chartWidth are drawn. The bars, scale,
// min/max, count, and date labels must all derive from that same visible tail
// — pushing the full history would let ntcharts' AutoMaxValue pin the scale to
// an evicted off-screen peak, and full-dataset labels would describe sessions
// no bar represents (same fix as the watch TUI's visibleCostHistory).
func renderCostChart(costs []float64, dates []time.Time, width int, noColor bool) string {
	if len(costs) == 0 {
		return ""
	}

	var sb strings.Builder

	// Calculate chart width (leave room for indent and some padding)
	indent := "    " // 4-space indent to match section content
	chartWidth := width - 8
	if chartWidth < 20 {
		chartWidth = 20
	}
	if chartWidth > 68 {
		chartWidth = 68 // Cap at reasonable width
	}

	// Window costs/dates to what the chart can actually draw
	visibleCosts := costs
	visibleDates := dates
	if len(costs) > chartWidth {
		visibleCosts = costs[len(costs)-chartWidth:]
		if len(dates) > chartWidth {
			visibleDates = dates[len(dates)-chartWidth:]
		}
	}

	// Find min/max for labels over the drawn window only
	minCost := visibleCosts[0]
	maxCost := visibleCosts[0]
	for _, c := range visibleCosts {
		if c < minCost {
			minCost = c
		}
		if c > maxCost {
			maxCost = c
		}
	}

	// Create sparkline chart with appropriate styling
	var chart sparkline.Model
	if !noColor {
		chartStyle := lipgloss.NewStyle().Foreground(styles.SuccessColor)
		chart = sparkline.New(chartWidth, summaryChartHeight, sparkline.WithStyle(chartStyle))
	} else {
		chart = sparkline.New(chartWidth, summaryChartHeight)
	}

	// Push only the visible window
	chart.PushAll(visibleCosts)

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

	// Count label: plain when every session is drawn, shown/total when truncated
	var countInfo string
	if len(visibleCosts) < len(costs) {
		countInfo = fmt.Sprintf("(last %d of %d sessions)", len(visibleCosts), len(costs))
	} else {
		countInfo = fmt.Sprintf("(%d sessions)", len(visibleCosts))
	}

	// Add scale labels below the chart
	scaleInfo := fmt.Sprintf("min: %s  max: %s  %s",
		render.Cost(minCost), render.Cost(maxCost), countInfo)
	if noColor {
		sb.WriteString(indent + scaleInfo + "\n")
	} else {
		sb.WriteString(indent + dimStyle.Render(scaleInfo) + "\n")
	}

	// Add date labels spanning the drawn window, not the full dataset.
	// "JAN" is not a Go layout token — it would render literally for every
	// month — so format with "Jan" and uppercase (same idiom as the
	// breakdown table's MODIFIED column).
	if len(visibleDates) >= 2 {
		startDate := strings.ToUpper(visibleDates[0].Format("02 Jan"))
		endDate := strings.ToUpper(visibleDates[len(visibleDates)-1].Format("02 Jan"))

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
