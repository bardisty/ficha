package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/tui"
)

// readmeKeysTable is the README's Keys table as the key tables have it:
// breakdown's rows in order, the ones watch lacks marked as breakdown's. A
// key watch has with its own meaning follows breakdown's row for it, marked
// as watch's.
func readmeKeysTable() string {
	inWatch := map[tui.Key]bool{}
	watchOnly := map[string][]tui.Key{}
	for _, k := range tui.WatchKeys() {
		inWatch[k] = true
		if !inBreakdown(k) {
			watchOnly[k.Keys] = append(watchOnly[k.Keys], k)
		}
	}
	lines := []string{"| Key | Does |", "| --- | --- |"}
	row := func(k tui.Key, view string) string {
		keys := "`" + strings.Join(strings.Split(k.Keys, ", "), "`, `") + "`"
		does := k.Does
		if k.Note != "" {
			does += "; " + k.Note
		}
		if view != "" {
			does = view + ": " + does
		}
		return "| " + keys + " | " + does + " |"
	}
	for _, k := range tui.BreakdownKeys() {
		view := ""
		if !inWatch[k] {
			view = "breakdown"
		}
		lines = append(lines, row(k, view))
		for _, w := range watchOnly[k.Keys] {
			lines = append(lines, row(w, "watch"))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// inBreakdown reports whether breakdown has watch's row k as it is.
func inBreakdown(k tui.Key) bool {
	for _, b := range tui.BreakdownKeys() {
		if b == k {
			return true
		}
	}
	return false
}

// The README's Keys table must match the views' key tables, which also
// build the help line, the ? list and each view's --help. A row only watch
// has must share its key with one of breakdown's, which places it.
func TestREADMEKeysTable(t *testing.T) {
	for _, k := range tui.WatchKeys() {
		placed := inBreakdown(k)
		for _, b := range tui.BreakdownKeys() {
			placed = placed || b.Keys == k.Keys
		}
		if !placed {
			t.Fatalf("watch's %q row has no breakdown row to follow; the README table has no place for it", k.Keys)
		}
	}
	data, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	readme := strings.ReplaceAll(string(data), "\r\n", "\n")
	if want := readmeKeysTable(); !strings.Contains(readme, want) {
		t.Errorf("README.md's Keys table doesn't match the key tables. Want:\n\n%s", want)
	}
}
