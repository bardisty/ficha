package analyzer

import (
	"path/filepath"
	"sort"

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
}

// GetBreakdownMessages parses a session and returns all messages (parent + agents)
// merged chronologically with sequential indices, each agent message tagged with
// its real agent ID (parser.ExtractAgentID).
func GetBreakdownMessages(sessionPath, sessionID string) (*BreakdownResult, error) {
	return GetBreakdownMessagesWithCache(sessionPath, sessionID, nil)
}

// GetBreakdownMessagesWithCache is GetBreakdownMessages with an optional
// agent-parse cache. The parent session is always re-parsed; unchanged agent
// sub-sessions are served from the cache. A nil cache parses every agent.
func GetBreakdownMessagesWithCache(sessionPath, sessionID string, cache *AgentParseCache) (*BreakdownResult, error) {
	// Parse parent session messages
	result, err := parser.ParseJSONLFileWithResult(sessionPath)
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

	// Discover and process agent sub-sessions. An unreadable subagents/ or
	// workflow-run directory hides agents entirely, so it counts as skipped
	// just like an agent file that fails to parse.
	projectDir := filepath.Dir(sessionPath)
	agentPaths, skippedAgents := parser.DiscoverAgentSessions(projectDir, sessionID)

	for _, agentPath := range agentPaths {
		agentAnalyses, agentSkipped, err := loadAgentMessages(agentPath, cache)
		if err != nil {
			skippedAgents++
			continue // Skip agents that fail to parse
		}
		skippedLines += agentSkipped

		// Same key space as AgentAnalysis.AgentID, so a marker in the TUI can
		// be cross-referenced against the machine outputs
		displayID := parser.ExtractAgentID(agentPath)

		// Convert agent messages to breakdown format (already cost-annotated by
		// loadAgentMessages)
		for _, msg := range agentAnalyses {
			if msg.EstimatedCost {
				estimatedCostMessages++
			}
			allMessages = append(allMessages, models.BreakdownMessage{
				AgentID:   displayID,
				Timestamp: msg.Timestamp,
				Model:     msg.Model,
				Usage:     msg.Usage,
				Cost:      msg.Cost,
			})
		}
	}

	// Sort all messages by timestamp; stable so equal timestamps keep the
	// deterministic append order (parent rows, then agents in discovery order)
	sort.SliceStable(allMessages, func(i, j int) bool {
		return allMessages[i].Timestamp.Before(allMessages[j].Timestamp)
	})

	// Assign sequential 1-based indices
	for i := range allMessages {
		allMessages[i].Index = i + 1
	}

	return &BreakdownResult{
		Messages:              allMessages,
		SkippedLines:          skippedLines,
		SkippedAgents:         skippedAgents,
		EstimatedCostMessages: estimatedCostMessages,
	}, nil
}
