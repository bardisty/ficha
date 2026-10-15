package analyzer

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
)

// BreakdownResult holds the merged message list plus the counters that say how
// much of the session it could not account for. The breakdown surface must
// report both, or its total looks complete when it is not — the same contract
// show and summary hold with SkippedLines/SkippedAgents.
type BreakdownResult struct {
	Messages      []models.BreakdownMessage
	SkippedLines  int // JSONL lines skipped as malformed or oversized (parent + agents)
	SkippedAgents int // Agent sub-sessions that could not be read
	// Messages whose cache-write cost is a 5m-rate estimate (parent + agents;
	// see models.MessageAnalysis.EstimatedCost)
	EstimatedCostMessages int
	// Insights over the merged messages. For an agent-free session they are
	// computed in file order, the order show/watch use over the same parent
	// list, so the figures match those surfaces exactly. When agents ran they
	// are computed in time order: the trend averages the latest messages, and
	// file order (parent block, then each agent's) would make those the tail
	// of whichever agent was discovered last. The breakdown's scope label
	// discloses that the set differs from show/watch's.
	//
	// Snapshot indices are always display Indexes, since the table's row
	// numbers are the only handle a reader has to find a row.
	Insights *models.MessageInsights
	// Workflows lists the workflow runs whose agents appear in Messages,
	// ordered by their earliest message the way show and summary order runs
	// (see agentOrder): a run with no timestamped message last, ties in
	// discovery order.
	Workflows []models.WorkflowMeta
	// Tasks names each agent's task, keyed by AgentID, for the agents that
	// have one (see models.AgentAnalysis.TaskLabel).
	Tasks map[string]string
	// Title is the session's title (see parser.ParseResult.Title).
	Title string
}

// GetBreakdownMessages parses a session and returns all messages (parent + agents)
// merged chronologically with sequential indices, each agent message tagged with
// its real agent ID (parser.ExtractAgentID).
func GetBreakdownMessages(sessionPath, sessionID string) (*BreakdownResult, error) {
	return GetBreakdownMessagesWithCache(sessionPath, sessionID, nil)
}

// GetBreakdownMessagesWithCache is GetBreakdownMessages with an optional
// agent-parse cache. The parent transcript and agent sub-sessions are served
// from the cache when unchanged. A nil cache parses every file. The files it
// has to parse are read in parallel.
func GetBreakdownMessagesWithCache(sessionPath, sessionID string, cache *AgentParseCache) (*BreakdownResult, error) {
	result, agents, err := parseSession(sessionPath, sessionID, cache)
	if err != nil {
		return nil, err
	}
	skippedLines := result.SkippedLines

	// Extract and calculate costs for parent messages
	parentAnalyses := parser.ExtractUsageFromMessages(result.Messages)
	for i := range parentAnalyses {
		CalculateMessageCost(&parentAnalyses[i])
	}

	// Convert parent messages to breakdown format
	var allMessages []models.BreakdownMessage
	estimatedCostMessages := 0
	for _, msg := range parentAnalyses {
		if msg.EstimatedCost {
			estimatedCostMessages++
		}
		allMessages = append(allMessages, models.BreakdownMessage{
			AgentID:   "", // Empty for parent session
			Timestamp: msg.Timestamp,
			Model:     msg.Model,
			Usage:     msg.Usage,
			Cost:      msg.Cost,
		})
	}
	sortKeys := streamSortKeys(nil, allMessages)

	// Process agent sub-sessions. An unreadable subagents/ or
	// workflow-run directory hides agents entirely, so it counts as skipped
	// just like an agent file that fails to parse.
	projectDir := filepath.Dir(sessionPath)
	skippedAgents := agents.unreadableDirs
	var workflows []models.WorkflowMeta
	seenRuns := make(map[string]bool)
	runStart := make(map[string]time.Time) // earliest message of any of the run's agents
	tasks := make(map[string]string)

	for i, agentPath := range agents.paths {
		if agents.parses[i].err != nil {
			skippedAgents++
			continue // Skip agents that fail to parse
		}
		agentAnalyses := agents.parses[i].messages
		skippedLines += agents.parses[i].skips.Count

		// Same key space as AgentAnalysis.AgentID, so a marker in the TUI can
		// be cross-referenced against the machine outputs
		displayID := parser.ExtractAgentID(agentPath)
		if task := (models.AgentAnalysis{AgentType: agents.parses[i].agentType, Description: agents.parses[i].description}).TaskLabel(); task != "" {
			tasks[displayID] = task
		}
		// A run is listed only once one of its agents has a message, so a
		// run with no rows can't claim a run tag.
		runID := parser.ExtractWorkflowRunID(agentPath)
		if runID != "" && len(agentAnalyses) > 0 && !seenRuns[runID] {
			seenRuns[runID] = true
			meta, _ := parser.ParseWorkflowMeta(projectDir, sessionID, runID)
			workflows = append(workflows, meta)
		}
		if runID != "" {
			if start, _ := timeRange(agentAnalyses); earlierStart(start, runStart[runID]) {
				runStart[runID] = start
			}
		}

		// Convert agent messages to breakdown format (already cost-annotated by
		// loadAgentMessages)
		for _, msg := range agentAnalyses {
			if msg.EstimatedCost {
				estimatedCostMessages++
			}
			allMessages = append(allMessages, models.BreakdownMessage{
				AgentID:    displayID,
				WorkflowID: runID,
				Timestamp:  msg.Timestamp,
				Model:      msg.Model,
				Usage:      msg.Usage,
				Cost:       msg.Cost,
			})
		}
		sortKeys = streamSortKeys(sortKeys, allMessages)
	}

	// Discovery walks run directories by name, and run IDs are random.
	sort.SliceStable(workflows, func(i, j int) bool {
		return earlierStart(runStart[workflows[i].RunID], runStart[workflows[j].RunID])
	})

	// Insights are order-sensitive; see BreakdownResult.Insights for which
	// order applies. The agent-free case is computed here, before the sort.
	hasAgentRows := len(allMessages) > len(parentAnalyses)
	var insights *models.MessageInsights
	if !hasAgentRows {
		insights = CalculateInsights(breakdownAnalyses(allMessages))
	}

	// Sort all messages by time (see streamSortKeys); stable so equal keys
	// keep the deterministic append order (parent rows, then agents in
	// discovery order). Sorting positions rather than messages keeps the
	// file-order position of each row, which the insight snapshots need to
	// find their display row.
	order := make([]int, len(allMessages))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return earlierStart(sortKeys[order[i]], sortKeys[order[j]])
	})
	sorted := make([]models.BreakdownMessage, len(allMessages))
	displayIndex := make([]int, len(allMessages)) // file-order position -> 1-based display Index
	for i, pos := range order {
		sorted[i] = allMessages[pos]
		sorted[i].Index = i + 1
		displayIndex[pos] = i + 1
	}
	if hasAgentRows {
		// Computed over the display order, so indices are display Indexes already
		insights = CalculateInsights(breakdownAnalyses(sorted))
	} else {
		remapSnapshotIndices(insights, displayIndex)
	}

	return &BreakdownResult{
		Messages:              sorted,
		SkippedLines:          skippedLines,
		SkippedAgents:         skippedAgents,
		EstimatedCostMessages: estimatedCostMessages,
		Insights:              insights,
		Workflows:             workflows,
		Tasks:                 tasks,
		Title:                 result.Title,
	}, nil
}

// streamSortKeys extends keys with the sort times of msgs[len(keys):], one
// transcript's replies in file order. A reply with no timestamp takes the time
// of the next timed reply in its own transcript, or the previous one's when
// none follows, so the sort leaves it where its file put it rather than
// floating its zero time to the top. A transcript with no timestamps at all
// keeps zero keys, which earlierStart sorts after every timed row.
func streamSortKeys(keys []time.Time, msgs []models.BreakdownMessage) []time.Time {
	start := len(keys)
	for _, msg := range msgs[start:] {
		keys = append(keys, msg.Timestamp)
	}
	var next time.Time
	for i := len(keys) - 1; i >= start; i-- {
		if keys[i].IsZero() {
			keys[i] = next
		} else {
			next = keys[i]
		}
	}
	var prev time.Time
	for i := start; i < len(keys); i++ {
		if keys[i].IsZero() {
			keys[i] = prev
		} else {
			prev = keys[i]
		}
	}
	return keys
}

// breakdownAnalyses converts breakdown rows to the analyses CalculateInsights
// takes, in the same order.
func breakdownAnalyses(msgs []models.BreakdownMessage) []models.MessageAnalysis {
	out := make([]models.MessageAnalysis, len(msgs))
	for i, msg := range msgs {
		out[i] = models.MessageAnalysis{
			AgentID:   msg.AgentID,
			Timestamp: msg.Timestamp,
			Model:     msg.Model,
			Usage:     msg.Usage,
			Cost:      msg.Cost,
		}
	}
	return out
}

// remapSnapshotIndices rewrites each snapshot's 1-based file-order Index to the
// display Index of the same message. Left in file order, "Peak: #N" names
// whichever row happens to sit at position N after the time sort, which in a
// session with agents is a different message, and drifts as the parent grows.
func remapSnapshotIndices(insights *models.MessageInsights, displayIndex []int) {
	if insights == nil {
		return
	}
	for _, snap := range []*models.MessageSnapshot{insights.FirstMessage, insights.LastMessage, insights.HighestCost} {
		if snap != nil && snap.Index >= 1 && snap.Index <= len(displayIndex) {
			snap.Index = displayIndex[snap.Index-1]
		}
	}
}
