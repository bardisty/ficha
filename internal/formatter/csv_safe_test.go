package formatter

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

// A model ID or agent ID that opens with a formula character must reach the
// spreadsheet as text, not as an expression.
func TestCSVCellNeutralizesFormulas(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"claude-opus-4-8", "claude-opus-4-8"}, // real model ID, untouched
		{"g1", "g1"},                           // real agent ID, untouched
		{"/home/u/.claude/x.jsonl", "/home/u/.claude/x.jsonl"},
		{"=1+1", "'=1+1"},
		{"+cmd", "'+cmd"},
		{"-2+3", "'-2+3"},
		{"@SUM(A1)", "'@SUM(A1)"},
		{"\tlead", "'\tlead"},
		{"\rlead", "'\rlead"},
	} {
		if got := csvCell(tc.in); got != tc.want {
			t.Errorf("csvCell(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The neutralizer must be wired into the per-message table, and the result must
// still parse as one valid CSV table.
func TestFormatSessionCSV_NeutralizesMessageCells(t *testing.T) {
	analysis := sampleAnalysis()
	analysis.Messages = []models.MessageAnalysis{
		{AgentID: "=evil()", Model: "@SUM(A1)", Cost: models.CostBreakdown{TotalCost: 0.01}},
	}

	output, err := FormatSessionCSV(analysis, true)
	if err != nil {
		t.Fatalf("FormatSessionCSV: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(output)).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}
	if records[1][0] != "'=evil()" {
		t.Errorf("agent_id: got %q, want %q", records[1][0], "'=evil()")
	}
	if records[1][2] != "'@SUM(A1)" {
		t.Errorf("model: got %q, want %q", records[1][2], "'@SUM(A1)")
	}
}

func TestUnpricedModelsCell(t *testing.T) {
	for _, tc := range []struct {
		ids  []string
		want string
	}{
		{nil, "[]"},
		{[]string{"claude-zeta-9"}, `["claude-zeta-9"]`},
		{[]string{"claude-zeta-9", "us.anthropic.claude-omega-1-v1:0"}, `["claude-zeta-9","us.anthropic.claude-omega-1-v1:0"]`},
		// Raw transcript IDs: an absent model, a space, a quote, a comma and a
		// formula all survive, and the cell never opens like a formula.
		{[]string{"", "=cmd", "my model", `a"b,c`, "<x>"}, `["","=cmd","my model","a\"b,c","<x>"]`},
	} {
		if got := unpricedModelsCell(tc.ids); got != tc.want {
			t.Errorf("unpricedModelsCell(%q) = %q, want %q", tc.ids, got, tc.want)
		}
	}
}
