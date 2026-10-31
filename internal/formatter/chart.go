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

// chartPoint is one session on the summary chart: its cost, and the row
// number and short ID the table under the chart gives it, so the caption
// can point into the table.
type chartPoint struct {
	cost float64
	row  int
	id   string
}

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
// off-screen peak.
//
// The table numbers its rows newest first, or costliest first, while the
// chart runs oldest first, so the caption names the max and min sessions by
// row and ID rather than leave the reader to hunt for the amounts. A tie
// names the newer session, which the cost order also puts first.
func renderCostChart(points []chartPoint, width int, noColor bool) string {
	if len(points) < summaryChartMin {
		return ""
	}

	var sb strings.Builder
	indent := "    " // 4-space indent to match section content
	chartWidth := min(len(points), summaryChartMax, width-2*len(indent))
	visible := points[len(points)-chartWidth:]

	costs := make([]float64, len(visible))
	lo, hi := visible[0], visible[0]
	for i, p := range visible {
		costs[i] = p.cost
		if p.cost <= lo.cost {
			lo = p
		}
		if p.cost >= hi.cost {
			hi = p
		}
	}

	var chart sparkline.Model
	if !noColor {
		chartStyle := lipgloss.NewStyle().Foreground(styles.SuccessColor)
		chart = sparkline.New(chartWidth, summaryChartHeight, sparkline.WithStyle(chartStyle))
	} else {
		chart = sparkline.New(chartWidth, summaryChartHeight)
	}
	chart.PushAll(costs)

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

	// points leaves out sessions that cost nothing, so the count says which
	// sessions it covers, beside a table that lists every one, and never
	// calls them just sessions. A narrow report gives that up before the
	// direction: the table runs newest or costliest first, the other way
	// from the chart, so nothing else on screen says which end is which.
	count, short := render.Count(len(visible))+" sessions", render.Count(len(visible))
	if len(visible) < len(points) {
		short = "last " + render.Count(len(visible)) + " of " + render.Count(len(points))
		count = short + " sessions"
	}
	direction := fmt.Sprintf(", oldest %s newest", styles.Arrow)
	order := firstFit(width-len(indent),
		count+" with a cost"+direction,
		short+" with a cost"+direction,
		short+direction,
		short+" with a cost",
		short,
	)

	// The extremes drop their IDs first, then min's row, then max's.
	maxCost, minCost := "max: "+render.Cost(hi.cost), "min: "+render.Cost(lo.cost)
	maxRow, minRow := fmt.Sprintf(" (#%d", hi.row), fmt.Sprintf(" (#%d", lo.row)
	extremes := firstFit(width-len(indent),
		maxCost+maxRow+" "+hi.id+")  "+minCost+minRow+" "+lo.id+")",
		maxCost+maxRow+")  "+minCost+minRow+")",
		maxCost+maxRow+")  "+minCost,
		maxCost+"  "+minCost,
	)
	for _, l := range []string{order, extremes} {
		if noColor {
			sb.WriteString(indent + l + "\n")
		} else {
			sb.WriteString(indent + styles.DimStyle.Render(l) + "\n")
		}
	}
	return sb.String()
}

// firstFit is the first of forms, longest first, no wider than width, or the
// last.
func firstFit(width int, forms ...string) string {
	for _, f := range forms {
		if lipgloss.Width(f) <= width {
			return f
		}
	}
	return forms[len(forms)-1]
}
