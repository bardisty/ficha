package analyzer

import (
	"os"
	"sync"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
)

// AgentParseCache memoizes parsed, cost-annotated agent sub-session messages
// keyed by file path plus identity (mtime + size).
//
// Live TUIs re-run analysis on every write to the parent session file. Agent
// sub-session files are written once and rarely change afterward, so without a
// cache every reload re-reads and re-parses each agent — O(agents) wasted work
// per keystroke-driven update. Caching their parse makes reloads scale with the
// parent file alone.
//
// Safe for concurrent use: bubbletea runs commands in goroutines, and several
// reloads may briefly overlap (e.g. a manual refresh during a pending load). A
// nil *AgentParseCache disables caching (every call parses), which is the
// behavior of the plain AnalyzeSession / GetBreakdownMessages entry points.
type AgentParseCache struct {
	mu      sync.Mutex
	entries map[string]agentParseEntry
}

// agentParseEntry is a cached parse keyed by file identity. messages are
// already cost-annotated; consumers read them without mutating.
type agentParseEntry struct {
	modTime  time.Time
	size     int64
	messages []models.MessageAnalysis
	skips    models.FileSkips
}

// NewAgentParseCache returns an empty, ready-to-use cache.
func NewAgentParseCache() *AgentParseCache {
	return &AgentParseCache{entries: make(map[string]agentParseEntry)}
}

// lookup returns the cached parse for path if the file identity (mtime + size)
// still matches; ok is false on a miss or a changed file.
func (c *AgentParseCache) lookup(path string, modTime time.Time, size int64) (messages []models.MessageAnalysis, skips models.FileSkips, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, found := c.entries[path]
	if found && e.size == size && e.modTime.Equal(modTime) {
		return e.messages, e.skips, true
	}
	return nil, models.FileSkips{}, false
}

// store records a parse under its file identity, overwriting any stale entry.
func (c *AgentParseCache) store(path string, modTime time.Time, size int64, messages []models.MessageAnalysis, skips models.FileSkips) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[path] = agentParseEntry{modTime: modTime, size: size, messages: messages, skips: skips}
}

// loadAgentMessages parses an agent file into cost-annotated messages plus its
// skipped lines. With a non-nil cache, an unchanged file (same mtime +
// size) is served from memory instead of re-parsed; a changed or absent cache
// entry is parsed and stored. A nil cache always parses.
//
// The returned slice is shared with the cache — callers must never mutate an
// element in place. The AllMessages export path tags each message with its
// AgentID, so it copies (and deep-copies Usage.CacheCreation) before writing.
func loadAgentMessages(agentPath string, cache *AgentParseCache) ([]models.MessageAnalysis, models.FileSkips, error) {
	if cache != nil {
		// Stat outside the lock; only the map operations are serialized. Parsing
		// happens outside the lock too, so concurrent misses on distinct files
		// don't block each other (two misses on the same file just parse twice
		// and both store the same result — correct, if briefly redundant).
		if info, err := os.Stat(agentPath); err == nil {
			if messages, skipped, ok := cache.lookup(agentPath, info.ModTime(), info.Size()); ok {
				return messages, skipped, nil
			}
			messages, skipped, perr := parseAgentMessages(agentPath)
			if perr != nil {
				return nil, models.FileSkips{}, perr
			}
			cache.store(agentPath, info.ModTime(), info.Size(), messages, skipped)
			return messages, skipped, nil
		}
		// Stat failed — fall through to a direct parse so the error surfaces the
		// same way as the uncached path.
	}
	return parseAgentMessages(agentPath)
}

// parseAgentMessages parses an agent JSONL file and returns its cost-annotated
// messages plus its skipped (malformed or oversized) lines.
func parseAgentMessages(agentPath string) ([]models.MessageAnalysis, models.FileSkips, error) {
	result, err := parser.ParseJSONLFileWithResult(agentPath)
	if err != nil {
		return nil, models.FileSkips{}, err
	}
	messageAnalyses := parser.ExtractUsageFromMessages(result.Messages)
	for i := range messageAnalyses {
		CalculateMessageCost(&messageAnalyses[i])
	}
	return messageAnalyses, fileSkips(agentPath, parser.ExtractAgentID(agentPath), result), nil
}

// fileSkips is what -v reports about one parsed transcript.
func fileSkips(path, agentID string, result *parser.ParseResult) models.FileSkips {
	return models.FileSkips{Path: path, AgentID: agentID, Count: result.SkippedLines, Lines: result.SkippedAt}
}
