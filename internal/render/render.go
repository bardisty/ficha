// Package render is the pieces internal/formatter and internal/tui both draw:
// cost and token strings, durations and clock times, section headers, the
// context gauge, and model and ID labels cut to a column.
//
// Each helper is a function of its arguments plus the palette and glyph set
// in internal/styles, with no I/O, so a piped show and a live watch print the
// same bytes.
package render

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/styles"
)

// Cost formats a dollar amount for human output: two decimals from $1 up
// ("$13.65"), four below ("$0.2628"), where per-message costs live. These are
// list-price estimates, so more digits would be false precision. Machine
// output (json, csv) keeps six decimals and never goes through here.
func Cost(cost float64) string {
	return fmt.Sprintf("$%.*f", costDecimals(cost), cost)
}

// costDecimals picks Cost's precision. The cutoff is where four decimals
// would round up to 1.0000, so 0.99996 prints "$1.00", not "$1.0000".
func costDecimals(cost float64) int {
	if math.Abs(cost) >= 0.99995 {
		return 2
	}
	return 4
}

// CostCell right-aligns Cost(cost) in a width-column cell with the decimal
// point at a fixed position: a two-decimal value is followed by two spaces,
// so "$13.65  " lines up under "$0.2628". A value too wide for the cell comes
// back whole at its natural width; size the column with CostCellWidth.
func CostCell(cost float64, width int) string {
	left, value, right := costCellParts(cost, width)
	return left + value + right
}

// CostCellWidth returns the narrowest cell width, at least minWidth, that
// holds every cost as a CostCell without overflowing.
func CostCellWidth(minWidth int, costs ...float64) int {
	width := minWidth
	for _, c := range costs {
		_, value, right := costCellParts(c, 0)
		width = max(width, len(value)+len(right))
	}
	return width
}

// costCellParts splits a CostCell into its left padding, the value, and the
// right padding, so styled callers can color the value alone.
func costCellParts(cost float64, width int) (left, value, right string) {
	value = Cost(cost)
	if costDecimals(cost) == 2 {
		right = "  "
	}
	if pad := width - len(value) - len(right); pad > 0 {
		left = strings.Repeat(" ", pad)
	}
	return left, value, right
}

// Number formats a token count compactly: "1.23B", "1.23M", "45.6K", or the
// raw integer. Each unit takes over where the smaller one would round up to
// 1000, so 999,960 prints "1.00M", not "1000.0K".
func Number(n int64) string {
	if n >= 999_995_000 {
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	}
	if n >= 999_950 {
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// Count formats an exact count with thousands separators ("1,523"), for
// counts a reader may compare, such as messages. Number abbreviates.
func Count(n int) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// Duration formats a span with hours as the largest unit (no day rollup), for
// single-session and per-message durations. Use DurationLong for aggregate
// spans that can reach days.
func Duration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// DurationLong rolls up to days once a span reaches 24h, for aggregate headers
// (summary, global) where a total can span weeks and "336h" would be unreadable.
// Note minutes drop their trailing seconds here, unlike Duration.
func DurationLong(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("%dd", days)
}

// Ago renders how long before now t was, coarsely: "just now", "5m ago",
// "3h ago", "12d ago", "4mo ago", "2y ago". A zero t (no timestamp) is "-".
// A t after now, from clock skew between machines, counts as just now.
func Ago(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	const day = 24 * time.Hour
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < day:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 60*day:
		return fmt.Sprintf("%dd ago", int(d/day))
	case d < 365*day:
		return fmt.Sprintf("%dmo ago", int(d/(30*day)))
	default:
		return fmt.Sprintf("%dy ago", int(d/(365*day)))
	}
}

// DateTime formats t as a local date and time, "Sep 28 18:38", with the
// year added ("Sep 28 2025 18:38") when it isn't now's year. It's the one
// absolute format every report uses for a moment in time.
func DateTime(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format(dateLayout(t, now) + " 15:04")
}

// Date is DateTime without the time: "Sep 28", or "Sep 28 2025".
func Date(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format(dateLayout(t, now))
}

func dateLayout(t, now time.Time) string {
	if t.Local().Year() != now.Local().Year() {
		return "Jan 02 2006"
	}
	return "Jan 02"
}

// Clock formats t as a local wall-clock time ("15:04:05"). Transcript
// timestamps parse as UTC, and the clocks shown next to them (the live
// header's update time, show's "Last active") are local, so every display
// site converts here. Machine output (json, csv) formats the parsed value
// directly and stays UTC.
//
// A transcript line with no timestamp parses to the zero time. Converting that
// would print the zone's year-1 offset, a real-looking time like 16:07:02, so
// it gets a placeholder of the same width instead.
func Clock(t time.Time) string {
	if t.IsZero() {
		return "--:--:--"
	}
	return t.Local().Format("15:04:05")
}

// ClockShort is Clock without seconds ("15:04"), for compact one-line summaries.
func ClockShort(t time.Time) string {
	if t.IsZero() {
		return "--:--"
	}
	return t.Local().Format("15:04")
}

// DayMarker labels a local calendar day ("Tue 29 Sep"), for the divider
// a per-message table draws where the day changes, since its rows show only
// a time.
func DayMarker(t time.Time) string {
	return t.Local().Format("Mon 02 Jan")
}

// TrendSymbol returns the glyph for a session cost trend.
func TrendSymbol(t models.TrendDirection) string {
	switch t {
	case models.TrendIncreasing:
		return styles.TrendUp
	case models.TrendDecreasing:
		return styles.TrendDown
	default:
		return styles.TrendFlat
	}
}

// SameLocalDay reports whether a and b fall on the same local calendar day.
func SameLocalDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

// TruncateID shortens an ID to a maxLen-character prefix. maxLen is a parameter
// because callers have different width budgets — the static header shows the
// full 36-char UUID, the compact live header an 8-char prefix. The cut is a
// plain prefix with no ellipsis, so a fixed maxLen yields a fixed-width column.
func TruncateID(id string, maxLen int) string {
	if maxLen < 0 {
		maxLen = 0
	}
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen]
}

// WorkflowLabel returns the group-header label shown above a workflow run's
// agents, e.g. "workflow: audit-codebase (completed)". Falls back to the run
// ID when metadata was unreadable. Long labels truncate with an ellipsis so
// the line stays within the agent-section row width.
func WorkflowLabel(meta models.WorkflowMeta) string {
	const maxLen = 48

	name := meta.Name
	if name == "" {
		name = meta.RunID
	}
	label := "workflow: " + name
	if meta.Status != "" {
		label += " (" + meta.Status + ")"
	}

	return truncateRunes(label, maxLen)
}

// CacheTokensByTTL returns the 5-minute and 1-hour cache-write token counts.
// Parser-derived usages always carry the detailed breakdown, with buckets
// summing to the flat count (parser.ExtractUsageFromMessages reconciles them),
// so for those the two returns cover every billed write token. The fallback —
// all cache-creation tokens as 5m — only serves hand-built usages, where it
// applies the same assumption pricing does.
func CacheTokensByTTL(usage models.TokenUsage) (int64, int64) {
	if usage.CacheCreation != nil {
		return usage.CacheCreation.Ephemeral5mInputTokens, usage.CacheCreation.Ephemeral1hInputTokens
	}
	return usage.CacheCreationInputTokens, 0
}

// ClampModel fits a model label into width display columns, cutting with an
// ellipsis when it must. Catalog display names top out at 10 columns and pass
// through untouched; an unknown model falls back to its raw ID
// ("claude-opus-4-9-20260101" is 24 columns) and would otherwise push every
// field after the MODEL column out of alignment — fmt's "%-Ns" pads but never
// truncates.
//
// A raw ID that has to be cut loses its "claude-" prefix first: every
// Anthropic ID shares it, so it spends the columns on the one part that
// doesn't tell models apart ("nova-9-202…" rather than "claude-no…").
//
// A display name's last word is its version, and a cut inside it would name a
// version that doesn't exist ("Fable 5…" for Fable 5.1), so the version goes
// whole: "Fable…".
//
// Callers still pass the result through "%-Ns". fmt pads by rune count, and a
// cut label is at most width runes whichever ellipsis the glyph set uses ("…"
// or "..."), so fmt only ever pads it.
func ClampModel(label string, width int) string {
	if width <= 0 {
		return ""
	}
	if utf8.RuneCountInString(label) <= width {
		return label
	}
	label = strings.TrimPrefix(label, "claude-")
	if utf8.RuneCountInString(label) <= width {
		return label
	}
	kept := width - utf8.RuneCountInString(styles.Ellipsis)
	if i := strings.LastIndexByte(label, ' '); i > 0 && kept >= utf8.RuneCountInString(label[:i]) {
		return strings.TrimRight(label[:i], " ") + styles.Ellipsis
	}
	return truncateRunes(label, width)
}

// truncateRunes cuts s to at most width runes, ending a cut in the glyph
// set's ellipsis. A cut that lands after a space drops the space too, so the
// ellipsis sits against the word it cut ("Opus…", not "Opus …"). A width
// narrower than the ellipsis gets as much of the ellipsis as fits.
func truncateRunes(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	ell := []rune(styles.Ellipsis)
	if width <= len(ell) {
		return string(ell[:width])
	}
	return strings.TrimRight(string(r[:width-len(ell)]), " ") + styles.Ellipsis
}

// ShortAgentID abbreviates an agent ID to at most 7 bytes for display. Byte
// truncation is deliberate: agent IDs come from agent-<id>.jsonl filenames and
// are ASCII, so bytes == columns and no rune can be split.
//
// Claude Code names every agent file agent-a<16 hex>, so a long ID's leading
// "a" is a constant. Dropping it gives all seven display characters to the
// part that tells agents apart, and keeps the "[A" marker prefix from reading
// as a doubled letter. Short IDs keep every character.
func ShortAgentID(id string) string {
	if len(id) > 7 && id[0] == 'a' {
		id = id[1:]
	}
	if len(id) > 7 {
		return id[:7]
	}
	return id
}

// PrimaryModel returns the display name of the dominant model (by highest
// cost, ties broken by ID ascending) in a cost-by-model map, or "-" only when
// the map is empty. Delegating to OrderModelsByCost keeps the table's pick
// identical to the machine formats' primaryModelID by construction — including
// for all-zero-cost maps, where a hand-rolled max scan can fail to seed and
// report no model while json/csv name one.
func PrimaryModel(costByModel map[string]models.CostBreakdown) string {
	ordered := OrderModelsByCost(costByModel)
	if len(ordered) == 0 {
		return "-"
	}
	return pricing.GetModelDisplayName(ordered[0])
}

// OrderModelsByCost returns a cost-by-model map's IDs ordered by total cost
// descending. Ties break by ID ascending so the output is stable run to run —
// Go randomizes map iteration order, so the tiebreak is what makes it
// deterministic, not a cosmetic detail.
func OrderModelsByCost(costByModel map[string]models.CostBreakdown) []string {
	ids := make([]string, 0, len(costByModel))
	for id := range costByModel {
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(i, j int) bool {
		ci, cj := costByModel[ids[i]].TotalCost, costByModel[ids[j]].TotalCost
		if ci != cj {
			return ci > cj
		}
		return ids[i] < ids[j]
	})
	return ids
}

// CostComponentLabel maps an internal cost-component key to the name the
// cost tables give that row, with the cache-write TTL kept since a row's
// cost depends on it.
func CostComponentLabel(component string) string {
	switch component {
	case "input":
		return "Input"
	case "output":
		return "Output"
	case "cache_write_5m":
		return "Cache write 5m"
	case "cache_write_1h":
		return "Cache write 1h"
	case "cache_read":
		return "Cache read"
	default:
		return component
	}
}

// SectionHeader renders a centered, bracketed section header padded to width
// with horizontal rules, e.g. "────────[ COST BY MODEL ]────────".
func SectionHeader(name string, width int, noColor bool) string {
	if width < 20 {
		width = 76
	}

	bracketedName := "[ " + name + " ]"
	nameLen := len(bracketedName)
	sideLen := max((width-nameLen)/2, 0)
	rightLen := max(width-sideLen-nameLen, 0)

	leftLine := strings.Repeat(styles.LineHorizontal, sideLen)
	rightLine := strings.Repeat(styles.LineHorizontal, rightLen)

	if noColor {
		return leftLine + bracketedName + rightLine
	}

	// Section name in cyan, lines in dim
	return styles.DimStyle.Render(leftLine) + "[ " + styles.SectionHeaderStyle.Render(name) + " ]" + styles.DimStyle.Render(rightLine)
}

// ContextBar renders a context-window bar of width cells between brackets:
// used (█) then free (░), with a tick (│) at the ~compaction point so the
// warning survives --no-color. Only the used part carries the usage color.
func ContextBar(contextSize int64, maxContext, width int, noColor bool) string {
	width = max(width, 1)
	used := 0
	if maxContext > 0 {
		used = int(float64(contextSize) / float64(maxContext) * float64(width))
	}
	used = min(max(used, 0), width)
	tick := min(int(math.Round(CompactionPct/100*float64(width))), width-1)

	usedStyle := lipgloss.NewStyle()
	freeStyle := lipgloss.NewStyle().Foreground(styles.ContextFreeColor)
	tickStyle := lipgloss.NewStyle().Bold(true)
	if maxContext > 0 {
		usedStyle = usedStyle.Foreground(styles.GetContextUsageColor(float64(contextSize) / float64(maxContext) * 100))
	}
	paint := func(st lipgloss.Style, s string) string {
		if noColor || s == "" {
			return s
		}
		return st.Render(s)
	}

	usedCells := func(n int) string { return strings.Repeat(styles.BarUsed, n) }
	freeCells := func(n int) string { return strings.Repeat(styles.BarFree, n) }
	var bar string
	if tick < used {
		bar = paint(usedStyle, usedCells(tick)) + paint(tickStyle, styles.BoxVerticalSep) +
			paint(usedStyle, usedCells(used-tick-1)) + paint(freeStyle, freeCells(width-used))
	} else {
		bar = paint(usedStyle, usedCells(used)) + paint(freeStyle, freeCells(tick-used)) +
			paint(tickStyle, styles.BoxVerticalSep) + paint(freeStyle, freeCells(width-tick-1))
	}
	return "[" + bar + "]"
}

// CompactionPct is where Claude Code auto-compacts, as a share of the context
// window (see styles.GetContextUsageColor, which turns red at the same
// point). It is approximate, and unconfirmed for 1M windows, so the UI
// always calls it "~compaction".
const CompactionPct = 75.0

// minContextBar is the narrowest bar worth drawing, brackets excluded.
const minContextBar = 10

// ContextGauge renders the context line's content and the note under it,
// fitted to width columns:
//
//	95% [████████████████████████████│░░░░░░] 190.1K / 200.0K
//	past ~compaction at 75% (main session, last request)
//
// The percentage leads because it is the reading people look for; the bar
// takes whatever width is left. When the line can't fit, whole fields drop
// (the token counts, then the bar) rather than clipping inside a number.
// highlight marks the token counts as just changed.
func ContextGauge(contextSize int64, maxContext, width int, noColor, highlight bool) (line, note string) {
	pct := 0.0
	if maxContext > 0 {
		pct = float64(contextSize) / float64(maxContext) * 100
	}
	pctStr := fmt.Sprintf("%3.0f%%", pct)
	value := Number(contextSize) + " / " + Number(int64(maxContext))

	style := func(st lipgloss.Style, s string) string {
		if noColor {
			return s
		}
		return st.Render(s)
	}
	pctStyled := style(lipgloss.NewStyle().Foreground(styles.GetContextUsageColor(pct)), pctStr)
	valueStyled := value
	if highlight {
		valueStyled = style(styles.HighlightStyle, value)
	}

	// "95% [" + bar + "] " + value
	inner := width - len(pctStr) - 1 - 2 - 1 - lipgloss.Width(value)
	switch {
	case inner >= minContextBar:
		line = pctStyled + " " + ContextBar(contextSize, maxContext, inner, noColor) + " " + valueStyled
	case width-len(pctStr)-3 >= minContextBar:
		line = pctStyled + " " + ContextBar(contextSize, maxContext, width-len(pctStr)-3, noColor)
	case width >= len(pctStr)+1+lipgloss.Width(value):
		line = pctStyled + " " + valueStyled
	default:
		line = pctStyled
	}

	return line, contextNote(contextSize, maxContext, width-1)
}

// AgentContextWidth is the width of AgentContextCell: "100% ctx".
const AgentContextWidth = 8

// AgentContextCell is an agent row's context reading, " 38% ctx",
// right-aligned in AgentContextWidth columns, or blanks when c is nil. It's
// dim like the message count beside it until the gauge would turn orange, so
// only an agent nearing compaction stands out.
func AgentContextCell(c *models.ContextUsage, noColor bool) string {
	if c == nil {
		return strings.Repeat(" ", AgentContextWidth)
	}
	cell := fmt.Sprintf("%3.0f%% ctx", c.Percent)
	if len(cell) < AgentContextWidth {
		cell = strings.Repeat(" ", AgentContextWidth-len(cell)) + cell
	}
	switch {
	case noColor:
		return cell
	case c.Percent < styles.ContextHighPct:
		return styles.DimStyle.Render(cell)
	default:
		return lipgloss.NewStyle().Foreground(styles.GetContextUsageColor(c.Percent)).Render(cell)
	}
}

// contextNote is the line under the gauge, laid out one column in (under
// the percentage's digits) within width. It sheds the scope before the
// reading, and a shorter reading before nothing at all, so a narrow
// terminal never cuts the headroom figure or the warning mid-word.
func contextNote(contextSize int64, maxContext, width int) string {
	if maxContext <= 0 {
		return ""
	}
	const scope = " (main session, last request)"
	var forms []string
	if headroom := int64(float64(maxContext)*CompactionPct/100) - contextSize; headroom > 0 {
		reading := Number(headroom) + " to ~compaction"
		forms = []string{reading + scope, reading, Number(headroom) + " left"}
	} else {
		reading := fmt.Sprintf("past ~compaction at %.0f%%", CompactionPct)
		forms = []string{reading + scope, reading, "past ~compaction"}
	}
	for _, f := range forms {
		if lipgloss.Width(f) <= width {
			return f
		}
	}
	return ""
}

// CostStyled renders a CostCell with the value in the default color. Only
// the value is styled, never the padding, so escape codes can't throw off the
// column. highlighted flags a value that just changed in the live view;
// static callers pass false.
func CostStyled(cost float64, width int, highlighted, noColor bool) string {
	return costStyledCell(cost, width, highlighted, noColor, lipgloss.NewStyle())
}

// CostStyledGreen is CostStyled in the savings (green) style, for cache-savings
// figures.
func CostStyledGreen(cost float64, width int, highlighted, noColor bool) string {
	return costStyledCell(cost, width, highlighted, noColor, styles.SavingsValueStyle)
}

// CostStyledBoldGreen is CostStyled in the bold-green total style, for totals
// and subtotals.
func CostStyledBoldGreen(cost float64, width int, highlighted, noColor bool) string {
	return costStyledCell(cost, width, highlighted, noColor, styles.TotalValueStyle)
}

// CostColored renders a CostCell in an arbitrary foreground color, for columns
// that color each value by its own magnitude (the per-message breakdown, the
// global and summary tables).
func CostColored(cost float64, fg color.Color, width int) string {
	return costStyledCell(cost, width, false, false, lipgloss.NewStyle().Foreground(fg))
}

func costStyledCell(cost float64, width int, highlighted, noColor bool, style lipgloss.Style) string {
	left, value, right := costCellParts(cost, width)
	switch {
	case noColor:
	case highlighted:
		value = styles.HighlightStyle.Render(value)
	default:
		value = style.Render(value)
	}
	return left + value + right
}

// noBreak marks a path for WrapHanging: one before the path, and one in
// place of each space in it. No path on any OS can hold a NUL, so taking
// the marks out again restores the path exactly.
const noBreak = "\x00"

// NoBreak marks a path so WrapHanging keeps it on one line, spaces and all.
// The caller knows which part of its message is the path. Finding one in
// finished text would be a guess, since a path with spaces looks like words.
// Text holding a marked path must pass through WrapHanging before it is
// printed, which takes the marks out.
func NoBreak(path string) string {
	return noBreak + strings.ReplaceAll(path, " ", noBreak)
}

// unmark returns word as it prints, and whether it holds a marked path.
func unmark(word string) (string, bool) {
	start := strings.Index(word, noBreak)
	if start < 0 {
		return word, false
	}
	return word[:start] + strings.ReplaceAll(word[start+len(noBreak):], noBreak, " "), true
}

// WrapHanging wraps each line of text to width display columns, breaking
// only between words. A continuation line is indented to where the line's
// text starts, past its leading spaces and a "Warning: " or "Note: " label,
// so a wrapped warning or note reads as one block. A word longer than the
// room left keeps a line of its own, or stays beside the label, rather than
// being cut: a URL, path or model ID must survive intact to be copied.
//
// For the same reason a path marked with NoBreak counts as one word, however
// many spaces it holds. When it doesn't fit under the indent it starts at
// the left edge instead, where it has the whole width, and where the rest of
// a path longer than that lines up with it once the terminal wraps it.
//
// A run of spaces between two words on one line is kept as it is. Lines that
// already fit come back unchanged, and a width of 0 or less wraps nothing.
func WrapHanging(text string, width int) string {
	lines := strings.SplitAfter(text, "\n")
	var sb strings.Builder
	for _, line := range lines {
		body := strings.TrimSuffix(line, "\n")
		sb.WriteString(wrapHangingLine(body, width))
		if len(body) < len(line) {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func wrapHangingLine(line string, width int) string {
	words := strings.Split(line, " ")
	for i, word := range words {
		words[i], _ = unmark(word)
	}
	if plain := strings.Join(words, " "); width <= 0 || lipgloss.Width(plain) <= width {
		return plain
	}
	text := strings.TrimLeft(line, " ")
	lead := line[:len(line)-len(text)]
	hang := len(lead)
	for _, label := range []string{"Warning: ", "Note: "} {
		if strings.HasPrefix(text, label) {
			hang += len(label)
		}
	}
	indent := strings.Repeat(" ", hang)
	var sb strings.Builder
	sb.WriteString(lead)
	col := len(lead)
	for first := true; ; first = false {
		rest := strings.TrimLeft(text, " ")
		gap := text[:len(text)-len(rest)]
		if rest == "" {
			break
		}
		end := strings.IndexByte(rest, ' ')
		if end < 0 {
			end = len(rest)
		}
		word, marked := unmark(rest[:end])
		text = rest[end:]
		w := lipgloss.Width(word)
		switch {
		case first:
			sb.WriteString(word)
			col += w
		// Right after the label, a break would put the word at the same
		// column on the next line, leaving the label alone for nothing.
		case col+len(gap)+w <= width, col+1 == hang:
			sb.WriteString(gap + word)
			col += len(gap) + w
		case marked && hang+w > width:
			sb.WriteString("\n" + word)
			col = w
		default:
			sb.WriteString("\n" + indent + word)
			col = hang + w
		}
	}
	return sb.String()
}
