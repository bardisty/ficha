package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
)

// Breakdown column widths, in display columns. Every cell but an agent's
// task is ASCII or single-width, so padding by byte or rune count is padding
// by column; render.FitTask pads a task by display width.
const (
	bdMinIndexWidth = 3
	bdModelWidth    = 10
	bdCostWidth     = 10 // fits "$9999.99  "; see render.CostCell
	bdInWidth       = 6
	bdOutWidth      = 5
	bdCacheWidth    = 6
	bdGap           = 2
	bdIndent        = 2
)

// breakdownLayout is the set of columns a breakdown frame draws. #, TIME,
// MODEL and COST always show. AGENT shows when any row came from an agent,
// sized to its widest cell. When the terminal is too narrow for everything,
// columns give way one by one, whole: a row cut at the right edge would leave
// half a number that reads as a different one, and on a narrow enough
// terminal would cut COST, the column the table is for.
type breakdownLayout struct {
	indexWidth int
	agentWidth int  // 0 when AGENT isn't drawn
	taskWidth  int  // columns AGENT gets past agentWidth for agents' tasks; 0 for none
	modelExtra int  // columns MODEL gets past bdModelWidth, for unknown model IDs
	runTags    bool // AGENT cells carry workflow run tags
	timeFmt    timeFormat
	// agents is set when a row came from an agent, so the full layout has
	// AGENT, for hidden. fullWidth is the full layout's width, TIME at the
	// form the session asks for. BreakdownModel.layout sets both.
	agents     bool
	fullWidth  int
	in, out    bool
	cacheWrite bool
	cacheRead  bool
}

// timeFormat is how TIME reads; see BreakdownModel.layout.
type timeFormat int

const (
	timeClock   timeFormat = iota // "15:04:05"
	timeWeekday                   // "Mon 15:04"
	timeDate                      // "02 Jan 15:04"
)

// breakdownCells holds one row's cells, padded to width and possibly styled.
type breakdownCells struct {
	index, time, agent, model, cost string
	in, out, cacheWrite, cacheRead  string
}

// newBreakdownLayout fits the columns to termWidth. markerWidth is the
// widest agent marker and cellWidth the widest marker plus run tag, both 0
// when no row came from an agent. timeFmt is how TIME reads. The token
// columns go first: IN (a few tokens per message, rarely what a reader is
// after), then C_WR, C_RD and OUT. Then the run tags, then AGENT itself.
// termWidth <= 0 means unknown, and keeps every column.
func newBreakdownLayout(indexWidth, markerWidth, cellWidth, termWidth int, timeFmt timeFormat) breakdownLayout {
	l := breakdownLayout{
		indexWidth: max(indexWidth, bdMinIndexWidth),
		timeFmt:    timeFmt,
		in:         true, out: true, cacheWrite: true, cacheRead: true,
	}
	if markerWidth > 0 {
		l.agentWidth = max(cellWidth, len("AGENT"))
		l.runTags = cellWidth > markerWidth
	}
	fits := func() bool { return termWidth <= 0 || l.width() <= termWidth }
	for _, drop := range []*bool{&l.in, &l.cacheWrite, &l.cacheRead, &l.out} {
		if fits() {
			return l
		}
		*drop = false
	}
	if !fits() && l.runTags {
		l.runTags = false
		l.agentWidth = max(markerWidth, len("AGENT"))
	}
	if !fits() {
		l.agentWidth = 0
	}
	return l
}

// hidden names the columns this layout leaves out for width, in the order
// they go. Run tags aren't a column of their own.
func (l breakdownLayout) hidden() []string {
	var names []string
	for _, c := range []struct {
		name  string
		drawn bool
	}{{"IN", l.in}, {"C_WR", l.cacheWrite}, {"C_RD", l.cacheRead}, {"OUT", l.out}, {"AGENT", !l.agents || l.agentWidth > 0}} {
		if !c.drawn {
			names = append(names, c.name)
		}
	}
	return names
}

// timeWidth is the TIME column's width. Every form is a fixed width.
func (l breakdownLayout) timeWidth() int { return len(l.clock(time.Unix(0, 0))) }

// clock is the TIME cell's text for t.
func (l breakdownLayout) clock(t time.Time) string {
	switch l.timeFmt {
	case timeWeekday:
		return render.WeekdayClockShort(t)
	case timeDate:
		return render.DateClockShort(t)
	}
	return render.Clock(t)
}

// modelWidth is the MODEL column's width.
func (l breakdownLayout) modelWidth() int { return bdModelWidth + l.modelExtra }

// bdMaxModelWidth caps how far MODEL grows for an unknown model ID.
const bdMaxModelWidth = 40

// widenModel gives MODEL the columns an unknown model ID needs to show in
// full (want, marker included), from whatever the row leaves spare. It never
// takes a column back from the others: the ID is a nicety next to a cost.
func (l breakdownLayout) widenModel(want, termWidth int) breakdownLayout {
	extra := min(want, bdMaxModelWidth) - bdModelWidth
	if termWidth > 0 {
		extra = min(extra, termWidth-l.width())
	}
	l.modelExtra = max(extra, 0)
	return l
}

// bdMinTaskWidth and bdMaxTaskWidth bound AGENT's task. A task repeats on
// every one of its agent's rows, so a stub that only show's one row per
// agent could live with says too little here, and past the cap the parent
// rows, blank in AGENT, would carry a wide gap for one long task.
const (
	bdMinTaskWidth = 20
	bdMaxTaskWidth = 40
)

// widenAgent gives AGENT room for the agents' tasks after their markers,
// want columns at most, from whatever the row leaves spare, as widenModel
// does for MODEL. A task is a nicety beside a cost, so it never takes a
// column from the others, and with less room than bdMinTaskWidth (or the
// whole task, if shorter) it doesn't show at all.
func (l breakdownLayout) widenAgent(want, termWidth int) breakdownLayout {
	if l.agentWidth == 0 || want == 0 {
		return l
	}
	room := min(want, bdMaxTaskWidth)
	if termWidth > 0 {
		room = min(room, termWidth-l.width()-bdGap)
	}
	if room >= min(want, bdMinTaskWidth) {
		l.taskWidth = room
	}
	return l
}

// agentColumn is AGENT's width: its markers, and the tasks after them, a
// column gap apart.
func (l breakdownLayout) agentColumn() int {
	if l.taskWidth > 0 {
		return l.agentWidth + bdGap + l.taskWidth
	}
	return l.agentWidth
}

// width is the display width of a row drawn with this layout, indent included.
func (l breakdownLayout) width() int {
	w := bdIndent + l.indexWidth + bdGap + l.timeWidth() + bdGap + l.modelWidth() + bdGap + bdCostWidth
	if l.agentWidth > 0 {
		w += bdGap + l.agentColumn()
	}
	if l.in {
		w += bdGap + bdInWidth
	}
	if l.out {
		w += bdGap + bdOutWidth
	}
	if l.cacheWrite {
		w += bdGap + bdCacheWidth
	}
	if l.cacheRead {
		w += bdGap + bdCacheWidth
	}
	return w
}

// join lays out a row's cells with the layout's columns. The cells arrive
// padded, so styling them doesn't move a column.
func (l breakdownLayout) join(c breakdownCells) string {
	cols := []string{c.index, c.time}
	if l.agentWidth > 0 {
		cols = append(cols, c.agent)
	}
	cols = append(cols, c.model, c.cost)
	if l.in {
		cols = append(cols, c.in)
	}
	if l.out {
		cols = append(cols, c.out)
	}
	if l.cacheWrite {
		cols = append(cols, c.cacheWrite)
	}
	if l.cacheRead {
		cols = append(cols, c.cacheRead)
	}
	return strings.Repeat(" ", bdIndent) + strings.Join(cols, strings.Repeat(" ", bdGap))
}

// header is the column header row, unstyled. COST right-aligns over the
// cell's four-decimal edge, like the numbers under it.
func (l breakdownLayout) header() string {
	return l.join(breakdownCells{
		index:      fmt.Sprintf("%-*s", l.indexWidth, "#"),
		time:       fmt.Sprintf("%-*s", l.timeWidth(), "TIME"),
		agent:      fmt.Sprintf("%-*s", l.agentColumn(), "AGENT"),
		model:      fmt.Sprintf("%-*s", l.modelWidth(), "MODEL"),
		cost:       fmt.Sprintf("%*s", bdCostWidth, "COST"),
		in:         fmt.Sprintf("%*s", bdInWidth, "IN"),
		out:        fmt.Sprintf("%*s", bdOutWidth, "OUT"),
		cacheWrite: fmt.Sprintf("%*s", bdCacheWidth, "C_WR"),
		cacheRead:  fmt.Sprintf("%*s", bdCacheWidth, "C_RD"),
	})
}

// workflowRunTags assigns each workflow run a short tag for the AGENT column
// (see workflowRunTag). Two runs that would share a tag, like two runs of one
// workflow, get a number after the first: "rc", "rc2".
func workflowRunTags(runs []models.WorkflowMeta) map[string]string {
	tags := make(map[string]string, len(runs))
	used := make(map[string]int, len(runs))
	for _, run := range runs {
		tag := workflowRunTag(run.Name)
		used[tag]++
		if n := used[tag]; n > 1 {
			tag = fmt.Sprintf("%s%d", tag, n)
		}
		tags[run.RunID] = tag
	}
	return tags
}

// workflowRunTag abbreviates a workflow name to the initials of its words
// ("review-changes" is "rc"), or the first two letters of a one-word name.
// Only ASCII letters and digits count, so the tag's width is its length. A
// run with no readable name is "wf".
func workflowRunTag(name string) string {
	var words []string
	var word strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			word.WriteRune(r)
			continue
		}
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}
	switch len(words) {
	case 0:
		return "wf"
	case 1:
		return words[0][:min(2, len(words[0]))]
	}
	return string(words[0][0]) + string(words[1][0])
}

// agentColumnCell is the AGENT cell padded to its column: agentCell, then,
// when the layout has room, the agent's task.
func (m BreakdownModel) agentColumnCell(msg models.BreakdownMessage, layout breakdownLayout) string {
	cell := fmt.Sprintf("%-*s", layout.agentWidth, m.agentCell(msg, layout.runTags))
	if layout.taskWidth == 0 {
		return cell
	}
	return cell + strings.Repeat(" ", bdGap) + render.FitTask(m.tasks[msg.AgentID], layout.taskWidth)
}
