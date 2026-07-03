// Package render holds the shared, presentation-layer string helpers used by
// both the static formatters (internal/formatter) and the live TUI
// (internal/tui). Before this package existed, ~23 of these helpers were
// copy-pasted between internal/formatter/table.go and internal/tui/app.go and
// had silently diverged (see audit findings DUP-1/DUP-2): different session-ID
// truncation, different progress-bar widths, and different COST BY MODEL
// ordering between `show` and `watch`. Centralizing them here gives one
// canonical behavior per helper so the static and live views can no longer
// drift apart.
//
// Everything here is a pure function of its arguments (plus the shared
// internal/styles palette); nothing does I/O. Styling is driven by the same
// styles.* values both callers already used, so moving a helper here produces
// byte-identical output.
package render

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/pricing"
	"github.com/bardisty/ccusage/internal/styles"
	"github.com/charmbracelet/lipgloss"
)

// Cost formats a cost value in plain text with 6 decimal places ("$123.456789").
func Cost(cost float64) string {
	return fmt.Sprintf("$%.6f", cost)
}

// Number formats a token count compactly: "1.23M", "45.6K", or the raw integer.
func Number(n int64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.2fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// Duration formats a span with hours as the largest unit. Use it for
// single-session and per-message durations, which rarely exceed a day.
func Duration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// DurationLong formats a span that can roll up to days. It is the single
// day-aware formatter shared by the aggregate headers of `summary` and
// `global` (audit finding CLI-6): summary previously showed "104h 45m" while
// global showed "4d" for comparable spans.
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

// TruncateID shortens an ID to at most maxLen characters with a hard prefix
// cut (no ellipsis). The two callers deliberately pass different budgets: the
// static session header has room for the full 36-char UUID (maxLen 40), while
// the compact live TUI header shows an 8-char prefix. Parameterizing the
// length keeps one implementation without forcing both views to the same width
// (audit finding DUP-2: formatter kept 40, TUI kept 8).
func TruncateID(id string, maxLen int) string {
	if maxLen < 0 {
		maxLen = 0
	}
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen]
}

// CacheTokensByTTL returns the 5-minute and 1-hour cache-write token counts.
// Falls back to treating all cache-creation tokens as 5m when the detailed
// per-TTL breakdown is unavailable.
func CacheTokensByTTL(usage models.TokenUsage) (int64, int64) {
	if usage.CacheCreation != nil {
		return usage.CacheCreation.Ephemeral5mInputTokens, usage.CacheCreation.Ephemeral1hInputTokens
	}
	return usage.CacheCreationInputTokens, 0
}

// PrimaryModel returns the display name of the dominant model (by highest
// cost) in a cost-by-model map, or "-" when there is no model data.
func PrimaryModel(costByModel map[string]models.CostBreakdown) string {
	var maxModel string
	var maxCost float64
	for model, cost := range costByModel {
		if cost.TotalCost > maxCost {
			maxCost = cost.TotalCost
			maxModel = model
		}
	}
	if maxModel == "" {
		return "-"
	}
	return pricing.GetModelDisplayName(maxModel)
}

// OrderModelsByCost returns the model IDs of a cost-by-model map ordered by
// total cost descending, breaking ties by model ID ascending for determinism.
// This is the single canonical COST BY MODEL ordering for every view (audit
// finding DUP-2: `show`/`global` sorted alphabetically while `watch` sorted by
// cost, so the same session rendered its models in different orders).
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

	leftLine := strings.Repeat(styles.LineHorizontal, sideLen)
	rightLine := strings.Repeat(styles.LineHorizontal, rightLen)

	if noColor {
		return leftLine + bracketedName + rightLine
	}

	// Section name in cyan, lines in dim
	return styles.DimStyle.Render(leftLine) + "[ " + styles.SectionHeaderStyle.Render(name) + " ]" + styles.DimStyle.Render(rightLine)
}

// ContextBar creates a visual progress bar showing context-window usage.
// Segments: used (█), free (░). Total width is 38 characters, the single
// canonical width that fits within the 76-column live panel with its 4-space
// indent (audit finding DUP-2: the formatter used 40, which overflows the
// live panel and wraps).
func ContextBar(contextSize, freeSpace int64, maxContext int, noColor bool) string {
	const barWidth = 38

	if maxContext == 0 {
		return strings.Repeat("░", barWidth)
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

	usedStr := strings.Repeat("█", usedChars)
	freeStr := strings.Repeat("░", freeChars)

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

// CostStyled returns a cost string padded to width (ignoring ANSI codes) with
// the digits past two decimals dimmed. When highlighted, the whole value is
// rendered in the highlight color instead. Static callers pass highlighted
// false (audit finding DUP-2 unified the formatter's 3-arg form with the TUI's
// 4-arg highlight-aware form).
func CostStyled(cost float64, width int, highlighted, noColor bool) string {
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

	padding := ""
	if width > plainLen {
		padding = strings.Repeat(" ", width-plainLen)
	}

	if noColor {
		return padding + full
	}

	// Split into main ($X.XX) and extra (XXXX) parts
	dotIdx := strings.Index(full, ".")
	if dotIdx == -1 || len(full) <= dotIdx+3 {
		if highlighted {
			return padding + styles.HighlightStyle.Render(full)
		}
		return padding + full
	}

	main := full[:dotIdx+3]  // "$123.45"
	extra := full[dotIdx+3:] // "6789"

	if highlighted {
		return padding + styles.HighlightStyle.Render(main+extra)
	}
	return padding + main + styles.DimStyle.Render(extra)
}

// CostStyledGreen returns a cost string in the savings (green) style with
// padding and dimmed trailing decimals; highlighted overrides to highlight.
func CostStyledGreen(cost float64, width int, highlighted, noColor bool) string {
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

	padding := ""
	if width > plainLen {
		padding = strings.Repeat(" ", width-plainLen)
	}

	if noColor {
		return padding + full
	}

	if highlighted {
		return padding + styles.HighlightStyle.Render(full)
	}

	dotIdx := strings.Index(full, ".")
	if dotIdx == -1 || len(full) <= dotIdx+3 {
		return padding + styles.SavingsValueStyle.Render(full)
	}

	main := full[:dotIdx+3]
	extra := full[dotIdx+3:]
	return padding + styles.SavingsValueStyle.Render(main) + styles.DimStyle.Render(extra)
}

// CostStyledBoldGreen returns a cost string in the bold-green total style
// (for totals/subtotals) with padding and dimmed trailing decimals;
// highlighted overrides to highlight.
func CostStyledBoldGreen(cost float64, width int, highlighted, noColor bool) string {
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

	padding := ""
	if width > plainLen {
		padding = strings.Repeat(" ", width-plainLen)
	}

	if noColor {
		return padding + full
	}

	if highlighted {
		return padding + styles.HighlightStyle.Render(full)
	}

	dotIdx := strings.Index(full, ".")
	if dotIdx == -1 || len(full) <= dotIdx+3 {
		return padding + styles.TotalValueStyle.Render(full)
	}

	main := full[:dotIdx+3]
	extra := full[dotIdx+3:]
	return padding + styles.TotalValueStyle.Render(main) + styles.DimStyle.Render(extra)
}

// CostWithDimDecimals formats a cost to 6 decimal places with the main part
// ($X.XX) in the provided color and the trailing decimals dimmed. Used by the
// per-message breakdown rows, which color each row by its own cost gradient.
func CostWithDimDecimals(cost float64, color lipgloss.Color, width int) string {
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

	padding := ""
	if width > plainLen {
		padding = strings.Repeat(" ", width-plainLen)
	}

	dotIdx := strings.Index(full, ".")
	if dotIdx == -1 || len(full) <= dotIdx+3 {
		return padding + lipgloss.NewStyle().Foreground(color).Render(full)
	}

	main := full[:dotIdx+3]  // "$0.09"
	extra := full[dotIdx+3:] // "3528"

	mainStyle := lipgloss.NewStyle().Foreground(color)
	dimStyle := lipgloss.NewStyle().Foreground(styles.SecondaryColor)

	return padding + mainStyle.Render(main) + dimStyle.Render(extra)
}
