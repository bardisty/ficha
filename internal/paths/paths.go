package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// GetClaudeConfigDir returns the path to the Claude config directory
func GetClaudeConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".claude"), nil
}

// GetProjectsDir returns the path to the Claude projects directory
func GetProjectsDir() (string, error) {
	configDir, err := GetClaudeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "projects"), nil
}

// PathToProjectDir converts a filesystem path to the Claude project directory name
// e.g., /home/user/project -> -home-user-project
func PathToProjectDir(path string) string {
	// Normalize the path
	path = filepath.Clean(path)

	// On Windows, replace backslashes with forward slashes first
	if runtime.GOOS == "windows" {
		// Convert C:\Users\foo to C/Users/foo style
		path = strings.ReplaceAll(path, "\\", "/")
		// Replace colon with dash (C: -> C-)
		path = strings.ReplaceAll(path, ":", "-")
	}

	// Replace path separators with dashes
	path = strings.ReplaceAll(path, "/", "-")

	// Replace underscores with dashes (Claude Code does this)
	path = strings.ReplaceAll(path, "_", "-")

	// Handle leading dash for absolute paths
	if !strings.HasPrefix(path, "-") && filepath.IsAbs(path) {
		path = "-" + path
	}

	return path
}

// CanonicalizePath resolves symlinks and converts relative paths to absolute.
// This ensures consistent project directory naming regardless of how the path is accessed.
func CanonicalizePath(path string) (string, error) {
	// Convert relative to absolute first
	if !filepath.IsAbs(path) {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = absPath
	}

	// Resolve symlinks to get the real path
	// This ensures /symlink/to/project and /real/path/to/project map to the same directory
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		// If the path doesn't exist yet, EvalSymlinks fails
		// In that case, just return the absolute path
		if os.IsNotExist(err) {
			return path, nil
		}
		return "", err
	}

	return realPath, nil
}

// GetProjectDirForPath returns the full path to the Claude project directory for a given path.
// It canonicalizes the path first (resolving symlinks and converting relative to absolute)
// to ensure consistent project directory naming.
func GetProjectDirForPath(path string) (string, error) {
	projectsDir, err := GetProjectsDir()
	if err != nil {
		return "", err
	}

	// Canonicalize the path to handle symlinks and relative paths
	canonicalPath, err := CanonicalizePath(path)
	if err != nil {
		return "", err
	}

	projectDir := PathToProjectDir(canonicalPath)
	return filepath.Join(projectsDir, projectDir), nil
}

// GetSessionsIndexPath returns the path to the sessions-index.json file for a project
func GetSessionsIndexPath(projectDir string) string {
	return filepath.Join(projectDir, "sessions-index.json")
}

// GetCurrentProjectDir returns the Claude project directory for the current working directory
func GetCurrentProjectDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return GetProjectDirForPath(cwd)
}

// GetSessionFilePath returns the full path to a session JSONL file
func GetSessionFilePath(projectDir, sessionID string) string {
	return filepath.Join(projectDir, sessionID+".jsonl")
}

// ProjectDirExists checks if a Claude project directory exists
func ProjectDirExists(path string) (bool, error) {
	projectDir, err := GetProjectDirForPath(path)
	if err != nil {
		return false, err
	}

	info, err := os.Stat(projectDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}
