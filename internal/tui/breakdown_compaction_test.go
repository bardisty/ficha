package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/styles"
)

// compactedRows is 40 main-conversation rows a minute apart from 10:00,
// the 21st the first reply after an automatic compaction at 180,000 tokens.
func compactedRows() []models.BreakdownMessage {
	rows := chromeRows(goldenTime(10, 0, 0), 40)
	rows[20].Compacted = &models.Compaction{Timestamp: rows[20].Timestamp.Add(-time.Second), Trigger: "auto", PreTokens: 180000}
	return rows
}

// The divider sits above the first reply after the compaction, and gives up
// the size, then the trigger, when the frame is narrow. Sorted, it goes.
func TestBreakdownCompactionDivider(t *testing.T) {
	m := datedBreakdown(t, 80, 60, goldenTime(12, 0, 0), compactedRows())
	lines := tableLinesOf(m)
	if lines[20] != "── compacted (auto, 180.0K) ──" || !strings.HasPrefix(lines[21], "21 ") {
		t.Errorf("lines 21-22: %q, %q", lines[20], lines[21])
	}
	if first, last := m.visibleRows(); first != 1 || last != 40 {
		t.Errorf("rows %d-%d on screen, want 1-40: the divider is a line, not a row", first, last)
	}
	for _, tc := range []struct {
		width int
		want  string
	}{
		{32, "── compacted (auto, 180.0K) ──"},
		{31, "── compacted (auto) ──"},
		{24, "── compacted (auto) ──"},
		{23, "── compacted ──"},
	} {
		m.width = tc.width
		if got := strings.TrimSpace(m.renderCompactionMarker(*m.messages[20].Compacted)); got != tc.want {
			t.Errorf("at %d columns: %q, want %q", tc.width, got, tc.want)
		}
	}
	m.width = 80
	if got := strings.TrimSpace(m.renderCompactionMarker(models.Compaction{})); got != "── compacted ──" {
		t.Errorf("with nothing recorded: %q", got)
	}
	if m = press(t, m, "s"); strings.Contains(tableContent(m), "compacted") {
		t.Error("the divider shows in cost order")
	}
}

// --ascii draws the divider with the ASCII rule, and a compaction on the
// first reply of a day sits under that day's divider.
func TestBreakdownCompactionUnderDayDivider(t *testing.T) {
	styles.SetASCII(true)
	t.Cleanup(func() { styles.SetASCII(false) })
	rows := compactedRows()
	rows[20].Timestamp = goldenTime(10, 0, 0).AddDate(0, 0, 1)
	for i := 21; i < len(rows); i++ {
		rows[i].Timestamp = rows[20].Timestamp.Add(time.Duration(i) * time.Minute)
	}
	m := datedBreakdown(t, 80, 60, goldenTime(12, 0, 0).AddDate(0, 0, 1), rows)
	lines := tableLinesOf(m)
	if lines[21] != "-- Fri 16 Jan --" || lines[22] != "-- compacted (auto, 180.0K) --" || !strings.HasPrefix(lines[23], "21 ") {
		t.Errorf("lines 22-24: %q", lines[21:24])
	}
	// With the compaction divider at the top, the rule still names the day.
	m = datedBreakdown(t, 80, 24, goldenTime(12, 0, 0).AddDate(0, 0, 1), rows)
	m.setFollow(false)
	m.scrollTo(22)
	if day := m.firstVisibleDay(); day != "Fri 16 Jan" {
		t.Errorf("rule day with the compaction divider on top: %q", day)
	}
}

// A line held by its row finds the same divider again: either of the two
// stacked above a row, as far up as it was.
func TestBreakdownCompactionLineMarks(t *testing.T) {
	rows := compactedRows()
	rows[20].Timestamp = goldenTime(10, 0, 0).AddDate(0, 0, 1)
	m := datedBreakdown(t, 80, 24, goldenTime(12, 0, 0).AddDate(0, 0, 1), rows)
	for _, line := range []int{21, 22, 23} {
		mark := m.markLine(m.lineRows, m.linePos, line)
		if got := m.lineOf(m.lineRows, m.linePos, mark); got != line {
			t.Errorf("line %d came back as %d", line, got)
		}
	}
	// Without the compaction, a mark on its divider lands on the day's.
	mark := m.markLine(m.lineRows, m.linePos, 22)
	plain := rows
	plain[20].Compacted = nil
	m = datedBreakdown(t, 80, 24, goldenTime(12, 0, 0).AddDate(0, 0, 1), plain)
	if got := m.lineOf(m.lineRows, m.linePos, mark); got != 21 {
		t.Errorf("the compaction's divider, gone, came back as line %d, want the day's, 21", got)
	}
}

// Scrolled up, a compacted reply that arrives counts as one new row, and p
// marks the row, not its divider.
func TestBreakdownCompactionCountsAsALine(t *testing.T) {
	rows := compactedRows()
	m := press(t, loadedBreakdown(t, 80, 24, rows[:20]), "g")
	updated, _ := m.Update(breakdownMsgsMsg{messages: rows[:21], insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if text := m.positionText(); !strings.Contains(text, "1 new") || !strings.Contains(text, "of 21") {
		t.Errorf("position %q, want 21 rows and 1 new", text)
	}
	m.messages[20].Cost.TotalCost = 1
	if m = press(t, m, "p"); m.lineRows[m.selectedLine()] != 21 {
		t.Errorf("p selected line %d, holding %d", m.selectedLine(), m.lineRows[m.selectedLine()])
	}
}

// The divider belongs to the main conversation's reply, so an agent's scope
// never shows it, and the main conversation's does.
func TestBreakdownCompactionInScope(t *testing.T) {
	m := scopedBreakdown(t, 100, 24, 30)
	m.messages[3].Compacted = &models.Compaction{Trigger: "manual"}
	m.refreshViewport()
	if m = press(t, m, "a"); !strings.Contains(tableContent(m), "compacted (manual)") {
		t.Error("the main conversation's scope lacks the divider")
	}
	if m = press(t, m, "a"); strings.Contains(tableContent(m), "compacted") {
		t.Error("an agent's scope shows the main conversation's divider")
	}
}

// Sorted, where no divider shows, p says a reply is the first after a
// compaction in place of its cost split; in time order the divider says it.
func TestBreakdownCompactionSelectionNote(t *testing.T) {
	rows := compactedRows()
	rows[20].Cost = models.CostBreakdown{TotalCost: 1, OutputCost: 1}
	m := press(t, loadedBreakdown(t, 100, 24, rows), "p")
	if text := m.selectionText(); strings.Contains(text, "compaction") || !strings.Contains(text, "Output $1.00") {
		t.Errorf("time order: %q", text)
	}
	if m = press(t, loadedBreakdown(t, 100, 24, rows), "s", "p"); m.selectionText() != "top 1 of 5: #21 $1.00 • first reply after compaction • p next" {
		t.Errorf("cost order: %q", m.selectionText())
	}
}
