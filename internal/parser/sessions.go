package parser

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bardisty/ccusage/internal/models"
)

const (
	// scannerInitialBufSize is the initial buffer size for the scanner (64KB).
	// This is large enough for most JSONL lines while being memory-efficient.
	scannerInitialBufSize = 64 * 1024

	// scannerMaxBufSize is the maximum buffer size for the scanner (10MB).
	// Claude Code session files can have very long lines due to base64-encoded
	// images and large tool outputs. 10MB handles sessions with large images.
	scannerMaxBufSize = 10 * 1024 * 1024

	// maxIndexFileSize is the maximum allowed size for sessions-index.json (10MB).
	// This prevents memory exhaustion from corrupted or malicious index files.
	maxIndexFileSize = 10 * 1024 * 1024
)

// ParseSessionsIndex parses a sessions-index.json file
func ParseSessionsIndex(path string) (*models.SessionsIndex, error) {
	// Check file size before reading to prevent memory exhaustion
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxIndexFileSize {
		return nil, fmt.Errorf("sessions-index.json too large (%d bytes, max %d)", info.Size(), maxIndexFileSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var index models.SessionsIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}

	return &index, nil
}

// GetLatestSession returns the most recently modified session entry
func GetLatestSession(index *models.SessionsIndex) *models.SessionEntry {
	if index == nil || len(index.Entries) == 0 {
		return nil
	}

	// We have at least one entry, safe to access entries[0] below
	// Sort by modified time descending
	entries := make([]models.SessionEntry, len(index.Entries))
	copy(entries, index.Entries)

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Modified.After(entries[j].Modified)
	})

	return &entries[0]
}

// GetSessionByID finds a session entry by its ID
func GetSessionByID(index *models.SessionsIndex, sessionID string) *models.SessionEntry {
	if index == nil {
		return nil
	}

	for _, entry := range index.Entries {
		if entry.SessionID == sessionID {
			return &entry
		}
	}

	return nil
}

// GetSessionsByModified returns sessions sorted by modified time (most recent first)
func GetSessionsByModified(index *models.SessionsIndex) []models.SessionEntry {
	if index == nil || len(index.Entries) == 0 {
		return nil
	}

	entries := make([]models.SessionEntry, len(index.Entries))
	copy(entries, index.Entries)

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Modified.After(entries[j].Modified)
	})

	return entries
}

// GetSessionsByCreated returns sessions sorted by creation time (most recent first)
func GetSessionsByCreated(index *models.SessionsIndex) []models.SessionEntry {
	if index == nil || len(index.Entries) == 0 {
		return nil
	}

	entries := make([]models.SessionEntry, len(index.Entries))
	copy(entries, index.Entries)

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Created.After(entries[j].Created)
	})

	return entries
}

// DiscoverSessionsFromDisk scans the project directory for .jsonl session files
// and builds SessionEntry records from file metadata
func DiscoverSessionsFromDisk(projectDir string) ([]models.SessionEntry, error) {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, err
	}

	var sessions []models.SessionEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		fullPath := filepath.Join(projectDir, entry.Name())

		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't stat
		}

		// Count messages by reading first pass of file
		// Treat -1 (error) as 0 for display purposes
		parentMsgCount := countMessagesInFile(fullPath)
		if parentMsgCount < 0 {
			parentMsgCount = 0
		}

		// Discover agent sub-sessions (ignore errors - missing subagents dir is common)
		agentPaths, err := DiscoverAgentSessions(projectDir, sessionID)
		if err != nil {
			// Log warning but continue - agent discovery failure shouldn't block session discovery
			// Note: NotExist errors are already handled inside DiscoverAgentSessions
			_ = err // Error intentionally ignored - subagent discovery is non-critical
		}

		// Count agent messages separately for display breakdown
		// Treat -1 (error) as 0 for individual files
		agentMsgCount := 0
		for _, agentPath := range agentPaths {
			count := countMessagesInFile(agentPath)
			if count > 0 {
				agentMsgCount += count
			}
		}

		sessions = append(sessions, models.SessionEntry{
			SessionID:         sessionID,
			FullPath:          fullPath,
			MessageCount:      parentMsgCount + agentMsgCount, // Total for consistency
			Created:           info.ModTime(),                 // Best approximation
			Modified:          info.ModTime(),
			AgentPaths:        agentPaths,
			AgentCount:        len(agentPaths),
			AgentMessageCount: agentMsgCount,
		})
	}

	return sessions, nil
}

// MergeSessionSources combines index entries with disk-discovered sessions
// Index entries take precedence for metadata (Created time, etc.)
// Returns merged list and count of orphaned sessions found on disk
func MergeSessionSources(index *models.SessionsIndex, diskSessions []models.SessionEntry) ([]models.SessionEntry, int) {
	// Build map from index for fast lookup
	indexMap := make(map[string]models.SessionEntry)
	if index != nil {
		for _, e := range index.Entries {
			indexMap[e.SessionID] = e
		}
	}

	// Build map from disk for agent info lookup
	diskMap := make(map[string]models.SessionEntry)
	for _, d := range diskSessions {
		diskMap[d.SessionID] = d
	}

	var merged []models.SessionEntry
	orphanCount := 0

	for _, disk := range diskSessions {
		if indexed, ok := indexMap[disk.SessionID]; ok {
			// Prefer index metadata but use disk values for paths and agent info
			indexed.FullPath = disk.FullPath
			indexed.AgentPaths = disk.AgentPaths
			indexed.AgentCount = disk.AgentCount
			indexed.AgentMessageCount = disk.AgentMessageCount
			// Use disk message count which includes agent messages for consistency
			indexed.MessageCount = disk.MessageCount
			// Use disk's Modified time (actual file mtime) instead of index's
			// The index may be stale, but the file mtime is always accurate
			indexed.Modified = disk.Modified
			merged = append(merged, indexed)
			delete(indexMap, disk.SessionID)
		} else {
			// Orphaned session - not in index
			orphanCount++
			merged = append(merged, disk)
		}
	}

	// Add index entries only if their files still exist on disk
	for _, e := range indexMap {
		if _, err := os.Stat(e.FullPath); err == nil {
			merged = append(merged, e)
		}
	}

	return merged, orphanCount
}

// DiscoverAgentSessions finds agent-*.jsonl files in {sessionID}/subagents/
func DiscoverAgentSessions(projectDir, sessionID string) ([]string, error) {
	subagentsDir := filepath.Join(projectDir, sessionID, "subagents")

	entries, err := os.ReadDir(subagentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No subagents directory - not an error
		}
		return nil, err
	}

	var agentPaths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "agent-") && strings.HasSuffix(name, ".jsonl") {
			agentPaths = append(agentPaths, filepath.Join(subagentsDir, name))
		}
	}

	return agentPaths, nil
}

// ExtractAgentID extracts the agent ID from an agent file path
// e.g., "agent-abc123.jsonl" -> "abc123"
func ExtractAgentID(agentPath string) string {
	base := filepath.Base(agentPath)
	// Remove "agent-" prefix and ".jsonl" suffix
	if strings.HasPrefix(base, "agent-") && strings.HasSuffix(base, ".jsonl") {
		return strings.TrimSuffix(strings.TrimPrefix(base, "agent-"), ".jsonl")
	}
	return base
}

// countMessagesInFile counts assistant messages in a JSONL file
// Returns -1 on error (file access, I/O) to distinguish from empty files (0)
// Returns partial count on buffer overflow (oversized lines are skipped)
func countMessagesInFile(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return -1
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, scannerInitialBufSize)
	scanner.Buffer(buf, scannerMaxBufSize)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg struct {
			Type    string           `json:"type"`
			Message *json.RawMessage `json:"message"`
		}
		if json.Unmarshal(line, &msg) == nil && msg.Type == "assistant" && msg.Message != nil {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return count // Return partial count for oversized lines
		}
		return -1 // I/O error
	}
	return count
}
