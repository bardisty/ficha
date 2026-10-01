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

// A session with nothing inside the window still counts its unreadable
// lines, so SkipDetails names it and its files, though results leave it out.
// A session the shortcut skips unparsed is in neither.
func TestWindowSkipDetailsNameSessionsOutsideTheWindow(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "proj")
	if err := os.Mkdir(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(projDir, "new.jsonl")
	writeJSONLFile(t, newPath, []string{windowMsg})
	outPath := filepath.Join(projDir, "out.jsonl")
	writeJSONLFile(t, outPath, []string{forkMsg1, corruptLine})
	writeAgentSession(t, projDir, "out", "agent-a1.jsonl", []string{corruptLine, forkMsg2})
	oldPath := filepath.Join(projDir, "old.jsonl")
	writeJSONLFile(t, oldPath, []string{forkMsg3, corruptLine})
	setMtime(t, oldPath, oldMtime)

	agg, results, err := AnalyzeMultipleSessionsInWindow(diskEntries(t, newPath, outPath, oldPath), skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Entry.SessionID != "new" {
		t.Fatalf("results: got %+v, want the new session alone", results)
	}
	if agg.SkippedLines != 2 {
		t.Errorf("SkippedLines: got %d, want 2", agg.SkippedLines)
	}
	if len(agg.SkipDetails) != 1 {
		t.Fatalf("SkipDetails: got %+v, want the out session alone", agg.SkipDetails)
	}
	d := agg.SkipDetails[0]
	agentPath := filepath.Join(projDir, "out", "subagents", "agent-a1.jsonl")
	if d.SessionID != "out" || d.Unreadable || d.Lines != 2 || len(d.Files) != 2 ||
		d.Files[0].Path != outPath || d.Files[0].Lines[0].Line != 2 ||
		d.Files[1].Path != agentPath || d.Files[1].AgentID != "a1" || d.Files[1].Lines[0].Line != 1 {
		t.Errorf("SkipDetails[0]: got %+v, want out with line 2 of its transcript and line 1 of agent a1", d)
	}

	global, err := AnalyzeAllProjectsInWindow([]models.ProjectInfo{{
		EncodedPath: "proj", FullPath: projDir, OriginalPath: "/home/test/proj", DisplayName: "proj",
	}}, skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(global.Projects) != 1 || global.SkippedLines != 2 ||
		len(global.Projects[0].SkipDetails) != 1 || global.Projects[0].SkipDetails[0].SessionID != "out" {
		t.Errorf("global: got %d skipped lines, projects %+v; want 2, and out named", global.SkippedLines, global.Projects)
	}
}

// A project with nothing inside the window stays out of global's projects and
// counts, but its unreadable input still reaches the skip counters, and
// OutOfWindow carries its SkipDetails. One whose only session the shortcut
// skips unparsed counts nowhere.
func TestWindowKeepsSkipsOfAProjectWithNothingInside(t *testing.T) {
	dir := t.TempDir()
	project := func(name string) models.ProjectInfo {
		projDir := filepath.Join(dir, name)
		if err := os.Mkdir(projDir, 0755); err != nil {
			t.Fatal(err)
		}
		return models.ProjectInfo{EncodedPath: name, FullPath: projDir, OriginalPath: "/home/test/" + name, DisplayName: name}
	}
	live, out, old := project("live"), project("out"), project("old")
	writeJSONLFile(t, filepath.Join(live.FullPath, "new.jsonl"), []string{windowMsg})
	outPath := filepath.Join(out.FullPath, "damaged.jsonl")
	writeJSONLFile(t, outPath, []string{forkMsg1, corruptLine})
	writeAgentSession(t, out.FullPath, "damaged", "agent-a1.jsonl", []string{corruptLine, forkMsg2})
	oldPath := filepath.Join(old.FullPath, "stale.jsonl")
	writeJSONLFile(t, oldPath, []string{forkMsg3, corruptLine})
	setMtime(t, oldPath, oldMtime)

	alone, err := AnalyzeAllProjectsInWindow([]models.ProjectInfo{live}, skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	global, err := AnalyzeAllProjectsInWindow([]models.ProjectInfo{live, out, old}, skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(global.Projects) != 1 || global.Projects[0].EncodedPath != "live" ||
		global.ProjectCount != 1 || global.SessionCount != 1 || global.MessageCount != 1 ||
		global.SkippedProjects != 0 {
		t.Errorf("records: got %d projects (%d listed), %d sessions, %d messages, %d skipped projects; want live alone, 1, 1, 0",
			global.ProjectCount, len(global.Projects), global.SessionCount, global.MessageCount, global.SkippedProjects)
	}
	if global.TotalCost.TotalCost != alone.TotalCost.TotalCost ||
		global.TotalUsage.InputTokens != alone.TotalUsage.InputTokens ||
		!global.FirstActive.Equal(alone.FirstActive) || !global.LastActive.Equal(alone.LastActive) {
		t.Errorf("totals: got %+v, want those of live alone, %+v", global, alone)
	}
	if global.SkippedLines != 2 || global.SkippedSessions != 0 || global.SkippedAgents != 0 {
		t.Errorf("skip counters: got %d lines, %d sessions, %d agents; want 2, 0, 0",
			global.SkippedLines, global.SkippedSessions, global.SkippedAgents)
	}
	if len(global.OutOfWindow) != 1 || global.OutOfWindow[0].EncodedPath != "out" ||
		len(global.OutOfWindow[0].SkipDetails) != 1 || global.OutOfWindow[0].SkipDetails[0].SessionID != "damaged" ||
		global.OutOfWindow[0].SkipDetails[0].Lines != 2 {
		t.Errorf("OutOfWindow: got %+v, want out alone, naming damaged with 2 lines", global.OutOfWindow)
	}

	// A transcript that fails to parse counts as a skipped session and is
	// named, though its project has nothing else inside the window. Discovery
	// keeps a symlink to a directory, and the parse fails on it.
	realDir := filepath.Join(dir, "realdir")
	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, filepath.Join(out.FullPath, "link.jsonl")); err == nil {
		linked, err := AnalyzeAllProjectsInWindow([]models.ProjectInfo{live, out}, skipWindow)
		if err != nil {
			t.Fatal(err)
		}
		if linked.ProjectCount != 1 || linked.SkippedSessions != 1 || linked.SkippedProjects != 0 ||
			len(linked.OutOfWindow) != 1 || len(linked.OutOfWindow[0].SkipDetails) != 2 {
			t.Errorf("unreadable session: got %d projects, %d skipped sessions, %d skipped projects, out of window %+v; want 1, 1, 0, and out naming both sessions",
				linked.ProjectCount, linked.SkippedSessions, linked.SkippedProjects, linked.OutOfWindow)
		}
		if err := os.Remove(filepath.Join(out.FullPath, "link.jsonl")); err != nil {
			t.Fatal(err)
		}
	}

	// Without a window the damaged project is an ordinary record.
	unwindowed, err := AnalyzeAllProjectsInWindow([]models.ProjectInfo{live, out, old}, models.TimeWindow{})
	if err != nil {
		t.Fatal(err)
	}
	if unwindowed.ProjectCount != 3 || len(unwindowed.OutOfWindow) != 0 || unwindowed.SkippedLines != 3 {
		t.Errorf("no window: got %d projects, %d out of window, %d skipped lines; want 3, 0, 3",
			unwindowed.ProjectCount, len(unwindowed.OutOfWindow), unwindowed.SkippedLines)
	}
}

// Written yesterday, counting from skipWindow: inside the day of slack, so
// the mtime alone can't rule the file out and its last lines decide.
var slackMtime = time.Date(2024, 1, 16, 12, 0, 0, 0, time.UTC)

// stampedLine is a line of any type with only a timestamp, as the user and
// system lines between assistant messages are.
func stampedLine(ts string) string {
	return `{"type":"user","timestamp":"` + ts + `"}`
}

// parsedInSlack writes lines to a transcript last written inside the slack
// and reports whether a windowed run parsed it. Every case holds corruptLine,
// which only a parse counts.
func parsedInSlack(t *testing.T, lines ...string) bool {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONLFile(t, path, lines)
	setMtime(t, path, slackMtime)
	newPath := filepath.Join(dir, "new.jsonl")
	writeJSONLFile(t, newPath, []string{windowMsg})
	setMtime(t, newPath, newMtime)

	agg, results, err := AnalyzeMultipleSessionsInWindow(diskEntries(t, path, newPath), skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	// Parsed or not, the report is the same: nothing in these files is
	// inside the window but windowMsg.
	if agg.MessageCount != 1 || len(results) != 1 || results[0].Entry.SessionID != "new" {
		t.Errorf("got %d messages in %+v, want the new session's one", agg.MessageCount, results)
	}
	return agg.SkippedLines == 1
}

// A file written inside the day of slack is skipped when its last lines are
// more than an hour older than --since, and parsed when they aren't or when
// they can't say.
func TestWindowSkipReadsTheTailInsideTheSlack(t *testing.T) {
	// A line longer than the tail read, as a large tool result is.
	longLine := `{"type":"user","timestamp":"2024-01-15T10:20:00Z","content":"` + strings.Repeat("x", 70*1024) + `"}`

	cases := []struct {
		name   string
		lines  []string
		parsed bool
	}{
		{"old tail", []string{forkMsg1, corruptLine, forkMsg2}, false},
		{"old tail after a long line", []string{corruptLine, longLine, forkMsg2, stampedLine("2024-01-15T10:30:00Z")}, false},
		{"tail within the hour before since", []string{forkMsg1, corruptLine, stampedLine("2024-01-16T23:30:00Z")}, true},
		{"newest line isn't last", []string{corruptLine, stampedLine("2024-01-16T23:30:00Z"), forkMsg2}, true},
		{"no complete line in the tail", []string{forkMsg1, corruptLine, longLine}, true},
		{"no timestamp in the tail", []string{forkMsg1, corruptLine, longLine, `{"type":"summary","summary":"untimed"}`}, true},
		{"only a corrupt line", []string{corruptLine}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parsedInSlack(t, c.lines...); got != c.parsed {
				t.Errorf("parsed: got %v, want %v", got, c.parsed)
			}
		})
	}
}

// A message inside the window in a file the mtime says is yesterday's, as a
// file system clock behind the writer's leaves it, is still counted: the tail
// shows it.
func TestWindowSkipKeepsAMessageTheTailShows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONLFile(t, path, []string{forkMsg1, windowMsg, forkMsg2})
	setMtime(t, path, slackMtime)

	agg, results, err := AnalyzeMultipleSessionsInWindow(diskEntries(t, path), skipWindow)
	if err != nil {
		t.Fatal(err)
	}
	if agg.MessageCount != 1 || len(results) != 1 {
		t.Errorf("got %d messages in %d sessions, want 1 in 1", agg.MessageCount, len(results))
	}
}

// Agent files get the same tail read, and one that can't be ruled out keeps
// its whole session parsed.
func TestWindowSkipReadsAgentTails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONLFile(t, path, []string{forkMsg1, corruptLine})
	setMtime(t, path, oldMtime)
	agentPath := filepath.Join(dir, "sess", "subagents", "agent-a1.jsonl")
	newPath := filepath.Join(dir, "new.jsonl")
	writeJSONLFile(t, newPath, []string{windowMsg})
	setMtime(t, newPath, newMtime)

	skippedLines := func(agentLines ...string) int {
		t.Helper()
		writeAgentSession(t, dir, "sess", "agent-a1.jsonl", agentLines)
		setMtime(t, agentPath, slackMtime)
		agg, _, err := AnalyzeMultipleSessionsInWindow(diskEntries(t, path, newPath), skipWindow)
		if err != nil {
			t.Fatal(err)
		}
		return agg.SkippedLines
	}
	if got := skippedLines(forkMsg2); got != 0 {
		t.Errorf("agent with an old tail: SkippedLines got %d, want 0 (the session isn't parsed)", got)
	}
	if got := skippedLines(forkMsg2, stampedLine("2024-01-16T23:30:00Z")); got != 1 {
		t.Errorf("agent with a recent tail: SkippedLines got %d, want 1 (the session is parsed)", got)
	}
}
