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

// MessageScope selects which per-message rows an analysis retains in
// SessionAnalysis.Messages. Aggregate fields are unaffected — agent costs are
// always rolled into the session totals regardless of scope.
type MessageScope int

const (
	// NoMessages omits the per-message list entirely.
	NoMessages MessageScope = iota
	// AllMessages keeps the parent transcript's messages followed by each agent
	// sub-session's, tagged with MessageAnalysis.AgentID. Message costs then sum
	// to SessionAnalysis.TotalCost.
	AllMessages
)

// addCost accumulates cost under key. The zero CostBreakdown is Add's identity,
// so a missing key needs no special case.
func addCost(dst map[string]models.CostBreakdown, key string, cost models.CostBreakdown) {
	existing := dst[key]
	existing.Add(cost)
	dst[key] = existing
}

// mergeCostByModel accumulates every entry of src into dst. Both must already be
// keyed by pricing.NormalizeModelID.
func mergeCostByModel(dst, src map[string]models.CostBreakdown) {
	for model, cost := range src {
		addCost(dst, model, cost)
	}
}

// AnalyzeSession analyzes a session JSONL file and returns the complete analysis.
func AnalyzeSession(sessionPath string, sessionID string, scope MessageScope) (*models.SessionAnalysis, error) {
	return AnalyzeSessionWithCache(sessionPath, sessionID, scope, nil)
}

// AnalyzeSessionWithCache is AnalyzeSession with an optional agent-parse cache.
// The parent session file is always re-parsed (it is the file being appended to
// in live views); only agent sub-sessions are served from the cache when
// unchanged. A nil cache parses every agent, matching AnalyzeSession.
func AnalyzeSessionWithCache(sessionPath string, sessionID string, scope MessageScope, cache *AgentParseCache) (*models.SessionAnalysis, error) {
	return analyzeSessionExcludingSeen(sessionPath, sessionID, scope, cache, nil)
}

// analyzeSessionExcludingSeen is AnalyzeSessionWithCache with an optional
// cross-file dedup set. When seen is non-nil, parent messages whose
// message.id:requestId was already kept from an earlier file are excluded
// from this session's totals (fork/branch copies the prior transcript into a
// new session file — see ExcludeSeenMessages); the keys this session keeps
// are added to seen. Only parent-file messages participate: agent sub-session
// files are never cloned by fork, so they stay file-local. A nil seen keeps
// the session fully file-local.
func analyzeSessionExcludingSeen(sessionPath string, sessionID string, scope MessageScope, cache *AgentParseCache, seen map[parser.DedupKey]struct{}) (*models.SessionAnalysis, error) {
	result, err := parser.ParseJSONLFileWithResult(sessionPath)
	if err != nil {
		return nil, err
	}
	return analyzeParsedSession(result, sessionPath, sessionID, scope, cache, seen, models.TimeWindow{}), nil
}

// analyzeParsedSession is analyzeSessionExcludingSeen after the parent parse.
// AnalyzeMultipleSessions parses every parent before analyzing any of them
// (processing order derives from the parsed timestamps), so it hands the
// result in rather than parse twice.
//
// A non-zero window counts only the messages timestamped inside it, parent
// and agents alike, so a session that straddles a bound splits at it.
// Agents with nothing inside it are left out.
func analyzeParsedSession(result *parser.ParseResult, sessionPath string, sessionID string, scope MessageScope, cache *AgentParseCache, seen map[parser.DedupKey]struct{}, window models.TimeWindow) *models.SessionAnalysis {
	messages := result.Messages
	if seen != nil {
		messages = parser.ExcludeSeenMessages(messages, seen)
	}
	if !window.IsZero() {
		var inside []models.JSONLMessage
		for _, m := range messages {
			if window.Contains(m.Timestamp) {
				inside = append(inside, m)
			}
		}
		messages = inside
	}

	// Extract usage data from messages
	messageAnalyses := parser.ExtractUsageFromMessages(messages)

	// Calculate costs for each message
	for i := range messageAnalyses {
		CalculateMessageCost(&messageAnalyses[i])
	}

	// Build the session analysis (parent session only)
	analysis := buildSessionAnalysis(sessionID, sessionPath, messageAnalyses, scope != NoMessages)
	analysis.SkippedLines = result.SkippedLines
	analysis.Title = result.Title

	// Store parent cost and message count before adding agent data
	analysis.ParentCost = analysis.TotalCost
	analysis.ParentMessageCount = analysis.MessageCount
	// Copy CostByModel before agents are merged in
	analysis.ParentCostByModel = make(map[string]models.CostBreakdown)
	for model, cost := range analysis.CostByModel {
		analysis.ParentCostByModel[model] = cost
	}

	// Discover and analyze agent sub-sessions. An unreadable subagents/ or
	// workflow-run directory hides agents we will never see, so it lands in
	// SkippedAgents exactly as an unparseable agent file does — otherwise the
	// missing spend looks like a session that simply had no agents.
	projectDir := filepath.Dir(sessionPath)
	agentPaths, unreadableAgentDirs := parser.DiscoverAgentSessions(projectDir, sessionID)
	analysis.SkippedAgents = unreadableAgentDirs

	if len(agentPaths) > 0 {
		analysis.HasAgents = true

		for _, agentPath := range agentPaths {
			agentAnalysis, agentMessages, err := analyzeAgentWithCache(agentPath, cache, window)
			if err != nil {
				analysis.SkippedAgents++
				continue // Skip agents that can't be parsed
			}
			if !window.IsZero() && agentAnalysis.MessageCount == 0 {
				continue
			}
			agentAnalysis.WorkflowID = parser.ExtractWorkflowRunID(agentPath)

			analysis.Agents = append(analysis.Agents, *agentAnalysis)

			// Agent rows follow the parent block, each agent in discovery order
			// (regular agents, then workflow runs alphabetically). Tag copies:
			// agentMessages may be the parse cache's own slice, and the struct
			// copy is shallow — Usage.CacheCreation is a pointer into it.
			if scope == AllMessages {
				for _, msg := range agentMessages {
					msg.AgentID = agentAnalysis.AgentID
					if msg.Usage.CacheCreation != nil {
						cc := *msg.Usage.CacheCreation
						msg.Usage.CacheCreation = &cc
					}
					analysis.Messages = append(analysis.Messages, msg)
				}
			}

			// Roll up agent costs and messages
			analysis.AgentsCost.Add(agentAnalysis.TotalCost)
			analysis.TotalCost.Add(agentAnalysis.TotalCost)
			analysis.TotalUsage.Add(agentAnalysis.TotalUsage)
			analysis.MessageCount += agentAnalysis.MessageCount
			analysis.AgentMessageCount += agentAnalysis.MessageCount
			analysis.SkippedLines += agentAnalysis.SkippedLines
			analysis.EstimatedCostMessages += agentAnalysis.EstimatedCostMessages

			// Merge agent cost by model
			mergeCostByModel(analysis.CostByModel, agentAnalysis.CostByModel)

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

		// Count the agents actually analyzed, so AgentCount describes the same
		// set as Agents (and the rows every exporter derives from it). The
		// agents discovery found but could not read are in SkippedAgents.
		analysis.AgentCount = len(analysis.Agents)

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
		for i := range analysis.Workflows {
			for _, agent := range analysis.Agents {
				if agent.WorkflowID == analysis.Workflows[i].RunID {
					analysis.Workflows[i].Cost += agent.TotalCost.TotalCost
				}
			}
		}
		analysis.WorkflowCount = len(analysis.Workflows)
		// Every agent fell outside the window.
		if len(analysis.Agents) == 0 && analysis.SkippedAgents == 0 {
			analysis.HasAgents = false
		}

		// Recalculate duration after including agents
		if !analysis.StartTime.IsZero() && !analysis.EndTime.IsZero() {
			analysis.Duration = models.Duration(analysis.EndTime.Sub(analysis.StartTime))
		}
	}

	return analysis
}

// AnalyzeAgent analyzes a single agent sub-session. There is no message-scope
// knob: AgentAnalysis carries aggregates only, never the per-message list.
func AnalyzeAgent(agentPath string) (*models.AgentAnalysis, error) {
	analysis, _, err := analyzeAgentWithCache(agentPath, nil, models.TimeWindow{})
	return analysis, err
}

// analyzeAgentWithCache builds an AgentAnalysis from an agent file, serving the
// parse from cache when unchanged (nil cache always parses). The returned
// messages back the caller's per-message list; they may alias the cache's
// slice, so copy before mutating an element.
func analyzeAgentWithCache(agentPath string, cache *AgentParseCache, window models.TimeWindow) (*models.AgentAnalysis, []models.MessageAnalysis, error) {
	messageAnalyses, skippedLines, err := loadAgentMessages(agentPath, cache)
	if err != nil {
		return nil, nil, err
	}
	if !window.IsZero() {
		// A new slice: messageAnalyses may be the cache's own.
		var inside []models.MessageAnalysis
		for _, m := range messageAnalyses {
			if window.Contains(m.Timestamp) {
				inside = append(inside, m)
			}
		}
		messageAnalyses = inside
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
		return analysis, messageAnalyses, nil
	}

	// Aggregate totals
	for _, msg := range messageAnalyses {
		analysis.TotalUsage.Add(msg.Usage)
		analysis.TotalCost.Add(msg.Cost)
		addCost(analysis.CostByModel, pricing.NormalizeModelID(msg.Model), msg.Cost)
		if msg.EstimatedCost {
			analysis.EstimatedCostMessages++
		}
	}

	// Set time range
	analysis.StartTime, analysis.EndTime = timeRange(messageAnalyses)
	analysis.Duration = models.Duration(analysis.EndTime.Sub(analysis.StartTime))

	return analysis, messageAnalyses, nil
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

// earliestMessageTime returns the earliest non-zero timestamp in messages, or
// the zero time when none carries one. Messages can be out of chronological
// order (see timeRange), so positional first is unreliable.
func earliestMessageTime(messages []models.JSONLMessage) time.Time {
	var earliest time.Time
	for _, msg := range messages {
		if msg.Timestamp.IsZero() {
			continue
		}
		if earliest.IsZero() || msg.Timestamp.Before(earliest) {
			earliest = msg.Timestamp
		}
	}
	return earliest
}

// createdOrModified returns the entry's Created time, falling back to Modified
// when the index never supplied one — a zero Created must not outrank entries
// with real creation times.
func createdOrModified(entry models.SessionEntry) time.Time {
	if entry.Created.IsZero() {
		return entry.Modified
	}
	return entry.Created
}

// AnalyzeSessionFromMessages analyzes already-parsed messages. It discovers no
// agent sub-sessions, so includeMessages retains the parent transcript only —
// there is no AllMessages equivalent here.
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
		SessionFile:  sessionPath,
		MessageCount: len(messageAnalyses),
		CostByModel:  make(map[string]models.CostBreakdown),
		// A per-session analysis covers exactly one session. The summary
		// aggregate is a separate struct that carries its own skip-adjusted
		// count (see AnalyzeMultipleSessions).
		SessionCount: 1,
	}
	// The transcript lives directly under its project dir, so its parent is the
	// project path (same derivation used for agent discovery above). Guard the
	// empty path: filepath.Dir("") is ".", and the exported
	// AnalyzeSessionFromMessages permits an empty sessionPath.
	if sessionPath != "" {
		analysis.ProjectPath = filepath.Dir(sessionPath)
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
		addCost(analysis.CostByModel, pricing.NormalizeModelID(msg.Model), msg.Cost)
		if msg.EstimatedCost {
			analysis.EstimatedCostMessages++
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

	// Capture last-message usage and model for context-window display. Claude
	// Code writes synthetic API-error lines (model "<synthetic>", all-zero
	// usage) that can end a transcript; taking the positional last message
	// would zero out the CONTEXT readout even though real context exists. Walk
	// back to the last message that actually carries context, falling back to
	// the positional last only when none does (an all-synthetic session still
	// degrades to zero). Synthetic lines stay counted in MessageCount/dedup/cost.
	lastMsg := messageAnalyses[len(messageAnalyses)-1]
	for i := len(messageAnalyses) - 1; i >= 0; i-- {
		if messageAnalyses[i].Usage.ContextWindowSize() > 0 {
			lastMsg = messageAnalyses[i]
			break
		}
	}
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
// are processed in ascending earliest-message-timestamp order (session
// Created time, then input order, break ties — see the sort below) so shared
// history is attributed to the original session; later copies are excluded
// from both the aggregate and their session's result, so per-session results
// sum to the aggregate. Standalone single-session views stay file-local.
//
// The aggregate carries the parent/agent cost partition (ParentCost, AgentsCost,
// AgentCount, ...) summed across sessions, but not the nested Agents/Workflows
// records — those stay on the per-session results the summary detail view
// renders. ParentCost + AgentsCost recovers TotalCost, as it does per session,
// up to float rounding: the total accumulates ((parent + agent1) + agent2)
// while the partition sums parent + (agent1 + agent2). Reconcile with a
// tolerance, never with ==.
func AnalyzeMultipleSessions(entries []models.SessionEntry) (*models.SessionAnalysis, []models.SessionResult, error) {
	return AnalyzeMultipleSessionsInWindow(entries, models.TimeWindow{})
}

// AnalyzeMultipleSessionsInWindow is AnalyzeMultipleSessions counting only
// the messages inside window (see analyzeParsedSession). A session with
// nothing inside it drops out of results and the counts, as if it weren't
// there, unless it has lines or agents that couldn't be read, whose
// messages might have been. The aggregate carries the window. Every session
// falling outside it isn't an error: the aggregate is then empty.
func AnalyzeMultipleSessionsInWindow(entries []models.SessionEntry, window models.TimeWindow) (*models.SessionAnalysis, []models.SessionResult, error) {
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("no sessions to analyze")
	}

	aggregate := &models.SessionAnalysis{
		SessionID:         "aggregate",
		CostByModel:       make(map[string]models.CostBreakdown),
		ParentCostByModel: make(map[string]models.CostBreakdown),
		// Every entry resolves to the one project directory (all discovered under
		// it); the aggregate names that project, matching what each per-session
		// result derives from its own transcript path. SessionFile stays empty —
		// the aggregate spans many files. entries is non-empty (checked above).
		ProjectPath: filepath.Dir(entries[0].FullPath),
	}

	results := make([]models.SessionResult, len(entries))
	var firstTime, lastTime time.Time
	firstTimeSet := false
	skippedSessions := 0
	successfulSessions := 0
	parsedSessions := 0
	outside := make([]bool, len(entries))
	seen := make(map[parser.DedupKey]struct{})

	// Parse every parent up front: processing order derives from each
	// session's earliest message timestamp, which only the parse can provide.
	// Parsed messages carry usage metadata, not content, so holding them all
	// is cheap. A nil result failed to parse; its sort key falls back to the
	// file mtime (position is moot — it consumes no dedup keys).
	parsed := make([]*parser.ParseResult, len(entries))
	sortKeys := make([]time.Time, len(entries))
	for i, entry := range entries {
		sortKeys[i] = entry.Modified
		result, err := parser.ParseJSONLFileWithResult(entry.FullPath)
		if err != nil {
			continue
		}
		parsed[i] = result
		if ts := earliestMessageTime(result.Messages); !ts.IsZero() {
			sortKeys[i] = ts
		}
	}

	// Analyze in ascending earliest-timestamp order so shared history is
	// attributed to the original session, not the file written to last. A
	// fork clones the original transcript verbatim, timestamps included, so
	// within a fork family the primary keys tie; the entry's Created time
	// (genuine session creation when the index supplied it) breaks the tie —
	// the original predates its forks. Disk-only entries carry Created ==
	// Modified, so without the index attribution degrades to mtime order.
	// Remaining ties keep input order; results stay in input order.
	order := make([]int, len(entries))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		if !sortKeys[order[a]].Equal(sortKeys[order[b]]) {
			return sortKeys[order[a]].Before(sortKeys[order[b]])
		}
		return createdOrModified(entries[order[a]]).Before(createdOrModified(entries[order[b]]))
	})

	for _, idx := range order {
		entry := entries[idx]
		if parsed[idx] == nil {
			// The parent transcript could not be read, so its agent
			// sub-sessions — readable or not — were never analyzed and their
			// spend is excluded. Disclose them as skipped: `list` counts
			// their messages independently of the parent, and a zero
			// SkippedAgents would present the excluded spend as a session
			// that simply had no agents.
			agentPaths, unreadableDirs := parser.DiscoverAgentSessions(filepath.Dir(entry.FullPath), entry.SessionID)
			aggregate.SkippedAgents += len(agentPaths) + unreadableDirs
			skippedSessions++
			results[idx] = models.SessionResult{Entry: entry, Analysis: nil}
			continue
		}
		sessionAnalysis := analyzeParsedSession(parsed[idx], entry.FullPath, entry.SessionID, NoMessages, nil, seen, window)
		parsedSessions++
		if !window.IsZero() && sessionAnalysis.MessageCount == 0 && sessionAnalysis.SkippedLines == 0 && sessionAnalysis.SkippedAgents == 0 {
			results[idx] = models.SessionResult{Entry: entry, Analysis: nil}
			outside[idx] = true
			continue
		}
		successfulSessions++
		results[idx] = models.SessionResult{Entry: entry, Analysis: sessionAnalysis}
		// Also aggregate skipped agents and lines from individual sessions
		aggregate.SkippedAgents += sessionAnalysis.SkippedAgents
		aggregate.SkippedLines += sessionAnalysis.SkippedLines
		aggregate.EstimatedCostMessages += sessionAnalysis.EstimatedCostMessages

		aggregate.MessageCount += sessionAnalysis.MessageCount
		aggregate.ParentMessageCount += sessionAnalysis.ParentMessageCount
		aggregate.AgentMessageCount += sessionAnalysis.AgentMessageCount
		aggregate.TotalUsage.Add(sessionAnalysis.TotalUsage)
		aggregate.TotalCost.Add(sessionAnalysis.TotalCost)

		// Parent/agent cost partition, mirroring the message-count fields
		aggregate.ParentCost.Add(sessionAnalysis.ParentCost)
		aggregate.AgentsCost.Add(sessionAnalysis.AgentsCost)
		aggregate.AgentCount += sessionAnalysis.AgentCount
		aggregate.WorkflowCount += sessionAnalysis.WorkflowCount
		if sessionAnalysis.HasAgents {
			aggregate.HasAgents = true
		}

		// Track cost by model
		mergeCostByModel(aggregate.CostByModel, sessionAnalysis.CostByModel)
		mergeCostByModel(aggregate.ParentCostByModel, sessionAnalysis.ParentCostByModel)

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
	// Count only the sessions the totals actually cover — an unparseable
	// session lands in SkippedSessions, so pairing it into the count would sit
	// an inclusive number next to an exclusive total (global's per-project
	// session_count subtracts the same way). Set here so any marshal of the
	// aggregate carries a truthful session_count, not a zero value.
	aggregate.SessionCount = successfulSessions

	// Return error if all sessions failed to parse
	if parsedSessions == 0 {
		return nil, nil, fmt.Errorf("all %d sessions failed to parse", len(entries))
	}

	if !window.IsZero() {
		aggregate.Window = &window
		kept := results[:0]
		for i, r := range results {
			if !outside[i] {
				kept = append(kept, r)
			}
		}
		results = kept
	}

	return aggregate, results, nil
}

// SkipDetails lists the sessions in results that contributed to the skip
// counters, in results order. A session whose transcript failed to parse is
// Unreadable, and its agents count as skipped, as they do in
// AnalyzeMultipleSessions' SkippedAgents, since none of them was analyzed.
func SkipDetails(results []models.SessionResult) []models.SkipDetail {
	var details []models.SkipDetail
	for _, r := range results {
		if r.Analysis == nil {
			agentPaths, unreadableDirs := parser.DiscoverAgentSessions(filepath.Dir(r.Entry.FullPath), r.Entry.SessionID)
			details = append(details, models.SkipDetail{
				SessionID:  r.Entry.SessionID,
				Unreadable: true,
				Agents:     len(agentPaths) + unreadableDirs,
			})
			continue
		}
		if r.Analysis.SkippedLines > 0 || r.Analysis.SkippedAgents > 0 {
			details = append(details, models.SkipDetail{
				SessionID: r.Entry.SessionID,
				Lines:     r.Analysis.SkippedLines,
				Agents:    r.Analysis.SkippedAgents,
			})
		}
	}
	return details
}
