package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

func TestIsDriveRoot(t *testing.T) {
	tests := []struct {
		encoded  string
		expected bool
	}{
		{"F--", true},
		{"C--", true},
		{"H--", true},
		{"C--Users", false},
		{"F-", false},
		{"F---", false},
		{"1--", false},
	}

	for _, tt := range tests {
		t.Run(tt.encoded, func(t *testing.T) {
			result := isDriveRoot(tt.encoded)
			if result != tt.expected {
				t.Errorf("isDriveRoot(%q) = %v, want %v", tt.encoded, result, tt.expected)
			}
		})
	}
}

func TestFormatDisplayNameFromEncoded(t *testing.T) {
	tests := []struct {
		name     string
		encoded  string
		expected string
	}{
		{
			name:     "drive root",
			encoded:  "F--",
			expected: "F:",
		},
		{
			name:     "unix path",
			encoded:  "-home-user-foo",
			expected: "home-user-foo",
		},
		{
			name:     "long path not truncated",
			encoded:  "-home-user-source-my-long-project-name",
			expected: "home-user-source-my-long-project-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDisplayNameFromEncoded(tt.encoded)
			if result != tt.expected {
				t.Errorf("formatDisplayNameFromEncoded(%q) = %q, want %q", tt.encoded, result, tt.expected)
			}
		})
	}
}

func TestFormatDisplayNameFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "shows full path with drive",
			path:     "C:\\Users\\user\\source\\my-project",
			expected: "C:/Users/user/source/my-project",
		},
		{
			name:     "unix path (no drive)",
			path:     "/home/user/projects/my-project",
			expected: "home/user/projects/my-project",
		},
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "user home path",
			path:     "C:\\Users\\user",
			expected: "C:/Users/user",
		},
		{
			name:     "unix home path",
			path:     "/home/user",
			expected: "home/user",
		},
		{
			name:     "drive root",
			path:     "H:\\",
			expected: "H:",
		},
		{
			name:     "deep path",
			path:     "C:\\Users\\user\\AppData\\Roaming\\app\\notes",
			expected: "C:/Users/user/AppData/Roaming/app/notes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDisplayNameFromPath(tt.path)
			if result != tt.expected {
				t.Errorf("formatDisplayNameFromPath(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestStripWindowsUserPrefix(t *testing.T) {
	tests := []struct {
		name     string
		encoded  string
		expected string
	}{
		{
			name:     "simple project",
			encoded:  "C--Users-user-my-app",
			expected: "C:Users-user-my-app",
		},
		{
			name:     "project in source",
			encoded:  "C--Users-user-source-foo",
			expected: "C:Users-user-source-foo",
		},
		{
			name:     "dot folder",
			encoded:  "C--Users-user--config-app",
			expected: "C:Users-user--config-app",
		},
		{
			name:     "user home only",
			encoded:  "C--Users-user",
			expected: "C:Users-user",
		},
		{
			name:     "non-Users path",
			encoded:  "D--Source-foo-bar-baz",
			expected: "D:Source-foo-bar-baz",
		},
		{
			name:     "drive root encoded",
			encoded:  "F--",
			expected: "F--", // len < 4, returns as-is (formatDisplayName handles drive roots separately)
		},
		{
			name:     "project on F: drive",
			encoded:  "F--4-Workspace-project",
			expected: "F:4-Workspace-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripWindowsUserPrefix(tt.encoded)
			if result != tt.expected {
				t.Errorf("stripWindowsUserPrefix(%q) = %q, want %q", tt.encoded, result, tt.expected)
			}
		})
	}
}

func TestStripUnixUserPrefix(t *testing.T) {
	tests := []struct {
		name     string
		encoded  string
		expected string
	}{
		{
			name:     "simple project",
			encoded:  "-home-user-source-foo",
			expected: "home-user-source-foo",
		},
		{
			name:     "user home only",
			encoded:  "-home-user",
			expected: "home-user",
		},
		{
			name:     "Users style (macOS)",
			encoded:  "-Users-user-projects-bar",
			expected: "Users-user-projects-bar",
		},
		{
			name:     "tmp path",
			encoded:  "-tmp-something",
			expected: "tmp-something",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripUnixUserPrefix(tt.encoded)
			if result != tt.expected {
				t.Errorf("stripUnixUserPrefix(%q) = %q, want %q", tt.encoded, result, tt.expected)
			}
		})
	}
}

func TestResolveDisplayNameCollisions(t *testing.T) {
	projects := []models.ProjectInfo{
		{DisplayName: "myproject"},
		{DisplayName: "myproject"},
		{DisplayName: "myproject"},
		{DisplayName: "unique"},
	}
	resolveDisplayNameCollisions(projects)

	// First occurrence keeps original name
	if projects[0].DisplayName != "myproject" {
		t.Errorf("first: got %q, want %q", projects[0].DisplayName, "myproject")
	}
	// Second gets ~2
	if projects[1].DisplayName != "myproject~2" {
		t.Errorf("second: got %q, want %q", projects[1].DisplayName, "myproject~2")
	}
	// Third gets ~3
	if projects[2].DisplayName != "myproject~3" {
		t.Errorf("third: got %q, want %q", projects[2].DisplayName, "myproject~3")
	}
	// Unique stays unchanged
	if projects[3].DisplayName != "unique" {
		t.Errorf("unique: got %q, want %q", projects[3].DisplayName, "unique")
	}
}

// DiscoverAllProjects must include a project directory reached through a
// symlink. os.ReadDir's DirEntry.IsDir() is false for a symlink-to-dir, so
// without the os.Stat follow the symlinked project is dropped from global
// aggregation while show/list still resolve it. A broken symlink is skipped.
func TestDiscoverAllProjects_IncludesSymlinkedDir(t *testing.T) {
	tempDir := t.TempDir()
	projectsDir := filepath.Join(tempDir, "projects")
	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		t.Fatalf("failed to create projects dir: %v", err)
	}

	realProj := filepath.Join(projectsDir, "-home-user-realproj")
	if err := os.MkdirAll(realProj, 0755); err != nil {
		t.Fatalf("failed to create real project: %v", err)
	}

	// A project dir relocated outside ~/.claude/projects, symlinked back in.
	target := filepath.Join(tempDir, "external-big")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("failed to create symlink target: %v", err)
	}
	linkPath := filepath.Join(projectsDir, "-home-user-linkedproj")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Skipf("symlink creation not supported: %v", err)
	}

	// A broken symlink must be skipped (Stat fails), not counted.
	brokenLink := filepath.Join(projectsDir, "-home-user-broken")
	if err := os.Symlink(filepath.Join(tempDir, "does-not-exist"), brokenLink); err != nil {
		t.Fatalf("failed to create broken symlink: %v", err)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", tempDir)

	projects, err := DiscoverAllProjects()
	if err != nil {
		t.Fatalf("DiscoverAllProjects failed: %v", err)
	}

	found := make(map[string]bool)
	for _, p := range projects {
		found[p.EncodedPath] = true
	}
	if !found["-home-user-realproj"] {
		t.Error("real project dir missing")
	}
	if !found["-home-user-linkedproj"] {
		t.Error("symlinked project dir was dropped")
	}
	if found["-home-user-broken"] {
		t.Error("broken symlink should have been skipped")
	}
}

// A synthesized "~N" collision suffix must not collide with a genuine project
// whose display name already ends in "~N" (paths may legitimately contain '~').
func TestResolveDisplayNameCollisions_SuffixAvoidsRealName(t *testing.T) {
	projects := []models.ProjectInfo{
		{DisplayName: "home/u/app"},
		{DisplayName: "home/u/app"},
		{DisplayName: "home/u/app~2"}, // a real, distinct project
	}
	resolveDisplayNameCollisions(projects)

	seen := make(map[string]bool)
	for i, p := range projects {
		if seen[p.DisplayName] {
			t.Errorf("duplicate display name %q at index %d", p.DisplayName, i)
		}
		seen[p.DisplayName] = true
	}
	// The second colliding project must skip the taken "~2" and use "~3".
	if projects[1].DisplayName != "home/u/app~3" {
		t.Errorf("second: got %q, want %q", projects[1].DisplayName, "home/u/app~3")
	}
	if projects[2].DisplayName != "home/u/app~2" {
		t.Errorf("real ~2 project name changed: got %q", projects[2].DisplayName)
	}
}

func TestIsLetter(t *testing.T) {
	tests := []struct {
		char     byte
		expected bool
	}{
		{'A', true},
		{'Z', true},
		{'a', true},
		{'z', true},
		{'0', false},
		{'-', false},
		{'/', false},
	}

	for _, tt := range tests {
		t.Run(string(tt.char), func(t *testing.T) {
			result := isLetter(tt.char)
			if result != tt.expected {
				t.Errorf("isLetter(%q) = %v, want %v", tt.char, result, tt.expected)
			}
		})
	}
}
