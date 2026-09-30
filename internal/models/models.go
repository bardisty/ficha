package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration wraps time.Duration to provide custom JSON marshaling
// that outputs human-readable strings instead of nanoseconds
type Duration time.Duration

// MarshalJSON implements json.Marshaler for Duration
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// Seconds is the duration in seconds, for machine output.
func (d Duration) Seconds() float64 {
	return time.Duration(d).Seconds()
}

// MachineTime is how json and csv write a time: UTC, whole seconds, Z
// ("2026-09-29T17:18:43Z"), the one layout jq's fromdate reads. The program
// keeps full-precision local times, which ordering and dedup rely on, and
// converts only when encoding. Two messages in the same second print alike.
func MachineTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// The MarshalJSON methods below write each time field through MachineTime.
// A field declared in the outer struct shadows the embedded one of the same
// json name, and the alias type drops the method so marshaling it doesn't
// recurse.
//
// The types that carry a Duration also add duration_seconds beside it: the
// duration marshals as a Go duration string ("3h12m5s") that jq can't do
// arithmetic on.

// MarshalJSON writes the session's times through MachineTime and adds
// duration_seconds.
func (s SessionAnalysis) MarshalJSON() ([]byte, error) {
	type plain SessionAnalysis
	return json.Marshal(struct {
		plain
		StartTime       string  `json:"start_time"`
		EndTime         string  `json:"end_time"`
		DurationSeconds float64 `json:"duration_seconds"`
	}{plain(s), MachineTime(s.StartTime), MachineTime(s.EndTime), s.Duration.Seconds()})
}

// MarshalJSON writes the agent's times through MachineTime and adds
// duration_seconds.
func (a AgentAnalysis) MarshalJSON() ([]byte, error) {
	type plain AgentAnalysis
	return json.Marshal(struct {
		plain
		StartTime       string  `json:"start_time"`
		EndTime         string  `json:"end_time"`
		DurationSeconds float64 `json:"duration_seconds"`
	}{plain(a), MachineTime(a.StartTime), MachineTime(a.EndTime), a.Duration.Seconds()})
}

// MarshalJSON writes the global span through MachineTime and adds
// duration_seconds.
func (g GlobalAnalysis) MarshalJSON() ([]byte, error) {
	type plain GlobalAnalysis
	return json.Marshal(struct {
		plain
		FirstActive     string  `json:"first_active"`
		LastActive      string  `json:"last_active"`
		DurationSeconds float64 `json:"duration_seconds"`
	}{plain(g), MachineTime(g.FirstActive), MachineTime(g.LastActive), g.Duration.Seconds()})
}

// MarshalJSON writes the project's span through MachineTime.
func (p ProjectAnalysis) MarshalJSON() ([]byte, error) {
	type plain ProjectAnalysis
	return json.Marshal(struct {
		plain
		FirstActive string `json:"first_active"`
		LastActive  string `json:"last_active"`
	}{plain(p), MachineTime(p.FirstActive), MachineTime(p.LastActive)})
}

// MarshalJSON writes the message's timestamp through MachineTime.
func (m MessageAnalysis) MarshalJSON() ([]byte, error) {
	type plain MessageAnalysis
	return json.Marshal(struct {
		plain
		Timestamp string `json:"timestamp"`
	}{plain(m), MachineTime(m.Timestamp)})
}

// MarshalJSON writes the snapshot's timestamp through MachineTime.
func (m MessageSnapshot) MarshalJSON() ([]byte, error) {
	type plain MessageSnapshot
	return json.Marshal(struct {
		plain
		Timestamp string `json:"timestamp"`
	}{plain(m), MachineTime(m.Timestamp)})
}

// MarshalJSON writes each bound through MachineTime, and leaves out an open
// one. A relative bound (--since 2h) loses its fraction of a second.
func (w TimeWindow) MarshalJSON() ([]byte, error) {
	var out struct {
		Since string `json:"since,omitempty"`
		Until string `json:"until,omitempty"`
	}
	if !w.Since.IsZero() {
		out.Since = MachineTime(w.Since)
	}
	if !w.Until.IsZero() {
		out.Until = MachineTime(w.Until)
	}
	return json.Marshal(out)
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
	case nil:
		// Null value - treat as zero duration
		*d = 0
		return nil
	default:
		return fmt.Errorf("cannot unmarshal %T into Duration", value)
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

// maxTokenField caps any single token count at a quadrillion — astronomically
// above any real API call (the largest observed is a few million) yet far below
// math.MaxInt64. Clamping each field here keeps every within-message sum of
// token fields (the cache buckets in reconcileUsage and ContextWindowSize)
// provably free of int64 overflow: a corrupt line carrying two ~6e18 buckets
// would otherwise wrap the sum negative and break the reconciled
// buckets-sum-to-flat invariant. Cross-message aggregation in Add is not bounded
// by this cap — it would still need ~9e3 clamped lines to wrap, far outside any
// real or single-line-triggered input.
const maxTokenField = 1_000_000_000_000_000

// clampTokenField clamps a raw token count into [0, maxTokenField].
func clampTokenField(n int64) int64 {
	if n < 0 {
		return 0
	}
	if n > maxTokenField {
		return maxTokenField
	}
	return n
}

// Sanitized returns a copy with each token count clamped into
// [0, maxTokenField], so a corrupt line can neither drag aggregates or costs
// negative nor overflow int64 when its fields are summed downstream. The
// CacheCreation pointer is always deep-copied, so the result is safe to mutate
// without touching the receiver's.
func (t TokenUsage) Sanitized() TokenUsage {
	t.InputTokens = clampTokenField(t.InputTokens)
	t.OutputTokens = clampTokenField(t.OutputTokens)
	t.CacheCreationInputTokens = clampTokenField(t.CacheCreationInputTokens)
	t.CacheReadInputTokens = clampTokenField(t.CacheReadInputTokens)
	if t.CacheCreation != nil {
		cc := *t.CacheCreation
		t.CacheCreation = &cc
		t.CacheCreation.Ephemeral5mInputTokens = clampTokenField(t.CacheCreation.Ephemeral5mInputTokens)
		t.CacheCreation.Ephemeral1hInputTokens = clampTokenField(t.CacheCreation.Ephemeral1hInputTokens)
	}
	return t
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
	// AgentID names the agent sub-session a message came from, joining to
	// AgentAnalysis.AgentID. Empty means the parent session's own transcript.
	AgentID   string        `json:"agent_id,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
	Model     string        `json:"model"`
	Usage     TokenUsage    `json:"usage"`
	Cost      CostBreakdown `json:"cost"`
	// EstimatedCost marks a message whose cache-write tokens were not fully
	// TTL-attributed by the session data (no cache_creation detail, or a flat
	// count its buckets don't cover); the remainder was priced at the 5m rate,
	// the cheapest write tier, so its cost is a lower-bound estimate. Surfaced
	// only as the aggregate EstimatedCostMessages counters, not per message.
	EstimatedCost bool `json:"-"`
}

// AgentAnalysis represents the analysis of a single agent sub-session
type AgentAnalysis struct {
	AgentID      string                   `json:"agent_id"`
	FullPath     string                   `json:"full_path"`
	WorkflowID   string                   `json:"workflow_id,omitempty"` // Workflow run ID; "" for regular subagents
	MessageCount int                      `json:"message_count"`
	TotalUsage   TokenUsage               `json:"total_usage"`
	TotalCost    CostBreakdown            `json:"total_cost"`
	CostByModel  map[string]CostBreakdown `json:"cost_by_model"` // Keyed by pricing.NormalizeModelID (see SessionAnalysis.CostByModel)
	StartTime    time.Time                `json:"start_time"`
	EndTime      time.Time                `json:"end_time"`
	Duration     Duration                 `json:"duration"`
	SkippedLines int                      `json:"skipped_lines,omitempty"` // JSONL lines skipped (malformed or oversized)
	SkippedAt    []SkippedLine            `json:"-"`                       // the first of those lines, and why
	// Messages whose cache-write cost is a 5m-rate estimate (see MessageAnalysis.EstimatedCost)
	EstimatedCostMessages int `json:"estimated_cost_messages,omitempty"`
	// CostByModel keys priced at a fallback rate (see SessionAnalysis)
	UnpricedModels []string `json:"unpriced_models,omitempty"`
}

// WorkflowMeta identifies a workflow run whose agents appear in a session's
// Agents list. Agents stay the unit costs aggregate from: Cost is their sum
// for this run, derived once the agents are analyzed, and never added into
// another total.
type WorkflowMeta struct {
	RunID  string `json:"run_id"`
	Name   string `json:"name,omitempty"`   // workflowName from wf_*.json; "" if unreadable
	Status string `json:"status,omitempty"` // e.g. "completed"; "" if unreadable
	// Cost is the total cost of this run's agents in the session.
	Cost float64 `json:"cost"`
}

// SessionAnalysis represents the complete analysis of a session
type SessionAnalysis struct {
	SessionID string `json:"session_id"`
	// ProjectPath names the Claude project directory the session(s) belong to
	// (~/.claude/projects/<encoded>), the value `summary -d -v` prints as
	// "Storage:". It is uniform across show, the summary aggregate, and every
	// per-session record (all derived from a transcript in that dir), so
	// machine outputs join on it. Set by the analyzer; the human table names
	// the project by Project instead.
	ProjectPath string `json:"project_path"`
	// SessionFile is the session's transcript .jsonl path. Empty on the summary
	// aggregate, which spans many files; use project_path to name the project.
	SessionFile string `json:"session_file,omitempty"`
	// Project is the project's display name ("~/source/webapp") for the
	// report header. Set by the command, which resolved the project; machine
	// output names it by project_path instead.
	Project string `json:"-"`
	// Window is the --since/--until range the analysis covers, when one was
	// given. Messages outside it aren't counted anywhere.
	Window *TimeWindow `json:"window,omitempty"`
	// Title is the session's latest "ai-title" record, raw from the
	// transcript. Empty when it has none, and on the summary aggregate.
	Title        string        `json:"title,omitempty"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time"`
	Duration     Duration      `json:"duration"`
	MessageCount int           `json:"message_count"`
	TotalUsage   TokenUsage    `json:"total_usage"`
	TotalCost    CostBreakdown `json:"total_cost"`
	// Keyed by pricing.NormalizeModelID, so every dated snapshot and provider
	// spelling of one model shares a row; unknown IDs keep their raw form.
	// MessageAnalysis.Model and LastMessageModel stay raw.
	CostByModel map[string]CostBreakdown `json:"cost_by_model"`
	// Messages carries agent rows too under analyzer.AllMessages (each tagged
	// with AgentID). Insights and the LastMessage* fields below describe the
	// parent transcript alone — don't reconcile them against this list.
	Messages []MessageAnalysis `json:"messages,omitempty"`
	Insights *MessageInsights  `json:"insights,omitempty"` // Parent-transcript cost insights
	// Last parent message usage for context window calculation (matches /context output)
	LastMessageUsage TokenUsage `json:"last_message_usage"`
	LastMessageModel string     `json:"last_message_model"` // Model used for last message (for context limit lookup)
	// Context is the table's context gauge: the last parent request against
	// its model's window. Set by the command on single sessions; nil on the
	// summary aggregate and on a session with no parent request.
	Context *ContextUsage `json:"context,omitempty"`
	// Agent-related fields
	Agents             []AgentAnalysis          `json:"agents,omitempty"`
	ParentCost         CostBreakdown            `json:"parent_cost"`          // Cost excluding agents
	ParentCostByModel  map[string]CostBreakdown `json:"parent_cost_by_model"` // Parent cost by model (excludes agents)
	AgentsCost         CostBreakdown            `json:"agents_cost"`          // Sum of agent costs
	HasAgents          bool                     `json:"has_agents"`
	AgentCount         int                      `json:"agent_count"`
	Workflows          []WorkflowMeta           `json:"workflows,omitempty"`        // Workflow runs with agents in this session
	WorkflowCount      int                      `json:"workflow_count,omitempty"`   // Distinct workflow runs
	ParentMessageCount int                      `json:"parent_message_count"`       // Messages from parent session only
	AgentMessageCount  int                      `json:"agent_message_count"`        // Messages from all agents
	SkippedAgents      int                      `json:"skipped_agents,omitempty"`   // Agent sub-sessions that could not be read (parse failure, or an unreadable agent directory)
	SkippedSessions    int                      `json:"skipped_sessions,omitempty"` // Sessions that failed to parse (for aggregates)
	SkippedLines       int                      `json:"skipped_lines,omitempty"`    // JSONL lines skipped (malformed or oversized), incl. agents
	// SkippedFiles names the transcripts behind SkippedLines, the session's
	// own first, for -v.
	SkippedFiles []FileSkips `json:"-"`
	// Messages (incl. agents) whose cache-write cost is a 5m-rate estimate
	// because the session data didn't attribute every write token to a TTL
	// (see MessageAnalysis.EstimatedCost). Zero means no cache write was
	// estimated; a model priced at the fallback rate is in UnpricedModels.
	EstimatedCostMessages int `json:"estimated_cost_messages,omitempty"`
	// UnpricedModels lists the CostByModel keys ficha has no price for, which
	// it priced at a fallback rate, sorted. Set by the command, from the same
	// check as its stderr warning.
	UnpricedModels []string `json:"unpriced_models,omitempty"`
	IsSummary      bool     `json:"-"` // True for aggregate summaries
	// SkipDetails names the sessions behind the aggregate's skip counters,
	// for -v, including any a window left out of the report. Set only on the
	// aggregate AnalyzeMultipleSessions returns.
	SkipDetails []SkipDetail `json:"-"`
	// Sessions this analysis covers: N (minus skipped) on the summary
	// aggregate, always 1 on a per-session analysis. Exported so machine
	// consumers can pair it with skipped_sessions to compute coverage, like
	// global's session_count.
	SessionCount int `json:"session_count"`
}

// ContextUsage is how full a session's context window was at its last parent
// request, as the table's gauge and Claude Code's /context show it.
type ContextUsage struct {
	Tokens  int64   `json:"tokens"`  // TokenUsage.ContextWindowSize of that request
	Window  int     `json:"window"`  // The model's context window, from ficha's catalog
	Percent float64 `json:"percent"` // Tokens / Window * 100, unrounded
}

// WorkflowByID returns the metadata for a workflow run in this session, or a
// runID-only fallback when the run isn't in the list (shouldn't happen for
// IDs taken from this session's own agents).
func (s *SessionAnalysis) WorkflowByID(runID string) WorkflowMeta {
	for _, wf := range s.Workflows {
		if wf.RunID == runID {
			return wf
		}
	}
	return WorkflowMeta{RunID: runID}
}

// TimeWindow limits an analysis to messages timestamped in [Since, Until).
// A zero bound is open, and the zero window takes everything. A message with
// no timestamp can't be placed, so a window with a bound leaves it out.
type TimeWindow struct {
	Since time.Time `json:"since,omitzero"`
	Until time.Time `json:"until,omitzero"`
}

// IsZero reports whether w takes every message.
func (w TimeWindow) IsZero() bool {
	return w.Since.IsZero() && w.Until.IsZero()
}

// Contains reports whether a message at t is in w.
func (w TimeWindow) Contains(t time.Time) bool {
	if w.IsZero() {
		return true
	}
	if t.IsZero() {
		return false
	}
	return (w.Since.IsZero() || !t.Before(w.Since)) && (w.Until.IsZero() || t.Before(w.Until))
}

// SessionResult pairs a session entry with its computed analysis. Analysis is
// nil when the session failed to parse (the summary detail view renders such
// rows as "(error)"). AnalyzeMultipleSessions returns one per input entry so
// callers can render per-session breakdowns without re-parsing each file.
type SessionResult struct {
	Entry    SessionEntry
	Analysis *SessionAnalysis
}

// SummaryDetail is the JSON shape of `summary --details`: the aggregate plus a
// per-session breakdown. Each session nests its agent sub-sessions only when
// --expand-agents is set (otherwise the agents array is omitted and the summary
// counts on each session convey the agent rollup). Sessions that failed to parse
// are excluded; the aggregate's skipped_sessions reports how many.
type SummaryDetail struct {
	Summary  *SessionAnalysis  `json:"summary"`
	Sessions []SessionAnalysis `json:"sessions"`
}

// SessionEntry represents an entry in sessions-index.json
type SessionEntry struct {
	SessionID    string    `json:"sessionId"`
	FullPath     string    `json:"fullPath"`
	MessageCount int       `json:"messageCount"`
	Created      time.Time `json:"created"`
	Modified     time.Time `json:"modified"`
	ProjectPath  string    `json:"projectPath,omitempty"` // Original project path
	// Agent-related fields
	AgentPaths        []string `json:"agent_paths,omitempty"`
	AgentCount        int      `json:"agent_count"`
	AgentMessageCount int      `json:"agent_message_count"` // Messages from agents (for list display)
	// Skip accounting for the discovery-time scan `list` performs. Zero on the
	// analysis paths, which skip the scan and do their own accounting.
	SkippedSessions int `json:"skipped_sessions,omitempty"` // 1 when this session's own transcript could not be read
	SkippedAgents   int `json:"skipped_agents,omitempty"`   // Agent sub-sessions that could not be read
	SkippedLines    int `json:"skipped_lines,omitempty"`    // JSONL lines skipped (malformed or oversized), incl. agents
	// SkippedFiles names the transcripts behind SkippedLines, the session's
	// own first, for -v. The scan fills it from the same parse as the count,
	// so the files always add up to SkippedLines.
	SkippedFiles []FileSkips `json:"-"`
}

// SessionsIndex represents the sessions-index.json file
type SessionsIndex struct {
	Entries      []SessionEntry `json:"entries"`
	OriginalPath string         `json:"originalPath,omitempty"` // Original project path (clean, not encoded)
}

// JSONLMessage represents a message in the JSONL session file
type JSONLMessage struct {
	Type      string            `json:"type"`
	Message   *AssistantMessage `json:"message,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	RequestID string            `json:"requestId,omitempty"`
	// AITitle is set on "ai-title" records, the session title Claude Code
	// generates and rewrites as the session goes on. The key is undocumented,
	// so a transcript without one is normal.
	AITitle string `json:"aiTitle,omitempty"`
}

// AssistantMessage represents the message field for assistant type messages
type AssistantMessage struct {
	ID    string     `json:"id"`
	Model string     `json:"model"`
	Usage TokenUsage `json:"usage"`
}

// ContextWindowSize returns the total input tokens for a single API call,
// matching what Claude Code's /context command displays.
// This is the sum of all input token types: regular, cache write, and cache read.
func (t TokenUsage) ContextWindowSize() int64 {
	cacheWriteTokens := t.CacheCreationInputTokens
	if t.CacheCreation != nil {
		ccSum := t.CacheCreation.Ephemeral5mInputTokens + t.CacheCreation.Ephemeral1hInputTokens
		if ccSum > cacheWriteTokens {
			cacheWriteTokens = ccSum
		}
	}
	return t.InputTokens + cacheWriteTokens + t.CacheReadInputTokens
}

// Add aggregates token usage from another TokenUsage.
//
// Add assumes reconciled inputs (parser.ExtractUsageFromMessages): when write
// tokens exist, CacheCreation is non-nil and its buckets sum to
// CacheCreationInputTokens. A hand-built usage that violates that (flat count
// without buckets, mixed with detailed ones) aggregates a flat total its
// buckets don't cover, and per-TTL displays will disagree with the flat field.
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

// MarshalText encodes the direction by name, so json carries "increasing"
// rather than an enum's position.
func (t TrendDirection) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

// UnmarshalText reads the names MarshalText writes.
func (t *TrendDirection) UnmarshalText(b []byte) error {
	switch string(b) {
	case "stable":
		*t = TrendStable
	case "increasing":
		*t = TrendIncreasing
	case "decreasing":
		*t = TrendDecreasing
	default:
		return fmt.Errorf("unknown cost trend %q", b)
	}
	return nil
}

// MessageSnapshot captures key data about a single message for insights
type MessageSnapshot struct {
	Index             int       `json:"index"` // 1-based message index
	Timestamp         time.Time `json:"timestamp"`
	Cost              float64   `json:"cost"`
	MainCostComponent string    `json:"main_cost_component"` // "input", "output", "cache_write_5m", "cache_write_1h" or "cache_read"
	MainCostValue     float64   `json:"main_cost_value"`
}

// MinMessagesForTrend is the message count at or above which
// analyzer.CalculateInsights actually computes the trend fields
// (RecentAvgCost/TrendWindow/CostTrend). Below it those fields are left at
// their zero values, so renderers must gate the trend row on HasTrend rather
// than a hardcoded count — otherwise they print a fabricated
// "$0.00/msg vs $0.00/msg flat" from never-computed zeros. The analyzer binds
// its own compute gate to this constant (see analyzer.minMessagesForTrend).
const MinMessagesForTrend = 6

// MessageInsights contains computed insights about message costs
type MessageInsights struct {
	FirstMessage *MessageSnapshot `json:"first_message,omitempty"`
	LastMessage  *MessageSnapshot `json:"last_message,omitempty"`
	HighestCost  *MessageSnapshot `json:"highest_cost,omitempty"` // nil if not notably higher than average
	CostTrend    TrendDirection   `json:"cost_trend"`
	// RecentAvgCost is the average cost of the last TrendWindow messages,
	// which CostTrend compares with AverageCost
	RecentAvgCost float64 `json:"recent_avg_cost"`
	TrendWindow   int     `json:"trend_window"`
	AverageCost   float64 `json:"average_cost"`  // Overall average cost per message
	MessageCount  int     `json:"message_count"` // Total message count for insights
}

// HasTrend reports whether a cost trend was actually computed. It is the single
// gate every trend renderer (and TrendDescription) must consult so the render
// threshold can never drift from the analyzer's compute threshold.
func (i *MessageInsights) HasTrend() bool {
	return i.MessageCount >= MinMessagesForTrend
}

// MarshalJSON leaves out cost_trend, recent_avg_cost and trend_window when no
// trend was computed. Their zero values would otherwise read as a stable
// trend over an empty window. omitempty can't do it: stable is the zero
// TrendDirection, so it would drop a real stable trend too.
func (i MessageInsights) MarshalJSON() ([]byte, error) {
	type plain MessageInsights
	if i.HasTrend() {
		return json.Marshal(plain(i))
	}
	// Nil pointers at the outer level shadow the embedded fields of the same
	// name, and omitempty drops them.
	return json.Marshal(struct {
		plain
		CostTrend     *TrendDirection `json:"cost_trend,omitempty"`
		RecentAvgCost *float64        `json:"recent_avg_cost,omitempty"`
		TrendWindow   *int            `json:"trend_window,omitempty"`
	}{plain: plain(i)})
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
	if !i.HasTrend() {
		return ""
	}

	switch i.CostTrend {
	case TrendIncreasing:
		return "rising"
	case TrendDecreasing:
		return "falling"
	default:
		return "flat"
	}
}

// BreakdownMessage represents a single message in the breakdown view
// combining parent and agent messages with sequential indexing
type BreakdownMessage struct {
	Index      int           // 1-based sequential index across all messages
	AgentID    string        // Real agent ID from agent-<id>.jsonl (same key space as AgentAnalysis.AgentID); empty for parent session
	WorkflowID string        // Workflow run ID for a workflow agent's message; empty for parent and regular subagent messages
	Timestamp  time.Time     // Message timestamp
	Model      string        // Model used for this message
	Usage      TokenUsage    // Token usage for this message
	Cost       CostBreakdown // Calculated cost for this message
}

// ProjectInfo represents a discovered Claude Code project directory
type ProjectInfo struct {
	EncodedPath  string `json:"encoded_path"`  // "-home-user-source-foo"
	FullPath     string `json:"full_path"`     // ~/.claude/projects/-home-user-source-foo
	OriginalPath string `json:"original_path"` // /home/user/source/foo: sessions-index.json's originalPath, else a transcript's cwd; "" when neither exists
	DisplayName  string `json:"display_name"`  // "~/source/foo" from OriginalPath, else the encoded name without its leading dash
}

// ProjectAnalysis represents the analysis of a single project.
//
// SessionCount counts the sessions that were successfully analyzed, so it
// describes the same set of sessions TotalCost and MessageCount do. Sessions
// discovered but not parsed are in SkippedSessions instead; the two sum to the
// number of session files on disk.
//
// FirstActive/LastActive come from message timestamps, matching the summary
// surface. A project whose sessions carry no usable timestamps falls back to
// session-file mtimes.
type ProjectAnalysis struct {
	ProjectInfo
	TotalCost    CostBreakdown            `json:"total_cost"`
	TotalUsage   TokenUsage               `json:"total_usage"`
	CostByModel  map[string]CostBreakdown `json:"cost_by_model"`
	SessionCount int                      `json:"session_count"`
	MessageCount int                      `json:"message_count"`
	FirstActive  time.Time                `json:"first_active"`
	LastActive   time.Time                `json:"last_active"`
	// Inputs this project's totals could not account for
	SkippedSessions int `json:"skipped_sessions,omitempty"` // Sessions that failed to parse
	SkippedAgents   int `json:"skipped_agents,omitempty"`   // Agent sub-sessions that could not be read
	SkippedLines    int `json:"skipped_lines,omitempty"`    // JSONL lines skipped (malformed or oversized)
	// Per-session breakdown of the skip counters, for verbose warnings.
	SkipDetails []SkipDetail `json:"-"`
	// Messages whose cache-write cost is a 5m-rate estimate (see SessionAnalysis)
	EstimatedCostMessages int `json:"estimated_cost_messages,omitempty"`
	// CostByModel keys priced at a fallback rate (see SessionAnalysis)
	UnpricedModels []string `json:"unpriced_models,omitempty"`
}

// GlobalAnalysis represents aggregated stats across all projects
type GlobalAnalysis struct {
	// Window is the --since/--until range the analysis covers, when one was
	// given (see SessionAnalysis.Window).
	Window          *TimeWindow              `json:"window,omitempty"`
	Projects        []ProjectAnalysis        `json:"projects"` // sorted by cost desc
	TotalCost       CostBreakdown            `json:"total_cost"`
	TotalUsage      TokenUsage               `json:"total_usage"`
	CostByModel     map[string]CostBreakdown `json:"cost_by_model"`
	ProjectCount    int                      `json:"project_count"`
	SessionCount    int                      `json:"session_count"` // Sessions successfully analyzed (see ProjectAnalysis)
	MessageCount    int                      `json:"message_count"`
	SkippedProjects int                      `json:"skipped_projects"`
	// Skipped inputs summed over the analyzed projects. A skipped project
	// contributes only to SkippedProjects — nothing inside it was counted.
	SkippedSessions int `json:"skipped_sessions,omitempty"` // Sessions that failed to parse
	SkippedAgents   int `json:"skipped_agents,omitempty"`   // Agent sub-sessions that could not be read
	SkippedLines    int `json:"skipped_lines,omitempty"`    // JSONL lines skipped (malformed or oversized)
	// Messages whose cache-write cost is a 5m-rate estimate (see SessionAnalysis)
	EstimatedCostMessages int `json:"estimated_cost_messages,omitempty"`
	// CostByModel keys priced at a fallback rate (see SessionAnalysis)
	UnpricedModels []string  `json:"unpriced_models,omitempty"`
	FirstActive    time.Time `json:"first_active"`
	LastActive     time.Time `json:"last_active"`
	Duration       Duration  `json:"duration"`
}
