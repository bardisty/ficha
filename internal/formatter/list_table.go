package formatter

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
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
}

// Column widths of the list table that don't depend on the data.
const (
	listStaticWidth   = 76
	listWhenWidth     = len("just now")
	listModelWidth    = 10
	listAgentsWidth   = len("AGENTS")
	listMinTitleWidth = 12
	// listGoodTitleWidth is the room TITLE gets before other columns go.
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

	ids := shortSessionIDs(results)
	idWidth := 0
	var costs []float64
	titleWidth := len("TITLE")
	for i, r := range results {
		idWidth = max(idWidth, len(ids[i]))
		if r.Analysis != nil {
			costs = append(costs, r.Analysis.TotalCost.TotalCost)
			titleWidth = max(titleWidth, runewidth.StringWidth(cleanTitle(r.Analysis.Title)))
		}
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
	showLength, showAgents := true, true
	tooNarrow := func() bool { return opts.Width > 0 && fixed+listGoodTitleWidth > opts.Width }
	if tooNarrow() {
		showLength = false
		fixed -= columnGap + listDurationWidth
	}
	if tooNarrow() {
		showAgents = false
		fixed -= columnGap + listAgentsWidth
	}

	// TITLE is the last column, so nothing after it needs aligning. Piped,
	// titles print whole, for grep, and the rules keep the fixed width. On a
	// terminal they're cut to fit it.
	width := listStaticWidth
	titleCol := -1
	if opts.Width > 0 {
		width = min(max(fixed+min(titleWidth, listMaxTitleWidth), listStaticWidth), opts.Width)
		width = max(width, fixed+listMinTitleWidth)
		titleCol = width - fixed
	}

	var sb strings.Builder
	sb.WriteString(renderPanel(opts.Project, []string{fmt.Sprintf("%d %s", len(results), sessionsWord(len(results)))}, width, noColor))
	sb.WriteString("\n\n")

	join := func(id, when, length, model, agents, cost, title string) string {
		cells := []string{id, when}
		if showLength {
			cells = append(cells, length)
		}
		cells = append(cells, model)
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
		"TITLE",
	)
	rule := strings.Repeat(" ", rowIndent) + strings.Repeat(styles.LineHorizontal, width-rowIndent)
	if noColor {
		sb.WriteString(header + "\n" + rule + "\n")
	} else {
		sb.WriteString(headerStyle.Render(header) + "\n" + dimStyle.Render(rule) + "\n")
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
		when := fmt.Sprintf("%-*s", listWhenWidth, render.Ago(r.Entry.Modified, opts.Now))
		a := r.Analysis

		// A session with no replies yet, or one that couldn't be read, has
		// nothing to price. Its row stays, dimmed, so the list still accounts
		// for every transcript.
		if a == nil || a.MessageCount == 0 {
			title := "(unreadable)"
			if a != nil {
				title = "(no replies yet)"
				if t := cleanTitle(a.Title); t != "" {
					title = t
				}
			}
			row := join(id, when,
				fmt.Sprintf("%*s", listDurationWidth, "-"),
				fmt.Sprintf("%-*s", listModelWidth, "-"),
				fmt.Sprintf("%*s", listAgentsWidth, "-"),
				fmt.Sprintf("%*s", costWidth, "-"),
				truncateRight(title, titleCol))
			if !noColor {
				row = dimStyle.Render(row)
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
		title := cleanTitle(a.Title)
		if title == "" {
			title = "-"
		}
		title = truncateRight(title, titleCol)

		if noColor {
			sb.WriteString(join(id, when, duration, model, agents, render.CostCell(a.TotalCost.TotalCost, costWidth), title) + "\n")
			continue
		}
		costColor := styles.GetCostGradientColor(a.TotalCost.TotalCost, minCost, maxCost)
		sb.WriteString(join(
			id,
			dimStyle.Render(when),
			dimStyle.Render(duration),
			lipgloss.NewStyle().Foreground(styles.GetModelColor(modelName)).Render(model),
			dimStyle.Render(agents),
			render.CostColored(a.TotalCost.TotalCost, costColor, costWidth),
			title,
		) + "\n")
	}

	sb.WriteString("\n")
	if noColor {
		sb.WriteString(strings.Repeat(styles.LineHorizontal, width))
	} else {
		sb.WriteString(dimStyle.Render(strings.Repeat(styles.LineHorizontal, width)))
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

// cleanTitle makes a transcript's title safe to print on one line. It comes
// from the transcript, so a control character could otherwise move the
// cursor or recolor the terminal.
func cleanTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// truncateRight fits s into width display columns, ending in an ellipsis
// when it's cut. A negative width leaves s whole.
func truncateRight(s string, width int) string {
	if width < 0 || runewidth.StringWidth(s) <= width {
		return s
	}
	return runewidth.Truncate(s, width, styles.Ellipsis)
}
