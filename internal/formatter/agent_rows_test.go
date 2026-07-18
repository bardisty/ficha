package formatter

import (
	"regexp"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// agentBreakdownAnalysis carries one agent whose message count overflows the
// historical 8-char msgs field ("12345 msgs" is 10 chars) next to a normal one.
func agentBreakdownAnalysis() *models.SessionAnalysis {
	return &models.SessionAnalysis{
		SessionID:  "0a1b2c3d-4e5f-6789-abcd-ef0123456789",
		ParentCost: models.CostBreakdown{TotalCost: 7.90},
		AgentsCost: models.CostBreakdown{TotalCost: 2.00},
		HasAgents:  true,
		AgentCount: 2,
		Agents: []models.AgentAnalysis{
			{
				AgentID:      "a1b2c3d4e5f6",
				MessageCount: 12345,
				TotalCost:    models.CostBreakdown{TotalCost: 0.42},
				CostByModel:  map[string]models.CostBreakdown{"claude-haiku-4-5": {TotalCost: 0.42}},
			},
			{
				AgentID:      "f6e5d4c3b2a1",
				MessageCount: 1,
				TotalCost:    models.CostBreakdown{TotalCost: 1.58},
				CostByModel:  map[string]models.CostBreakdown{"claude-sonnet-5": {TotalCost: 1.58}},
			},
		},
	}
}

// The AGENT SUB-SESSIONS section aligns every cost — "Parent session", each
// agent row, "Agents subtotal" — at the same column. The msgs field used a
// fixed %8s, so a 1000+ message agent pushed its cost out of that column; now
// the column widens (eating the gap) and the costs stay put.
func TestAgentBreakdownMsgsColumnKeepsCostAligned(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		name := "color"
		if noColor {
			name = "no-color"
		}
		t.Run(name, func(t *testing.T) {
			if noColor {
				forceProfile(t, termenv.Ascii)
			} else {
				forceProfile(t, termenv.ANSI256)
			}
			out := formatAgentBreakdownContent(agentBreakdownAnalysis(), noColor)

			var costCols []int
			for _, line := range strings.Split(out, "\n") {
				plain := ansiRe.ReplaceAllString(line, "")
				if idx := strings.Index(plain, "$"); idx >= 0 {
					costCols = append(costCols, idx)
				}
			}
			if len(costCols) != 4 {
				t.Fatalf("expected 4 cost lines (parent, 2 agents, subtotal), got %d:\n%s", len(costCols), out)
			}
			for i, col := range costCols[1:] {
				if col != costCols[0] {
					t.Errorf("cost line %d starts at column %d, line 0 at %d:\n%s", i+1, col, costCols[0], out)
				}
			}
		})
	}
}

// The expand-agents tree rows share the same msgs column, so sibling agent
// costs must stay mutually aligned when one agent's count overflows.
func TestSummaryTreeRowsMsgsColumnKeepsCostAligned(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		name := "color"
		if noColor {
			name = "no-color"
		}
		t.Run(name, func(t *testing.T) {
			if noColor {
				forceProfile(t, termenv.Ascii)
			} else {
				forceProfile(t, termenv.ANSI256)
			}
			r := sessionRow("aaaa1111", "claude-opus-4-8", 10, 3.00)
			r.Analysis.HasAgents = true
			r.Analysis.AgentCount = 2
			r.Analysis.AgentsCost = models.CostBreakdown{TotalCost: 2.00}
			r.Analysis.Agents = agentBreakdownAnalysis().Agents

			aggregate := &models.SessionAnalysis{
				SessionID: "aggregate", IsSummary: true, SessionCount: 1,
				CostByModel: map[string]models.CostBreakdown{},
			}
			out := FormatSummaryTableWithDetails(aggregate, []models.SessionResult{r}, "/home/user/src/app", noColor, true)

			var costCols, rowWidths []int
			for _, line := range strings.Split(out, "\n") {
				plain := ansiRe.ReplaceAllString(line, "")
				if !strings.Contains(plain, "[A") {
					continue
				}
				costCols = append(costCols, strings.Index(plain, "$"))
				rowWidths = append(rowWidths, lipgloss.Width(line))
			}
			if len(costCols) != 2 {
				t.Fatalf("expected 2 agent tree rows, got %d:\n%s", len(costCols), out)
			}
			if costCols[0] != costCols[1] {
				t.Errorf("sibling agent costs start at columns %d and %d:\n%s", costCols[0], costCols[1], out)
			}
			// Tree rows must stay inside the 74-column section (2 indent + 72 rule).
			for i, w := range rowWidths {
				if w > 74 {
					t.Errorf("tree row %d is %d columns, exceeds the 74-column separator", i, w)
				}
			}
		})
	}
}
