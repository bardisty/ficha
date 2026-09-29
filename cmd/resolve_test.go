package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/paths"
)

// realDir makes a directory under a temp dir and returns its canonical path,
// the form ficha reports (macOS's temp dir sits behind a /var symlink).
func realDir(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(append([]string{t.TempDir()}, parts...)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

// addProject creates the Claude project directory for workDir, with one
// session unless empty is set.
func addProject(t *testing.T, configDir, workDir string, empty bool) string {
	t.Helper()
	dir := filepath.Join(configDir, "projects", paths.PathToProjectDir(workDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !empty {
		line := e2eMsg("2026-02-01T10:00:00Z", "r1", "claude-opus-4-8", 100, 50, 0, 0, 0) + "\n"
		if err := os.WriteFile(filepath.Join(dir, "dddddddd-0000-0000-0000-000000000000.jsonl"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestE2EResolutionErrors: each reason ficha can't find sessions gets its own
// message and next step.
func TestE2EResolutionErrors(t *testing.T) {
	t.Run("missing projects dir from CLAUDE_CONFIG_DIR", func(t *testing.T) {
		config := filepath.Join(t.TempDir(), "nope")
		t.Setenv("CLAUDE_CONFIG_DIR", config)
		want := "no Claude Code data in " + filepath.Join(config, "projects") +
			" (from CLAUDE_CONFIG_DIR). Check that CLAUDE_CONFIG_DIR points at Claude Code's config directory."
		for _, args := range [][]string{{"show"}, {"list", "--project-dir=-x"}, {"global"}} {
			if _, _, err := executeCLISplit(t, args...); err == nil || err.Error() != want {
				t.Errorf("%v:\n got: %v\nwant: %s", args, err, want)
			}
		}
	})

	t.Run("empty projects dir under the default config dir", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		projects := filepath.Join(home, ".claude", "projects")
		if err := os.MkdirAll(projects, 0o755); err != nil {
			t.Fatal(err)
		}
		want := "no Claude Code data in " + projects +
			" (default ~/.claude). Start Claude Code in a project, then run ficha from the same directory."
		if _, _, err := executeCLISplit(t, "summary", "-p", home); err == nil || err.Error() != want {
			t.Errorf("\n got: %v\nwant: %s", err, want)
		}
	})

	config := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	work := realDir(t, "work")
	webapp := filepath.Join(work, "webapp")
	if err := os.MkdirAll(filepath.Join(webapp, "src", "components"), 0o755); err != nil {
		t.Fatal(err)
	}
	addProject(t, config, webapp, false)
	stale := filepath.Join(work, "stale")
	if err := os.MkdirAll(filepath.Join(stale, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	addProject(t, config, stale, true)
	file := filepath.Join(work, "notes.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	projectsDir := filepath.Join(config, "projects")
	elsewhere := realDir(t, "elsewhere")

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"-p path that doesn't exist", []string{"show", "-p", filepath.Join(work, "gone")},
			"no such directory: " + filepath.Join(work, "gone")},
		{"-p path that is a file", []string{"show", "-p", file}, "not a directory: " + file},
		{"directory Claude Code never ran in", []string{"list", "-p", elsewhere},
			"Claude Code has no sessions for " + elsewhere + ". Sessions are recorded per directory Claude Code was started in.\n" +
				"Run 'ficha global' to see every project."},
		{"subdirectory of a project", []string{"watch", "-p", filepath.Join(webapp, "src", "components")},
			"Claude Code has no sessions for " + filepath.Join(webapp, "src", "components") + ". Sessions are recorded per directory Claude Code was started in.\n" +
				"Found sessions for parent directory " + webapp + ". Run: ficha watch -p " + shellQuote(webapp) + "\n" +
				"Run 'ficha global' to see every project."},
		{"project with no transcripts", []string{"show", "-p", stale}, "Claude Code has no sessions for " + stale + " yet"},
		// stale's project has no transcripts, so the hint skips it for the
		// next parent up (none here, since work has no project).
		{"subdirectory of a project with no transcripts", []string{"show", "-p", filepath.Join(stale, "sub")},
			"Claude Code has no sessions for " + filepath.Join(stale, "sub") + ". Sessions are recorded per directory Claude Code was started in.\n" +
				"Run 'ficha global' to see every project."},
		{"real directory passed to --project-dir", []string{"show", "--project-dir", webapp},
			webapp + " has no Claude Code sessions. --project-dir takes a directory under " + projectsDir +
				". For sessions started in " + webapp + ", run: ficha show -p " + shellQuote(webapp)},
		{"unknown --project-dir name", []string{"list", "--project-dir=-nope"},
			"no project directory -nope in " + projectsDir + ". Run 'ficha global' to see every project."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := executeCLISplit(t, tt.args...)
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("\n got: %v\nwant: %s", err, tt.wantErr)
			}
		})
	}

	// Many people have run Claude Code in ~ itself, so a project there is
	// never offered as the parent of an unmatched directory.
	t.Run("home directory is not offered as the parent", func(t *testing.T) {
		t.Setenv("HOME", work)
		t.Setenv("USERPROFILE", work)
		addProject(t, config, work, false)
		if err := os.Mkdir(filepath.Join(work, "plain"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, _, err := executeCLISplit(t, "show", "-p", filepath.Join(work, "plain"))
		if err == nil || strings.Contains(err.Error(), "parent directory") {
			t.Errorf("home should not be suggested as a parent: %v", err)
		}
	})
}

// TestE2EAmbiguousProjectName: when several projects share a directory's
// name, the error lists a command per project rather than encoded names.
func TestE2EAmbiguousProjectName(t *testing.T) {
	home := setupCwdFixture(t)
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	second := filepath.Join(home, "old", "webapp")
	dir := filepath.Join(root, "projects", "-x-old-webapp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := strings.Replace(e2eMsg("2026-02-01T10:00:00Z", "o1", "claude-opus-4-8", 100, 200, 0, 0, 0),
		`{"type":"assistant",`, `{"type":"assistant","cwd":`+jsonString(second)+`,`, 1)
	if err := os.WriteFile(filepath.Join(dir, "aaaaaaaa-0000-0000-0000-000000000009.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	missing := filepath.Join(t.TempDir(), "webapp")
	_, _, err := executeCLISplit(t, "show", "-p", missing)
	if err == nil {
		t.Fatal("want an ambiguous-project error")
	}
	canonical, _ := paths.CanonicalizePath(missing)
	first := filepath.Join(home, "work", "webapp")
	for _, want := range []string{
		"Claude Code has no sessions for " + canonical + ", and 2 projects share its name \"webapp\". Pick one:",
		"\n  ficha show " + projectArgsFor(first, "-x-work-webapp"),
		"\n  ficha show " + projectArgsFor(second, "-x-old-webapp"),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "Use --project-dir") {
		t.Errorf("error should give commands, not --project-dir advice:\n%v", err)
	}
}

func projectArgsFor(original, encoded string) string {
	return projectArgs(models.ProjectInfo{OriginalPath: original, EncodedPath: encoded})
}

// TestE2ESessionLookup: session IDs match regardless of case or a pasted
// .jsonl, a miss says which other project has the session, and an ambiguous
// prefix lists what `list` would say about each candidate.
func TestE2ESessionLookup(t *testing.T) {
	root := setupE2EFixture(t)
	proj := filepath.Join(root, "projects", e2eProjDir)
	extraID := "aaaabbbb-0000-0000-0000-000000000000"
	line := e2eMsg("2026-02-01T11:00:00Z", "x1", "claude-opus-4-8", 100, 50, 0, 0, 0) + "\n"
	if err := os.WriteFile(filepath.Join(proj, extraID+".jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	// An agent, so the count must include agent messages as `list` does.
	agent := filepath.Join(proj, extraID, "subagents", "agent-x1.jsonl")
	if err := os.MkdirAll(filepath.Dir(agent), 0o755); err != nil {
		t.Fatal(err)
	}
	agentLines := e2eMsg("2026-02-01T11:01:00Z", "x2", "claude-sonnet-5", 100, 50, 0, 0, 0) + "\n" +
		e2eMsg("2026-02-01T11:02:00Z", "x3", "claude-sonnet-5", 100, 50, 0, 0, 0) + "\n"
	if err := os.WriteFile(agent, []byte(agentLines), 0o644); err != nil {
		t.Fatal(err)
	}
	older, newer := time.Now().Add(-3*time.Hour), time.Now().Add(-5*time.Minute)
	if err := os.Chtimes(filepath.Join(proj, e2eAlphaID+".jsonl"), older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(proj, extraID+".jsonl"), newer, newer); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{strings.ToUpper(e2eAlphaID[:8]), e2eAlphaID + ".jsonl", strings.ToUpper(e2eAlphaID) + ".JSONL"} {
		out, _, err := executeCLISplit(t, "show", projFlag, id, "-f", "json")
		if err != nil {
			t.Errorf("show %s: %v", id, err)
			continue
		}
		if !strings.Contains(out, `"session_id": "`+e2eAlphaID+`"`) {
			t.Errorf("show %s resolved to the wrong session:\n%s", id, out)
		}
	}

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"session in another project", []string{"show", projFlag, "CCCC"},
			"session cccccccc is in " + e2eOtherDir + ". Run: ficha show cccccccc --project-dir=" + e2eOtherDir},
		{"session elsewhere, from a directory with no project", []string{"watch", "-p", t.TempDir(), "cccc"},
			"session cccccccc is in " + e2eOtherDir + ". Run: ficha watch cccccccc --project-dir=" + e2eOtherDir},
		{"session nowhere", []string{"show", projFlag, "ffff"},
			"session not found: ffff. Run 'ficha list " + projFlag + "' to see this project's sessions."},
		{"ambiguous prefix, newest first", []string{"show", projFlag, "aaaa"},
			`ambiguous session ID "aaaa" matches 2 sessions:` +
				"\n  " + extraID + "  modified 5m ago · 3 msgs" +
				"\n  " + e2eAlphaID + "  modified 3h ago · 2 msgs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := executeCLISplit(t, tt.args...)
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("\n got: %v\nwant: %s", err, tt.wantErr)
			}
		})
	}
}

// TestE2EVerboseTrace: -v says where ficha looked and what it found.
func TestE2EVerboseTrace(t *testing.T) {
	config := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	webapp := realDir(t, "webapp")
	projectDir := addProject(t, config, webapp, false)
	encoded := filepath.Base(projectDir)

	_, stderr, err := executeCLISplit(t, "list", "-p", webapp, "-v")
	if err != nil {
		t.Fatal(err)
	}
	want := "Debug: projects dir " + filepath.Join(config, "projects") + " (from CLAUDE_CONFIG_DIR)\n" +
		"Debug: project path " + webapp + " (from -p)\n" +
		"Debug: found project directory " + encoded + "\n" +
		"Debug: using " + encoded + " (1 sessions)\n"
	if stderr != want {
		t.Errorf("stderr:\n got: %q\nwant: %q", stderr, want)
	}

	sub := filepath.Join(webapp, "src")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	_, stderr, _ = executeCLISplit(t, "list", "-p", sub, "-v")
	for _, want := range []string{
		"Debug: no project directory " + paths.PathToProjectDir(sub) + "\n",
		"Debug: no project among 1 has the name \"src\"\n",
		"Debug: parent directory " + webapp + " has a project\n",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}
