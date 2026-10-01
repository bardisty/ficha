package styles

import (
	"hash/fnv"
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/muesli/termenv"
)

// Glyphs. ficha draws with Unicode box-drawing characters and symbols by
// default; SetASCII swaps in plain-ASCII stand-ins for terminals without
// UTF-8 (--ascii). Glyphs are independent of color: --no-color keeps them.
//
// The choice is process-wide, like the palette's, and is made once at
// startup before anything renders. Every stand-in except Arrow, ScrollKeys
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
// Every run starts on the dark values. cmd switches to the light ones, with
// SetDark, when the terminal says its background is light, and to the 16
// basic colors, with SetBasic, on a terminal that has no more. It asks the
// terminal only when a report or live view is about to draw in color, so
// the dark values are also what anything drawn before that gets.
type pair struct{ dark, light uint8 }

func (p pair) resolve() color.Color {
	n := p.light
	if darkPalette {
		n = p.dark
	}
	if basicPalette {
		// Left to the writer that fits escapes to the terminal, the basic
		// color would come from a fixed table that sends the palette's
		// yellows to bright red. termenv picks the nearest of the 16,
		// which keeps a yellow yellow.
		nearest, _ := termenv.ANSI.Convert(termenv.ANSI256Color(n)).(termenv.ANSIColor)
		n = uint8(nearest) //nolint:gosec // one of the 16 basic colors
	}
	return lipgloss.Color(strconv.Itoa(int(n)))
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

// The palette as SetDark and SetBasic last resolved it.
var (
	PrimaryColor   color.Color // Purple
	SecondaryColor color.Color // Gray (dimmed but readable)
	SuccessColor   color.Color // Green
	InfoColor      color.Color // Cyan
	WarningColor   color.Color // Yellow
	AccentColor    color.Color // Pink
	ErrorColor     color.Color // Red
	OrangeColor    color.Color // Orange
	BlueColor      color.Color // Blue

	// NeutralColor sits near the default foreground, for values that
	// shouldn't draw the eye.
	NeutralColor color.Color
	// SoftTextColor is for text a step behind the figures it labels, such
	// as breakdown's stats line.
	SoftTextColor color.Color
	// NoteColor is for explanatory notes under a value: less prominent than
	// NeutralColor, more than SecondaryColor.
	NoteColor color.Color
	// SeparatorColor is for inline separators that should recede behind
	// the text around them.
	SeparatorColor color.Color

	// HighlightColor is for recently changed values.
	HighlightColor color.Color

	// Model colors, by tier.
	FableColor  color.Color // Pink/Magenta - flagship tier
	OpusColor   color.Color // Purple - premium tier
	SonnetColor color.Color // Blue - mid tier
	HaikuColor  color.Color // Cyan/Teal - lightweight tier

	// Token type colors.
	OutputTokenColor     color.Color // Light blue
	CacheWriteTokenColor color.Color // Warm orange - cost investment
	CacheReadTokenColor  color.Color // Cyan - efficiency/savings

	// Context usage level colors (thresholds based on ~75-78% compaction trigger)
	ContextLowColor      color.Color // Green - 0-65%
	ContextHighColor     color.Color // Orange - 65-75% (approaching compaction)
	ContextCriticalColor color.Color // Red - 75%+ (compaction territory)
	ContextFreeColor     color.Color // free space in the context bar

	// AgentColors is the cycling palette that tells sub-agents apart:
	// pink, orange, yellow, blue, cyan.
	AgentColors []color.Color
)

// The styles built from the palette, rebuilt with it.
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

var darkPalette, basicPalette bool

// SetDark selects the dark palette (true) or the light one (false) and
// rebuilds every color and style above from it. Like the glyph set, the
// choice is process-wide and made before anything renders, so nothing may
// keep a copy of a color or style from before the call.
func SetDark(dark bool) {
	darkPalette = dark
	rebuild()
}

// Dark reports whether the dark palette is active.
func Dark() bool { return darkPalette }

// SetBasic draws the palette in the 16 basic colors (true), for a terminal
// that has no more, or in its xterm-256 indexes (false). It rebuilds what
// SetDark does.
func SetBasic(basic bool) {
	basicPalette = basic
	rebuild()
}

func rebuild() {
	PrimaryColor = palette.primary.resolve()
	SecondaryColor = palette.secondary.resolve()
	SuccessColor = palette.success.resolve()
	InfoColor = palette.info.resolve()
	WarningColor = palette.warning.resolve()
	AccentColor = palette.accent.resolve()
	ErrorColor = palette.err.resolve()
	OrangeColor = palette.orange.resolve()
	BlueColor = palette.blue.resolve()
	NeutralColor = palette.neutral.resolve()
	SoftTextColor = palette.softText.resolve()
	NoteColor = palette.note.resolve()
	SeparatorColor = palette.separator.resolve()
	HighlightColor = palette.highlight.resolve()

	FableColor = palette.fable.resolve()
	OpusColor, SonnetColor, HaikuColor = PrimaryColor, BlueColor, InfoColor
	OutputTokenColor, CacheWriteTokenColor, CacheReadTokenColor = BlueColor, OrangeColor, InfoColor
	ContextLowColor, ContextHighColor, ContextCriticalColor = SuccessColor, OrangeColor, ErrorColor
	ContextFreeColor = palette.contextFree.resolve()
	AgentColors = []color.Color{AccentColor, OrangeColor, palette.agentOlive.resolve(), BlueColor, InfoColor}

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

// GetContextUsageColor returns the appropriate color based on context usage percentage.
// Thresholds aligned with Claude Code's ~75-78% auto-compaction trigger.
func GetContextUsageColor(usagePct float64) color.Color {
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
func GetAgentColor(agentID string) color.Color {
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
func GetModelColor(modelName string) color.Color {
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
func GetCostGradientColor(cost, minCost, maxCost float64) color.Color {
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
