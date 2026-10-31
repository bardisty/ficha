package render

import (
	"fmt"
	"image/color"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/x/ansi"
)

func TestCost(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0.0, "$0.0000"},
		{0.0000001, "$0.0000"},
		{0.0001, "$0.0001"},
		{0.0512, "$0.0512"},
		{-0.5, "$-0.5000"},
		{0.123456, "$0.1235"},
		// Four decimals would round these to 1.0000, so they switch to two
		{0.99994, "$0.9999"},
		{0.99995, "$1.00"},
		{0.99999, "$1.00"},
		{1.0, "$1.00"},
		{1.5, "$1.50"},
		{10.5, "$10.50"},
		{123.456789, "$123.46"},
		{999.99, "$999.99"},
		// Rounding comes first, so the comma goes where the rounded figure
		// needs it.
		{999.996, "$1,000.00"},
		{1000, "$1,000.00"},
		{6224.395577, "$6,224.40"},
		{54522.39, "$54,522.39"},
		{99999.99, "$99,999.99"},
		{1234567.891, "$1,234,567.89"},
		{-1234.5, "$-1,234.50"},
	}
	if got := Cost(math.NaN()); got != "$NaN" {
		t.Errorf("Cost(NaN) = %q, want %q", got, "$NaN")
	}
	for _, tc := range tests {
		if got := Cost(tc.input); got != tc.want {
			t.Errorf("Cost(%f) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestCostCellAlignsDecimals(t *testing.T) {
	tests := []struct {
		cost  float64
		width int
		want  string
	}{
		{0.2628, 11, "    $0.2628"},
		{13.65, 11, "   $13.65  "},
		{1234.56, 11, "$1,234.56  "},
		{999.99, 11, "  $999.99  "},
		{0, 8, " $0.0000"},
		// Too wide for the cell: returned whole, never cut
		{123456.78, 8, "$123,456.78  "},
	}
	for _, tc := range tests {
		got := CostCell(tc.cost, tc.width)
		if got != tc.want {
			t.Errorf("CostCell(%v, %d) = %q, want %q", tc.cost, tc.width, got, tc.want)
		}
	}

	// Every cell in a column puts its decimal point at the same offset
	width := CostCellWidth(0, 0.2628, 13.65, 1234.56, 0.001)
	dot := -1
	for _, c := range []float64{0.2628, 13.65, 1234.56, 0.001} {
		cell := CostCell(c, width)
		if len(cell) != width {
			t.Errorf("CostCell(%v, %d) has width %d", c, width, len(cell))
		}
		d := strings.Index(cell, ".")
		if dot == -1 {
			dot = d
		} else if d != dot {
			t.Errorf("CostCell(%v) decimal at %d, want %d", c, d, dot)
		}
	}
}

func TestCostCellWidth(t *testing.T) {
	if got := CostCellWidth(10); got != 10 {
		t.Errorf("no costs: got %d, want the minimum 10", got)
	}
	if got := CostCellWidth(10, 0.5, 12.34); got != 10 {
		t.Errorf("narrow costs: got %d, want 10", got)
	}
	// "$12,345.67" plus the two-space decimal pad
	if got := CostCellWidth(10, 0.5, 12345.67); got != 12 {
		t.Errorf("wide cost: got %d, want 12", got)
	}
}

func TestNumber(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{999949, "999.9K"},
		// Rounding would print 1000.0K, so the next unit takes over
		{999950, "1.00M"},
		{999999, "1.00M"},
		{1000000, "1.00M"},
		{2500000, "2.50M"},
		{999994999, "999.99M"},
		{999995000, "1.00B"},
		{1638000000, "1.64B"},
	}
	for _, tc := range tests {
		if got := Number(tc.input); got != tc.want {
			t.Errorf("Number(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDuration(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  string
	}{
		{0, "0s"},
		{30 * time.Second, "30s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m 0s"},
		{90 * time.Second, "1m 30s"},
		{59*time.Minute + 59*time.Second, "59m 59s"},
		{time.Hour, "1h 0m"},
		{65 * time.Minute, "1h 5m"},
		{2*time.Hour + 30*time.Minute, "2h 30m"},
		// Duration never rolls up to days: a multi-day span reads in hours.
		{25 * time.Hour, "25h 0m"},
	}
	for _, tc := range tests {
		if got := Duration(tc.input); got != tc.want {
			t.Errorf("Duration(%v) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// The last three cases mirror the exact spans in the summary, summary-details,
// and global golden fixtures, so they pin how each header rolls up.
func TestDurationLong(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"}, // minutes drop the seconds, unlike Duration
		{time.Hour, "1h 0m"},
		{2*time.Hour + 30*time.Minute, "2h 30m"},
		{23*time.Hour + 59*time.Minute, "23h 59m"},
		{24 * time.Hour, "1d"},
		{57*time.Hour + 20*time.Minute, "2d"},  // summary_details golden
		{104*time.Hour + 45*time.Minute, "4d"}, // summary golden
		{45 * 24 * time.Hour, "45d"},           // global golden
	}
	for _, tc := range tests {
		if got := DurationLong(tc.input); got != tc.want {
			t.Errorf("DurationLong(%v) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// Covers both budgets the callers pass: 8 for the compact live header, 40 for
// the static session header. Truncation is a plain prefix, no ellipsis.
func TestTruncateID(t *testing.T) {
	tests := []struct {
		id     string
		maxLen int
		want   string
	}{
		// maxLen 8 (TUI live header)
		{"abc", 8, "abc"},
		{"12345678", 8, "12345678"},
		{"123456789", 8, "12345678"},
		{"550e8400-e29b-41d4-a716-446655440000", 8, "550e8400"},
		{"", 8, ""},
		// maxLen 40 (static show/summary header): real 36-char UUIDs pass through
		{"0a1b2c3d-4e5f-6789-abcd-ef0123456789", 40, "0a1b2c3d-4e5f-6789-abcd-ef0123456789"},
		{"this-is-a-very-long-session-id-that-exceeds-40-chars", 40, "this-is-a-very-long-session-id-that-exce"},
		// defensive: negative budget yields empty, not a panic
		{"anything", -1, ""},
	}
	for _, tc := range tests {
		got := TruncateID(tc.id, tc.maxLen)
		if got != tc.want {
			t.Errorf("TruncateID(%q, %d) = %q, want %q", tc.id, tc.maxLen, got, tc.want)
		}
		if strings.Contains(got, "...") {
			t.Errorf("TruncateID must not add an ellipsis, got %q", got)
		}
	}
}

func TestShortAgentID(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"", ""},
		{"w1", "w1"},
		{"exact77", "exact77"},                 // exactly 7 stays whole
		{"g7h8i9j0k1l2", "g7h8i9j"},            // 12-char hash cut to 7
		{"550e8400-e29b-41d4-a716", "550e840"}, // UUID-shaped
		// Real IDs are "a" + 16 hex; the constant "a" is dropped first
		{"a641f79aa692e33b9", "641f79a"},
		{"a1234567", "1234567"},
		{"abcdefg", "abcdefg"}, // short IDs keep their leading "a"
	}
	for _, tt := range tests {
		if got := ShortAgentID(tt.id); got != tt.want {
			t.Errorf("ShortAgentID(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestClampModel(t *testing.T) {
	tests := []struct {
		label string
		width int
		want  string
	}{
		// Every catalog display name fits the narrowest MODEL column (10).
		{"Opus 4.8", 10, "Opus 4.8"},
		{"Sonnet 4.6", 10, "Sonnet 4.6"}, // exactly 10: the name that overflowed %-9s
		{"Haiku 4.5", 10, "Haiku 4.5"},
		// Unknown models fall back to their raw ID and must be cut to fit.
		// A raw ID that must be cut drops the shared "claude-" prefix first.
		{"claude-opus-4-9-20260101", 10, "opus-4-9-…"},
		{"claude-opus-4-9-20260101", 12, "opus-4-9-20…"},
		{"claude-nova-9", 9, "nova-9"},
		{"claude-nova-9", 13, "claude-nova-9"}, // fits whole: the full ID stays
		{"us.anthropic.claude-opus-4-9-v1:0", 11, "us.anthrop…"},
		// A narrowed column drops a display name's version whole rather
		// than cut inside it, and never leaves a space before the ellipsis.
		{"Fable 5.1", 8, "Fable…"},
		{"Fable 5.1", 7, "Fable…"},
		{"Fable 5.1", 6, "Fable…"},
		{"Fable 5.1", 5, "Fabl…"},
		{"Opus 5.5", 6, "Opus…"},
		{"Sonnet 4.5", 9, "Sonnet…"},
		{"claude-nova 9-1", 7, "nova…"},
		// Degenerate widths yield no panic.
		{"Opus 4.8", 1, "…"},
		{"Opus 4.8", 0, ""},
		{"Opus 4.8", -1, ""},
		{"", 10, ""},
	}
	for _, tc := range tests {
		got := ClampModel(tc.label, tc.width)
		if got != tc.want {
			t.Errorf("ClampModel(%q, %d) = %q, want %q", tc.label, tc.width, got, tc.want)
		}
		if w := utf8.RuneCountInString(got); tc.width > 0 && w > tc.width {
			t.Errorf("ClampModel(%q, %d) = %q: %d columns, exceeds width", tc.label, tc.width, got, w)
		}
	}
}

// A clamped label must still be paddable by a "%-Ns" verb: when the cut inserts
// a multi-byte ellipsis the byte length must reach width so fmt adds no padding,
// and when it doesn't cut the label must stay pure ASCII so fmt pads correctly.
func TestClampModelKeepsFixedWidthColumns(t *testing.T) {
	const width = 10
	for _, label := range []string{
		"Opus 4.8", "Sonnet 4.6", "Sonnet 3.5", "Fable 5", "-",
		"claude-opus-4-9-20260101", "<synthetic>",
		"arn:aws:bedrock:us-east-1:123:inference-profile/us.anthropic.claude-opus-4-9",
	} {
		col := fmt.Sprintf("%-*s", width, ClampModel(label, width))
		if got := lipgloss.Width(col); got != width {
			t.Errorf("%q rendered %d columns in a %d-wide field: %q", label, got, width, col)
		}
	}
}

// Cost descending, ties broken by ID ascending for stable output.
func TestOrderModelsByCost(t *testing.T) {
	costByModel := map[string]models.CostBreakdown{
		"claude-haiku-4-5": {TotalCost: 0.42},
		"claude-opus-4-8":  {TotalCost: 7.90},
		"claude-sonnet-5":  {TotalCost: 1.58},
	}
	got := OrderModelsByCost(costByModel)
	want := []string{"claude-opus-4-8", "claude-sonnet-5", "claude-haiku-4-5"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("OrderModelsByCost = %v, want %v", got, want)
	}

	// Equal costs break the tie by ID ascending (deterministic output).
	tie := map[string]models.CostBreakdown{
		"b-model": {TotalCost: 1.0},
		"a-model": {TotalCost: 1.0},
		"c-model": {TotalCost: 1.0},
	}
	got = OrderModelsByCost(tie)
	want = []string{"a-model", "b-model", "c-model"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("OrderModelsByCost tie = %v, want %v", got, want)
	}

	if got := OrderModelsByCost(map[string]models.CostBreakdown{}); len(got) != 0 {
		t.Errorf("OrderModelsByCost(empty) = %v, want empty", got)
	}
}

func TestPrimaryModel(t *testing.T) {
	tests := []struct {
		name        string
		costByModel map[string]models.CostBreakdown
		want        string
	}{
		{
			"highest cost wins",
			map[string]models.CostBreakdown{
				"claude-sonnet-4":  {TotalCost: 0.50},
				"claude-opus-4-5":  {TotalCost: 1.20},
				"claude-haiku-4-5": {TotalCost: 0.05},
			},
			"Opus 4.5",
		},
		{
			"single model",
			map[string]models.CostBreakdown{"claude-sonnet-4": {TotalCost: 0.50}},
			"Sonnet 4",
		},
		{
			"empty map returns dash",
			map[string]models.CostBreakdown{},
			"-",
		},
		{
			"single zero-cost model still reports the model",
			map[string]models.CostBreakdown{"claude-sonnet-4": {TotalCost: 0}},
			"Sonnet 4",
		},
		{
			"all-zero-cost tie breaks by ID ascending",
			map[string]models.CostBreakdown{
				"claude-sonnet-5": {TotalCost: 0},
				"claude-opus-4-8": {TotalCost: 0},
			},
			"Opus 4.8",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PrimaryModel(tt.costByModel); got != tt.want {
				t.Errorf("PrimaryModel() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Equal-cost models must resolve the same way every run — Go randomizes map
// iteration, so a naive ">" comparison would let the reported primary flip.
func TestPrimaryModelTieIsDeterministic(t *testing.T) {
	tie := map[string]models.CostBreakdown{
		"claude-opus-4-8":  {TotalCost: 1.0},
		"claude-haiku-4-5": {TotalCost: 1.0},
		"claude-sonnet-5":  {TotalCost: 1.0},
	}
	// Lowest ID ascending wins, matching OrderModelsByCost's tiebreak.
	const want = "Haiku 4.5"
	for range 50 {
		if got := PrimaryModel(tie); got != want {
			t.Fatalf("PrimaryModel() = %q, want %q (tie must break by ID ascending)", got, want)
		}
	}
}

func TestCacheTokensByTTL(t *testing.T) {
	// Detailed per-TTL breakdown present: return each split.
	detailed := models.TokenUsage{
		CacheCreationInputTokens: 300,
		CacheCreation:            &models.CacheCreation{Ephemeral5mInputTokens: 200, Ephemeral1hInputTokens: 100},
	}
	if got5m, got1h := CacheTokensByTTL(detailed); got5m != 200 || got1h != 100 {
		t.Errorf("CacheTokensByTTL(detailed) = (%d, %d), want (200, 100)", got5m, got1h)
	}

	// No breakdown: all cache-creation tokens attributed to 5m.
	fallback := models.TokenUsage{CacheCreationInputTokens: 250}
	if got5m, got1h := CacheTokensByTTL(fallback); got5m != 250 || got1h != 0 {
		t.Errorf("CacheTokensByTTL(fallback) = (%d, %d), want (250, 0)", got5m, got1h)
	}
}

func TestCostComponentLabel(t *testing.T) {
	tests := []struct {
		component string
		want      string
	}{
		// The names the cost tables give these rows
		{"input", "Input"},
		{"output", "Output"},
		{"cache_write_5m", "Cache write 5m"},
		{"cache_write_1h", "Cache write 1h"},
		{"cache_read", "Cache read"},
		{"unknown_component", "unknown_component"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := CostComponentLabel(tt.component); got != tt.want {
			t.Errorf("CostComponentLabel(%q) = %q, want %q", tt.component, got, tt.want)
		}
	}
}

func TestSectionHeader(t *testing.T) {
	// noColor output is exactly width columns wide and centers the name.
	got := SectionHeader("COST BY MODEL", 76, true)
	if lenRunes := len([]rune(got)); lenRunes != 76 {
		t.Errorf("SectionHeader width = %d runes, want 76", lenRunes)
	}
	if !strings.Contains(got, "[ COST BY MODEL ]") {
		t.Errorf("SectionHeader should contain the bracketed name: %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("noColor SectionHeader should not contain ANSI codes: %q", got)
	}
}

// SectionHeaderFit takes the longest title that leaves a rule cell on each
// side, measured in columns rather than bytes, or the last title when none
// fits.
func TestSectionHeaderFit(t *testing.T) {
	tests := []struct {
		width int
		want  string
	}{
		{40, "───────[ NAME · 3 of 12 running ]───────"},
		{28, "─[ NAME · 3 of 12 running ]─"},
		{27, "───[ NAME · 3 running ]────"},
		{20, "──────[ NAME ]──────"},
	}
	for _, tt := range tests {
		got := SectionHeaderFit(tt.width, true, "NAME · 3 of 12 running", "NAME · 3 running", "NAME")
		if got != tt.want {
			t.Errorf("width %d: got %q, want %q", tt.width, got, tt.want)
		}
		if w := lipgloss.Width(got); w != tt.width {
			t.Errorf("width %d: header is %d columns wide", tt.width, w)
		}
	}
	if got := SectionHeaderFit(20, true, "A TITLE FAR TOO LONG TO FIT"); got != "[ A TITLE FAR TOO LONG TO FIT ]" {
		t.Errorf("no title fits: got %q, want the last title unruled", got)
	}
}

func TestContextBar(t *testing.T) {
	tests := []struct {
		name      string
		size      int64
		max       int
		compactAt int
		width     int
		want      string
	}{
		{"half full: tick in the free part", 50, 100, 75, 20, "[██████████░░░░░│░░░░]"},
		{"past compaction: tick inside the used part", 90, 100, 75, 20, "[███████████████│██░░]"},
		{"exactly at the tick", 75, 100, 75, 20, "[███████████████│░░░░]"},
		{"full", 100, 100, 75, 8, "[██████│█]"},
		{"over full clamps", 150, 100, 75, 8, "[██████│█]"},
		// The tick takes the cell 967K falls in, not the nearest edge.
		{"1M model: mark in the second-last cell", 678_200, 1_000_000, 967_000, 44, "[█████████████████████████████░░░░░░░░░░░░░│░]"},
		{"1M model past its mark", 980_000, 1_000_000, 967_000, 20, "[███████████████████│]"},
		{"mark at the end of the window takes the last cell", 100_000, 200_000, 200_000, 20, "[██████████░░░░░░░░░│]"},
		{"full to a mark at the end", 200_000, 200_000, 200_000, 8, "[███████│]"},
		{"unknown window", 10, 0, 0, 8, "[░░░░░░░│]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContextBar(tt.size, tt.max, tt.compactAt, tt.width, true)
			if got != tt.want {
				t.Errorf("ContextBar(%d, %d, %d, %d) = %q, want %q", tt.size, tt.max, tt.compactAt, tt.width, got, tt.want)
			}
			if n := len([]rune(got)); n != tt.width+2 {
				t.Errorf("width = %d runes, want %d", n, tt.width+2)
			}
		})
	}

	// Color touches only the escapes, never the cells.
	colored := ContextBar(90, 100, 75, 20, false)
	if !strings.Contains(colored, "\x1b[") {
		t.Errorf("colored bar has no escapes: %q", colored)
	}

	// The ASCII glyph set: '#' used, '-' free, '|' tick.
	styles.SetASCII(true)
	t.Cleanup(func() { styles.SetASCII(false) })
	if got, want := ContextBar(50, 100, 75, 20, true), "[##########-----|----]"; got != want {
		t.Errorf("ASCII bar = %q, want %q", got, want)
	}
	if got, want := ContextBar(980_000, 1_000_000, 967_000, 20, true), "[###################|]"; got != want {
		t.Errorf("ASCII bar past a 1M mark = %q, want %q", got, want)
	}
}

// The bar and the percentage take their color from the reading's zone
// against its own mark, not from a fixed share of the window: 78% of a 1M
// window is well short of 967K, and 90% of a 200K one is in the band.
func TestContextGaugeColorFollowsTheMark(t *testing.T) {
	tests := []struct {
		name      string
		size      int64
		max       int
		compactAt int
		want      color.Color
	}{
		{"1M model at 78%", 780_000, 1_000_000, 967_000, styles.ContextLowColor},
		{"1M model in the band", 900_000, 1_000_000, 967_000, styles.ContextHighColor},
		{"1M model past its mark", 980_000, 1_000_000, 967_000, styles.ContextCriticalColor},
		{"200K model at 78%", 156_000, 200_000, 200_000, styles.ContextLowColor},
		{"200K model in the band", 180_000, 200_000, 200_000, styles.ContextHighColor},
		{"200K model at its mark", 200_000, 200_000, 200_000, styles.ContextCriticalColor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, _ := ContextGauge(reading(tt.size, tt.max, tt.compactAt), "Opus", nil, 60, false, false)
			pct := fmt.Sprintf("%3.0f%%", float64(tt.size)/float64(tt.max)*100)
			want := lipgloss.NewStyle().Foreground(tt.want).Render(pct)
			if !strings.HasPrefix(line, want) {
				t.Errorf("line = %q, want it to start with %q", line, want)
			}
		})
	}
}

// reading is a context reading of size tokens out of window, as
// analyzer.ContextUsage builds one.
func reading(size int64, window, compactAt int) models.ContextUsage {
	return models.ContextUsage{Tokens: size, Window: window, Percent: float64(size) / float64(window) * 100, CompactAt: compactAt}
}

// A reading with no window shows its tokens and says why, naming the model
// while there's room, and never a percentage, bar or compaction note.
func TestContextGaugeWindowUnknown(t *testing.T) {
	c := models.ContextUsage{Tokens: 680_400}
	tests := []struct {
		width    int
		wantNote string
	}{
		{60, "window unknown for claude-mystery-9"},
		{30, "window unknown"},
		{10, ""},
	}
	for _, tt := range tests {
		line, note := ContextGauge(c, "claude-mystery-9", nil, tt.width, true, false)
		if line != "680.4K tokens" {
			t.Errorf("width %d: line = %q, want %q", tt.width, line, "680.4K tokens")
		}
		if note != tt.wantNote {
			t.Errorf("width %d: note = %q, want %q", tt.width, note, tt.wantNote)
		}
	}
	if line, _ := ContextGauge(c, "claude-mystery-9", nil, 60, false, true); line != styles.HighlightStyle.Render("680.4K tokens") {
		t.Errorf("highlighted line = %q", line)
	}

	// A model ficha knows has a window; this reading outgrew every one it
	// knows, so the note says that rather than calling the model unknown.
	over := models.ContextUsage{Tokens: 1_050_000}
	for width, want := range map[int]string{60: "over the 1.00M window ficha knows for Opus 5.5", 30: "over the known window", 10: ""} {
		if _, note := ContextGauge(over, "claude-opus-5-5", nil, width, true, false); note != want {
			t.Errorf("1.05M on Opus 5.5, width %d: note = %q, want %q", width, note, want)
		}
	}
}

func TestNumberShort(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{512, "512"},
		{1000, "1K"},
		{95_400, "95K"},
		{680_400, "680K"},
		{999_499, "999K"},
		{999_500, "1.0M"},
		{1_050_000, "1.1M"},
		{12_000_000, "12M"},
	}
	for _, tt := range tests {
		if got := NumberShort(tt.n); got != tt.want {
			t.Errorf("NumberShort(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

// The note keeps its reading whole at any width, shortening and then
// shedding the scope first.
// It counts tokens to or past the mark, so it reads true for any model.
func TestContextNote(t *testing.T) {
	tests := []struct {
		size      int64
		compactAt int
		width     int
		want      string
	}{
		{100_000, 150_000, 60, "50.0K to ~compaction (main conversation, last request)"},
		{100_000, 150_000, 45, "50.0K to ~compaction (main, last request)"},
		{100_000, 150_000, 40, "50.0K to ~compaction"},
		{100_000, 150_000, 15, ""},
		{100_000, 150_000, 5, ""},
		{678_200, 967_000, 60, "288.8K to ~compaction (main conversation, last request)"},
		{980_000, 967_000, 60, "13.0K past ~compaction (main conversation, last request)"},
		{980_000, 967_000, 50, "13.0K past ~compaction (main, last request)"},
		{980_000, 967_000, 30, "13.0K past ~compaction"},
		{980_000, 967_000, 20, "past ~compaction"},
		{200_000, 200_000, 60, "at ~compaction (main conversation, last request)"},
		{200_000, 200_000, 20, "at ~compaction"},
		{200_000, 200_000, 10, ""},
	}
	for _, tt := range tests {
		if got := firstFit(tt.width, contextNotes(tt.size, tt.compactAt)...); got != tt.want {
			t.Errorf("contextNotes(%d, %d), width %d: = %q, want %q", tt.size, tt.compactAt, tt.width, got, tt.want)
		}
	}
	if got := firstFit(80, contextNotes(10, 0)...); got != "" {
		t.Errorf("no mark: note = %q, want none", got)
	}
}

func TestContextGauge(t *testing.T) {
	tests := []struct {
		name     string
		size     int64
		width    int
		wantLine string
		wantNote string
	}{
		{"fits: bar takes the rest", 980_000, 50,
			" 98% [███████████████████████████│] 980.0K / 1.00M",
			"13.0K past ~compaction (main, last request)"},
		{"headroom before compaction", 500_000, 50,
			" 50% [██████████████░░░░░░░░░░░░░│] 500.0K / 1.00M",
			"467.0K to ~compaction (main, last request)"},
		// The bar outranks the counts: it carries the compaction tick.
		{"narrow drops the counts before clipping", 980_000, 25,
			" 98% [█████████████████│]", ""},
		{"narrower still keeps a floor-width bar", 980_000, 17, " 98% [█████████│]", ""},
		{"narrowest keeps the percentage", 980_000, 8, " 98%", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, note := ContextGauge(reading(tt.size, 1_000_000, 967_000), "Opus 5.5", nil, tt.width, true, false)
			if line != tt.wantLine {
				t.Errorf("line = %q, want %q", line, tt.wantLine)
			}
			if n := len([]rune(line)); n > tt.width {
				t.Errorf("line is %d wide, over %d", n, tt.width)
			}
			if tt.wantNote != "" && note != tt.wantNote {
				t.Errorf("note = %q, want %q", note, tt.wantNote)
			}
		})
	}
}

// An agent's cell is dim until its reading enters its own model's warning
// band, then takes the zone's color.
func TestAgentContextCell(t *testing.T) {
	dim := styles.DimStyle.Render(" 78% ctx")
	tests := []struct {
		name string
		c    models.ContextUsage
		want string
	}{
		{"1M model at 78% stays dim", models.ContextUsage{Tokens: 780_000, Window: 1_000_000, Percent: 78, CompactAt: 967_000}, dim},
		{"200K model at 78% stays dim", models.ContextUsage{Tokens: 156_000, Window: 200_000, Percent: 78, CompactAt: 200_000}, dim},
		{"1M model in the band", models.ContextUsage{Tokens: 900_000, Window: 1_000_000, Percent: 90, CompactAt: 967_000},
			lipgloss.NewStyle().Foreground(styles.ContextHighColor).Render(" 90% ctx")},
		{"1M model past its mark", models.ContextUsage{Tokens: 980_000, Window: 1_000_000, Percent: 98, CompactAt: 967_000},
			lipgloss.NewStyle().Foreground(styles.ContextCriticalColor).Render(" 98% ctx")},
		{"200K model at its mark", models.ContextUsage{Tokens: 200_000, Window: 200_000, Percent: 100, CompactAt: 200_000},
			lipgloss.NewStyle().Foreground(styles.ContextCriticalColor).Render("100% ctx")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AgentContextCell(&tt.c, false); got != tt.want {
				t.Errorf("AgentContextCell = %q, want %q", got, tt.want)
			}
		})
	}
	// No window: the tokens, in the same 8 columns, and dim.
	unknown := &models.ContextUsage{Tokens: 680_400}
	if got, want := AgentContextCell(unknown, false), styles.DimStyle.Render("680K ctx"); got != want {
		t.Errorf("window unknown = %q, want %q", got, want)
	}
	if got := AgentContextCell(&models.ContextUsage{Tokens: 1_050_000}, true); got != "1.1M ctx" {
		t.Errorf("window unknown, no color = %q, want %q", got, "1.1M ctx")
	}
	if got := AgentContextCell(nil, false); got != "        " {
		t.Errorf("nil reading = %q, want blanks", got)
	}
}

func TestCostStyledNoColor(t *testing.T) {
	tests := []struct {
		name  string
		cost  float64
		width int
		want  string
	}{
		{"zero cost width 11", 0.0, 11, "    $0.0000"},
		{"small cost width 11", 0.05, 11, "    $0.0500"},
		{"dollar cost no pad", 1.234567, 0, "$1.23  "},
		{"exact width", 0.123456, 7, "$0.1235"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// noColor path is deterministic regardless of highlight flag.
			got := CostStyled(tt.cost, tt.width, false, true)
			if got != tt.want {
				t.Errorf("CostStyled(%f, %d, false, true) = %q, want %q", tt.cost, tt.width, got, tt.want)
			}
			if hi := CostStyled(tt.cost, tt.width, true, true); hi != tt.want {
				t.Errorf("CostStyled noColor should ignore highlight: %q != %q", hi, tt.want)
			}
			if strings.Contains(got, "\x1b[") {
				t.Errorf("noColor output should not contain ANSI codes: %q", got)
			}
		})
	}
}

func TestCostStyledGreenBoldGreenNoColor(t *testing.T) {
	if got, want := CostStyledGreen(1.5, 11, false, true), "    $1.50  "; got != want {
		t.Errorf("CostStyledGreen = %q, want %q", got, want)
	}
	if got, want := CostStyledBoldGreen(2.0, 11, false, true), "    $2.00  "; got != want {
		t.Errorf("CostStyledBoldGreen = %q, want %q", got, want)
	}
}

// TestCostStyledColorPathsContainValue exercises the colored branches (which
// may or may not emit ANSI depending on the ambient terminal) and asserts the
// whole value survives in one piece: no digits split off into a dimmed run.
func TestCostStyledColorPathsContainValue(t *testing.T) {
	for _, s := range []string{
		CostStyled(1.234567, 11, false, false),
		CostStyled(1.234567, 11, true, false),
		CostStyledGreen(1.234567, 11, false, false),
		CostStyledBoldGreen(1.234567, 11, false, false),
		CostColored(1.234567, styles.SuccessColor, 10),
	} {
		if !strings.Contains(s, "$1.23") {
			t.Errorf("styled cost should contain the whole value: %q", s)
		}
	}
}

func TestClockConvertsToLocal(t *testing.T) {
	orig := time.Local
	time.Local = time.FixedZone("UTC-7", -7*3600)
	t.Cleanup(func() { time.Local = orig })

	ts := time.Date(2026, 9, 29, 1, 53, 3, 0, time.UTC)
	if got := Clock(ts); got != "18:53:03" {
		t.Errorf("Clock = %q, want 18:53:03", got)
	}
	if got := ClockShort(ts); got != "18:53" {
		t.Errorf("ClockShort = %q, want 18:53", got)
	}
	// A missing timestamp must not turn into a plausible local time
	if got := Clock(time.Time{}); got != "--:--:--" {
		t.Errorf("Clock(zero) = %q, want --:--:--", got)
	}
	if got := ClockShort(time.Time{}); got != "--:--" {
		t.Errorf("ClockShort(zero) = %q, want --:--", got)
	}
	// 01:53 UTC on the 29th is still the 28th in UTC-7
	if got := DayMarker(ts); got != "Mon 28 Sep" {
		t.Errorf("DayMarker = %q, want Mon 28 Sep", got)
	}
}

func TestSameLocalDay(t *testing.T) {
	orig := time.Local
	time.Local = time.FixedZone("UTC+9", 9*3600)
	t.Cleanup(func() { time.Local = orig })

	// Same UTC day, but 15:00 UTC is midnight in UTC+9
	a := time.Date(2026, 9, 28, 14, 59, 0, 0, time.UTC)
	b := time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	if SameLocalDay(a, b) {
		t.Error("14:59 and 15:00 UTC straddle local midnight in UTC+9")
	}
	// Different UTC days, same local day
	c := time.Date(2026, 9, 28, 23, 0, 0, 0, time.UTC)
	d := time.Date(2026, 9, 29, 1, 0, 0, 0, time.UTC)
	if !SameLocalDay(c, d) {
		t.Error("23:00 and 01:00 UTC are the same day in UTC+9")
	}
}

func TestTrendSymbol(t *testing.T) {
	for _, tc := range []struct {
		ascii bool
		trend models.TrendDirection
		want  string
	}{
		{false, models.TrendStable, "═"},
		{false, models.TrendIncreasing, "▲"},
		{false, models.TrendDecreasing, "▼"},
		{true, models.TrendStable, "="},
		{true, models.TrendIncreasing, "^"},
		{true, models.TrendDecreasing, "v"},
	} {
		styles.SetASCII(tc.ascii)
		if got := TrendSymbol(tc.trend); got != tc.want {
			t.Errorf("TrendSymbol(%v) ascii=%v = %q, want %q", tc.trend, tc.ascii, got, tc.want)
		}
	}
	styles.SetASCII(false)
}

// A cut after a space drops the space, so the ellipsis sits against the word.
func TestTruncateRunesTrimsSpaceBeforeEllipsis(t *testing.T) {
	if got := truncateRunes("workflow: review changes", 11); got != "workflow:…" {
		t.Errorf("truncateRunes = %q, want %q", got, "workflow:…")
	}
	if got := truncateRunes("ab cd", 4); got != "ab…" {
		t.Errorf("truncateRunes = %q, want %q", got, "ab…")
	}
}

func TestClampModelASCIIEllipsis(t *testing.T) {
	styles.SetASCII(true)
	t.Cleanup(func() { styles.SetASCII(false) })
	if got := ClampModel("claude-opus-4-9-20260101", 10); got != "opus-4-..." {
		t.Errorf("ClampModel ASCII = %q, want opus-4-...", got)
	}
	if got := ClampModel("claude-opus-4-9", 2); got != ".." {
		t.Errorf("ClampModel ASCII width 2 = %q, want ..", got)
	}
	if got := ClampModel("Fable 5.1", 8); got != "Fable..." {
		t.Errorf("ClampModel ASCII = %q, want Fable...", got)
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		ago  time.Duration
		want string
	}{
		{-time.Hour, "just now"}, // clock skew
		{30 * time.Second, "just now"},
		{time.Minute, "1m ago"},
		{59 * time.Minute, "59m ago"},
		{time.Hour, "1h ago"},
		{23*time.Hour + 59*time.Minute, "23h ago"},
		{24 * time.Hour, "1d ago"},
		{59 * 24 * time.Hour, "59d ago"},
		{60 * 24 * time.Hour, "2mo ago"},
		{364 * 24 * time.Hour, "12mo ago"},
		{365 * 24 * time.Hour, "1y ago"},
		{800 * 24 * time.Hour, "2y ago"},
	}
	for _, tt := range tests {
		if got := Ago(now.Add(-tt.ago), now); got != tt.want {
			t.Errorf("Ago(now-%v) = %q, want %q", tt.ago, got, tt.want)
		}
	}
	if got := Ago(time.Time{}, now); got != "-" {
		t.Errorf("Ago(zero) = %q, want %q", got, "-")
	}
}

func TestDateTimeAddsYearOnlyWhenNeeded(t *testing.T) {
	orig := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = orig })

	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	ts := time.Date(2026, 9, 8, 18, 38, 0, 0, time.UTC)
	if got := DateTime(ts, now); got != "Sep 08 18:38" {
		t.Errorf("DateTime = %q", got)
	}
	if got := Date(ts, now); got != "Sep 08" {
		t.Errorf("Date = %q", got)
	}
	last := ts.AddDate(-1, 0, 0)
	if got := DateTime(last, now); got != "Sep 08 2025 18:38" {
		t.Errorf("DateTime last year = %q", got)
	}
	if got := Date(time.Time{}, now); got != "-" {
		t.Errorf("Date(zero) = %q", got)
	}
}

// Warnings wrap between words, each continuation under the text of its own
// line, and a word too long for the room keeps a line to itself.
func TestWrapHanging(t *testing.T) {
	in := "Warning: unknown model \"claude-nova-9\" priced at fallback $5/$25 per MTok\n" +
		"  ficha 0.55.0 has no price for it. A newer release may know it: https://github.com/bardisty/ficha/releases\n"
	want := "Warning: unknown model \"claude-nova-9\"\n" +
		"         priced at fallback $5/$25 per\n" +
		"         MTok\n" +
		"  ficha 0.55.0 has no price for it. A\n" +
		"  newer release may know it:\n" +
		"  https://github.com/bardisty/ficha/releases\n"
	if got := WrapHanging(in, 40); got != want {
		t.Errorf("WrapHanging at 40:\ngot:\n%s\nwant:\n%s", got, want)
	}
	for _, line := range strings.Split(strings.TrimSuffix(WrapHanging(in, 40), "\n"), "\n") {
		if w := lipgloss.Width(line); w > 40 && !strings.Contains(line, "https://") {
			t.Errorf("line is %d columns wide: %q", w, line)
		}
	}
	if got := WrapHanging(in, 200); got != in {
		t.Errorf("lines that fit changed:\n%s", got)
	}
	if got := WrapHanging("", 40); got != "" {
		t.Errorf("empty input = %q", got)
	}
}

// A note hangs past its label the way a warning does, and a path too long
// for the room after the label keeps a line of its own, uncut.
func TestWrapHangingNote(t *testing.T) {
	in := "Note: session cccccccc is in ~/work/webapp.\n"
	want := "Note: session cccccccc is in\n" +
		"      ~/work/webapp.\n"
	if got := WrapHanging(in, 30); got != want {
		t.Errorf("WrapHanging at 30:\ngot:\n%s\nwant:\n%s", got, want)
	}

	path := "/very/long/path/to/a/copied/transcript/folder/"
	in = "Note: " + path + " isn't there.\n"
	want = "Note: " + path + "\n" +
		"      isn't there.\n"
	if got := WrapHanging(in, 30); got != want {
		t.Errorf("overlong first word at 30:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// A path the caller marks stays on one line with its spaces, so it can be
// copied out of the terminal whole.
func TestWrapHangingKeepsMarkedPathWhole(t *testing.T) {
	path := "~/My Projects/webapp"
	in := "Note: session cccccccc is in " + NoBreak(path) + ".\n"
	// 32 columns end the first line inside the path, 44 right after "~/My"
	// and its space, 49 one short of the whole note
	for _, width := range []int{32, 44, 49} {
		want := "Note: session cccccccc is in\n" +
			"      ~/My Projects/webapp.\n"
		if got := WrapHanging(in, width); got != want {
			t.Errorf("at %d:\ngot:\n%s\nwant:\n%s", width, got, want)
		}
	}
	plain := "Note: session cccccccc is in " + path + ".\n"
	for _, width := range []int{50, 0, -1} {
		if got := WrapHanging(in, width); got != plain {
			t.Errorf("at %d the note fits or isn't wrapped, got %q", width, got)
		}
	}

	// Unmarked, the same path is words like any others
	if got := WrapHanging(plain, 44); !strings.Contains(got, "~/My\n") {
		t.Errorf("an unmarked path didn't break at its space at 44:\n%s", got)
	}
}

func TestWrapHangingKeepsRunsOfSpaces(t *testing.T) {
	path := "/srv/two  spaces/and   three"
	in := "Warning: ignoring " + NoBreak(path) + ": permission denied\n"
	want := "Warning: ignoring\n" +
		"         " + path + ":\n" +
		"         permission denied\n"
	// The path and its colon are 29 columns, which with the 9 of indent is
	// all of 38. Unmarked, "/srv/two  spaces/and" would end the first line
	if got := WrapHanging(in, 38); got != want {
		t.Errorf("marked path with runs of spaces at 38:\ngot:\n%s\nwant:\n%s", got, want)
	}

	// Outside a path, a run between two words that share a line survives
	in = "  col one    col two    col three and then enough words to wrap\n"
	want = "  col one    col two    col three\n" +
		"  and then enough words to wrap\n"
	if got := WrapHanging(in, 34); got != want {
		t.Errorf("runs of spaces at 34:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// A marked path too long for the room under the indent starts at the left
// edge. There it may fit after all. If it still doesn't, it runs off the edge
// on a line of its own: a path the terminal wraps can still be copied, and
// one broken in two with an indent in between can't.
func TestWrapHangingMarkedPathLongerThanWidth(t *testing.T) {
	short := "/srv/My Config/projects/-srv-webapp/sessions-index.json"
	in := "Warning: ignoring " + NoBreak(short) + ": permission denied\n"
	want := "Warning: ignoring\n" +
		short + ":\n" +
		"         permission denied\n"
	// The path and its colon are 56 columns: they fit in 64, but after the 9
	// columns of indent they'd need 65
	if got := WrapHanging(in, 64); got != want {
		t.Errorf("at 64:\ngot:\n%s\nwant:\n%s", got, want)
	}
	// With room for both, the path hangs like any other word. It still can't
	// follow the label, where it would end at column 74
	want = "Warning: ignoring\n" +
		"         " + short + ":\n" +
		"         permission denied\n"
	if got := WrapHanging(in, 65); got != want {
		t.Errorf("at 65:\ngot:\n%s\nwant:\n%s", got, want)
	}

	path := "/Users/dev/Library/Application Support/Claude Config/projects/-Users-dev-My-Projects-webapp/"
	in = "Note: any agents this session ran aren't counted. ficha looks for them in " + NoBreak(path) + ", which isn't there.\n"
	want = "Note: any agents this session ran aren't\n" +
		"      counted. ficha looks for them in\n" +
		path + ",\n" +
		"      which isn't there.\n"
	if got := WrapHanging(in, 40); got != want {
		t.Errorf("at 40:\ngot:\n%s\nwant:\n%s", got, want)
	}

	// A line that is only an indented path, as -v lists skipped files, keeps
	// its place under the line above it
	in = "    " + NoBreak(path) + "\n"
	if got, want := WrapHanging(in, 40), "    "+path+"\n"; got != want {
		t.Errorf("a path on its own line at 40:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// A task fits by display columns, so a wide character counts twice, a cut
// never splits an emoji, and it never leaves a space before its ellipsis.
func TestFitTask(t *testing.T) {
	tests := []struct {
		task  string
		width int
		want  string
	}{
		{"Find the auth middleware", 30, "Find the auth middleware      "},
		{"Find the auth middleware", 14, "Find the auth…"},
		{"Find the auth middleware", 15, "Find the auth… "},
		{"認証ミドルウェアを探す", 12, "認証ミドル… "},
		{"Check \u2600\ufe0f weather", 8, "Check…  "},
		{"Pair \U0001F469\u200d\U0001F4BB review", 8, "Pair \U0001F469\u200d\U0001F4BB…"},
		{"", 4, "    "},
	}
	for _, tt := range tests {
		got := FitTask(tt.task, tt.width)
		if got != tt.want {
			t.Errorf("FitTask(%q, %d) = %q, want %q", tt.task, tt.width, got, tt.want)
		}
		if w := lipgloss.Width(got); w != tt.width {
			t.Errorf("FitTask(%q, %d) is %d wide", tt.task, tt.width, w)
		}
	}
}

// CostComponents ranks a cost's parts largest first, leaves out the free
// ones, and keeps the cache-write TTLs apart.
func TestCostComponents(t *testing.T) {
	got := CostComponents(models.CostBreakdown{
		InputCost: 0.001, OutputCost: 0.05, CacheWrite5mCost: 0.05, CacheWrite1hCost: 0, CacheReadCost: 0.19, TotalCost: 0.291,
	})
	want := []CostComponent{{"cache_read", 0.19}, {"output", 0.05}, {"cache_write_5m", 0.05}, {"input", 0.001}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CostComponents = %v, want %v", got, want)
	}
	if got := CostComponents(models.CostBreakdown{}); len(got) != 0 {
		t.Errorf("a free message has no components, got %v", got)
	}
}

// WeekdayClock, DateClockShort and their other forms put the local day before
// the time, and keep Clock's placeholders for a zero time.
func TestWeekdayClock(t *testing.T) {
	orig := time.Local
	time.Local = time.FixedZone("UTC+1", 3600)
	t.Cleanup(func() { time.Local = orig })
	ts := time.Date(2026, 10, 13, 23, 30, 5, 0, time.UTC)
	if got := WeekdayClock(ts); got != "Wed 00:30:05" {
		t.Errorf("WeekdayClock = %q", got)
	}
	if got := WeekdayClockShort(ts); got != "Wed 00:30" {
		t.Errorf("WeekdayClockShort = %q", got)
	}
	if got := DateClockShort(ts); got != "14 Oct 00:30" {
		t.Errorf("DateClockShort = %q", got)
	}
	if got := WeekdayDateClock(ts); got != "Wed 14 Oct 00:30:05" {
		t.Errorf("WeekdayDateClock = %q", got)
	}
	for _, f := range []func(time.Time) string{WeekdayClock, WeekdayClockShort, DateClockShort, WeekdayDateClock} {
		if f(time.Time{}) != Clock(time.Time{}) {
			t.Error("a zero time should keep Clock's placeholder")
		}
	}
}

// TruncateRight and Tail measure as lipgloss.Width does and cut whole
// graphemes: an emoji with a variation selector is two columns, a ZWJ
// sequence is never split, and a wide character the cut falls inside goes.
func TestTruncateRightAndTail(t *testing.T) {
	sun := "☀\ufe0f"
	coder := "\U0001F469\u200d\U0001F4BB"
	for _, tt := range []struct {
		s           string
		width       int
		right, tail string
	}{
		{"short", 10, "short", "short"},
		{"weather report", 9, "weather…", "er report"},
		{"a  b", 3, "a…", "  b"},
		{"ab" + sun + "cd", 4, "ab…", sun + "cd"},
		{"ab" + sun + "cd", 3, "ab…", "cd"},
		{"x" + coder + "y", 3, "x…", coder + "y"},
		{"日本語", 3, "日…", "語"},
		{"日本語", 0, "", ""},
		{"ab1\ufe0f\u20e3cd", 4, "ab…", "1\ufe0f\u20e3cd"},
		{"/" + strings.Repeat("1\ufe0f\u20e3", 5) + "/p", 6, "/1\ufe0f\u20e31\ufe0f\u20e3…", "1\ufe0f\u20e31\ufe0f\u20e3/p"},
	} {
		if got := TruncateRight(tt.s, tt.width); got != tt.right {
			t.Errorf("TruncateRight(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.right)
		}
		if got := Tail(tt.s, tt.width); got != tt.tail {
			t.Errorf("Tail(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.tail)
		}
		for _, got := range []string{TruncateRight(tt.s, tt.width), Tail(tt.s, tt.width)} {
			if w := lipgloss.Width(got); w > tt.width {
				t.Errorf("%q is %d columns, wider than %d", got, w, tt.width)
			}
		}
	}
	if got := TruncateRight("whole", -1); got != "whole" {
		t.Errorf("a negative width should leave the string whole, got %q", got)
	}
}

// tailLinear is Tail as a linear search over head lengths, the reference
// Tail's bisection must agree with.
func tailLinear(s string, width int) string {
	if width <= 0 {
		return ""
	}
	for n := 0; ; n++ {
		head := ansi.Truncate(s, n, "")
		if tail := s[len(head):]; lipgloss.Width(tail) <= width {
			return tail
		}
	}
}

// Tail's bisection finds the same shortest head as trying each length in
// turn, for the strings where display width and ansi's count part ways.
func TestTailMatchesALinearSearch(t *testing.T) {
	sun := "☀\ufe0f"
	coder := "\U0001F469\u200d\U0001F4BB"
	keycap := "1\ufe0f\u20e3"
	for _, s := range []string{
		"",
		"plain/ascii/path",
		"日本語のプロジェクト/パス",
		"ab" + sun + "cd" + sun + sun + "e",
		"/" + strings.Repeat(keycap, 5) + "/p",
		"x" + coder + "y" + coder + coder,
		"cafe\u0301/nai\u0308ve/e\u0301\u0301",
		"mix/日本" + sun + keycap + coder + "e\u0301/end",
	} {
		for width := 0; width <= lipgloss.Width(s)+1; width++ {
			if got, want := Tail(s, width), tailLinear(s, width); got != want {
				t.Errorf("Tail(%q, %d) = %q, want %q", s, width, got, want)
			}
		}
	}
	// A skin-tone modifier after a letter measures wider cut off than in
	// place, so bisection can miss the shortest head. The tail it keeps
	// still fits, and text that fits whole comes back whole.
	for _, tt := range []struct {
		s     string
		width int
		want  string
	}{
		{"a🏽aa", 3, "a🏽aa"},
		{"~/x🏽/abcdefghij", 12, "/abcdefghij"},
	} {
		if got := Tail(tt.s, tt.width); got != tt.want {
			t.Errorf("Tail(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
		}
	}
}

func BenchmarkTail4K(b *testing.B) {
	s := strings.Repeat("work/a-long-project-name/", 4096/25+1)[:4096]
	for b.Loop() {
		Tail(s, 60)
	}
}

// A workflow's name comes from the transcript, so a bidi override or a
// newline in it doesn't reach the heading.
func TestWorkflowLabelCleansName(t *testing.T) {
	got := WorkflowLabel(models.WorkflowMeta{Name: "deploy \u202erollback\nnow", Status: "completed"})
	if want := "workflow: deploy rollback now (completed)"; got != want {
		t.Errorf("WorkflowLabel = %q, want %q", got, want)
	}
	// The status is read from the same file.
	got = WorkflowLabel(models.WorkflowMeta{Name: "deploy", Status: "\u202ecompleted\n"})
	if want := "workflow: deploy (completed)"; got != want {
		t.Errorf("WorkflowLabel = %q, want %q", got, want)
	}
	if got := WorkflowLabel(models.WorkflowMeta{Name: "deploy", Status: "\n\t"}); got != "workflow: deploy" {
		t.Errorf("a status of only control characters: %q, want none", got)
	}
}

// A name too long for WorkflowLabel's cap is cut before its status is, so
// a heading never loses its status to the cap while the others keep theirs.
func TestWorkflowLabelKeepsStatusPastTheCap(t *testing.T) {
	long := models.WorkflowMeta{RunID: "wf1", Name: "review-changes-across-the-billing-and-auth-modules", Status: "running"}
	got := WorkflowLabel(long)
	if want := "workflow: review-changes-across-the-b… (running)"; got != want {
		t.Errorf("long name: got %q, want %q", got, want)
	}
	if w := lipgloss.Width(got); w != WorkflowLabelWidth {
		t.Errorf("long name: %d wide, want the cap's %d", w, WorkflowLabelWidth)
	}
	// Cut at the cap as one string, this would end in "(co…".
	mid := models.WorkflowMeta{RunID: "wf1", Name: "review-changes-across-the-billing", Status: "completed"}
	if got, want := WorkflowLabel(mid), "workflow: review-changes-across-the… (completed)"; got != want {
		t.Errorf("mid-length name: got %q, want %q", got, want)
	}
	// A status that leaves the name no room goes, and the name takes the cap.
	odd := models.WorkflowMeta{RunID: "wf1", Name: long.Name, Status: "a-very-long-status-string-from-a-future-version"}
	if got, want := WorkflowLabel(odd), "workflow: review-changes-across-the-billing-and…"; got != want {
		t.Errorf("long status: got %q, want %q", got, want)
	}
	if !WorkflowStatusDropped(odd) {
		t.Error("long status: WorkflowStatusDropped is false")
	}
	// Room for one letter of the name and the ellipsis keeps the status;
	// less drops it.
	for status, want := range map[string]string{
		strings.Repeat("s", WorkflowLabelWidth-lipgloss.Width("workflow: x… ()")):   "workflow: r… (",
		strings.Repeat("s", WorkflowLabelWidth-lipgloss.Width("workflow: x… ()")+1): "workflow: review-changes",
	} {
		meta := models.WorkflowMeta{RunID: "wf1", Name: long.Name, Status: status}
		if got := WorkflowLabel(meta); !strings.HasPrefix(got, want) || lipgloss.Width(got) > WorkflowLabelWidth {
			t.Errorf("status of %d: got %q, want it to start %q", len(status), got, want)
		}
	}
	// A name that fits once the status goes is whole, with nothing to mark.
	for _, name := range []string{"x", strings.Repeat("n", WorkflowLabelWidth-len("workflow: "))} {
		short := odd
		short.Name = name
		if got, want := WorkflowLabel(short), "workflow: "+name; got != want || !WorkflowStatusDropped(short) {
			t.Errorf("long status, name %q: got %q, want %q, dropped", name, got, want)
		}
	}
	for _, meta := range []models.WorkflowMeta{long, mid, {RunID: "wf1"}, {RunID: "wf1", Status: "running"}} {
		if WorkflowStatusDropped(meta) {
			t.Errorf("%+v: WorkflowStatusDropped is true", meta)
		}
	}
}

// WorkflowLabel's cap is display columns, not runes: a name of wide
// characters stays within it, and a cut never leaves an emoji without its
// variation selector or a keycap without its digit's companions.
func TestWorkflowLabelCutsByWidth(t *testing.T) {
	sun := "☀\ufe0f"
	keycap := "1\ufe0f\u20e3"
	for _, name := range []string{
		strings.Repeat("日本語", 10),
		strings.Repeat(sun, 30),
		strings.Repeat(keycap, 30),
		"x" + strings.Repeat(sun, 30),
	} {
		got := WorkflowLabel(models.WorkflowMeta{Name: name, Status: "running"})
		if w := lipgloss.Width(got); w > WorkflowLabelWidth {
			t.Errorf("%q is %d columns, past the cap of %d", got, w, WorkflowLabelWidth)
		}
		body, ok := strings.CutSuffix(got, styles.Ellipsis+" (running)")
		if !ok {
			t.Errorf("%q: want the name cut and the status kept", got)
		}
		for _, piece := range []string{sun, keycap} {
			if strings.Contains(name, piece) && strings.Count(body, piece) != strings.Count(body, string([]rune(piece)[0])) {
				t.Errorf("%q splits %q", got, piece)
			}
		}
	}
	if got, want := WorkflowLabel(models.WorkflowMeta{Name: "abcdefghijklmnopqrstuvwxyz0123456789 and more"}), "workflow: abcdefghijklmnopqrstuvwxyz0123456789…"; got != want {
		t.Errorf("a cut after a space: %q, want %q", got, want)
	}
}

// ClockOn dates a time only when it's from an earlier local day than now:
// the weekday within six days, the date too before that, as breakdown's
// rows do. ClockOnShort drops the seconds of a dated time, and its weekday
// before a date.
func TestClockOn(t *testing.T) {
	orig := time.Local
	time.Local = time.FixedZone("UTC+1", 3600)
	t.Cleanup(func() { time.Local = orig })
	// Saturday 31 Oct, 08:44 local.
	now := time.Date(2026, 10, 31, 7, 44, 0, 0, time.UTC)
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"earlier today", time.Date(2026, 10, 31, 6, 9, 32, 0, time.UTC), "07:09:32"},
		// 23:30 UTC on the 30th is 00:30 local on the 31st: today.
		{"today by the local date", time.Date(2026, 10, 30, 23, 30, 0, 0, time.UTC), "00:30:00"},
		{"later than now", time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC), "10:00:00"},
		{"yesterday", time.Date(2026, 10, 30, 7, 29, 56, 0, time.UTC), "Fri 08:29:56"},
		{"five days back", time.Date(2026, 10, 26, 7, 0, 0, 0, time.UTC), "Mon 08:00:00"},
		{"six days back", time.Date(2026, 10, 25, 7, 0, 0, 0, time.UTC), "Sun 25 Oct 08:00:00"},
		{"eleven days back", time.Date(2026, 10, 20, 5, 9, 32, 0, time.UTC), "Tue 20 Oct 06:09:32"},
		{"last year", time.Date(2025, 10, 20, 5, 9, 32, 0, time.UTC), "Mon 20 Oct 2025 06:09:32"},
		{"no timestamp", time.Time{}, Clock(time.Time{})},
	}
	for _, tt := range tests {
		if got := ClockOn(tt.t, now); got != tt.want {
			t.Errorf("%s: ClockOn = %q, want %q", tt.name, got, tt.want)
		}
	}

	short := map[string]string{
		"earlier today":           "07:09:32",
		"today by the local date": "00:30:00",
		"later than now":          "10:00:00",
		"yesterday":               "Fri 08:29",
		"five days back":          "Mon 08:00",
		"six days back":           "25 Oct 08:00",
		"eleven days back":        "20 Oct 06:09",
		"last year":               "20 Oct 2025 06:09",
		"no timestamp":            Clock(time.Time{}),
	}
	for _, tt := range tests {
		if got := ClockOnShort(tt.t, now); got != short[tt.name] {
			t.Errorf("%s: ClockOnShort = %q, want %q", tt.name, got, short[tt.name])
		}
	}
}

// Span reads a session's length as Duration does under a day, and in days
// and hours from 24h on.
func TestSpan(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{9*time.Minute + 45*time.Second, "9m 45s"},
		{3*time.Hour + 6*time.Minute, "3h 6m"},
		{23*time.Hour + 59*time.Minute, "23h 59m"},
		{24 * time.Hour, "1d 0h"},
		{27*time.Hour + 6*time.Minute, "1d 3h"},
		{264*time.Hour + 9*time.Minute, "11d 0h"},
		{100*24*time.Hour + 23*time.Hour + 59*time.Minute, "100d 23h"},
	}
	for _, tt := range tests {
		if got := Span(tt.d); got != tt.want {
			t.Errorf("Span(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
