package cmd

import (
	"bytes"
	"errors"
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

func TestFormatNoProjectError(t *testing.T) {
	t.Run("no similar projects", func(t *testing.T) {
		err := formatNoProjectError("/path/to/myproject", nil)
		if !strings.Contains(err.Error(), "no Claude sessions found for") {
			t.Errorf("error should contain 'no Claude sessions found for': %v", err)
		}
		if strings.Contains(err.Error(), "Similar projects") {
			t.Error("should not contain 'Similar projects' when none match")
		}
	})

	t.Run("with similar project", func(t *testing.T) {
		projects := []models.ProjectInfo{
			{EncodedPath: "-home-user-myproject", OriginalPath: "/home/user/myproject"},
		}
		err := formatNoProjectError("/other/path/myproject", projects)
		if !strings.Contains(err.Error(), "Similar projects") {
			t.Errorf("error should contain 'Similar projects': %v", err)
		}
	})

	t.Run("no basename match", func(t *testing.T) {
		projects := []models.ProjectInfo{
			{EncodedPath: "-home-user-otherproject", OriginalPath: "/home/user/otherproject"},
		}
		err := formatNoProjectError("/path/to/myproject", projects)
		if strings.Contains(err.Error(), "Similar projects") {
			t.Error("should not contain 'Similar projects' when basenames don't match")
		}
	})

	// A Windows-style originalPath (WSL sharing a Windows config dir) must
	// still surface as a suggestion; filepath.Base does not split on backslash
	// on Linux, so BasenameCrossOS is required for the basename comparison.
	t.Run("windows-style originalPath suggested", func(t *testing.T) {
		projects := []models.ProjectInfo{
			{EncodedPath: "C--Users-user-source-foo", OriginalPath: `C:\Users\user\source\foo`},
		}
		err := formatNoProjectError("/mnt/c/Users/user/source/foo", projects)
		if !strings.Contains(err.Error(), "Similar projects") {
			t.Errorf("expected Windows-style originalPath to be suggested: %v", err)
		}
		if !strings.Contains(err.Error(), "C--Users-user-source-foo") {
			t.Errorf("suggestion list should list the matching project: %v", err)
		}
	})
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
