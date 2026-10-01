package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// pinSuspend fixes what canSuspend reports for one test, so the ? list's
// ctrl+z row doesn't depend on how the tests were started.
func pinSuspend(t *testing.T, can bool) {
	t.Helper()
	orig := canSuspend
	canSuspend = func() bool { return can }
	t.Cleanup(func() { canSuspend = orig })
}

func escKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEsc} }

// ? opens the list and ? or esc closes it, and neither touches anything else:
// the scroll position, follow mode and the switch notice stay as they were.
func TestWatchKeyListToggles(t *testing.T) {
	m := NewModel("/p/s.jsonl", "s", true, "", true)
	m = load(t, sized(t, m, 80, 24), tallAnalysis(1))
	m = key(t, m, "jjj")
	m.switched = &switchNotice{}

	m = key(t, m, "?")
	if !m.keysOpen {
		t.Fatal("? didn't open the key list")
	}
	m = key(t, m, "?")
	if m.keysOpen {
		t.Fatal("? didn't close the key list")
	}
	m = key(t, m, "?")
	updated, _ := m.Update(escKey())
	m = updated.(Model)
	if m.keysOpen {
		t.Fatal("esc didn't close the key list")
	}
	if m.viewport.YOffset != 3 || !m.followMode || m.switched == nil {
		t.Errorf("opening and closing the list changed the view: YOffset %d, follow %v, notice %v",
			m.viewport.YOffset, m.followMode, m.switched != nil)
	}
}

// Any other key closes the list and then does what it always does, so a key
// read off the list works the first time.
func TestWatchKeyListOtherKeysCloseAndAct(t *testing.T) {
	m := NewModel("/p/s.jsonl", "s", true, "", true)
	m = load(t, sized(t, m, 80, 24), tallAnalysis(1))

	m = key(t, key(t, m, "?"), "f")
	if m.keysOpen || m.followMode {
		t.Errorf("f with the list open: open %v, follow %v; want closed and pinned", m.keysOpen, m.followMode)
	}
	m = key(t, key(t, m, "?"), "j")
	if m.keysOpen || m.viewport.YOffset != 1 {
		t.Errorf("j with the list open: open %v, YOffset %d; want closed and 1", m.keysOpen, m.viewport.YOffset)
	}
	updated, cmd := key(t, m, "?").Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q with the list open didn't quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok || updated.(Model).keysOpen {
		t.Error("q with the list open didn't quit")
	}
}

// ctrl+z, a resume and a resize leave the list open, and live data keeps
// arriving under it.
func TestWatchKeyListSurvivesSuspendResizeAndLoads(t *testing.T) {
	m := NewModel("/p/s.jsonl", "s", true, "", true)
	m = key(t, load(t, sized(t, m, 80, 24), tallAnalysis(1)), "?")

	m, _ = keyType(t, m, tea.KeyCtrlZ)
	updated, _ := m.Update(tea.ResumeMsg{})
	m = sized(t, updated.(Model), 60, 20)
	m = load(t, m, tallAnalysis(2))
	if !m.keysOpen {
		t.Fatal("the list closed on ctrl+z, resume, resize or a load")
	}
	if m.analysis.MessageCount != tallAnalysis(2).MessageCount {
		t.Error("a load while the list was open didn't reach the model")
	}
	if lines := strings.Split(m.View(), "\n"); len(lines) != 20 {
		t.Errorf("after the resize the list's frame is %d rows, want 20", len(lines))
	}
}

// Keys that change breakdown's state close the list and act, as in watch.
// ? and esc touch neither the sort nor p's selection.
func TestBreakdownKeyListKeys(t *testing.T) {
	m := loadedBreakdown(t, 80, 24, chromeRows(goldenTime(10, 0, 0), 100))
	m = pressKeys(t, m, "p")
	selected := m.selectedKey
	m = pressKeys(t, m, "?")
	if !m.keysOpen {
		t.Fatal("? didn't open the key list")
	}
	updated, _ := m.Update(escKey())
	m = updated.(BreakdownModel)
	if m.keysOpen || m.selectedKey != selected || m.sortByCost {
		t.Errorf("esc: open %v, selection kept %v, sorted %v", m.keysOpen, m.selectedKey == selected, m.sortByCost)
	}

	m = pressKeys(t, m, "?", "s")
	if m.keysOpen || !m.sortByCost {
		t.Errorf("s with the list open: open %v, sorted %v; want closed and sorted", m.keysOpen, m.sortByCost)
	}
	m = pressKeys(t, m, "?", "f")
	if m.keysOpen || !m.followMode {
		t.Errorf("f with the list open: open %v, follow %v; want closed and following", m.keysOpen, m.followMode)
	}

	m = pressKeys(t, m, "?")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 60, Height: 15})
	if !updated.(BreakdownModel).keysOpen {
		t.Error("ctrl+z or a resize closed the list")
	}
}

// The list fills the frame above the help line at every size the views
// draw, never wider than the terminal. It fits whole at 60 columns and in
// the compact frame at 15 rows, and at 40 columns it drops alternate keys
// rather than cut a description. The too-small screen wins over it.
func TestKeyListFits(t *testing.T) {
	pinSuspend(t, true)
	tables := map[string][]Key{"watch": WatchKeys(), "breakdown": BreakdownKeys()}
	views := map[string]func(w, h int) tea.Model{
		"watch": func(w, h int) tea.Model {
			m := NewModel("/p/s.jsonl", "s", true, "", true)
			return key(t, load(t, sized(t, m, w, h), tallAnalysis(1)), "?")
		},
		"breakdown": func(w, h int) tea.Model {
			return pressKeys(t, loadedBreakdown(t, w, h, chromeRows(goldenTime(10, 0, 0), 50)), "?")
		},
	}
	for name, open := range views {
		for _, sz := range []struct{ w, h int }{{80, 24}, {60, 40}, {50, 24}, {40, 24}, {80, 15}, {60, 15}, {80, 9}} {
			view := open(sz.w, sz.h).View()
			lines := strings.Split(view, "\n")
			for i := range lines {
				lines[i] = strings.TrimRight(lines[i], " ")
			}
			view = strings.Join(lines, "\n")
			if len(lines) != sz.h {
				t.Errorf("%s %dx%d: frame is %d rows", name, sz.w, sz.h, len(lines))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > sz.w {
					t.Errorf("%s %dx%d: row %d is %d wide: %q", name, sz.w, sz.h, i, w, l)
				}
			}
			if help := strings.TrimSpace(lines[len(lines)-1]); !strings.HasSuffix(help, closeHint) {
				t.Errorf("%s %dx%d: help line %q doesn't end in %q", name, sz.w, sz.h, help, closeHint)
			}
			cut := strings.Contains(view, "more in ficha "+name+" --help")
			if whole := sz.h >= 15; whole == cut {
				t.Errorf("%s %dx%d: list cut = %v\n%s", name, sz.w, sz.h, cut, view)
			} else if whole {
				for _, k := range tables[name] {
					if !strings.Contains(view+"\n", "  "+k.Does+"\n") {
						t.Errorf("%s %dx%d: %q isn't listed whole\n%s", name, sz.w, sz.h, k.Does, view)
					}
				}
			}
		}
		if view := open(30, 5).View(); !strings.Contains(view, "terminal too small") {
			t.Errorf("%s: the list drew over the too-small screen:\n%s", name, view)
		}
	}
}

// ctrl+z is listed only where it suspends.
func TestKeyListSuspendRow(t *testing.T) {
	for _, can := range []bool{true, false} {
		pinSuspend(t, can)
		listed := strings.Contains(strings.Join(keyList("watch", WatchKeys(), 80, 40, true), "\n"), "ctrl+z")
		if listed != can {
			t.Errorf("canSuspend %v: ctrl+z listed = %v", can, listed)
		}
	}
}

func TestGoldenWatchKeyList(t *testing.T) {
	pinSuspend(t, true)
	m := NewModel("/fixture/sess.jsonl", "s", true, "", true)
	checkGolden(t, "watch_keys_80x24", key(t, sized(t, m, 80, 24), "?").View())
}

func TestGoldenBreakdownKeyListColor(t *testing.T) {
	forceProfile(t, termenv.ANSI256)
	pinSuspend(t, true)
	m := NewBreakdownModel("/fixture/sess.jsonl", "s", false, "", true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 15})
	checkGolden(t, "breakdown_keys_80x15_color", pressKeys(t, updated.(BreakdownModel), "?").View())
}
