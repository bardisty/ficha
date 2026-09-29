package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// A bad --since or --until is a flag error, so it must be reported before
// anything that reads the disk. Otherwise a typo run from a directory with no
// sessions, or on a machine with no Claude Code data, reports the missing data
// and the typo never shows up.
func TestE2EWindowErrorsComeBeforeDiskErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		noData  bool
		wantErr string
	}{
		{"summary, bad --since", []string{"summary", "--since", "yesterdayy"}, false, `invalid --since "yesterdayy"`},
		{"summary, bad --until", []string{"summary", "--until", "tomorow"}, false, `invalid --until "tomorow"`},
		{"summary, backwards range", []string{"summary", "--since", "2026-09-10", "--until", "2026-09-01"}, false, "--since 2026-09-10 is not before --until 2026-09-01"},
		{"global, bad --since", []string{"global", "--since", "yesterdayy"}, true, `invalid --since "yesterdayy"`},
		{"global, bad --until", []string{"global", "--until", "tomorow"}, true, `invalid --until "tomorow"`},
		{"global, backwards range", []string{"global", "--since", "2026-09-10", "--until", "2026-09-01"}, true, "--since 2026-09-10 is not before --until 2026-09-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupE2EFixture(t)
			// summary runs from a directory Claude Code never recorded a
			// session for; global gets a config dir that doesn't exist.
			t.Chdir(t.TempDir())
			if tt.noData {
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "nonexistent"))
			}
			stdout, _, err := executeCLISplit(t, tt.args...)
			if err == nil {
				t.Fatalf("expected error %q, got nil\nstdout: %s", tt.wantErr, stdout)
			}
			if !strings.HasPrefix(err.Error(), tt.wantErr) {
				t.Errorf("error:\n got: %s\nwant prefix: %s", err, tt.wantErr)
			}
		})
	}
}
