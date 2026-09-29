package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// A windowed report skips a session whose files were all last written a day
// or more before --since, without parsing it. These tests pin what that
// shortcut may change (the skip counters, for files it no longer opens) and
// what it must not (totals, results, errors).

const (
	// Written inside the window used below, which starts on Jan 17.
	windowMsg = `{"type":"assistant","requestId":"req_5","timestamp":"2024-01-17T09:00:00Z","message":{"id":"msg_5","model":"claude-sonnet-4-5","usage":{"input_tokens":3000,"output_tokens":100}}}`
	// An agent line inside the window.
	windowAgentMsg = `{"type":"assistant","requestId":"req_6","timestamp":"2024-01-17T10:00:00Z","message":{"id":"msg_6","model":"claude-sonnet-4-5","usage":{"input_tokens":700,"output_tokens":70}}}`
	corruptLine    = `{"type":"assistant","timestamp":`
)

var (
	skipWindow = models.TimeWindow{Since: time.Date(2024, 1, 17, 0, 0, 0, 0, time.UTC)}
	// Before skipWindow.Since minus a day, so the shortcut applies.
	oldMtime = time.Date(2024, 1, 15, 10, 10, 0, 0, time.UTC)
	// Inside the window, so the shortcut can't apply.
	newMtime = time.Date(2024, 1, 17, 9, 0, 0, 0, time.UTC)
)

func setMtime(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// diskEntry builds the entry discovery would: Modified is the file's mtime.
func diskEntry(t *testing.T, path string) models.SessionEntry {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return models.SessionEntry{
		SessionID: strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		FullPath:  path,
		Modified:  info.ModTime(),
		Created:   info.ModTime(),
	}
}

func diskEntries(t *testing.T, paths ...string) []models.SessionEntry {
	t.Helper()
	var entries []models.SessionEntry
	for _, p := range paths {
		entries = append(entries, diskEntry(t, p))
	}
	return entries
}

// sameTotals fails unless a and b count the same messages at the same cost.
func sameTotals(t *testing.T, label string, a, b *models.SessionAnalysis) {
	t.Helper()
	if a.MessageCount != b.MessageCount || a.SessionCount != b.SessionCount ||
		a.TotalUsage.InputTokens != b.TotalUsage.InputTokens ||
		a.TotalUsage.OutputTokens != b.TotalUsage.OutputTokens ||
		a.TotalCost.TotalCost != b.TotalCost.TotalCost ||
		a.SkippedSessions != b.SkippedSessions || a.SkippedAgents != b.SkippedAgents ||
		!a.StartTime.Equal(b.StartTime) || !a.EndTime.Equal(b.EndTime) {
		t.Errorf("%s: totals differ\n got %+v\nwant %+v", label, a, b)
	}
}

// An old session drops out without being parsed: its corrupt line no longer
// counts, and everything else matches what a parse of it would give.
func TestWindowSkipsSessionsWrittenBeforeSince(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.jsonl")
	writeJSONLFile(t, oldPath, []string{forkMsg1, corruptLine, forkMsg2})
	newPath := filepath.Join(dir, "new.jsonl")
	writeJSONLFile(t, newPath, []string{windowMsg})
	setMtime(t, oldPath, oldMtime)
	setMtime(t, newPath, newMtime)

	skipped, skippedResults, err := AnalyzeMultipleSessionsInWindow(diskEntries(t, oldPath, newPath), skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	if skipped.SkippedLines != 0 {
		t.Errorf("SkippedLines: got %d, want 0 (the old file isn't opened)", skipped.SkippedLines)
	}
	if len(skippedResults) != 1 || skippedResults[0].Entry.SessionID != "new" {
		t.Fatalf("results: got %+v, want the new session alone", skippedResults)
	}

	// The same files with the old one's mtime moved into the window: now it's
	// parsed, and the only difference is its corrupt line.
	setMtime(t, oldPath, newMtime)
	parsed, parsedResults, err := AnalyzeMultipleSessionsInWindow(diskEntries(t, oldPath, newPath), skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SkippedLines != 1 {
		t.Errorf("parsed SkippedLines: got %d, want 1", parsed.SkippedLines)
	}
	sameTotals(t, "skip vs parse", skipped, parsed)
	if len(parsedResults) != 1 || parsedResults[0].Entry.SessionID != "new" {
		t.Errorf("parsed results: got %+v, want the new session alone", parsedResults)
	}
}

// The newest file decides: an old parent whose agent wrote inside the window
// is parsed, and --until alone gets no shortcut.
func TestWindowSkipNeedsEveryFileOldAndASince(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONLFile(t, path, []string{forkMsg1, corruptLine})
	writeAgentSession(t, dir, "sess", "agent-a1.jsonl", []string{windowAgentMsg})
	setMtime(t, path, oldMtime)
	setMtime(t, filepath.Join(dir, "sess", "subagents", "agent-a1.jsonl"), newMtime)

	agg, _, err := AnalyzeMultipleSessionsInWindow(diskEntries(t, path), skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	if agg.MessageCount != 1 || agg.AgentMessageCount != 1 || agg.SkippedLines != 1 {
		t.Errorf("new agent: got %d messages (%d agent), %d skipped lines; want 1 (1), 1",
			agg.MessageCount, agg.AgentMessageCount, agg.SkippedLines)
	}

	setMtime(t, filepath.Join(dir, "sess", "subagents", "agent-a1.jsonl"), oldMtime)
	agg, _, err = AnalyzeMultipleSessionsInWindow(diskEntries(t, path), models.TimeWindow{Until: skipWindow.Since})
	if err != nil {
		t.Fatal(err)
	}
	if agg.SkippedLines != 1 {
		t.Errorf("--until alone: SkippedLines got %d, want 1 (every file parsed)", agg.SkippedLines)
	}
}

// A window before any activity is empty, not an error, and in global the
// project vanishes as it would with nothing in the window.
func TestWindowSkipOfEverySessionIsAnEmptyReport(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "proj")
	if err := os.Mkdir(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(projDir, "a.jsonl")
	b := filepath.Join(projDir, "b.jsonl")
	writeJSONLFile(t, a, []string{forkMsg1})
	writeJSONLFile(t, b, []string{forkMsg2, corruptLine})
	setMtime(t, a, oldMtime)
	setMtime(t, b, oldMtime)

	agg, results, err := AnalyzeMultipleSessionsInWindow(diskEntries(t, a, b), skipWindow)
	if err != nil {
		t.Fatalf("every session old: got error %v, want an empty report", err)
	}
	if agg.SessionCount != 0 || agg.MessageCount != 0 || agg.SkippedSessions != 0 || agg.SkippedLines != 0 || len(results) != 0 {
		t.Errorf("every session old: got %d sessions, %d messages, %d skipped sessions, %d skipped lines, %d results; want all 0",
			agg.SessionCount, agg.MessageCount, agg.SkippedSessions, agg.SkippedLines, len(results))
	}
	if agg.Window == nil {
		t.Error("the aggregate should carry the window")
	}

	global, err := AnalyzeAllProjectsInWindow([]models.ProjectInfo{{
		EncodedPath: "proj", FullPath: projDir, OriginalPath: "/home/test/proj", DisplayName: "proj",
	}}, skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	if global.ProjectCount != 0 || len(global.Projects) != 0 || global.SkippedProjects != 0 {
		t.Errorf("global: got %d projects (%d listed), %d skipped; want 0, 0, 0",
			global.ProjectCount, len(global.Projects), global.SkippedProjects)
	}
}

// A fork clones its original's lines, timestamps included, and adds its own.
// Skipping the old original leaves its keys out of the dedup set, but the
// fork's copies are as old as the original's lines, so the window drops them
// anyway: totals match a run that parses the original.
func TestWindowSkipOfAForkedOriginalKeepsTotals(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "original.jsonl")
	writeJSONLFile(t, originalPath, []string{forkMsg1, forkMsg2})
	forkPath := filepath.Join(dir, "fork.jsonl")
	writeJSONLFile(t, forkPath, []string{forkMsg1, forkMsg2, windowMsg})
	setMtime(t, originalPath, oldMtime)
	setMtime(t, forkPath, newMtime)

	skipped, skippedResults, err := AnalyzeMultipleSessionsInWindow(diskEntries(t, forkPath, originalPath), skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	if skipped.MessageCount != 1 || skipped.TotalUsage.InputTokens != 3000 {
		t.Errorf("fork: got %d messages, %d input tokens; want 1, 3000", skipped.MessageCount, skipped.TotalUsage.InputTokens)
	}

	setMtime(t, originalPath, newMtime)
	parsed, parsedResults, err := AnalyzeMultipleSessionsInWindow(diskEntries(t, forkPath, originalPath), skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	sameTotals(t, "skip vs parse", skipped, parsed)
	if len(skippedResults) != 1 || len(parsedResults) != 1 ||
		skippedResults[0].Analysis.TotalCost.TotalCost != parsedResults[0].Analysis.TotalCost.TotalCost {
		t.Errorf("fork result: got %+v, want %+v", skippedResults, parsedResults)
	}
}

// Only a file that opens and proves old is skipped. An entry whose transcript
// is missing, can't be opened or isn't a file fails as it would without the
// shortcut, and an agent file that can't be opened still counts as a skipped
// agent.
func TestWindowSkipLeavesUnreadableSessionsFailing(t *testing.T) {
	dir := t.TempDir()
	missing := models.SessionEntry{SessionID: "gone", FullPath: filepath.Join(dir, "gone.jsonl"), Modified: oldMtime}
	if _, _, err := AnalyzeMultipleSessionsInWindow([]models.SessionEntry{missing}, skipWindow); err == nil {
		t.Error("a missing transcript alone: got no error, want the all-failed error")
	}

	okPath := filepath.Join(dir, "ok.jsonl")
	writeJSONLFile(t, okPath, []string{windowMsg})
	setMtime(t, okPath, newMtime)
	agg, _, err := AnalyzeMultipleSessionsInWindow([]models.SessionEntry{missing, diskEntry(t, okPath)}, skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	if agg.SkippedSessions != 1 {
		t.Errorf("missing transcript: SkippedSessions got %d, want 1", agg.SkippedSessions)
	}

	// Discovery keeps a transcript that is a symlink to a directory, and the
	// directory's mtime is old, but the parse fails on it.
	realDir := filepath.Join(dir, "realdir")
	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	setMtime(t, realDir, oldMtime)
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(realDir, link); err == nil {
		agg, _, err = AnalyzeMultipleSessionsInWindow([]models.SessionEntry{
			{SessionID: "link", FullPath: link, Modified: oldMtime}, diskEntry(t, okPath),
		}, skipWindow)
		if err != nil {
			t.Fatal(err)
		}
		if agg.SkippedSessions != 1 {
			t.Errorf("transcript linked to a directory: SkippedSessions got %d, want 1", agg.SkippedSessions)
		}
	}

	locked := filepath.Join(dir, "locked.jsonl")
	writeJSONLFile(t, locked, []string{forkMsg1})
	setMtime(t, locked, oldMtime)
	if err := os.Chmod(locked, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0644) })
	if _, err := os.ReadFile(locked); err == nil {
		t.Skip("chmod 000 does not bar reads (running as root, or on Windows)")
	}
	agg, _, err = AnalyzeMultipleSessionsInWindow(diskEntries(t, locked, okPath), skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	if agg.SkippedSessions != 1 {
		t.Errorf("unopenable old transcript: SkippedSessions got %d, want 1", agg.SkippedSessions)
	}

	parentPath := filepath.Join(dir, "parent.jsonl")
	writeJSONLFile(t, parentPath, []string{forkMsg1})
	writeAgentSession(t, dir, "parent", "agent-a1.jsonl", []string{forkMsg2})
	agentPath := filepath.Join(dir, "parent", "subagents", "agent-a1.jsonl")
	setMtime(t, parentPath, oldMtime)
	setMtime(t, agentPath, oldMtime)
	if err := os.Chmod(agentPath, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(agentPath, 0644) })
	agg, _, err = AnalyzeMultipleSessionsInWindow(diskEntries(t, parentPath, okPath), skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	if agg.SkippedAgents != 1 {
		t.Errorf("unopenable old agent: SkippedAgents got %d, want 1", agg.SkippedAgents)
	}
}
