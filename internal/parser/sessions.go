package parser

import (
	"encoding/json"
	"fmt"
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
//
// The skip counters have three sources: unreadable agent directories (found by
// discovery, which always runs), and — only when the scan runs — agent files
// and the parent file that cannot be read. With countMessages=false the scan is
// skipped, so only the directory count is populated; harmless, because the
// analysis paths that pass false detect unreadable files themselves.
//
// AgentCount, like MessageCount, describes what the scan could read: an agent
// file it could not open lands in SkippedAgents instead, so AgentCount +
// SkippedAgents recovers what is on disk. Without the scan AgentCount is the
// discovery count, which is the best the listing alone can know.
func buildDiskEntry(projectDir, sessionID, fullPath string, modTime time.Time, countMessages bool) models.SessionEntry {
	agentPaths, unreadableDirs := DiscoverAgentSessions(projectDir, sessionID)
	skippedAgents := unreadableDirs

	// Count parent + agent messages for display. countMessagesInFile returns a
	// negative count on file access / I/O error.
	parentMsgCount := 0
	agentMsgCount := 0
	skippedLines := 0
	skippedSessions := 0
	readableAgents := len(agentPaths)
	if countMessages {
		if c, skipped := countMessagesInFile(fullPath); c >= 0 {
			parentMsgCount = c
			skippedLines += skipped
		} else {
			// The session's own transcript is unreadable: its row would show a
			// zero message count that looks like an empty session.
			skippedSessions = 1
		}
		for _, agentPath := range agentPaths {
			c, skipped := countMessagesInFile(agentPath)
			if c < 0 {
				skippedAgents++
				readableAgents--
				continue
			}
			agentMsgCount += c
			skippedLines += skipped
		}
	}

	return models.SessionEntry{
		SessionID:         sessionID,
		FullPath:          fullPath,
		MessageCount:      parentMsgCount + agentMsgCount, // Total for consistency
		Created:           modTime,                        // Best approximation
		Modified:          modTime,
		AgentPaths:        agentPaths,
		AgentCount:        readableAgents,
		AgentMessageCount: agentMsgCount,
		SkippedSessions:   skippedSessions,
		SkippedAgents:     skippedAgents,
		SkippedLines:      skippedLines,
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
			indexed.SkippedSessions = disk.SkippedSessions
			indexed.SkippedAgents = disk.SkippedAgents
			indexed.SkippedLines = disk.SkippedLines
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
// and, for workflow runs, in {sessionID}/subagents/workflows/{runID}/.
// Regular agents come first, then workflow runs alphabetically. The name
// filter excludes each run's journal.jsonl and agent-*.meta.json files.
//
// unreadableDirs counts directories that exist but could not be listed
// (permissions, or a plain file where a directory was expected). Their agent
// files are missing from the returned paths, so the caller must fold the count
// into its skipped-agent accounting rather than report a complete result. An
// absent directory is normal (a session without agents) and counts nothing.
//
// The count is a lower bound on the agents lost: an unreadable directory hides
// however many agent files it held, and one that held none still counts 1.
// There is no way to do better without reading it — a loud undercount beats
// silence.
func DiscoverAgentSessions(projectDir, sessionID string) (paths []string, unreadableDirs int) {
	subagentsDir := filepath.Join(projectDir, sessionID, "subagents")
	entries, unreadable := readAgentDir(subagentsDir)
	if unreadable > 0 {
		// Nothing beneath an unlistable subagents/ can be read either, so the
		// whole subtree counts once rather than once per directory in it.
		return collectAgentFiles(subagentsDir, entries), unreadable
	}
	agentPaths := collectAgentFiles(subagentsDir, entries)

	workflowsDir := filepath.Join(subagentsDir, "workflows")
	runDirs, unreadable := readAgentDir(workflowsDir)
	if unreadable > 0 {
		return agentPaths, unreadable
	}
	for _, run := range runDirs {
		if !run.IsDir() {
			continue
		}
		runDir := filepath.Join(workflowsDir, run.Name())
		runEntries, unreadable := readAgentDir(runDir)
		unreadableDirs += unreadable
		agentPaths = append(agentPaths, collectAgentFiles(runDir, runEntries)...)
	}

	return agentPaths, unreadableDirs
}

// readAgentDir lists dir, separating "it isn't there" from "it's there and I
// can't read it". Only the latter hides agent files, so only it counts. A run
// directory can also vanish between the parent listing and this call — the live
// TUIs rescan directories Claude Code is still writing — and a directory that
// no longer exists hides nothing.
//
// os.ReadDir returns the entries it managed to read alongside the error, so a
// partially-readable directory still contributes the agents it named.
func readAgentDir(dir string) (entries []os.DirEntry, unreadable int) {
	entries, err := os.ReadDir(dir)
	if err == nil || os.IsNotExist(err) {
		return entries, 0
	}
	return entries, 1
}

// collectAgentFiles returns full paths of agent-*.jsonl files among entries.
func collectAgentFiles(dir string, entries []os.DirEntry) []string {
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "agent-") && strings.HasSuffix(name, ".jsonl") {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths
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

// ExtractWorkflowRunID returns the workflow run ID for agent paths under
// subagents/workflows/{runID}/, or "" for regular subagent paths.
func ExtractWorkflowRunID(agentPath string) string {
	runDir := filepath.Dir(agentPath)
	parent := filepath.Dir(runDir)
	if filepath.Base(parent) == "workflows" && filepath.Base(filepath.Dir(parent)) == "subagents" {
		return filepath.Base(runDir)
	}
	return ""
}

// ParseWorkflowMeta reads {projectDir}/{sessionID}/workflows/{runID}.json for
// display metadata. Missing, oversized, or malformed files yield a runID-only
// result with ok=false — orphan run dirs still analyze and display.
func ParseWorkflowMeta(projectDir, sessionID, runID string) (models.WorkflowMeta, bool) {
	meta := models.WorkflowMeta{RunID: runID}

	path := filepath.Join(projectDir, sessionID, "workflows", runID+".json")
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxIndexFileSize {
		return meta, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return meta, false
	}

	// The file also carries the full script text and logs; decode only what
	// display needs.
	var raw struct {
		WorkflowName string `json:"workflowName"`
		Status       string `json:"status"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return meta, false
	}

	meta.Name = raw.WorkflowName
	meta.Status = raw.Status
	return meta, true
}

// countMessagesInFile counts the assistant messages a JSONL file contributes to
// an analysis, and how many of its lines the parse rejected. It runs the very
// parse the analysis paths run, so the two can never disagree: a line dropped
// there (malformed JSON, unparseable timestamp, non-integer token count,
// oversized) is dropped here and counted in skippedLines, and streaming lines
// repeating the same message.id + requestId collapse to one message.
//
// Deriving the count from a cheaper, laxer decode is what made `list` report
// more messages than `show` analyzed. Keep this delegating to the real parser.
//
// count is -1 on file access / I/O error, distinguishing failure from an empty
// file (0).
func countMessagesInFile(path string) (count, skippedLines int) {
	result, err := ParseJSONLFileWithResult(path)
	if err != nil {
		return -1, 0
	}
	return len(result.Messages), result.SkippedLines
}
