package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

// unevenProject writes a project of n sessions whose transcripts and agent
// files differ widely in size, so a parallel run finishes them in an order
// far from the one they're summed in. Session 1 forks session 0, one session
// has a corrupt line, and one has a file where its workflow runs should be.
func unevenProject(t *testing.T, root, name string, n int) models.ProjectInfo {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Token counts chosen so costs aren't round numbers and their sum
	// depends on the order of addition.
	line := func(id string, minute, k int) string {
		return fmt.Sprintf(`{"type":"assistant","requestId":"req_%s","timestamp":"2024-01-15T%02d:%02d:00Z","message":{"id":"msg_%s","model":"claude-sonnet-4-5","usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d}}}`,
			id, 10+minute/60, minute%60, id, 1237*(k+1)+k*k*31, 7919*(k+3)%10007, 104729*(k+7)%99991)
	}
	transcript := func(prefix string, count int) []string {
		lines := make([]string, count)
		for m := range lines {
			lines[m] = line(fmt.Sprintf("%s_%d", prefix, m), m%240, m+len(prefix))
		}
		return lines
	}
	for s := range n {
		id := fmt.Sprintf("sess-%02d", s)
		lines := transcript(name+id, 1+(s*s*37)%190)
		switch s {
		case 1:
			lines = append(transcript(name+"sess-00", 1), lines...)
		case 2:
			lines = append(lines, corruptLine)
		}
		writeJSONLFile(t, filepath.Join(dir, id+".jsonl"), lines)
		for a := range s % 5 {
			writeAgentSession(t, dir, id, fmt.Sprintf("agent-a%d.jsonl", a), transcript(fmt.Sprintf("%s%sa%d", name, id, a), 1+(a*61+s*13)%150))
		}
		if s%4 == 3 {
			runDir := filepath.Join(dir, id, "subagents", "workflows", "wf-run")
			if err := os.MkdirAll(runDir, 0o755); err != nil {
				t.Fatal(err)
			}
			for a := range 3 {
				writeJSONLFile(t, filepath.Join(runDir, fmt.Sprintf("agent-w%d.jsonl", a)), transcript(fmt.Sprintf("%s%sw%d", name, id, a), 2+a*40))
			}
		}
		if s == 4 {
			writeJSONLFile(t, filepath.Join(dir, id, "subagents", "workflows"), nil)
		}
	}
	return models.ProjectInfo{EncodedPath: name, FullPath: dir, DisplayName: name}
}

func projectEntries(t *testing.T, project models.ProjectInfo) []models.SessionEntry {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(project.FullPath, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return diskEntries(t, paths...)
}

// sameFields fails for each field of the structs got and want point to that
// differs, by name.
func sameFields(t *testing.T, label string, got, want any) {
	t.Helper()
	g, w := reflect.ValueOf(got).Elem(), reflect.ValueOf(want).Elem()
	for i := range g.NumField() {
		if !reflect.DeepEqual(g.Field(i).Interface(), w.Field(i).Interface()) {
			t.Errorf("%s: %s differs\n got %+v\nwant %+v", label, g.Type().Field(i).Name, g.Field(i).Interface(), w.Field(i).Interface())
		}
	}
}

// Parsing in parallel must change nothing but the time it takes: every field
// of every result matches a run on one thread, where the jobs run in turn.
func TestParallelParseMatchesSerial(t *testing.T) {
	root := t.TempDir()
	projects := []models.ProjectInfo{
		unevenProject(t, root, "big", 23),
		unevenProject(t, root, "mid", 6),
		unevenProject(t, root, "small", 1),
	}
	entries := projectEntries(t, projects[0])
	session := entries[7]
	window := models.TimeWindow{
		Since: entries[0].Modified.AddDate(-10, 0, 0),
		Until: entries[0].Modified.AddDate(-1, 0, 0),
	}

	type run struct {
		aggregate, windowed *models.SessionAnalysis
		results             []models.SessionResult
		global              *models.GlobalAnalysis
		session             *models.SessionAnalysis
		breakdown           *BreakdownResult
	}
	analyze := func(threads int) run {
		defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(threads))
		var r run
		var err error
		if r.aggregate, r.results, err = AnalyzeMultipleSessions(entries); err != nil {
			t.Fatal(err)
		}
		if r.windowed, _, err = AnalyzeMultipleSessionsInWindow(entries, window); err != nil {
			t.Fatal(err)
		}
		if r.global, err = AnalyzeAllProjects(projects); err != nil {
			t.Fatal(err)
		}
		if r.session, err = AnalyzeSession(session.FullPath, session.SessionID, AllMessages); err != nil {
			t.Fatal(err)
		}
		if r.breakdown, err = GetBreakdownMessages(session.FullPath, session.SessionID); err != nil {
			t.Fatal(err)
		}
		return r
	}

	serial := analyze(1)
	if serial.aggregate.AgentCount < 8 || serial.aggregate.SkippedAgents == 0 || serial.aggregate.SkippedLines == 0 {
		t.Fatalf("the project lacks the cases this test is for: %d agents, %d skipped agents, %d skipped lines",
			serial.aggregate.AgentCount, serial.aggregate.SkippedAgents, serial.aggregate.SkippedLines)
	}
	// Fewer workers than files, so jobs queue.
	for range 5 {
		parallel := analyze(4)
		sameFields(t, "aggregate", parallel.aggregate, serial.aggregate)
		sameFields(t, "windowed aggregate", parallel.windowed, serial.windowed)
		if len(parallel.results) != len(serial.results) {
			t.Fatalf("results: got %d, want %d", len(parallel.results), len(serial.results))
		}
		for i := range serial.results {
			sameFields(t, "session "+serial.results[i].Entry.SessionID, parallel.results[i].Analysis, serial.results[i].Analysis)
		}
		sameFields(t, "global", parallel.global, serial.global)
		sameFields(t, "show", parallel.session, serial.session)
		sameFields(t, "breakdown", parallel.breakdown, serial.breakdown)
	}
}

func TestForEachParallelCallsEachIndexOnce(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(4))
	for _, n := range []int{0, 1, 3, 4, 100} {
		calls := make([]atomic.Int32, n)
		forEachParallel(n, func(i int) { calls[i].Add(1) })
		for i := range calls {
			if got := calls[i].Load(); got != 1 {
				t.Errorf("n=%d: index %d called %d times", n, i, got)
			}
		}
	}
}

// The queue runs largest first whatever order the jobs were made in.
func TestRunParseJobsStartsWithTheLargest(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	var ran []int64
	var jobs []parseJob
	for _, size := range []int64{3, 0, 900, 12, 900, 7} {
		jobs = append(jobs, parseJob{size: size, run: func() { ran = append(ran, size) }})
	}
	runParseJobs(jobs)
	if want := []int64{900, 900, 12, 7, 3, 0}; !reflect.DeepEqual(ran, want) {
		t.Errorf("ran %v, want %v", ran, want)
	}
}
