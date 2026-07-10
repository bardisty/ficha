package formatter

import (
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

// The summary aggregate sets HasAgents/AgentCount/AgentsCost without collecting
// the per-agent records. Every line of the AGENT SUB-SESSIONS section is
// per-agent, so the section must not render for that shape.
func TestFormatSessionTable_AggregateOmitsAgentSection(t *testing.T) {
	analysis := sampleAnalysis()
	analysis.IsSummary = true
	analysis.HasAgents = true
	analysis.AgentCount = 2
	analysis.ParentCost = models.CostBreakdown{TotalCost: 0.03}
	analysis.AgentsCost = models.CostBreakdown{TotalCost: 0.01}
	analysis.Agents = nil

	output := FormatSessionTable(analysis, true)
	if strings.Contains(output, "AGENT SUB-SESSIONS") {
		t.Error("aggregate table rendered an agent section with no per-agent records")
	}
}

// A real single-session show with agents must still render the section.
func TestFormatSessionTable_SessionRendersAgentSection(t *testing.T) {
	analysis := sampleAnalysis()
	analysis.HasAgents = true
	analysis.AgentCount = 1
	analysis.AgentsCost = models.CostBreakdown{TotalCost: 0.01}
	analysis.Agents = []models.AgentAnalysis{{
		AgentID:     "g1",
		CostByModel: map[string]models.CostBreakdown{"claude-sonnet-5": {TotalCost: 0.01}},
		TotalCost:   models.CostBreakdown{TotalCost: 0.01},
	}}

	output := FormatSessionTable(analysis, true)
	if !strings.Contains(output, "AGENT SUB-SESSIONS") {
		t.Error("single-session table dropped the agent section")
	}
}
