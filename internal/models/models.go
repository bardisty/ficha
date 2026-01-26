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
// Note: Token counts use int which is 64-bit on modern systems but 32-bit on
// older 32-bit systems. For typical usage this is sufficient, but aggregating
// across many large sessions on 32-bit systems could theoretically overflow.
type TokenUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	// Detailed cache creation breakdown
	CacheCreation *CacheCreation `json:"cache_creation,omitempty"`
}

// CacheCreation contains detailed cache write token breakdown
type CacheCreation struct {
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
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
	SessionID     string                   `json:"session_id"`
	ProjectPath   string                   `json:"project_path"`
	StartTime     time.Time                `json:"start_time"`
	EndTime       time.Time                `json:"end_time"`
	Duration      Duration                 `json:"duration"`
	MessageCount  int                      `json:"message_count"`
	TotalUsage    TokenUsage               `json:"total_usage"`
	TotalCost     CostBreakdown            `json:"total_cost"`
	CostByModel   map[string]CostBreakdown `json:"cost_by_model"`
	Messages      []MessageAnalysis        `json:"messages,omitempty"`
	// Last message usage for context window calculation (matches /context output)
	LastMessageUsage TokenUsage `json:"last_message_usage"`
	LastMessageModel string     `json:"last_message_model"` // Model used for last message (for context limit lookup)
	// Agent-related fields
	Agents        []AgentAnalysis `json:"agents,omitempty"`
	ParentCost    CostBreakdown   `json:"parent_cost"`     // Cost excluding agents
	AgentsCost    CostBreakdown   `json:"agents_cost"`     // Sum of agent costs
	HasAgents     bool            `json:"has_agents"`
	AgentCount    int             `json:"agent_count"`
	SkippedAgents   int `json:"skipped_agents,omitempty"`   // Agents that failed to parse
	SkippedSessions int `json:"skipped_sessions,omitempty"` // Sessions that failed to parse (for aggregates)
}

// SessionEntry represents an entry in sessions-index.json
type SessionEntry struct {
	SessionID    string    `json:"sessionId"`
	FullPath     string    `json:"fullPath"`
	MessageCount int       `json:"messageCount"`
	Created      time.Time `json:"created"`
	Modified     time.Time `json:"modified"`
	// Agent-related fields
	AgentPaths []string `json:"agent_paths,omitempty"`
	AgentCount int      `json:"agent_count"`
}

// SessionsIndex represents the sessions-index.json file
type SessionsIndex struct {
	Entries []SessionEntry `json:"entries"`
}

// JSONLMessage represents a message in the JSONL session file
type JSONLMessage struct {
	Type      string          `json:"type"`
	Message   *AssistantMessage `json:"message,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// AssistantMessage represents the message field for assistant type messages
type AssistantMessage struct {
	Model string     `json:"model"`
	Usage TokenUsage `json:"usage"`
}

// ContextWindowSize returns the total input tokens for a single API call,
// matching what Claude Code's /context command displays.
// This is the sum of all input token types: regular, cache write, and cache read.
func (t TokenUsage) ContextWindowSize() int {
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
