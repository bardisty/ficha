package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// bdMsg builds a message with a fixed timestamp (minutes offset) and optional
// agent ID. Its identity — what highlight tracking keys on — is stable across
// reloads regardless of the row's Index, so tests can add rows and reorder them.
func bdMsg(index, minute int, agentID string) models.BreakdownMessage {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	return models.BreakdownMessage{
		Index:     index,
		AgentID:   agentID,
		Timestamp: base.Add(time.Duration(minute) * time.Minute),
	}
}

func TestBreakdownModel_DetectNewMessages(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	// Currently displayed: three messages at minutes 0, 1, 2.
	m.messages = []models.BreakdownMessage{bdMsg(1, 0, ""), bdMsg(2, 1, ""), bdMsg(3, 2, "")}

	// Reload appends two more (minutes 3, 4). Indices are irrelevant to detection.
	newMessages := []models.BreakdownMessage{
		bdMsg(1, 0, ""), bdMsg(2, 1, ""), bdMsg(3, 2, ""), bdMsg(4, 3, ""), bdMsg(5, 4, ""),
	}
	m.detectNewMessages(newMessages)

	if !m.isNewMessage(bdMsg(4, 3, "")) {
		t.Error("expected the minute-3 message to be marked new")
	}
	if !m.isNewMessage(bdMsg(5, 4, "")) {
		t.Error("expected the minute-4 message to be marked new")
	}
	if m.isNewMessage(bdMsg(1, 0, "")) {
		t.Error("the minute-0 message already existed and should not be new")
	}
	if m.isNewMessage(bdMsg(3, 2, "")) {
		t.Error("the minute-2 message already existed and should not be new")
	}
}

// A newly discovered agent message whose timestamp precedes existing
// rows inserts mid-list; the sort then renumbers the old tail rows past the old
// count. Identity-based detection must flag only the inserted agent row, not the
// shifted parent rows.
func TestBreakdownModel_DetectNewMessages_MidListInsertion(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	// Displayed: three parent rows at minutes 0, 2, 4 (indices 1..3).
	m.messages = []models.BreakdownMessage{bdMsg(1, 0, ""), bdMsg(2, 2, ""), bdMsg(3, 4, "")}

	// Reload: an agent row at minute 1 lands mid-list; after the timestamp sort
	// and 1..N reindex the parent rows shift to indices 3 and 4.
	agentRow := bdMsg(2, 1, "x")
	reloaded := []models.BreakdownMessage{
		bdMsg(1, 0, ""), agentRow, bdMsg(3, 2, ""), bdMsg(4, 4, ""),
	}
	m.detectNewMessages(reloaded)

	if !m.isNewMessage(agentRow) {
		t.Error("the inserted agent row should be flagged new")
	}
	// The parent row now at index 4 must NOT be flagged just because its index
	// exceeds the old count of 3 — that was the positional-key bug.
	if m.isNewMessage(bdMsg(4, 4, "")) {
		t.Error("a pre-existing parent row must not flash as new after a mid-list insertion shifted its index")
	}
	if m.isNewMessage(bdMsg(3, 2, "")) {
		t.Error("a pre-existing parent row must not be flagged after reindexing")
	}
}

func TestBreakdownModel_IsNewMessage(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	newRow := bdMsg(5, 5, "")
	m.newMsgKeys[breakdownMsgKey(newRow)] = time.Now()

	if !m.isNewMessage(newRow) {
		t.Error("expected the tracked message to be highlighted")
	}
	if m.isNewMessage(bdMsg(10, 10, "")) {
		t.Error("expected an untracked message to not be highlighted")
	}
}

func TestBreakdownModel_IsNewMessage_NoColor(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", true, "", false) // noColor = true

	newRow := bdMsg(5, 5, "")
	m.newMsgKeys[breakdownMsgKey(newRow)] = time.Now()

	if m.isNewMessage(newRow) {
		t.Error("expected no highlight in noColor mode")
	}
}

func TestBreakdownModel_CleanupExpiredHighlights(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	expired := bdMsg(1, 0, "")
	fresh := bdMsg(2, 1, "")
	m.newMsgKeys[breakdownMsgKey(expired)] = time.Now().Add(-3 * time.Second) // highlightDuration is 2s
	m.newMsgKeys[breakdownMsgKey(fresh)] = time.Now()

	m.cleanupExpiredHighlights()

	if _, exists := m.newMsgKeys[breakdownMsgKey(expired)]; exists {
		t.Error("expected expired highlight to be cleaned up")
	}
	if _, exists := m.newMsgKeys[breakdownMsgKey(fresh)]; !exists {
		t.Error("expected fresh highlight to remain")
	}
}

func TestBreakdownModel_RenderRow(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", true, "", false) // noColor for predictable output

	msg := models.BreakdownMessage{
		Index:     42,
		AgentID:   "",
		Timestamp: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC),
		Model:     "claude-sonnet-4",
		Usage: models.TokenUsage{
			InputTokens:          1234,
			OutputTokens:         56,
			CacheReadInputTokens: 89300,
		},
		Cost: models.CostBreakdown{
			TotalCost: 0.0512,
		},
	}

	row := m.renderRow(msg, false, newBreakdownLayout(2, 0, 0, 0))

	// Check that the row contains expected values
	if !strings.Contains(row, "42") {
		t.Error("row should contain index 42")
	}
	if !strings.Contains(row, "14:30:45") {
		t.Error("row should contain timestamp")
	}
	if !strings.Contains(row, "Sonnet 4") {
		t.Error("row should contain model name 'Sonnet 4'")
	}
	if !strings.Contains(row, "$0.0512") || strings.Contains(row, "$0.05120") {
		t.Error("row should contain the cost with 4 decimal places")
	}
	if !strings.Contains(row, "1.2K") {
		t.Error("row should contain input tokens")
	}
	// No per-row change arrow: it compared each row with whichever stream
	// wrote the row before it
	for _, arrow := range []string{"↑", "↓", "·"} {
		if strings.Contains(row, arrow) {
			t.Errorf("row should carry no change arrow, got %q", row)
		}
	}
}

func TestBreakdownModel_RenderRow_WithAgent(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", true, "", false) // noColor

	msg := models.BreakdownMessage{
		Index:     10,
		AgentID:   "g7h8i9j0k1l2", // real 12-char ID — marker shows the first 7
		Timestamp: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC),
		Model:     "claude-haiku-4-5",
		Usage: models.TokenUsage{
			InputTokens:  100,
			OutputTokens: 50,
		},
		Cost: models.CostBreakdown{
			TotalCost: 0.001,
		},
	}

	row := m.renderRow(msg, false, newBreakdownLayout(2, 10, 10, 0))

	// Check agent marker shows the truncated real ID
	if !strings.Contains(row, "[Ag7h8i9j]") {
		t.Errorf("row should contain agent marker [Ag7h8i9j], got %q", row)
	}
	// Check model name is present (agent using Haiku)
	if !strings.Contains(row, "Haiku 4.5") {
		t.Error("row should contain model name 'Haiku 4.5'")
	}

	// A short ID is shown whole
	msg.AgentID = "w1"
	row = m.renderRow(msg, false, newBreakdownLayout(2, 10, 10, 0))
	if !strings.Contains(row, "[Aw1]") {
		t.Errorf("row should contain agent marker [Aw1], got %q", row)
	}
}

func TestBreakdownModel_RenderRow_ANSICodes(t *testing.T) {
	msg := models.BreakdownMessage{
		Index:     1,
		Timestamp: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC),
		Model:     "claude-sonnet-4",
		Usage:     models.TokenUsage{InputTokens: 100, OutputTokens: 50},
		Cost:      models.CostBreakdown{TotalCost: 0.05},
	}

	// noColor=false takes color code path
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)
	coloredRow := m.renderRow(msg, false, newBreakdownLayout(2, 0, 0, 0))

	// noColor=true takes plain code path - must not contain ANSI
	m2 := NewBreakdownModel("/test/path", "test-session", true, "", false)
	plainRow := m2.renderRow(msg, false, newBreakdownLayout(2, 0, 0, 0))

	if strings.Contains(plainRow, "\x1b[") {
		t.Error("noColor output should not contain ANSI escape codes")
	}

	// Both rows should contain essential data
	for _, row := range []string{coloredRow, plainRow} {
		if !strings.Contains(row, "14:30:45") {
			t.Errorf("row should contain timestamp: %q", row)
		}
		if !strings.Contains(row, "Sonnet 4") {
			t.Errorf("row should contain model name: %q", row)
		}
	}

	// In a TTY environment, colored output would contain ANSI codes;
	// in non-TTY test environments, lipgloss may strip them.
	// Verify the colored path was exercised by checking both produce valid output.
	if !strings.Contains(coloredRow, "14:30:45") {
		t.Errorf("colored row should contain timestamp: %q", coloredRow)
	}
}

func TestBreakdownModel_DetectNewMessages_FirstLoad(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)

	// First load has nothing to diff against — flagging every row would flash
	// the whole table as "new"
	first := []models.BreakdownMessage{
		{Index: 1, Timestamp: time.Now()},
		{Index: 2, Timestamp: time.Now()},
		{Index: 3, Timestamp: time.Now()},
	}
	m.detectNewMessages(first)

	if len(m.newMsgKeys) != 0 {
		t.Errorf("first load flagged %d messages as new, want 0", len(m.newMsgKeys))
	}
}

// fadeReadyBreakdownModel returns a model with an initialized viewport holding
// sentinel content, so tests can observe whether a fade re-rendered the table.
func fadeReadyBreakdownModel() BreakdownModel {
	m := NewBreakdownModel("/test/path", "test-session", true, "", false)
	m.ready = true
	m.viewport = viewport.New(80, 10)
	m.messages = []models.BreakdownMessage{bdMsg(1, 0, ""), bdMsg(2, 1, "")}
	m.relayout()
	m.viewport.SetContent("SENTINEL")
	return m
}

// Nothing lit, nothing scheduled: the first load has nothing to diff
// against, so it lights no row.
func TestBreakdownFade_NothingToFade(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)
	updated, _ := m.Update(breakdownMsgsMsg{messages: []models.BreakdownMessage{bdMsg(1, 0, "")}})
	m = updated.(BreakdownModel)
	if m.fading || m.fadeCmd() != nil {
		t.Error("a fade was scheduled with no row lit")
	}
}

// A load that lights rows schedules one fade, for the earliest expiry. A
// second load while it's pending lights more rows but starts no second
// chain.
func TestBreakdownFade_OneAtEarliestExpiry(t *testing.T) {
	m := NewBreakdownModel("/test/path", "test-session", false, "", false)
	updated, _ := m.Update(breakdownMsgsMsg{messages: []models.BreakdownMessage{bdMsg(1, 0, "")}})
	m = updated.(BreakdownModel)

	updated, _ = m.Update(breakdownMsgsMsg{messages: []models.BreakdownMessage{bdMsg(1, 0, ""), bdMsg(2, 1, "")}})
	m = updated.(BreakdownModel)
	if !m.fading {
		t.Fatal("a new row lit, but no fade was scheduled")
	}
	if d := m.nextFade(); d <= highlightDuration-time.Second || d > highlightDuration {
		t.Errorf("fade due in %v, want just under %v", d, highlightDuration)
	}

	updated, _ = m.Update(breakdownMsgsMsg{messages: []models.BreakdownMessage{bdMsg(1, 0, ""), bdMsg(2, 1, ""), bdMsg(3, 2, "")}})
	m = updated.(BreakdownModel)
	if len(m.newMsgKeys) != 2 {
		t.Fatalf("%d rows lit, want 2", len(m.newMsgKeys))
	}
	if m.fadeCmd() != nil {
		t.Error("a second fade chain started while one was pending")
	}
}

// A fade redraws once for the highlights that expired and re-arms for the
// ones still lit; the one after the last leaves nothing scheduled.
func TestBreakdownFade_ClearsAndReArms(t *testing.T) {
	m := fadeReadyBreakdownModel()
	m.fading = true
	m.newMsgKeys[breakdownMsgKey(m.messages[0])] = time.Now().Add(-highlightDuration)
	m.newMsgKeys[breakdownMsgKey(m.messages[1])] = time.Now().Add(-highlightDuration / 2)

	updated, cmd := m.Update(fadeMsg{})
	m = updated.(BreakdownModel)
	if strings.Contains(m.viewport.View(), "SENTINEL") {
		t.Error("the fade that expired a highlight didn't redraw the table")
	}
	if len(m.newMsgKeys) != 1 {
		t.Errorf("%d highlights left, want 1", len(m.newMsgKeys))
	}
	if cmd == nil || !m.fading {
		t.Fatal("no fade scheduled for the row still lit")
	}
	if d := m.nextFade(); d <= 0 || d > highlightDuration/2 {
		t.Errorf("next fade in %v, want within %v", d, highlightDuration/2)
	}

	m.viewport.SetContent("SENTINEL")
	m.newMsgKeys[breakdownMsgKey(m.messages[1])] = time.Now().Add(-highlightDuration)
	updated, cmd = m.Update(fadeMsg{})
	m = updated.(BreakdownModel)
	if strings.Contains(m.viewport.View(), "SENTINEL") {
		t.Error("the fade that expired the last highlight didn't redraw the table")
	}
	if cmd != nil || m.fading || len(m.newMsgKeys) != 0 {
		t.Errorf("after the last highlight: cmd %v, fading %v, %d lit", cmd != nil, m.fading, len(m.newMsgKeys))
	}
}

// A fade with nothing expired, as after a session switch dropped the
// highlights it was scheduled for, leaves the table alone.
func TestBreakdownFade_NothingExpiredNoRedraw(t *testing.T) {
	m := fadeReadyBreakdownModel()
	m.fading = true
	updated, cmd := m.Update(fadeMsg{})
	m = updated.(BreakdownModel)
	if !strings.Contains(m.viewport.View(), "SENTINEL") {
		t.Error("a fade with nothing to expire redrew the table")
	}
	if cmd != nil || m.fading {
		t.Error("a fade with nothing lit re-armed")
	}
}

func TestBreakdownViewportDoesNotWrapRowsOnNarrowTerminal(t *testing.T) {
	// Table rows are ~74 columns; on a narrower terminal the viewport
	// soft-wraps overlong lines into extra rows, shifting the whole table.
	// Content must be clipped before it reaches the viewport.
	m := NewBreakdownModel("/test/path", "test-session", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m = updated.(BreakdownModel)

	msgs := []models.BreakdownMessage{
		{Index: 1, Timestamp: time.Now(), Model: "claude-opus-4-8",
			Usage: models.TokenUsage{InputTokens: 3500, OutputTokens: 717, CacheCreationInputTokens: 6000, CacheReadInputTokens: 15500}},
		{Index: 2, Timestamp: time.Now(), Model: "claude-opus-4-8",
			Usage: models.TokenUsage{InputTokens: 2, OutputTokens: 421, CacheCreationInputTokens: 144800, CacheReadInputTokens: 21500}},
	}
	updated, _ = m.Update(breakdownMsgsMsg{messages: msgs})
	m = updated.(BreakdownModel)

	if got := m.viewport.TotalLineCount(); got != len(msgs) {
		t.Errorf("viewport holds %d lines for %d messages — overlong rows were wrapped, not clipped", got, len(msgs))
	}
}
