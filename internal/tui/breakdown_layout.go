package tui

import (
	"fmt"
	"strings"

	"github.com/bardisty/ficha/internal/models"
)

// Breakdown column widths, in display columns. Every cell is ASCII or
// single-width, so padding by byte or rune count is padding by column.
const (
	bdIndexWidth = 5
	bdTimeWidth  = 8
	bdModelWidth = 10
	bdCostWidth  = 10 // fits "$9999.99  "; see render.CostCell
	bdInWidth    = 6
	bdOutWidth   = 5
	bdCacheWidth = 6
	bdGap        = 2
	bdIndent     = 2
)

// breakdownLayout is the set of columns a breakdown frame draws. #, TIME,
// MODEL and COST always show. AGENT shows when any row came from an agent,
// sized to its widest cell. The token columns give way one by one, whole,
// when the terminal is too narrow for all of them: a row cut at the right
// edge would leave half a number that reads as a different one.
type breakdownLayout struct {
	agentWidth int // 0 when no row came from an agent
	in, out    bool
	cacheWrite bool
	cacheRead  bool
}

// breakdownCells holds one row's cells, padded to width and possibly styled.
type breakdownCells struct {
	index, time, agent, model, cost string
	in, out, cacheWrite, cacheRead  string
}

// newBreakdownLayout fits the columns to termWidth. IN goes first (a few
// tokens per message, rarely what a reader is after), then C_WR, C_RD and
// OUT. termWidth <= 0 means unknown, and keeps every column.
func newBreakdownLayout(agentWidth, termWidth int) breakdownLayout {
	if agentWidth > 0 {
		agentWidth = max(agentWidth, len("AGENT"))
	}
	l := breakdownLayout{agentWidth: agentWidth, in: true, out: true, cacheWrite: true, cacheRead: true}
	if termWidth <= 0 {
		return l
	}
	for _, drop := range []*bool{&l.in, &l.cacheWrite, &l.cacheRead, &l.out} {
		if l.width() <= termWidth {
			break
		}
		*drop = false
	}
	return l
}

// width is the display width of a row drawn with this layout, indent included.
func (l breakdownLayout) width() int {
	w := bdIndent + bdIndexWidth + bdGap + bdTimeWidth + bdGap + bdModelWidth + bdGap + bdCostWidth
	if l.agentWidth > 0 {
		w += bdGap + l.agentWidth
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
		index:      fmt.Sprintf("%-*s", bdIndexWidth, "#"),
		time:       fmt.Sprintf("%-*s", bdTimeWidth, "TIME"),
		agent:      fmt.Sprintf("%-*s", l.agentWidth, "AGENT"),
		model:      fmt.Sprintf("%-*s", bdModelWidth, "MODEL"),
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
