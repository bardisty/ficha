package analyzer

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/parser"
)

// AnalyzeSession analyzes a session JSONL file and returns the complete analysis
func AnalyzeSession(sessionPath string, sessionID string, includeMessages bool) (*models.SessionAnalysis, error) {
	// Parse the JSONL file
	messages, err := parser.ParseJSONLFile(sessionPath)
	if err != nil {
		return nil, err
	}

	// Extract usage data from messages
	messageAnalyses := parser.ExtractUsageFromMessages(messages)

	// Calculate costs for each message
	for i := range messageAnalyses {
		CalculateMessageCost(&messageAnalyses[i])
	}

	// Build the session analysis (parent session only)
	analysis := buildSessionAnalysis(sessionID, sessionPath, messageAnalyses, includeMessages)

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
			agentAnalysis, err := AnalyzeAgent(agentPath, includeMessages)
			if err != nil {
				skippedAgents++
				continue // Skip agents that can't be parsed
			}

			analysis.Agents = append(analysis.Agents, *agentAnalysis)

			// Roll up agent costs and messages
			analysis.AgentsCost.Add(agentAnalysis.TotalCost)
			analysis.TotalCost.Add(agentAnalysis.TotalCost)
			analysis.TotalUsage.Add(agentAnalysis.TotalUsage)
			analysis.MessageCount += agentAnalysis.MessageCount
			analysis.AgentMessageCount += agentAnalysis.MessageCount

			// Merge agent cost by model
			for model, cost := range agentAnalysis.CostByModel {
				if existing, ok := analysis.CostByModel[model]; ok {
					existing.Add(cost)
					analysis.CostByModel[model] = existing
				} else {
					analysis.CostByModel[model] = cost
				}
			}

			// Extend time range if needed
			if !agentAnalysis.StartTime.IsZero() && agentAnalysis.StartTime.Before(analysis.StartTime) {
				analysis.StartTime = agentAnalysis.StartTime
			}
			if agentAnalysis.EndTime.After(analysis.EndTime) {
				analysis.EndTime = agentAnalysis.EndTime
			}
		}

		// Track skipped agents count for caller visibility
		analysis.SkippedAgents = skippedAgents

		// Recalculate duration after including agents
		if !analysis.StartTime.IsZero() && !analysis.EndTime.IsZero() {
			analysis.Duration = models.Duration(analysis.EndTime.Sub(analysis.StartTime))
		}
	}

	return analysis, nil
}

// AnalyzeAgent analyzes a single agent sub-session
func AnalyzeAgent(agentPath string, includeMessages bool) (*models.AgentAnalysis, error) {
	// Parse the JSONL file
	messages, err := parser.ParseJSONLFile(agentPath)
	if err != nil {
		return nil, err
	}

	// Extract usage data from messages
	messageAnalyses := parser.ExtractUsageFromMessages(messages)

	// Calculate costs for each message
	for i := range messageAnalyses {
		CalculateMessageCost(&messageAnalyses[i])
	}

	agentID := parser.ExtractAgentID(agentPath)

	analysis := &models.AgentAnalysis{
		AgentID:      agentID,
		FullPath:     agentPath,
		MessageCount: len(messageAnalyses),
		CostByModel:  make(map[string]models.CostBreakdown),
	}

	if len(messageAnalyses) == 0 {
		return analysis, nil
	}

	// Aggregate totals
	for _, msg := range messageAnalyses {
		analysis.TotalUsage.Add(msg.Usage)
		analysis.TotalCost.Add(msg.Cost)

		// Track cost by model
		if existing, ok := analysis.CostByModel[msg.Model]; ok {
			existing.Add(msg.Cost)
			analysis.CostByModel[msg.Model] = existing
		} else {
			analysis.CostByModel[msg.Model] = msg.Cost
		}
	}

	// Set time range
	analysis.StartTime = messageAnalyses[0].Timestamp
	analysis.EndTime = messageAnalyses[len(messageAnalyses)-1].Timestamp
	analysis.Duration = models.Duration(analysis.EndTime.Sub(analysis.StartTime))

	return analysis, nil
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
		if existing, ok := analysis.CostByModel[msg.Model]; ok {
			existing.Add(msg.Cost)
			analysis.CostByModel[msg.Model] = existing
		} else {
			analysis.CostByModel[msg.Model] = msg.Cost
		}
	}

	analysis.TotalUsage = totalUsage
	analysis.TotalCost = totalCost

	// Set time range
	analysis.StartTime = messageAnalyses[0].Timestamp
	analysis.EndTime = messageAnalyses[len(messageAnalyses)-1].Timestamp
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

// AnalyzeMultipleSessions analyzes multiple sessions and returns aggregate stats
func AnalyzeMultipleSessions(entries []models.SessionEntry) (*models.SessionAnalysis, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("no sessions to analyze")
	}

	aggregate := &models.SessionAnalysis{
		SessionID:   "aggregate",
		CostByModel: make(map[string]models.CostBreakdown),
	}

	var firstTime, lastTime time.Time
	firstTimeSet := false
	skippedSessions := 0
	successfulSessions := 0

	for _, entry := range entries {
		sessionAnalysis, err := AnalyzeSession(entry.FullPath, entry.SessionID, false)
		if err != nil {
			skippedSessions++
			continue // Skip sessions that can't be parsed
		}
		successfulSessions++
		// Also aggregate skipped agents from individual sessions
		aggregate.SkippedAgents += sessionAnalysis.SkippedAgents

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
		return nil, fmt.Errorf("all %d sessions failed to parse", len(entries))
	}

	return aggregate, nil
}
