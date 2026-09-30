package cmd

import (
	"encoding/csv"
	"path/filepath"
	"strings"
	"testing"
)

// A project with nothing inside --since stays out of global's records and
// counts, but a damaged session in it still reaches the warning, and -v names
// it under its project, as summary does.
func TestGlobalSinceWarnsAboutAProjectWithNothingInside(t *testing.T) {
	root := setupE2EFixture(t)
	alpha := filepath.Join(root, "projects", e2eProjDir, e2eAlphaID+".jsonl")
	appendBrokenLine(t, alpha)
	// Only the other project has a message on or after Feb 3.
	since := []string{"--since", "2026-02-03"}

	warning := "Warning: 1 unparseable line(s) skipped; totals may be undercounted\n"
	listing := e2eProjDir + "/" + e2eAlphaID[:8] + ": 1 line\n    session " + e2eAlphaID[:8] +
		": skipped line 3 (malformed)\n      " + alpha + "\n"
	for _, format := range []string{"table", "json", "csv"} {
		t.Run(format, func(t *testing.T) {
			_, stderr, err := executeCLISplit(t, append([]string{"global", "-f", format}, since...)...)
			if err != nil {
				t.Fatal(err)
			}
			mustContainAll(t, stderr, warning, "Run with -v to list the affected sessions.")

			_, stderr, err = executeCLISplit(t, append([]string{"global", "-v", "-f", format}, since...)...)
			if err != nil {
				t.Fatal(err)
			}
			mustContainAll(t, stderr, warning, listing)
		})
	}

	stdout, _, err := executeCLISplit(t, append([]string{"global", "-f", "json"}, since...)...)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ProjectCount int `json:"project_count"`
		SessionCount int `json:"session_count"`
		SkippedLines int `json:"skipped_lines"`
		Projects     []struct {
			EncodedPath  string `json:"encoded_path"`
			SkippedLines int    `json:"skipped_lines"`
		} `json:"projects"`
	}
	mustJSON(t, stdout, &got)
	if got.ProjectCount != 1 || got.SessionCount != 1 || got.SkippedLines != 1 ||
		len(got.Projects) != 1 || got.Projects[0].EncodedPath != e2eOtherDir || got.Projects[0].SkippedLines != 0 {
		t.Errorf("json: got %+v; want the other project alone, 1 session, and the damaged line counted at the top", got)
	}

	stdout, _, err = executeCLISplit(t, append([]string{"global", "-f", "csv"}, since...)...)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(stdout)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || strings.Contains(stdout, e2eProjDir) {
		t.Errorf("csv: want a header and the other project's row alone:\n%s", stdout)
	}
}
