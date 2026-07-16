package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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
		// A terminal narrower than 8 columns must clamp to 1, not go negative —
		// a negative width panics sparkline's canvas make().
		{"width 8 clamps to 1", 8, 1},
		{"width 7 clamps to 1", 7, 1},
		{"width 1 clamps to 1", 1, 1},
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

func TestRecentlyChangedIsReadOnly(t *testing.T) {
	m := NewModel("/test/path", "test-session", false, false, "", false)
	m.changedAt["stale"] = time.Now().Add(-3 * highlightDuration)
	m.deltaTokens["stale"] = 42
	m.changedAt["fresh"] = time.Now()

	if m.recentlyChanged("stale") {
		t.Error("stale entry should not count as recently changed")
	}
	if !m.recentlyChanged("fresh") {
		t.Error("fresh entry should count as recently changed")
	}

	// recentlyChanged is reached from View(), which must not mutate model
	// state — pruning is cleanupStaleChanges' job (tick handler)
	if _, exists := m.changedAt["stale"]; !exists {
		t.Error("recentlyChanged must not delete changedAt entries")
	}
	if _, exists := m.deltaTokens["stale"]; !exists {
		t.Error("recentlyChanged must not delete deltaTokens entries")
	}

	m.cleanupStaleChanges()
	if _, exists := m.changedAt["stale"]; exists {
		t.Error("cleanupStaleChanges should prune stale entries")
	}
	if _, exists := m.deltaTokens["stale"]; exists {
		t.Error("cleanupStaleChanges should prune stale delta entries")
	}
	if _, exists := m.changedAt["fresh"]; !exists {
		t.Error("cleanupStaleChanges should keep fresh entries")
	}
}

// assertPanelLinesAligned checks that all rendered panel lines share the same
// display width (i.e. the right border doesn't drift).
func assertPanelLinesAligned(t *testing.T, panel, label string) {
	t.Helper()
	lines := strings.Split(panel, "\n")
	if len(lines) != 3 {
		t.Fatalf("%s: expected 3 panel lines, got %d", label, len(lines))
	}
	want := lipgloss.Width(lines[0])
	for i, line := range lines[1:] {
		if got := lipgloss.Width(line); got != want {
			t.Errorf("%s: line %d width = %d, want %d", label, i+1, got, want)
		}
	}
}

func TestRenderHeaderPanelAlignment(t *testing.T) {
	// Force color so the loading status includes the spinner, which is wider
	// than the plain "Loading..." string the old padding math measured
	r := lipgloss.DefaultRenderer()
	origProfile := r.ColorProfile()
	r.SetColorProfile(termenv.ANSI256)
	defer r.SetColorProfile(origProfile)

	for _, loading := range []bool{true, false} {
		m := NewModel("/test/path", "0123456789abcdef", false, false, "", false)
		m.loading = loading
		assertPanelLinesAligned(t, m.renderHeaderPanel(76), fmt.Sprintf("watch loading=%v", loading))

		b := NewBreakdownModel("/test/path", "0123456789abcdef", false, "", false)
		b.loading = loading
		assertPanelLinesAligned(t, b.renderHeaderPanel(76), fmt.Sprintf("breakdown loading=%v", loading))
	}
}

func TestWaitForFileChangeRegistersWaitGroupBeforeScheduling(t *testing.T) {
	// watcher is nil, so the command exits immediately once invoked; the
	// WaitGroup must still be registered when the command is constructed,
	// or a quit-time Wait can observe zero while the command is pending
	m := NewModel("/test/path", "test-session", false, false, "", false)
	cmd := waitForFileChangeCmd(m.wg, m.closing, nil, m.done, m.sessionPath)

	waitDone := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		t.Fatal("wg.Wait returned before the scheduled command ran — Add must precede Wait")
	case <-time.After(50 * time.Millisecond):
	}

	if msg := cmd(); msg != nil {
		t.Fatalf("expected nil msg from no-watcher command, got %v", msg)
	}

	select {
	case <-waitDone:
	case <-time.After(time.Second):
		t.Fatal("wg.Wait did not return after the command completed")
	}
}

func TestWaitForNewSessionNilWatcherReturnsNilCmd(t *testing.T) {
	// With no session watcher there is nothing to wait for; a nil command
	// keeps the WaitGroup untouched (tea.Batch ignores nil commands)
	m := NewModel("/test/path", "test-session", false, false, "", false)
	if m.waitForNewSession() != nil {
		t.Error("watch model: expected nil cmd when sessionWatcher is nil")
	}

	b := NewBreakdownModel("/test/path", "test-session", false, "", false)
	if b.waitForNewSession() != nil {
		t.Error("breakdown model: expected nil cmd when sessionWatcher is nil")
	}
}

func TestPanelWidthFor(t *testing.T) {
	tests := []struct {
		term int
		want int
	}{
		{0, 76},   // no WindowSizeMsg yet — design width
		{-1, 76},  // defensive
		{120, 76}, // wide terminals cap at the design width
		{78, 76},  // exact fit including the 2-column indent
		{60, 58},  // narrow: terminal width minus indent
		{42, 40},  // at the floor
		{30, 40},  // below the floor — clamped (View clips the overflow)
	}

	for _, tt := range tests {
		if got := panelWidthFor(tt.term); got != tt.want {
			t.Errorf("panelWidthFor(%d) = %d, want %d", tt.term, got, tt.want)
		}
	}
}

func TestViewFitsNarrowTerminal(t *testing.T) {
	width := 60

	m := NewModel("/test/path", "test-session", false, true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	m = updated.(Model)
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("watch view line %d wider than terminal: %d > %d (%q)", i, w, width, line)
		}
	}

	b := NewBreakdownModel("/test/path", "test-session", true, "", false)
	bUpdated, _ := b.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	b = bUpdated.(BreakdownModel)
	for i, line := range strings.Split(b.View(), "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("breakdown view line %d wider than terminal: %d > %d (%q)", i, w, width, line)
		}
	}
}
