package formatter

import (
	"fmt"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// renderDaysSection is the COST BY DAY section --by-day adds to summary and
// global, one row per day newest first, or "" with no days to show.
func renderDaysSection(days []models.DayCost, width int, noColor bool) string {
	if len(days) == 0 {
		return ""
	}
	// A day is labeled like every other date ficha prints, the year added
	// when it isn't this one's, with its weekday ahead of it. A terminal too
	// narrow for every column drops the weekday, then SESSIONS, before a
	// row would wrap, as PROJECTS gives up its columns.
	labels := dayLabels(days, true)
	costs := make([]float64, len(days))
	msgsWidth := len("MESSAGES")
	for i, d := range days {
		costs[i] = d.TotalCost.TotalCost
		msgsWidth = max(msgsWidth, len(render.Count(d.MessageCount)))
	}
	costWidth := render.CostCellWidth(costMinWidth, costs...)
	widest := func(labels []string) int {
		dayWidth := len("DAY")
		for _, l := range labels {
			dayWidth = max(dayWidth, len(l))
		}
		return dayWidth
	}
	dayWidth := widest(labels)
	fixed := rowIndent + columnGap + sessionsWidth + columnGap + msgsWidth + columnGap + costWidth
	if fixed+dayWidth > width {
		labels = dayLabels(days, false)
		dayWidth = widest(labels)
	}
	showSessions := fixed+dayWidth <= width
	minCost, maxCost := costs[0], costs[0]
	for _, c := range costs[1:] {
		minCost, maxCost = min(minCost, c), max(maxCost, c)
	}

	var sb strings.Builder
	sb.WriteString(render.SectionHeader("COST BY DAY", width, noColor))
	sb.WriteString("\n\n")
	indent := strings.Repeat(" ", rowIndent)
	gap := strings.Repeat(" ", columnGap)
	row := func(day, sessions, msgs, cost string) string {
		cells := []string{day}
		if showSessions {
			cells = append(cells, sessions)
		}
		return indent + strings.Join(append(cells, msgs, cost), gap)
	}
	header := row(fmt.Sprintf("%-*s", dayWidth, "DAY"), fmt.Sprintf("%*s", sessionsWidth, "SESSIONS"),
		fmt.Sprintf("%*s", msgsWidth, "MESSAGES"), fmt.Sprintf("%*s", costWidth, "COST"))
	rule := indent + strings.Repeat(styles.LineHorizontal, width-rowIndent)
	if !noColor {
		header = styles.HeaderStyle.Render(header)
		rule = indent + styles.DimStyle.Render(strings.Repeat(styles.LineHorizontal, width-rowIndent))
	}
	sb.WriteString(header + "\n" + rule + "\n")
	for i, d := range days {
		sessions := fmt.Sprintf("%*s", sessionsWidth, render.Count(d.SessionCount))
		msgs := fmt.Sprintf("%*s", msgsWidth, render.Count(d.MessageCount))
		cost := render.CostCell(d.TotalCost.TotalCost, costWidth)
		if !noColor {
			sessions, msgs = styles.DimStyle.Render(sessions), styles.DimStyle.Render(msgs)
			cost = render.CostColored(d.TotalCost.TotalCost, styles.GetCostGradientColor(d.TotalCost.TotalCost, minCost, maxCost), costWidth)
		}
		sb.WriteString(strings.TrimRight(row(fmt.Sprintf("%-*s", dayWidth, labels[i]), sessions, msgs, cost), " ") + "\n")
	}
	// Closed like PROJECTS and SESSION BREAKDOWN, so a long run of days
	// doesn't run into the next heading.
	sb.WriteString(rule + "\n")
	return sb.String()
}

// dayLabels names each day, "Fri Oct 31" or "Oct 31" without the weekday,
// and "no date" for the messages that carry no timestamp.
func dayLabels(days []models.DayCost, weekday bool) []string {
	labels := make([]string, len(days))
	for i, d := range days {
		day, err := time.Parse(time.DateOnly, d.Date)
		if err != nil {
			labels[i] = "no date"
			continue
		}
		labels[i] = render.CalendarDate(day, now())
		if weekday {
			labels[i] = day.Format("Mon") + " " + labels[i]
		}
	}
	return labels
}
