// Package render provides the presentation-layer string helpers shared by the
// static formatters (internal/formatter) and the live TUI (internal/tui) so
// both render costs, tokens, durations, and section chrome identically.
//
// Every helper is a pure function of its arguments plus the internal/styles
// palette — no I/O, no hidden state — so a given call yields the same bytes in
// a piped `show` and a live `watch`.
package render

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
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

	if r := []rune(label); len(r) > maxLen {
		label = string(r[:maxLen-1]) + "…"
	}
	return label
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
// Callers still pass the result through "%-Ns". That works because an uncut
// label is pure ASCII (bytes == columns, so fmt pads it correctly) while a cut
// one ends in a 3-byte, 1-column ellipsis (bytes > width, so fmt leaves the
// already-exact width alone).
func ClampModel(label string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(label)
	if len(r) <= width {
		return label
	}
	if width == 1 {
		return "…"
	}
	return string(r[:width-1]) + "…"
}

// ShortAgentID abbreviates an agent ID to at most 7 bytes for display. Byte
// truncation is deliberate: agent IDs come from agent-<id>.jsonl filenames and
// are ASCII, so bytes == columns and no rune can be split.
func ShortAgentID(id string) string {
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

// CostComponentLabel maps an internal cost-component key to a human-readable
// label (both cache-write TTLs collapse to "cache_write").
func CostComponentLabel(component string) string {
	switch component {
	case "input":
		return "input"
	case "output":
		return "output"
	case "cache_write_5m":
		return "cache_write"
	case "cache_write_1h":
		return "cache_write"
	case "cache_read":
		return "cache_read"
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

	rule := styles.LineHorizontal
	if noColor {
		rule = styles.AsciiRule
	}
	leftLine := strings.Repeat(rule, sideLen)
	rightLine := strings.Repeat(rule, rightLen)

	if noColor {
		return leftLine + bracketedName + rightLine
	}

	// Section name in cyan, lines in dim
	return styles.DimStyle.Render(leftLine) + "[ " + styles.SectionHeaderStyle.Render(name) + " ]" + styles.DimStyle.Render(rightLine)
}

// ContextBar renders a used(█)/free(░) progress bar of context-window usage.
// The width is fixed at 38 so the whole "Context [bar] 380.0K (38% of 1.00M)"
// line fits the 76-column live panel; a wider bar wraps and desyncs the TUI's
// fixed header/footer height math.
func ContextBar(contextSize, freeSpace int64, maxContext int, noColor bool) string {
	const barWidth = 38

	usedGlyph, freeGlyph := "█", "░"
	if noColor {
		usedGlyph, freeGlyph = styles.AsciiBarUsed, styles.AsciiBarFree
	}

	if maxContext == 0 {
		return strings.Repeat(freeGlyph, barWidth)
	}

	// Calculate proportions
	usedRatio := float64(contextSize) / float64(maxContext)
	freeRatio := float64(freeSpace) / float64(maxContext)

	// Convert to bar segments
	usedChars := int(usedRatio * float64(barWidth))
	freeChars := int(freeRatio * float64(barWidth))

	// Adjust for rounding to hit exactly barWidth
	total := usedChars + freeChars
	if total < barWidth {
		freeChars += barWidth - total
	} else if total > barWidth {
		diff := total - barWidth
		if freeChars >= diff {
			freeChars -= diff
		} else {
			diff -= freeChars
			freeChars = 0
			usedChars -= diff
		}
	}

	// Final safety: ensure non-negative for strings.Repeat
	usedChars = max(0, usedChars)
	freeChars = max(0, freeChars)
	if total := usedChars + freeChars; total < barWidth {
		freeChars += barWidth - total
	}

	usedStr := strings.Repeat(usedGlyph, usedChars)
	freeStr := strings.Repeat(freeGlyph, freeChars)

	if noColor {
		return "[" + usedStr + freeStr + "]"
	}

	// Get usage color based on percentage - only the used portion is colored
	usagePct := usedRatio * 100
	usageColor := styles.GetContextUsageColor(usagePct)

	usedStyled := lipgloss.NewStyle().Foreground(usageColor).Render(usedStr)
	freeStyled := lipgloss.NewStyle().Foreground(styles.ContextFreeColor).Render(freeStr)

	return "[" + usedStyled + freeStyled + "]"
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
func CostColored(cost float64, color lipgloss.Color, width int) string {
	return costStyledCell(cost, width, false, false, lipgloss.NewStyle().Foreground(color))
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
