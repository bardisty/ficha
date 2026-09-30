package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

func TestDescribeSkippedLines(t *testing.T) {
	m, o := models.SkipMalformed, models.SkipOversized
	tests := []struct {
		name string
		f    models.FileSkips
		want string
	}{
		{"one line", models.FileSkips{Count: 1, Lines: []models.SkippedLine{{Line: 7, Reason: m}}},
			"skipped line 7 (malformed)"},
		{"runs of one reason are labeled once", models.FileSkips{Count: 4, Lines: []models.SkippedLine{
			{Line: 1203, Reason: m}, {Line: 1207, Reason: m}, {Line: 1500, Reason: o}, {Line: 1600, Reason: m}}},
			"skipped lines 1203, 1207 (malformed), 1500 (oversized), 1600 (malformed)"},
		{"past the parser's cap, a count", models.FileSkips{Count: 130, Lines: []models.SkippedLine{
			{Line: 1, Reason: m}, {Line: 2, Reason: m}}},
			"skipped lines 1, 2 (malformed), and 128 more"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeSkippedLines(tt.f); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSkipWarningListsFilesOnlyUnderVerbose(t *testing.T) {
	files := []models.FileSkips{
		{Path: "/p/s.jsonl", Count: 1, Lines: []models.SkippedLine{{Line: 3, Reason: models.SkipMalformed}}},
		{Path: "/p/s/subagents/agent-a1.jsonl", AgentID: "a1", Count: 1, Lines: []models.SkippedLine{{Line: 9, Reason: models.SkipOversized}}},
	}
	w := skipWarning{counts: "totals", lines: 2, sessionID: "bdd640fb-0667-1ad1-1c80-317fa3b1799d", files: files}

	var quiet bytes.Buffer
	w.write(&quiet, false)
	if want := "Warning: 2 unparseable line(s) skipped; totals may be undercounted\n"; quiet.String() != want {
		t.Errorf("without -v:\n got %q\nwant %q", quiet.String(), want)
	}

	var loud bytes.Buffer
	w.write(&loud, true)
	want := "Warning: 2 unparseable line(s) skipped; totals may be undercounted\n" +
		"  session bdd640fb: skipped line 3 (malformed)\n" +
		"    /p/s.jsonl\n" +
		"  agent a1: skipped line 9 (oversized)\n" +
		"    /p/s/subagents/agent-a1.jsonl\n"
	if loud.String() != want {
		t.Errorf("with -v:\n got %q\nwant %q", loud.String(), want)
	}
}

// A report over many damaged sessions names at most maxListedFiles files.
func TestSkipWarningCapsListedFiles(t *testing.T) {
	var details []namedSkip
	for i := range maxListedFiles + 3 {
		id := fmt.Sprintf("%08d-0000-0000-0000-000000000000", i)
		details = append(details, namedSkip{label: id[:8], SkipDetail: models.SkipDetail{
			SessionID: id,
			Lines:     1,
			Files: []models.FileSkips{{Path: id + ".jsonl", Count: 1,
				Lines: []models.SkippedLine{{Line: 1, Reason: models.SkipMalformed}}}},
		}})
	}
	var out bytes.Buffer
	skipWarning{counts: "totals", lines: len(details), details: details}.write(&out, true)
	if n := strings.Count(out.String(), "skipped line 1"); n != maxListedFiles {
		t.Errorf("listed %d files, want %d:\n%s", n, maxListedFiles, out.String())
	}
	if !strings.HasSuffix(out.String(), "  and 3 more files with skipped lines\n") {
		t.Errorf("no count of the rest:\n%s", out.String())
	}
}

// End to end: a malformed line in the parent and one in an agent are named
// with their files under -v, and the run starts with the version line.
func TestE2EVerboseNamesSkippedLines(t *testing.T) {
	root := setupE2EFixture(t)
	proj := filepath.Join(root, "projects", e2eProjDir)
	parent := filepath.Join(proj, e2eBetaID+".jsonl")
	agent := filepath.Join(proj, e2eBetaID, "subagents", "agent-g1.jsonl")
	for _, path := range []string{parent, agent} {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("{broken\n"); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}

	_, stderr, err := executeCLISplit(t, "show", projFlag, e2eBetaID, "-v")
	if err != nil {
		t.Fatal(err)
	}
	if first, _, _ := strings.Cut(stderr, "\n"); first != "Debug: "+versionLine() {
		t.Errorf("first -v line %q, want the version line", first)
	}
	mustContainAll(t, stderr,
		"Debug: session file "+parent+"\n",
		"  session bbbbbbbb: skipped line 3 (malformed)\n    "+parent+"\n",
		"  agent g1: skipped line 2 (malformed)\n    "+agent+"\n",
	)

	_, quiet, err := executeCLISplit(t, "show", projFlag, e2eBetaID)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Warning: 2 unparseable line(s) skipped; totals may be undercounted\n"; quiet != want {
		t.Errorf("without -v:\n got %q\nwant %q", quiet, want)
	}
}

// When show passes over a newest session with no replies, -v names the file
// the report is actually about.
func TestVerboseNamesTheSessionShown(t *testing.T) {
	setupEmptyNewestSession(t)
	_, stderr, err := executeCLISplit(t, "show", projFlag, "-v", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	beta := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", e2eProjDir, e2eBetaID+".jsonl")
	if want := "Debug: newest session has no replies; showing session file " + beta + "\n"; !strings.Contains(stderr, want) {
		t.Errorf("stderr lacks %q:\n%s", want, stderr)
	}
}
