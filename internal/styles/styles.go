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
	IdleDot        string // ○ idle indicator
	Warning        string // ⚠ warning prefix
	Arrow          string // → "from -> to"
	TrendUp        string // ▲ session trend increasing
	TrendDown      string // ▼ session trend decreasing
	TrendFlat      string // ═ session trend stable
	TreeBranch     string // ├─ tree connector
	TreeLast       string // └─ last tree connector
	TreeRail       string // │ tree rail past a node with more siblings below
	GroupRule      string // ── lead-in of a group or day divider
	ScrollKeys     string // ↑↓ scroll keys in help lines
	MoreAbove      string // ↑ content hidden above a viewport
	MoreBelow      string // ↓ content hidden below a viewport
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
		TrendUp, TrendDown, TrendFlat = "^", "v", "="
		TreeBranch, TreeLast, TreeRail, GroupRule = "+-", "`-", "|", "--"
		ScrollKeys, Ellipsis = "j/k", "..."
		MoreAbove, MoreBelow = "^", "v"
		IdleDot = "o"
		return
	}
	BoxTopLeft, BoxTopRight, BoxBottomLeft, BoxBottomRight = "╔", "╗", "╚", "╝"
	BoxHorizontal, BoxVertical, BoxVerticalSep, LineHorizontal = "═", "║", "│", "─"
	BarUsed, BarFree = "█", "░"
	Bullet, LiveDot, Warning, Arrow = "•", "●", "⚠", "→"
	TrendUp, TrendDown, TrendFlat = "▲", "▼", "═"
	TreeBranch, TreeLast, TreeRail, GroupRule = "├─", "└─", "│", "──"
	ScrollKeys, Ellipsis = "↑↓", "…"
	MoreAbove, MoreBelow = "↑", "↓"
	IdleDot = "○"
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

// Shared color palette. Each color carries two xterm-256 indexes: Dark is
// the one ficha was designed with, and Light takes over on a light terminal
// background. Light values keep text at 4.5:1 or better against white, and
// bars and separators at 3:1. They're palette indexes rather than hex so the
// ratio holds under both the 256-color and truecolor profiles.
//
// The background is detected once per process, by Bubble Tea's init asking
// the terminal (OSC 11) before a TUI can take stdin. Inside tmux or screen,
// which termenv doesn't ask, it goes by COLORFGBG. Anywhere else it can't
// tell, including when stdout isn't a terminal and always on Windows, it
// reads as dark, so the dark values are the fallback.
var (
	PrimaryColor   = lipgloss.AdaptiveColor{Light: "92", Dark: "99"}   // Purple
	SecondaryColor = lipgloss.AdaptiveColor{Light: "242", Dark: "245"} // Gray (dimmed but readable)
	SuccessColor   = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}   // Green
	InfoColor      = lipgloss.AdaptiveColor{Light: "23", Dark: "43"}   // Cyan
	WarningColor   = lipgloss.AdaptiveColor{Light: "94", Dark: "221"}  // Yellow
	AccentColor    = lipgloss.AdaptiveColor{Light: "162", Dark: "212"} // Pink
	ErrorColor     = lipgloss.AdaptiveColor{Light: "160", Dark: "196"} // Red
	OrangeColor    = lipgloss.AdaptiveColor{Light: "130", Dark: "214"} // Orange
	BlueColor      = lipgloss.AdaptiveColor{Light: "25", Dark: "75"}   // Blue

	// NeutralColor sits near the default foreground, for values that
	// shouldn't draw the eye.
	NeutralColor = lipgloss.AdaptiveColor{Light: "238", Dark: "252"}
	// SoftTextColor is for text a step behind the figures it labels, such
	// as breakdown's stats line.
	SoftTextColor = lipgloss.AdaptiveColor{Light: "239", Dark: "250"}
	// NoteColor is for explanatory notes under a value: less prominent than
	// NeutralColor, more than SecondaryColor.
	NoteColor = lipgloss.AdaptiveColor{Light: "241", Dark: "248"}
	// SeparatorColor is for inline separators that should recede behind
	// the text around them.
	SeparatorColor = lipgloss.AdaptiveColor{Light: "246", Dark: "240"}
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

// Highlight style for recently changed values. On a dark background it's a
// brighter gold than WarningColor. On white no gold reaches 4.5:1, so both
// take the same dark amber.
var (
	HighlightColor = lipgloss.AdaptiveColor{Light: "94", Dark: "220"} // Bright yellow/gold

	HighlightStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(HighlightColor)
)

// Dim style for extra decimal precision in costs
var DimStyle = lipgloss.NewStyle().
	Foreground(SecondaryColor)

// Model-specific colors (by tier)
var (
	FableColor  = lipgloss.AdaptiveColor{Light: "162", Dark: "213"} // Pink/Magenta - flagship tier
	OpusColor   = PrimaryColor                                      // Purple - premium tier
	SonnetColor = BlueColor                                         // Blue - mid tier
	HaikuColor  = InfoColor                                         // Cyan/Teal - lightweight tier
)

// Token type colors
var (
	OutputTokenColor     = BlueColor   // Light blue
	CacheWriteTokenColor = OrangeColor // Warm orange - cost investment
	CacheReadTokenColor  = InfoColor   // Cyan - efficiency/savings
)

// Context usage level colors (thresholds based on ~75-78% compaction trigger)
var (
	ContextLowColor      = SuccessColor // Green - 0-65%
	ContextHighColor     = OrangeColor  // Orange - 65-75% (approaching compaction)
	ContextCriticalColor = ErrorColor   // Red - 75%+ (compaction territory)
	// Bright gray - clearly visible free space. The light value is darker
	// than a 3:1 bar needs because ░ is a stipple, inked at a fraction of
	// its color.
	ContextFreeColor = lipgloss.AdaptiveColor{Light: "241", Dark: "252"}
)

// GetContextUsageColor returns the appropriate color based on context usage percentage.
// Thresholds aligned with Claude Code's ~75-78% auto-compaction trigger.
func GetContextUsageColor(usagePct float64) lipgloss.AdaptiveColor {
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
var AgentColors = []lipgloss.AdaptiveColor{
	AccentColor, // Pink - A1
	OrangeColor, // Orange - A2
	// Yellow - A3. On white WarningColor's amber is too close to OrangeColor
	// to tell two agents apart, so this slot takes an olive instead.
	{Light: "58", Dark: "221"},
	BlueColor, // Blue - A4
	InfoColor, // Cyan - A5
}

// GetAgentColor returns a color for the given agent ID (cycles through palette).
// Ordinal IDs ("1", "2" — the [An] markers) walk the palette in order; any
// other ID (real agent IDs like "a1b2c3d") hashes to a stable palette entry so
// distinct agents usually get distinct colors. The numeric path requires the
// WHOLE string to be a number — a digit-prefixed hash like "3f2a" must hash,
// not masquerade as ordinal 3.
func GetAgentColor(agentID string) lipgloss.AdaptiveColor {
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
func GetModelColor(modelName string) lipgloss.AdaptiveColor {
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
func GetCostGradientColor(cost, minCost, maxCost float64) lipgloss.AdaptiveColor {
	// Handle edge cases
	if maxCost <= minCost {
		return NeutralColor // Single value - neutral white
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
		return NeutralColor // Neutral white - cheap, unobtrusive
	}
	if normalized < 0.75 {
		return WarningColor // Yellow (221)
	}
	if normalized < 0.9 {
		return OrangeColor
	}
	return ErrorColor // Red (196)
}
