package formatter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// GlobalTableOptions shapes FormatGlobalTable. json and csv ignore it and
// always export every project.
type GlobalTableOptions struct {
	// TopN is how many project rows to show. Details shows them all.
	TopN    int
	Details bool
	// SortBy names the order analysis.Projects is already in: cost (also
	// when empty), sessions, name or activity. It titles the table, and the
	// running CUMULATIVE column only means something in cost order.
	SortBy string
	// Width is the terminal's width, or 0 when stdout isn't a terminal. With
	// a width, PROJECT widens to fit long paths. Without one the layout is
	// fixed, so piped output doesn't depend on the terminal it came from.
	Width int
	// Now anchors LAST ACTIVE's relative times. Zero means the current time.
	Now time.Time
}

// FormatGlobalTable renders global analysis as a table. Color and glyph choices
// are driven by noColor.
func FormatGlobalTable(analysis *models.GlobalAnalysis, noColor bool, opts GlobalTableOptions) string {
	if opts.SortBy == "" {
		opts.SortBy = "cost"
	}
	if opts.Now.IsZero() {
		opts.Now = now()
	}
	if analysis.Window != nil && len(analysis.Projects) == 0 {
		return emptyWindow(*analysis.Window)
	}
	// The cost rows narrow with the frame, but only as far as their fitted
	// form, so the frame goes no narrower than that.
	minWidth := lipgloss.Width(costRows(analysis.TotalCost, analysis.TotalUsage, false, false, fittedCostRowWidths(analysis.TotalCost, analysis.TotalUsage), true))
	layout := newProjectsLayout(analysis.Projects, opts, minWidth)
	sectionWidth := layout.width

	var sb strings.Builder

	// Header panel
	sb.WriteString(renderGlobalHeaderPanel(analysis, sectionWidth, noColor))
	sb.WriteString("\n\n")

	// Hero total cost
	sb.WriteString(renderHeroCost(analysis.TotalCost.TotalCost, sectionWidth, noColor))
	sb.WriteString("\n\n")

	sb.WriteString(renderCostRows(analysis.TotalCost, analysis.TotalUsage, sectionWidth, noColor))

	// Cost by model section, left out for the same reason as in show
	if len(analysis.CostByModel) > 0 {
		sb.WriteString("\n")
		sb.WriteString(render.SectionHeader("COST BY MODEL", sectionWidth, noColor))
		sb.WriteString("\n\n")
		sb.WriteString(formatGlobalCostByModel(analysis.CostByModel, noColor))
	}

	// Projects section
	sb.WriteString("\n")
	sb.WriteString(render.SectionHeader(projectsHeading(layout.rows, len(analysis.Projects), opts.SortBy), sectionWidth, noColor))
	sb.WriteString("\n\n")
	sb.WriteString(renderProjectsTable(analysis, noColor, layout, opts))

	// Footer
	sb.WriteString("\n")
	sb.WriteString(renderFooterDoubleRule(sectionWidth, noColor))
	sb.WriteString("\n")

	sb.WriteString(footerStats([]string{
		"Total: " + render.Cost(analysis.TotalCost.TotalCost),
		messagesField(analysis.MessageCount, analysis.MessageCount, 0),
		fmt.Sprintf("Sessions: %d", analysis.SessionCount),
		fmt.Sprintf("Projects: %d", analysis.ProjectCount),
	}, sectionWidth, noColor))
	sb.WriteString("\n")
	sb.WriteString(renderFooterSingleRule(sectionWidth, noColor))

	return trimLineEnds(sb.String())
}

// projectsHeading titles the projects table with how many rows it holds out
// of how many, and the order they're in, since only the cost order is
// visible from the columns alone: "PROJECTS (5 of 20, by activity)".
// The count is there even when no rows are, so the line accounting for the
// rest sits under a heading that says why the table is empty.
func projectsHeading(shown, total int, sortBy string) string {
	count := fmt.Sprintf("%d of %d", shown, total)
	if shown == total {
		count = fmt.Sprintf("all %d", total)
	}
	return fmt.Sprintf("PROJECTS (%s, by %s)", count, sortBy)
}

// Column widths of the projects table that don't depend on the data.
const (
	// globalStaticWidth is the report width when stdout isn't a terminal,
	// the same 76 columns as show, list and summary, so it fits in 80.
	globalStaticWidth = 76
	// globalPipedMaxWidth is as far as the piped layout stretches for the
	// cumulative column or wide costs before it drops columns instead.
	globalPipedMaxWidth = 80
	// minProjectWidth keeps enough of a left-truncated path to tell rows
	// apart ("…/work/api-server") on a narrow terminal.
	minProjectWidth = 16
	rankWidth       = 3
	sessionsWidth   = len("SESSIONS")
	pctWidth        = len("% TOTAL")
	activeWidth     = len("LAST ACTIVE")
	cumMinWidth     = len("CUMULATIVE")
	costMinWidth    = 8
	columnGap       = 2
	rowIndent       = 2
)

// projectsLayout sizes the projects table. Every width is in display columns.
type projectsLayout struct {
	rows       int // project rows shown
	project    int
	cost       int
	cumulative int  // 0 when the column is hidden
	sessions   bool // whether SESSIONS is shown
	pct        bool // whether % TOTAL is shown
	active     bool // whether LAST ACTIVE is shown
	width      int  // the whole report's width, rules included
}

func newProjectsLayout(projects []models.ProjectAnalysis, opts GlobalTableOptions, minWidth int) projectsLayout {
	l := projectsLayout{rows: max(opts.TopN, 0), sessions: true, pct: true, active: true}
	if opts.Details || l.rows > len(projects) {
		l.rows = len(projects)
	}

	// Cost columns grow to fit their widest value, so a large total widens the
	// column for every row instead of pushing one row out of line.
	var costs, running []float64
	var sum float64
	nameWidth := len("PROJECT")
	for _, p := range projects[:l.rows] {
		sum += p.TotalCost.TotalCost
		costs = append(costs, p.TotalCost.TotalCost)
		running = append(running, sum)
		nameWidth = max(nameWidth, runewidth.StringWidth(stripControl(p.DisplayName)))
	}
	l.cost = render.CostCellWidth(costMinWidth, costs...)
	if opts.Details && opts.SortBy == "cost" {
		l.cumulative = render.CostCellWidth(cumMinWidth, running...)
	}

	fixed := rowIndent + rankWidth + columnGap + columnGap + sessionsWidth +
		columnGap + l.cost + columnGap + pctWidth + columnGap + activeWidth
	if l.cumulative > 0 {
		fixed += columnGap + l.cumulative
	}
	// A terminal too narrow for every column gives up % TOTAL first, since
	// COST against the total says the same, then LAST ACTIVE, then SESSIONS,
	// each unless the rows are sorted by it. Only then do the rows wrap. Piped
	// output holds to 80 columns the same way, which large costs would
	// otherwise push past.
	budget := opts.Width
	if budget <= 0 {
		budget = globalPipedMaxWidth
	}
	tooNarrow := func() bool { return fixed+minProjectWidth > budget }
	if tooNarrow() {
		l.pct = false
		fixed -= columnGap + pctWidth
	}
	if tooNarrow() && opts.SortBy != "activity" {
		l.active = false
		fixed -= columnGap + activeWidth
	}
	if tooNarrow() && opts.SortBy != "sessions" {
		l.sessions = false
		fixed -= columnGap + sessionsWidth
	}

	l.width = globalStaticWidth
	if opts.Width > 0 {
		l.width = min(max(fixed+nameWidth, globalStaticWidth), opts.Width)
	}
	l.width = max(l.width, fixed+minProjectWidth, minWidth)
	l.project = l.width - fixed
	return l
}

// renderGlobalHeaderPanel renders global's boxed header: how many projects
// and sessions, and the span from the first message to the last.
func renderGlobalHeaderPanel(analysis *models.GlobalAnalysis, width int, noColor bool) string {
	return renderPanel("", []string{
		fmt.Sprintf("%d %s", analysis.ProjectCount, projectsWord(analysis.ProjectCount)),
		fmt.Sprintf("%d %s", analysis.SessionCount, sessionsWord(analysis.SessionCount)),
		spanOrWindow(analysis.Window, analysis.FirstActive, analysis.LastActive),
	}, []int{2, 1}, width, noColor)
}

// formatGlobalCostByModel renders cost by model for global stats
func formatGlobalCostByModel(costByModel map[string]models.CostBreakdown, noColor bool) string {
	var sb strings.Builder

	// Highest-cost model first.
	for _, modelID := range render.OrderModelsByCost(costByModel) {
		cost := costByModel[modelID]
		modelName := pricing.GetModelDisplayName(modelID)
		modelLabel := render.ClampModel(modelName, 12)
		if noColor {
			sb.WriteString(fmt.Sprintf("    %-12s %s\n", modelLabel, render.CostCell(cost.TotalCost, 12)))
		} else {
			modelColor := styles.GetModelColor(modelName)
			modelStyled := lipgloss.NewStyle().Foreground(modelColor).Render(fmt.Sprintf("%-12s", modelLabel))
			sb.WriteString(fmt.Sprintf("    %s %s\n", modelStyled, formatCostStyled(cost.TotalCost, 12, noColor)))
		}
	}

	return sb.String()
}

// renderProjectsTable renders the projects table, or with no rows to show,
// just the line accounting for the projects it leaves out.
func renderProjectsTable(analysis *models.GlobalAnalysis, noColor bool, layout projectsLayout, opts GlobalTableOptions) string {
	var sb strings.Builder
	projects := analysis.Projects

	if layout.rows > 0 {
		writeProjectRows(&sb, analysis, noColor, layout, opts)
	}

	if remaining := len(projects) - layout.rows; remaining > 0 {
		var remainingCost float64
		for _, p := range projects[layout.rows:] {
			remainingCost += p.TotalCost.TotalCost
		}
		more := " more"
		if layout.rows == 0 {
			more = ""
		}
		summaryText := fmt.Sprintf("(%d%s %s totaling %s)", remaining, more, projectsWord(remaining), render.Cost(remainingCost))
		if noColor {
			sb.WriteString(fmt.Sprintf("  %s\n", summaryText))
		} else {
			sb.WriteString(fmt.Sprintf("  %s\n", styles.DimStyle.Render(summaryText)))
		}
	}

	return sb.String()
}

func projectsWord(n int) string {
	if n == 1 {
		return "project"
	}
	return "projects"
}

// writeProjectRows writes the header, the rows and the closing rule of the
// projects table.
func writeProjectRows(sb *strings.Builder, analysis *models.GlobalAnalysis, noColor bool, layout projectsLayout, opts GlobalTableOptions) {
	projects := analysis.Projects

	// Calculate min/max for gradient by scanning — projects may be re-sorted
	// by any key (--sort-by), so positional first/last are not cost extremes
	minCost := projects[0].TotalCost.TotalCost
	maxCost := minCost
	for _, p := range projects[1:] {
		c := p.TotalCost.TotalCost
		minCost = min(minCost, c)
		maxCost = max(maxCost, c)
	}

	gap := strings.Repeat(" ", columnGap)
	join := func(cells ...string) string {
		return strings.Repeat(" ", rowIndent) + strings.Join(cells, gap)
	}
	header := []string{
		fmt.Sprintf("%*s", rankWidth, "#"),
		fmt.Sprintf("%-*s", layout.project, "PROJECT"),
	}
	if layout.sessions {
		header = append(header, fmt.Sprintf("%*s", sessionsWidth, "SESSIONS"))
	}
	header = append(header, fmt.Sprintf("%*s", layout.cost, "COST"))
	if layout.pct {
		header = append(header, fmt.Sprintf("%*s", pctWidth, "% TOTAL"))
	}
	if layout.cumulative > 0 {
		header = append(header, fmt.Sprintf("%*s", layout.cumulative, "CUMULATIVE"))
	}
	if layout.active {
		header = append(header, fmt.Sprintf("%*s", activeWidth, "LAST ACTIVE"))
	}

	indent := strings.Repeat(" ", rowIndent)
	rule := indent + strings.Repeat(styles.LineHorizontal, layout.width-rowIndent)
	if !noColor {
		rule = indent + styles.DimStyle.Render(strings.Repeat(styles.LineHorizontal, layout.width-rowIndent))
	}
	if noColor {
		sb.WriteString(join(header...) + "\n")
	} else {
		sb.WriteString(styles.HeaderStyle.Render(join(header...)) + "\n")
	}
	sb.WriteString(rule + "\n")

	// The column the rows are sorted by stays undimmed, so the order reads
	// at a glance.
	keyStyle := func(key string) lipgloss.Style {
		if opts.SortBy == key {
			return lipgloss.NewStyle()
		}
		return styles.DimStyle
	}

	var cumulative float64
	for i, p := range projects[:layout.rows] {
		cumulative += p.TotalCost.TotalCost
		pctTotal := 0.0
		if analysis.TotalCost.TotalCost > 0 {
			pctTotal = (p.TotalCost.TotalCost / analysis.TotalCost.TotalCost) * 100
		}

		// Truncate project name from the left if needed, then pad by
		// display width: fmt's %-Ns counts runes, so a wide (CJK/emoji) name
		// would under-pad and shift every column to its right.
		name := truncateLeft(stripControl(p.DisplayName), layout.project)
		name += strings.Repeat(" ", layout.project-runewidth.StringWidth(name))

		rank := fmt.Sprintf("%*d", rankWidth, i+1)
		sessions := fmt.Sprintf("%*d", sessionsWidth, p.SessionCount)
		pct := fmt.Sprintf("%*s", pctWidth, fmt.Sprintf("%.1f%%", pctTotal))
		active := fmt.Sprintf("%*s", activeWidth, render.Ago(p.LastActive, opts.Now))

		var cells []string
		if noColor {
			cells = []string{rank, name}
			if layout.sessions {
				cells = append(cells, sessions)
			}
			cells = append(cells, render.CostCell(p.TotalCost.TotalCost, layout.cost))
			if layout.pct {
				cells = append(cells, pct)
			}
			if layout.cumulative > 0 {
				cells = append(cells, render.CostCell(cumulative, layout.cumulative))
			}
			if layout.active {
				cells = append(cells, active)
			}
		} else {
			costColor := styles.GetCostGradientColor(p.TotalCost.TotalCost, minCost, maxCost)
			cells = []string{styles.DimStyle.Render(rank), name}
			if layout.sessions {
				cells = append(cells, keyStyle("sessions").Render(sessions))
			}
			cells = append(cells, render.CostColored(p.TotalCost.TotalCost, costColor, layout.cost))
			if layout.pct {
				cells = append(cells, styles.DimStyle.Render(pct))
			}
			if layout.cumulative > 0 {
				cells = append(cells, render.CostColored(cumulative, styles.SuccessColor, layout.cumulative))
			}
			if layout.active {
				cells = append(cells, keyStyle("activity").Render(active))
			}
		}
		sb.WriteString(join(cells...) + "\n")
	}

	sb.WriteString(rule + "\n")
}

// truncateLeft fits a project name into maxWidth display columns by cutting
// from the left, so the distinctive tail survives ("…/source/webapp"): every
// name shares its leading home or root prefix. A cut a few columns short of a
// separator moves to it, so "…/work/api-server" doesn't read "…rk/api-server".
// A cut further into a long directory name stays put: the partial name it
// leaves ("…ing-service/worktrees/x") is often what tells two rows apart.
// Operates on runes and display width, so wide (CJK) characters are never
// split.
func truncateLeft(s string, maxWidth int) string {
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	ell := styles.Ellipsis
	available := maxWidth - runewidth.StringWidth(ell)
	if available < 1 {
		return runewidth.Truncate(s, maxWidth, "")
	}

	runes := []rune(s)
	start, w := len(runes), 0
	for i := len(runes) - 1; i >= 0; i-- {
		rw := runewidth.RuneWidth(runes[i])
		if w+rw > available {
			break
		}
		w += rw
		start = i
	}
	const snapWindow = 3
	if start > 0 && runes[start-1] != '/' {
		for j := start; j < min(start+snapWindow, len(runes)-1); j++ {
			if runes[j] == '/' {
				start = j
				break
			}
		}
	}
	return ell + string(runes[start:])
}

// FormatGlobalJSON formats global analysis as JSON
func FormatGlobalJSON(analysis *models.GlobalAnalysis, pretty bool) (string, error) {
	var data []byte
	var err error

	if pretty {
		data, err = json.MarshalIndent(analysis, "", "  ")
	} else {
		data, err = json.Marshal(analysis)
	}

	if err != nil {
		return "", err
	}

	return string(data), nil
}

// FormatGlobalCSV formats global analysis as CSV
func FormatGlobalCSV(analysis *models.GlobalAnalysis) (string, error) {
	var sb strings.Builder
	w := csv.NewWriter(&sb)

	// Write header
	header := []string{
		"project",
		"sessions",
		"messages",
		"input_cost",
		"output_cost",
		"cache_write_5m_cost",
		"cache_write_1h_cost",
		"cache_read_cost",
		"total_cost",
		"cache_savings",
		"first_active",
		"last_active",
		"skipped_sessions",
		"skipped_agents",
		"skipped_lines",
		"estimated_cost_messages",
		"unpriced_models",
		"encoded_path",
		"full_path",
	}
	if err := w.Write(header); err != nil {
		return "", fmt.Errorf("writing CSV header: %w", err)
	}

	// Write project rows
	for _, p := range analysis.Projects {
		row := []string{
			csvCell(p.DisplayName),
			fmt.Sprintf("%d", p.SessionCount),
			fmt.Sprintf("%d", p.MessageCount),
			fmt.Sprintf("%.6f", p.TotalCost.InputCost),
			fmt.Sprintf("%.6f", p.TotalCost.OutputCost),
			fmt.Sprintf("%.6f", p.TotalCost.CacheWrite5mCost),
			fmt.Sprintf("%.6f", p.TotalCost.CacheWrite1hCost),
			fmt.Sprintf("%.6f", p.TotalCost.CacheReadCost),
			fmt.Sprintf("%.6f", p.TotalCost.TotalCost),
			fmt.Sprintf("%.6f", p.TotalCost.CacheSavings),
			models.MachineTime(p.FirstActive),
			models.MachineTime(p.LastActive),
			fmt.Sprintf("%d", p.SkippedSessions),
			fmt.Sprintf("%d", p.SkippedAgents),
			fmt.Sprintf("%d", p.SkippedLines),
			fmt.Sprintf("%d", p.EstimatedCostMessages),
			unpricedModelsCell(p.UnpricedModels),
			csvCell(p.EncodedPath),
			csvCell(p.FullPath),
		}
		if err := w.Write(row); err != nil {
			return "", fmt.Errorf("writing CSV row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("flushing CSV: %w", err)
	}

	return sb.String(), nil
}
