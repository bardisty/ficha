package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/models"
)

func TestFormatDelta(t *testing.T) {
	tests := []struct {
		delta int64
		want  string
	}{
		{500, "(+500)"},
		{2700, "(+2.7K)"},
		{-1000, "(-1.0K)"},
		{0, ""},
	}
	for _, tt := range tests {
		if got := formatDelta(tt.delta); got != tt.want {
			t.Errorf("formatDelta(%d) = %q, want %q", tt.delta, got, tt.want)
		}
	}
}

func TestDetectChanges(t *testing.T) {
	m := NewModel("/test/path", "test-session", true, "", false)

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

// Each cache-write TTL row must highlight only its own bucket's
// delta. With the detailed breakdown present, detectChanges keys deltas per TTL
// (cache_write_5m_tokens / cache_write_1h_tokens); a detail-less legacy usage
// keeps the single flat cache_write_tokens key.
func TestDetectChanges_CacheWritePerTTLDeltas(t *testing.T) {
	withBuckets := func(fivem, oneh int64) models.TokenUsage {
		return models.TokenUsage{
			CacheCreationInputTokens: fivem + oneh,
			CacheCreation:            &models.CacheCreation{Ephemeral5mInputTokens: fivem, Ephemeral1hInputTokens: oneh},
		}
	}
	flat := func(n int64) models.TokenUsage {
		return models.TokenUsage{CacheCreationInputTokens: n}
	}

	tests := []struct {
		name           string
		old, new       models.TokenUsage
		want5m, want1h *int64
		wantFlat       *int64
	}{
		{
			name:   "5m-only write moves only the 5m row",
			old:    withBuckets(200000, 50000),
			new:    withBuckets(201000, 50000),
			want5m: ptr(1000),
		},
		{
			name:   "1h-only write moves only the 1h row",
			old:    withBuckets(200000, 50000),
			new:    withBuckets(200000, 51000),
			want1h: ptr(1000),
		},
		{
			name:   "mixed write splits the delta per TTL",
			old:    withBuckets(200000, 50000),
			new:    withBuckets(200800, 50200),
			want5m: ptr(800),
			want1h: ptr(200),
		},
		{
			name:     "detail-less usage keeps the flat key",
			old:      flat(1000),
			new:      flat(1800),
			wantFlat: ptr(800),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel("/test/path", "test-session", true, "", false)
			old := &models.SessionAnalysis{TotalUsage: tt.old, CostByModel: map[string]models.CostBreakdown{}}
			neu := &models.SessionAnalysis{TotalUsage: tt.new, CostByModel: map[string]models.CostBreakdown{}}
			m.detectChanges(old, neu)

			assertDelta := func(key string, want *int64) {
				got, exists := m.deltaTokens[key]
				if want == nil {
					if exists {
						t.Errorf("deltaTokens[%q] = %d, want absent", key, got)
					}
					return
				}
				if !exists || got != *want {
					t.Errorf("deltaTokens[%q] = %d (exists=%v), want %d", key, got, exists, *want)
				}
				if _, hl := m.changedAt[key]; !hl {
					t.Errorf("changedAt[%q] not marked despite a delta", key)
				}
			}
			assertDelta("cache_write_5m_tokens", tt.want5m)
			assertDelta("cache_write_1h_tokens", tt.want1h)
			assertDelta("cache_write_tokens", tt.wantFlat)
		})
	}
}

func ptr(n int64) *int64 { return &n }

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
			m := NewModel("/test/path", "test-session", true, "", false)
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
			m := NewModel("/test/path", "test-session", true, "", false)
			m.width = tt.width
			got := m.getChartWidth()
			if got != tt.want {
				t.Errorf("getChartWidth() = %d, want %d (width=%d)", got, tt.want, tt.width)
			}
		})
	}
}

func TestRecentlyChangedIsReadOnly(t *testing.T) {
	m := NewModel("/test/path", "test-session", false, "", false)
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
	// than the plain "loading..." string the old padding math measured
	// Cover the whole clamped panel-width range (minPanelWidth..design width),
	// both with and without a prev-session clause: the header content is widest
	// there, and overflowing the frame would drift the right border.
	// Elision must keep all three lines the same display width at every width.
	widths := []int{minPanelWidth, 42, 50, 57, 60, 74, defaultPanelWidth, 100}
	for _, noColor := range []bool{false, true} {
		for _, loading := range []bool{true, false} {
			for _, prev := range []bool{false, true} {
				for _, width := range widths {
					m := NewModel("/test/path", "0123456789abcdef", noColor, "", false)
					m.loading = loading
					if prev {
						m.prevSessionID = "fedcba9876543210"
					}
					assertPanelLinesAligned(t, m.renderHeaderPanel(width),
						fmt.Sprintf("watch noColor=%v loading=%v prev=%v width=%d", noColor, loading, prev, width))

					b := NewBreakdownModel("/test/path", "0123456789abcdef", noColor, "", false)
					b.loading = loading
					if prev {
						b.prevSessionID = "fedcba9876543210"
					}
					assertPanelLinesAligned(t, b.renderHeaderPanel(width),
						fmt.Sprintf("breakdown noColor=%v loading=%v prev=%v width=%d", noColor, loading, prev, width))
				}
			}
		}
	}
}

func TestWaitForFileChangeRegistersWaitGroupBeforeScheduling(t *testing.T) {
	// watcher is nil, so the command exits immediately once invoked; the
	// WaitGroup must still be registered when the command is constructed,
	// or a quit-time Wait can observe zero while the command is pending
	m := NewModel("/test/path", "test-session", false, "", false)
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
	m := NewModel("/test/path", "test-session", false, "", false)
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
		{42, 40},  // narrow: terminal width minus indent
		{40, 38},  // watch's minimum terminal: at the floor
		{30, 38},  // below the floor — clamped (View clips the overflow)
	}

	for _, tt := range tests {
		if got := panelWidthFor(tt.term); got != tt.want {
			t.Errorf("panelWidthFor(%d) = %d, want %d", tt.term, got, tt.want)
		}
	}
}

func TestViewFitsNarrowTerminal(t *testing.T) {
	width := 60

	m := NewModel("/test/path", "test-session", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	m = updated.(Model)
	for i, line := range strings.Split(frameOf(m), "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("watch view line %d wider than terminal: %d > %d (%q)", i, w, width, line)
		}
	}

	b := NewBreakdownModel("/test/path", "test-session", true, "", false)
	bUpdated, _ := b.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	b = bUpdated.(BreakdownModel)
	for i, line := range strings.Split(frameOf(b), "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("breakdown view line %d wider than terminal: %d > %d (%q)", i, w, width, line)
		}
	}
}
