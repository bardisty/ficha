package tui

import (
	"testing"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// keyMsg is the message Bubble Tea delivers for one key press. Tests name
// the key the way the key list and msg.String() do, and the message is built
// only here, so its shape is one function's concern and not every test's.
func keyMsg(t *testing.T, name string) tea.Msg {
	t.Helper()
	var k tea.Key
	switch name {
	case "esc":
		k = tea.Key{Code: tea.KeyEscape}
	case "down":
		k = tea.Key{Code: tea.KeyDown}
	case "left":
		k = tea.Key{Code: tea.KeyLeft}
	case "right":
		k = tea.Key{Code: tea.KeyRight}
	case "space":
		k = tea.Key{Code: tea.KeySpace, Text: " "}
	case "ctrl+z", "ctrl+d":
		k = tea.Key{Code: rune(name[len(name)-1]), Mod: tea.ModCtrl}
	default:
		if utf8.RuneCountInString(name) != 1 {
			t.Fatalf("keyMsg(%q): want a named key or a single rune", name)
		}
		r, _ := utf8.DecodeRuneInString(name)
		k = tea.Key{Code: r, Text: name}
		// A capital arrives as its lower-case key with shift held.
		if unicode.IsUpper(r) {
			k.Code, k.ShiftedCode, k.Mod = unicode.ToLower(r), r, tea.ModShift
		}
	}
	msg := tea.KeyPressMsg(k)
	// The views match on msg.String(), so the name has to survive the trip.
	if got := msg.String(); got != name {
		t.Fatalf("keyMsg(%q) built a key that reads %q", name, got)
	}
	return msg
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
	return m.View().Content
}
