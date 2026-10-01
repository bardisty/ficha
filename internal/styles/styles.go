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

func init() {
	SetASCII(false)
	SetDark(true)
}

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

// Shared color palette. Each color carries two xterm-256 indexes: dark is
// the one ficha was designed with, and light takes over on a light terminal
// background. Light values keep text at 4.5:1 or better against white, and
// bars and separators at 3:1. They're palette indexes rather than hex so the
// ratio holds under both the 256-color and truecolor profiles.
//
// The background is detected once per process, by Bubble Tea's init asking
// the terminal (OSC 11) before a TUI can take stdin. Inside tmux or screen,
// which termenv doesn't ask, it goes by COLORFGBG. Anywhere else it can't
// tell, including when stdout isn't a terminal and always on Windows, it
// reads as dark, so the dark values are the fallback. cmd hands the answer
// to SetDark every run.
type pair struct{ dark, light uint8 }

func (p pair) on(dark bool) lipgloss.TerminalColor {
	if dark {
		return lipgloss.Color(strconv.Itoa(int(p.dark)))
	}
	return lipgloss.Color(strconv.Itoa(int(p.light)))
}

var palette = struct {
	primary, secondary, success, info, warning, accent, err, orange, blue pair
	neutral, softText, note, separator                                    pair
	highlight, fable, contextFree, agentOlive                             pair
}{
	primary:   pair{dark: 99, light: 92},   // Purple
	secondary: pair{dark: 245, light: 242}, // Gray (dimmed but readable)
	success:   pair{dark: 42, light: 28},   // Green
	info:      pair{dark: 43, light: 23},   // Cyan
	warning:   pair{dark: 221, light: 94},  // Yellow
	accent:    pair{dark: 212, light: 162}, // Pink
	err:       pair{dark: 196, light: 160}, // Red
	orange:    pair{dark: 214, light: 130}, // Orange
	blue:      pair{dark: 75, light: 25},   // Blue

	neutral:   pair{dark: 252, light: 238},
	softText:  pair{dark: 250, light: 239},
	note:      pair{dark: 248, light: 241},
	separator: pair{dark: 240, light: 246},

	// On a dark background the highlight is a brighter gold than warning.
	// On white no gold reaches 4.5:1, so both take the same dark amber.
	highlight: pair{dark: 220, light: 94},
	fable:     pair{dark: 213, light: 162}, // Pink/Magenta
	// Bright gray, clearly visible free space. The light value is darker
	// than a 3:1 bar needs because ░ is a stipple, inked at a fraction of
	// its color.
	contextFree: pair{dark: 252, light: 241},
	// Yellow on a dark background. On white, warning's amber is too close
	// to orange to tell two agents apart, so this slot takes an olive.
	agentOlive: pair{dark: 221, light: 58},
}

// The palette as SetDark last resolved it.
var (
	PrimaryColor   lipgloss.TerminalColor // Purple
	SecondaryColor lipgloss.TerminalColor // Gray (dimmed but readable)
	SuccessColor   lipgloss.TerminalColor // Green
	InfoColor      lipgloss.TerminalColor // Cyan
	WarningColor   lipgloss.TerminalColor // Yellow
	AccentColor    lipgloss.TerminalColor // Pink
	ErrorColor     lipgloss.TerminalColor // Red
	OrangeColor    lipgloss.TerminalColor // Orange
	BlueColor      lipgloss.TerminalColor // Blue

	// NeutralColor sits near the default foreground, for values that
	// shouldn't draw the eye.
	NeutralColor lipgloss.TerminalColor
	// SoftTextColor is for text a step behind the figures it labels, such
	// as breakdown's stats line.
	SoftTextColor lipgloss.TerminalColor
	// NoteColor is for explanatory notes under a value: less prominent than
	// NeutralColor, more than SecondaryColor.
	NoteColor lipgloss.TerminalColor
	// SeparatorColor is for inline separators that should recede behind
	// the text around them.
	SeparatorColor lipgloss.TerminalColor

	// HighlightColor is for recently changed values.
	HighlightColor lipgloss.TerminalColor

	// Model colors, by tier.
	FableColor  lipgloss.TerminalColor // Pink/Magenta - flagship tier
	OpusColor   lipgloss.TerminalColor // Purple - premium tier
	SonnetColor lipgloss.TerminalColor // Blue - mid tier
	HaikuColor  lipgloss.TerminalColor // Cyan/Teal - lightweight tier

	// Token type colors.
	OutputTokenColor     lipgloss.TerminalColor // Light blue
	CacheWriteTokenColor lipgloss.TerminalColor // Warm orange - cost investment
	CacheReadTokenColor  lipgloss.TerminalColor // Cyan - efficiency/savings

	// Context usage level colors (thresholds based on ~75-78% compaction trigger)
	ContextLowColor      lipgloss.TerminalColor // Green - 0-65%
	ContextHighColor     lipgloss.TerminalColor // Orange - 65-75% (approaching compaction)
	ContextCriticalColor lipgloss.TerminalColor // Red - 75%+ (compaction territory)
	ContextFreeColor     lipgloss.TerminalColor // free space in the context bar

	// AgentColors is the cycling palette that tells sub-agents apart:
	// pink, orange, yellow, blue, cyan.
	AgentColors []lipgloss.TerminalColor
)

// The styles built from the palette, rebuilt by SetDark.
var (
	// HeaderStyle uses bold white for clean, minimal section headers
	HeaderStyle lipgloss.Style
	// HeroCostStyle for the prominent centered total cost display
	HeroCostStyle lipgloss.Style
	// SectionHeaderStyle for bracketed section headers (IBM 3279 convention: cyan for static text)
	SectionHeaderStyle lipgloss.Style
	// PanelBorderStyle for header panel box-drawing characters
	PanelBorderStyle lipgloss.Style

	// BorderStyle for table borders. Width and padding come from
	// fmt.Sprintf, not from the style.
	BorderStyle lipgloss.Style
	// TotalValueStyle for the values on summary lines
	TotalValueStyle lipgloss.Style
	// SavingsLabelStyle and SavingsValueStyle for the cache savings display
	SavingsLabelStyle lipgloss.Style
	SavingsValueStyle lipgloss.Style
	// FooterStyle for status and footer lines
	FooterStyle lipgloss.Style
	// LiveIndicatorStyle uses green dot for active/healthy status (not red which implies error)
	LiveIndicatorStyle lipgloss.Style
	// SpinnerStyle for the loading spinner
	SpinnerStyle lipgloss.Style
	// HighlightStyle for recently changed values
	HighlightStyle lipgloss.Style
	// DimStyle for extra decimal precision in costs
	DimStyle lipgloss.Style
)

var darkPalette bool

// SetDark selects the dark palette (true) or the light one (false) and
// rebuilds every color and style above from it. Like the glyph set, the
// choice is process-wide and made before anything renders, so nothing may
// keep a copy of a color or style from before the call.
func SetDark(dark bool) {
	darkPalette = dark

	PrimaryColor = palette.primary.on(dark)
	SecondaryColor = palette.secondary.on(dark)
	SuccessColor = palette.success.on(dark)
	InfoColor = palette.info.on(dark)
	WarningColor = palette.warning.on(dark)
	AccentColor = palette.accent.on(dark)
	ErrorColor = palette.err.on(dark)
	OrangeColor = palette.orange.on(dark)
	BlueColor = palette.blue.on(dark)
	NeutralColor = palette.neutral.on(dark)
	SoftTextColor = palette.softText.on(dark)
	NoteColor = palette.note.on(dark)
	SeparatorColor = palette.separator.on(dark)
	HighlightColor = palette.highlight.on(dark)

	FableColor = palette.fable.on(dark)
	OpusColor, SonnetColor, HaikuColor = PrimaryColor, BlueColor, InfoColor
	OutputTokenColor, CacheWriteTokenColor, CacheReadTokenColor = BlueColor, OrangeColor, InfoColor
	ContextLowColor, ContextHighColor, ContextCriticalColor = SuccessColor, OrangeColor, ErrorColor
	ContextFreeColor = palette.contextFree.on(dark)
	AgentColors = []lipgloss.TerminalColor{AccentColor, OrangeColor, palette.agentOlive.on(dark), BlueColor, InfoColor}

	HeaderStyle = lipgloss.NewStyle().Bold(true)
	HeroCostStyle = lipgloss.NewStyle().Bold(true).Foreground(SuccessColor)
	SectionHeaderStyle = lipgloss.NewStyle().Foreground(InfoColor)
	PanelBorderStyle = lipgloss.NewStyle().Foreground(SecondaryColor)
	BorderStyle = lipgloss.NewStyle().Foreground(SecondaryColor)
	TotalValueStyle = lipgloss.NewStyle().Bold(true).Foreground(SuccessColor)
	SavingsLabelStyle = lipgloss.NewStyle().Foreground(InfoColor)
	SavingsValueStyle = lipgloss.NewStyle().Foreground(InfoColor)
	FooterStyle = lipgloss.NewStyle().Foreground(SecondaryColor)
	LiveIndicatorStyle = lipgloss.NewStyle().Bold(true).Foreground(SuccessColor)
	SpinnerStyle = lipgloss.NewStyle().Foreground(PrimaryColor)
	HighlightStyle = lipgloss.NewStyle().Bold(true).Foreground(HighlightColor)
	DimStyle = lipgloss.NewStyle().Foreground(SecondaryColor)
}

// Dark reports whether the dark palette is active.
func Dark() bool { return darkPalette }

// GetContextUsageColor returns the appropriate color based on context usage percentage.
// Thresholds aligned with Claude Code's ~75-78% auto-compaction trigger.
func GetContextUsageColor(usagePct float64) lipgloss.TerminalColor {
	switch {
	case usagePct >= 75:
		return ContextCriticalColor // Red - compaction territory
	case usagePct >= 65:
		return ContextHighColor // Orange - approaching compaction
	default:
		return ContextLowColor // Green - comfortable
	}
}

// GetAgentColor returns a color for the given agent ID (cycles through palette).
// Ordinal IDs ("1", "2" — the [An] markers) walk the palette in order; any
// other ID (real agent IDs like "a1b2c3d") hashes to a stable palette entry so
// distinct agents usually get distinct colors. The numeric path requires the
// WHOLE string to be a number — a digit-prefixed hash like "3f2a" must hash,
// not masquerade as ordinal 3.
func GetAgentColor(agentID string) lipgloss.TerminalColor {
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
func GetModelColor(modelName string) lipgloss.TerminalColor {
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
func GetCostGradientColor(cost, minCost, maxCost float64) lipgloss.TerminalColor {
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
