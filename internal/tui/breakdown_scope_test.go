package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/bardisty/ficha/internal/models"
)

const (
	scopeAgentB = "b2222222222222222"
	scopeAgentC = "c3333333333333333"
)

// scopeRows is n rows a minute apart, every third the main conversation's:
// rows 1, 4, 7… are main, 2, 5, 8… agent C's (which so appears first) and
// 3, 6, 9… agent B's. Each costs a cent more than the last, so the newest
// row of each source is its costliest.
func scopeRows(n int) []models.BreakdownMessage {
	msgs := chromeRows(goldenTime(10, 0, 0), n)
	for i := range msgs {
		msgs[i].AgentID = []string{"", scopeAgentC, scopeAgentB}[i%3]
		msgs[i].Cost.TotalCost = float64(i+1) / 100
	}
	return msgs
}

// scopedBreakdown is breakdown at width x height on scopeRows(n), with the
// session's own insights from the analyzer, as a load has them.
func scopedBreakdown(t *testing.T, width, height, n int) BreakdownModel {
	t.Helper()
	m := NewBreakdownModel("/fixture/sess.jsonl", "0a1b2c3d", true, "", false)
	m.now = func() time.Time { return goldenTime(12, 0, 0) }
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	updated, _ = updated.Update(scopeLoad(n))
	return updated.(BreakdownModel)
}

func scopeLoad(n int) breakdownMsgsMsg {
	rows := scopeRows(n)
	var total float64
	for _, r := range rows {
		total += r.Cost.TotalCost
	}
	return breakdownMsgsMsg{messages: rows, totalCost: total, hasAgents: true, insights: &models.MessageInsights{}}
}

// rowNumbers is the row numbers on the table's lines, dividers left out.
func rowNumbers(m BreakdownModel) []int {
	var out []int
	for _, index := range m.lineRows {
		if index > 0 {
			out = append(out, index)
		}
	}
	return out
}

// a steps every row, the main conversation, then each agent in the order
// its first row appears, then every row again. A session with no agent rows
// ignores it.
func TestBreakdownScopeStepOrder(t *testing.T) {
	m := scopedBreakdown(t, 100, 24, 30)
	want := []rowScope{{on: true}, {on: true, agent: scopeAgentC}, {on: true, agent: scopeAgentB}, {}}
	for i, w := range want {
		if m = press(t, m, "a"); m.scope != w {
			t.Fatalf("press %d: scope %+v, want %+v", i+1, m.scope, w)
		}
	}
	plain := loadedBreakdown(t, 100, 24, chromeRows(goldenTime(10, 0, 0), 30))
	if plain = press(t, plain, "a"); plain.scope.on {
		t.Error("a scoped a session with no agents")
	}
}

// Scoped to an agent, the table holds its rows under the session's own row
// numbers, the footer rule names it and counts its rows, and the stats line
// says how much of the session is on show. Peak is the agent's costliest
// row, by its session number.
func TestBreakdownScopeShowsOneAgent(t *testing.T) {
	m := press(t, scopedBreakdown(t, 100, 24, 60), "a", "a")
	got := rowNumbers(m)
	if len(got) != 20 || got[0] != 2 || got[1] != 5 || got[19] != 59 {
		t.Fatalf("agent C's rows: %v", got)
	}
	if rule := m.renderFooterRule(m.panelWidth()); !strings.Contains(rule, "[ Ac333333 • rows 7-20 of 20 • a next ]") {
		t.Errorf("footer rule %q", rule)
	}
	if stats := m.renderStatsTotals(); !strings.Contains(stats, "Messages: 20 of 60 │ Total: $6.10 of $18.30") {
		t.Errorf("stats line %q", stats)
	}
	if line := m.renderCompactInsights(); !strings.Contains(line, "[Ac333333] │ Peak: #59 $0.5900") {
		t.Errorf("insights line %q", line)
	}
	main := press(t, m, "a", "a", "a")
	if !main.scope.on || main.scope.agent != "" {
		t.Fatalf("setup: scope %+v", main.scope)
	}
	if line := main.renderCompactInsights(); !strings.Contains(line, "main conversation │ Peak: #58 $0.5800") {
		t.Errorf("main conversation's insights line %q", line)
	}
}

// p and s work inside the scope: p's top five are the scope's, sorted ranks
// count the scope's rows, and esc clears the selection before the scope.
func TestBreakdownScopePeakSortAndEsc(t *testing.T) {
	m := press(t, scopedBreakdown(t, 100, 24, 60), "a", "a", "p")
	for _, pos := range m.topByCost() {
		if m.messages[pos].AgentID != scopeAgentC {
			t.Fatalf("p's top rows include row %d, outside the scope", m.messages[pos].Index)
		}
	}
	if m.selectedRank(m.topByCost()) != 0 {
		t.Fatalf("p should select the scope's costliest")
	}
	m = press(t, m, "s")
	if rule := m.renderFooterRule(m.panelWidth()); !strings.Contains(rule, "rank 1-14 of 20") {
		t.Errorf("sorted rule %q", rule)
	}
	if m = press(t, m, "esc"); m.selectedKey != "" || !m.scope.on {
		t.Fatalf("first esc: selection %q scope %+v, want the selection gone and the scope kept", m.selectedKey, m.scope)
	}
	if m = press(t, m, "esc"); m.scope.on {
		t.Errorf("second esc kept the scope %+v", m.scope)
	}

	// Scoped while sorted, s goes back to the new scope's first row at or
	// after the row it would have gone back to.
	m = scopedBreakdown(t, 100, 24, 150)
	scrollToRow(t, &m, 32) // agent C's
	m = press(t, m, "s", "a")
	if m.scope != (rowScope{on: true}) {
		t.Fatalf("setup: scope %+v, want the main conversation", m.scope)
	}
	if m = press(t, m, "s"); firstRow(m) != 34 {
		t.Errorf("s after scoping while sorted: first row %d, want 34, the main conversation's next", firstRow(m))
	}
}

// scrollToRow stops following and scrolls the row with display Index index
// to the top.
func scrollToRow(t *testing.T, m *BreakdownModel, index int) {
	t.Helper()
	m.setFollow(false)
	for line, row := range m.lineRows {
		if row == index {
			m.scrollTo(line)
			return
		}
	}
	t.Fatalf("setup: row %d isn't in the table", index)
}

// firstRow is the display Index of the first row on screen.
func firstRow(m BreakdownModel) int {
	first, _ := m.visibleRows()
	return first
}

// Following inside a scope, rows from elsewhere neither move the view nor
// count as new; the scope's own do. A reload keeps the scope, and a switch
// to another session goes back to every row.
func TestBreakdownScopeThroughReloads(t *testing.T) {
	m := press(t, scopedBreakdown(t, 100, 24, 60), "a", "a", "k")
	if m.autoScroll {
		t.Fatal("setup: k should stop following")
	}
	updated, _ := m.Update(scopeLoad(61)) // row 61 is the main conversation's
	m = updated.(BreakdownModel)
	if !m.scope.on || m.scope.agent != scopeAgentC {
		t.Fatalf("reload lost the scope: %+v", m.scope)
	}
	if strings.Contains(m.positionText(), "new") {
		t.Errorf("a row outside the scope counted as new: %q", m.positionText())
	}
	updated, _ = m.Update(scopeLoad(62)) // row 62 is agent C's
	if m = updated.(BreakdownModel); !strings.Contains(m.positionText(), "1 new") {
		t.Errorf("the scope's own new row didn't count: %q", m.positionText())
	}
	m.followMode = true
	updated, _ = m.Update(sessionActivityMsg{path: "/fixture/new.jsonl", id: "new", created: true})
	if m = updated.(BreakdownModel); m.scope.on {
		t.Errorf("a switch kept the scope %+v", m.scope)
	}
}

// A session with no main conversation rows steps from every row straight to
// its agents, and a reload that leaves the scope no rows goes back to every
// row with none of them counted new.
func TestBreakdownScopeWithNoRows(t *testing.T) {
	load := scopeLoad(30)
	for i := range load.messages {
		if load.messages[i].AgentID == "" {
			load.messages[i].AgentID = scopeAgentB
		}
	}
	m := scopedBreakdown(t, 100, 24, 30)
	updated, _ := m.Update(load)
	if m = press(t, updated.(BreakdownModel), "a"); m.scope != (rowScope{on: true, agent: scopeAgentB}) {
		t.Errorf("first a with no main rows: scope %+v, want agent B, whose row is first", m.scope)
	}

	m = press(t, scopedBreakdown(t, 100, 24, 30), "a", "k")
	updated, _ = m.Update(load)
	if m = updated.(BreakdownModel); m.scope.on || strings.Contains(m.positionText(), "new") {
		t.Errorf("reload emptied the main scope: scope %+v, position %q", m.scope, m.positionText())
	}

	// Every row comes back at the row number the reader was on, and so
	// does the line s goes back to.
	gone := scopeLoad(90)
	for i := range gone.messages {
		if gone.messages[i].AgentID == scopeAgentC {
			gone.messages[i].AgentID = scopeAgentB
		}
	}
	m = press(t, scopedBreakdown(t, 100, 24, 90), "a", "a")
	scrollToRow(t, &m, 50) // agent C's
	updated, _ = m.Update(gone)
	if m = updated.(BreakdownModel); m.scope.on || firstRow(m) != 50 {
		t.Errorf("reload emptied agent C's scope: scope %+v, first row %d, want every row from 50", m.scope, firstRow(m))
	}
	m = press(t, scopedBreakdown(t, 100, 24, 90), "a", "a")
	scrollToRow(t, &m, 50)
	m = press(t, m, "s", "j")
	updated, _ = m.Update(gone)
	if m = updated.(BreakdownModel); m.viewport.YOffset() != 0 {
		t.Errorf("reload emptied agent C's sorted scope: view at line %d, want the costliest row at the top", m.viewport.YOffset())
	}
	if m = press(t, m, "s"); m.scope.on || firstRow(m) != 50 {
		t.Errorf("reload emptied agent C's sorted scope, then s: scope %+v, first row %d, want every row from 50", m.scope, firstRow(m))
	}
}
