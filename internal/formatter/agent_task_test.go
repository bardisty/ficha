package formatter

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/models"
)

// longTask is as long as a real agent's description gets.
const longTask = "Check every query in the billing module for N+1 calls"

// taskAgents are agents with every kind of task a row has to fit: one as
// long as they get, one in wide characters, one carrying an escape sequence
// and a newline, one with only a type, and one with neither, in a workflow
// run.
func taskAgents() []models.AgentAnalysis {
	agent := func(id, agentType, description, workflow string, msgs int) models.AgentAnalysis {
		return models.AgentAnalysis{
			AgentID:      id,
			AgentType:    agentType,
			Description:  description,
			WorkflowID:   workflow,
			MessageCount: msgs,
			TotalCost:    models.CostBreakdown{TotalCost: 0.42},
			CostByModel:  map[string]models.CostBreakdown{"claude-sonnet-5": {TotalCost: 0.42}},
			Context:      &models.ContextUsage{Tokens: 50000, Window: 200000, Percent: 25},
		}
	}
	return []models.AgentAnalysis{
		agent("a1b2c3d4e5f6", "general-purpose", longTask, "", 128),
		agent("b1b2c3d4e5f6", "Explore", "認証ミドルウェアを探す", "", 12),
		agent("c1b2c3d4e5f6", "", "\x1b[31mRed\x1b[0m task\nline two", "", 3),
		agent("d1b2c3d4e5f6", "code-reviewer", "", "wf_run", 1),
		agent("e1b2c3d4e5f6", "", "", "wf_run", 9),
	}
}

func taskAnalysis(agents []models.AgentAnalysis) *models.SessionAnalysis {
	return &models.SessionAnalysis{
		SessionID:  "0a1b2c3d-4e5f-6789-abcd-ef0123456789",
		ParentCost: models.CostBreakdown{TotalCost: 7.90},
		AgentsCost: models.CostBreakdown{TotalCost: 2.10},
		HasAgents:  true,
		AgentCount: len(agents),
		Agents:     agents,
		Workflows:  []models.WorkflowMeta{{RunID: "wf_run", Name: "audit-codebase", Status: "completed"}},
	}
}

// checkTaskRows fails when a line of out is wider than width, or prints
// anything of a task's escape sequence but its harmless text.
func checkTaskRows(t *testing.T, out string, width int) {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line is %d columns, wider than %d: %q", w, width, line)
		}
		if strings.Contains(ansiRe.ReplaceAllString(line, ""), "\x1b") {
			t.Errorf("a task's escape sequence reached the output: %q", line)
		}
		if strings.Contains(line, "line two") && !strings.Contains(line, "task line two") {
			t.Errorf("a task's newline reached the output: %q", line)
		}
	}
}

// show's agent rows give a task the room the width has: the message count
// goes before a task is cut, a task is cut by display width with an
// ellipsis, and below render.MinTaskWidth the tasks go and the rows are as
// they are without them. Every cost stays in one column.
func TestAgentBreakdownTasks(t *testing.T) {
	for _, tt := range []struct {
		width          int
		tasks, msgs    bool
		cut, wholeTask string
	}{
		{width: 45, tasks: false, msgs: false},
		{width: 57, tasks: false, msgs: true},
		{width: 58, tasks: true, msgs: false, cut: "Check every…"},
		{width: 60, tasks: true, msgs: false, cut: "Check every q…"},
		{width: 76, tasks: true, msgs: false, cut: "Check every query in the bill…", wholeTask: "認証ミドルウェアを探す"},
	} {
		for _, noColor := range []bool{true, false} {
			out := formatAgentBreakdownContent(taskAnalysis(taskAgents()), tt.width, noColor)
			plain := ansiRe.ReplaceAllString(out, "")
			checkTaskRows(t, out, tt.width)

			var costCols []int
			for line := range strings.SplitSeq(plain, "\n") {
				if before, _, ok := strings.Cut(line, "$"); ok {
					costCols = append(costCols, lipgloss.Width(before))
				}
			}
			for _, col := range costCols {
				if col != costCols[0] {
					t.Errorf("width %d: costs start at columns %v, not one:\n%s", tt.width, costCols, plain)
					break
				}
			}
			if got := strings.Contains(plain, "] Check every"); got != tt.tasks {
				t.Errorf("width %d: tasks shown %v, want %v:\n%s", tt.width, got, tt.tasks, plain)
			}
			if got := strings.Contains(plain, "msgs"); got != tt.msgs {
				t.Errorf("width %d: message counts shown %v, want %v:\n%s", tt.width, got, tt.msgs, plain)
			}
			if tt.cut != "" && !strings.Contains(plain, "] "+tt.cut+" ") {
				t.Errorf("width %d: want the long task cut to %q:\n%s", tt.width, tt.cut, plain)
			}
			if tt.wholeTask != "" && !strings.Contains(plain, tt.wholeTask) {
				t.Errorf("width %d: want %q whole:\n%s", tt.width, tt.wholeTask, plain)
			}
		}
	}
}

// When every task fits whole beside the message count, the count stays,
// and the cost column sits only as far right as the rows need.
func TestAgentBreakdownShortTasksKeepMessageCount(t *testing.T) {
	agents := taskAgents()[3:]
	agents[1].Description = "Find the router"
	out := formatAgentBreakdownContent(taskAnalysis(agents), 76, true)
	want := "  [Ad1b2c3d] code-reviewer   Sonnet 5       1 msg  25% ctx     $0.4200\n"
	if !strings.Contains(out, want) {
		t.Errorf("want the row\n%q\nin\n%s", want, out)
	}
}

// The --expand-agents tree gives tasks the room its cost column has, in the
// same order as show: message counts first, then the task is cut, then it
// goes.
func TestSummaryTreeTasks(t *testing.T) {
	for _, tt := range []struct {
		width       int
		tasks, msgs bool
	}{
		{width: 45, tasks: false, msgs: false},
		{width: 60, tasks: true, msgs: false},
		{width: 76, tasks: true, msgs: false},
	} {
		for _, noColor := range []bool{true, false} {
			r := sessionRow("aaaa1111", "claude-opus-4-8", 10, 3.00)
			r.Analysis.HasAgents = true
			r.Analysis.Agents = taskAgents()
			r.Analysis.AgentCount = len(r.Analysis.Agents)
			r.Analysis.Workflows = taskAnalysis(nil).Workflows
			aggregate := &models.SessionAnalysis{
				SessionID: "aggregate", IsSummary: true, SessionCount: 1,
				CostByModel: map[string]models.CostBreakdown{},
			}
			out := FormatSummaryTableWithDetails(aggregate, []models.SessionResult{r}, "/home/user/src/app", noColor, true, tt.width)
			plain := ansiRe.ReplaceAllString(out, "")
			checkTaskRows(t, out, tt.width)
			if got := strings.Contains(plain, "] Check every"); got != tt.tasks {
				t.Errorf("width %d: tasks shown %v, want %v:\n%s", tt.width, got, tt.tasks, plain)
			}
			if got := strings.Contains(plain, " msgs"); got != tt.msgs {
				t.Errorf("width %d: message counts shown %v, want %v:\n%s", tt.width, got, tt.msgs, plain)
			}
			if tt.tasks && strings.Contains(plain, longTask) {
				t.Errorf("width %d: want the long task cut:\n%s", tt.width, plain)
			}
		}
	}
}

// A task shorter than render.MinTaskWidth is never cut, so it stays where
// the message count has to go.
func TestShortTasksStayWithoutMessageCount(t *testing.T) {
	agents := taskAgents()[3:]
	agents[0].AgentType = "Explore"
	agents[0].MessageCount = 123456
	for _, width := range []int{58, 59, 60, 61} {
		out := formatAgentBreakdownContent(taskAnalysis(agents), width, true)
		checkTaskRows(t, out, width)
		if !strings.Contains(out, "] Explore ") {
			t.Errorf("width %d: want the type-only task whole:\n%s", width, out)
		}
	}

	r := sessionRow("aaaa1111", "claude-opus-4-8", 10, 3.00)
	r.Analysis.HasAgents = true
	r.Analysis.Agents = agents
	r.Analysis.AgentCount = len(agents)
	r.Analysis.Workflows = taskAnalysis(nil).Workflows
	aggregate := &models.SessionAnalysis{
		SessionID: "aggregate", IsSummary: true, SessionCount: 1,
		CostByModel: map[string]models.CostBreakdown{},
	}
	for _, width := range []int{56, 57, 58, 59, 60} {
		out := ansiRe.ReplaceAllString(FormatSummaryTableWithDetails(aggregate, []models.SessionResult{r}, "/home/user/src/app", true, true, width), "")
		checkTaskRows(t, out, width)
		if !strings.Contains(out, "] Explore ") {
			t.Errorf("width %d: want the type-only task whole in the tree:\n%s", width, out)
		}
	}
}

// Without tasks, a message count too wide for the row gives way rather
// than moving the cost column.
func TestNoTasksHugeCountGivesWay(t *testing.T) {
	agents := taskAgents()[4:]
	agents[0].MessageCount = 123456
	// At 57 a task has no room either, and goes.
	tasked := append(taskAgents()[:1], agents...)
	tasked[0].MessageCount = 123456
	cut := ansiRe.ReplaceAllString(formatAgentBreakdownContent(taskAnalysis(tasked), 57, true), "")
	var costCols []int
	for line := range strings.SplitSeq(cut, "\n") {
		if before, _, ok := strings.Cut(line, "$"); ok {
			costCols = append(costCols, lipgloss.Width(before))
		}
	}
	for _, col := range costCols {
		if col != costCols[0] {
			t.Errorf("costs start at columns %v, not one:\n%s", costCols, cut)
			break
		}
	}
	out := formatAgentBreakdownContent(taskAnalysis(agents), 76, true)
	if strings.Contains(out, "msgs") {
		t.Errorf("want the 11-column count dropped:\n%s", out)
	}
	if !strings.Contains(out, "  Parent session                             ") {
		t.Errorf("want the cost column where it is without tasks:\n%s", out)
	}
}

// Transcript text can carry a bidi override, which would turn the rest of
// the row around, and emoji drawn two columns wide: one with a variation
// selector, and a ZWJ sequence. The override goes, the sequences print
// whole, and the costs stay in one column, in show and in the summary tree,
// whole and cut alike.
func TestTasksWithBidiAndEmojiStayInColumns(t *testing.T) {
	var agents []models.AgentAnalysis
	for i, task := range []string{
		"Fix ‮parser tests",
		"Check ☀️ weather API",
		"Pair with \U0001F469‍\U0001F4BB on review",
		"Check ☀️☀️☀️☀️☀️ weather for every region we serve",
	} {
		a := taskAgents()[0]
		a.AgentID = strings.Repeat(string(rune('a'+i)), 12)
		a.Description = task
		agents = append(agents, a)
	}
	check := func(name, out string, width int) {
		t.Helper()
		checkTaskRows(t, out, width)
		plain := ansiRe.ReplaceAllString(out, "")
		if strings.ContainsRune(plain, '‮') {
			t.Errorf("%s: the override reached the output:\n%s", name, plain)
		}
		for _, whole := range []string{"Check ☀️ weather API", "\U0001F469‍\U0001F4BB"} {
			if !strings.Contains(plain, whole) {
				t.Errorf("%s: want %q whole:\n%s", name, whole, plain)
			}
		}
		var costCols []int
		for line := range strings.SplitSeq(plain, "\n") {
			if before, _, ok := strings.Cut(line, "$"); ok && strings.Contains(line, "] ") {
				costCols = append(costCols, lipgloss.Width(before))
			}
		}
		for _, col := range costCols {
			if col != costCols[0] {
				t.Errorf("%s: costs start at columns %v, not one:\n%s", name, costCols, plain)
				break
			}
		}
	}
	for _, noColor := range []bool{true, false} {
		check("show", formatAgentBreakdownContent(taskAnalysis(agents), reportWidth(80), noColor), reportWidth(80))

		r := sessionRow("aaaa1111", "claude-opus-4-8", 10, 3.00)
		r.Analysis.HasAgents = true
		r.Analysis.Agents = agents
		r.Analysis.AgentCount = len(agents)
		aggregate := &models.SessionAnalysis{
			SessionID: "aggregate", IsSummary: true, SessionCount: 1,
			CostByModel: map[string]models.CostBreakdown{},
		}
		check("summary", FormatSummaryTableWithDetails(aggregate, []models.SessionResult{r}, "/home/user/src/app", noColor, true, 80), 80)
	}
}
