package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeProject creates a project dir under a temp CLAUDE_CONFIG_DIR with the
// given transcripts (name -> content), each mtime a minute newer than the
// last, so the final one listed is the newest.
func writeProject(t *testing.T, encoded string, files [][2]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "projects", encoded)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	for i, f := range files {
		p := filepath.Join(dir, f[0])
		if err := os.WriteFile(p, []byte(f[1]), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	return dir
}

func discoverOne(t *testing.T) (originalPath, displayName string) {
	t.Helper()
	projects, err := DiscoverAllProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("want 1 project, got %d", len(projects))
	}
	return projects[0].OriginalPath, projects[0].DisplayName
}

// No sessions-index.json, but the newest transcript carries cwd: that's the
// project's real path, and the display name is built from it.
func TestDiscoverRecoversPathFromCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	real := filepath.Join(home, "infra", "terraform")
	writeProject(t, "-x-infra-terraform", [][2]string{
		{"old.jsonl", `{"type":"user","cwd":"/somewhere/else"}` + "\n"},
		{"new.jsonl", `{"type":"summary","summary":"no cwd here"}` + "\n" +
			`{"type":"user","cwd":` + quote(real) + `,"message":{}}` + "\n"},
	})

	orig, name := discoverOne(t)
	if orig != real {
		t.Errorf("OriginalPath = %q, want %q (newest transcript's cwd)", orig, real)
	}
	// Lossless where the encoded name isn't: infra/terraform, not infra-terraform
	if name != "~/infra/terraform" {
		t.Errorf("DisplayName = %q, want ~/infra/terraform", name)
	}
}

// No index and no cwd anywhere: today's behavior, the encoded name.
func TestDiscoverWithoutCwdFallsBackToEncoded(t *testing.T) {
	writeProject(t, "-home-user-source-webapp", [][2]string{
		{"a.jsonl", `{"type":"summary","summary":"x"}` + "\n"},
		{"b.jsonl", ""},
	})
	orig, name := discoverOne(t)
	if orig != "" {
		t.Errorf("OriginalPath = %q, want empty", orig)
	}
	if name != formatDisplayNameFromEncoded("-home-user-source-webapp") {
		t.Errorf("DisplayName = %q, want the encoded fallback", name)
	}
}

// The newest transcript can be empty (a session opened and closed); the next
// newest still answers.
func TestDiscoverSkipsEmptyNewestTranscript(t *testing.T) {
	writeProject(t, "-srv-app", [][2]string{
		{"a.jsonl", `{"type":"user","cwd":"/srv/app"}` + "\n"},
		{"b.jsonl", ""},
	})
	if orig, _ := discoverOne(t); orig != "/srv/app" {
		t.Errorf("OriginalPath = %q, want /srv/app", orig)
	}
}

// A Windows transcript records a backslash cwd with a drive letter. It must
// come through intact, and name matching must split it on backslashes.
func TestDiscoverWindowsStyleCwd(t *testing.T) {
	cwd := `C:\Users\bob\source\billing-service`
	writeProject(t, "C--Users-bob-source-billing-service", [][2]string{
		{"a.jsonl", `{"type":"user","cwd":` + quote(cwd) + `}` + "\n"},
	})
	orig, name := discoverOne(t)
	if orig != cwd {
		t.Errorf("OriginalPath = %q, want %q", orig, cwd)
	}
	if !strings.HasSuffix(name, "/source/billing-service") {
		t.Errorf("DisplayName = %q, want a forward-slash path ending in /source/billing-service", name)
	}
}

// The index stays authoritative when it has an originalPath.
func TestDiscoverPrefersIndexOverCwd(t *testing.T) {
	dir := writeProject(t, "-a-b", [][2]string{
		{"a.jsonl", `{"type":"user","cwd":"/from/cwd"}` + "\n"},
	})
	if err := os.WriteFile(filepath.Join(dir, "sessions-index.json"),
		[]byte(`{"version":1,"originalPath":"/from/index","entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if orig, _ := discoverOne(t); orig != "/from/index" {
		t.Errorf("OriginalPath = %q, want the index's /from/index", orig)
	}
}

// A line over cwdLineBytes is skipped, not the end of the scan: a pasted
// screenshot early in a session must not hide the cwd on the next line.
func TestTranscriptCwdSkipsLongLines(t *testing.T) {
	huge := strings.Repeat("x", cwdLineBytes+1024)
	for name, content := range map[string]string{
		"long line without cwd, then cwd": `{"type":"user","message":{"content":"` + huge + `"}}` + "\n" +
			`{"type":"user","cwd":"/a/b"}` + "\n",
		"cwd at the far end of a long line, then cwd": `{"type":"user","message":{"content":"` + huge + `"},"cwd":"/a/b"}` + "\n" +
			`{"type":"assistant","cwd":"/a/b"}` + "\n",
		"final line without a newline": `{"type":"summary"}` + "\n" + `{"type":"user","cwd":"/a/b"}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeProject(t, "-a-b", [][2]string{{"a.jsonl", content}})
			if got := transcriptCwd(filepath.Join(dir, "a.jsonl")); got != "/a/b" {
				t.Errorf("transcriptCwd = %q, want /a/b", got)
			}
		})
	}
}

// The whole read is bounded: a cwd past the first cwdScanBytes is not found,
// so a huge transcript without one near the top costs a bounded read.
func TestTranscriptCwdIsBounded(t *testing.T) {
	filler := `{"type":"assistant","message":{"content":"` + strings.Repeat("x", 64*1024) + `"}}` + "\n"
	var sb strings.Builder
	for sb.Len() < cwdScanBytes+4096 {
		sb.WriteString(filler)
	}
	sb.WriteString(`{"type":"user","cwd":"/too/far"}` + "\n")
	dir := writeProject(t, "-big", [][2]string{{"a.jsonl", sb.String()}})
	if got := transcriptCwd(filepath.Join(dir, "a.jsonl")); got != "" {
		t.Errorf("transcriptCwd read past its bound, got %q", got)
	}
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}
