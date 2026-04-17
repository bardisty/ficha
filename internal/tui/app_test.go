package tui

import (
	"strings"
	"testing"

	"github.com/bardisty/ccusage/internal/models"
)

func TestFormatNumberWithDelta(t *testing.T) {
	tests := []struct {
		name      string
		n         int64
		delta     int64
		showDelta bool
		wantSub   string // substring that must appear
		wantNot   string // substring that must NOT appear (empty = skip check)
	}{
		{
			name:      "showDelta false ignores delta",
			n:         1000,
			delta:     500,
			showDelta: false,
			wantSub:   "1.0K",
			wantNot:   "+",
		},
		{
			name:      "positive delta",
			n:         5000,
			delta:     2000,
			showDelta: true,
			wantSub:   "(+2.0K)",
		},
		{
			name:      "negative delta",
			n:         3000,
			delta:     -1000,
			showDelta: true,
			wantSub:   "(-1.0K)",
		},
		{
			name:      "zero delta no suffix",
			n:         1000,
			delta:     0,
			showDelta: true,
			wantSub:   "1.0K",
			wantNot:   "(",
		},
		{
			name:      "small numbers",
			n:         42,
			delta:     10,
			showDelta: true,
			wantSub:   "(+10)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatNumberWithDelta(tt.n, tt.delta, tt.showDelta)
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("formatNumberWithDelta(%d, %d, %v) = %q, want substring %q",
					tt.n, tt.delta, tt.showDelta, got, tt.wantSub)
			}
			if tt.wantNot != "" && strings.Contains(got, tt.wantNot) {
				t.Errorf("formatNumberWithDelta(%d, %d, %v) = %q, should not contain %q",
					tt.n, tt.delta, tt.showDelta, got, tt.wantNot)
			}
		})
	}
}

func TestFormatCostStyled_NoColor(t *testing.T) {
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
			got := formatCostStyled(tt.cost, tt.width, false, true)
			if got != tt.want {
				t.Errorf("formatCostStyled(%f, %d, false, true) = %q, want %q",
					tt.cost, tt.width, got, tt.want)
			}
			// No ANSI codes in noColor mode
			if strings.Contains(got, "\x1b[") {
				t.Errorf("noColor output should not contain ANSI codes: %q", got)
			}
		})
	}
}

func TestFormatCostStyledGreen_NoColor(t *testing.T) {
	got := formatCostStyledGreen(1.5, 11, false, true)
	want := "  $1.500000"
	if got != want {
		t.Errorf("formatCostStyledGreen(1.5, 11, false, true) = %q, want %q", got, want)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("noColor output should not contain ANSI codes: %q", got)
	}
}

func TestFormatCostStyledBoldGreen_NoColor(t *testing.T) {
	got := formatCostStyledBoldGreen(2.0, 11, false, true)
	want := "  $2.000000"
	if got != want {
		t.Errorf("formatCostStyledBoldGreen(2.0, 11, false, true) = %q, want %q", got, want)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("noColor output should not contain ANSI codes: %q", got)
	}
}

func TestFormatContextProgressBar_NoColor(t *testing.T) {
	// 50% usage: contextSize=50000, freeSpace=50000, maxContext=100000
	bar := formatContextProgressBar(50000, 50000, 100000, true)

	// Must start with [ and end with ]
	if !strings.HasPrefix(bar, "[") || !strings.HasSuffix(bar, "]") {
		t.Errorf("bar should start with [ and end with ]: got %q", bar)
	}

	// Total rune width should be 38 (bar) + 2 (brackets) = 40
	runeCount := len([]rune(bar))
	if runeCount != 40 {
		t.Errorf("bar rune length = %d, want 40", runeCount)
	}

	// Should contain used and free segment types
	if !strings.ContainsRune(bar, '\u2588') { // █
		t.Error("bar should contain filled segments (█)")
	}
	if !strings.ContainsRune(bar, '\u2591') { // ░
		t.Error("bar should contain free segments (░)")
	}

	// No ANSI
	if strings.Contains(bar, "\x1b[") {
		t.Errorf("noColor bar should not contain ANSI codes: %q", bar)
	}
}

func TestFormatContextProgressBar_MaxContextZero(t *testing.T) {
	bar := formatContextProgressBar(0, 0, 0, true)
	// maxContext=0 early-returns all free blocks without brackets
	expected := strings.Repeat("░", 38)
	if bar != expected {
		t.Errorf("maxContext=0 bar = %q, want %q", bar, expected)
	}
}

func TestTruncateID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"short ID", "abc", "abc"},
		{"exactly 8 chars", "12345678", "12345678"},
		{"longer than 8 chars", "123456789", "12345678"},
		{"UUID", "550e8400-e29b-41d4-a716-446655440000", "550e8400"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateID(tt.id)
			if got != tt.want {
				t.Errorf("truncateID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestDetectChanges(t *testing.T) {
	m := NewModel("/test/path", "test-session", false, true, "", false)

	oldAnalysis := &models.SessionAnalysis{
		TotalCost: models.CostBreakdown{
			InputCost:  0.10,
			OutputCost: 0.05,
			TotalCost:  0.15,
		},
		TotalUsage: models.TokenUsage{
			InputTokens:  1000,
			OutputTokens: 500,
		},
		MessageCount: 5,
		CostByModel:  map[string]models.CostBreakdown{},
	}

	newAnalysis := &models.SessionAnalysis{
		TotalCost: models.CostBreakdown{
			InputCost:  0.20,
			OutputCost: 0.05, // unchanged
			TotalCost:  0.25,
		},
		TotalUsage: models.TokenUsage{
			InputTokens:  2000,
			OutputTokens: 500, // unchanged
		},
		MessageCount: 7,
		CostByModel:  map[string]models.CostBreakdown{},
	}

	m.detectChanges(oldAnalysis, newAnalysis)

	// Input cost changed
	if _, exists := m.changedAt["input_cost"]; !exists {
		t.Error("expected input_cost to be marked as changed")
	}

	// Output cost unchanged
	if _, exists := m.changedAt["output_cost"]; exists {
		t.Error("output_cost should not be marked as changed")
	}

	// Total cost changed
	if _, exists := m.changedAt["total"]; !exists {
		t.Error("expected total to be marked as changed")
	}

	// Input tokens delta tracked
	if delta, exists := m.deltaTokens["input_tokens"]; !exists || delta != 1000 {
		t.Errorf("deltaTokens[input_tokens] = %d, want 1000", delta)
	}

	// Output tokens unchanged
	if _, exists := m.deltaTokens["output_tokens"]; exists {
		t.Error("output_tokens delta should not exist (unchanged)")
	}

	// Message count delta
	if m.deltaCount != 2 {
		t.Errorf("deltaCount = %d, want 2", m.deltaCount)
	}
}

func TestIsEmptySession(t *testing.T) {
	tests := []struct {
		name     string
		analysis *models.SessionAnalysis
		want     bool
	}{
		{
			"nil analysis",
			nil,
			true,
		},
		{
			"zero cost and zero messages",
			&models.SessionAnalysis{
				TotalCost:    models.CostBreakdown{TotalCost: 0},
				MessageCount: 0,
			},
			true,
		},
		{
			"nonzero cost",
			&models.SessionAnalysis{
				TotalCost:    models.CostBreakdown{TotalCost: 0.05},
				MessageCount: 0,
			},
			false,
		},
		{
			"nonzero messages",
			&models.SessionAnalysis{
				TotalCost:    models.CostBreakdown{TotalCost: 0},
				MessageCount: 3,
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel("/test/path", "test-session", false, true, "", false)
			m.analysis = tt.analysis
			got := m.isEmptySession()
			if got != tt.want {
				t.Errorf("isEmptySession() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetChartWidth(t *testing.T) {
	tests := []struct {
		name  string
		width int
		want  int
	}{
		{"zero width returns default", 0, 68},
		{"wide terminal", 80, 68},       // 80-8=72 > 68, capped at 68
		{"narrow terminal", 50, 42},     // 50-8=42 < 68, use 42
		{"very narrow terminal", 10, 2}, // 10-8=2 < 68, use 2
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel("/test/path", "test-session", false, true, "", false)
			m.width = tt.width
			got := m.getChartWidth()
			if got != tt.want {
				t.Errorf("getChartWidth() = %d, want %d (width=%d)", got, tt.want, tt.width)
			}
		})
	}
}

func TestGetPrimaryModel(t *testing.T) {
	tests := []struct {
		name        string
		costByModel map[string]models.CostBreakdown
		want        string
	}{
		{
			"multiple models returns highest cost",
			map[string]models.CostBreakdown{
				"claude-sonnet-4":  {TotalCost: 0.50},
				"claude-opus-4-5":  {TotalCost: 1.20},
				"claude-haiku-4-5": {TotalCost: 0.05},
			},
			"Opus 4.5",
		},
		{
			"single model",
			map[string]models.CostBreakdown{
				"claude-sonnet-4": {TotalCost: 0.50},
			},
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
			got := getPrimaryModel(tt.costByModel)
			if got != tt.want {
				t.Errorf("getPrimaryModel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatCostComponentLabel(t *testing.T) {
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
		t.Run(tt.component, func(t *testing.T) {
			got := formatCostComponentLabel(tt.component)
			if got != tt.want {
				t.Errorf("formatCostComponentLabel(%q) = %q, want %q", tt.component, got, tt.want)
			}
		})
	}
}

func TestFormatCostStyled_WithColor(t *testing.T) {
	// noColor=false takes different code path than noColor=true
	// In non-TTY test environments, lipgloss may strip ANSI codes,
	// so we verify the code path is exercised and output contains the cost value
	colored := formatCostStyled(1.234567, 11, false, false)
	plain := formatCostStyled(1.234567, 11, false, true)

	// Both should contain the cost digits
	if !strings.Contains(colored, "1.23") {
		t.Errorf("colored formatCostStyled should contain cost digits: %q", colored)
	}
	if !strings.Contains(plain, "1.23") {
		t.Errorf("plain formatCostStyled should contain cost digits: %q", plain)
	}

	// With a TTY, colored output would contain ANSI; without, they may be equal
	// The important thing is that both paths produce valid output
	if !strings.Contains(plain, "$1.234567") {
		t.Errorf("plain output should contain full cost: %q", plain)
	}
}

func TestFormatCostStyledGreen_WithColor(t *testing.T) {
	colored := formatCostStyledGreen(1.5, 11, false, false)
	plain := formatCostStyledGreen(1.5, 11, false, true)

	// Both should contain cost digits
	if !strings.Contains(colored, "1.50") {
		t.Errorf("colored output should contain cost digits: %q", colored)
	}
	if !strings.Contains(plain, "$1.500000") {
		t.Errorf("plain output should contain full cost: %q", plain)
	}
}

func TestFormatCostStyledBoldGreen_WithColor(t *testing.T) {
	colored := formatCostStyledBoldGreen(2.0, 11, false, false)
	plain := formatCostStyledBoldGreen(2.0, 11, false, true)

	// Both should contain cost digits
	if !strings.Contains(colored, "2.00") {
		t.Errorf("colored output should contain cost digits: %q", colored)
	}
	if !strings.Contains(plain, "$2.000000") {
		t.Errorf("plain output should contain full cost: %q", plain)
	}
}
