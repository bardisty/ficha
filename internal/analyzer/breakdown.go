package analyzer

import (
	"fmt"
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
}

// GetBreakdownMessages parses a session and returns all messages (parent + agents)
// merged chronologically with sequential indices and agent IDs assigned.
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
	for _, msg := range parentAnalyses {
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

	// Track agents with simple sequential numbering
	agentNum := 1
	agentIDMap := make(map[string]string) // agentPath -> display ID like "1", "2"

	for _, agentPath := range agentPaths {
		agentAnalyses, agentSkipped, err := loadAgentMessages(agentPath, cache)
		if err != nil {
			skippedAgents++
			continue // Skip agents that fail to parse
		}
		skippedLines += agentSkipped

		// Assign a display ID for this agent
		displayID := fmt.Sprintf("%d", agentNum)
		agentIDMap[agentPath] = displayID
		agentNum++

		// Convert agent messages to breakdown format (already cost-annotated by
		// loadAgentMessages)
		for _, msg := range agentAnalyses {
			allMessages = append(allMessages, models.BreakdownMessage{
				AgentID:   displayID,
				Timestamp: msg.Timestamp,
				Model:     msg.Model,
				Usage:     msg.Usage,
				Cost:      msg.Cost,
			})
		}
	}

	// Sort all messages by timestamp
	sort.Slice(allMessages, func(i, j int) bool {
		return allMessages[i].Timestamp.Before(allMessages[j].Timestamp)
	})

	// Assign sequential 1-based indices
	for i := range allMessages {
		allMessages[i].Index = i + 1
	}

	return &BreakdownResult{
		Messages:      allMessages,
		SkippedLines:  skippedLines,
		SkippedAgents: skippedAgents,
	}, nil
}
