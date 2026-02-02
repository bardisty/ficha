package paths

import (
	"runtime"
	"strings"
	"testing"
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
