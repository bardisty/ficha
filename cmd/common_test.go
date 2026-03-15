package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/bardisty/ccusage/internal/models"
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
}
