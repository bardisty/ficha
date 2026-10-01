package analyzer

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

// An index that can't be used reaches the caller for every project that has
// one, in input order: one the window empties, and one with no transcripts,
// as well as one in the report. A missing index isn't an error.
func TestGlobalReportsIgnoredIndexesInInputOrder(t *testing.T) {
	dir := t.TempDir()
	project := func(name, index string, lines ...string) models.ProjectInfo {
		projDir := filepath.Join(dir, name)
		if err := os.Mkdir(projDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if index != "" {
			if err := os.WriteFile(filepath.Join(projDir, "sessions-index.json"), []byte(index), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if len(lines) > 0 {
			writeJSONLFile(t, filepath.Join(projDir, "s.jsonl"), lines)
		}
		return models.ProjectInfo{EncodedPath: name, FullPath: projDir, DisplayName: name}
	}
	projects := []models.ProjectInfo{
		project("zz-live", "{", windowMsg),
		project("no-index", "", windowMsg),
		project("out", "{", forkMsg1),
		project("valid", `{"version":1,"entries":[]}`, windowMsg),
		project("aa-empty", "{"),
	}

	for range 20 {
		global, err := AnalyzeAllProjectsInWindow(projects, skipWindow)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, ignored := range global.IgnoredIndexes {
			if ignored.Err == nil || errors.Is(ignored.Err, fs.ErrNotExist) {
				t.Errorf("%s: got error %v, want a parse error", ignored.Path, ignored.Err)
			}
			got = append(got, ignored.Path)
		}
		want := []string{
			filepath.Join(dir, "zz-live", "sessions-index.json"),
			filepath.Join(dir, "out", "sessions-index.json"),
			filepath.Join(dir, "aa-empty", "sessions-index.json"),
		}
		if !slices.Equal(got, want) {
			t.Fatalf("ignored indexes:\n got: %q\nwant: %q", got, want)
		}
		if global.ProjectCount != 3 || len(global.OutOfWindow) != 0 {
			t.Errorf("got %d projects and %d out of window, want 3 and 0", global.ProjectCount, len(global.OutOfWindow))
		}
	}
}
