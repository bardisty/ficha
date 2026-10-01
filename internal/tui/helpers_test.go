package tui

import (
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// keyMsg is the message Bubble Tea delivers for one key press. Tests name
// the key the way the key list and msg.String() do, and the message is built
// only here, so its shape is one function's concern and not every test's.
func keyMsg(t *testing.T, name string) tea.Msg {
	t.Helper()
	switch name {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	case "ctrl+z":
		return tea.KeyMsg{Type: tea.KeyCtrlZ}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	}
	if utf8.RuneCountInString(name) != 1 {
		t.Fatalf("keyMsg(%q): want a named key or a single rune", name)
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

// press sends each key to m in turn and returns the model it ends as. It
// hands back the type it was given, so a test holding a Model, a
// BreakdownModel or a plain tea.Model keeps what it had.
func press[M tea.Model](t *testing.T, m M, keys ...string) M {
	t.Helper()
	var model tea.Model = m
	for _, k := range keys {
		model, _ = model.Update(keyMsg(t, k))
	}
	return model.(M)
}

// frameOf is the screen m would draw. Tests read a frame only through it,
// so what View returns is unpacked in one place.
func frameOf(m tea.Model) string {
	return m.View()
}
