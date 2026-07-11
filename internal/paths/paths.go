package paths

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/bardisty/ficha/internal/models"
)

// ErrNoProjectFound indicates no matching project directory was found
var ErrNoProjectFound = errors.New("no matching project directory found")

// AmbiguousProjectError indicates multiple projects matched by basename
type AmbiguousProjectError struct {
	Basename string
	Matches  []models.ProjectInfo
}

func (e *AmbiguousProjectError) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ambiguous project name %q matches %d directories:\n", e.Basename, len(e.Matches)))
	for _, m := range e.Matches {
		sb.WriteString(fmt.Sprintf("  %s (original: %s)\n", m.EncodedPath, m.OriginalPath))
	}
	sb.WriteString("\nUse --project-dir to specify the exact directory")
	return sb.String()
}

// ProjectMatch contains the result of project directory matching
type ProjectMatch struct {
	ProjectDir  string // Full path to the matched project directory
	EncodedPath string // Encoded directory name
	MatchMethod string // "exact", "suffix", or "override"
	MatchInfo   string // Human-readable match info for logging
}

// GetClaudeConfigDir returns the path to the Claude config directory.
// Honors CLAUDE_CONFIG_DIR (Claude Code's own override), falling back to ~/.claude.
func GetClaudeConfigDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
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

// PathToProjectDir converts a filesystem path to the Claude project directory name.
// e.g., /home/user/project -> -home-user-project
// Note: encoding is lossy (e.g., underscores and slashes both become dashes),
// which mirrors Claude Code's own behavior. Collisions are possible but rare.
func PathToProjectDir(p string) string {
	p = filepath.Clean(p)
	// Replace all non-alphanumeric chars (except dash) with dashes.
	// Handles: / \ : _ (space) ( ) [ ] and any other special chars.
	//
	// Claude Code encodes with JS `/[^a-zA-Z0-9]/g`, which replaces per UTF-16
	// code unit, so an astral-plane character (a surrogate pair) becomes TWO
	// dashes. A naive per-rune replacement emits one dash and the encoded names
	// diverge, so the exact-directory match misses. Mirror the JS behavior by
	// emitting one dash per UTF-16 code unit of each replaced rune. ASCII/BMP
	// characters are a single code unit, so their encoding is unchanged.
	var b strings.Builder
	b.Grow(len(p))
	for _, r := range p {
		if isEncodingPreserved(r) {
			b.WriteRune(r)
			continue
		}
		// utf16.RuneLen returns -1 for an unencodable rune; emit one dash then.
		b.WriteString(strings.Repeat("-", max(1, utf16.RuneLen(r))))
	}
	return b.String()
}

// isEncodingPreserved reports whether a rune passes through PathToProjectDir
// unchanged: the characters excluded from Claude Code's [^a-zA-Z0-9-] class
// (dash is kept so existing dashes survive).
func isEncodingPreserved(r rune) bool {
	return r == '-' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// BasenameCrossOS returns the final element of p, treating both '/' and '\'
// as separators and stripping a leading Windows drive prefix ("C:"). On Linux
// filepath.Base does not split on backslash, so a Windows-style originalPath
// (e.g. a WSL user sharing a Windows Claude config dir) would otherwise never
// match by basename. Normalizing first makes the result identical on every OS.
func BasenameCrossOS(p string) string {
	if len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0]) {
		p = p[2:] // strip drive prefix so "C:" isn't mistaken for content
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" {
		return "."
	}
	return path.Base(p)
}

func isASCIILetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
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
		// Fall back to uncanonicalized path for any error:
		// ENOENT, ELOOP (circular symlinks), EACCES (permission denied), etc.
		return path, nil
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

// FindProjectDir finds the Claude project directory for a given path.
// It tries exact match first, then falls back to basename matching.
// allProjects should be obtained from parser.DiscoverAllProjects().
func FindProjectDir(path string, allProjects []models.ProjectInfo) (*ProjectMatch, error) {
	projectsDir, err := GetProjectsDir()
	if err != nil {
		return nil, err
	}

	// Canonicalize the path
	canonicalPath, err := CanonicalizePath(path)
	if err != nil {
		return nil, err
	}

	encodedPath := PathToProjectDir(canonicalPath)
	exactDir := filepath.Join(projectsDir, encodedPath)

	// Try exact match first (duplicated in cmd/common.go resolveProjectDirectory for perf)
	if info, err := os.Stat(exactDir); err == nil && info.IsDir() {
		return &ProjectMatch{
			ProjectDir:  exactDir,
			EncodedPath: encodedPath,
			MatchMethod: "exact",
			MatchInfo:   "",
		}, nil
	}

	// Fall back to basename matching
	basename := filepath.Base(canonicalPath)
	var matches []models.ProjectInfo

	for _, proj := range allProjects {
		// Match by basename of originalPath. BasenameCrossOS handles
		// Windows-style originalPath values (backslash separators, drive
		// prefix) that filepath.Base would not split on Linux/WSL.
		if proj.OriginalPath != "" {
			projBasename := BasenameCrossOS(proj.OriginalPath)
			if strings.EqualFold(projBasename, basename) {
				matches = append(matches, proj)
			}
		}
	}

	if len(matches) == 0 {
		return nil, ErrNoProjectFound
	}

	if len(matches) > 1 {
		return nil, &AmbiguousProjectError{
			Basename: basename,
			Matches:  matches,
		}
	}

	// Single match - return it with info
	match := matches[0]
	return &ProjectMatch{
		ProjectDir:  match.FullPath,
		EncodedPath: match.EncodedPath,
		MatchMethod: "suffix",
		MatchInfo:   fmt.Sprintf("matched by name '%s' (original: %s)", basename, match.OriginalPath),
	}, nil
}

// ResolveProjectDir resolves a --project-dir flag value to a full project directory path.
// The value can be either an encoded directory name or a full path.
func ResolveProjectDir(projectDirFlag string) (string, error) {
	var fullPath string

	if filepath.IsAbs(projectDirFlag) {
		// Absolute path - use directly but validate it exists
		fullPath = projectDirFlag
	} else if looksLikePath(projectDirFlag) {
		// Relative path - convert to absolute before validating
		absPath, err := filepath.Abs(projectDirFlag)
		if err != nil {
			return "", err
		}
		fullPath = absPath
	} else {
		// Assume it's an encoded directory name
		projectsDir, err := GetProjectsDir()
		if err != nil {
			return "", err
		}
		fullPath = filepath.Join(projectsDir, projectDirFlag)
	}

	// Verify it exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("project directory not found: %s", projectDirFlag)
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", fullPath)
	}

	return fullPath, nil
}

// looksLikePath reports whether a --project-dir value is a filesystem path
// rather than an encoded project directory name. Encoded names are a flat
// token like "-home-user-foo" that never contains a path separator, so any
// value carrying one — either '/' or '\' — or a Windows drive/volume prefix
// is a path. Recognizing '\' and drive-relative "C:proj" (via VolumeName)
// keeps Windows-style relative paths out of the encoded-name branch, where
// they would be joined under ~/.claude/projects and never resolve.
func looksLikePath(v string) bool {
	return v == "." || v == ".." ||
		strings.ContainsAny(v, `/\`) ||
		filepath.VolumeName(v) != ""
}
