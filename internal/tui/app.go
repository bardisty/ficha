package tui

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bardisty/ccusage/internal/analyzer"
	"github.com/bardisty/ccusage/internal/models"
	"github.com/bardisty/ccusage/internal/pricing"
	"github.com/bardisty/ccusage/internal/styles"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
)

// Highlight duration for changed values
const highlightDuration = 2 * time.Second

// Duration to show "Switched to new session" notification
const switchNotifyDuration = 5 * time.Second

// Model is the Bubbletea model for the TUI
type Model struct {
	sessionPath string
	sessionID   string
	verbose     bool
	noColor     bool

	analysis    *models.SessionAnalysis
	err         error
	loading     bool
	lastUpdated time.Time

	// Change tracking for highlight animation
	changedAt   map[string]time.Time
	deltaTokens map[string]int // Delta values for token counts
	deltaCount  int            // Delta for message count

	spinner spinner.Model
	watcher *fsnotify.Watcher
	done    chan struct{}  // Channel to signal watcher goroutine to stop
	closing *atomic.Bool   // Atomic flag for shutdown coordination
	closeOnce sync.Once    // Ensure shutdown happens exactly once
	watching *atomic.Bool  // Tracks if a watcher goroutine is active

	// Auto-follow mode for tracking new sessions
	projectDir     string          // Project directory to watch for new sessions
	followMode     bool            // Whether to auto-follow new sessions
	prevSessionID  string          // Previous session ID (shown after switch)
	sessionWatcher *SessionWatcher // Watches for new session files
	switchNotifyAt time.Time       // When session switch notification started

	width  int
	height int
}

// Messages
type (
	analysisMsg    *models.SessionAnalysis
	errorMsg       error
	fileChangedMsg struct{}
	tickMsg        time.Time
	sessionSwitchedMsg struct {
		newSessionPath string
		newSessionID   string
	}
)

// NewModel creates a new TUI model
func NewModel(sessionPath, sessionID string, verbose, noColor bool, projectDir string, followMode bool) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	closing := &atomic.Bool{}
	watching := &atomic.Bool{}
	return Model{
		sessionPath: sessionPath,
		sessionID:   sessionID,
		verbose:     verbose,
		noColor:     noColor,
		loading:     true,
		spinner:     s,
		done:        make(chan struct{}),
		closing:     closing,
		watching:    watching,
		changedAt:   make(map[string]time.Time),
		deltaTokens: make(map[string]int),
		projectDir:  projectDir,
		followMode:  followMode,
	}
}

// Init initializes the TUI
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.spinner.Tick,
		m.loadAnalysis,
		func() tea.Msg { return m.watchFile() },
		tickCmd(),
	}

	// Start session watcher if follow mode is enabled
	if m.followMode && m.projectDir != "" {
		cmds = append(cmds, m.startSessionWatcher())
	}

	return tea.Batch(cmds...)
}

// tickCmd returns a command that sends a tick every 100ms for animation
func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			// Signal shutdown using atomic flag and sync.Once
			m.closeOnce.Do(func() {
				m.closing.Store(true)
				if m.done != nil {
					close(m.done)
				}
				if m.watcher != nil {
					m.watcher.Close()
				}
				if m.sessionWatcher != nil {
					m.sessionWatcher.Stop()
				}
			})
			return m, tea.Quit
		case "r":
			m.loading = true
			return m, m.loadAnalysis
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case analysisMsg:
		// Detect changes and mark them for highlighting
		// Compare current analysis (before update) with new analysis
		if m.analysis != nil && msg != nil {
			m.detectChanges(m.analysis, msg)
		}
		m.analysis = msg
		m.loading = false
		m.lastUpdated = time.Now()
		m.err = nil

	case errorMsg:
		m.err = msg
		m.loading = false

	case watcherStartedMsg:
		// Store the watcher and start listening for file changes
		m.watcher = msg.watcher
		return m, m.waitForFileChange()

	case sessionWatcherStartedMsg:
		// Store the session watcher and start waiting for new sessions
		m.sessionWatcher = msg.watcher
		return m, m.waitForNewSession()

	case sessionWatcherRestartMsg:
		// Session was changed externally, restart waiting
		return m, m.waitForNewSession()

	case fileChangedMsg:
		m.loading = true
		// After handling the change, continue waiting for more changes
		return m, tea.Batch(m.loadAnalysis, m.waitForFileChange())

	case sessionSwitchedMsg:
		// Store previous session ID and switch to new session
		m.prevSessionID = m.sessionID
		m.sessionID = msg.newSessionID
		m.sessionPath = msg.newSessionPath
		m.switchNotifyAt = time.Now()

		// Reset analysis state for clean switch
		m.analysis = nil
		m.loading = true
		m.changedAt = make(map[string]time.Time)
		m.deltaTokens = make(map[string]int)
		m.deltaCount = 0

		// Stop old file watcher, will be restarted by watchFile
		if m.watcher != nil {
			m.watcher.Close()
			m.watcher = nil
		}

		// Update session watcher's current session
		if m.sessionWatcher != nil {
			m.sessionWatcher.SetCurrentSession(m.sessionID)
		}

		// Reload analysis, restart file watcher, and restart session watcher
		// We must explicitly restart waitForNewSession because the goroutine that
		// detected this switch has already exited after returning sessionSwitchedMsg
		cmds := []tea.Cmd{m.loadAnalysis, func() tea.Msg { return m.watchFile() }}
		if m.sessionWatcher != nil {
			cmds = append(cmds, m.waitForNewSession())
		}
		return m, tea.Batch(cmds...)

	case tickMsg:
		// Continue the animation tick for highlight fade
		return m, tickCmd()
	}

	return m, nil
}

// detectChanges compares old and new analysis and marks changed fields
func (m *Model) detectChanges(old, new *models.SessionAnalysis) {
	now := time.Now()

	// Clean up stale entries (older than 2x highlight duration)
	for field, changedTime := range m.changedAt {
		if time.Since(changedTime) > highlightDuration*2 {
			delete(m.changedAt, field)
			delete(m.deltaTokens, field)
		}
	}

	// Compare cost fields
	if old.TotalCost.InputCost != new.TotalCost.InputCost {
		m.changedAt["input_cost"] = now
	}
	if old.TotalCost.OutputCost != new.TotalCost.OutputCost {
		m.changedAt["output_cost"] = now
	}
	if old.TotalCost.CacheWrite5mCost != new.TotalCost.CacheWrite5mCost {
		m.changedAt["cache_write_5m"] = now
	}
	if old.TotalCost.CacheWrite1hCost != new.TotalCost.CacheWrite1hCost {
		m.changedAt["cache_write_1h"] = now
	}
	if old.TotalCost.CacheReadCost != new.TotalCost.CacheReadCost {
		m.changedAt["cache_read"] = now
	}
	if old.TotalCost.TotalCost != new.TotalCost.TotalCost {
		m.changedAt["total"] = now
	}
	if old.TotalCost.CacheSavings != new.TotalCost.CacheSavings {
		m.changedAt["savings"] = now
	}

	// Compare token counts with delta tracking (show only most recent change)
	if old.TotalUsage.InputTokens != new.TotalUsage.InputTokens {
		m.deltaTokens["input_tokens"] = new.TotalUsage.InputTokens - old.TotalUsage.InputTokens
		m.changedAt["input_tokens"] = now
	}
	if old.TotalUsage.OutputTokens != new.TotalUsage.OutputTokens {
		m.deltaTokens["output_tokens"] = new.TotalUsage.OutputTokens - old.TotalUsage.OutputTokens
		m.changedAt["output_tokens"] = now
	}
	if old.TotalUsage.CacheCreationInputTokens != new.TotalUsage.CacheCreationInputTokens {
		m.deltaTokens["cache_write_tokens"] = new.TotalUsage.CacheCreationInputTokens - old.TotalUsage.CacheCreationInputTokens
		m.changedAt["cache_write_tokens"] = now
	}
	if old.TotalUsage.CacheReadInputTokens != new.TotalUsage.CacheReadInputTokens {
		m.deltaTokens["cache_read_tokens"] = new.TotalUsage.CacheReadInputTokens - old.TotalUsage.CacheReadInputTokens
		m.changedAt["cache_read_tokens"] = now
	}

	// Compare message count with delta tracking (show only most recent change)
	if old.MessageCount != new.MessageCount {
		m.deltaCount = new.MessageCount - old.MessageCount
		m.changedAt["messages"] = now
	}

	// Compare cost by model
	for modelID, newCost := range new.CostByModel {
		if oldCost, exists := old.CostByModel[modelID]; !exists || oldCost.TotalCost != newCost.TotalCost {
			m.changedAt["model_"+modelID] = now
		}
	}

	// Compare context window values
	oldContextSize := old.LastMessageUsage.ContextWindowSize()
	newContextSize := new.LastMessageUsage.ContextWindowSize()
	if oldContextSize != newContextSize {
		m.changedAt["context_window"] = now
	}

	// Compare insights
	if new.Insights != nil {
		if old.Insights == nil {
			// Insights newly appeared
			m.changedAt["insights_first"] = now
			m.changedAt["insights_last"] = now
			if new.Insights.HighestCost != nil {
				m.changedAt["insights_highest"] = now
			}
			if new.Insights.MessageCount >= 5 {
				m.changedAt["insights_trend"] = now
			}
		} else {
			// Compare individual insight fields
			if old.Insights.LastMessage == nil || new.Insights.LastMessage.Cost != old.Insights.LastMessage.Cost {
				m.changedAt["insights_last"] = now
			}
			if (old.Insights.HighestCost == nil) != (new.Insights.HighestCost == nil) ||
				(new.Insights.HighestCost != nil && old.Insights.HighestCost != nil &&
					new.Insights.HighestCost.Cost != old.Insights.HighestCost.Cost) {
				m.changedAt["insights_highest"] = now
			}
			if old.Insights.LateAvgCost != new.Insights.LateAvgCost ||
				old.Insights.CostTrend != new.Insights.CostTrend {
				m.changedAt["insights_trend"] = now
			}
		}
	}

	// Compare agent breakdown
	if old.ParentCost.TotalCost != new.ParentCost.TotalCost {
		m.changedAt["parent_cost"] = now
	}
	if old.AgentsCost.TotalCost != new.AgentsCost.TotalCost {
		m.changedAt["agents_subtotal"] = now
	}
	// Track individual agent changes
	for _, newAgent := range new.Agents {
		found := false
		for _, oldAgent := range old.Agents {
			if oldAgent.AgentID == newAgent.AgentID {
				found = true
				if oldAgent.TotalCost.TotalCost != newAgent.TotalCost.TotalCost {
					m.changedAt["agent_"+newAgent.AgentID] = now
				}
				break
			}
		}
		if !found {
			// New agent appeared
			m.changedAt["agent_"+newAgent.AgentID] = now
		}
	}
}

// recentlyChanged checks if a field was recently changed (within highlight duration)
// This is used to determine whether to show delta values, regardless of color mode
func (m *Model) recentlyChanged(field string) bool {
	changedTime, exists := m.changedAt[field]
	if !exists {
		return false
	}

	elapsed := time.Since(changedTime)

	// Cleanup stale entry if it's past the highlight window
	// This prevents unbounded map growth when updates are infrequent
	if elapsed > highlightDuration*2 {
		delete(m.changedAt, field)
		delete(m.deltaTokens, field)
		return false
	}

	return elapsed < highlightDuration
}

// isHighlighted checks if a field should be highlighted with color (recently changed AND color enabled)
func (m Model) isHighlighted(field string) bool {
	if m.noColor {
		return false
	}
	return m.recentlyChanged(field)
}

// View renders the TUI
func (m Model) View() string {
	var sb strings.Builder

	// Status line: Session info + LIVE indicator + timestamp on same line
	// Format: "Session: 7de42d4f (prev: 8f026711)    ● LIVE  Updated: 19:04:02"
	sessionDisplay := truncateID(m.sessionID)
	if m.prevSessionID != "" {
		sessionDisplay = fmt.Sprintf("%s (prev: %s)", truncateID(m.sessionID), truncateID(m.prevSessionID))
	}

	// Build the status line parts
	var statusParts []string

	// Session info (de-emphasized)
	sessionInfo := fmt.Sprintf("Session: %s", sessionDisplay)
	if !m.noColor {
		sessionInfo = titleStyle.Render(sessionInfo)
	}
	statusParts = append(statusParts, sessionInfo)

	// LIVE indicator (green dot)
	liveIndicator := "● LIVE"
	if !m.noColor {
		liveIndicator = liveIndicatorStyle.Render(liveIndicator)
	}
	statusParts = append(statusParts, liveIndicator)

	// Status (loading/error/timestamp)
	var statusStr string
	if m.loading {
		if m.noColor {
			statusStr = "Loading..."
		} else {
			statusStr = m.spinner.View() + " Loading..."
		}
	} else if m.err != nil {
		statusStr = fmt.Sprintf("Error: %v", m.err)
	} else {
		statusStr = fmt.Sprintf("Updated: %s", m.lastUpdated.Format("15:04:05"))
	}
	statusParts = append(statusParts, statusStr)

	// Join with spacing
	sb.WriteString(strings.Join(statusParts, "  "))

	// Show switch notification on next line if applicable
	showSwitchNotify := !m.switchNotifyAt.IsZero() && time.Since(m.switchNotifyAt) < switchNotifyDuration
	if showSwitchNotify {
		sb.WriteString("\n")
		if m.noColor {
			sb.WriteString("[Switched to new session]")
		} else {
			sb.WriteString(lipgloss.NewStyle().Foreground(styles.HighlightColor).Bold(true).Render("Switched to new session"))
		}
	}

	sb.WriteString("\n")

	// Analysis content
	if m.analysis != nil {
		sb.WriteString(m.renderAnalysis())
	} else if m.loading {
		// Show spinner during initial load
		sb.WriteString("\n")
	}

	// Separator and help
	sb.WriteString("\n")
	separator := strings.Repeat("─", 60)
	if !m.noColor {
		separator = dimStyle.Render(separator)
	}
	sb.WriteString(separator + "\n")
	sb.WriteString(helpStyle.Render("q: quit  •  r: refresh"))

	return sb.String()
}

func (m Model) renderAnalysis() string {
	if m.noColor {
		return m.renderAnalysisPlain()
	}

	// Handle empty session state
	if m.isEmptySession() {
		return m.renderEmptyState()
	}

	var sb strings.Builder
	a := m.analysis

	// Hero total cost (centered, prominent)
	sb.WriteString("\n")
	totalHighlighted := m.isHighlighted("total")
	sb.WriteString(m.renderHeroCost(a.TotalCost.TotalCost, totalHighlighted))
	sb.WriteString("\n")

	// Unified cost+token rows
	// Input tokens
	sb.WriteString(m.renderUnifiedCostRow(
		"Input", a.TotalCost.InputCost, a.TotalUsage.InputTokens,
		"input_cost", "input_tokens", lipgloss.Color(""), ""))

	// Output tokens
	sb.WriteString(m.renderUnifiedCostRow(
		"Output", a.TotalCost.OutputCost, a.TotalUsage.OutputTokens,
		"output_cost", "output_tokens", styles.OutputTokenColor, ""))

	// Cache write rows - tokens aren't split by TTL, so show them on whichever row has cost
	// If both TTLs have cost, tokens go on the 5m row (first one shown)
	cacheWriteTokens := a.TotalUsage.CacheCreationInputTokens
	has5mCost := a.TotalCost.CacheWrite5mCost > 0
	has1hCost := a.TotalCost.CacheWrite1hCost > 0

	if has5mCost {
		// 5m row gets tokens whenever it has cost
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache write", a.TotalCost.CacheWrite5mCost, cacheWriteTokens,
			"cache_write_5m", "cache_write_tokens", styles.CacheWriteTokenColor, "5m TTL"))
	}

	if has1hCost {
		// 1h row gets tokens only if 5m row doesn't exist
		tokens := 0
		if !has5mCost {
			tokens = cacheWriteTokens
		}
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache write", a.TotalCost.CacheWrite1hCost, tokens,
			"cache_write_1h", "cache_write_tokens", styles.CacheWriteTokenColor, "1h TTL"))
	}

	// Cache read - only show if present
	if a.TotalCost.CacheReadCost > 0 || a.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache read", a.TotalCost.CacheReadCost, a.TotalUsage.CacheReadInputTokens,
			"cache_read", "cache_read_tokens", styles.CacheReadTokenColor, ""))
	}

	// Savings row (no separator - the rows above sum to hero TOTAL, not savings)
	if a.TotalCost.CacheSavings > 0 {
		savingsHighlighted := m.isHighlighted("savings")
		savingsStr := formatCostStyledGreen(a.TotalCost.CacheSavings, 11, savingsHighlighted, m.noColor)
		sb.WriteString(fmt.Sprintf("  %s %s  %s\n",
			savingsLabelStyle.Render(fmt.Sprintf("%-14s", "Savings")),
			savingsStr,
			dimStyle.Render("(from cache reads)")))
	}

	// Context window section
	sb.WriteString("\n")
	sb.WriteString(m.renderContextSection())

	// Cost by model
	sb.WriteString("\n")
	sb.WriteString(m.renderCostByModel())

	// Agent breakdown (shown when agents exist)
	if a.HasAgents {
		sb.WriteString("\n")
		sb.WriteString(m.renderAgentBreakdown())
	}

	// Message insights (shown when insights are available)
	if a.Insights != nil {
		sb.WriteString("\n")
		sb.WriteString(m.renderInsights())
	}

	// Footer with message count and duration
	sb.WriteString("\n")
	sb.WriteString(m.renderFooter())

	return sb.String()
}

func (m Model) renderAnalysisPlain() string {
	// Handle empty session state
	if m.isEmptySession() {
		return m.renderEmptyState()
	}

	var sb strings.Builder
	a := m.analysis

	// Hero total cost (centered)
	sb.WriteString("\n")
	sb.WriteString(m.renderHeroCost(a.TotalCost.TotalCost, false))
	sb.WriteString("\n")

	// Unified cost+token rows
	sb.WriteString(m.renderUnifiedCostRow(
		"Input", a.TotalCost.InputCost, a.TotalUsage.InputTokens,
		"input_cost", "input_tokens", lipgloss.Color(""), ""))

	// Output tokens
	sb.WriteString(m.renderUnifiedCostRow(
		"Output", a.TotalCost.OutputCost, a.TotalUsage.OutputTokens,
		"output_cost", "output_tokens", lipgloss.Color(""), ""))

	// Cache write rows - tokens aren't split by TTL, so show them on whichever row has cost
	cacheWriteTokens := a.TotalUsage.CacheCreationInputTokens
	has5mCost := a.TotalCost.CacheWrite5mCost > 0
	has1hCost := a.TotalCost.CacheWrite1hCost > 0

	if has5mCost {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache write", a.TotalCost.CacheWrite5mCost, cacheWriteTokens,
			"cache_write_5m", "cache_write_tokens", lipgloss.Color(""), "5m TTL"))
	}

	if has1hCost {
		tokens := 0
		if !has5mCost {
			tokens = cacheWriteTokens
		}
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache write", a.TotalCost.CacheWrite1hCost, tokens,
			"cache_write_1h", "cache_write_tokens", lipgloss.Color(""), "1h TTL"))
	}

	// Cache read
	if a.TotalCost.CacheReadCost > 0 || a.TotalUsage.CacheReadInputTokens > 0 {
		sb.WriteString(m.renderUnifiedCostRow(
			"Cache read", a.TotalCost.CacheReadCost, a.TotalUsage.CacheReadInputTokens,
			"cache_read", "cache_read_tokens", lipgloss.Color(""), ""))
	}

	// Savings row (no separator - rows above sum to hero TOTAL, not savings)
	if a.TotalCost.CacheSavings > 0 {
		sb.WriteString(fmt.Sprintf("  %-14s %s  (from cache reads)\n",
			"Savings", formatCost(a.TotalCost.CacheSavings)))
	}

	// Context window section
	sb.WriteString("\n")
	sb.WriteString(m.renderContextSection())

	// Cost by model
	sb.WriteString("\n")
	sb.WriteString(m.renderCostByModel())

	// Agent breakdown (shown when agents exist)
	if a.HasAgents {
		sb.WriteString("\n")
		sb.WriteString(m.renderAgentBreakdown())
	}

	// Message insights (shown when insights are available)
	if a.Insights != nil {
		sb.WriteString("\n")
		sb.WriteString(m.renderInsights())
	}

	// Footer
	sb.WriteString("\n")
	sb.WriteString(m.renderFooter())

	return sb.String()
}

// renderContextSection renders the context window progress bar and stats
func (m Model) renderContextSection() string {
	var sb strings.Builder

	contextSize := m.analysis.LastMessageUsage.ContextWindowSize()
	if contextSize == 0 {
		return ""
	}

	modelPricing := pricing.GetModelPricing(m.analysis.LastMessageModel)
	maxContext := modelPricing.MaxContextTokens
	contextPct := pricing.GetContextPercentage(modelPricing, contextSize)
	freeSpace := pricing.GetFreeSpace(modelPricing, contextSize)
	buffer := pricing.GetAutocompactBuffer(modelPricing)
	freePct := float64(freeSpace) / float64(maxContext) * 100

	highlighted := m.isHighlighted("context_window")
	usageColor := styles.GetContextUsageColor(contextPct)

	// Context label with value
	contextVal := formatNumber(contextSize)
	contextMeta := fmt.Sprintf("(%.0f%% of %s)", contextPct, formatNumber(maxContext))

	if m.noColor {
		sb.WriteString(fmt.Sprintf("  Context  %s  %s %s\n", formatContextProgressBar(contextSize, freeSpace, buffer, maxContext, true), contextVal, contextMeta))
		sb.WriteString(fmt.Sprintf("           Free: %s (%.1f%%)  │  Buffer: %s\n", formatNumber(freeSpace), freePct, formatNumber(buffer)))
	} else {
		// Progress bar with context info
		if highlighted {
			sb.WriteString(fmt.Sprintf("  Context  %s  %s %s\n",
				formatContextProgressBar(contextSize, freeSpace, buffer, maxContext, false),
				highlightStyle.Render(contextVal),
				dimStyle.Render(contextMeta)))
		} else {
			coloredMeta := lipgloss.NewStyle().Foreground(usageColor).Render(contextMeta)
			sb.WriteString(fmt.Sprintf("  Context  %s  %s %s\n",
				formatContextProgressBar(contextSize, freeSpace, buffer, maxContext, false),
				contextVal, coloredMeta))
		}

		// Free space and buffer info
		freeVal := formatNumber(freeSpace)
		bufferVal := formatNumber(buffer)
		freeValWithPct := fmt.Sprintf("%s (%.1f%%)", freeVal, freePct)
		var freeStyled string
		if highlighted {
			freeStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render("Free: ") + highlightStyle.Render(freeValWithPct)
		} else {
			freeStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(fmt.Sprintf("Free: %s", freeValWithPct))
		}
		bufferStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(fmt.Sprintf("Buffer: %s", bufferVal))
		sb.WriteString(fmt.Sprintf("           %s  %s  %s\n", freeStyled, dimStyle.Render("│"), bufferStyled))
	}

	return sb.String()
}

// renderFooter renders the message count and duration footer
func (m Model) renderFooter() string {
	a := m.analysis
	changed := m.recentlyChanged("messages")
	highlighted := m.isHighlighted("messages")

	var footerLine string
	if changed {
		var valStr string
		if m.deltaCount > 0 {
			valStr = fmt.Sprintf("%d (+%d)", a.MessageCount, m.deltaCount)
		} else if m.deltaCount < 0 {
			valStr = fmt.Sprintf("%d (%d)", a.MessageCount, m.deltaCount)
		} else {
			valStr = fmt.Sprintf("%d", a.MessageCount)
		}
		if highlighted {
			footerLine = footerStyle.Render("Messages: ") + highlightStyle.Render(valStr) +
				footerStyle.Render(fmt.Sprintf("  │  Duration: %s", formatDuration(a.Duration.Duration())))
		} else {
			footerLine = fmt.Sprintf("Messages: %s  │  Duration: %s", valStr, formatDuration(a.Duration.Duration()))
		}
	} else {
		footerLine = footerStyle.Render(fmt.Sprintf("Messages: %d  │  Duration: %s",
			a.MessageCount, formatDuration(a.Duration.Duration())))
	}
	return footerLine
}

func (m Model) renderCostByModel() string {
	var sb strings.Builder

	title := "Cost by Model"
	if !m.noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString("  " + title + "\n")

	// Extract and sort by cost (highest first)
	type modelCost struct {
		id   string
		cost float64
	}
	models := make([]modelCost, 0, len(m.analysis.CostByModel))
	for id, breakdown := range m.analysis.CostByModel {
		models = append(models, modelCost{id, breakdown.TotalCost})
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].cost > models[j].cost
	})

	totalCost := m.analysis.TotalCost.TotalCost
	for _, mc := range models {
		cost := m.analysis.CostByModel[mc.id]
		modelName := pricing.GetModelDisplayName(mc.id)
		highlighted := m.isHighlighted("model_" + mc.id)

		// Apply model color to the label
		var labelStr string
		if !m.noColor {
			modelColor := getModelColor(mc.id)
			labelStyle := lipgloss.NewStyle().Foreground(modelColor)
			labelStr = labelStyle.Render(fmt.Sprintf("%-18s", modelName))
		} else {
			labelStr = fmt.Sprintf("%-18s", modelName)
		}

		// Apply cost magnitude shading
		costStr := formatCostStyledWithMagnitude(cost.TotalCost, 12, highlighted, m.noColor, totalCost)
		sb.WriteString(fmt.Sprintf("    %s %s\n", labelStr, costStr))
	}

	return sb.String()
}

func (m Model) renderAgentBreakdown() string {
	var sb strings.Builder
	a := m.analysis

	title := fmt.Sprintf("Agent Sub-Sessions (%d)", a.AgentCount)
	if !m.noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString("  " + title + "\n")

	totalCost := a.TotalCost.TotalCost

	// Show parent session cost with highlight and magnitude shading
	parentHighlighted := m.isHighlighted("parent_cost")
	parentCostStr := formatCostStyledWithMagnitude(a.ParentCost.TotalCost, 12, parentHighlighted, m.noColor, totalCost)
	if !m.noColor {
		sb.WriteString(fmt.Sprintf("    %-18s %s\n", dimStyle.Render("Parent session"), parentCostStr))
	} else {
		sb.WriteString(fmt.Sprintf("    %-18s %s\n", "Parent session", parentCostStr))
	}

	// Show each agent with sequential numbering matching breakdown view [A1], [A2], etc.
	for i, agent := range a.Agents {
		agentNum := fmt.Sprintf("%d", i+1)
		agentHighlighted := m.isHighlighted("agent_" + agent.AgentID)
		costStr := formatCostStyledWithMagnitude(agent.TotalCost.TotalCost, 12, agentHighlighted, m.noColor, totalCost)

		if !m.noColor {
			// Color the [An] marker to match breakdown view
			agentColor := styles.GetAgentColor(agentNum)
			agentStyle := lipgloss.NewStyle().Foreground(agentColor)
			marker := agentStyle.Render(fmt.Sprintf("[A%s]", agentNum))
			// Show truncated raw ID in dim for reference
			shortID := agent.AgentID
			if len(shortID) > 7 {
				shortID = shortID[:7]
			}
			idRef := dimStyle.Render(fmt.Sprintf("(%s)", shortID))
			msgInfo := dimStyle.Render(fmt.Sprintf("%d msgs", agent.MessageCount))
			sb.WriteString(fmt.Sprintf("    %s %s  %s   %s\n", marker, idRef, costStr, msgInfo))
		} else {
			shortID := agent.AgentID
			if len(shortID) > 7 {
				shortID = shortID[:7]
			}
			sb.WriteString(fmt.Sprintf("    [A%s] (%s)     %s   %d msgs\n", agentNum, shortID, costStr, agent.MessageCount))
		}
	}

	// Show agents subtotal with highlight and magnitude shading
	subtotalHighlighted := m.isHighlighted("agents_subtotal")
	subtotalStr := formatCostStyledWithMagnitude(a.AgentsCost.TotalCost, 12, subtotalHighlighted, m.noColor, totalCost)
	if !m.noColor {
		sb.WriteString(fmt.Sprintf("    %-18s %s\n", dimStyle.Render("Agents subtotal"), subtotalStr))
	} else {
		sb.WriteString(fmt.Sprintf("    %-18s %s\n", "Agents subtotal", subtotalStr))
	}

	return sb.String()
}

func (m Model) renderInsights() string {
	var sb strings.Builder
	insights := m.analysis.Insights

	title := "Message Insights"
	if !m.noColor {
		title = headerStyle.Render(title)
	}
	sb.WriteString("  " + title + "\n")

	// First message
	if insights.FirstMessage != nil {
		first := insights.FirstMessage
		componentLabel := formatCostComponentLabel(first.MainCostComponent)
		highlighted := m.isHighlighted("insights_first")

		if !m.noColor {
			costStr := formatCostStyled(first.Cost, 10, highlighted, m.noColor)
			componentStr := dimStyle.Render(fmt.Sprintf("%s: %s", componentLabel, formatCost(first.MainCostValue)))
			sb.WriteString(fmt.Sprintf("    %-8s %s  (%s)  %s\n",
				"First:",
				costStr,
				first.Timestamp.Format("15:04:05"),
				componentStr))
		} else {
			sb.WriteString(fmt.Sprintf("    %-8s %s  (%s)  %s: %s\n",
				"First:",
				formatCost(first.Cost),
				first.Timestamp.Format("15:04:05"),
				componentLabel,
				formatCost(first.MainCostValue)))
		}
	}

	// Last message
	if insights.LastMessage != nil {
		last := insights.LastMessage
		componentLabel := formatCostComponentLabel(last.MainCostComponent)
		highlighted := m.isHighlighted("insights_last")

		if !m.noColor {
			costStr := formatCostStyled(last.Cost, 10, highlighted, m.noColor)
			componentStr := dimStyle.Render(fmt.Sprintf("%s: %s", componentLabel, formatCost(last.MainCostValue)))
			sb.WriteString(fmt.Sprintf("    %-8s %s  (%s)  %s\n",
				"Last:",
				costStr,
				last.Timestamp.Format("15:04:05"),
				componentStr))
		} else {
			sb.WriteString(fmt.Sprintf("    %-8s %s  (%s)  %s: %s\n",
				"Last:",
				formatCost(last.Cost),
				last.Timestamp.Format("15:04:05"),
				componentLabel,
				formatCost(last.MainCostValue)))
		}
	}

	// Highest cost (only if notably above average)
	if insights.HighestCost != nil {
		highest := insights.HighestCost
		multiplier := insights.CostMultiplier()
		warningStr := fmt.Sprintf("%.1fx avg cost", multiplier)
		highlighted := m.isHighlighted("insights_highest")

		if !m.noColor {
			costStr := formatCostStyled(highest.Cost, 10, highlighted, m.noColor)
			warningStyled := lipgloss.NewStyle().Foreground(styles.WarningColor).Render("⚠ " + warningStr)
			sb.WriteString(fmt.Sprintf("    %-8s %s  (%s)  %s\n",
				"Peak:",
				costStr,
				highest.Timestamp.Format("15:04:05"),
				warningStyled))
		} else {
			sb.WriteString(fmt.Sprintf("    %-8s %s  (%s)  ! %s\n",
				"Peak:",
				formatCost(highest.Cost),
				highest.Timestamp.Format("15:04:05"),
				warningStr))
		}
	}

	// Trend (only for sessions with 5+ messages)
	if insights.MessageCount >= 5 {
		trendDesc := insights.TrendDescription()
		trendSymbol := insights.CostTrend.Symbol()
		highlighted := m.isHighlighted("insights_trend")

		earlyStr := fmt.Sprintf("$%.2f/msg", insights.EarlyAvgCost)
		lateStr := fmt.Sprintf("$%.2f/msg", insights.LateAvgCost)

		if !m.noColor {
			// Color the trend symbol based on direction
			var symbolStyled string
			switch insights.CostTrend {
			case models.TrendIncreasing:
				symbolStyled = lipgloss.NewStyle().Foreground(styles.WarningColor).Render(trendSymbol)
			case models.TrendDecreasing:
				symbolStyled = lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(trendSymbol)
			default:
				symbolStyled = dimStyle.Render(trendSymbol)
			}
			var trendLine string
			if highlighted {
				trendLine = highlightStyle.Render(fmt.Sprintf("%s → %s", earlyStr, lateStr))
			} else {
				trendLine = fmt.Sprintf("%s → %s", earlyStr, lateStr)
			}
			sb.WriteString(fmt.Sprintf("    %-8s %s  %s %s\n",
				"Trend:",
				trendLine,
				symbolStyled,
				trendDesc))
		} else {
			sb.WriteString(fmt.Sprintf("    %-8s %s -> %s  %s %s\n",
				"Trend:",
				earlyStr,
				lateStr,
				trendSymbol,
				trendDesc))
		}
	}

	return sb.String()
}

// formatCostComponentLabel returns a human-readable label for a cost component
func formatCostComponentLabel(component string) string {
	switch component {
	case "input":
		return "input"
	case "output":
		return "output"
	case "cache_write_5m":
		return "cache_write"
	case "cache_write_1h":
		return "cache_write"
	case "cache_read":
		return "cache_read"
	default:
		return component
	}
}

// Commands

func (m Model) loadAnalysis() tea.Msg {
	analysis, err := analyzer.AnalyzeSession(m.sessionPath, m.sessionID, m.verbose)
	if err != nil {
		return errorMsg(err)
	}
	return analysisMsg(analysis)
}

// loadAnalysisCmd returns a command that loads the analysis
func (m Model) loadAnalysisCmd() tea.Cmd {
	return func() tea.Msg {
		return m.loadAnalysis()
	}
}

// watcherStartedMsg is sent when the watcher is successfully created
type watcherStartedMsg struct {
	watcher *fsnotify.Watcher
}

func (m Model) watchFile() tea.Msg {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errorMsg(err)
	}

	err = watcher.Add(m.sessionPath)
	if err != nil {
		watcher.Close()
		return errorMsg(err)
	}

	// Return the watcher to be stored in the model
	return watcherStartedMsg{watcher: watcher}
}

// waitForFileChange returns a command that waits for file changes or shutdown
// Uses a loop instead of recursion to avoid unbounded goroutine spawning
func (m Model) waitForFileChange() tea.Cmd {
	return func() tea.Msg {
		// Check shutdown flag before starting
		if m.closing != nil && m.closing.Load() {
			return nil
		}
		if m.watcher == nil || m.done == nil {
			return nil
		}

		// Ensure only one watcher goroutine runs at a time
		// If another is already watching, exit early
		if m.watching != nil && !m.watching.CompareAndSwap(false, true) {
			return nil
		}
		// Mark as not watching when this goroutine exits
		defer func() {
			if m.watching != nil {
				m.watching.Store(false)
			}
		}()

		// Loop within this goroutine to avoid recursive goroutine creation
		for {
			select {
			case <-m.done:
				// Shutdown signal received
				return nil
			case event, ok := <-m.watcher.Events:
				if !ok {
					return nil
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					return fileChangedMsg{}
				}
				// For other events (chmod, rename, etc.), continue looping
				// instead of spawning a new goroutine
				continue
			case err, ok := <-m.watcher.Errors:
				if !ok {
					return nil
				}
				return errorMsg(err)
			}
		}
	}
}

// sessionWatcherStartedMsg is sent when the session watcher is ready
type sessionWatcherStartedMsg struct {
	watcher *SessionWatcher
}

// startSessionWatcher creates and starts the session watcher for auto-follow mode
func (m Model) startSessionWatcher() tea.Cmd {
	return func() tea.Msg {
		watcher := NewSessionWatcher(m.projectDir, m.sessionID)

		if err := watcher.Start(); err != nil {
			return errorMsg(err)
		}

		return sessionWatcherStartedMsg{watcher: watcher}
	}
}

// sessionWatcherRestartMsg signals that the watcher should restart waiting
type sessionWatcherRestartMsg struct{}

// waitForNewSession returns a command that blocks until a new session is created
func (m Model) waitForNewSession() tea.Cmd {
	return func() tea.Msg {
		if m.sessionWatcher == nil {
			return nil
		}

		path, id := m.sessionWatcher.WaitForNewSession()
		if path == "" {
			return nil // Shutdown or error
		}
		if path == sessionRestartedPath {
			// Session was updated externally, just restart waiting
			return sessionWatcherRestartMsg{}
		}

		return sessionSwitchedMsg{
			newSessionPath: path,
			newSessionID:   id,
		}
	}
}

// Helper functions

func formatCost(cost float64) string {
	// Plain format with 6 decimal places
	return fmt.Sprintf("$%.6f", cost)
}

// formatCostStyled returns a cost string with extra precision (after 2 decimals) dimmed
// When highlighted, the entire cost is shown in highlight color
// The width parameter pads the result to a fixed visual width (ignoring ANSI codes)
func formatCostStyled(cost float64, width int, highlighted bool, noColor bool) string {
	// Format to 6 decimal places: "$123.456789"
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

	// Calculate padding needed
	padding := ""
	if width > plainLen {
		padding = strings.Repeat(" ", width-plainLen)
	}

	if noColor {
		return padding + full
	}

	// Split into main ($X.XX) and extra (XXXX) parts
	dotIdx := strings.Index(full, ".")
	if dotIdx == -1 || len(full) <= dotIdx+3 {
		// No decimal or not enough digits
		if highlighted {
			return padding + highlightStyle.Render(full)
		}
		return padding + full
	}

	main := full[:dotIdx+3]  // "$123.45"
	extra := full[dotIdx+3:] // "6789"

	if highlighted {
		return padding + highlightStyle.Render(main+extra)
	}
	return padding + main + dimStyle.Render(extra)
}

// formatCostStyledGreen returns a cost string in green (for savings) with proper padding
func formatCostStyledGreen(cost float64, width int, highlighted bool, noColor bool) string {
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

	padding := ""
	if width > plainLen {
		padding = strings.Repeat(" ", width-plainLen)
	}

	if noColor {
		return padding + full
	}

	if highlighted {
		return padding + highlightStyle.Render(full)
	}

	// Apply green color (savings style) with dimmed extra decimals
	dotIdx := strings.Index(full, ".")
	if dotIdx == -1 || len(full) <= dotIdx+3 {
		return padding + savingsValueStyle.Render(full)
	}

	main := full[:dotIdx+3]
	extra := full[dotIdx+3:]
	return padding + savingsValueStyle.Render(main) + dimStyle.Render(extra)
}

// formatCostStyledWithMagnitude returns a cost string with magnitude-based coloring
// Higher costs relative to total are brighter, lower costs are dimmer
func formatCostStyledWithMagnitude(cost float64, width int, highlighted bool, noColor bool, total float64) string {
	// Format to 6 decimal places: "$123.456789"
	full := fmt.Sprintf("$%.6f", cost)
	plainLen := len(full)

	// Calculate padding needed
	padding := ""
	if width > plainLen {
		padding = strings.Repeat(" ", width-plainLen)
	}

	if noColor {
		return padding + full
	}

	// Split into main ($X.XX) and extra (XXXX) parts
	dotIdx := strings.Index(full, ".")
	if dotIdx == -1 || len(full) <= dotIdx+3 {
		if highlighted {
			return padding + highlightStyle.Render(full)
		}
		magnitudeColor := getCostMagnitudeColor(cost, total)
		return padding + lipgloss.NewStyle().Foreground(magnitudeColor).Render(full)
	}

	main := full[:dotIdx+3]  // "$123.45"
	extra := full[dotIdx+3:] // "6789"

	if highlighted {
		return padding + highlightStyle.Render(main+extra)
	}

	// Apply magnitude coloring to main part, dim the extra
	magnitudeColor := getCostMagnitudeColor(cost, total)
	mainStyled := lipgloss.NewStyle().Foreground(magnitudeColor).Render(main)
	return padding + mainStyled + dimStyle.Render(extra)
}

func formatNumber(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.2fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// formatNumberWithDelta formats a number with optional delta during highlight
func formatNumberWithDelta(n int, delta int, showDelta bool) string {
	valStr := formatNumber(n)
	if !showDelta || delta == 0 {
		return fmt.Sprintf("%12s", valStr)
	}

	// Format delta
	var deltaStr string
	if delta > 0 {
		deltaStr = fmt.Sprintf("(+%s)", formatNumber(delta))
	} else {
		deltaStr = fmt.Sprintf("(-%s)", formatNumber(-delta))
	}

	return fmt.Sprintf("%12s %s", valStr, deltaStr)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

func truncateID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// getModelColor returns the appropriate color for a model based on its tier
func getModelColor(modelID string) lipgloss.Color {
	modelLower := strings.ToLower(modelID)
	switch {
	case strings.Contains(modelLower, "opus"):
		return styles.OpusColor
	case strings.Contains(modelLower, "sonnet"):
		return styles.SonnetColor
	case strings.Contains(modelLower, "haiku"):
		return styles.HaikuColor
	default:
		return styles.SecondaryColor
	}
}

// getCostMagnitudeColor returns a color based on the cost's proportion of total
func getCostMagnitudeColor(cost, total float64) lipgloss.Color {
	if total == 0 {
		return styles.CostMediumColor
	}
	proportion := cost / total
	switch {
	case proportion > 0.5:
		return styles.CostHighColor
	case proportion >= 0.1:
		return styles.CostMediumColor
	default:
		return styles.CostLowColor
	}
}

// formatContextProgressBar creates a visual progress bar showing context usage
// Bar segments: used (█), free (░), buffer (▒)
// Total width: 50 characters
func formatContextProgressBar(contextSize, freeSpace, buffer, maxContext int, noColor bool) string {
	const barWidth = 50

	if maxContext == 0 {
		return strings.Repeat("░", barWidth)
	}

	// Calculate proportions
	usedRatio := float64(contextSize) / float64(maxContext)
	freeRatio := float64(freeSpace) / float64(maxContext)

	// Convert to bar segments
	usedChars := int(usedRatio * float64(barWidth))
	freeChars := int(freeRatio * float64(barWidth))
	bufferChars := barWidth - usedChars - freeChars

	// Ensure we don't go negative due to rounding
	if bufferChars < 0 {
		bufferChars = 0
	}
	// Adjust for rounding to hit exactly barWidth
	total := usedChars + freeChars + bufferChars
	if total < barWidth {
		freeChars += barWidth - total
	} else if total > barWidth {
		if freeChars > 0 {
			freeChars -= total - barWidth
		} else if usedChars > 0 {
			usedChars -= total - barWidth
		}
	}

	usedStr := strings.Repeat("█", usedChars)
	freeStr := strings.Repeat("░", freeChars)
	bufferStr := strings.Repeat("▒", bufferChars)

	if noColor {
		return "[" + usedStr + freeStr + bufferStr + "]"
	}

	// Get usage color based on percentage - only the used portion is colored
	usagePct := usedRatio * 100
	usageColor := styles.GetContextUsageColor(usagePct)

	// Free space is neutral light gray for contrast, buffer is darker gray
	usedStyled := lipgloss.NewStyle().Foreground(usageColor).Render(usedStr)
	freeStyled := lipgloss.NewStyle().Foreground(styles.ContextFreeColor).Render(freeStr)
	bufferStyled := lipgloss.NewStyle().Foreground(styles.ContextBufferColor).Render(bufferStr)

	return "[" + usedStyled + freeStyled + bufferStyled + "]"
}

// renderHeroCost renders the prominent centered total cost with underline
func (m Model) renderHeroCost(cost float64, highlighted bool) string {
	var sb strings.Builder

	// Format the cost with 6 decimal places
	costStr := fmt.Sprintf("$%.6f", cost)

	// Create the underline (same width as cost string)
	underline := strings.Repeat("─", len(costStr))

	// Center padding (assuming ~60 char width for content area)
	const contentWidth = 60
	costPadding := (contentWidth - len(costStr)) / 2
	labelPadding := (contentWidth - len("total cost")) / 2

	if costPadding < 0 {
		costPadding = 0
	}
	if labelPadding < 0 {
		labelPadding = 0
	}

	pad := strings.Repeat(" ", costPadding)
	labelPad := strings.Repeat(" ", labelPadding)

	if m.noColor {
		sb.WriteString(pad + costStr + "\n")
		sb.WriteString(pad + underline + "\n")
		sb.WriteString(labelPad + "total cost\n")
	} else {
		if highlighted {
			sb.WriteString(pad + highlightStyle.Render(costStr) + "\n")
		} else {
			sb.WriteString(pad + heroCostStyle.Render(costStr) + "\n")
		}
		sb.WriteString(pad + dimStyle.Render(underline) + "\n")
		sb.WriteString(labelPad + dimStyle.Render("total cost") + "\n")
	}

	return sb.String()
}

// renderUnifiedCostRow renders a single row with cost and token info combined
// Format: "  Label          $0.371042     53.9K tokens"
func (m Model) renderUnifiedCostRow(label string, cost float64, tokens int, costField, tokenField string, labelColor lipgloss.Color, extra string) string {
	costHighlighted := m.isHighlighted(costField)
	tokenChanged := m.recentlyChanged(tokenField)
	tokenHighlighted := m.isHighlighted(tokenField)
	delta := m.deltaTokens[tokenField]

	// Format label with optional color
	var labelStr string
	if !m.noColor && labelColor != "" {
		labelStyle := lipgloss.NewStyle().Foreground(labelColor)
		labelStr = labelStyle.Render(fmt.Sprintf("%-14s", label))
	} else {
		labelStr = fmt.Sprintf("%-14s", label)
	}

	// Format cost (10 chars for "$123.456789")
	costStr := formatCostStyled(cost, 11, costHighlighted, m.noColor)

	// Format tokens
	var tokenStr string
	if tokenChanged {
		tokenStr = formatNumberWithDelta(tokens, delta, true)
		if tokenHighlighted {
			tokenStr = highlightStyle.Render(tokenStr)
		}
	} else {
		tokenStr = fmt.Sprintf("%12s", formatNumber(tokens))
	}

	// Add extra info (like TTL)
	extraStr := ""
	if extra != "" {
		if !m.noColor {
			extraStr = "  " + dimStyle.Render(extra)
		} else {
			extraStr = "  " + extra
		}
	}

	return fmt.Sprintf("  %s %s  %s tokens%s\n", labelStr, costStr, tokenStr, extraStr)
}

// renderEmptyState renders a clean empty state for new sessions
func (m Model) renderEmptyState() string {
	var sb strings.Builder

	// Hero cost (even $0.00 to establish visual anchor)
	sb.WriteString("\n")
	sb.WriteString(m.renderHeroCost(0, false))
	sb.WriteString("\n")

	// Simple awaiting message
	msg := "Awaiting first message..."
	const contentWidth = 60
	padding := (contentWidth - len(msg)) / 2
	if padding < 0 {
		padding = 0
	}
	pad := strings.Repeat(" ", padding)

	if m.noColor {
		sb.WriteString(pad + msg + "\n")
	} else {
		sb.WriteString(pad + dimStyle.Render(msg) + "\n")
	}

	return sb.String()
}

// isEmptySession checks if this is an empty/new session with no real data
func (m Model) isEmptySession() bool {
	if m.analysis == nil {
		return true
	}
	// Session is empty if total cost is 0 and no messages
	return m.analysis.TotalCost.TotalCost == 0 && m.analysis.MessageCount == 0
}
