package tui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/bardisty/ficha/internal/models"
)

func TestRollingRate(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	at := func(minAgo float64, cost float64) models.MessageAnalysis {
		return models.MessageAnalysis{
			Timestamp: now.Add(-time.Duration(minAgo * float64(time.Minute))),
			Cost:      models.CostBreakdown{TotalCost: cost},
		}
	}

	tests := []struct {
		name   string
		msgs   []models.MessageAnalysis
		want   float64
		wantOK bool
	}{
		{name: "no messages", wantOK: false},
		{name: "session younger than the window", msgs: []models.MessageAnalysis{at(3, 5), at(1, 5)}, wantOK: false},
		{name: "window sums only recent spend", msgs: []models.MessageAnalysis{at(30, 100), at(9, 0.50), at(2, 0.70)}, want: 7.20, wantOK: true},
		{name: "idle session decays to zero", msgs: []models.MessageAnalysis{at(60, 3), at(15, 2)}, want: 0, wantOK: true},
		{name: "a message exactly at the window edge is out", msgs: []models.MessageAnalysis{at(10, 1)}, want: 0, wantOK: true},
		{
			name: "timestampless messages are skipped",
			msgs: []models.MessageAnalysis{at(20, 1), {Cost: models.CostBreakdown{TotalCost: 50}}, at(1, 1)},
			want: 6, wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := rollingRate(tt.msgs, now, 10*time.Minute)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && !almostEqual(got, tt.want) {
				t.Errorf("rate = %v, want %v", got, tt.want)
			}
		})
	}
}

func almostEqual(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}

func TestWrapWords(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{"fits", "a bb ccc", 20, []string{"a bb ccc"}},
		{"wraps at words", "aaa bbb ccc", 7, []string{"aaa bbb", "ccc"}},
		{"cuts the last row with an ellipsis", "aaa bbb ccc ddd eee", 7, []string{"aaa bbb", "ccc dd…"}},
		{"clips an overlong word", "abcdefghij", 4, []string{"abcd"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapWords(tt.text, tt.width, 2)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("wrapWords(%q, %d) = %q, want %q", tt.text, tt.width, got, tt.want)
			}
			for _, row := range got {
				if w := lipgloss.Width(row); w > tt.width {
					t.Errorf("row %q is %d wide, over %d", row, w, tt.width)
				}
			}
		})
	}
}

func TestPackNotes(t *testing.T) {
	notes := []string{"! 1 skipped line(s)", "! * = fallback pricing"}
	tests := []struct {
		width int
		want  []string
	}{
		{60, []string{"! 1 skipped line(s) | ! * = fallback pricing"}},
		{30, []string{"! 1 skipped line(s)", "! * = fallback pricing"}},
		{12, []string{"! 1 skipped", "line(s)…"}},
	}
	for _, tt := range tests {
		got := packNotes(notes, " | ", tt.width, 2)
		if strings.Join(got, "/") != strings.Join(tt.want, "/") {
			t.Errorf("width %d: got %q, want %q", tt.width, got, tt.want)
		}
	}
}

// tallAnalysis has enough agents that the body overflows a 24-row terminal.
func tallAnalysis(cost float64) *models.SessionAnalysis {
	a := &models.SessionAnalysis{
		SessionID:    "sess",
		MessageCount: 40,
		TotalCost:    models.CostBreakdown{InputCost: cost, TotalCost: cost},
		TotalUsage:   models.TokenUsage{InputTokens: 1000},
		CostByModel:  map[string]models.CostBreakdown{"claude-opus-4-8": {TotalCost: cost}},
		HasAgents:    true,
	}
	for i := 0; i < 30; i++ {
		a.Agents = append(a.Agents, models.AgentAnalysis{
			AgentID:      strings.Repeat(string(rune('a'+i%26)), 12),
			MessageCount: 1,
			TotalCost:    models.CostBreakdown{TotalCost: 0.1},
		})
	}
	return a
}

func sized(t *testing.T, m Model, w, h int) Model {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(Model)
}

func load(t *testing.T, m Model, a *models.SessionAnalysis) Model {
	t.Helper()
	updated, _ := m.Update(analysisMsg{analysis: a})
	return updated.(Model)
}

func key(t *testing.T, m Model, k string) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	return updated.(Model)
}

// watch opens at the top so the total is on the first screen, and a reload
// or resize never moves the reader.
func TestWatchOpensAtTopAndKeepsPosition(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	m := NewModel("/fixture/sess.jsonl", "sess", false, true, "", false)
	m = sized(t, m, 80, 24)
	m = load(t, m, tallAnalysis(1))

	if m.viewport.YOffset != 0 {
		t.Fatalf("first load: YOffset = %d, want 0", m.viewport.YOffset)
	}
	if !strings.Contains(m.View(), "TOTAL ]") {
		t.Fatalf("first screen lacks the total:\n%s", m.View())
	}

	for i := 0; i < 5; i++ {
		m = key(t, m, "j")
	}
	m = load(t, m, tallAnalysis(2))
	if m.viewport.YOffset != 5 {
		t.Errorf("after reload: YOffset = %d, want 5", m.viewport.YOffset)
	}

	m = key(t, m, "G")
	m = load(t, m, tallAnalysis(3))
	if !m.viewport.AtBottom() {
		t.Errorf("reload at the bottom moved the view: YOffset = %d", m.viewport.YOffset)
	}
	m = key(t, m, "g")
	m = load(t, m, tallAnalysis(4))
	if m.viewport.YOffset != 0 {
		t.Errorf("reload after g: YOffset = %d, want 0 (no snap to bottom)", m.viewport.YOffset)
	}

	for i := 0; i < 3; i++ {
		m = key(t, m, "j")
	}
	m = sized(t, m, 100, 30)
	if m.viewport.YOffset != 3 {
		t.Errorf("after resize: YOffset = %d, want 3", m.viewport.YOffset)
	}

	// A window tall enough to show everything clamps the offset rather than
	// leaving blank rows below the body.
	m = sized(t, m, 100, 200)
	if m.viewport.PastBottom() || m.viewport.YOffset != 0 {
		t.Errorf("tall resize: YOffset = %d, want 0", m.viewport.YOffset)
	}
}

// The footer rule says how much of the body is off-screen, in each direction.
func TestWatchFooterRuleShowsOverflow(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	m := NewModel("/fixture/sess.jsonl", "sess", false, true, "", false)
	m = sized(t, m, 80, 24)
	m = load(t, m, tallAnalysis(1))

	below := m.viewport.TotalLineCount() - m.viewport.Height
	view := m.View()
	if strings.Contains(view, "↑ ") {
		t.Errorf("top of body shows an above-marker:\n%s", view)
	}
	if want := "↓ " + strconv.Itoa(below) + " more"; !strings.Contains(view, want) {
		t.Errorf("missing %q:\n%s", want, view)
	}

	m = key(t, m, "j")
	m = key(t, m, "j")
	if want := "↑ 2 more  ↓ " + strconv.Itoa(below-2) + " more"; !strings.Contains(m.View(), want) {
		t.Errorf("missing %q:\n%s", want, m.View())
	}

	m = key(t, m, "G")
	if strings.Contains(m.View(), "↓ ") {
		t.Errorf("bottom of body still shows a below-marker:\n%s", m.View())
	}

	// A body that fits shows a plain rule.
	m = sized(t, m, 80, 200)
	if strings.Contains(m.View(), " more ") {
		t.Errorf("fitting body shows an overflow marker:\n%s", m.View())
	}
}

// Warnings take their own rows, wrap rather than clip, and the frame still
// fits the terminal with them.
func TestWatchWarningRows(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	a := tallAnalysis(1)
	a.SkippedLines = 2
	a.EstimatedCostMessages = 3
	a.CostByModel["claude-mystery-9"] = models.CostBreakdown{TotalCost: 0.5}

	for _, w := range []int{80, 60} {
		m := NewModel("/fixture/sess.jsonl", "sess", false, true, "", false)
		m = sized(t, m, w, 24)
		m = load(t, m, a)
		view := m.View()

		flat := strings.Join(strings.Fields(view), " ")
		for _, want := range []string{"2 skipped line(s), 3 estimated cost(s)", "* = fallback pricing"} {
			if !strings.Contains(flat, want) {
				t.Errorf("width %d: warnings lost %q:\n%s", w, want, view)
			}
		}
		if lines := strings.Count(view, "\n") + 1; lines > 24 {
			t.Errorf("width %d: frame is %d rows, over the terminal's 24", w, lines)
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > w {
				t.Errorf("width %d: line overflows: %q", w, line)
			}
		}
		rows := len(m.warningRows(panelWidthFor(w)))
		if w == 80 && rows != 1 {
			t.Errorf("width 80: %d warning rows, want 1", rows)
		}
		if w == 60 && rows != 2 {
			t.Errorf("width 60: %d warning rows, want 2", rows)
		}
	}

	// No warnings, no row.
	m := NewModel("/fixture/sess.jsonl", "sess", false, true, "", false)
	m = sized(t, m, 80, 24)
	m = load(t, m, tallAnalysis(1))
	if got := m.footerHeight(); got != watchFooterBase {
		t.Errorf("clean session footer height = %d, want %d", got, watchFooterBase)
	}
}

// The stats line shows "-" until the session spans the rate window, and drops
// its least important segments first on a narrow terminal.
func TestWatchStatsLine(t *testing.T) {
	forceProfile(t, termenv.Ascii)
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	a := tallAnalysis(30.05)
	a.MessageCount = 400
	a.Duration = models.Duration(3*time.Hour + 12*time.Minute)
	a.Messages = []models.MessageAnalysis{
		{Timestamp: now.Add(-3 * time.Minute), Cost: models.CostBreakdown{TotalCost: 1.20}},
	}

	m := NewModel("/fixture/sess.jsonl", "sess", false, true, "", false)
	m.now = func() time.Time { return now }
	m = sized(t, m, 80, 24)
	m = load(t, m, a)
	if got, want := m.renderStatsLine(78), "$30.05 API est. │ -/h (10m) │ 400 msgs │ 3h 12m"; got != want {
		t.Errorf("young session:\n got %q\nwant %q", got, want)
	}

	a.Messages = append([]models.MessageAnalysis{
		{Timestamp: now.Add(-3 * time.Hour), Cost: models.CostBreakdown{TotalCost: 5}},
	}, a.Messages...)
	m = load(t, m, a)
	if got, want := m.renderStatsLine(78), "$30.05 API est. │ $7.20/h (10m) │ 400 msgs │ 3h 12m"; got != want {
		t.Errorf("rate:\n got %q\nwant %q", got, want)
	}
	if got, want := m.renderStatsLine(40), "$30.05 │ $7.20/h (10m) │ 400 msgs"; got != want {
		t.Errorf("narrow:\n got %q\nwant %q", got, want)
	}
	if got, want := m.renderStatsLine(45), "$30.05 │ $7.20/h (10m) │ 400 msgs │ 3h 12m"; got != want {
		t.Errorf("label goes before readings:\n got %q\nwant %q", got, want)
	}
	if got, want := m.renderStatsLine(5), "$30.05"; got != want {
		t.Errorf("too narrow keeps the total:\n got %q\nwant %q", got, want)
	}
}
