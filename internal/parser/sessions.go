package parser

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

const (
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

// DiscoverSessionsFromDisk scans the project directory for .jsonl session files
// and builds SessionEntry records from file metadata. countMessages controls
// whether each file is scanned for its message count: only `list` displays
// discovery-time counts, so analysis paths pass false to skip the scan (they
// recompute counts from their own parse — see buildDiskEntry).
func DiscoverSessionsFromDisk(projectDir string, countMessages bool) ([]models.SessionEntry, error) {
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

		sessions = append(sessions, buildDiskEntry(projectDir, sessionID, fullPath, info.ModTime(), countMessages))
	}

	return sessions, nil
}

// buildDiskEntry builds a SessionEntry for a session file on disk, discovering
// its agent sub-sessions and (when countMessages is true) counting messages.
//
// Message counting fully scans every parent and agent file. That cost is wasted
// on analysis paths, which re-parse the same files and recompute counts anyway,
// so they pass countMessages=false and leave the counts zero. Agent discovery
// is a cheap directory listing and always runs.
func buildDiskEntry(projectDir, sessionID, fullPath string, modTime time.Time, countMessages bool) models.SessionEntry {
	// Discover agent sub-sessions (ignore errors - missing subagents dir is common)
	agentPaths, err := DiscoverAgentSessions(projectDir, sessionID)
	if err != nil {
		// Log warning but continue - agent discovery failure shouldn't block session discovery
		// Note: NotExist errors are already handled inside DiscoverAgentSessions
		_ = err // Error intentionally ignored - subagent discovery is non-critical
	}

	// Count parent + agent messages for display. countMessagesInFile returns -1
	// on error; treat that (and empty files) as 0.
	parentMsgCount := 0
	agentMsgCount := 0
	if countMessages {
		if c := countMessagesInFile(fullPath); c > 0 {
			parentMsgCount = c
		}
		for _, agentPath := range agentPaths {
			if c := countMessagesInFile(agentPath); c > 0 {
				agentMsgCount += c
			}
		}
	}

	return models.SessionEntry{
		SessionID:         sessionID,
		FullPath:          fullPath,
		MessageCount:      parentMsgCount + agentMsgCount, // Total for consistency
		Created:           modTime,                        // Best approximation
		Modified:          modTime,
		AgentPaths:        agentPaths,
		AgentCount:        len(agentPaths),
		AgentMessageCount: agentMsgCount,
	}
}

// MergeSessionSources combines index entries with disk-discovered sessions.
// Index entries take precedence for metadata (Created time, etc.).
// Index-only entries (present in the index but missed by the disk scan) are
// kept only if their FullPath resolves inside projectDir and still exists;
// their message counts and agent info are recomputed from disk because the
// index may be stale. countMessages is forwarded to that rebuild so analysis
// paths skip the message-count scan (see buildDiskEntry). Returns merged list
// and count of orphaned sessions found on disk.
func MergeSessionSources(index *models.SessionsIndex, diskSessions []models.SessionEntry, projectDir string, countMessages bool) ([]models.SessionEntry, int) {
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

	// Add index-only entries whose files still exist inside projectDir. The
	// index's FullPath is untrusted (may point outside the project) and its
	// counts may be stale, so validate the path and rebuild from disk.
	for _, e := range indexMap {
		if !pathWithinDir(e.FullPath, projectDir) {
			continue
		}
		info, err := os.Stat(e.FullPath)
		if err != nil || info.IsDir() {
			continue
		}
		rebuilt := buildDiskEntry(projectDir, e.SessionID, e.FullPath, info.ModTime(), countMessages)
		// Index metadata still takes precedence, as in the matched branch above
		rebuilt.Created = e.Created
		rebuilt.ProjectPath = e.ProjectPath
		merged = append(merged, rebuilt)
	}

	return merged, orphanCount
}

// pathWithinDir reports whether path resolves lexically inside dir. Symlinks
// are not resolved — this guards against index entries that plainly point
// outside the project directory, not against adversarial filesystems.
func pathWithinDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

// countMessagesInFile counts distinct assistant messages in a JSONL file.
// Streaming lines repeating the same message.id + requestId count once, so
// the count matches the deduplicated analysis (see DeduplicateMessages).
// Returns -1 on error (file access, I/O) to distinguish from empty files (0).
// Oversized lines are skipped individually; counting continues after them,
// matching ParseJSONLWithResult.
func countMessagesInFile(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return -1
	}
	defer file.Close()

	count := 0 // lines without a message id — never collapsed
	seen := make(map[string]struct{})
	reader := bufio.NewReaderSize(file, readerBufSize)

	for {
		line, oversized, err := readLine(reader, maxLineBytes)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return -1 // I/O error
		}
		if oversized || len(line) == 0 {
			continue
		}
		var msg struct {
			Type      string `json:"type"`
			RequestID string `json:"requestId"`
			Message   *struct {
				ID string `json:"id"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &msg) == nil && msg.Type == "assistant" && msg.Message != nil {
			if msg.Message.ID == "" {
				count++
			} else {
				seen[msg.Message.ID+":"+msg.RequestID] = struct{}{}
			}
		}
	}
	return count + len(seen)
}
