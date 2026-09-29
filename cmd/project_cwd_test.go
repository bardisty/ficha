package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupCwdFixture builds a projects tree the way current Claude Code writes
// it: no sessions-index.json, the real path only in each line's cwd. The
// real paths sit under a fake home so display names shorten to "~".
func setupCwdFixture(t *testing.T) (home string) {
	t.Helper()
	root := t.TempDir()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	write := func(encoded, id, cwd string) {
		t.Helper()
		dir := filepath.Join(root, "projects", encoded)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		line := strings.Replace(e2eMsg("2026-02-01T10:00:00Z", id, "claude-opus-4-8", 100, 200, 0, 0, 0),
			`{"type":"assistant",`, `{"type":"assistant","cwd":`+jsonString(cwd)+`,`, 1)
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Two projects whose encoded names differ only where the real paths
	// differ in '/' versus '-', which the encoded name can't tell apart
	write("-x-infra-terraform", "aaaaaaaa-0000-0000-0000-000000000001", filepath.Join(home, "infra", "terraform"))
	write("-x-infra-terraform2", "aaaaaaaa-0000-0000-0000-000000000002", filepath.Join(home, "infra-terraform2"))
	write("-x-work-webapp", "aaaaaaaa-0000-0000-0000-000000000003", filepath.Join(home, "work", "webapp"))

	t.Setenv("CLAUDE_CONFIG_DIR", root)
	return home
}

func jsonString(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}

// global names come from the recovered path, shortened to "~".
func TestGlobalNamesFromTranscriptCwd(t *testing.T) {
	setupCwdFixture(t)
	out, _, err := executeCLISplit(t, "global", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"~/infra/terraform", "~/infra-terraform2", "~/work/webapp"} {
		if !strings.Contains(out, want) {
			t.Errorf("global should show %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "x-work-webapp") {
		t.Errorf("global fell back to the encoded name:\n%s", out)
	}
}

// A path that doesn't match exactly falls back to the recovered path's name.
func TestProjectNameFallbackUsesTranscriptCwd(t *testing.T) {
	home := setupCwdFixture(t)
	elsewhere := filepath.Join(t.TempDir(), "webapp")
	out, stderr, err := executeCLISplit(t, "show", "-p", elsewhere, "--no-color")
	if err != nil {
		t.Fatalf("name fallback should find the webapp project: %v", err)
	}
	if !strings.Contains(stderr, "matched by name 'webapp'") ||
		!strings.Contains(stderr, filepath.Join(home, "work", "webapp")) {
		t.Errorf("stderr should note the name match and the real path, got:\n%s", stderr)
	}
	if !strings.Contains(out, "TOTAL") {
		t.Errorf("show should render the matched project:\n%s", out)
	}
}

// With no exact or name match, the suggestion is a -p command for the real
// path, not an encoded --project-dir name.
func TestSimilarProjectsSuggestsDashP(t *testing.T) {
	home := setupCwdFixture(t)
	elsewhere := filepath.Join(t.TempDir(), "web")
	_, _, err := executeCLISplit(t, "show", "-p", elsewhere)
	if err == nil {
		t.Fatal("want a no-project error")
	}
	want := "ficha show -p " + shellQuote(filepath.Join(home, "work", "webapp"))
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error should suggest %q:\n%v", want, err)
	}
	if strings.Contains(err.Error(), "--project-dir") {
		t.Errorf("error should not ask for --project-dir:\n%v", err)
	}
}

// Per-session warning details name a project by its recovered path.
func TestVerboseWarningsNameProjectByCwd(t *testing.T) {
	home := setupCwdFixture(t)
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	p := filepath.Join(root, "projects", "-x-work-webapp", "aaaaaaaa-0000-0000-0000-000000000003.jsonl")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not json\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, stderr, err := executeCLISplit(t, "global", "-v")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "work", "webapp") + "/aaaaaaaa: 1 line"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr should name the project by its cwd path %q:\n%s", want, stderr)
	}
}
