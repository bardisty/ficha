package render

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins a dark background. Left to detect, lipgloss would ask
// whatever terminal the tests run in the first time a test renders a color.
func TestMain(m *testing.M) {
	lipgloss.SetHasDarkBackground(true)
	os.Exit(m.Run())
}

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
		{6224.395577, "$6224.40"},
		{99999.99, "$99999.99"},
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
		{1234.56, 11, " $1234.56  "},
		{0, 8, " $0.0000"},
		// Too wide for the cell: returned whole, never cut
		{123456.78, 8, "$123456.78  "},
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
	// "$12345.67" plus the two-space decimal pad
	if got := CostCellWidth(10, 0.5, 12345.67); got != 11 {
		t.Errorf("wide cost: got %d, want 11", got)
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

func TestContextBar(t *testing.T) {
	tests := []struct {
		name  string
		size  int64
		max   int
		width int
		want  string
	}{
		{"half full: tick in the free part", 50, 100, 20, "[██████████░░░░░│░░░░]"},
		{"past compaction: tick inside the used part", 90, 100, 20, "[███████████████│██░░]"},
		{"exactly at the tick", 75, 100, 20, "[███████████████│░░░░]"},
		{"full", 100, 100, 8, "[██████│█]"},
		{"over full clamps", 150, 100, 8, "[██████│█]"},
		{"unknown window", 10, 0, 8, "[░░░░░░│░]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContextBar(tt.size, tt.max, tt.width, true)
			if got != tt.want {
				t.Errorf("ContextBar(%d, %d, %d) = %q, want %q", tt.size, tt.max, tt.width, got, tt.want)
			}
			if n := len([]rune(got)); n != tt.width+2 {
				t.Errorf("width = %d runes, want %d", n, tt.width+2)
			}
		})
	}

	// Color touches only the escapes, never the cells.
	r := lipgloss.DefaultRenderer()
	orig := r.ColorProfile()
	r.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { r.SetColorProfile(orig) })
	colored := ContextBar(90, 100, 20, false)
	if !strings.Contains(colored, "\x1b[") {
		t.Errorf("colored bar has no escapes: %q", colored)
	}

	// The ASCII glyph set: '#' used, '-' free, '|' tick.
	styles.SetASCII(true)
	t.Cleanup(func() { styles.SetASCII(false) })
	if got, want := ContextBar(50, 100, 20, true), "[##########-----|----]"; got != want {
		t.Errorf("ASCII bar = %q, want %q", got, want)
	}
}

// The note keeps its reading whole at any width, shedding the scope first.
func TestContextNote(t *testing.T) {
	tests := []struct {
		size  int64
		width int
		want  string
	}{
		{100_000, 60, "50.0K to ~compaction (main session, last request)"},
		{100_000, 40, "50.0K to ~compaction"},
		{100_000, 15, "50.0K left"},
		{100_000, 5, ""},
		{190_100, 60, "past ~compaction at 75% (main session, last request)"},
		{190_100, 30, "past ~compaction at 75%"},
		{190_100, 20, "past ~compaction"},
	}
	for _, tt := range tests {
		if got := contextNote(tt.size, 200_000, tt.width); got != tt.want {
			t.Errorf("contextNote(%d, width %d) = %q, want %q", tt.size, tt.width, got, tt.want)
		}
	}
	if got := contextNote(10, 0, 80); got != "" {
		t.Errorf("unknown window: note = %q, want none", got)
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
		{"fits: bar takes the rest", 190_100, 50,
			" 95% [████████████████████│████░░] 190.1K / 200.0K",
			"past ~compaction at 75%"},
		{"headroom before compaction", 100_000, 50,
			" 50% [█████████████░░░░░░░│░░░░░░] 100.0K / 200.0K",
			"50.0K to ~compaction (main session, last request)"},
		// The bar outranks the counts: it carries the compaction tick.
		{"narrow drops the counts before clipping", 190_100, 25,
			" 95% [██████████████│██░]", ""},
		{"narrower still keeps a floor-width bar", 190_100, 17, " 95% [████████│░]", ""},
		{"narrowest keeps the percentage", 190_100, 8, " 95%", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, note := ContextGauge(tt.size, 200_000, tt.width, true, false)
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
