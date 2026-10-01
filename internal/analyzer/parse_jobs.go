package analyzer

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
)

// parseJob reads one transcript into a slot only it writes. Jobs run in any
// order on any goroutine, and the analysis that reads the slots afterwards
// sums them in a fixed order, so the totals don't depend on which job
// finished first.
type parseJob struct {
	size int64
	run  func()
}

// newParseJob sizes the job by its file, so runParseJobs can order the queue.
// A file that can't be statted sorts last: its parse fails at once.
func newParseJob(path string, run func()) parseJob {
	job := parseJob{run: run}
	if info, err := os.Stat(path); err == nil {
		job.size = info.Size()
	}
	return job
}

// runParseJobs runs jobs on up to GOMAXPROCS goroutines, largest file first.
// The largest file sets the floor on the run's time, so it starts at once
// rather than whenever a worker reaches it.
func runParseJobs(jobs []parseJob) {
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].size > jobs[j].size })
	forEachParallel(len(jobs), func(i int) { jobs[i].run() })
}

// forEachParallel calls fn(0) to fn(n-1) on up to GOMAXPROCS goroutines,
// handing the indices out in order, and returns when every call has.
func forEachParallel(n int, fn func(i int)) {
	workers := min(runtime.GOMAXPROCS(0), n)
	if workers <= 1 {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Go(func() {
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				fn(i)
			}
		})
	}
	wg.Wait()
}

// agentParse is what loadAgentMessages returned for one agent file.
type agentParse struct {
	messages []models.MessageAnalysis
	skips    models.FileSkips
	err      error
}

// sessionAgents is a session's agent files as one directory listing found
// them, with each file's parse once its job has run. The analysis reads the
// agents from here and never from disk, so it sees one version of each file
// and the same set of files the jobs were made for.
type sessionAgents struct {
	paths          []string
	unreadableDirs int
	parses         []agentParse // parallel to paths
}

func discoverAgents(sessionPath, sessionID string) *sessionAgents {
	agentPaths, unreadableDirs := parser.DiscoverAgentSessions(filepath.Dir(sessionPath), sessionID)
	return &sessionAgents{
		paths:          agentPaths,
		unreadableDirs: unreadableDirs,
		parses:         make([]agentParse, len(agentPaths)),
	}
}

// jobs returns a job per agent file.
func (a *sessionAgents) jobs(cache *AgentParseCache) []parseJob {
	jobs := make([]parseJob, len(a.paths))
	for i, path := range a.paths {
		p := &a.parses[i]
		jobs[i] = newParseJob(path, func() {
			p.messages, p.skips, p.err = loadAgentMessages(path, cache)
		})
	}
	return jobs
}

// parseSession parses a session's transcript and its agent files in
// parallel, taking from cache what hasn't changed. The error is the parent
// transcript's.
func parseSession(sessionPath, sessionID string, cache *AgentParseCache) (*parser.ParseResult, *sessionAgents, error) {
	agents := discoverAgents(sessionPath, sessionID)
	var result *parser.ParseResult
	var err error
	runParseJobs(append(agents.jobs(cache), newParseJob(sessionPath, func() {
		result, err = loadParentParse(sessionPath, cache)
	})))
	if err != nil {
		return nil, nil, err
	}
	return result, agents, nil
}

// sessionParses is what the parse step read for a list of session entries,
// each slice parallel to the entries.
type sessionParses struct {
	parsed []*parser.ParseResult // nil when the parse failed or never ran
	old    []bool                // written too long before the window to be parsed
	agents []*sessionAgents
}

// planSessionParses lists each entry's agent files and returns the jobs that
// parse every transcript the window can't rule out by age alone. The parses
// are complete once the jobs have run.
func planSessionParses(entries []models.SessionEntry, window models.TimeWindow) (*sessionParses, []parseJob) {
	parses := &sessionParses{
		parsed: make([]*parser.ParseResult, len(entries)),
		old:    make([]bool, len(entries)),
		agents: make([]*sessionAgents, len(entries)),
	}
	var cutoff time.Time
	if !window.Since.IsZero() {
		cutoff = window.Since.Add(-windowSkipSlack)
	}
	var jobs []parseJob
	for i, entry := range entries {
		agents := discoverAgents(entry.FullPath, entry.SessionID)
		parses.agents[i] = agents
		if !cutoff.IsZero() && writtenBefore(entry, agents, cutoff) {
			parses.old[i] = true
			continue
		}
		jobs = append(jobs, newParseJob(entry.FullPath, func() {
			// A failed parse leaves the slot nil.
			parses.parsed[i], _ = parser.ParseJSONLFileWithResult(entry.FullPath)
		}))
		jobs = append(jobs, agents.jobs(nil)...)
	}
	return parses, jobs
}
