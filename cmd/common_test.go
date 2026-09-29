package cmd

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

func TestFindSessionByPartialID(t *testing.T) {
	sessions := []models.SessionEntry{
		{SessionID: "abc12345-full-id-here"},
		{SessionID: "abc12399-different"},
		{SessionID: "def45678-unique"},
	}

	t.Run("exact match", func(t *testing.T) {
		s, err := findSessionByPartialID(sessions, "abc12345-full-id-here")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.SessionID != "abc12345-full-id-here" {
			t.Errorf("got %q, want %q", s.SessionID, "abc12345-full-id-here")
		}
	})

	t.Run("unique prefix", func(t *testing.T) {
		s, err := findSessionByPartialID(sessions, "def4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.SessionID != "def45678-unique" {
			t.Errorf("got %q, want %q", s.SessionID, "def45678-unique")
		}
	})

	t.Run("ambiguous prefix", func(t *testing.T) {
		_, err := findSessionByPartialID(sessions, "abc")
		if err == nil {
			t.Fatal("expected error for ambiguous prefix")
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Errorf("error should contain 'ambiguous': %v", err)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, err := findSessionByPartialID(sessions, "zzz")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("expected ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("empty sessions", func(t *testing.T) {
		_, err := findSessionByPartialID([]models.SessionEntry{}, "abc")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("expected ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("empty partialID", func(t *testing.T) {
		_, err := findSessionByPartialID(sessions, "")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("expected ErrSessionNotFound, got %v", err)
		}
	})
}

func TestSimilarProjects(t *testing.T) {
	t.Run("no projects", func(t *testing.T) {
		if got := similarProjects("/path/to/myproject", nil); len(got) != 0 {
			t.Errorf("want none, got %v", got)
		}
	})

	t.Run("name prefix match", func(t *testing.T) {
		projects := []models.ProjectInfo{
			{EncodedPath: "-home-user-myproject", OriginalPath: "/home/user/myproject"},
			{EncodedPath: "-home-user-otherproject", OriginalPath: "/home/user/otherproject"},
		}
		got := similarProjects("/other/path/myproj", projects)
		if len(got) != 1 || got[0].EncodedPath != "-home-user-myproject" {
			t.Errorf("want only myproject, got %v", got)
		}
	})

	// A Windows-style originalPath (WSL sharing a Windows config dir) must
	// still surface as a suggestion; filepath.Base does not split on backslash
	// on Linux, so BasenameCrossOS is required for the basename comparison.
	t.Run("windows-style originalPath", func(t *testing.T) {
		projects := []models.ProjectInfo{
			{EncodedPath: "C--Users-user-source-foo", OriginalPath: `C:\Users\user\source\foo`},
		}
		if got := similarProjects("/mnt/c/Users/user/source/foo", projects); len(got) != 1 {
			t.Errorf("want the Windows project suggested, got %v", got)
		}
	})
}

func TestProjectArgs(t *testing.T) {
	unix := models.ProjectInfo{EncodedPath: "-home-user-myproject", OriginalPath: "/home/user/myproject"}
	windows := models.ProjectInfo{EncodedPath: "C--Users-user-source-foo", OriginalPath: `C:\Users\user\source\foo`}
	noPath := models.ProjectInfo{EncodedPath: "-home-user-gone"}

	// A path from the other OS isn't absolute here, so -p couldn't resolve
	// it and the encoded name stands in.
	wantUnix, wantWindows := "-p /home/user/myproject", "--project-dir=C--Users-user-source-foo"
	if runtime.GOOS == "windows" {
		wantUnix, wantWindows = "--project-dir=-home-user-myproject", `-p 'C:\Users\user\source\foo'`
	}
	for _, tt := range []struct {
		p    models.ProjectInfo
		want string
	}{{unix, wantUnix}, {windows, wantWindows}, {noPath, "--project-dir=-home-user-gone"}} {
		if got := projectArgs(tt.p); got != tt.want {
			t.Errorf("projectArgs(%s) = %q, want %q", tt.p.EncodedPath, got, tt.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"/home/u/source/webapp": "/home/u/source/webapp",
		"~/src/app-2.0":         "~/src/app-2.0",
		"/home/u/my project":    "'/home/u/my project'",
		"/tmp/it's":             `'/tmp/it'\''s'`,
		"":                      "''",
	}
	for in, want := range tests {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestCompletionValue(t *testing.T) {
	sessions := []models.SessionEntry{
		{SessionID: "aaaaaaaa-1111"},
		{SessionID: "bbbbbbbb-1111"},
		{SessionID: "bbbbbbbb-2222"},
		{SessionID: "agent-a1"},
	}
	tests := []struct {
		id    string
		typed int
		want  string
	}{
		{"aaaaaaaa-1111", 0, "aaaaaaaa"},
		{"aaaaaaaa-1111", 9, "aaaaaaaa-1111"}, // typed past the short form
		{"bbbbbbbb-1111", 0, "bbbbbbbb-1111"}, // shares its 8-char prefix
		{"agent-a1", 0, "agent-a1"},           // already short
	}
	for _, tt := range tests {
		if got := completionValue(tt.id, sessions, tt.typed); got != tt.want {
			t.Errorf("completionValue(%q, typed=%d) = %q, want %q", tt.id, tt.typed, got, tt.want)
		}
	}
}

// On a terminal the warnings follow the report; redirected, they lead.
func TestWriteReportOrder(t *testing.T) {
	for _, tt := range []struct {
		warningsLast bool
		want         string
	}{
		{true, "report\nWarning: w\n"},
		{false, "Warning: w\nreport\n"},
	} {
		var both bytes.Buffer
		warnings := bytes.NewBufferString("Warning: w\n")
		writeReport(&both, &both, warnings, "report", tt.warningsLast)
		if both.String() != tt.want {
			t.Errorf("warningsLast=%v: got %q, want %q", tt.warningsLast, both.String(), tt.want)
		}
	}
}

func TestProjectLabel(t *testing.T) {
	tests := []struct {
		name string
		p    models.ProjectInfo
		want string
	}{
		{"original path wins", models.ProjectInfo{EncodedPath: "-home-u-src-app", OriginalPath: "/home/u/src/app", DisplayName: "home/u/src/app"}, "/home/u/src/app"},
		{"Windows original path kept as is", models.ProjectInfo{EncodedPath: "C--Users-u-app", OriginalPath: `C:\Users\u\app`}, `C:\Users\u\app`},
		// DisplayName's decoding of the encoded name differs by OS; the raw name doesn't.
		{"no original path: encoded name", models.ProjectInfo{EncodedPath: "-home-u-src-app", DisplayName: "home-u-src-app"}, "-home-u-src-app"},
	}
	for _, tt := range tests {
		if got := projectLabel(tt.p); got != tt.want {
			t.Errorf("%s: projectLabel = %q, want %q", tt.name, got, tt.want)
		}
	}
}
