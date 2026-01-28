package models

import (
	"encoding/json"
	"time"
)

// Duration wraps time.Duration to provide custom JSON marshaling
// that outputs human-readable strings instead of nanoseconds
type Duration time.Duration

// MarshalJSON implements json.Marshaler for Duration
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON implements json.Unmarshaler for Duration
// It handles both string format ("1h30m") and numeric (nanoseconds) for backwards compatibility
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		// Numeric value - treat as nanoseconds for backwards compatibility
		*d = Duration(time.Duration(value))
		return nil
	case string:
		// String value - parse as duration string
		dur, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(dur)
		return nil
	default:
		return nil
	}
}

// Duration returns the underlying time.Duration
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// TokenUsage represents token usage from a single API call.
// Token counts use int64 to ensure no overflow when aggregating across many sessions.
type TokenUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	// Detailed cache creation breakdown
	CacheCreation *CacheCreation `json:"cache_creation,omitempty"`
}

// CacheCreation contains detailed cache write token breakdown
type CacheCreation struct {
	Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens"`
}

// CostBreakdown represents the calculated costs for a token usage
type CostBreakdown struct {
	InputCost        float64 `json:"input_cost"`
	OutputCost       float64 `json:"output_cost"`
	CacheWrite5mCost float64 `json:"cache_write_5m_cost"`
	CacheWrite1hCost float64 `json:"cache_write_1h_cost"`
	CacheReadCost    float64 `json:"cache_read_cost"`
	TotalCost        float64 `json:"total_cost"`
	CacheSavings     float64 `json:"cache_savings"`
}

// MessageAnalysis represents analysis of a single message
type MessageAnalysis struct {
	Timestamp time.Time     `json:"timestamp"`
	Model     string        `json:"model"`
	Usage     TokenUsage    `json:"usage"`
	Cost      CostBreakdown `json:"cost"`
}

// AgentAnalysis represents the analysis of a single agent sub-session
type AgentAnalysis struct {
	AgentID      string                   `json:"agent_id"`
	FullPath     string                   `json:"full_path"`
	MessageCount int                      `json:"message_count"`
	TotalUsage   TokenUsage               `json:"total_usage"`
	TotalCost    CostBreakdown            `json:"total_cost"`
	CostByModel  map[string]CostBreakdown `json:"cost_by_model"`
	StartTime    time.Time                `json:"start_time"`
	EndTime      time.Time                `json:"end_time"`
	Duration     Duration                 `json:"duration"`
}

// SessionAnalysis represents the complete analysis of a session
type SessionAnalysis struct {
	SessionID    string                   `json:"session_id"`
	ProjectPath  string                   `json:"project_path"`
	StartTime    time.Time                `json:"start_time"`
	EndTime      time.Time                `json:"end_time"`
	Duration     Duration                 `json:"duration"`
	MessageCount int                      `json:"message_count"`
	TotalUsage   TokenUsage               `json:"total_usage"`
	TotalCost    CostBreakdown            `json:"total_cost"`
	CostByModel  map[string]CostBreakdown `json:"cost_by_model"`
	Messages     []MessageAnalysis        `json:"messages,omitempty"`
	Insights     *MessageInsights         `json:"insights,omitempty"` // Cost insights (populated when messages available)
	// Last message usage for context window calculation (matches /context output)
	LastMessageUsage TokenUsage `json:"last_message_usage"`
	LastMessageModel string     `json:"last_message_model"` // Model used for last message (for context limit lookup)
	// Agent-related fields
	Agents             []AgentAnalysis          `json:"agents,omitempty"`
	ParentCost         CostBreakdown            `json:"parent_cost"`          // Cost excluding agents
	ParentCostByModel  map[string]CostBreakdown `json:"parent_cost_by_model"` // Parent cost by model (excludes agents)
	AgentsCost         CostBreakdown            `json:"agents_cost"`          // Sum of agent costs
	HasAgents          bool            `json:"has_agents"`
	AgentCount         int             `json:"agent_count"`
	ParentMessageCount int             `json:"parent_message_count"`       // Messages from parent session only
	AgentMessageCount  int             `json:"agent_message_count"`        // Messages from all agents
	SkippedAgents      int             `json:"skipped_agents,omitempty"`   // Agents that failed to parse
	SkippedSessions    int             `json:"skipped_sessions,omitempty"` // Sessions that failed to parse (for aggregates)
}

// SessionEntry represents an entry in sessions-index.json
type SessionEntry struct {
	SessionID    string    `json:"sessionId"`
	FullPath     string    `json:"fullPath"`
	MessageCount int       `json:"messageCount"`
	Created      time.Time `json:"created"`
	Modified     time.Time `json:"modified"`
	// Agent-related fields
	AgentPaths        []string `json:"agent_paths,omitempty"`
	AgentCount        int      `json:"agent_count"`
	AgentMessageCount int      `json:"agent_message_count"` // Messages from agents (for list display)
}

// SessionsIndex represents the sessions-index.json file
type SessionsIndex struct {
	Entries []SessionEntry `json:"entries"`
}

// JSONLMessage represents a message in the JSONL session file
type JSONLMessage struct {
	Type      string            `json:"type"`
	Message   *AssistantMessage `json:"message,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// AssistantMessage represents the message field for assistant type messages
type AssistantMessage struct {
	Model string     `json:"model"`
	Usage TokenUsage `json:"usage"`
}

// ContextWindowSize returns the total input tokens for a single API call,
// matching what Claude Code's /context command displays.
// This is the sum of all input token types: regular, cache write, and cache read.
func (t TokenUsage) ContextWindowSize() int64 {
	return t.InputTokens + t.CacheCreationInputTokens + t.CacheReadInputTokens
}

// Add aggregates token usage from another TokenUsage
func (t *TokenUsage) Add(other TokenUsage) {
	t.InputTokens += other.InputTokens
	t.OutputTokens += other.OutputTokens
	t.CacheCreationInputTokens += other.CacheCreationInputTokens
	t.CacheReadInputTokens += other.CacheReadInputTokens
	if other.CacheCreation != nil {
		if t.CacheCreation == nil {
			t.CacheCreation = &CacheCreation{}
		}
		t.CacheCreation.Ephemeral5mInputTokens += other.CacheCreation.Ephemeral5mInputTokens
		t.CacheCreation.Ephemeral1hInputTokens += other.CacheCreation.Ephemeral1hInputTokens
	}
}

// Add aggregates costs from another CostBreakdown
func (c *CostBreakdown) Add(other CostBreakdown) {
	c.InputCost += other.InputCost
	c.OutputCost += other.OutputCost
	c.CacheWrite5mCost += other.CacheWrite5mCost
	c.CacheWrite1hCost += other.CacheWrite1hCost
	c.CacheReadCost += other.CacheReadCost
	c.TotalCost += other.TotalCost
	c.CacheSavings += other.CacheSavings
}

// TrendDirection indicates the cost trend direction
type TrendDirection int

const (
	// TrendStable indicates costs are stable (within 20% variance)
	TrendStable TrendDirection = iota
	// TrendIncreasing indicates costs are increasing
	TrendIncreasing
	// TrendDecreasing indicates costs are decreasing
	TrendDecreasing
)

// String returns a human-readable representation of the trend direction
func (t TrendDirection) String() string {
	switch t {
	case TrendIncreasing:
		return "increasing"
	case TrendDecreasing:
		return "decreasing"
	default:
		return "stable"
	}
}

// Symbol returns a visual symbol for the trend direction
func (t TrendDirection) Symbol() string {
	switch t {
	case TrendIncreasing:
		return "▲"
	case TrendDecreasing:
		return "▼"
	default:
		return "═"
	}
}

// MessageSnapshot captures key data about a single message for insights
type MessageSnapshot struct {
	Index             int       `json:"index"` // 1-based message index
	Timestamp         time.Time `json:"timestamp"`
	Cost              float64   `json:"cost"`
	MainCostComponent string    `json:"main_cost_component"` // "cache_write", "cache_read", "output", "input"
	MainCostValue     float64   `json:"main_cost_value"`
}

// MessageInsights contains computed insights about message costs
type MessageInsights struct {
	FirstMessage *MessageSnapshot `json:"first_message,omitempty"`
	LastMessage  *MessageSnapshot `json:"last_message,omitempty"`
	HighestCost  *MessageSnapshot `json:"highest_cost,omitempty"` // nil if not notably higher than average
	CostTrend    TrendDirection   `json:"cost_trend"`
	EarlyAvgCost float64          `json:"early_avg_cost"` // Average cost of first 3 messages
	LateAvgCost  float64          `json:"late_avg_cost"`  // Average cost of last 3 messages
	AverageCost  float64          `json:"average_cost"`   // Overall average cost per message
	MessageCount int              `json:"message_count"`  // Total message count for insights
}

// CostMultiplier returns how many times above average the highest cost is
// Returns 0 if there's no notable highest cost
func (i *MessageInsights) CostMultiplier() float64 {
	if i.HighestCost == nil || i.AverageCost == 0 {
		return 0
	}
	return i.HighestCost.Cost / i.AverageCost
}

// TrendDescription returns a human-readable description of the cost trend
func (i *MessageInsights) TrendDescription() string {
	if i.MessageCount < 5 {
		return ""
	}

	switch i.CostTrend {
	case TrendIncreasing:
		return "increasing"
	case TrendDecreasing:
		return "stabilizing"
	default:
		return "stable"
	}
}

// BreakdownMessage represents a single message in the breakdown view
// combining parent and agent messages with sequential indexing
type BreakdownMessage struct {
	Index     int           // 1-based sequential index across all messages
	AgentID   string        // Empty for parent session, "1", "2", etc. for agents
	Timestamp time.Time     // Message timestamp
	Model     string        // Model used for this message
	Usage     TokenUsage    // Token usage for this message
	Cost      CostBreakdown // Calculated cost for this message
}
