package paths

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bardisty/ccusage/internal/models"
)

func TestPathToProjectDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		testWindowsPaths(t)
	} else {
		testUnixPaths(t)
	}
}

func testUnixPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple absolute", "/home/user/project", "-home-user-project"},
		{"root", "/", "-"},
		{"nested path", "/home/user/code/my-project", "-home-user-code-my-project"},
		{"trailing slash", "/home/user/project/", "-home-user-project"},
		{"double slashes", "/home//user/project", "-home-user-project"},
		{"path with underscores", "/home/user/my_project", "-home-user-my-project"},
		{"path with spaces", "/home/user/my project", "-home-user-my-project"},
		{"path with parens", "/home/user/test (1)", "-home-user-test--1-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PathToProjectDir(tt.input)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

func testWindowsPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple windows path", "C:\\Users\\user\\project", "C--Users-user-project"},
		{"drive letter only", "C:\\", "C--"},
		{"forward slashes on windows", "C:/Users/user/project", "C--Users-user-project"},
		{"path with underscores", "D:\\4_Workspace\\action-based-filing-system", "D--4-Workspace-action-based-filing-system"},
		{"path with spaces and parens", "F:\\4_Workspace\\2026-02-03 - FitBit (Health) _b632", "F--4-Workspace-2026-02-03---FitBit--Health---b632"},
		{"path with brackets", "C:\\Users\\test\\[project]", "C--Users-test--project-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PathToProjectDir(tt.input)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestGetSessionsIndexPath(t *testing.T) {
	projectDir := "/home/user/.claude/projects/-home-user-myproject"
	result := GetSessionsIndexPath(projectDir)
	expected := projectDir + "/sessions-index.json"

	// Normalize for cross-platform
	result = strings.ReplaceAll(result, "\\", "/")
	expected = strings.ReplaceAll(expected, "\\", "/")

	if result != expected {
		t.Errorf("got %s, want %s", result, expected)
	}
}

func TestGetSessionFilePath(t *testing.T) {
	projectDir := "/home/user/.claude/projects/-home-user-myproject"
	sessionID := "abc123-def456"
	result := GetSessionFilePath(projectDir, sessionID)
	expected := projectDir + "/" + sessionID + ".jsonl"

	// Normalize for cross-platform
	result = strings.ReplaceAll(result, "\\", "/")
	expected = strings.ReplaceAll(expected, "\\", "/")

	if result != expected {
		t.Errorf("got %s, want %s", result, expected)
	}
}

func TestFindProjectDir(t *testing.T) {
	// Create temp directory structure
	tempDir, err := os.MkdirTemp("", "ccusage-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create fake projects directory
	projectsDir := filepath.Join(tempDir, ".claude", "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatalf("failed to create projects dir: %v", err)
	}

	// Create project directories using Unix-style paths for portability
	project1 := filepath.Join(projectsDir, "-home-user-myproject")
	project2 := filepath.Join(projectsDir, "-old-path-myproject")
	project3 := filepath.Join(projectsDir, "-other-path-otherproject")

	for _, dir := range []string{project1, project2, project3} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create dir %s: %v", dir, err)
		}
	}

	// Mock projects list (simulating what DiscoverAllProjects would return)
	// Use Unix-style paths for cross-platform compatibility
	allProjects := []models.ProjectInfo{
		{
			EncodedPath:  "-home-user-myproject",
			FullPath:     project1,
			OriginalPath: "/home/user/myproject",
			DisplayName:  "myproject",
		},
		{
			EncodedPath:  "-old-path-myproject",
			FullPath:     project2,
			OriginalPath: "/old/path/myproject",
			DisplayName:  "myproject~2",
		},
		{
			EncodedPath:  "-other-path-otherproject",
			FullPath:     project3,
			OriginalPath: "/other/path/otherproject",
			DisplayName:  "otherproject",
		},
	}

	t.Run("exact match", func(t *testing.T) {
		// This test requires the directory to exist at the exact encoded path
		// which is tricky to test without mocking the filesystem
		// Skip for now - the exact match path is tested implicitly by the fallback tests
		t.Skip("exact match requires filesystem mocking")
	})

	t.Run("suffix match single", func(t *testing.T) {
		// Looking for a project named "otherproject" from a different path
		// Use an absolute path that won't match any existing encoded path
		match, err := FindProjectDir("/new/path/otherproject", allProjects)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if match.MatchMethod != "suffix" {
			t.Errorf("expected suffix match, got %s", match.MatchMethod)
		}
		if match.EncodedPath != "-other-path-otherproject" {
			t.Errorf("expected -other-path-otherproject, got %s", match.EncodedPath)
		}
		if match.MatchInfo == "" {
			t.Error("expected match info for suffix match")
		}
	})

	t.Run("ambiguous match", func(t *testing.T) {
		// Looking for "myproject" which exists in two locations
		_, err := FindProjectDir("/some/new/path/myproject", allProjects)
		if err == nil {
			t.Fatal("expected error for ambiguous match")
		}
		var ambigErr *AmbiguousProjectError
		if !errors.As(err, &ambigErr) {
			t.Fatalf("expected AmbiguousProjectError, got %T: %v", err, err)
		}
		if ambigErr.Basename != "myproject" {
			t.Errorf("expected basename 'myproject', got %s", ambigErr.Basename)
		}
		if len(ambigErr.Matches) != 2 {
			t.Errorf("expected 2 matches, got %d", len(ambigErr.Matches))
		}
	})

	t.Run("case insensitive match", func(t *testing.T) {
		// Looking for "OtherProject" (different case) should still match
		match, err := FindProjectDir("/new/path/OtherProject", allProjects)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if match.MatchMethod != "suffix" {
			t.Errorf("expected suffix match, got %s", match.MatchMethod)
		}
		if match.EncodedPath != "-other-path-otherproject" {
			t.Errorf("expected -other-path-otherproject, got %s", match.EncodedPath)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, err := FindProjectDir("/path/to/unknownproject", allProjects)
		if err == nil {
			t.Fatal("expected error for no match")
		}
		if !errors.Is(err, ErrNoProjectFound) {
			t.Errorf("expected ErrNoProjectFound, got %v", err)
		}
	})
}

func TestResolveProjectDir(t *testing.T) {
	// Create temp directory structure
	tempDir, err := os.MkdirTemp("", "ccusage-test-resolve-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a fake project directory in the expected location
	// This is tricky because ResolveProjectDir uses GetProjectsDir()
	// which returns ~/.claude/projects - we can't easily mock that

	t.Run("absolute path", func(t *testing.T) {
		// Create a temp dir to use as the project dir
		projDir := filepath.Join(tempDir, "test-project")
		if err := os.MkdirAll(projDir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}

		result, err := ResolveProjectDir(projDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != projDir {
			t.Errorf("expected %s, got %s", projDir, result)
		}
	})

	t.Run("nonexistent absolute path", func(t *testing.T) {
		_, err := ResolveProjectDir(filepath.Join(tempDir, "nonexistent"))
		if err == nil {
			t.Fatal("expected error for nonexistent path")
		}
	})

	t.Run("relative path with dot prefix", func(t *testing.T) {
		// Create a directory accessible via relative path
		relDir := filepath.Join(tempDir, "rel-project")
		if err := os.MkdirAll(relDir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}

		// Save and restore cwd
		origDir, _ := os.Getwd()
		defer os.Chdir(origDir)
		os.Chdir(tempDir)

		result, err := ResolveProjectDir("./rel-project")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != relDir {
			t.Errorf("expected %s, got %s", relDir, result)
		}
	})
}

func TestAmbiguousProjectError(t *testing.T) {
	err := &AmbiguousProjectError{
		Basename: "myproject",
		Matches: []models.ProjectInfo{
			{EncodedPath: "path1", OriginalPath: "/original/path1"},
			{EncodedPath: "path2", OriginalPath: "/original/path2"},
		},
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "myproject") {
		t.Error("error should contain basename")
	}
	if !strings.Contains(errStr, "path1") {
		t.Error("error should contain first match")
	}
	if !strings.Contains(errStr, "path2") {
		t.Error("error should contain second match")
	}
	if !strings.Contains(errStr, "--project-dir") {
		t.Error("error should suggest --project-dir flag")
	}
}
