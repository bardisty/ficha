package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
)

// BreakdownModel is the Bubbletea model for the breakdown TUI
type BreakdownModel struct {
	sessionPath string
	sessionID   string
	noColor     bool

	messages       []models.BreakdownMessage
	totalCost      float64
	minCost        float64 // For cost gradient coloring
	maxCost        float64 // For cost gradient coloring
	insights       *models.MessageInsights
	skippedLines   int  // JSONL lines skipped during parsing (malformed or oversized)
	skippedAgents  int  // Agent sub-sessions that could not be read
	estimatedCosts int  // Messages whose cache-write cost is a 5m-rate estimate
	hasUnknown     bool // Any message priced from the fallback table (marked in the rows)
	hasAgents      bool // Any agent row in the merged list — drives the insight scope label
	err            error
	loading        bool
	lastUpdated    time.Time

	// agentCache memoizes agent sub-session parses so a reload triggered by a
	// parent-file write doesn't re-parse every unchanged agent.
	agentCache *analyzer.AgentParseCache

	// Viewport for scrolling
	viewport   viewport.Model
	autoScroll bool
	ready      bool // viewport initialized

	// Change tracking for highlight animation. Keyed by message identity, not
	// position: on every reload the merged list is timestamp-sorted and reindexed
	// 1..N, so a positional key would flag old rows that merely shifted when a new
	// agent message inserts mid-list.
	newMsgKeys map[string]time.Time // message identity -> when it was added

	spinner   spinner.Model
	watcher   *fsnotify.Watcher
	done      chan struct{}
	closing   *atomic.Bool
	closeOnce *sync.Once
	wg        *sync.WaitGroup

	// A file-change waiter is blocked on the watcher; gates armFileWaiter so
	// poll-triggered reloads can't stack extra waiters
	fileWaiterActive bool

	// Auto-follow mode for tracking new sessions
	projectDir     string          // Project directory to watch for new sessions
	followMode     bool            // Whether to auto-follow new sessions
	prevSessionID  string          // Previous session ID (shown after switch)
	sessionWatcher *SessionWatcher // Watches for new session files
	switchNotifyAt time.Time       // When session switch notification started

	// Last subagent-tree fingerprint; the poll reloads when it changes
	// (fsnotify never sees subagent/workflow writes — see subagentPollCmd)
	subagentSig string

	width  int
	height int
}

// Breakdown-specific messages
type (
	breakdownMsgsMsg struct {
		messages       []models.BreakdownMessage
		totalCost      float64
		minCost        float64
		maxCost        float64
		insights       *models.MessageInsights
		skippedLines   int
		skippedAgents  int
		estimatedCosts int
		hasUnknown     bool
		hasAgents      bool
		// sessionPath identifies the session this load was started for. In
		// follow mode a slow in-flight load for the previous session can land
		// after a switch; the handler drops it when it doesn't match the
		// current session so it can't overwrite the new session's data.
		sessionPath string
	}
	breakdownErrorMsg struct {
		err error
		// sessionPath as in breakdownMsgsMsg: a stale load error for the old
		// session must not stamp itself over the new one after a switch.
		sessionPath string
	}
)

// NewBreakdownModel creates a new breakdown TUI model
func NewBreakdownModel(sessionPath, sessionID string, noColor bool, projectDir string, followMode bool) BreakdownModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	closing := &atomic.Bool{}
	closeOnce := &sync.Once{}

	return BreakdownModel{
		sessionPath: sessionPath,
		sessionID:   sessionID,
		noColor:     noColor,
		loading:     true,
		autoScroll:  true,
		spinner:     s,
		done:        make(chan struct{}),
		closing:     closing,
		closeOnce:   closeOnce,
		wg:          &sync.WaitGroup{},
		newMsgKeys:  make(map[string]time.Time),
		projectDir:  projectDir,
		followMode:  followMode,
		agentCache:  analyzer.NewAgentParseCache(),
		subagentSig: subagentTreeSignature(filepath.Dir(sessionPath), sessionID),
	}
}

// Init initializes the breakdown TUI
func (m BreakdownModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.spinner.Tick,
		m.loadBreakdown,
		func() tea.Msg { return m.watchFile() },
		tickCmd(),
		subagentPollCmd(),
	}

	// Start session watcher if follow mode is enabled
	if m.followMode && m.projectDir != "" {
		cmds = append(cmds, m.startSessionWatcher())
	}

	return tea.Batch(cmds...)
}

// Update handles messages
func (m BreakdownModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			shutdownWatchers(m.closeOnce, m.closing, m.done, m.watcher, m.sessionWatcher, m.wg)
			return m, tea.Quit

		case "up", "k":
			m.autoScroll = false
			m.viewport.ScrollUp(1)

		case "down", "j":
			m.viewport.ScrollDown(1)
			// Re-enable auto-scroll if at bottom
			if m.viewport.AtBottom() {
				m.autoScroll = true
			}

		case "pgup":
			m.autoScroll = false
			m.viewport.HalfPageUp()

		case "pgdown":
			m.viewport.HalfPageDown()
			if m.viewport.AtBottom() {
				m.autoScroll = true
			}

		case "g", "home":
			m.autoScroll = false
			m.viewport.GotoTop()

		case "G", "end":
			m.viewport.GotoBottom()
			m.autoScroll = true
		}

	case tea.WindowSizeMsg:
		// Header: panel(3 lines) + blank/notify(1) + insights(1) + blank(1) + table header(1) + separator(1)
		// Always use 8 to avoid layout shift when insights load after initial render
		headerHeight := 8
		footerHeight := 4 // Double-line separator(1) + stats(1) + single-line(1) + help(1)

		if !m.ready {
			m.viewport = viewport.New(msg.Width, viewportHeight(msg.Height, headerHeight, footerHeight))
			m.viewport.YPosition = headerHeight
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = viewportHeight(msg.Height, headerHeight, footerHeight)
		}
		m.width = msg.Width
		m.height = msg.Height

		// Re-render content with new dimensions
		if len(m.messages) > 0 {
			m.viewport.SetContent(clipToWidth(m.renderTableContent(), m.width))
			if m.autoScroll {
				m.viewport.GotoBottom()
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case breakdownMsgsMsg:
		if msg.sessionPath != "" && msg.sessionPath != m.sessionPath {
			// A load for a previous session finished after a follow-mode switch;
			// dropping it keeps it from overwriting the new session's data under
			// the new header. Re-arm (idempotent) so the waiter isn't lost. An
			// unstamped (empty) message applies — only hand-built test messages
			// omit the path; real loads always stamp it.
			return m, m.armFileWaiter()
		}
		// Track new messages for highlighting
		m.detectNewMessages(msg.messages)

		m.messages = msg.messages
		m.totalCost = msg.totalCost
		m.minCost = msg.minCost
		m.maxCost = msg.maxCost
		m.insights = msg.insights
		m.skippedLines = msg.skippedLines
		m.skippedAgents = msg.skippedAgents
		m.estimatedCosts = msg.estimatedCosts
		m.hasUnknown = msg.hasUnknown
		m.hasAgents = msg.hasAgents
		m.loading = false
		m.lastUpdated = time.Now()
		m.err = nil

		if m.ready {
			m.viewport.SetContent(clipToWidth(m.renderTableContent(), m.width))
			if m.autoScroll {
				m.viewport.GotoBottom()
			}
		}
		// Re-arm the file watcher only when no waiter is in flight: this reload
		// may have been poll-triggered, in which case the file-change waiter is
		// still blocked on the watcher
		// Call before return: the arm must mutate the m the caller receives
		armCmd := m.armFileWaiter()
		return m, armCmd

	case breakdownErrorMsg:
		if msg.sessionPath != "" && msg.sessionPath != m.sessionPath {
			// Stale load error for a previous session; see breakdownMsgsMsg.
			return m, m.armFileWaiter()
		}
		m.err = msg.err
		m.loading = false
		// Re-arm (if needed) even after an error to continue monitoring
		// Call before return: the arm must mutate the m the caller receives
		armCmd := m.armFileWaiter()
		return m, armCmd

	case fileWatchErrMsg:
		if msg.watcher != m.watcher {
			// Stale message from a superseded watcher; the current waiter
			// accounting doesn't cover it
			return m, nil
		}
		// The file-change waiter exited with a watcher error. Errors like an
		// event-queue overflow mean changes may have been dropped unseen, so
		// reload as well as arming a replacement waiter
		m.err = msg.err
		m.fileWaiterActive = false
		m.loading = true
		armCmd := m.armFileWaiter()
		return m, tea.Batch(m.loadBreakdownCmd(), armCmd)

	case watcherStartedMsg:
		// A replacement watcher (rapid session switches can have two watchFile
		// calls in flight) supersedes the current one: close it so its waiter
		// exits, and account for that exit here since it carries no message
		if m.watcher != nil && m.watcher != msg.watcher {
			m.watcher.Close()
			m.fileWaiterActive = false
		}
		m.watcher = msg.watcher
		// Call before return: the arm must mutate the m the caller receives
		armCmd := m.armFileWaiter()
		return m, armCmd

	case sessionWatcherStartedMsg:
		// Store the session watcher and start waiting for new sessions
		m.sessionWatcher = msg.watcher
		return m, m.waitForNewSession()

	case sessionWatcherRestartMsg:
		// Session was changed externally, restart waiting
		return m, m.waitForNewSession()

	case fileChangedMsg:
		if msg.watcher != m.watcher {
			// Stale message from a superseded watcher; the current waiter
			// accounting doesn't cover it
			return m, nil
		}
		// The waiter that reported this change has exited; the reload's
		// breakdownMsgsMsg/breakdownErrorMsg arms its replacement
		m.fileWaiterActive = false
		m.loading = true
		return m, m.loadBreakdownCmd()

	case sessionSwitchedMsg:
		// Store previous session ID and switch to new session
		m.prevSessionID = m.sessionID
		m.sessionID = msg.newSessionID
		m.sessionPath = msg.newSessionPath
		m.switchNotifyAt = time.Now()

		// Reset state for clean switch. hasUnknown and err must reset too, or the
		// old session's "* = fallback pricing" footnote (and a stale header
		// error) persist under the new session until its first load lands — or
		// indefinitely if that load errors.
		m.messages = nil
		m.insights = nil
		m.hasAgents = false
		m.totalCost = 0
		m.minCost = 0
		m.maxCost = 0
		m.skippedLines = 0
		m.skippedAgents = 0
		m.estimatedCosts = 0
		m.hasUnknown = false
		m.err = nil
		m.loading = true
		m.newMsgKeys = make(map[string]time.Time)
		// Drop the previous session's cached agent parses
		m.agentCache = analyzer.NewAgentParseCache()
		// Fingerprint the new session's subagent tree; the pending reload
		// covers anything already on disk
		m.subagentSig = subagentTreeSignature(filepath.Dir(m.sessionPath), m.sessionID)

		// Stop old file watcher, will be restarted by watchFile. Closing it
		// unblocks the old waiter, which exits without a message, so the
		// in-flight flag resets here for the new watcher's waiter
		if m.watcher != nil {
			m.watcher.Close()
			m.watcher = nil
		}
		m.fileWaiterActive = false

		// Update session watcher's current session
		if m.sessionWatcher != nil {
			m.sessionWatcher.SetCurrentSession(m.sessionID)
		}

		// Reload data, restart file watcher, and restart session watcher
		// We must explicitly restart waitForNewSession because the goroutine that
		// detected this switch has already exited after returning sessionSwitchedMsg
		cmds := []tea.Cmd{m.loadBreakdownCmd(), func() tea.Msg { return m.watchFile() }}
		if m.sessionWatcher != nil {
			cmds = append(cmds, m.waitForNewSession())
		}
		return m, tea.Batch(cmds...)

	case tickMsg:
		// Re-render only while highlights are active — an idle table would
		// otherwise be fully re-rendered every tick. Capture before cleanup:
		// the tick that expires the last highlight still needs one final
		// re-render to un-highlight its rows.
		hadHighlights := len(m.newMsgKeys) > 0
		m.cleanupExpiredHighlights()
		if hadHighlights && m.ready && len(m.messages) > 0 {
			m.viewport.SetContent(clipToWidth(m.renderTableContent(), m.width))
		}
		return m, tickCmd()

	case subagentPollMsg:
		sig := subagentTreeSignature(filepath.Dir(m.sessionPath), m.sessionID)
		if sig != m.subagentSig {
			m.subagentSig = sig
			m.loading = true
			return m, tea.Batch(m.loadBreakdownCmd(), subagentPollCmd())
		}
		return m, subagentPollCmd()
	}

	return m, tea.Batch(cmds...)
}

// breakdownMsgKey identifies a message by content, not position, so highlight
// tracking survives the per-reload timestamp sort and 1..N reindex. Two rows
// with identical timestamp, agent, model, usage, and cost are indistinguishable
// anyway; collisions only affect a 2s cosmetic highlight.
func breakdownMsgKey(msg models.BreakdownMessage) string {
	u := msg.Usage
	var e5m, e1h int64
	if u.CacheCreation != nil {
		e5m = u.CacheCreation.Ephemeral5mInputTokens
		e1h = u.CacheCreation.Ephemeral1hInputTokens
	}
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d\x00%.10f",
		msg.Timestamp.UTC().Format(time.RFC3339Nano), msg.AgentID, msg.Model,
		u.InputTokens, u.OutputTokens, u.CacheCreationInputTokens, u.CacheReadInputTokens,
		e5m, e1h, msg.Cost.TotalCost)
}

// detectNewMessages flags messages whose identity is absent from the currently
// displayed set as new. Keying on identity (not index) means a mid-list agent
// insertion highlights only the inserted row, not the old tail rows the sort
// shifted past the old count.
func (m *BreakdownModel) detectNewMessages(newMessages []models.BreakdownMessage) {
	// Nothing to diff against on first load (or right after a session switch):
	// flagging every row would flash the whole table as "new"
	if len(m.messages) == 0 {
		return
	}

	seen := make(map[string]struct{}, len(m.messages))
	for _, msg := range m.messages {
		seen[breakdownMsgKey(msg)] = struct{}{}
	}

	now := time.Now()
	for _, msg := range newMessages {
		key := breakdownMsgKey(msg)
		if _, ok := seen[key]; !ok {
			m.newMsgKeys[key] = now
		}
	}
}

// cleanupExpiredHighlights removes highlight entries older than highlightDuration.
// This also bounds the map: keys for messages whose content changed (or that
// vanished) are pruned once their window lapses.
func (m *BreakdownModel) cleanupExpiredHighlights() {
	for key, addedAt := range m.newMsgKeys {
		if time.Since(addedAt) > highlightDuration {
			delete(m.newMsgKeys, key)
		}
	}
}

// isNewMessage checks if a message should be highlighted as new
func (m *BreakdownModel) isNewMessage(msg models.BreakdownMessage) bool {
	if m.noColor {
		return false
	}
	addedAt, exists := m.newMsgKeys[breakdownMsgKey(msg)]
	if !exists {
		return false
	}
	return time.Since(addedAt) < highlightDuration
}

// View renders the breakdown TUI
func (m BreakdownModel) View() string {
	var sb strings.Builder
	panelWidth := panelWidthFor(m.width)

	// Header panel
	sb.WriteString(m.renderHeaderPanel(panelWidth))

	// Show switch notification after session switch
	showSwitchNotify := !m.switchNotifyAt.IsZero() && time.Since(m.switchNotifyAt) < switchNotifyDuration
	if showSwitchNotify {
		sb.WriteString("\n")
		if m.noColor {
			sb.WriteString("  [Switched to new session]")
		} else {
			sb.WriteString("  " + lipgloss.NewStyle().Foreground(styles.HighlightColor).Bold(true).Render("Switched to new session"))
		}
	}

	sb.WriteString("\n")

	// Compact insights line
	if m.insights != nil && len(m.messages) > 0 {
		sb.WriteString(m.renderCompactInsights())
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// Table header
	sb.WriteString(m.renderTableHeader())
	sb.WriteString("\n")

	// Separator
	sb.WriteString(m.renderTableSeparator())
	sb.WriteString("\n")

	// Viewport with scrollable content
	if m.ready {
		sb.WriteString(m.viewport.View())
	}
	sb.WriteString("\n")

	// Double-line footer separator
	footerRule := styles.BoxHorizontal
	if m.noColor {
		footerRule = styles.AsciiHorizontal
	}
	footerSep := strings.Repeat(footerRule, panelWidth)
	if !m.noColor {
		footerSep = panelBorderStyle.Render(footerSep)
	}
	sb.WriteString("  " + footerSep + "\n")

	// Footer with colored cost
	scrollMode := "AUTO"
	if !m.autoScroll {
		scrollMode = "MANUAL"
	}
	if !m.noColor {
		msgPart := fmt.Sprintf("Messages: %d", len(m.messages))
		costStyled := lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(render.Cost(m.totalCost))
		scrollPart := fmt.Sprintf("Scroll: %s", scrollMode)

		// Use lighter gray (250) for text
		lightGray := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
		sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

		sb.WriteString("  ")
		sb.WriteString(lightGray.Render(msgPart))
		sb.WriteString(sepStyle.Render(" │ "))
		sb.WriteString(lightGray.Render("Total: "))
		sb.WriteString(costStyled)
		sb.WriteString(sepStyle.Render(" │ "))
		sb.WriteString(lightGray.Render(scrollPart))
		// Surface parse warnings so an incomplete breakdown doesn't look complete
		if note := accountingFootnote(m.skippedAgents, m.skippedLines, m.estimatedCosts, false); note != "" {
			warnStyle := lipgloss.NewStyle().Foreground(styles.WarningColor)
			sb.WriteString(sepStyle.Render(" │ "))
			sb.WriteString(warnStyle.Render(note))
		}
		// Explain the MODEL-column asterisk: those rows are fallback-priced
		if m.hasUnknown {
			warnStyle := lipgloss.NewStyle().Foreground(styles.WarningColor)
			sb.WriteString(sepStyle.Render(" │ "))
			sb.WriteString(warnStyle.Render(unknownModelFootnote(false)))
		}
	} else {
		sb.WriteString(fmt.Sprintf("  Messages: %d | Total: %s | Scroll: %s",
			len(m.messages), render.Cost(m.totalCost), scrollMode))
		if note := accountingFootnote(m.skippedAgents, m.skippedLines, m.estimatedCosts, true); note != "" {
			sb.WriteString(" | " + note)
		}
		if m.hasUnknown {
			sb.WriteString(" | " + unknownModelFootnote(true))
		}
	}
	sb.WriteString("\n")

	// Single-line separator before help
	helpRule := styles.LineHorizontal
	if m.noColor {
		helpRule = styles.AsciiRule
	}
	helpSep := strings.Repeat(helpRule, panelWidth)
	if !m.noColor {
		helpSep = dimStyle.Render(helpSep)
	}
	sb.WriteString("  " + helpSep + "\n")

	// Help - use lighter gray
	if !m.noColor {
		helpText := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("q: quit • g/G: top/bottom • ↑↓: scroll")
		sb.WriteString("  " + helpText)
	} else {
		sb.WriteString("  q: quit • g/G: top/bottom • ↑↓: scroll")
	}

	return clipToWidth(sb.String(), m.width)
}

// renderHeaderPanel renders the mainframe-style header panel with session info
func (m BreakdownModel) renderHeaderPanel(width int) string {
	return renderLiveHeaderPanel(liveHeaderParams{
		sessionID:     m.sessionID,
		prevSessionID: m.prevSessionID,
		loading:       m.loading,
		err:           m.err,
		lastUpdated:   m.lastUpdated,
		spinnerView:   m.spinner.View(),
		noColor:       m.noColor,
		width:         width,
	})
}

// renderCompactInsights renders a single line of insights
func (m BreakdownModel) renderCompactInsights() string {
	if m.insights == nil {
		return ""
	}

	var parts []string

	// Peak cost with message number for easy lookup
	if m.insights.HighestCost != nil {
		mult := m.insights.CostMultiplier()
		peakStr := fmt.Sprintf("Peak: #%d %s @ %s (%.1fx avg)",
			m.insights.HighestCost.Index,
			render.Cost(m.insights.HighestCost.Cost),
			render.ClockShort(m.insights.HighestCost.Timestamp),
			mult)
		if !m.noColor {
			parts = append(parts, lipgloss.NewStyle().Foreground(styles.WarningColor).Render(peakStr))
		} else {
			parts = append(parts, peakStr)
		}
	}

	// Trend (only once the analyzer actually computed one — see HasTrend)
	if m.insights.HasTrend() {
		trendDesc := m.insights.TrendDescription()
		trendSymbol := m.insights.CostTrend.Symbol()
		trendStr := fmt.Sprintf("Trend: %s %s", trendSymbol, trendDesc)
		if !m.noColor {
			var color lipgloss.Color
			switch m.insights.CostTrend {
			case models.TrendIncreasing:
				color = styles.WarningColor
			case models.TrendDecreasing:
				color = styles.SuccessColor
			default:
				color = styles.SecondaryColor
			}
			parts = append(parts, lipgloss.NewStyle().Foreground(color).Render(trendStr))
		} else {
			parts = append(parts, trendStr)
		}
	}

	// Label the scope when agents ran: the breakdown computes Peak/trend over the
	// merged parent+agent messages, unlike show/watch's parent-only insights, so
	// the figures can legitimately differ. Without agents the sets are identical,
	// so no label (and no churn for the common case).
	if m.hasAgents && len(parts) > 0 {
		const scope = "scope: parent + agents"
		if m.noColor {
			parts = append(parts, scope)
		} else {
			parts = append(parts, lipgloss.NewStyle().Foreground(styles.SecondaryColor).Render(scope))
		}
	}

	sep := " │ "
	if m.noColor {
		sep = " | "
	}
	return "  " + strings.Join(parts, sep)
}

// renderTableHeader renders the table header row
func (m BreakdownModel) renderTableHeader() string {
	// The COST field is the 10-column cost cell plus a space and the change
	// arrow; the label right-aligns over the cell's four-decimal edge.
	header := fmt.Sprintf("  %-5s  %-8s  %-10s  %10s    %6s  %5s  %6s  %6s",
		"#", "TIME", "MODEL", "COST", "IN", "OUT", "C_WR", "C_RD")
	if !m.noColor {
		return headerStyle.Render(header)
	}
	return header
}

// renderTableSeparator renders the table separator rule: box-drawing when
// colored, an ASCII fallback in no-color.
func (m BreakdownModel) renderTableSeparator() string {
	rule := styles.LineHorizontal
	if m.noColor {
		rule = styles.AsciiRule
	}
	sep := "  " + strings.Repeat(rule, panelWidthFor(m.width))
	if !m.noColor {
		return tableBorderStyle.Render(sep)
	}
	return sep
}

// renderTableContent renders all message rows for the viewport
func (m BreakdownModel) renderTableContent() string {
	var sb strings.Builder
	var prevCost float64

	for i, msg := range m.messages {
		isFirst := i == 0
		// Rows carry only a time, so mark where the local day changes. A
		// zero timestamp (unparseable in the transcript) has no day to mark.
		if !isFirst && !msg.Timestamp.IsZero() && !m.messages[i-1].Timestamp.IsZero() &&
			!render.SameLocalDay(m.messages[i-1].Timestamp, msg.Timestamp) {
			sb.WriteString(m.renderDayMarker(msg.Timestamp))
			sb.WriteString("\n")
		}
		sb.WriteString(m.renderRow(msg, m.isNewMessage(msg), prevCost, isFirst))
		prevCost = msg.Cost.TotalCost
		if i < len(m.messages)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// breakdownCostWidth fits a per-message cost up to "$9999.99  ".
const breakdownCostWidth = 10

// renderDayMarker renders the divider row placed above the first message of a
// new local day.
func (m BreakdownModel) renderDayMarker(t time.Time) string {
	if m.noColor {
		return "  -- " + render.DayMarker(t) + " --"
	}
	return "  " + dimStyle.Render("── "+render.DayMarker(t)+" ──")
}

// renderRow renders a single message row
func (m BreakdownModel) renderRow(msg models.BreakdownMessage, isNew bool, prevCost float64, isFirst bool) string {
	// Format column values
	indexStr := fmt.Sprintf("%-5d", msg.Index)
	timeStr := fmt.Sprintf("%-8s", render.Clock(msg.Timestamp))
	modelName := pricing.GetModelDisplayName(msg.Model)
	// Flag fallback-priced rows inline; the footer explains the marker. Clamp
	// first so a long raw ID can't push it out of the column (or off it).
	modelLabel := render.ClampModel(modelName, 10)
	if !pricing.IsKnownModel(msg.Model) {
		modelLabel = render.ClampModel(modelName, 9) + unknownModelMarker
	}
	modelStr := fmt.Sprintf("%-10s", modelLabel)
	costStr := render.CostCell(msg.Cost.TotalCost, breakdownCostWidth)
	inStr := fmt.Sprintf("%6s", render.Number(msg.Usage.InputTokens))
	outStr := fmt.Sprintf("%5s", render.Number(msg.Usage.OutputTokens))
	cacheWriteStr := fmt.Sprintf("%6s", render.Number(msg.Usage.CacheCreationInputTokens))
	cacheReadStr := fmt.Sprintf("%6s", render.Number(msg.Usage.CacheReadInputTokens))

	// Get trend indicator
	trendSymbol, trendDirection := getRowTrendIndicator(msg.Cost.TotalCost, prevCost, isFirst)

	// Agent marker at the end: the real agent ID, abbreviated. No fixed-width
	// padding — nothing renders after it and the viewport pads every line.
	agentMarker := ""
	if msg.AgentID != "" {
		agentMarker = "[A" + render.ShortAgentID(msg.AgentID) + "]"
	}

	if m.noColor {
		return fmt.Sprintf("  %s  %s  %s  %s %s  %s  %s  %s  %s  %s",
			indexStr, timeStr, modelStr, costStr, trendSymbol, inStr, outStr, cacheWriteStr, cacheReadStr, agentMarker)
	}

	// Apply per-column styling
	dimStyle := lipgloss.NewStyle().Foreground(styles.SecondaryColor)
	modelStyle := lipgloss.NewStyle().Foreground(styles.GetModelColor(modelName))
	costColor := styles.GetCostGradientColor(msg.Cost.TotalCost, m.minCost, m.maxCost)
	outStyle := lipgloss.NewStyle().Foreground(styles.OutputTokenColor)
	cacheWriteStyle := lipgloss.NewStyle().Foreground(styles.CacheWriteTokenColor)
	cacheReadStyle := lipgloss.NewStyle().Foreground(styles.CacheReadTokenColor)

	// Color the trend indicator based on direction
	var trendStyled string
	switch trendDirection {
	case models.TrendIncreasing:
		trendStyled = lipgloss.NewStyle().Foreground(styles.WarningColor).Render(trendSymbol)
	case models.TrendDecreasing:
		trendStyled = lipgloss.NewStyle().Foreground(styles.SuccessColor).Render(trendSymbol)
	default:
		trendStyled = dimStyle.Render(trendSymbol)
	}

	costStyled := render.CostColored(msg.Cost.TotalCost, costColor, breakdownCostWidth)

	// For new messages, override with highlight style
	if isNew {
		highlightStyle := styles.HighlightStyle
		return fmt.Sprintf("  %s  %s  %s  %s %s  %s  %s  %s  %s  %s",
			highlightStyle.Render(indexStr),
			highlightStyle.Render(timeStr),
			highlightStyle.Render(modelStr),
			highlightStyle.Render(costStr),
			highlightStyle.Render(trendSymbol),
			highlightStyle.Render(inStr),
			highlightStyle.Render(outStr),
			highlightStyle.Render(cacheWriteStr),
			highlightStyle.Render(cacheReadStr),
			highlightStyle.Render(agentMarker))
	}

	// Agent marker with its own color
	agentRendered := ""
	if agentMarker != "" {
		agentStyle := lipgloss.NewStyle().Foreground(styles.GetAgentColor(msg.AgentID))
		agentRendered = agentStyle.Render(agentMarker)
	}

	return fmt.Sprintf("  %s  %s  %s  %s %s  %s  %s  %s  %s  %s",
		dimStyle.Render(indexStr),
		dimStyle.Render(timeStr),
		modelStyle.Render(modelStr),
		costStyled,
		trendStyled,
		inStr, // Input stays white/default
		outStyle.Render(outStr),
		cacheWriteStyle.Render(cacheWriteStr),
		cacheReadStyle.Render(cacheReadStr),
		agentRendered)
}

// Commands

func (m BreakdownModel) loadBreakdown() tea.Msg {
	if m.closing != nil && m.closing.Load() {
		return nil
	}
	result, err := analyzer.GetBreakdownMessagesWithCache(m.sessionPath, m.sessionID, m.agentCache)
	if err != nil {
		return breakdownErrorMsg{err: err, sessionPath: m.sessionPath}
	}
	messages := result.Messages

	// Calculate total cost and min/max cost for the gradient. Insights are
	// order-sensitive and come from the analyzer, computed over the file-order
	// merged list; these aggregates are order-insensitive so the
	// display-sorted list is fine.
	var totalCost float64
	var minCost, maxCost float64
	hasUnknown := false
	hasAgents := false

	for i, msg := range messages {
		totalCost += msg.Cost.TotalCost
		if !hasUnknown && !pricing.IsKnownModel(msg.Model) {
			hasUnknown = true
		}
		if msg.AgentID != "" {
			hasAgents = true
		}

		// Track min/max for cost gradient
		if i == 0 {
			minCost = msg.Cost.TotalCost
			maxCost = msg.Cost.TotalCost
		} else {
			if msg.Cost.TotalCost < minCost {
				minCost = msg.Cost.TotalCost
			}
			if msg.Cost.TotalCost > maxCost {
				maxCost = msg.Cost.TotalCost
			}
		}
	}

	return breakdownMsgsMsg{
		messages:       messages,
		totalCost:      totalCost,
		minCost:        minCost,
		maxCost:        maxCost,
		insights:       result.Insights,
		skippedLines:   result.SkippedLines,
		skippedAgents:  result.SkippedAgents,
		estimatedCosts: result.EstimatedCostMessages,
		hasUnknown:     hasUnknown,
		hasAgents:      hasAgents,
		sessionPath:    m.sessionPath,
	}
}

func (m BreakdownModel) loadBreakdownCmd() tea.Cmd {
	return func() tea.Msg {
		return m.loadBreakdown()
	}
}

// wrapErr lets the shared watcher commands report failures as this model's
// error message.
func (m BreakdownModel) wrapErr(err error) tea.Msg {
	return breakdownErrorMsg{err: err, sessionPath: m.sessionPath}
}

func (m BreakdownModel) watchFile() tea.Msg { return watchFileCmd(m.sessionPath, m.wrapErr) }

// armFileWaiter starts a file-change waiter unless one is already blocked on
// the watcher; see Model.armFileWaiter. Returns nil when a waiter is already
// in flight.
func (m *BreakdownModel) armFileWaiter() tea.Cmd {
	if m.watcher == nil || m.fileWaiterActive {
		return nil
	}
	m.fileWaiterActive = true
	return waitForFileChangeCmd(m.wg, m.closing, m.watcher, m.done, m.sessionPath)
}

// Per-message change symbols (distinct from session trend ▲/▼/═)
const (
	changeUp     = "↑"
	changeDown   = "↓"
	changeStable = "·"
)

// getRowTrendIndicator returns the change indicator for a message based on cost change
// from previous message. Uses 5% threshold for significance.
func getRowTrendIndicator(currentCost, previousCost float64, isFirst bool) (symbol string, direction models.TrendDirection) {
	if isFirst || previousCost == 0 {
		return changeStable, models.TrendStable
	}

	change := (currentCost - previousCost) / previousCost

	if change > 0.05 {
		return changeUp, models.TrendIncreasing
	} else if change < -0.05 {
		return changeDown, models.TrendDecreasing
	}
	return changeStable, models.TrendStable
}

func (m BreakdownModel) startSessionWatcher() tea.Cmd {
	return startSessionWatcherCmd(m.projectDir, m.sessionID, m.wrapErr)
}

func (m BreakdownModel) waitForNewSession() tea.Cmd {
	return waitForNewSessionCmd(m.wg, m.sessionWatcher)
}
