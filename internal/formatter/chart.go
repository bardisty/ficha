package formatter

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/sparkline"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// Chart display constants for summary sparkline
const (
	summaryChartHeight = 6 // 6 rows = 24 braille levels (matching watch command)
	summaryChartMax    = 68
	// summaryChartMin is the fewest sessions worth a chart. Below it the
	// table under the chart says everything, and a few columns of bars
	// read as noise.
	summaryChartMin = 5
)

// renderCostChart renders a sparkline of session costs, oldest to newest, one
// column per session. Points are evenly spaced by session, not by time, so
// the axis says so instead of carrying dates that would suggest a timeline.
// The chart is as wide as its points, up to summaryChartMax or what a width
// report leaves after its margins: a wider one would bunch them against its
// right edge.
//
// The sparkline's ring buffer holds exactly chartWidth points, so with more
// sessions than that only the newest chartWidth are drawn. The bars, scale,
// min/max and count must all derive from that same visible tail: pushing the
// full history would let ntcharts' AutoMaxValue pin the scale to an evicted
// off-screen peak (same fix as the watch TUI's visibleCostHistory).
func renderCostChart(costs []float64, width int, noColor bool) string {
	if len(costs) < summaryChartMin {
		return ""
	}

	var sb strings.Builder
	indent := "    " // 4-space indent to match section content
	chartWidth := min(len(costs), summaryChartMax, width-2*len(indent))
	visibleCosts := costs[len(costs)-chartWidth:]

	minCost, maxCost := visibleCosts[0], visibleCosts[0]
	for _, c := range visibleCosts {
		minCost = min(minCost, c)
		maxCost = max(maxCost, c)
	}

	var chart sparkline.Model
	if !noColor {
		chartStyle := lipgloss.NewStyle().Foreground(styles.SuccessColor)
		chart = sparkline.New(chartWidth, summaryChartHeight, sparkline.WithStyle(chartStyle))
	} else {
		chart = sparkline.New(chartWidth, summaryChartHeight)
	}
	chart.PushAll(visibleCosts)

	// Braille has four times the vertical resolution but no ASCII stand-in;
	// the ASCII glyph set draws columns and maps them to ASCII.
	if styles.ASCII() {
		chart.Draw()
	} else {
		chart.DrawBraille()
	}
	chartView := chart.View()
	if styles.ASCII() {
		chartView = styles.ASCIIChart(chartView)
	}
	for _, line := range strings.Split(chartView, "\n") {
		if line != "" {
			sb.WriteString(indent + line + "\n")
		}
	}

	// costs leaves out sessions that cost nothing, so the count says which
	// sessions it covers, beside a table that lists every one.
	count := fmt.Sprintf("%d sessions with a cost", len(visibleCosts))
	if len(visibleCosts) < len(costs) {
		count = fmt.Sprintf("last %d of %d sessions with a cost", len(visibleCosts), len(costs))
	}
	order := fmt.Sprintf("%s, oldest %s newest", count, styles.Arrow)
	if len(indent)+lipgloss.Width(order) > width {
		order = strings.Replace(order, " sessions with a cost", " with a cost", 1)
	}
	labels := []string{
		order,
		fmt.Sprintf("min: %s  max: %s", render.Cost(minCost), render.Cost(maxCost)),
	}
	for _, l := range labels {
		if noColor {
			sb.WriteString(indent + l + "\n")
		} else {
			sb.WriteString(indent + styles.DimStyle.Render(l) + "\n")
		}
	}
	return sb.String()
}
