package parser

import (
	"testing"
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
			path:     "C:\\Users\\Brian\\source\\my-project",
			expected: "C:/Users/Brian/source/my-project",
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
			expected: "C:/Users/Brian",
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
			expected: "C:/Users/Brian/AppData/Roaming/Heynote/notes",
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
