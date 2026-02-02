package parser

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/paths"
)

// DiscoverAllProjects scans ~/.claude/projects/ and returns all project directories
func DiscoverAllProjects() ([]models.ProjectInfo, error) {
	projectsDir, err := paths.GetProjectsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, err
	}

	var projects []models.ProjectInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		encodedPath := entry.Name()
		// Skip hidden directories
		if strings.HasPrefix(encodedPath, ".") {
			continue
		}

		fullPath := filepath.Join(projectsDir, encodedPath)

		// Try to get original path from sessions-index.json
		originalPath := getOriginalPathFromIndex(fullPath)

		// Generate display name from original path if available, else from encoded
		var displayName string
		if originalPath != "" {
			displayName = formatDisplayNameFromPath(originalPath)
		}
		// Fall back to encoded path if no original path or if display name is empty
		if displayName == "" {
			displayName = formatDisplayNameFromEncoded(encodedPath)
		}

		projects = append(projects, models.ProjectInfo{
			EncodedPath:  encodedPath,
			FullPath:     fullPath,
			OriginalPath: originalPath,
			DisplayName:  displayName,
		})
	}

	// Handle display name collisions by adding ~2, ~3, etc.
	resolveDisplayNameCollisions(projects)

	return projects, nil
}

// getOriginalPathFromIndex reads sessions-index.json and returns the originalPath if present.
// Falls back to projectPath from the first session entry if originalPath is empty.
func getOriginalPathFromIndex(projectDir string) string {
	indexPath := filepath.Join(projectDir, "sessions-index.json")
	index, err := ParseSessionsIndex(indexPath)
	if err != nil {
		return ""
	}

	// Use top-level originalPath if available
	if index.OriginalPath != "" {
		return index.OriginalPath
	}

	// Fall back to projectPath from first session entry
	if len(index.Entries) > 0 && index.Entries[0].ProjectPath != "" {
		return index.Entries[0].ProjectPath
	}

	return ""
}

// formatDisplayNameFromPath creates a display name from a real file path.
// Shows drive:full/path with forward slashes.
func formatDisplayNameFromPath(path string) string {
	if path == "" {
		return ""
	}

	// Handle Windows drive roots like "H:\" or "F:\"
	if len(path) == 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return string(path[0]) + ":"
	}
	// Handle drive roots without trailing slash "H:"
	if len(path) == 2 && path[1] == ':' {
		return path
	}

	// Extract drive letter if present (Windows paths)
	var drivePrefix string
	workPath := path
	if len(path) >= 2 && path[1] == ':' {
		drivePrefix = string(path[0]) + ":"
		workPath = path[2:]
	}

	// Normalize path separators to forward slashes
	normalized := strings.ReplaceAll(workPath, "\\", "/")
	// Remove leading/trailing slashes
	normalized = strings.Trim(normalized, "/")

	if normalized == "" {
		return drivePrefix
	}

	// Return full path with drive prefix
	return drivePrefix + normalized
}

// formatDisplayNameFromEncoded creates a display name from an encoded folder name.
// Used as fallback when sessions-index.json doesn't exist.
func formatDisplayNameFromEncoded(encoded string) string {
	// Handle drive roots (e.g., "F--" → "F:")
	if isDriveRoot(encoded) {
		return string(encoded[0]) + ":"
	}

	// Strip drive/root prefix, keep the rest
	return stripUserPrefix(encoded)
}

// stripUserPrefix removes the common user directory prefix from an encoded path.
// Windows: C--Users-Brian- or C--Users-Brian--
// Unix: -home-username- or -Users-username-
func stripUserPrefix(encoded string) string {
	if runtime.GOOS == "windows" {
		return stripWindowsUserPrefix(encoded)
	}
	return stripUnixUserPrefix(encoded)
}

// stripWindowsUserPrefix handles Windows paths like C--Users-Brian-source-foo
// Only strips the drive encoding (C-- → C:), keeps the rest as-is
func stripWindowsUserPrefix(encoded string) string {
	if len(encoded) < 4 {
		return encoded
	}

	// Check for drive letter pattern: X--...
	if !isLetter(encoded[0]) || encoded[1] != '-' || encoded[2] != '-' {
		return encoded
	}

	// Convert C--rest to C:rest
	return string(encoded[0]) + ":" + encoded[3:]
}

// stripUnixUserPrefix handles Unix paths like -home-bah-source-foo
// Only strips the leading dash (root /), keeps the rest as-is
func stripUnixUserPrefix(encoded string) string {
	if len(encoded) < 2 || encoded[0] != '-' {
		return encoded
	}

	// Convert -rest to /rest (but we'll just show rest without the slash for brevity)
	return encoded[1:]
}

// truncateWithEllipsis truncates a string to maxLen by taking the last N chars
func truncateWithEllipsis(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	// Take last (maxLen-3) chars, prefix with "..."
	suffixLen := maxLen - 3
	return "..." + s[len(s)-suffixLen:]
}

// resolveDisplayNameCollisions adds ~2, ~3, etc. suffixes when display names collide
func resolveDisplayNameCollisions(projects []models.ProjectInfo) {
	// Count occurrences of each display name
	counts := make(map[string]int)
	for _, p := range projects {
		counts[p.DisplayName]++
	}

	// For names that appear multiple times, add suffixes
	seen := make(map[string]int)
	for i := range projects {
		name := projects[i].DisplayName
		if counts[name] > 1 {
			seen[name]++
			if seen[name] > 1 {
				projects[i].DisplayName = name + "~" + strconv.Itoa(seen[name])
			}
		}
	}
}

// isDriveRoot checks if an encoded path represents a Windows drive root (e.g., "F--" for "F:\")
func isDriveRoot(encoded string) bool {
	return len(encoded) == 3 && isLetter(encoded[0]) && encoded[1] == '-' && encoded[2] == '-'
}

// generateUnixPathCandidates generates candidate paths for Unix systems
func generateUnixPathCandidates(encoded string) []string {
	// Remove leading dash if present
	if len(encoded) > 0 && encoded[0] == '-' {
		encoded = encoded[1:]
	}

	parts := strings.Split(encoded, "-")
	if len(parts) < 2 {
		return nil
	}

	// Build path by keeping last N segments joined with dashes
	// This handles cases like "source-claude-code-usage" -> "source/claude-code-usage"
	var candidates []string
	for joinFrom := len(parts) - 1; joinFrom >= 1; joinFrom-- {
		// Build path: /parts[0]/parts[1]/.../parts[joinFrom-1]/parts[joinFrom]-parts[joinFrom+1]-...
		prefix := "/" + strings.Join(parts[:joinFrom], "/")
		suffix := strings.Join(parts[joinFrom:], "-")
		candidate := prefix + "/" + suffix
		candidates = append(candidates, candidate)
	}

	return candidates
}

// generateWindowsPathCandidates generates candidate paths for Windows systems
// For "C--Users-Brian-source-foo-bar", tries:
//   - C:\Users\Brian\source\foo-bar
//   - C:\Users\Brian\source-foo-bar
//   - C:\Users\Brian-source-foo-bar
//   - etc.
//
// Also handles "--" patterns which may represent "\.folder" (dot-prefixed folders)
// since Claude Code encodes "." as "-" as well.
func generateWindowsPathCandidates(encoded string) []string {
	if len(encoded) < 4 {
		return nil
	}

	// Check for drive letter pattern: "X--..." (e.g., "C--Users-...")
	// The encoding produces C:\ → C-/ → C-- (colon to dash, then slash to dash)
	if !isLetter(encoded[0]) || encoded[1] != '-' || encoded[2] != '-' {
		return nil
	}

	driveLetter := string(encoded[0])
	rest := encoded[3:] // Skip "X--"

	var candidates []string

	// First, try with "--" interpreted as "\." (dot-prefixed folder)
	// e.g., "Users-Brian--config-yasb" → "Users\Brian\.config\yasb"
	if strings.Contains(rest, "--") {
		dotVariant := strings.ReplaceAll(rest, "--", "\\.")
		dotVariant = strings.ReplaceAll(dotVariant, "-", "\\")
		candidate := filepath.Clean(driveLetter + ":\\" + dotVariant)
		candidates = append(candidates, candidate)
	}

	parts := strings.Split(rest, "-")

	// Filter out empty parts (from "--" sequences)
	var filteredParts []string
	for _, p := range parts {
		if p != "" {
			filteredParts = append(filteredParts, p)
		}
	}

	if len(filteredParts) < 2 {
		return candidates // Return any dot-variant candidates we found
	}

	for joinFrom := len(filteredParts) - 1; joinFrom >= 1; joinFrom-- {
		prefix := driveLetter + ":\\" + strings.Join(filteredParts[:joinFrom], "\\")
		suffix := strings.Join(filteredParts[joinFrom:], "-")
		candidate := prefix + "\\" + suffix
		candidates = append(candidates, candidate)
	}

	return candidates
}

// DecodeProjectPath converts an encoded project directory name back to the original path
// e.g., "-home-bah-source-foo" -> "/home/bah/source/foo"
func DecodeProjectPath(encoded string) string {
	if encoded == "" {
		return ""
	}

	// Handle Windows-style paths (e.g., "C--Users-foo" -> "C:\Users\foo")
	if runtime.GOOS == "windows" {
		return decodeWindowsPath(encoded)
	}

	// Unix-style: "-home-bah-source-foo" -> "/home/bah/source/foo"
	// Leading dash becomes leading slash
	if strings.HasPrefix(encoded, "-") {
		encoded = "/" + encoded[1:]
	}
	// Remaining dashes become slashes
	return strings.ReplaceAll(encoded, "-", "/")
}

// decodeWindowsPath handles Windows-specific path decoding
// e.g., "C--Users-foo" -> "C:\Users\foo"
func decodeWindowsPath(encoded string) string {
	// Windows encoding produces: C:\ → C:/ → C-/ → C--
	// So "C--Users-foo-bar" decodes to "C:\Users\foo\bar" (naive, treating all dashes as separators)

	if len(encoded) < 3 {
		return encoded
	}

	// Check for drive letter pattern: "X--..." means "X:\..."
	// The double-dash comes from: colon→dash, then backslash→slash→dash
	if isLetter(encoded[0]) && encoded[1] == '-' && encoded[2] == '-' {
		driveLetter := string(encoded[0])
		rest := encoded[3:] // Skip "X--"
		// Replace remaining dashes with backslashes
		rest = strings.ReplaceAll(rest, "-", "\\")
		return driveLetter + ":\\" + rest
	}

	// Fallback: just replace dashes with backslashes
	return strings.ReplaceAll(encoded, "-", "\\")
}

func isLetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// HasSessions checks if a project directory contains any session files
func HasSessions(projectDir string) bool {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			return true
		}
	}
	return false
}
