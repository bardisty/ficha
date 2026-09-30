package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// A damaged session with nothing inside --since still counts in the warning,
// so -v names it and its file, in summary and in global.
func TestVerboseSinceNamesSessionsOutsideTheWindow(t *testing.T) {
	root := setupE2EFixture(t)
	alpha := filepath.Join(root, "projects", e2eProjDir, e2eAlphaID+".jsonl")
	appendBrokenLine(t, alpha)

	file := ": 1 line\n    session " + e2eAlphaID[:8] + ": skipped line 3 (malformed)\n      " + alpha + "\n"
	for _, args := range [][]string{
		{"summary", projFlag, "--since", "2026-02-02", "-v"},
		{"summary", projFlag, "-v"},
		{"global", "--since", "2026-02-02", "-v"},
		{"global", "-v"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, stderr, err := executeCLISplit(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			mustContainAll(t, stderr,
				"Warning: 1 unparseable line(s) skipped; totals may be undercounted\n",
				e2eAlphaID[:8]+file,
			)
		})
	}
}
