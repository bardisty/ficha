package tui

import (
	"slices"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
)

// rowScope narrows breakdown's table to one source's rows: the main
// conversation's when agent is "", else that agent's. The zero scope is
// every row.
type rowScope struct {
	on    bool
	agent string
}

// inScope reports whether msg is one of the scope's rows.
func (m BreakdownModel) inScope(msg models.BreakdownMessage) bool {
	return !m.scope.on || msg.AgentID == m.scope.agent
}

// scopeRows is the scope's rows, by time.
func (m BreakdownModel) scopeRows() []models.BreakdownMessage {
	if !m.scope.on {
		return m.messages
	}
	var rows []models.BreakdownMessage
	for _, msg := range m.messages {
		if m.inScope(msg) {
			rows = append(rows, msg)
		}
	}
	return rows
}

// scopeLen is how many rows the scope has.
func (m BreakdownModel) scopeLen() int {
	if !m.scope.on {
		return len(m.messages)
	}
	return m.scopeCount
}

// countScope works out scopeCount and scopeCost, after the rows or the
// scope change.
func (m *BreakdownModel) countScope() {
	m.scopeCount, m.scopeCost = 0, 0
	for _, msg := range m.scopeRows() {
		m.scopeCount++
		m.scopeCost += msg.Cost.TotalCost
	}
}

// scopeSteps is the order a steps through: every row, the main
// conversation, then each agent in the order its first row appears, as the
// AGENT column meets them reading down. A session with no agent rows has
// nowhere to step, and one with no main conversation rows skips it.
func (m BreakdownModel) scopeSteps() []rowScope {
	var agents []rowScope
	seen := map[string]bool{}
	for _, msg := range m.messages {
		if !seen[msg.AgentID] && msg.AgentID != "" {
			agents = append(agents, rowScope{on: true, agent: msg.AgentID})
		}
		seen[msg.AgentID] = true
	}
	if len(agents) == 0 {
		return nil
	}
	steps := []rowScope{{}}
	if seen[""] {
		steps = append(steps, rowScope{on: true})
	}
	return append(steps, agents...)
}

// stepScope moves the table to the next scope in scopeSteps.
func (m *BreakdownModel) stepScope() {
	steps := m.scopeSteps()
	if len(steps) == 0 || !m.ready {
		return
	}
	next := steps[0]
	for i, s := range steps {
		if s == m.scope {
			next = steps[(i+1)%len(steps)]
		}
	}
	m.setScope(next)
}

// setScope shows scope's rows. A p selection outside it goes. Following
// stays on the newest row; otherwise the view lands on the first of the
// scope's rows at or after the first row on screen, else its last, or
// sorted, on the costliest. Rows outside the scope don't count as new, so
// the count starts over.
func (m *BreakdownModel) setScope(scope rowScope) {
	var from int
	if first, _ := m.visibleRows(); first > 0 {
		from = first - 1
	}
	m.scope = scope
	m.countScope()
	m.scopeInsights()
	if m.selectedKey != "" && !m.selectionInScope() {
		m.clearSelection()
	}
	m.pausedAt = m.scopeLen()
	m.refreshViewport()
	switch {
	case m.sortByCost:
		m.scrollTo(0)
	case m.autoScroll:
		m.viewport.GotoBottom()
		m.scrollTo(m.viewport.YOffset())
	default:
		// With none of the scope's rows that late, the view ends on its last.
		line := len(m.linePos) - 1
		for i, pos := range m.linePos {
			if pos >= from {
				line = i
				break
			}
		}
		m.scrollTo(max(line, 0))
	}
}

// selectionInScope reports whether p's selected row is one of the scope's.
func (m BreakdownModel) selectionInScope() bool {
	return slices.ContainsFunc(m.scopeRows(), func(msg models.BreakdownMessage) bool { return breakdownMsgKey(msg) == m.selectedKey })
}

// scopeInsights sets the insights the stats line shows: the session's,
// or worked out over the scope's rows, so Peak and Trend describe the rows
// on show.
func (m *BreakdownModel) scopeInsights() {
	if !m.scope.on {
		m.insights = m.sessionInsights
		return
	}
	m.insights = analyzer.ScopeInsights(m.scopeRows())
}

// scopeTotal is the cost of the scope's rows.
func (m BreakdownModel) scopeTotal() float64 {
	if !m.scope.on {
		return m.totalCost
	}
	return m.scopeCost
}

// scopeLabels names the scope for the stats line, fullest first: an
// agent's marker and its task, or the marker alone. "" for every row.
func (m BreakdownModel) scopeLabels() []string {
	switch {
	case !m.scope.on:
		return nil
	case m.scope.agent == "":
		return []string{mainScopeLabel}
	}
	marker := agentMarker(m.scope.agent)
	if task := m.tasks[m.scope.agent]; task != "" {
		return []string{marker + " " + task, marker}
	}
	return []string{marker}
}

// ruleScope names the scope on the footer rule, which keeps it where the
// AGENT column is hidden, so an agent's rows aren't taken for the main
// conversation's.
func (m BreakdownModel) ruleScope() string {
	switch {
	case !m.scope.on:
		return ""
	case m.scope.agent == "":
		return mainScopeLabel
	}
	return "A" + render.ShortAgentID(m.scope.agent)
}

// mainScopeLabel names the main conversation's rows as a scope, in the words
// watch and show use for them.
const mainScopeLabel = "main conversation"

// scopeKeyHint ends the footer rule while scoped, since a narrow help line
// has no room to say a steps on.
const scopeKeyHint = "a next"
