package analyzer

import (
	"errors"
	"io/fs"
	"sort"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
)

// projectResult holds the result of analyzing a single project
type projectResult struct {
	analysis *models.ProjectAnalysis
	err      error
	// indexErr is why the project's sessions-index.json went unused, nil
	// when it was read or isn't there.
	indexErr error
}

// AnalyzeAllProjects analyzes all projects in parallel and returns aggregated stats
func AnalyzeAllProjects(projects []models.ProjectInfo) (*models.GlobalAnalysis, error) {
	return AnalyzeAllProjectsInWindow(projects, models.TimeWindow{})
}

// AnalyzeAllProjectsInWindow is AnalyzeAllProjects counting only the
// messages inside window. A project with nothing inside it is left out, like
// one with no sessions, except in the skip counters (see OutOfWindow).
func AnalyzeAllProjectsInWindow(projects []models.ProjectInfo, window models.TimeWindow) (*models.GlobalAnalysis, error) {
	if len(projects) == 0 {
		return &models.GlobalAnalysis{
			CostByModel: make(map[string]models.CostBreakdown),
		}, nil
	}

	// Every project's transcripts go in one queue. A queue per project would
	// leave the largest project to one thread while the others sit idle.
	sessions := make([][]models.SessionEntry, len(projects))
	parses := make([]*sessionParses, len(projects))
	projectJobs := make([][]parseJob, len(projects))
	results := make([]projectResult, len(projects))
	forEachParallel(len(projects), func(idx int) {
		sessions[idx], results[idx].indexErr, results[idx].err = discoverProjectSessions(projects[idx])
		if len(sessions[idx]) > 0 {
			parses[idx], projectJobs[idx] = planSessionParses(sessions[idx], window)
		}
	})
	var jobs []parseJob
	for _, pj := range projectJobs {
		jobs = append(jobs, pj...)
	}
	runParseJobs(jobs)

	// Each project's result goes to its own slot. The totals are summed
	// afterwards in input order: float addition isn't associative, so summing
	// as projects finish would change the last digits of the json totals from
	// run to run.
	forEachParallel(len(projects), func(idx int) {
		if results[idx].err == nil {
			results[idx].analysis, results[idx].err = analyzeProject(projects[idx], sessions[idx], parses[idx], window)
		}
	})

	// Projects starts empty, not nil, so json has [] to iterate even when a
	// window leaves nothing in it.
	global := &models.GlobalAnalysis{
		Projects:    []models.ProjectAnalysis{},
		CostByModel: make(map[string]models.CostBreakdown),
	}
	if !window.IsZero() {
		global.Window = &window
	}

	firstActiveSet := false
	for idx, result := range results {
		// Kept in input order, like the sums below, so the warnings read the
		// same from run to run.
		if result.indexErr != nil {
			global.IgnoredIndexes = append(global.IgnoredIndexes, models.IgnoredIndex{
				Path: paths.GetSessionsIndexPath(projects[idx].FullPath),
				Err:  result.indexErr,
			})
		}
		if result.err != nil {
			global.SkippedProjects++
			continue
		}

		analysis := result.analysis
		// A project a window emptied still counts its unreadable input: the
		// skipped lines could hold messages from any date, so the warning
		// holds for any window, as it does in summary.
		global.SkippedSessions += analysis.SkippedSessions
		global.SkippedAgents += analysis.SkippedAgents
		global.SkippedLines += analysis.SkippedLines
		if analysis.SessionCount == 0 {
			if analysis.SkippedSessions+analysis.SkippedAgents+analysis.SkippedLines > 0 {
				global.OutOfWindow = append(global.OutOfWindow, *analysis)
			}
			continue
		}

		global.Projects = append(global.Projects, *analysis)
		global.ProjectCount++
		global.SessionCount += analysis.SessionCount
		global.MessageCount += analysis.MessageCount
		global.EstimatedCostMessages += analysis.EstimatedCostMessages
		global.TotalCost.Add(analysis.TotalCost)
		global.TotalUsage.Add(analysis.TotalUsage)

		// Merge cost by model
		for model, cost := range analysis.CostByModel {
			if existing, ok := global.CostByModel[model]; ok {
				existing.Add(cost)
				global.CostByModel[model] = existing
			} else {
				global.CostByModel[model] = cost
			}
		}

		// Track time range
		if !analysis.FirstActive.IsZero() {
			if !firstActiveSet || analysis.FirstActive.Before(global.FirstActive) {
				global.FirstActive = analysis.FirstActive
				firstActiveSet = true
			}
		}
		if !analysis.LastActive.IsZero() {
			if analysis.LastActive.After(global.LastActive) {
				global.LastActive = analysis.LastActive
			}
		}
	}

	// Calculate duration
	if firstActiveSet && !global.FirstActive.IsZero() && !global.LastActive.IsZero() {
		global.Duration = models.Duration(global.LastActive.Sub(global.FirstActive))
	}

	// Sort projects by cost descending; equal costs keep input order.
	sort.SliceStable(global.Projects, func(i, j int) bool {
		return global.Projects[i].TotalCost.TotalCost > global.Projects[j].TotalCost.TotalCost
	})

	return global, nil
}

// discoverProjectSessions lists a project directory's sessions. The sessions
// on disk load without sessions-index.json, so an index that can't be used
// comes back as indexErr beside them. A missing one is no error: current
// Claude Code writes none.
func discoverProjectSessions(project models.ProjectInfo) (sessions []models.SessionEntry, indexErr, err error) {
	diskSessions, err := parser.DiscoverSessionsFromDisk(project.FullPath)
	if err != nil {
		return nil, nil, err
	}

	index, indexErr := parser.ParseSessionsIndex(paths.GetSessionsIndexPath(project.FullPath))
	if errors.Is(indexErr, fs.ErrNotExist) {
		indexErr = nil
	}

	sessions, _ = parser.MergeSessionSources(index, diskSessions, project.FullPath)
	return sessions, indexErr, nil
}

// analyzeProject builds a project's analysis from its sessions and their
// parses.
func analyzeProject(project models.ProjectInfo, sessions []models.SessionEntry, parses *sessionParses, window models.TimeWindow) (*models.ProjectAnalysis, error) {
	if len(sessions) == 0 {
		return &models.ProjectAnalysis{
			ProjectInfo: project,
			CostByModel: make(map[string]models.CostBreakdown),
		}, nil
	}

	// Analyze all sessions (the per-session results are only needed by the
	// summary detail view, not the global rollup). Cross-file dedup of
	// fork-copied transcripts happens inside AnalyzeMultipleSessions, scoped
	// per project: forks never land in another project's directory, and a
	// project-local seen set keeps the parallel project workers lock-free.
	aggregate, _, err := analyzeSessionParses(sessions, window, parses)
	if err != nil {
		return nil, err
	}

	// Build project analysis. SessionCount counts only the sessions the
	// aggregate could cost — a discovered-but-unparseable session belongs in
	// SkippedSessions, not in a count sitting next to a total that omits it.
	analysis := &models.ProjectAnalysis{
		ProjectInfo:           project,
		TotalCost:             aggregate.TotalCost,
		TotalUsage:            aggregate.TotalUsage,
		CostByModel:           aggregate.CostByModel,
		SessionCount:          aggregate.SessionCount,
		MessageCount:          aggregate.MessageCount,
		SkippedSessions:       aggregate.SkippedSessions,
		SkippedAgents:         aggregate.SkippedAgents,
		SkippedLines:          aggregate.SkippedLines,
		SkipDetails:           aggregate.SkipDetails,
		EstimatedCostMessages: aggregate.EstimatedCostMessages,
	}

	// Activity span comes from message timestamps, as it does on every other
	// surface. AnalyzeMultipleSessions already derived them; a file's mtime is
	// its LAST write, so a project of one session would otherwise span 0s.
	if !aggregate.StartTime.IsZero() {
		analysis.FirstActive = aggregate.StartTime
		analysis.LastActive = aggregate.EndTime
		return analysis, nil
	}

	// No session carried a usable message timestamp — fall back to file mtimes.
	// A window's messages all have timestamps, so with one there's nothing to
	// fall back from, and mtimes outside it would misstate the span.
	if !window.IsZero() {
		return analysis, nil
	}
	for _, session := range sessions {
		if session.Modified.After(analysis.LastActive) {
			analysis.LastActive = session.Modified
		}
		if analysis.FirstActive.IsZero() || session.Modified.Before(analysis.FirstActive) {
			analysis.FirstActive = session.Modified
		}
	}

	return analysis, nil
}

// SortProjectsBy sorts projects by the specified field
func SortProjectsBy(projects []models.ProjectAnalysis, sortBy string) {
	switch sortBy {
	case "sessions":
		sort.Slice(projects, func(i, j int) bool {
			return projects[i].SessionCount > projects[j].SessionCount
		})
	case "name":
		sort.Slice(projects, func(i, j int) bool {
			return projects[i].DisplayName < projects[j].DisplayName
		})
	case "activity":
		sort.Slice(projects, func(i, j int) bool {
			return projects[i].LastActive.After(projects[j].LastActive)
		})
	default: // "cost"
		sort.Slice(projects, func(i, j int) bool {
			return projects[i].TotalCost.TotalCost > projects[j].TotalCost.TotalCost
		})
	}
}
