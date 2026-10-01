package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// e2eIndexPath is where the fixture project projDir keeps its
// sessions-index.json.
func e2eIndexPath(root, projDir string) string {
	return filepath.Join(root, "projects", projDir, "sessions-index.json")
}

func writeIndex(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// global says what summary says about a sessions-index.json it can't use, in
// every format, and prints the same report it would without the file.
func TestGlobalWarnsAboutAnUnusableSessionsIndex(t *testing.T) {
	tests := []struct {
		name   string
		damage func(t *testing.T, index string)
		// reason is the text after the path. Empty where the OS words it.
		reason string
	}{
		{
			name:   "malformed json",
			damage: func(t *testing.T, index string) { writeIndex(t, index, "{broken") },
			reason: "invalid character 'b' looking for beginning of object key string",
		},
		{
			name: "over the size cap",
			damage: func(t *testing.T, index string) {
				writeIndex(t, index, "")
				if err := os.Truncate(index, 10*1024*1024+1); err != nil {
					t.Fatal(err)
				}
			},
			reason: "too large (10485761 bytes, limit 10485760)",
		},
		{
			// A directory where the index should be can't be read as a file,
			// on every OS and for every user, root included
			name: "file error",
			damage: func(t *testing.T, index string) {
				if err := os.Mkdir(index, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := setupE2EFixture(t)
			index := e2eIndexPath(root, e2eProjDir)
			clean := map[string]string{}
			for _, format := range []string{"table", "json", "csv"} {
				stdout, stderr, err := executeCLISplit(t, "global", "-f", format)
				if err != nil {
					t.Fatal(err)
				}
				if stderr != "" {
					t.Fatalf("%s without an index: want nothing on stderr, got %q", format, stderr)
				}
				clean[format] = stdout
			}
			tt.damage(t, index)

			_, want, err := executeCLISplit(t, "summary", projFlag, "-f", "json")
			if err != nil {
				t.Fatal(err)
			}
			prefix := "Warning: ignoring " + index + ": "
			if !strings.HasPrefix(want, prefix) || strings.Count(want, "\n") != 1 {
				t.Fatalf("summary: want one line starting %q, got %q", prefix, want)
			}
			if tt.reason != "" && want != prefix+tt.reason+"\n" {
				t.Errorf("summary:\n got: %q\nwant: %q", want, prefix+tt.reason+"\n")
			}
			for _, format := range []string{"table", "json", "csv"} {
				stdout, stderr, err := executeCLISplit(t, "global", "-f", format)
				if err != nil {
					t.Fatal(err)
				}
				if stderr != want {
					t.Errorf("%s:\n got: %q\nwant: %q", format, stderr, want)
				}
				if n := strings.Count(stderr, "sessions-index.json"); n != 1 {
					t.Errorf("%s: the warning names the file %d times, want once: %q", format, n, stderr)
				}
				if stdout != clean[format] {
					t.Errorf("%s: the report changed:\n got: %q\nwant: %q", format, stdout, clean[format])
				}
			}
		})
	}
}

// On a terminal the warning wraps like the other three commands' does: the
// path whole on a line of its own, the reason hung under the warning's text.
func TestGlobalSessionsIndexWarningWraps(t *testing.T) {
	root := setupE2EFixture(t)
	index := e2eIndexPath(root, e2eProjDir)
	writeIndex(t, index, "{")
	withStderrWidth(t, 40)
	_, stderr, err := executeCLISplit(t, "global", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	want := "Warning: ignoring\n" + index + ":\n         unexpected end of JSON input\n"
	if stderr != want {
		t.Errorf("at 40 columns:\n got: %q\nwant: %q", stderr, want)
	}
}

// Current Claude Code writes no sessions-index.json, so a warning for a
// missing one would fire for every project.
func TestGlobalMissingSessionsIndexIsSilent(t *testing.T) {
	root := setupE2EFixture(t)
	for _, dir := range []string{e2eProjDir, e2eOtherDir} {
		if _, err := os.Stat(e2eIndexPath(root, dir)); !os.IsNotExist(err) {
			t.Fatalf("the fixture has an index in %s: %v", dir, err)
		}
	}
	for _, args := range [][]string{
		{"global", "-f", "json"},
		{"global", "--since", "2026-02-03", "-f", "json"},
	} {
		_, stderr, err := executeCLISplit(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		if stderr != "" {
			t.Errorf("%v: want nothing on stderr, got %q", args, stderr)
		}
	}
}

// Projects are read in parallel, and the warnings still come out in the
// order the projects are listed in, the same on every run.
func TestGlobalSessionsIndexWarningsKeepProjectOrder(t *testing.T) {
	root := setupE2EFixture(t)
	other, proj := e2eIndexPath(root, e2eOtherDir), e2eIndexPath(root, e2eProjDir)
	writeIndex(t, other, "{")
	writeIndex(t, proj, "[")
	// Before the skip warning, which is about what was read after discovery
	appendBrokenLine(t, filepath.Join(root, "projects", e2eProjDir, e2eAlphaID+".jsonl"))
	want := "Warning: ignoring " + other + ": unexpected end of JSON input\n" +
		"Warning: ignoring " + proj + ": unexpected end of JSON input\n" +
		"Warning: 1 unparseable line(s) skipped; totals may be undercounted\n" +
		"  Run with -v to list the affected sessions.\n"
	for run := range 20 {
		_, stderr, err := executeCLISplit(t, "global", "-f", "json")
		if err != nil {
			t.Fatal(err)
		}
		if stderr != want {
			t.Fatalf("run %d:\n got: %q\nwant: %q", run, stderr, want)
		}
	}
}

// summary warns before any window applies, and before it finds a project has
// no transcripts. global warns about those projects too, though neither
// reaches its records.
func TestGlobalWarnsAboutTheIndexOfAProjectItLeavesOut(t *testing.T) {
	root := setupE2EFixture(t)
	windowed := e2eIndexPath(root, e2eProjDir)
	writeIndex(t, windowed, "{")
	// Only the other project has a message on or after Feb 3.
	empty := e2eIndexPath(root, "-home-test-empty")
	writeIndex(t, empty, "{")
	want := "Warning: ignoring " + empty + ": unexpected end of JSON input\n" +
		"Warning: ignoring " + windowed + ": unexpected end of JSON input\n"

	for _, format := range []string{"table", "json", "csv"} {
		_, stderr, err := executeCLISplit(t, "global", "--since", "2026-02-03", "-f", format)
		if err != nil {
			t.Fatal(err)
		}
		if stderr != want {
			t.Errorf("%s:\n got: %q\nwant: %q", format, stderr, want)
		}
	}
	stdout, _, err := executeCLISplit(t, "global", "--since", "2026-02-03", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Projects []struct {
			EncodedPath string `json:"encoded_path"`
		} `json:"projects"`
	}
	mustJSON(t, stdout, &got)
	if len(got.Projects) != 1 || got.Projects[0].EncodedPath != e2eOtherDir {
		t.Errorf("json: got %+v, want the other project alone", got.Projects)
	}
}

// One cause can damage every project's index. The first ten are named and the
// rest counted, so the warnings don't push the report off the screen, and -v
// names them all.
func TestGlobalSessionsIndexWarningsStopAtTen(t *testing.T) {
	root := setupE2EFixture(t)
	var all []string
	for i := range 12 {
		index := e2eIndexPath(root, fmt.Sprintf("-home-test-extra-%02d", i))
		writeIndex(t, index, "{")
		all = append(all, "Warning: ignoring "+index+": unexpected end of JSON input\n")
	}

	_, stderr, err := executeCLISplit(t, "global", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join(all[:10], "") + "  ignoring 2 more sessions-index.json files. Run with -v to list them.\n"
	if stderr != want {
		t.Errorf("without -v:\n got: %q\nwant: %q", stderr, want)
	}

	_, stderr, err = executeCLISplit(t, "global", "-v", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(stderr, strings.Join(all, "")) || strings.Contains(stderr, "more sessions-index.json") {
		t.Errorf("-v: want all 12 named and no count, got:\n%s", stderr)
	}

	// Ten is the most that fit without the count line
	for _, extra := range []string{"-home-test-extra-10", "-home-test-extra-11"} {
		if err := os.RemoveAll(filepath.Join(root, "projects", extra)); err != nil {
			t.Fatal(err)
		}
	}
	_, stderr, err = executeCLISplit(t, "global", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Join(all[:10], ""); stderr != want {
		t.Errorf("ten indexes:\n got: %q\nwant: %q", stderr, want)
	}
}
