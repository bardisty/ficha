package formatter

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/mattn/go-runewidth"
)

// ListTableOptions shapes FormatSessionListTable.
type ListTableOptions struct {
	// Width is the terminal's width, or 0 when stdout isn't a terminal. With
	// a width, TITLE widens to fit long titles. Without one the layout is
	// fixed, so piped output doesn't depend on the terminal it came from.
	Width int
	// Now anchors WHEN's relative times. Zero means the current time.
	Now time.Time
	// Project is the project's display name for the header.
	Project string
	// Window is the --since/--until range the results were analyzed in, or
	// nil for none.
	Window *models.TimeWindow
}

// Column widths of the list table that don't depend on the data.
const (
	listStaticWidth = 76
	listWhenWidth   = len("just now")
	listModelWidth  = 10
	listAgentsWidth = len("AGENTS")
	// listMinTitleWidth is the room TITLE keeps before MODEL goes. Past
	// that, TITLE shrinks, to nothing if it must, rather than rows wrap.
	listMinTitleWidth = 12
	// listGoodTitleWidth is the most room TITLE asks for before other
	// columns go: a narrower widest title asks for its own width.
	listGoodTitleWidth = 24
	// listMaxTitleWidth keeps one long title from stretching the rules
	// across a wide terminal.
	listMaxTitleWidth = 72
)

// FormatSessionListTable renders one row per session, newest first as given,
// with enough to pick one out: when, how long, which model, what it cost and
// its title. results carries each session's analysis; a nil analysis is a
// transcript that couldn't be read.
func FormatSessionListTable(results []models.SessionResult, noColor bool, opts ListTableOptions) string {
	if opts.Now.IsZero() {
		opts.Now = now()
	}
	if opts.Window != nil && !slices.ContainsFunc(results, func(r models.SessionResult) bool {
		return r.Analysis != nil && r.Analysis.MessageCount > 0
	}) {
		return emptyWindow(*opts.Window)
	}

	ids := shortSessionIDs(results)
	idWidth := 0
	var costs []float64
	titleWidth := len("TITLE")
	for i, r := range results {
		idWidth = max(idWidth, len(ids[i]))
		if r.Analysis != nil {
			// A session with no replies prints a dash, not a cost, so its 0
			// doesn't set the floor the color gradient runs from.
			if r.Analysis.MessageCount > 0 {
				costs = append(costs, r.Analysis.TotalCost.TotalCost)
			}
		}
		titleWidth = max(titleWidth, runewidth.StringWidth(rowTitle(r)))
	}
	costWidth := render.CostCellWidth(len("COST"), costs...)
	// LENGTH grows past "12h 34m" for a session left open for days.
	listDurationWidth := len("LENGTH")
	for _, r := range results {
		if r.Analysis != nil && r.Analysis.MessageCount > 0 {
			listDurationWidth = max(listDurationWidth, len(render.Duration(r.Analysis.Duration.Duration())))
		}
	}

	gap := strings.Repeat(" ", columnGap)
	fixed := rowIndent + idWidth + columnGap + listWhenWidth + columnGap + listDurationWidth +
		columnGap + listModelWidth + columnGap + listAgentsWidth + columnGap + costWidth + columnGap

	// A terminal that would leave TITLE too little room gives up LENGTH, then
	// AGENTS, keeping what picks a session out: when, model, cost and title.
	// A narrower one gives up MODEL too, since the ID, when and cost still
	// tell sessions apart and a title is what names them. TITLE asks only
	// for the room its widest title takes, never less than its header, so
	// short titles keep the columns.
	showLength, showAgents, showModel := true, true, true
	tooNarrow := func(title int) bool { return opts.Width > 0 && fixed+min(titleWidth, title) > opts.Width }
	if tooNarrow(listGoodTitleWidth) {
		showLength = false
		fixed -= columnGap + listDurationWidth
	}
	if tooNarrow(listGoodTitleWidth) {
		showAgents = false
		fixed -= columnGap + listAgentsWidth
	}
	if tooNarrow(listMinTitleWidth) {
		showModel = false
		fixed -= columnGap + listModelWidth
	}

	// TITLE is the last column, so nothing after it needs aligning. Piped,
	// titles print whole, for grep, and the rules keep the fixed width. On a
	// terminal they're cut to fit it.
	width := listStaticWidth
	titleCol := -1
	if opts.Width > 0 {
		width = min(max(fixed+min(titleWidth, listMaxTitleWidth), listStaticWidth), opts.Width)
		width = max(width, fixed)
		titleCol = width - fixed
	}

	var sb strings.Builder
	// The count is of sessions read, as summary counts them, so the two
	// agree; the rest are named apart, since their rows still show.
	readable := 0
	for _, r := range results {
		if r.Analysis != nil {
			readable++
		}
	}
	count := fmt.Sprintf("%d %s", readable, sessionsWord(readable))
	fields := []string{count}
	if unreadable := len(results) - readable; unreadable > 0 {
		fields[0] += fmt.Sprintf(", %d unreadable", unreadable)
	}
	// A window stays in the header when nothing else fits: without it the
	// rows read as all-time.
	giveWay := []int{panelLead}
	if opts.Window != nil {
		when := spanOrWindow(opts.Window, time.Time{}, time.Time{})
		if lipgloss.Width(when) > width-6 {
			when = strings.TrimPrefix(when, "Window: ")
		}
		fields = append(fields, when)
		giveWay = []int{panelLead, 0}
	}
	// A box too narrow for the unreadable sessions' count drops it where
	// that keeps the project, or the session count, which would otherwise
	// go; their rows still name them. The project needs the room
	// renderPanel keeps for it.
	fits := func(head string, project bool) bool {
		need := append([]string{head}, fields[1:]...)
		if project && opts.Project != "" {
			need = append(need, strings.Repeat(" ", min(lipgloss.Width(models.StripControl(opts.Project)), minProjectWidth)))
		}
		return lipgloss.Width(strings.Join(need, "  "+styles.BoxVerticalSep+"  ")) <= width-6
	}
	if !fits(fields[0], true) && (fits(count, true) || !fits(fields[0], false)) {
		fields[0] = count
	}
	sb.WriteString(renderPanel(opts.Project, fields, giveWay, width, noColor))
	sb.WriteString("\n\n")

	join := func(id, when, length, model, agents, cost, title string) string {
		cells := []string{id, when}
		if showLength {
			cells = append(cells, length)
		}
		if showModel {
			cells = append(cells, model)
		}
		if showAgents {
			cells = append(cells, agents)
		}
		cells = append(cells, cost, title)
		return strings.Repeat(" ", rowIndent) + strings.Join(cells, gap)
	}
	header := join(
		fmt.Sprintf("%-*s", idWidth, "ID"),
		fmt.Sprintf("%-*s", listWhenWidth, "WHEN"),
		fmt.Sprintf("%*s", listDurationWidth, "LENGTH"),
		fmt.Sprintf("%-*s", listModelWidth, "MODEL"),
		fmt.Sprintf("%*s", listAgentsWidth, "AGENTS"),
		fmt.Sprintf("%*s", costWidth, "COST"),
		truncateRight("TITLE", titleCol),
	)
	rule := strings.Repeat(" ", rowIndent) + strings.Repeat(styles.LineHorizontal, width-rowIndent)
	if noColor {
		sb.WriteString(header + "\n" + rule + "\n")
	} else {
		sb.WriteString(styles.HeaderStyle.Render(header) + "\n" + styles.DimStyle.Render(rule) + "\n")
	}

	var minCost, maxCost float64
	for i, c := range costs {
		if i == 0 || c < minCost {
			minCost = c
		}
		maxCost = max(maxCost, c)
	}

	for i, r := range results {
		id := fmt.Sprintf("%-*s", idWidth, ids[i])
		when := fmt.Sprintf("%-*s", listWhenWidth, render.Ago(r.LastActive(), opts.Now))
		a := r.Analysis

		// A session with no replies yet, or one that couldn't be read, has
		// nothing to price. Its row stays, dimmed, so the list still accounts
		// for every transcript.
		if a == nil || a.MessageCount == 0 {
			row := join(id, when,
				fmt.Sprintf("%*s", listDurationWidth, "-"),
				fmt.Sprintf("%-*s", listModelWidth, "-"),
				fmt.Sprintf("%*s", listAgentsWidth, "-"),
				fmt.Sprintf("%*s", costWidth, "-"),
				truncateRight(rowTitle(r), titleCol))
			if !noColor {
				row = styles.DimStyle.Render(row)
			}
			sb.WriteString(row + "\n")
			continue
		}

		duration := fmt.Sprintf("%*s", listDurationWidth, render.Duration(a.Duration.Duration()))
		modelName := parentPrimaryModel(a)
		model := fmt.Sprintf("%-*s", listModelWidth, render.ClampModel(modelName, listModelWidth))
		agents := fmt.Sprintf("%*s", listAgentsWidth, "-")
		if a.AgentCount > 0 {
			agents = fmt.Sprintf("%*d", listAgentsWidth, a.AgentCount)
		}
		title := truncateRight(rowTitle(r), titleCol)

		if noColor {
			sb.WriteString(join(id, when, duration, model, agents, render.CostCell(a.TotalCost.TotalCost, costWidth), title) + "\n")
			continue
		}
		costColor := styles.GetCostGradientColor(a.TotalCost.TotalCost, minCost, maxCost)
		sb.WriteString(join(
			id,
			styles.DimStyle.Render(when),
			styles.DimStyle.Render(duration),
			lipgloss.NewStyle().Foreground(styles.GetModelColor(modelName)).Render(model),
			styles.DimStyle.Render(agents),
			render.CostColored(a.TotalCost.TotalCost, costColor, costWidth),
			title,
		) + "\n")
	}

	sb.WriteString("\n")
	if noColor {
		sb.WriteString(strings.Repeat(styles.LineHorizontal, width))
	} else {
		sb.WriteString(styles.DimStyle.Render(strings.Repeat(styles.LineHorizontal, width)))
	}
	return trimLineEnds(sb.String())
}

// parentPrimaryModel names the model that cost the most in the parent
// transcript, the one the user talked to, ignoring what agents ran on.
func parentPrimaryModel(a *models.SessionAnalysis) string {
	if len(a.ParentCostByModel) > 0 {
		return render.PrimaryModel(a.ParentCostByModel)
	}
	return render.PrimaryModel(a.CostByModel)
}

// shortSessionIDs gives each session the shortest ID prefix, at least 8
// characters, that no other listed session shares, so every ID shown still
// works as `ficha show <id>`.
func shortSessionIDs(results []models.SessionResult) []string {
	const short = 8
	ids := make([]string, len(results))
	for i, r := range results {
		id := r.Entry.SessionID
		n := min(short, len(id))
		for j, other := range results {
			if j == i {
				continue
			}
			o := other.Entry.SessionID
			for n < len(id) && strings.HasPrefix(o, id[:n]) {
				n++
			}
		}
		ids[i] = id[:n]
	}
	return ids
}

// truncateRight fits s into width display columns, ending in an ellipsis
// when it's cut. A negative width leaves s whole, and a zero width, with no
// room even for the ellipsis, leaves nothing.
func truncateRight(s string, width int) string {
	if width < 0 || runewidth.StringWidth(s) <= width {
		return s
	}
	if width == 0 {
		return ""
	}
	return runewidth.Truncate(s, width, styles.Ellipsis)
}

// rowTitle is what a row prints in TITLE before it's cut: the session's
// title, or a placeholder for a session with nothing to price.
func rowTitle(r models.SessionResult) string {
	a := r.Analysis
	if a == nil {
		return nestedTitle(r, "(unreadable)")
	}
	title := models.OneLine(a.Title)
	if title == "" && a.MessageCount == 0 {
		title = "(no replies yet)"
	}
	return nestedTitle(r, title)
}

// nestedTitle is a row's TITLE: the session's title, "-" for none, after the
// name of the nested project the session ran in, when it ran in one.
func nestedTitle(r models.SessionResult, title string) string {
	if title == "" {
		title = "-"
	}
	if r.Nested == "" {
		return title
	}
	return r.Nested + " " + styles.FieldSep + " " + title
}
