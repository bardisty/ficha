package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
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

// parentLine builds one parent assistant line. Lines of one id are a
// response's streaming lines, which the parse collapses to the last.
func parentLine(id string, minute, outputTokens int) string {
	return fmt.Sprintf(`{"type":"assistant","requestId":"req_%s","timestamp":"2024-01-01T00:%02d:00Z","message":{"id":"msg_%s","model":"claude-haiku-4-5","usage":{"input_tokens":1000,"output_tokens":%d,"cache_creation_input_tokens":300,"cache_creation":{"ephemeral_5m_input_tokens":100,"ephemeral_1h_input_tokens":200}}}}`,
		id, minute, id, outputTokens)
}

// countParentParses counts parses of sessionPath made through the cache's
// loader until the test ends.
func countParentParses(t *testing.T, sessionPath string) *int {
	t.Helper()
	var mu sync.Mutex
	count := 0
	original := parseTranscript
	parseTranscript = func(path string) (*parser.ParseResult, error) {
		if path == sessionPath {
			mu.Lock()
			count++
			mu.Unlock()
		}
		return original(path)
	}
	t.Cleanup(func() { parseTranscript = original })
	return &count
}

// writeParentFixture is writeCacheFixture with a parent of several messages,
// one of them written as two streaming lines.
func writeParentFixture(t *testing.T, dir, sessionID string) (sessionPath, agentPath string) {
	t.Helper()
	sessionPath, agentPath = writeCacheFixture(t, dir, sessionID, agentLine(500))
	parent := parentLine("p1", 0, 100) + "\n" + parentLine("p2", 2, 50) + "\n" + parentLine("p2", 2, 250) + "\n" + parentLine("p3", 3, 300) + "\n"
	if err := os.WriteFile(sessionPath, []byte(parent), 0o644); err != nil {
		t.Fatal(err)
	}
	return sessionPath, agentPath
}

// sameAsFreshParse fails unless a load through cache gives exactly what a
// load without one does, for both of the live views' entry points.
func sameAsFreshParse(t *testing.T, label, sessionPath, sessionID string, cache *AgentParseCache) {
	t.Helper()
	got, err := AnalyzeSessionWithCache(sessionPath, sessionID, AllMessages, cache)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	want, err := AnalyzeSession(sessionPath, sessionID, AllMessages)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	sameFields(t, label+": analysis", got, want)
	gotRows, err := GetBreakdownMessagesWithCache(sessionPath, sessionID, cache)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	wantRows, err := GetBreakdownMessages(sessionPath, sessionID)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	sameFields(t, label+": breakdown", gotRows, wantRows)
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

// A reload set off by an agent write must not parse the parent again, and a
// write to the parent must.
func TestParseCacheKeepsParentAcrossAgentWrites(t *testing.T) {
	dir := t.TempDir()
	sessionID := "cache-parent"
	sessionPath, agentPath := writeParentFixture(t, dir, sessionID)
	parses := countParentParses(t, sessionPath)
	cache := NewAgentParseCache()
	load := func() *models.SessionAnalysis {
		t.Helper()
		analysis, err := AnalyzeSessionWithCache(sessionPath, sessionID, NoMessages, cache)
		if err != nil {
			t.Fatal(err)
		}
		return analysis
	}

	first := load()
	if *parses != 1 {
		t.Fatalf("first load: %d parent parses, want 1", *parses)
	}

	appendLine(t, agentPath, strings.Replace(agentLine(700), `"id":"a1"`, `"id":"a2"`, 1))
	second := load()
	if *parses != 1 {
		t.Errorf("after an agent write: %d parent parses, want 1", *parses)
	}
	if second.AgentMessageCount != first.AgentMessageCount+1 || second.ParentMessageCount != first.ParentMessageCount {
		t.Errorf("after an agent write: %d agent and %d parent messages, want %d and %d",
			second.AgentMessageCount, second.ParentMessageCount, first.AgentMessageCount+1, first.ParentMessageCount)
	}

	appendLine(t, sessionPath, parentLine("p4", 4, 400))
	third := load()
	if *parses != 2 {
		t.Errorf("after a parent append: %d parent parses, want 2", *parses)
	}
	if third.ParentMessageCount != first.ParentMessageCount+1 {
		t.Errorf("after a parent append: %d parent messages, want %d", third.ParentMessageCount, first.ParentMessageCount+1)
	}

	*parses = 0
	sameAsFreshParse(t, "after both writes", sessionPath, sessionID, cache)
	if *parses != 2 {
		// Only the two loads without a cache parse.
		t.Errorf("unchanged files: %d parent parses, want the 2 uncached ones", *parses)
	}
}

// A file swapped in at the same path is a different file even when its size
// and mtime match the one it replaced.
func TestParseCacheReparsesAReplacedParent(t *testing.T) {
	dir := t.TempDir()
	sessionID := "cache-replace"
	sessionPath, _ := writeParentFixture(t, dir, sessionID)
	parses := countParentParses(t, sessionPath)
	cache := NewAgentParseCache()

	before, err := AnalyzeSessionWithCache(sessionPath, sessionID, NoMessages, cache)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	// 300 and 900 output tokens: another cost at the same length.
	replaced := strings.Replace(string(content), `"output_tokens":300,`, `"output_tokens":900,`, 1)
	if replaced == string(content) || len(replaced) != len(content) {
		t.Fatal("the replacement must differ in content and not in size")
	}
	tmp := sessionPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(replaced), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, sessionPath); err != nil {
		t.Fatal(err)
	}

	after, err := AnalyzeSessionWithCache(sessionPath, sessionID, NoMessages, cache)
	if err != nil {
		t.Fatal(err)
	}
	if *parses != 2 {
		t.Errorf("%d parent parses, want 2", *parses)
	}
	if after.ParentCost.TotalCost <= before.ParentCost.TotalCost {
		t.Errorf("parent cost %v should have grown from %v", after.ParentCost.TotalCost, before.ParentCost.TotalCost)
	}
}

// Every load that hits the cache shares one parse of the parent. No load may
// change it: overlapping loads of both views, repeated, must each match a
// fresh parse. Run under -race.
func TestParseCacheSharedParentIsNeverMutated(t *testing.T) {
	dir := t.TempDir()
	sessionID := "cache-shared"
	sessionPath, _ := writeParentFixture(t, dir, sessionID)
	cache := NewAgentParseCache()

	sameAsFreshParse(t, "first load", sessionPath, sessionID, cache)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			analysis, err := AnalyzeSessionWithCache(sessionPath, sessionID, AllMessages, cache)
			if err != nil {
				t.Errorf("analyze: %v", err)
				return
			}
			// What a view does with its own result must stay its own.
			for i := range analysis.Messages {
				analysis.Messages[i].Usage.OutputTokens = -1
				if cc := analysis.Messages[i].Usage.CacheCreation; cc != nil {
					cc.Ephemeral1hInputTokens = -1
				}
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := GetBreakdownMessagesWithCache(sessionPath, sessionID, cache); err != nil {
				t.Errorf("breakdown: %v", err)
			}
		}()
	}
	wg.Wait()
	sameAsFreshParse(t, "after overlapping loads", sessionPath, sessionID, cache)
}
