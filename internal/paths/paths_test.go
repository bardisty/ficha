package paths

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
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

// PathToProjectDir must mirror Claude Code's JS `/[^a-zA-Z0-9]/g`, which
// replaces per UTF-16 code unit: an astral-plane character (surrogate pair)
// becomes two dashes, while ASCII/BMP characters stay one dash.
func TestPathToProjectDir_UTF16CodeUnits(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"astral emoji is two dashes", "/home/x/app😀", "-home-x-app--"},
		{"bmp accented char stays one dash", "/home/x/café", "-home-x-caf-"},
		{"bmp cjk stays one dash", "/home/x/日本", "-home-x---"},
		{"multiple astral chars", "/a/😀😀", "-a-----"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PathToProjectDir(tt.input)
			if result != tt.expected {
				t.Errorf("PathToProjectDir(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBasenameCrossOS(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"windows path", `C:\Users\user\source\foo`, "foo"},
		{"windows path forward slashes", "C:/Users/user/source/foo", "foo"},
		{"windows drive root", `C:\`, "/"},
		{"unix path unchanged", "/home/user/foo", "foo"},
		{"bare name", "foo", "foo"},
		{"trailing backslash", `C:\Users\user\`, "user"},
		{"empty", "", "."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BasenameCrossOS(tt.input)
			if result != tt.expected {
				t.Errorf("BasenameCrossOS(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// FindProjectDir's basename fallback must match a Windows-style originalPath
// (WSL sharing a Windows config dir) even on Linux, where filepath.Base does
// not split on backslash.
func TestFindProjectDir_WindowsOriginalPathBasename(t *testing.T) {
	allProjects := []models.ProjectInfo{
		{
			EncodedPath:  "C--Users-user-source-foo",
			FullPath:     "/mnt/c/Users/user/.claude/projects/C--Users-user-source-foo",
			OriginalPath: `C:\Users\user\source\foo`,
			DisplayName:  "C:/Users/user/source/foo",
		},
	}

	match, err := FindProjectDir("/mnt/c/Users/user/source/foo", allProjects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match.MatchMethod != "suffix" {
		t.Errorf("expected suffix match, got %s", match.MatchMethod)
	}
	if match.EncodedPath != "C--Users-user-source-foo" {
		t.Errorf("expected C--Users-user-source-foo, got %s", match.EncodedPath)
	}
}

func TestLooksLikePath(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		{`.\proj`, true},             // Windows-style relative
		{`..\projects\C--foo`, true}, // Windows-style parent-relative
		{"./proj", true},             // Unix relative
		{"../proj", true},            // Unix parent-relative
		{"foo/bar", true},            // embedded separator
		{".", true},                  // current dir
		{"..", true},                 // parent dir
		{"-home-user-foo", false},    // encoded project name
		{"C--Users-user-foo", false}, // encoded Windows project name
		{"plainname", false},         // bare token
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := looksLikePath(tt.value); got != tt.expected {
				t.Errorf("looksLikePath(%q) = %v, want %v", tt.value, got, tt.expected)
			}
		})
	}
}

// A relative --project-dir value carrying a separator but no "./" prefix must
// route to the path branch (resolved relative to cwd), not the encoded-name
// branch under ~/.claude/projects. Before the looksLikePath change only "./"
// and "../" prefixes were recognized, so this — and the Windows backslash
// forms it now also catches — landed in the wrong branch. Uses a forward-slash
// path so the positive resolution is deterministic on Linux.
func TestResolveProjectDir_SeparatorRelativeRoutesToPath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ficha-resolve-sep-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	nested := filepath.Join(tempDir, "nested", "proj")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	t.Chdir(tempDir)

	result, err := ResolveProjectDir("nested/proj")
	if err != nil {
		t.Fatalf("unexpected error (value misrouted to encoded-name branch?): %v", err)
	}
	if result != nested {
		t.Errorf("expected %s, got %s", nested, result)
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

func TestFindProjectDir(t *testing.T) {
	// Create temp directory structure
	tempDir, err := os.MkdirTemp("", "ficha-test-*")
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
	tempDir, err := os.MkdirTemp("", "ficha-test-resolve-*")
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

		// t.Chdir restores the original working directory at test end.
		t.Chdir(tempDir)

		result, err := ResolveProjectDir("./rel-project")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != relDir {
			t.Errorf("expected %s, got %s", relDir, result)
		}
	})
}

func TestCanonicalizePath(t *testing.T) {
	t.Run("absolute path", func(t *testing.T) {
		// An absolute path that exists should resolve
		tmpDir, err := os.MkdirTemp("", "canon-test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		result, err := CanonicalizePath(tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !filepath.IsAbs(result) {
			t.Errorf("expected absolute path, got %q", result)
		}
	})

	t.Run("symlink resolution", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "canon-symlink-test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		realDir := filepath.Join(tmpDir, "real")
		if err := os.Mkdir(realDir, 0755); err != nil {
			t.Fatal(err)
		}
		linkDir := filepath.Join(tmpDir, "link")
		if err := os.Symlink(realDir, linkDir); err != nil {
			t.Skip("symlink creation not supported")
		}

		result, err := CanonicalizePath(linkDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != realDir {
			t.Errorf("expected %q, got %q", realDir, result)
		}
	})

	t.Run("nonexistent path fallback", func(t *testing.T) {
		path := "/nonexistent/path/to/project"
		result, err := CanonicalizePath(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should fall back to the uncanonicalized path
		if result != path {
			t.Errorf("expected fallback to %q, got %q", path, result)
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

// GetClaudeConfigDir must honor CLAUDE_CONFIG_DIR, Claude Code's own location override.
func TestGetClaudeConfigDir_EnvOverride(t *testing.T) {
	custom := filepath.Join(string(filepath.Separator), "custom", "claude-config")
	t.Setenv("CLAUDE_CONFIG_DIR", custom)

	dir, err := GetClaudeConfigDir()
	if err != nil {
		t.Fatalf("GetClaudeConfigDir failed: %v", err)
	}
	if dir != custom {
		t.Errorf("got %q, want %q", dir, custom)
	}

	projectsDir, err := GetProjectsDir()
	if err != nil {
		t.Fatalf("GetProjectsDir failed: %v", err)
	}
	if want := filepath.Join(custom, "projects"); projectsDir != want {
		t.Errorf("got %q, want %q", projectsDir, want)
	}
}

func TestGetClaudeConfigDir_DefaultWithoutEnv(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	dir, err := GetClaudeConfigDir()
	if err != nil {
		t.Fatalf("GetClaudeConfigDir failed: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}
	if want := filepath.Join(home, ".claude"); dir != want {
		t.Errorf("got %q, want %q", dir, want)
	}
}
