package render

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bardisty/ficha/internal/models"
	"github.com/charmbracelet/lipgloss"
)

func TestCost(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0.0, "$0.000000"},
		{0.0000001, "$0.000000"},
		{-0.5, "$-0.500000"},
		{0.123456, "$0.123456"},
		{1.5, "$1.500000"},
		{123.456789, "$123.456789"},
		{99999.99, "$99999.990000"},
	}
	for _, tc := range tests {
		if got := Cost(tc.input); got != tc.want {
			t.Errorf("Cost(%f) = %q, want %q", tc.input, got, tc.want)
		}
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
		{999999, "1000.0K"},
		{1000000, "1.00M"},
		{2500000, "2.50M"},
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
		{"claude-opus-4-9-20260101", 10, "claude-op…"},
		{"claude-opus-4-9-20260101", 12, "claude-opus…"},
		{"us.anthropic.claude-opus-4-9-v1:0", 11, "us.anthrop…"},
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
		{"input", "input"},
		{"output", "output"},
		{"cache_write_5m", "cache_write"},
		{"cache_write_1h", "cache_write"},
		{"cache_read", "cache_read"},
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
	// 50% usage: 38-char bar + 2 brackets = 40 runes.
	bar := ContextBar(50000, 50000, 100000, true)
	if !strings.HasPrefix(bar, "[") || !strings.HasSuffix(bar, "]") {
		t.Errorf("bar should be bracketed: %q", bar)
	}
	if n := len([]rune(bar)); n != 40 {
		t.Errorf("bar rune length = %d, want 40 (38 bar + 2 brackets)", n)
	}
	// no-color uses the ASCII bar fallback (D4): '#' used, '-' free.
	if !strings.ContainsRune(bar, '#') || !strings.ContainsRune(bar, '-') {
		t.Errorf("bar should contain both used (#) and free (-) segments: %q", bar)
	}
	if strings.ContainsRune(bar, '█') || strings.ContainsRune(bar, '░') {
		t.Errorf("no-color bar should not contain Unicode block glyphs: %q", bar)
	}
	if strings.Contains(bar, "\x1b[") {
		t.Errorf("noColor bar should not contain ANSI codes: %q", bar)
	}

	// maxContext == 0 returns 38 free cells with no brackets.
	if got, want := ContextBar(0, 0, 0, true), strings.Repeat("-", 38); got != want {
		t.Errorf("ContextBar(maxContext=0) = %q, want %q", got, want)
	}
}

func TestCostStyledNoColor(t *testing.T) {
	tests := []struct {
		name  string
		cost  float64
		width int
		want  string
	}{
		{"zero cost width 11", 0.0, 11, "  $0.000000"},
		{"small cost width 11", 0.05, 11, "  $0.050000"},
		{"dollar cost no pad", 1.234567, 0, "$1.234567"},
		{"exact width", 0.123456, 10, " $0.123456"},
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
	if got, want := CostStyledGreen(1.5, 11, false, true), "  $1.500000"; got != want {
		t.Errorf("CostStyledGreen = %q, want %q", got, want)
	}
	if got, want := CostStyledBoldGreen(2.0, 11, false, true), "  $2.000000"; got != want {
		t.Errorf("CostStyledBoldGreen = %q, want %q", got, want)
	}
}

// TestCostStyledColorPathsContainValue exercises the colored branches (which
// may or may not emit ANSI depending on the ambient terminal) and asserts the
// numeric value survives regardless.
func TestCostStyledColorPathsContainValue(t *testing.T) {
	for _, s := range []string{
		CostStyled(1.234567, 11, false, false),
		CostStyled(1.234567, 11, true, false),
		CostStyledGreen(1.5, 11, false, false),
		CostStyledBoldGreen(2.0, 11, false, false),
		CostWithDimDecimals(0.093528, lipgloss.Color("42"), 10),
	} {
		if !strings.Contains(s, "$") {
			t.Errorf("styled cost should contain the value: %q", s)
		}
	}
	if !strings.Contains(CostStyled(1.234567, 11, false, true), "$1.234567") {
		t.Error("plain CostStyled should contain the full cost value")
	}
}
