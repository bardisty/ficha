package parser

import (
	"testing"
)

func TestDecodeProjectPath(t *testing.T) {
	tests := []struct {
		name     string
		encoded  string
		expected string
	}{
		{
			name:     "empty string",
			encoded:  "",
			expected: "",
		},
		{
			name:     "simple unix path",
			encoded:  "-home-user-project",
			expected: "/home/user/project",
		},
		{
			name:     "path with dashes in name",
			encoded:  "-home-user-my-project",
			expected: "/home/user/my/project", // Naive decode - ambiguous
		},
		{
			name:     "single component",
			encoded:  "-tmp",
			expected: "/tmp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DecodeProjectPath(tt.encoded)
			if result != tt.expected {
				t.Errorf("DecodeProjectPath(%q) = %q, want %q", tt.encoded, result, tt.expected)
			}
		})
	}
}

func TestGenerateUnixPathCandidates(t *testing.T) {
	// Test that candidates are generated correctly for Unix
	encoded := "-home-user-my-project"
	// Strip leading dash like generatePathCandidates does
	candidates := generateUnixPathCandidates(encoded)

	// Should generate multiple candidates
	if len(candidates) == 0 {
		t.Fatal("generateUnixPathCandidates should return candidates")
	}

	// Should include /home/user/my-project as a candidate
	found := false
	for _, c := range candidates {
		if c == "/home/user/my-project" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected /home/user/my-project in candidates, got: %v", candidates)
	}

	// Should include /home/user-my-project as a candidate
	found = false
	for _, c := range candidates {
		if c == "/home/user-my-project" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected /home/user-my-project in candidates, got: %v", candidates)
	}
}

func TestGenerateWindowsPathCandidates(t *testing.T) {
	// Test Windows path candidate generation
	encoded := "C--Users-Brian-source-firefox-tab-management"
	candidates := generateWindowsPathCandidates(encoded)

	// Should generate multiple candidates
	if len(candidates) == 0 {
		t.Fatal("generateWindowsPathCandidates should return candidates")
	}

	// Should include C:\Users\Brian\source\firefox-tab-management
	expected := `C:\Users\Brian\source\firefox-tab-management`
	found := false
	for _, c := range candidates {
		if c == expected {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected %s in candidates, got: %v", expected, candidates)
	}

	// Should include C:\Users\Brian\source-firefox-tab-management
	expected = `C:\Users\Brian\source-firefox-tab-management`
	found = false
	for _, c := range candidates {
		if c == expected {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected %s in candidates, got: %v", expected, candidates)
	}
}

func TestGenerateWindowsPathCandidates_DotFolder(t *testing.T) {
	// Test handling of "--" which represents "\.folder" (dot-prefixed)
	encoded := "C--Users-Brian--config-yasb"
	candidates := generateWindowsPathCandidates(encoded)

	if len(candidates) == 0 {
		t.Fatal("generateWindowsPathCandidates should return candidates for dot-folder paths")
	}

	// First candidate should be the dot-variant: C:\Users\Brian\.config\yasb
	expected := `C:\Users\Brian\.config\yasb`
	if candidates[0] != expected {
		t.Errorf("First candidate should be %s, got: %s", expected, candidates[0])
	}
}

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
			path:     "C:\\Users\\Brian\\source\\my-project",
			expected: "C:Users/Brian/source/my-project",
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
			path:     "C:\\Users\\Brian",
			expected: "C:Users/Brian",
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
			path:     "C:\\Users\\Brian\\AppData\\Roaming\\Heynote\\notes",
			expected: "C:Users/Brian/AppData/Roaming/Heynote/notes",
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
			encoded:  "C--Users-Brian-hyper-key",
			expected: "C:Users-Brian-hyper-key",
		},
		{
			name:     "project in source",
			encoded:  "C--Users-Brian-source-fun",
			expected: "C:Users-Brian-source-fun",
		},
		{
			name:     "dot folder",
			encoded:  "C--Users-Brian--config-yasb",
			expected: "C:Users-Brian--config-yasb",
		},
		{
			name:     "user home only",
			encoded:  "C--Users-Brian",
			expected: "C:Users-Brian",
		},
		{
			name:     "non-Users path",
			encoded:  "D--Source-action-based-filing",
			expected: "D:Source-action-based-filing",
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
			encoded:  "-home-bah-source-foo",
			expected: "home-bah-source-foo",
		},
		{
			name:     "user home only",
			encoded:  "-home-bah",
			expected: "home-bah",
		},
		{
			name:     "Users style (macOS)",
			encoded:  "-Users-brian-projects-bar",
			expected: "Users-brian-projects-bar",
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

func TestTruncateWithEllipsis(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "no truncation needed",
			input:    "short",
			maxLen:   25,
			expected: "short",
		},
		{
			name:     "simple truncation",
			input:    "this-is-a-very-long-project-name-that-exceeds-limit",
			maxLen:   25,
			expected: "...ame-that-exceeds-limit",
		},
		{
			name:     "exact truncation",
			input:    "foo-bar-baz-qux-very-long-name",
			maxLen:   20,
			expected: "...ux-very-long-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateWithEllipsis(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncateWithEllipsis(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestDecodeWindowsPath(t *testing.T) {
	tests := []struct {
		name     string
		encoded  string
		expected string
	}{
		{
			name:     "simple path",
			encoded:  "C--Users-Brian",
			expected: `C:\Users\Brian`,
		},
		{
			name:     "path with dashes in name (naive decode)",
			encoded:  "C--Users-Brian-source-firefox-tab-management",
			expected: `C:\Users\Brian\source\firefox\tab\management`,
		},
		{
			name:     "short path",
			encoded:  "C--tmp",
			expected: `C:\tmp`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := decodeWindowsPath(tt.encoded)
			if result != tt.expected {
				t.Errorf("decodeWindowsPath(%q) = %q, want %q", tt.encoded, result, tt.expected)
			}
		})
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
