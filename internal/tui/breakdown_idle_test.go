package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bardisty/ficha/internal/models"
)

// Breakdown's header ages the newest message the way watch's does, on the
// same clock: 1s while the age counts seconds, 15s after, and a message
// after an idle stretch starts a fresh 1s chain that the slow one yields to.
func TestBreakdownClockChain(t *testing.T) {
	now := goldenTime(12, 0, 0)
	m := loadedBreakdown(t, 100, 24, chromeRows(now.Add(-30*time.Minute), 3))
	if m.clockInterval() != 15*time.Second {
		t.Fatalf("idle interval = %v, want 15s", m.clockInterval())
	}
	oldGen := m.clockGen

	updated, _ := m.Update(breakdownMsgsMsg{messages: chromeRows(now.Add(-2*time.Minute), 3), insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if m.clockInterval() != time.Second {
		t.Fatalf("busy interval = %v, want 1s", m.clockInterval())
	}
	if m.clockGen == oldGen {
		t.Fatal("a fresh message didn't start a faster clock chain")
	}
	if _, cmd := m.Update(clockMsg{gen: oldGen}); cmd != nil {
		t.Error("the superseded slow chain kept ticking")
	}
	if _, cmd := m.Update(clockMsg{gen: m.clockGen}); cmd == nil {
		t.Error("the live chain stopped")
	}

	// Already on the 1s chain, another message starts no second one.
	gen := m.clockGen
	updated, _ = m.Update(breakdownMsgsMsg{messages: chromeRows(now.Add(-2*time.Minute), 4), insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	if m.clockGen != gen {
		t.Error("a message on the fast clock started another chain")
	}
}

// The clock is what moves the header's age between loads.
func TestBreakdownClockAgesHeader(t *testing.T) {
	clock := goldenTime(12, 0, 0)
	m := loadedBreakdown(t, 100, 24, chromeRows(clock.Add(-2*time.Minute-10*time.Second), 3))
	m.now = func() time.Time { return clock }
	if !strings.Contains(frameOf(m), "last msg 10s ago") {
		t.Fatalf("header before the tick:\n%s", frameOf(m))
	}
	clock = clock.Add(time.Second)
	updated, _ := m.Update(clockMsg{gen: m.clockGen})
	if v := frameOf(updated.(BreakdownModel)); !strings.Contains(v, "last msg 11s ago") {
		t.Errorf("header after the tick:\n%s", v)
	}
}

// "loading..." and its spinner show only until a session's first load
// lands, as in watch: a reload keeps the last status up, and a switch to
// another session shows it again.
func TestBreakdownSpinnerStopsWhenLoaded(t *testing.T) {
	m := NewBreakdownModel("/nonexistent/s.jsonl", "s", false, "", false)
	if _, cmd := m.Update(spinner.TickMsg{}); cmd == nil {
		t.Fatal("spinner didn't tick during the first load")
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	updated, _ = updated.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(10, 0, 0), 2)})
	m = updated.(BreakdownModel)
	if _, cmd := m.Update(spinner.TickMsg{}); cmd != nil {
		t.Fatal("spinner kept ticking with nothing loading")
	}

	isTick := func(msg tea.Msg) bool { _, ok := msg.(spinner.TickMsg); return ok }
	for _, tc := range []struct {
		name    string
		msg     tea.Msg
		loading bool
	}{
		{"r", keyMsg(t, "r"), false},
		{"resume", tea.ResumeMsg{}, false},
		{"write", fileChangedMsg{}, false},
		{"switch", sessionActivityMsg{path: "/nonexistent/t.jsonl", id: "t", created: true}, true},
	} {
		m := m
		m.followMode = true
		updated, cmd := m.Update(tc.msg)
		got := updated.(BreakdownModel)
		if !got.loading {
			t.Fatalf("%s: no load started", tc.name)
		}
		if batchHas(cmd, isTick) != tc.loading {
			t.Errorf("%s: spinner restarted = %v, want %v", tc.name, !tc.loading, tc.loading)
		}
		if strings.Contains(frameOf(got), "loading...") != tc.loading {
			t.Errorf("%s: header shows loading... = %v, want %v:\n%s", tc.name, !tc.loading, tc.loading, frameOf(got))
		}
	}
}

// A session with no rows yet has loaded all the same; its reloads keep
// "no messages yet" up too.
func TestBreakdownEmptySessionReloadKeepsStatus(t *testing.T) {
	m := NewBreakdownModel("/nonexistent/s.jsonl", "s", true, "", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	updated, _ = updated.Update(breakdownMsgsMsg{})
	updated = press(t, updated, "r")
	// A resize redraws the table area while the reload is in flight.
	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	v := frameOf(updated)
	if strings.Contains(v, "loading...") {
		t.Errorf("reload of an empty session shows loading...:\n%s", v)
	}
	if !strings.Contains(v, emptyStateText) {
		t.Errorf("reload of an empty session blanked the empty state:\n%s", v)
	}
}

// The layout is worked out once per change of the rows, the run tags or
// the width, and each of those changes brings it up to date: a stale one
// misaligns the columns.
func TestBreakdownLayoutCacheFollowsInputs(t *testing.T) {
	check := func(step string, m BreakdownModel) {
		t.Helper()
		if m.table != m.layout() {
			t.Errorf("%s: cached layout %+v, want %+v", step, m.table, m.layout())
		}
		// The COST heading ends where the costs under it do.
		lines := strings.Split(frameOf(m), "\n")
		var header, row string
		for i, l := range lines {
			if strings.Contains(l, "COST") && i+2 < len(lines) {
				header, row = l, lines[i+2]
				if !strings.Contains(row, "$") {
					row = lines[i+1]
				}
				break
			}
		}
		if header == "" || !strings.Contains(row, "$") {
			return
		}
		if got, want := strings.Index(row, "$")+len("$0.0500"), strings.Index(header, "COST")+len("COST"); got != want {
			t.Errorf("%s: COST ends at %d, the cost at %d:\n%s\n%s", step, want, got, header, row)
		}
	}

	msgs, runs := runTagRows(12)
	m := loadedBreakdown(t, 120, 30, nil)
	check("empty", m)
	updated, _ := m.Update(breakdownMsgsMsg{messages: msgs, workflows: runs, insights: &models.MessageInsights{}})
	m = updated.(BreakdownModel)
	check("load", m)
	if !m.table.runTags || m.table.agentWidth == 0 {
		t.Fatalf("load: layout has no tagged AGENT column: %+v", m.table)
	}

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m = updated.(BreakdownModel)
	check("resize to 60", m)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(BreakdownModel)
	check("resize to 120", m)

	// A session without agents, and with ten times the rows: no AGENT
	// column, and a wider # column.
	m.followMode = true
	updated, _ = m.Update(sessionActivityMsg{path: "/fixture/other.jsonl", id: "other", created: true})
	m = updated.(BreakdownModel)
	check("switch", m)
	updated, _ = m.Update(breakdownMsgsMsg{messages: chromeRows(goldenTime(10, 0, 0), 120), insights: &models.MessageInsights{}, sessionPath: "/fixture/other.jsonl"})
	m = updated.(BreakdownModel)
	check("switch load", m)
	if m.table.agentWidth != 0 || m.table.indexWidth != 3 {
		t.Errorf("switch load: layout %+v, want no AGENT and a 3-wide #", m.table)
	}
}
