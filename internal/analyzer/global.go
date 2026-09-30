package analyzer

import (
	"runtime"
	"sort"
	"sync"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/paths"
)

// projectResult holds the result of analyzing a single project
type projectResult struct {
	analysis *models.ProjectAnalysis
	err      error
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

	// Worker pool with GOMAXPROCS workers
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > len(projects) {
		numWorkers = len(projects)
	}

	// Each worker writes its result to the project's own slot. The totals
	// are summed afterwards in input order: float addition isn't
	// associative, so summing as workers finish would change the last
	// digits of the json totals from run to run.
	jobs := make(chan int, len(projects))
	results := make([]projectResult, len(projects))
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				analysis, err := analyzeProject(projects[idx], window)
				results[idx] = projectResult{analysis: analysis, err: err}
			}
		}()
	}
	for idx := range projects {
		jobs <- idx
	}
	close(jobs)
	wg.Wait()

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
	for _, result := range results {
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

// analyzeProject analyzes a single project directory
func analyzeProject(project models.ProjectInfo, window models.TimeWindow) (*models.ProjectAnalysis, error) {
	// Discover sessions on disk. AnalyzeMultipleSessions recomputes message
	// counts from its own parse below, so skip the discovery-time count scan.
	diskSessions, err := parser.DiscoverSessionsFromDisk(project.FullPath, false)
	if err != nil {
		return nil, err
	}

	// Try to load index (may not exist)
	indexPath := paths.GetSessionsIndexPath(project.FullPath)
	index, _ := parser.ParseSessionsIndex(indexPath) // Ignore error - index may not exist

	// Merge sources
	sessions, _ := parser.MergeSessionSources(index, diskSessions, project.FullPath, false)

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
	aggregate, _, err := AnalyzeMultipleSessionsInWindow(sessions, window)
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
