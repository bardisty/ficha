package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/tui"
)

// readmeKeysTable is the README's Keys table as the key tables have it:
// breakdown's rows in order, the ones watch lacks marked as breakdown's.
func readmeKeysTable() string {
	inWatch := map[string]bool{}
	for _, k := range tui.WatchKeys() {
		inWatch[k.Keys] = true
	}
	lines := []string{"| Key | Does |", "| --- | --- |"}
	for _, k := range tui.BreakdownKeys() {
		keys := "`" + strings.Join(strings.Split(k.Keys, ", "), "`, `") + "`"
		does := k.Does
		if k.Note != "" {
			does += "; " + k.Note
		}
		if !inWatch[k.Keys] {
			does = "breakdown: " + does
		}
		lines = append(lines, "| "+keys+" | "+does+" |")
	}
	return strings.Join(lines, "\n") + "\n"
}

// The README's Keys table must match the views' key tables, which also
// build the help line, the ? list and each view's --help.
func TestREADMEKeysTable(t *testing.T) {
	for _, k := range tui.WatchKeys() {
		found := false
		for _, b := range tui.BreakdownKeys() {
			found = found || b == k
		}
		if !found {
			t.Fatalf("watch's %q row isn't breakdown's; the README table has no place for it", k.Keys)
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
