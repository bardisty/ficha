package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// agentLine builds one assistant JSONL line with the given output_tokens.
// Callers pass equal-digit-width values (e.g. 500 and 900) so lines differ in
// content but not byte length — letting a test change an agent file's content
// without changing its size, to isolate the cache's mtime+size identity check.
// (Values are not zero-padded: JSON forbids leading zeros in numbers.)
func agentLine(outputTokens int) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":"2024-01-01T00:01:00Z","message":{"id":"a1","model":"claude-haiku-4-5","usage":{"input_tokens":1000,"output_tokens":%d}}}`, outputTokens)
}

// writeCacheFixture writes a parent session plus one agent sub-session and
// returns the parent path and the agent file path.
func writeCacheFixture(t *testing.T, dir, sessionID, agentContent string) (sessionPath, agentPath string) {
	t.Helper()
	sessionPath = filepath.Join(dir, sessionID+".jsonl")
	parent := `{"type":"assistant","timestamp":"2024-01-01T00:00:00Z","message":{"id":"p1","model":"claude-haiku-4-5","usage":{"input_tokens":100,"output_tokens":100}}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(parent), 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, sessionID, "subagents")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	agentPath = filepath.Join(sub, "agent-a1.jsonl")
	if err := os.WriteFile(agentPath, []byte(agentContent+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return sessionPath, agentPath
}

// TestAgentParseCache_ServesUnchangedAgent verifies the cache serves an
// unchanged agent from memory (skipping the re-parse) and re-parses once the
// file's identity changes.
func TestAgentParseCache_ServesUnchangedAgent(t *testing.T) {
	dir := t.TempDir()
	sessionID := "cache-sess"

	contentA := agentLine(500)
	sessionPath, agentPath := writeCacheFixture(t, dir, sessionID, contentA)

	cache := NewAgentParseCache()

	first, err := AnalyzeSessionWithCache(sessionPath, sessionID, NoMessages, cache)
	if err != nil {
		t.Fatalf("first analyze: %v", err)
	}
	costA := first.AgentsCost.TotalCost
	if costA <= 0 {
		t.Fatalf("expected non-zero agent cost, got %v", costA)
	}

	// Overwrite the agent with a different output_tokens value of the SAME byte
	// length, then restore the original mtime so the file identity is unchanged.
	info, err := os.Stat(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	contentB := agentLine(900) + "\n"
	if len(contentB) != len(contentA)+1 {
		t.Fatalf("fixture byte lengths differ (A=%d B=%d); test cannot isolate identity", len(contentA)+1, len(contentB))
	}
	if err := os.WriteFile(agentPath, []byte(contentB), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(agentPath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	// Same mtime + size => cache hit => still the stale content-A cost, proving
	// the agent was not re-parsed.
	second, err := AnalyzeSessionWithCache(sessionPath, sessionID, NoMessages, cache)
	if err != nil {
		t.Fatalf("second analyze: %v", err)
	}
	if second.AgentsCost.TotalCost != costA {
		t.Errorf("cache miss on unchanged agent: got %v, want cached %v (agent should not have been re-parsed)",
			second.AgentsCost.TotalCost, costA)
	}

	// Bump the mtime => cache invalidated => re-parse => content-B cost.
	newer := info.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(agentPath, newer, newer); err != nil {
		t.Fatal(err)
	}
	third, err := AnalyzeSessionWithCache(sessionPath, sessionID, NoMessages, cache)
	if err != nil {
		t.Fatalf("third analyze: %v", err)
	}
	if third.AgentsCost.TotalCost == costA {
		t.Errorf("cache not invalidated after mtime change: still serving stale cost %v", costA)
	}
}

// TestAgentParseCache_NilMatchesUncached verifies AnalyzeSessionWithCache(nil)
// produces the same result as the plain AnalyzeSession entry point.
func TestAgentParseCache_NilMatchesUncached(t *testing.T) {
	dir := t.TempDir()
	sessionID := "cache-nil"
	sessionPath, _ := writeCacheFixture(t, dir, sessionID, agentLine(500))

	uncached, err := AnalyzeSession(sessionPath, sessionID, NoMessages)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	nilCache, err := AnalyzeSessionWithCache(sessionPath, sessionID, NoMessages, nil)
	if err != nil {
		t.Fatalf("AnalyzeSessionWithCache(nil): %v", err)
	}
	if uncached.TotalCost.TotalCost != nilCache.TotalCost.TotalCost {
		t.Errorf("nil cache diverged: got %v, want %v", nilCache.TotalCost.TotalCost, uncached.TotalCost.TotalCost)
	}
}

// TestAgentParseCache_ConcurrentUse hammers a shared cache from many goroutines
// to surface data races (run under -race). bubbletea reloads run in goroutines
// and may briefly overlap, so the cache must tolerate concurrent access.
func TestAgentParseCache_ConcurrentUse(t *testing.T) {
	dir := t.TempDir()
	sessionID := "cache-concurrent"
	sessionPath, _ := writeCacheFixture(t, dir, sessionID, agentLine(500))
	cache := NewAgentParseCache()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := AnalyzeSessionWithCache(sessionPath, sessionID, NoMessages, cache); err != nil {
				t.Errorf("concurrent analyze failed: %v", err)
			}
		}()
	}
	wg.Wait()
}
