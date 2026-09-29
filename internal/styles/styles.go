package styles

import (
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Glyphs. ficha draws with Unicode box-drawing characters and symbols by
// default; SetASCII swaps in plain-ASCII stand-ins for terminals without
// UTF-8 (--ascii). Glyphs are independent of color: --no-color keeps them.
//
// The choice is process-wide, like lipgloss's color profile, and is made once
// at startup before anything renders. Every stand-in except Arrow, ScrollKeys
// and Ellipsis is as wide as its Unicode glyph, so fixed-width columns hold
// in both sets; those three only appear inline, where width doesn't matter,
// or in helpers that measure the ellipsis they append.
var (
	BoxTopLeft     string // ╔
	BoxTopRight    string // ╗
	BoxBottomLeft  string // ╚
	BoxBottomRight string // ╝
	BoxHorizontal  string // ═ heavy rule and panel border
	BoxVertical    string // ║ panel side
	BoxVerticalSep string // │ field separator
	LineHorizontal string // ─ light rule
	BarUsed        string // █ filled progress-bar cell
	BarFree        string // ░ empty progress-bar cell
	Bullet         string // • help-line separator
	LiveDot        string // ● live indicator
	Warning        string // ⚠ warning prefix
	Arrow          string // → "from -> to"
	RowUp          string // ↑ per-message cost rose
	RowDown        string // ↓ per-message cost fell
	RowFlat        string // · per-message cost held
	TrendUp        string // ▲ session trend increasing
	TrendDown      string // ▼ session trend decreasing
	TrendFlat      string // ═ session trend stable
	TreeBranch     string // ├─ tree connector
	TreeLast       string // └─ last tree connector
	GroupRule      string // ── lead-in of a group or day divider
	ScrollKeys     string // ↑↓ scroll keys in help lines
	Ellipsis       string // … truncation marker
)

var asciiGlyphs bool

func init() { SetASCII(false) }

// SetASCII selects the ASCII glyph set (true) or the Unicode one (false).
func SetASCII(ascii bool) {
	asciiGlyphs = ascii
	if ascii {
		BoxTopLeft, BoxTopRight, BoxBottomLeft, BoxBottomRight = "+", "+", "+", "+"
		BoxHorizontal, BoxVertical, BoxVerticalSep, LineHorizontal = "=", "|", "|", "-"
		BarUsed, BarFree = "#", "-"
		Bullet, LiveDot, Warning, Arrow = "|", "*", "!", "->"
		RowUp, RowDown, RowFlat = "^", "v", "."
		TrendUp, TrendDown, TrendFlat = "^", "v", "="
		TreeBranch, TreeLast, GroupRule = "+-", "`-", "--"
		ScrollKeys, Ellipsis = "j/k", "..."
		return
	}
	BoxTopLeft, BoxTopRight, BoxBottomLeft, BoxBottomRight = "╔", "╗", "╚", "╝"
	BoxHorizontal, BoxVertical, BoxVerticalSep, LineHorizontal = "═", "║", "│", "─"
	BarUsed, BarFree = "█", "░"
	Bullet, LiveDot, Warning, Arrow = "•", "●", "⚠", "→"
	RowUp, RowDown, RowFlat = "↑", "↓", "·"
	TrendUp, TrendDown, TrendFlat = "▲", "▼", "═"
	TreeBranch, TreeLast, GroupRule = "├─", "└─", "──"
	ScrollKeys, Ellipsis = "↑↓", "…"
}

// ASCII reports whether the ASCII glyph set is active.
func ASCII() bool { return asciiGlyphs }

// ASCIIChart maps a sparkline's block characters (U+2581..U+2588, as drawn
// by ntcharts' column mode) to ASCII by height: low blocks to ".", middle
// ones to ":", the full block to "#". Everything else, escape codes
// included, passes through.
func ASCIIChart(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '▁' && r <= '▃':
			return '.'
		case r >= '▄' && r <= '▇':
			return ':'
		case r == '█':
			return '#'
		}
		return r
	}, s)
}

// Shared color palette
var (
	PrimaryColor   = lipgloss.Color("99")  // Purple
	SecondaryColor = lipgloss.Color("245") // Gray (dimmed but readable)
	SuccessColor   = lipgloss.Color("42")  // Green
	InfoColor      = lipgloss.Color("43")  // Cyan
	WarningColor   = lipgloss.Color("221") // Yellow
	AccentColor    = lipgloss.Color("212") // Pink
	ErrorColor     = lipgloss.Color("196") // Red
)

// Header styles for titles and section headers
var (
	// HeaderStyle uses bold white for clean, minimal section headers
	HeaderStyle = lipgloss.NewStyle().
			Bold(true)

	// HeroCostStyle for the prominent centered total cost display
	HeroCostStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(SuccessColor)

	// SectionHeaderStyle for bracketed section headers (IBM 3279 convention: cyan for static text)
	SectionHeaderStyle = lipgloss.NewStyle().
				Foreground(InfoColor)

	// PanelBorderStyle for header panel box-drawing characters
	PanelBorderStyle = lipgloss.NewStyle().
				Foreground(SecondaryColor)
)

// Table styles for borders and cells
// Note: Width/Padding removed - use fmt.Sprintf for consistent formatting
var (
	BorderStyle = lipgloss.NewStyle().
		Foreground(SecondaryColor)
)

// Total row styles for summary lines
var (
	TotalValueStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(SuccessColor)
)

// Savings styles for cache savings display
var (
	SavingsLabelStyle = lipgloss.NewStyle().
				Foreground(InfoColor)

	SavingsValueStyle = lipgloss.NewStyle().
				Foreground(InfoColor)
)

// Status and footer styles
var (
	FooterStyle = lipgloss.NewStyle().
		Foreground(SecondaryColor)
)

// Live mode styles
var (
	// LiveIndicatorStyle uses green dot for active/healthy status (not red which implies error)
	LiveIndicatorStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(SuccessColor)
)

// Spinner style
var (
	SpinnerStyle = lipgloss.NewStyle().
		Foreground(PrimaryColor)
)

// Highlight style for recently changed values
var (
	HighlightColor = lipgloss.Color("220") // Bright yellow/gold

	HighlightStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(HighlightColor)
)

// Dim style for extra decimal precision in costs
var DimStyle = lipgloss.NewStyle().
	Foreground(SecondaryColor)

// Model-specific colors (by tier)
var (
	FableColor  = lipgloss.Color("213") // Pink/Magenta - flagship tier
	OpusColor   = lipgloss.Color("99")  // Purple - premium tier
	SonnetColor = lipgloss.Color("75")  // Blue - mid tier
	HaikuColor  = lipgloss.Color("43")  // Cyan/Teal - lightweight tier
)

// Token type colors
var (
	OutputTokenColor     = lipgloss.Color("75")  // Light blue
	CacheWriteTokenColor = lipgloss.Color("214") // Warm orange - cost investment
	CacheReadTokenColor  = lipgloss.Color("43")  // Cyan - efficiency/savings
)

// Context usage level colors (thresholds based on ~75-78% compaction trigger)
var (
	ContextLowColor      = SuccessColor          // Green - 0-65%
	ContextHighColor     = lipgloss.Color("214") // Orange - 65-75% (approaching compaction)
	ContextCriticalColor = ErrorColor            // Red - 75%+ (compaction territory)
	ContextFreeColor     = lipgloss.Color("252") // Bright gray - clearly visible free space
)

// GetContextUsageColor returns the appropriate color based on context usage percentage.
// Thresholds aligned with Claude Code's ~75-78% auto-compaction trigger.
func GetContextUsageColor(usagePct float64) lipgloss.Color {
	switch {
	case usagePct >= 75:
		return ContextCriticalColor // Red - compaction territory
	case usagePct >= 65:
		return ContextHighColor // Orange - approaching compaction
	default:
		return ContextLowColor // Green - comfortable
	}
}

// Agent marker colors - cycling palette for distinguishing sub-agents
var AgentColors = []lipgloss.Color{
	lipgloss.Color("212"), // Pink - A1
	lipgloss.Color("214"), // Orange - A2
	lipgloss.Color("221"), // Yellow - A3
	lipgloss.Color("75"),  // Blue - A4
	lipgloss.Color("43"),  // Cyan - A5
}

// GetAgentColor returns a color for the given agent ID (cycles through palette).
// Ordinal IDs ("1", "2" — the [An] markers) walk the palette in order; any
// other ID (real agent IDs like "a1b2c3d") hashes to a stable palette entry so
// distinct agents usually get distinct colors. The numeric path requires the
// WHOLE string to be a number — a digit-prefixed hash like "3f2a" must hash,
// not masquerade as ordinal 3.
func GetAgentColor(agentID string) lipgloss.Color {
	if agentID == "" {
		return SecondaryColor
	}
	if num, err := strconv.Atoi(agentID); err == nil && num >= 1 {
		return AgentColors[(num-1)%len(AgentColors)]
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(agentID))
	return AgentColors[h.Sum32()%uint32(len(AgentColors))] //nolint:gosec // the palette length is a small positive constant
}

// GetModelColor returns the tier-appropriate color for a model name or ID.
// Works with both display names ("Opus 4.5") and raw IDs ("claude-opus-4-6")
// via case-insensitive substring matching.
func GetModelColor(modelName string) lipgloss.Color {
	switch {
	case contains(modelName, "Fable"), contains(modelName, "Mythos"):
		return FableColor
	case contains(modelName, "Opus"):
		return OpusColor
	case contains(modelName, "Sonnet"):
		return SonnetColor
	case contains(modelName, "Haiku"):
		return HaikuColor
	default:
		return SecondaryColor
	}
}

// contains checks if s contains substr (case-insensitive)
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// GetCostGradientColor returns a color based on cost position in the session's range
// Neutral (cheap) -> Yellow -> Orange -> Red (expensive)
// Only expensive items "heat up" - cheap items stay unobtrusive
func GetCostGradientColor(cost, minCost, maxCost float64) lipgloss.Color {
	// Handle edge cases
	if maxCost <= minCost {
		return lipgloss.Color("252") // Single value - neutral white
	}

	// Normalize to 0.0-1.0 range
	normalized := (cost - minCost) / (maxCost - minCost)

	// Clamp to valid range
	if normalized < 0 {
		normalized = 0
	}
	if normalized > 1 {
		normalized = 1
	}

	// Neutral -> Warm gradient (only expensive items draw attention)
	// 0-50%: neutral white (blends in)
	// 50-75%: yellow (starting to warm up)
	// 75-90%: orange (getting hot)
	// 90%+: red (expensive!)
	if normalized < 0.5 {
		return lipgloss.Color("252") // Neutral white - cheap, unobtrusive
	}
	if normalized < 0.75 {
		return WarningColor // Yellow (221)
	}
	if normalized < 0.9 {
		return lipgloss.Color("214") // Orange
	}
	return ErrorColor // Red (196)
}
