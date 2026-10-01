package tui

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Pager keys reach the viewport's keymap, and j/k still move by one line.
func TestWatchPagerKeys(t *testing.T) {
	m := NewModel("/p/s.jsonl", "s", true, "", false)
	m = load(t, sized(t, m, 80, 24), tallAnalysis(1))
	page := m.viewport.Height()

	m = press(t, m, "j")
	if m.viewport.YOffset() != 1 {
		t.Fatalf("j: YOffset = %d, want 1", m.viewport.YOffset())
	}
	m = press(t, m, "k")
	m = press(t, m, "space")
	if m.viewport.YOffset() != page {
		t.Errorf("space: YOffset = %d, want a page (%d)", m.viewport.YOffset(), page)
	}
	// A terminal that reports modifiers sends shift+space as its own key.
	m = press(t, m, "b", "shift+space")
	if m.viewport.YOffset() != page {
		t.Errorf("shift+space: YOffset = %d, want a page (%d)", m.viewport.YOffset(), page)
	}
	m = press(t, m, "b")
	if m.viewport.YOffset() != 0 {
		t.Errorf("b: YOffset = %d, want 0", m.viewport.YOffset())
	}
	m = press(t, m, "ctrl+d")
	if m.viewport.YOffset() != page/2 {
		t.Errorf("ctrl+d: YOffset = %d, want half a page (%d)", m.viewport.YOffset(), page/2)
	}

	// f toggles follow rather than paging down
	m = press(t, m, "g")
	m = press(t, m, "f")
	if m.viewport.YOffset() != 0 || !m.followMode {
		t.Errorf("f: YOffset = %d follow = %v, want 0 and following", m.viewport.YOffset(), m.followMode)
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

// A paste is text, not keystrokes: "qq" pasted must not quit, in either
// view.
func TestPasteIsNotKeys(t *testing.T) {
	for name, m := range map[string]tea.Model{
		"watch":     NewModel("/p/s.jsonl", "s", true, "", false),
		"breakdown": NewBreakdownModel("/p/s.jsonl", "s", true, "", false),
	} {
		_, cmd := m.Update(tea.PasteMsg{Content: "qq"})
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Errorf("%s: pasting qq quit", name)
			}
		}
	}
}

// The viewport binds h, l and the left and right arrows to sideways
// scrolling. A view's body is clipped to the terminal, so there is nothing
// to scroll to, and a stray one of these must not shift the frame.
func TestSidewaysKeysMoveNothing(t *testing.T) {
	watch := NewModel("/p/s.jsonl", "s", true, "", false)
	watch = load(t, sized(t, watch, 40, 24), tallAnalysis(1))
	breakdown := loadedBreakdown(t, 40, 24, chromeRows(goldenTime(11, 0, 0), 40))

	for name, m := range map[string]tea.Model{"watch": watch, "breakdown": breakdown} {
		before := frameOf(m)
		for _, k := range []string{"l", "right", "l", "h", "left"} {
			m = press(t, m, k)
			if got := frameOf(m); got != before {
				t.Fatalf("%s: %s changed the frame:\n%s\nwas:\n%s", name, k, got, before)
			}
		}
	}
}

// keyRecorder is a model that writes down what each key press reads as.
type keyRecorder struct{ keys *[]string }

func (keyRecorder) Init() tea.Cmd { return nil }

func (r keyRecorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		*r.keys = append(*r.keys, k.String())
		if k.String() == "q" {
			return r, tea.Quit
		}
	}
	return r, nil
}

func (keyRecorder) View() tea.View { return tea.NewView("") }

// The views match keys by what msg.String() returns, and the other tests
// build their key presses by hand. This one feeds a terminal's bytes through
// Bubble Tea's own decoder, so the names the views match are the names a
// real key arrives with: as plain bytes, and in the forms a terminal uses
// once Bubble Tea has asked it to report modified keys apart. A held key
// arrives as one press per character.
func TestKeyNamesFromTerminalInput(t *testing.T) {
	input := []struct{ bytes, want string }{
		{"j", "j"}, {"j", "j"}, {"j", "j"},
		{"G", "G"}, {"?", "?"}, {"-", "-"},
		{" ", "space"}, {"\x1b[27;2;32~", "shift+space"}, {"\x1b[32;2u", "shift+space"},
		{"\x1b[27;5;99~", "ctrl+c"}, {"\x1b[99;5u", "ctrl+c"}, {"\x1b[27u", "esc"},
		{"\x03", "ctrl+c"}, {"\x1a", "ctrl+z"}, {"\x04", "ctrl+d"}, {"\x15", "ctrl+u"},
		{"\x1b[A", "up"}, {"\x1b[B", "down"}, {"\x1b[C", "right"}, {"\x1b[D", "left"},
		{"\x1b[H", "home"}, {"\x1b[F", "end"},
		{"\x1b[5~", "pgup"}, {"\x1b[6~", "pgdown"},
		{"q", "q"},
	}
	var stream strings.Builder
	var want []string
	for _, in := range input {
		stream.WriteString(in.bytes)
		want = append(want, in.want)
	}

	var got []string
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := tea.NewProgram(keyRecorder{&got}, tea.WithContext(ctx),
		tea.WithInput(strings.NewReader(stream.String())), tea.WithOutput(io.Discard), tea.WithoutSignals())
	if _, err := p.Run(); err != nil {
		t.Fatalf("the program ended with %v after reading %q", err, got)
	}
	if !slices.Equal(got, want) {
		t.Errorf("keys read as\n  %q\nwant\n  %q", got, want)
	}
}

// Even handed a line wider than itself, the views' viewport doesn't scroll
// sideways: the footer has no way to say a frame is shifted.
func TestViewportHasNoSidewaysScroll(t *testing.T) {
	vp := newViewport(10, 2)
	vp.SetContent(strings.Repeat("x", 50))
	for _, k := range []string{"l", "right"} {
		vp, _ = vp.Update(keyMsg(t, k))
		if got := vp.XOffset(); got != 0 {
			t.Errorf("%s scrolled the viewport to column %d", k, got)
		}
	}
}
