package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// `ficha help environment` names the four variables it documents, and root
// help lists it as a topic.
func TestEnvironmentHelpTopic(t *testing.T) {
	out, _, err := executeCLISplit(t, "help", "environment")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CLAUDE_CONFIG_DIR", "NO_COLOR", "CLICOLOR_FORCE", "COLORFGBG"} {
		if !strings.Contains(out, name) {
			t.Errorf("help environment missing %s:\n%s", name, out)
		}
	}
	root, _, err := executeCLISplit(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(root, "Environment variables ficha reads") {
		t.Errorf("root help should list the environment topic:\n%s", root)
	}
}

// global --help reads the same on every machine: it names the default and
// the override, never the directory CLAUDE_CONFIG_DIR resolves to.
func TestGlobalHelpNamesNoLocalPath(t *testing.T) {
	config := filepath.Join(t.TempDir(), "ficha-help-config")
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	out, _, err := executeCLISplit(t, "global", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ficha-help-config") {
		t.Errorf("global --help printed the resolved CLAUDE_CONFIG_DIR:\n%s", out)
	}
	if !strings.Contains(out, "~/.claude/projects") || !strings.Contains(out, "$CLAUDE_CONFIG_DIR/projects") {
		t.Errorf("global --help should name ~/.claude/projects and $CLAUDE_CONFIG_DIR/projects:\n%s", out)
	}
}
