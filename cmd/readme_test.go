package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// TestREADMEDocumentsCLISurface fails when the README drifts from the actual
// CLI surface: every registered app command and every visible flag must be
// mentioned. cobra's built-in help/completion commands are not app commands and
// are only registered during Execute, so they never appear here.
func TestREADMEDocumentsCLISurface(t *testing.T) {
	data, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	readme := string(data)

	root := newRootCmd()
	builtin := map[string]bool{"help": true, "completion": true}

	t.Run("commands", func(t *testing.T) {
		for _, c := range root.Commands() {
			name := c.Name()
			if builtin[name] || c.Hidden {
				continue
			}
			if !strings.Contains(readme, name) {
				t.Errorf("README.md does not mention command %q", name)
			}
		}
	})

	t.Run("persistent flags", func(t *testing.T) {
		root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
			if !strings.Contains(readme, "--"+f.Name) {
				t.Errorf("README.md does not mention persistent flag --%s", f.Name)
			}
		})
	})

	t.Run("command flags", func(t *testing.T) {
		for _, c := range append(root.Commands(), root) {
			if builtin[c.Name()] || c.Hidden {
				continue
			}
			c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
				if !f.Hidden && !strings.Contains(readme, "--"+f.Name) {
					t.Errorf("README.md does not mention %s flag --%s", c.Name(), f.Name)
				}
			})
		}
	})
}
