package analyzer

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/pricing"
)

// AnalyzeSession analyzes a session JSONL file and returns the complete analysis.
func AnalyzeSession(sessionPath string, sessionID string, includeMessages bool) (*models.SessionAnalysis, error) {
	return AnalyzeSessionWithCache(sessionPath, sessionID, includeMessages, nil)
}

// AnalyzeSessionWithCache is AnalyzeSession with an optional agent-parse cache.
// The parent session file is always re-parsed (it is the file being appended to
// in live views); only agent sub-sessions are served from the cache when
// unchanged. A nil cache parses every agent, matching AnalyzeSession.
func AnalyzeSessionWithCache(sessionPath string, sessionID string, includeMessages bool, cache *AgentParseCache) (*models.SessionAnalysis, error) {
	return analyzeSessionExcludingSeen(sessionPath, sessionID, includeMessages, cache, nil)
}

// analyzeSessionExcludingSeen is AnalyzeSessionWithCache with an optional
// cross-file dedup set. When seen is non-nil, parent messages whose
// message.id:requestId was already kept from an earlier file are excluded
// from this session's totals (fork/branch copies the prior transcript into a
// new session file — see ExcludeSeenMessages); the keys this session keeps
// are added to seen. Only parent-file messages participate: agent sub-session
// files are never cloned by fork, so they stay file-local. A nil seen keeps
// the session fully file-local.
func analyzeSessionExcludingSeen(sessionPath string, sessionID string, includeMessages bool, cache *AgentParseCache, seen map[string]struct{}) (*models.SessionAnalysis, error) {
	// Parse the JSONL file
	result, err := parser.ParseJSONLFileWithResult(sessionPath)
	if err != nil {
		return nil, err
	}

	messages := result.Messages
	if seen != nil {
		messages = parser.ExcludeSeenMessages(messages, seen)
	}

	// Extract usage data from messages
	messageAnalyses := parser.ExtractUsageFromMessages(messages)

	// Calculate costs for each message
	for i := range messageAnalyses {
		CalculateMessageCost(&messageAnalyses[i])
	}

	// Build the session analysis (parent session only)
	analysis := buildSessionAnalysis(sessionID, sessionPath, messageAnalyses, includeMessages)
	analysis.SkippedLines = result.SkippedLines

	// Store parent cost and message count before adding agent data
	analysis.ParentCost = analysis.TotalCost
	analysis.ParentMessageCount = analysis.MessageCount
	// Copy CostByModel before agents are merged in
	analysis.ParentCostByModel = make(map[string]models.CostBreakdown)
	for model, cost := range analysis.CostByModel {
		analysis.ParentCostByModel[model] = cost
	}

	// Discover and analyze agent sub-sessions
	projectDir := filepath.Dir(sessionPath)
	agentPaths, _ := parser.DiscoverAgentSessions(projectDir, sessionID)

	if len(agentPaths) > 0 {
		analysis.HasAgents = true
		analysis.AgentCount = len(agentPaths)
		skippedAgents := 0

		for _, agentPath := range agentPaths {
			agentAnalysis, err := analyzeAgentWithCache(agentPath, cache)
			if err != nil {
				skippedAgents++
				continue // Skip agents that can't be parsed
			}
			agentAnalysis.WorkflowID = parser.ExtractWorkflowRunID(agentPath)

			analysis.Agents = append(analysis.Agents, *agentAnalysis)

			// Roll up agent costs and messages
			analysis.AgentsCost.Add(agentAnalysis.TotalCost)
			analysis.TotalCost.Add(agentAnalysis.TotalCost)
			analysis.TotalUsage.Add(agentAnalysis.TotalUsage)
			analysis.MessageCount += agentAnalysis.MessageCount
			analysis.AgentMessageCount += agentAnalysis.MessageCount
			analysis.SkippedLines += agentAnalysis.SkippedLines

			// Merge agent cost by model
			for model, cost := range agentAnalysis.CostByModel {
				if existing, ok := analysis.CostByModel[model]; ok {
					existing.Add(cost)
					analysis.CostByModel[model] = existing
				} else {
					analysis.CostByModel[model] = cost
				}
			}

			// Extend time range if needed (parent StartTime may be zero when it
			// has no valid timestamps — take the agent's rather than keep zero)
			if !agentAnalysis.StartTime.IsZero() &&
				(analysis.StartTime.IsZero() || agentAnalysis.StartTime.Before(analysis.StartTime)) {
				analysis.StartTime = agentAnalysis.StartTime
			}
			if agentAnalysis.EndTime.After(analysis.EndTime) {
				analysis.EndTime = agentAnalysis.EndTime
			}
		}

		// Track skipped agents count for caller visibility
		analysis.SkippedAgents = skippedAgents

		// Collect workflow run metadata in first-seen agent order
		seenRuns := make(map[string]bool)
		for _, agent := range analysis.Agents {
			if agent.WorkflowID == "" || seenRuns[agent.WorkflowID] {
				continue
			}
			seenRuns[agent.WorkflowID] = true
			meta, _ := parser.ParseWorkflowMeta(projectDir, sessionID, agent.WorkflowID)
			analysis.Workflows = append(analysis.Workflows, meta)
		}
		analysis.WorkflowCount = len(analysis.Workflows)

		// Recalculate duration after including agents
		if !analysis.StartTime.IsZero() && !analysis.EndTime.IsZero() {
			analysis.Duration = models.Duration(analysis.EndTime.Sub(analysis.StartTime))
		}
	}

	return analysis, nil
}

// AnalyzeAgent analyzes a single agent sub-session. includeMessages is accepted
// for symmetry with AnalyzeSession but unused: AgentAnalysis carries aggregates
// only, never the per-message list.
func AnalyzeAgent(agentPath string, includeMessages bool) (*models.AgentAnalysis, error) {
	return analyzeAgentWithCache(agentPath, nil)
}

// analyzeAgentWithCache builds an AgentAnalysis from an agent file, serving the
// parse from cache when unchanged (nil cache always parses).
func analyzeAgentWithCache(agentPath string, cache *AgentParseCache) (*models.AgentAnalysis, error) {
	messageAnalyses, skippedLines, err := loadAgentMessages(agentPath, cache)
	if err != nil {
		return nil, err
	}

	agentID := parser.ExtractAgentID(agentPath)

	analysis := &models.AgentAnalysis{
		AgentID:      agentID,
		FullPath:     agentPath,
		MessageCount: len(messageAnalyses),
		CostByModel:  make(map[string]models.CostBreakdown),
		SkippedLines: skippedLines,
	}

	if len(messageAnalyses) == 0 {
		return analysis, nil
	}

	// Aggregate totals
	for _, msg := range messageAnalyses {
		analysis.TotalUsage.Add(msg.Usage)
		analysis.TotalCost.Add(msg.Cost)

		// Track cost by model
		model := pricing.NormalizeModelID(msg.Model)
		if existing, ok := analysis.CostByModel[model]; ok {
			existing.Add(msg.Cost)
			analysis.CostByModel[model] = existing
		} else {
			analysis.CostByModel[model] = msg.Cost
		}
	}

	// Set time range
	analysis.StartTime, analysis.EndTime = timeRange(messageAnalyses)
	analysis.Duration = models.Duration(analysis.EndTime.Sub(analysis.StartTime))

	return analysis, nil
}

// timeRange returns the earliest and latest non-zero timestamps in messages.
// Messages can be out of chronological order (resumed/interleaved sessions)
// and missing timestamps unmarshal to the zero time.Time, so positional
// first/last are unreliable. Both returns are zero if no valid timestamp exists.
func timeRange(messages []models.MessageAnalysis) (start, end time.Time) {
	for _, msg := range messages {
		if msg.Timestamp.IsZero() {
			continue
		}
		if start.IsZero() || msg.Timestamp.Before(start) {
			start = msg.Timestamp
		}
		if end.IsZero() || msg.Timestamp.After(end) {
			end = msg.Timestamp
		}
	}
	return start, end
}

// AnalyzeSessionFromMessages analyzes already-parsed messages
func AnalyzeSessionFromMessages(sessionID string, sessionPath string, messages []models.JSONLMessage, includeMessages bool) *models.SessionAnalysis {
	// Collapse repeated streaming lines — messages may come from sources that
	// bypass ParseJSONLWithResult's dedup (no-op when already deduplicated)
	messages = parser.DeduplicateMessages(messages)

	// Extract usage data from messages
	messageAnalyses := parser.ExtractUsageFromMessages(messages)

	// Calculate costs for each message
	for i := range messageAnalyses {
		CalculateMessageCost(&messageAnalyses[i])
	}

	return buildSessionAnalysis(sessionID, sessionPath, messageAnalyses, includeMessages)
}

// buildSessionAnalysis builds a SessionAnalysis from message analyses
func buildSessionAnalysis(sessionID string, sessionPath string, messageAnalyses []models.MessageAnalysis, includeMessages bool) *models.SessionAnalysis {
	analysis := &models.SessionAnalysis{
		SessionID:    sessionID,
		ProjectPath:  sessionPath,
		MessageCount: len(messageAnalyses),
		CostByModel:  make(map[string]models.CostBreakdown),
	}

	if len(messageAnalyses) == 0 {
		// Return early with zero values - no messages to analyze
		return analysis
	}

	// We have at least one message, safe to access messageAnalyses[0] and [len-1] below
	// Aggregate totals
	var totalUsage models.TokenUsage
	var totalCost models.CostBreakdown

	for _, msg := range messageAnalyses {
		totalUsage.Add(msg.Usage)
		totalCost.Add(msg.Cost)

		// Track cost by model
		model := pricing.NormalizeModelID(msg.Model)
		if existing, ok := analysis.CostByModel[model]; ok {
			existing.Add(msg.Cost)
			analysis.CostByModel[model] = existing
		} else {
			analysis.CostByModel[model] = msg.Cost
		}
	}

	analysis.TotalUsage = totalUsage
	analysis.TotalCost = totalCost

	// Set time range
	analysis.StartTime, analysis.EndTime = timeRange(messageAnalyses)
	analysis.Duration = models.Duration(analysis.EndTime.Sub(analysis.StartTime))

	// Optionally include individual messages
	if includeMessages {
		analysis.Messages = messageAnalyses
	}

	// Capture last message usage and model for context window calculation
	lastMsg := messageAnalyses[len(messageAnalyses)-1]
	analysis.LastMessageUsage = lastMsg.Usage
	analysis.LastMessageModel = lastMsg.Model

	// Calculate message insights (always computed when messages available)
	analysis.Insights = CalculateInsights(messageAnalyses)

	return analysis
}

// AnalyzeMultipleSessions analyzes multiple sessions and returns aggregate
// stats plus the per-session results (one per input entry, in input order). A
// result whose Analysis is nil failed to parse. Returning the per-session
// analyses lets the summary detail view render its breakdown without
// re-parsing every file (the aggregate already parsed them once).
//
// Sessions share a cross-file dedup set: fork/branch flows copy the prior
// transcript (billed assistant lines included) into a new session file, and
// summing file-local totals would bill those responses once per file. Files
// are processed in ascending mtime order so shared history is attributed to
// the earliest file (the original session); later copies are excluded from
// both the aggregate and their session's result, so per-session results sum
// to the aggregate. Standalone single-session views stay file-local.
func AnalyzeMultipleSessions(entries []models.SessionEntry) (*models.SessionAnalysis, []models.SessionResult, error) {
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("no sessions to analyze")
	}

	aggregate := &models.SessionAnalysis{
		SessionID:   "aggregate",
		CostByModel: make(map[string]models.CostBreakdown),
	}

	results := make([]models.SessionResult, len(entries))
	var firstTime, lastTime time.Time
	firstTimeSet := false
	skippedSessions := 0
	successfulSessions := 0
	seen := make(map[string]struct{})

	// Analyze in ascending mtime order (ties keep input order) so duplicate
	// attribution is deterministic; results stay in input order.
	order := make([]int, len(entries))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return entries[order[a]].Modified.Before(entries[order[b]].Modified)
	})

	for _, idx := range order {
		entry := entries[idx]
		sessionAnalysis, err := analyzeSessionExcludingSeen(entry.FullPath, entry.SessionID, false, nil, seen)
		if err != nil {
			skippedSessions++
			results[idx] = models.SessionResult{Entry: entry, Analysis: nil}
			continue // Skip sessions that can't be parsed
		}
		successfulSessions++
		results[idx] = models.SessionResult{Entry: entry, Analysis: sessionAnalysis}
		// Also aggregate skipped agents and lines from individual sessions
		aggregate.SkippedAgents += sessionAnalysis.SkippedAgents
		aggregate.SkippedLines += sessionAnalysis.SkippedLines

		aggregate.MessageCount += sessionAnalysis.MessageCount
		aggregate.ParentMessageCount += sessionAnalysis.ParentMessageCount
		aggregate.AgentMessageCount += sessionAnalysis.AgentMessageCount
		aggregate.TotalUsage.Add(sessionAnalysis.TotalUsage)
		aggregate.TotalCost.Add(sessionAnalysis.TotalCost)

		// Track cost by model
		for model, cost := range sessionAnalysis.CostByModel {
			if existing, ok := aggregate.CostByModel[model]; ok {
				existing.Add(cost)
				aggregate.CostByModel[model] = existing
			} else {
				aggregate.CostByModel[model] = cost
			}
		}

		// Update time range - skip sessions with zero times (no assistant messages)
		if !sessionAnalysis.StartTime.IsZero() {
			if !firstTimeSet || sessionAnalysis.StartTime.Before(firstTime) {
				firstTime = sessionAnalysis.StartTime
				firstTimeSet = true
			}
		}
		if !sessionAnalysis.EndTime.IsZero() && sessionAnalysis.EndTime.After(lastTime) {
			lastTime = sessionAnalysis.EndTime
		}
	}

	if firstTimeSet {
		aggregate.StartTime = firstTime
		aggregate.EndTime = lastTime
		aggregate.Duration = models.Duration(lastTime.Sub(firstTime))
	}

	aggregate.SkippedSessions = skippedSessions

	// Return error if all sessions failed to parse
	if successfulSessions == 0 {
		return nil, nil, fmt.Errorf("all %d sessions failed to parse", len(entries))
	}

	return aggregate, results, nil
}
