package analyzer

import (
	"runtime"
	"sort"
	"sync"

	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/parser"
	"github.com/bardisty/ccusage/internal/paths"
)

// projectResult holds the result of analyzing a single project
type projectResult struct {
	analysis *models.ProjectAnalysis
	err      error
}

// AnalyzeAllProjects analyzes all projects in parallel and returns aggregated stats
func AnalyzeAllProjects(projects []models.ProjectInfo) (*models.GlobalAnalysis, error) {
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

	// Channels for work distribution
	jobs := make(chan models.ProjectInfo, len(projects))
	results := make(chan projectResult, len(projects))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for project := range jobs {
				analysis, err := analyzeProject(project)
				results <- projectResult{analysis: analysis, err: err}
			}
		}()
	}

	// Queue all projects
	for _, project := range projects {
		jobs <- project
	}
	close(jobs)

	// Wait for workers to finish and close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results and aggregate
	global := &models.GlobalAnalysis{
		CostByModel: make(map[string]models.CostBreakdown),
	}

	firstActiveSet := false
	for result := range results {
		if result.err != nil {
			global.SkippedProjects++
			continue
		}

		analysis := result.analysis
		if analysis.SessionCount == 0 {
			// Skip projects with no sessions
			continue
		}

		global.Projects = append(global.Projects, *analysis)
		global.ProjectCount++
		global.SessionCount += analysis.SessionCount
		global.MessageCount += analysis.MessageCount
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

	// Sort projects by cost descending
	sort.Slice(global.Projects, func(i, j int) bool {
		return global.Projects[i].TotalCost.TotalCost > global.Projects[j].TotalCost.TotalCost
	})

	return global, nil
}

// analyzeProject analyzes a single project directory
func analyzeProject(project models.ProjectInfo) (*models.ProjectAnalysis, error) {
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
	// summary detail view, not the global rollup)
	aggregate, _, err := AnalyzeMultipleSessions(sessions)
	if err != nil {
		return nil, err
	}

	// Build project analysis
	analysis := &models.ProjectAnalysis{
		ProjectInfo:  project,
		TotalCost:    aggregate.TotalCost,
		TotalUsage:   aggregate.TotalUsage,
		CostByModel:  aggregate.CostByModel,
		SessionCount: len(sessions),
		MessageCount: aggregate.MessageCount,
	}

	// Find first/last active times from sessions
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
