package analyzer

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/parser"
)

// GetBreakdownMessages parses a session and returns all messages (parent + agents)
// merged chronologically with sequential indices and agent IDs assigned.
func GetBreakdownMessages(sessionPath, sessionID string) ([]models.BreakdownMessage, error) {
	// Parse parent session messages
	messages, err := parser.ParseJSONLFile(sessionPath)
	if err != nil {
		return nil, err
	}

	// Extract and calculate costs for parent messages
	parentAnalyses := parser.ExtractUsageFromMessages(messages)
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

	// Discover and process agent sub-sessions
	projectDir := filepath.Dir(sessionPath)
	agentPaths, _ := parser.DiscoverAgentSessions(projectDir, sessionID)

	// Track agents with simple sequential numbering
	agentNum := 1
	agentIDMap := make(map[string]string) // agentPath -> display ID like "1", "2"

	for _, agentPath := range agentPaths {
		agentMessages, err := parser.ParseJSONLFile(agentPath)
		if err != nil {
			continue // Skip agents that fail to parse
		}

		// Assign a display ID for this agent
		displayID := fmt.Sprintf("%d", agentNum)
		agentIDMap[agentPath] = displayID
		agentNum++

		// Extract and calculate costs for agent messages
		agentAnalyses := parser.ExtractUsageFromMessages(agentMessages)
		for i := range agentAnalyses {
			CalculateMessageCost(&agentAnalyses[i])
		}

		// Convert agent messages to breakdown format
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

	return allMessages, nil
}
