package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Pager keys reach the viewport's keymap, and j/k still move by one line.
func TestWatchPagerKeys(t *testing.T) {
	m := NewModel("/p/s.jsonl", "s", true, "", false)
	m = load(t, sized(t, m, 80, 24), tallAnalysis(1))
	page := m.viewport.Height

	m = press(t, m, "j")
	if m.viewport.YOffset != 1 {
		t.Fatalf("j: YOffset = %d, want 1", m.viewport.YOffset)
	}
	m = press(t, m, "k")
	m = press(t, m, "space")
	if m.viewport.YOffset != page {
		t.Errorf("space: YOffset = %d, want a page (%d)", m.viewport.YOffset, page)
	}
	m = press(t, m, "b")
	if m.viewport.YOffset != 0 {
		t.Errorf("b: YOffset = %d, want 0", m.viewport.YOffset)
	}
	m = press(t, m, "ctrl+d")
	if m.viewport.YOffset != page/2 {
		t.Errorf("ctrl+d: YOffset = %d, want half a page (%d)", m.viewport.YOffset, page/2)
	}

	// f toggles follow rather than paging down
	m = press(t, m, "g")
	m = press(t, m, "f")
	if m.viewport.YOffset != 0 || !m.followMode {
		t.Errorf("f: YOffset = %d follow = %v, want 0 and following", m.viewport.YOffset, m.followMode)
	}
}

// Repeated keys that arrive as one chunk each count.
func TestWatchRepeatedKeyChunk(t *testing.T) {
	m := NewModel("/p/s.jsonl", "s", true, "", false)
	m = load(t, sized(t, m, 80, 24), tallAnalysis(1))
	m = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjjj")})
	if m.viewport.YOffset != 4 {
		t.Errorf("jjjj: YOffset = %d, want 4", m.viewport.YOffset)
	}
}

// ctrl+z suspends only where the shell can resume it; without job control
// the stop would be discarded and the program would hang on a blank screen.
func TestWatchCtrlZSuspends(t *testing.T) {
	m := NewModel("/p/s.jsonl", "s", true, "", false)
	_, cmd := m.Update(keyMsg(t, "ctrl+z"))
	if !canSuspend() {
		if cmd != nil {
			t.Error("ctrl+z suspended without job control")
		}
	} else if cmd == nil {
		t.Fatal("ctrl+z returned no command")
	} else if _, ok := cmd().(tea.SuspendMsg); !ok {
		t.Error("ctrl+z didn't suspend")
	}
	updated, cmd := m.Update(tea.ResumeMsg{})
	if cmd == nil || !updated.(Model).loading {
		t.Error("resume didn't reload")
	}
}

// A paste is text, not keystrokes: "qq" pasted must not quit.
func TestWatchPasteIsNotRepeatedKeys(t *testing.T) {
	m := NewModel("/p/s.jsonl", "s", true, "", false)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("qq"), Paste: true})
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Error("pasting qq quit")
		}
	}
}
