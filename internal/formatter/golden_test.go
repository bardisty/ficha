package formatter

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/styles"
)

var update = flag.Bool("update", false, "rewrite .golden files with current rendered output")

// TestMain pins the process timezone and the reports' clock: dates render
// in local time, and carry their year only when it isn't the current one, so
// goldens would otherwise depend on the machine's TZ and on the date. The
// clock is late on the day the golden sessions ran, so their insight times
// are today's and print bare; the multi-day goldens move it themselves.
//
// The palette is dark unless a test calls styles.SetDark(false), and colored
// output always carries its xterm-256 escapes. Nothing here looks at a
// terminal: cmd does, and cuts the escapes down to what stdout takes.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	now = func() time.Time { return time.Date(2026, 1, 15, 23, 0, 0, 0, time.UTC) }
	os.Exit(m.Run())
}

// checkGolden compares got against testdata/<name>.golden byte-for-byte.
// Run `go test ./internal/formatter -update` (or `make update-golden`) to
// regenerate after an intentional rendering change, then review the diff.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("creating testdata dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		return
	}
	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file %s (regenerate with `make update-golden`): %v", path, err)
	}
	want := string(wantBytes)
	if got == want {
		return
	}
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			t.Fatalf("%s: output differs from golden at line %d\n got: %q\nwant: %q\n(intentional change? regenerate with `make update-golden` and review the diff)",
				path, i+1, g, w)
		}
	}
	t.Fatalf("%s: output differs from golden (same lines, different trailing bytes)", path)
}

// --- fixtures (all timestamps fixed, UTC) ---

func goldenTime(hour, min, sec int) time.Time {
	return time.Date(2026, 1, 15, hour, min, sec, 0, time.UTC)
}

// goldenShowAnalysis exercises every section of the show-mode table: both
// cache TTLs, cache reads, savings, context window, three models, two agent
// sub-sessions (one with a single message), and full insights with a trend.
func goldenShowAnalysis() *models.SessionAnalysis {
	return &models.SessionAnalysis{
		SessionID:    "0a1b2c3d-4e5f-6789-abcd-ef0123456789",
		Project:      "~/src/app",
		ProjectPath:  "/home/user/.claude/projects/-home-user-src-app/0a1b2c3d.jsonl",
		StartTime:    goldenTime(10, 0, 0),
		EndTime:      goldenTime(11, 30, 0),
		Duration:     models.Duration(90 * time.Minute),
		MessageCount: 37,
		TotalUsage: models.TokenUsage{
			InputTokens:              45678,
			OutputTokens:             23456,
			CacheCreationInputTokens: 250000,
			CacheReadInputTokens:     1500000,
			CacheCreation: &models.CacheCreation{
				Ephemeral5mInputTokens: 200000,
				Ephemeral1hInputTokens: 50000,
			},
		},
		TotalCost: models.CostBreakdown{
			InputCost:        0.30,
			OutputCost:       2.10,
			CacheWrite5mCost: 3.75,
			CacheWrite1hCost: 1.50,
			CacheReadCost:    2.25,
			TotalCost:        9.90,
			CacheSavings:     20.25,
		},
		CostByModel: map[string]models.CostBreakdown{
			"claude-opus-4-8":  {TotalCost: 7.90},
			"claude-haiku-4-5": {TotalCost: 0.42},
			"claude-sonnet-5":  {TotalCost: 1.58},
		},
		LastMessageUsage: models.TokenUsage{
			InputTokens:              12,
			CacheCreationInputTokens: 8000,
			CacheReadInputTokens:     371988,
			CacheCreation: &models.CacheCreation{
				Ephemeral5mInputTokens: 8000,
			},
		},
		LastMessageModel: "claude-opus-4-8",
		Agents: []models.AgentAnalysis{
			{
				AgentID:      "a1b2c3d4e5f6",
				AgentType:    "Explore",
				Description:  "Find the auth middleware",
				MessageCount: 12,
				TotalCost:    models.CostBreakdown{TotalCost: 0.42},
				CostByModel: map[string]models.CostBreakdown{
					"claude-haiku-4-5": {TotalCost: 0.42},
				},
				StartTime: goldenTime(10, 15, 0),
				EndTime:   goldenTime(10, 25, 0),
				Duration:  models.Duration(10 * time.Minute),
				// At its 200K mark, so the color goldens show it red
				Context: &models.ContextUsage{Tokens: 200000, Window: 200000, Percent: 100, CompactAt: 200000},
			},
			{
				// No description, so the row shows the type
				AgentID:      "f6e5d4c3b2a1",
				AgentType:    "code-reviewer",
				MessageCount: 1,
				TotalCost:    models.CostBreakdown{TotalCost: 1.58},
				CostByModel: map[string]models.CostBreakdown{
					"claude-sonnet-5": {TotalCost: 1.58},
				},
				StartTime: goldenTime(11, 0, 0),
				EndTime:   goldenTime(11, 2, 0),
				Duration:  models.Duration(2 * time.Minute),
				Context:   &models.ContextUsage{Tokens: 380000, Window: 1000000, Percent: 38, CompactAt: 967000},
			},
		},
		ParentCost: models.CostBreakdown{TotalCost: 7.90},
		ParentCostByModel: map[string]models.CostBreakdown{
			"claude-opus-4-8": {TotalCost: 7.90},
		},
		AgentsCost:         models.CostBreakdown{TotalCost: 2.00},
		HasAgents:          true,
		AgentCount:         2,
		ParentMessageCount: 24,
		AgentMessageCount:  13,
		Insights: &models.MessageInsights{
			FirstMessage: &models.MessageSnapshot{
				Index: 1, Timestamp: goldenTime(10, 0, 5),
				Cost: 0.35, MainCostComponent: "cache_write_5m", MainCostValue: 0.30,
			},
			LastMessage: &models.MessageSnapshot{
				Index: 24, Timestamp: goldenTime(11, 29, 55),
				Cost: 0.52, MainCostComponent: "cache_read", MainCostValue: 0.31,
			},
			HighestCost: &models.MessageSnapshot{
				Index: 9, Timestamp: goldenTime(10, 42, 13),
				Cost: 1.87, MainCostComponent: "output", MainCostValue: 1.02,
			},
			CostTrend:     models.TrendIncreasing,
			RecentAvgCost: 0.55,
			TrendWindow:   12,
			AverageCost:   0.41,
			MessageCount:  24,
		},
	}
}

// goldenSummaryAnalysis mirrors what cmd/summary builds: an
// AnalyzeMultipleSessions aggregate with IsSummary and SessionCount set.
func goldenSummaryAnalysis() *models.SessionAnalysis {
	return &models.SessionAnalysis{
		SessionID:    "aggregate",
		Project:      "~/src/app",
		StartTime:    time.Date(2025, 12, 28, 9, 0, 0, 0, time.UTC),
		EndTime:      time.Date(2026, 1, 14, 17, 45, 0, 0, time.UTC),
		Duration:     models.Duration(104*time.Hour + 45*time.Minute),
		MessageCount: 61,
		TotalUsage: models.TokenUsage{
			InputTokens:              98765,
			OutputTokens:             54321,
			CacheCreationInputTokens: 400000,
			CacheReadInputTokens:     9800000,
			CacheCreation: &models.CacheCreation{
				Ephemeral5mInputTokens: 320000,
				Ephemeral1hInputTokens: 80000,
			},
		},
		TotalCost: models.CostBreakdown{
			InputCost:        0.55,
			OutputCost:       3.05,
			CacheWrite5mCost: 6.00,
			CacheWrite1hCost: 2.40,
			CacheReadCost:    14.70,
			TotalCost:        26.70,
			CacheSavings:     132.30,
		},
		CostByModel: map[string]models.CostBreakdown{
			"claude-opus-4-8":  {TotalCost: 24.10},
			"claude-haiku-4-5": {TotalCost: 2.60},
		},
		ParentCost:         models.CostBreakdown{TotalCost: 24.10},
		AgentsCost:         models.CostBreakdown{TotalCost: 2.60},
		ParentMessageCount: 48,
		AgentMessageCount:  13,
		IsSummary:          true,
		SessionCount:       3,
	}
}

func goldenSessionEntries() []models.SessionEntry {
	return []models.SessionEntry{
		{
			SessionID:         "0a1b2c3d-4e5f-6789-abcd-ef0123456789",
			FullPath:          "/p/0a1b2c3d.jsonl",
			MessageCount:      24,
			Created:           time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
			Modified:          time.Date(2026, 1, 15, 11, 30, 0, 0, time.UTC),
			AgentCount:        2,
			AgentMessageCount: 13,
		},
		{
			// 40 chars: exceeds the list's 36-char column and gets "..." truncated
			SessionID:    "0123456789abcdef0123456789abcdef01234567",
			FullPath:     "/p/0123456789.jsonl",
			MessageCount: 150,
			Created:      time.Date(2026, 1, 12, 8, 0, 0, 0, time.UTC),
			Modified:     time.Date(2026, 1, 14, 22, 5, 0, 0, time.UTC),
		},
		{
			SessionID:    "sess-empty",
			FullPath:     "/p/sess-empty.jsonl",
			MessageCount: 0,
			Created:      time.Date(2026, 1, 11, 7, 0, 0, 0, time.UTC),
			Modified:     time.Date(2026, 1, 11, 7, 0, 0, 0, time.UTC),
		},
	}
}

func goldenGlobalAnalysis() *models.GlobalAnalysis {
	return &models.GlobalAnalysis{
		Projects: []models.ProjectAnalysis{
			{
				ProjectInfo:  models.ProjectInfo{DisplayName: "example"},
				TotalCost:    models.CostBreakdown{TotalCost: 123.45},
				SessionCount: 42,
				MessageCount: 2400,
				LastActive:   time.Date(2026, 1, 15, 11, 30, 0, 0, time.UTC),
			},
			{
				// 59 chars: exceeds the 45-char project column, middle-truncated
				ProjectInfo:  models.ProjectInfo{DisplayName: "a-very-long-project-directory-name-that-exceeds-the-column"},
				TotalCost:    models.CostBreakdown{TotalCost: 56.78},
				SessionCount: 17,
				MessageCount: 1200,
				LastActive:   time.Date(2026, 1, 14, 9, 0, 0, 0, time.UTC),
			},
			{
				ProjectInfo:  models.ProjectInfo{DisplayName: "web-app"},
				TotalCost:    models.CostBreakdown{TotalCost: 9.87},
				SessionCount: 5,
				MessageCount: 500,
				LastActive:   time.Date(2026, 1, 10, 16, 20, 0, 0, time.UTC),
			},
			{
				ProjectInfo:  models.ProjectInfo{DisplayName: "dotfiles"},
				TotalCost:    models.CostBreakdown{TotalCost: 1.23},
				SessionCount: 3,
				MessageCount: 180,
				LastActive:   time.Date(2025, 12, 20, 12, 0, 0, 0, time.UTC),
			},
			{
				ProjectInfo:  models.ProjectInfo{DisplayName: "scratch"},
				TotalCost:    models.CostBreakdown{TotalCost: 0.05},
				SessionCount: 1,
				MessageCount: 41,
				LastActive:   time.Date(2025, 12, 1, 8, 30, 0, 0, time.UTC),
			},
		},
		TotalCost: models.CostBreakdown{
			InputCost:        40.00,
			OutputCost:       90.00,
			CacheWrite5mCost: 30.00,
			CacheWrite1hCost: 11.38,
			CacheReadCost:    20.00,
			TotalCost:        191.38,
			CacheSavings:     180.00,
		},
		TotalUsage: models.TokenUsage{
			InputTokens:              2345678,
			OutputTokens:             1234567,
			CacheCreationInputTokens: 3500000,
			CacheReadInputTokens:     200000000,
			CacheCreation: &models.CacheCreation{
				Ephemeral5mInputTokens: 3000000,
				Ephemeral1hInputTokens: 500000,
			},
		},
		CostByModel: map[string]models.CostBreakdown{
			"claude-opus-4-8":  {TotalCost: 150.00},
			"claude-sonnet-5":  {TotalCost: 40.00},
			"claude-haiku-4-5": {TotalCost: 1.38},
		},
		ProjectCount:       5,
		SessionCount:       68,
		MessageCount:       4321,
		ParentCost:         models.CostBreakdown{TotalCost: 142.50},
		AgentsCost:         models.CostBreakdown{TotalCost: 48.88},
		AgentCount:         31,
		ParentMessageCount: 2987,
		AgentMessageCount:  1334,
		FirstActive:        time.Date(2025, 12, 1, 8, 30, 0, 0, time.UTC),
		LastActive:         time.Date(2026, 1, 15, 11, 30, 0, 0, time.UTC),
		Duration:           models.Duration(45 * 24 * time.Hour),
	}
}

// --- golden tests: FormatSessionTable ---

func TestGoldenSessionTableShow(t *testing.T) {
	checkGolden(t, "session_table_show", FormatSessionTable(goldenShowAnalysis(), true, 0))
}

func TestGoldenSessionTableShowColor(t *testing.T) {
	checkGolden(t, "session_table_show_color", FormatSessionTable(goldenShowAnalysis(), false, 0))
}

// A titled session's header carries its title on a second line, cut to the
// panel and dimmed in color; the first line keeps its fields at any width.
func TestGoldenSessionTableShowTitle(t *testing.T) {
	a := goldenShowAnalysis()
	a.Title = "Migrate the billing service from the legacy monolith to the new payments platform"
	checkGolden(t, "session_table_show_title", FormatSessionTable(a, true, 0))
	checkGolden(t, "session_table_show_title_w45", FormatSessionTable(a, true, 45))
	checkGolden(t, "session_table_show_title_color", FormatSessionTable(a, false, 0))
}

// A model ficha doesn't know has no window: the gauge gives the tokens and
// says why, and so does an agent's cell, in the same 8 columns.
func TestGoldenSessionTableShowWindowUnknown(t *testing.T) {
	a := goldenShowAnalysis()
	a.LastMessageModel = "claude-mystery-9"
	a.CostByModel["claude-mystery-9"] = models.CostBreakdown{TotalCost: 1.20}
	a.LastMessageUsage = models.TokenUsage{InputTokens: 400, CacheReadInputTokens: 680_000}
	a.Agents[0].CostByModel = map[string]models.CostBreakdown{"claude-mystery-9": {TotalCost: 0.42}}
	a.Agents[0].Context = &models.ContextUsage{Tokens: 680_400}
	checkGolden(t, "session_table_show_window_unknown", FormatSessionTable(a, true, 0))
	checkGolden(t, "session_table_show_window_unknown_w45", FormatSessionTable(a, true, 45))
}

// A fork's file holds replies another session in its project also has. Its
// footer says how many, their cost, where list and summary count them and
// what they put the session at, wrapped to the width, dim in color, and in
// plain words under --ascii. Copies credited to several sessions name none
// of them, and a file that's all copies says list calls it one.
func TestGoldenSessionTableShowFork(t *testing.T) {
	a := multiDayShowAnalysis()
	a.Copies = &models.CopiedReplies{Messages: 20, Cost: 0.4072, SessionID: "66666666-ffff-4fff-8fff-000000000006", Sessions: 1}
	checkGolden(t, "session_table_show_fork", FormatSessionTable(a, true, 0))
	checkGolden(t, "session_table_show_fork_w45", FormatSessionTable(a, true, 45))
	checkGolden(t, "session_table_show_fork_color", FormatSessionTable(a, false, 0))
	a.Copies = &models.CopiedReplies{Messages: 1, Cost: 0.0205, Sessions: 1, SessionID: "66666666-ffff-4fff-8fff-000000000006"}
	checkGolden(t, "session_table_show_fork_one_w45", FormatSessionTable(a, true, 45))
	a.Copies = &models.CopiedReplies{Messages: 12, Cost: 3.13, Sessions: 2}
	checkGolden(t, "session_table_show_fork_split_w45", FormatSessionTable(a, true, 45))
	a.Copies = &models.CopiedReplies{Messages: a.MessageCount, Cost: a.TotalCost.TotalCost, SessionID: "66666666-ffff-4fff-8fff-000000000006", Sessions: 1}
	checkGolden(t, "session_table_show_fork_copy_w45", FormatSessionTable(a, true, 45))
	useASCII(t)
	checkGolden(t, "session_table_show_fork_ascii", FormatSessionTable(a, true, 0))
}

// multiDayShowAnalysis is goldenShowAnalysis picked up eleven days after it
// began: its first message from then, its peak from yesterday and its last
// from today.
func multiDayShowAnalysis() *models.SessionAnalysis {
	a := goldenShowAnalysis()
	a.StartTime = goldenTime(10, 0, 0).AddDate(0, 0, -11)
	a.Duration = models.Duration(a.EndTime.Sub(a.StartTime))
	a.Insights.FirstMessage.Timestamp = goldenTime(10, 0, 5).AddDate(0, 0, -11)
	a.Insights.HighestCost.Timestamp = goldenTime(10, 42, 13).AddDate(0, 0, -1)
	return a
}

// A session that spans days reads its length in days and hours, and its
// insight times carry the day they're from: the weekday for yesterday, the
// date too for eleven days back, nothing for today. At 45 columns First and
// Last drop their detail together, and First's dated time, too wide beside
// its cost, takes its short form.
func TestGoldenSessionTableShowMultiDay(t *testing.T) {
	checkGolden(t, "session_table_show_multiday", FormatSessionTable(multiDayShowAnalysis(), true, 0))
	checkGolden(t, "session_table_show_multiday_w45", FormatSessionTable(multiDayShowAnalysis(), true, 45))
}

func TestGoldenSessionTableSummary(t *testing.T) {
	checkGolden(t, "session_table_summary", FormatSessionTable(goldenSummaryAnalysis(), true, 0))
}

// --- golden tests: FormatSessionListTable ---

// goldenListResults pairs goldenSessionEntries with analyses: a titled
// session with agents, an untitled one whose long title-less row falls back
// to "-", a session with no replies yet, and one whose transcript couldn't
// be read.
func goldenListResults() []models.SessionResult {
	entries := goldenSessionEntries()
	opus := map[string]models.CostBreakdown{"claude-opus-4-8": {TotalCost: 12.34}}
	results := []models.SessionResult{
		{Entry: entries[0], Analysis: &models.SessionAnalysis{
			Title:             "Refactor the auth middleware to use short-lived tokens",
			Duration:          models.Duration(83 * time.Minute),
			MessageCount:      24,
			AgentCount:        2,
			TotalCost:         models.CostBreakdown{TotalCost: 12.34},
			ParentCostByModel: opus,
			CostByModel:       opus,
		}},
		{Entry: entries[1], Analysis: &models.SessionAnalysis{
			Duration:          models.Duration(47*time.Minute + 5*time.Second),
			MessageCount:      150,
			TotalCost:         models.CostBreakdown{TotalCost: 0.4321},
			ParentCostByModel: map[string]models.CostBreakdown{"claude-sonnet-5": {TotalCost: 0.4321}},
		}},
		{Entry: entries[2], Analysis: &models.SessionAnalysis{}},
		{Entry: models.SessionEntry{
			SessionID: "deadbeef-0000-4000-8000-000000000000",
			Modified:  time.Date(2025, 11, 2, 9, 0, 0, 0, time.UTC),
		}},
	}
	return results
}

var goldenListOptions = ListTableOptions{Now: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), Project: "~/src/app"}

func TestGoldenSessionList(t *testing.T) {
	checkGolden(t, "session_list", FormatSessionListTable(goldenListResults(), true, goldenListOptions))
}

// A window adds its range to the header, which keeps it when the box is too
// narrow for the project.
func TestGoldenSessionListWindow(t *testing.T) {
	opts := goldenListOptions
	opts.Window = &models.TimeWindow{Since: time.Date(2026, 1, 8, 0, 0, 0, 0, time.Local)}
	checkGolden(t, "session_list_window", FormatSessionListTable(goldenListResults()[:2], true, opts))
	opts.Width = 40
	checkGolden(t, "session_list_window_40", FormatSessionListTable(goldenListResults()[:2], true, opts))
}

// A window with no messages in it prints the line summary prints.
func TestSessionListEmptyWindow(t *testing.T) {
	opts := goldenListOptions
	opts.Window = &models.TimeWindow{Since: time.Date(2026, 1, 8, 0, 0, 0, 0, time.Local)}
	got := FormatSessionListTable(goldenListResults()[2:], true, opts)
	if want := emptyWindow(*opts.Window, opts.Project); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// list's header counts the sessions it read, as summary does, and names the
// unreadable ones apart. A box too narrow for both keeps the project and
// the session count, but where the project goes beside a window anyway,
// the unreadable count stays.
func TestSessionListCountsUnreadableApart(t *testing.T) {
	windowed := goldenListOptions
	windowed.Project = "~/source/webapp-api"
	windowed.Window = &models.TimeWindow{Since: time.Date(2026, 1, 8, 0, 0, 0, 0, time.Local)}
	for width, want := range map[int]string{
		80: "~/src/app  │  3 sessions, 1 unreadable  ",
		40: "~/src/app  │  3 sessions  ",
		55: "║  3 sessions, 1 unreadable  │  Window: Jan 08 → now  ║",
	} {
		opts := goldenListOptions
		if width == 55 {
			opts = windowed
		}
		opts.Width = width
		out := FormatSessionListTable(goldenListResults(), true, opts)
		if !strings.Contains(out, want) {
			t.Errorf("width %d: want %q in the header:\n%s", width, want, out)
		}
		if w := maxLineWidth(out); w > width {
			t.Errorf("width %d: widest line is %d:\n%s", width, w, out)
		}
	}
}

// A title cut at a space loses the space too, as show and watch cut tasks:
// "Refactor the…", not "Refactor the …".
func TestSessionListCutsTitlesWithoutASpaceBeforeTheEllipsis(t *testing.T) {
	cuts := 0
	for width := 40; width <= 120; width++ {
		opts := goldenListOptions
		opts.Width = width
		out := FormatSessionListTable(goldenListResults(), true, opts)
		if strings.Contains(out, " "+styles.Ellipsis) {
			t.Errorf("width %d: a space before the ellipsis:\n%s", width, out)
		}
		cuts += strings.Count(out, "Refactor the"+styles.Ellipsis)
	}
	if cuts == 0 {
		t.Error("no width cut the title at a space, so nothing was checked")
	}
}

// listHeaderReference is list's header as FormatSessionListTable fitted it
// with its own copy of renderPanel's room math, the reference the shared
// panelFits must agree with.
func listHeaderReference(results []models.SessionResult, opts ListTableOptions, width int) string {
	readable := 0
	for _, r := range results {
		if r.Analysis != nil {
			readable++
		}
	}
	count := fmt.Sprintf("%d %s", readable, sessionsWord(readable))
	fields := []string{count}
	if unreadable := len(results) - readable; unreadable > 0 {
		fields[0] += fmt.Sprintf(", %d unreadable", unreadable)
	}
	giveWay := []int{panelLead}
	if opts.Window != nil {
		when := spanOrWindow(opts.Window, time.Time{}, time.Time{})
		if lipgloss.Width(when) > width-6 {
			when = strings.TrimPrefix(when, "Window: ")
		}
		fields = append(fields, when)
		giveWay = []int{panelLead, 0}
	}
	fits := func(head string, project bool) bool {
		need := append([]string{head}, fields[1:]...)
		if project && models.StripControl(opts.Project) != "" {
			need = append(need, strings.Repeat(" ", min(lipgloss.Width(models.StripControl(opts.Project)), minProjectWidth)))
		}
		return lipgloss.Width(strings.Join(need, "  "+styles.BoxVerticalSep+"  ")) <= width-6
	}
	if !fits(fields[0], true) && (fits(count, true) || !fits(fields[0], false)) {
		fields[0] = count
	}
	return renderPanel(opts.Project, fields, giveWay, width, true)
}

// list's header reads the same at every width as it did when list fitted it
// with its own copy of renderPanel's math.
func TestSessionListHeaderMatchesItsReference(t *testing.T) {
	window := &models.TimeWindow{Since: time.Date(2026, 1, 8, 0, 0, 0, 0, time.Local)}
	until := &models.TimeWindow{Since: time.Date(2025, 12, 1, 0, 0, 0, 0, time.Local), Until: time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)}
	for _, project := range []string{"~/src/app", "~/source/webapp-api", "", "~/work/日本語のプロジェクト", "~/source/a-rather-long-project-directory-name"} {
		for _, w := range []*models.TimeWindow{nil, window, until} {
			for _, results := range [][]models.SessionResult{goldenListResults(), goldenListResults()[:2]} {
				for width := 40; width <= 120; width++ {
					opts := goldenListOptions
					opts.Project, opts.Window, opts.Width = project, w, width
					out := FormatSessionListTable(results, true, opts)
					lines := strings.SplitN(out, "\n", 4)
					got := strings.Join(lines[:3], "\n")
					if want := listHeaderReference(results, opts, lipgloss.Width(lines[0])); got != want {
						t.Errorf("project %q, window %v, %d results, width %d:\n got\n%s\nwant\n%s", project, w, len(results), width, got, want)
					}
				}
			}
		}
	}
}

// A project that's nothing but bidi characters prints as no project, so
// the header saves it no room either.
func TestSessionListBidiOnlyProjectTakesNoRoom(t *testing.T) {
	for width := 20; width <= 80; width++ {
		opts := goldenListOptions
		opts.Width = width
		opts.Project = ""
		want := FormatSessionListTable(goldenListResults(), true, opts)
		opts.Project = "\u202e\u2066"
		if got := FormatSessionListTable(goldenListResults(), true, opts); got != want {
			t.Errorf("width %d: got\n%s\nwant\n%s", width, got, want)
		}
	}
}

func TestGoldenSessionListColor(t *testing.T) {
	checkGolden(t, "session_list_color", FormatSessionListTable(goldenListResults(), false, goldenListOptions))
}

// Sessions of a day or more read LENGTH in days and hours.
func TestGoldenSessionListMultiDay(t *testing.T) {
	results := goldenListResults()[:2]
	results[0].Analysis.Duration = models.Duration(27*time.Hour + 6*time.Minute)
	results[1].Analysis.Duration = models.Duration(264*time.Hour + 9*time.Minute)
	checkGolden(t, "session_list_multiday", FormatSessionListTable(results, true, goldenListOptions))
}

// Under --sort-by cost the header names the order. The rows come in
// already sorted: the costlier untitled session above the titled one, then
// the session with no replies, then the unreadable one.
func TestGoldenSessionListByCost(t *testing.T) {
	results := goldenListResults()
	results[1].Analysis.TotalCost.TotalCost = 50
	SortSessionsByCost(results)
	opts := goldenListOptions
	opts.SortBy = SortByCost
	checkGolden(t, "session_list_cost", FormatSessionListTable(results, true, opts))
}

// The cost order's name in the header never pushes anything out: at every
// width, with and without a window, the header holds what it holds in time
// order, plus "by cost" where that still fits.
func TestSessionListCostOrderNameGivesWayFirst(t *testing.T) {
	header := func(out string) string {
		return strings.Join(strings.Fields(strings.Split(out, "\n")[1]), " ")
	}
	for _, window := range []*models.TimeWindow{nil, {Since: time.Date(2026, 1, 8, 0, 0, 0, 0, time.Local)}} {
		for width := 30; width <= 100; width++ {
			opts := goldenListOptions
			opts.Project = "~/source/claude-code-usage"
			opts.Window, opts.Width = window, width
			byTime := header(FormatSessionListTable(goldenListResults(), true, opts))
			opts.SortBy = SortByCost
			byCost := header(FormatSessionListTable(goldenListResults(), true, opts))
			if strings.Replace(byCost, " by cost", "", 1) != byTime {
				t.Errorf("width %d, window %v: by cost %q, by time %q", width, window != nil, byCost, byTime)
			}
			if width == 100 && !strings.Contains(byCost, "by cost") {
				t.Errorf("window %v: a wide header should name the order: %q", window != nil, byCost)
			}
		}
	}
}

// forkListResults is goldenListResults' priced sessions beside two copies:
// transcripts that hold replies, all of them already counted under another
// session. One has its own title and one doesn't.
func forkListResults() []models.SessionResult {
	results := goldenListResults()[:2]
	entries := goldenSessionEntries()
	copied := entries[2]
	titled := copied
	titled.SessionID = "c0ffee00-0000-4000-8000-000000000000"
	return append(results,
		models.SessionResult{Entry: copied, Analysis: &models.SessionAnalysis{TranscriptMessages: 240}},
		models.SessionResult{Entry: titled, Analysis: &models.SessionAnalysis{TranscriptMessages: 20, Title: "Move sessions to Redis"}},
	)
}

// A copy of another session's replies says so, rather than reading as a
// session with no replies yet.
func TestGoldenSessionListFork(t *testing.T) {
	checkGolden(t, "session_list_fork", FormatSessionListTable(forkListResults(), true, goldenListOptions))
	checkGolden(t, "session_list_fork_color", FormatSessionListTable(forkListResults(), false, goldenListOptions))
}

// Beside one priced session, a session with no replies leaves the priced
// one the neutral color a lone session gets, as summary --details does.
func TestGoldenSessionListNoRepliesColor(t *testing.T) {
	checkGolden(t, "session_list_no_replies_color", FormatSessionListTable(goldenListResults()[1:3], false, goldenListOptions))
}

// --- golden tests: FormatGlobalTable ---

// goldenGlobalOptions is the piped (fixed-width) layout, cost order, top 3,
// half an hour after the newest project's last activity.
func goldenGlobalOptions(details bool) GlobalTableOptions {
	return GlobalTableOptions{
		TopN:    3,
		Details: details,
		SortBy:  "cost",
		Now:     time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}
}

func TestGoldenGlobalTable(t *testing.T) {
	checkGolden(t, "global_table", FormatGlobalTable(goldenGlobalAnalysis(), true, goldenGlobalOptions(false)))
}

func TestGoldenGlobalTableColor(t *testing.T) {
	checkGolden(t, "global_table_color", FormatGlobalTable(goldenGlobalAnalysis(), false, goldenGlobalOptions(false)))
}

func TestGoldenGlobalTableDetails(t *testing.T) {
	checkGolden(t, "global_table_details", FormatGlobalTable(goldenGlobalAnalysis(), true, goldenGlobalOptions(true)))
}

// worktreeGlobalAnalysis has a repo, three of its worktrees, and worktrees
// of two other repos, one sharing a worktree's name with the first repo's.
func worktreeGlobalAnalysis() *models.GlobalAnalysis {
	a := &models.GlobalAnalysis{}
	for i, p := range []struct {
		name string
		cost float64
	}{
		{"~/work/webapp", 73.99},
		{"~/work/webapp/.claude/worktrees/feat-x", 17.38},
		{"~/source/billing-dashboard/.claude/worktrees/fix-rounding", 12.34},
		{"~/source/payments-service/.claude/worktrees/grpc-client", 8.20},
		{"~/source/payments-service/.claude/worktrees/fix-rounding", 4.70},
		{"~/source/payments-service", 2.05},
	} {
		a.Projects = append(a.Projects, models.ProjectAnalysis{
			ProjectInfo:  models.ProjectInfo{DisplayName: p.name},
			TotalCost:    models.CostBreakdown{TotalCost: p.cost},
			SessionCount: 1,
			MessageCount: 40 * (i + 1),
			LastActive:   time.Date(2026, 1, 15, 11-i, 30, 0, 0, time.UTC),
		})
		a.TotalCost.TotalCost += p.cost
		a.SessionCount++
		a.MessageCount += 40 * (i + 1)
	}
	a.ProjectCount = len(a.Projects)
	return a
}

// worktreeGlobalOptions shows every row of worktreeGlobalAnalysis, without
// the cumulative column details adds.
func worktreeGlobalOptions() GlobalTableOptions {
	opts := goldenGlobalOptions(false)
	opts.TopN = 10
	return opts
}

// Piped, worktree rows keep their repo's and their worktree's names.
func TestGoldenGlobalTableWorktrees(t *testing.T) {
	checkGolden(t, "global_table_worktrees", FormatGlobalTable(worktreeGlobalAnalysis(), true, worktreeGlobalOptions()))
}

// --- golden tests: FormatSummaryTableWithDetails ---
//
// The formatter is pure: it renders the per-session results that
// AnalyzeMultipleSessions produces, doing no parsing itself. These tests build
// a real temp-dir project fixture and run it through AnalyzeMultipleSessions so
// the results (and the goldens) reflect the real analysis pipeline. Costs in
// the goldens therefore come from the live pricing catalog; a pricing change
// legitimately changes them.

const goldenAlphaID = "aaaa1111-2222-3333-4444-555566667777"
const goldenBetaID = "bbbb2222-3333-4444-5555-666677778888"
const goldenEmptyID = "eeee4444-5555-6666-7777-888899990000"
const goldenGhostID = "cccc3333-4444-5555-6666-777788889999"

func summaryDetailsFixture(t *testing.T) []models.SessionEntry {
	t.Helper()
	dir := t.TempDir()

	writeFixture := func(rel, content string) string {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		return path
	}

	msg := func(ts, id, model string, in, out, cc5m, cc1h, read int64) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"req_%s","message":{"id":%q,"model":%q,"usage":{"input_tokens":%d,"output_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"cache_creation":{"ephemeral_5m_input_tokens":%d,"ephemeral_1h_input_tokens":%d}}}}`,
			ts, id, id, model, in, out, cc5m+cc1h, read, cc5m, cc1h)
	}

	alphaPath := writeFixture(goldenAlphaID+".jsonl", strings.Join([]string{
		msg("2026-01-10T09:00:00Z", "a1", "claude-opus-4-8", 1200, 800, 3000, 1000, 20000),
		msg("2026-01-10T09:05:00Z", "a2", "claude-opus-4-8", 400, 1500, 2000, 0, 30000),
		msg("2026-01-10T09:12:00Z", "a3", "claude-opus-4-8", 90, 60, 0, 0, 50000),
	}, "\n")+"\n")

	betaPath := writeFixture(goldenBetaID+".jsonl", strings.Join([]string{
		msg("2026-01-12T18:00:00Z", "b1", "claude-haiku-4-5", 5000, 2500, 10000, 0, 0),
		msg("2026-01-12T18:20:00Z", "b2", "claude-haiku-4-5", 300, 900, 0, 0, 15000),
	}, "\n")+"\n")

	writeFixture(filepath.Join(goldenBetaID, "subagents", "agent-gld1.jsonl"), strings.Join([]string{
		msg("2026-01-12T18:05:00Z", "g1", "claude-sonnet-5", 2000, 1200, 5000, 0, 0),
		msg("2026-01-12T18:08:00Z", "g2", "claude-sonnet-5", 100, 400, 0, 0, 7000),
	}, "\n")+"\n")
	writeFixture(filepath.Join(goldenBetaID, "subagents", "agent-gld1.meta.json"),
		`{"agentType":"general-purpose","description":"Summarize the failing tests","spawnDepth":1}`)

	return []models.SessionEntry{
		{
			SessionID: goldenAlphaID,
			FullPath:  alphaPath,
			Created:   time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC),
			Modified:  time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC),
		},
		{
			SessionID: goldenBetaID,
			FullPath:  betaPath,
			Created:   time.Date(2026, 1, 12, 18, 0, 0, 0, time.UTC),
			Modified:  time.Date(2026, 1, 12, 18, 30, 0, 0, time.UTC),
		},
		{
			// File does not exist: renders the "(error)" row
			SessionID: goldenGhostID,
			FullPath:  filepath.Join(dir, "missing.jsonl"),
			Created:   time.Date(2026, 1, 14, 9, 0, 0, 0, time.UTC),
			Modified:  time.Date(2026, 1, 14, 9, 15, 0, 0, time.UTC),
		},
	}
}

// summaryDetailsAnalysis aggregates the fixture the same way cmd/summary does,
// returning both the aggregate and the per-session results the formatter renders.
func summaryDetailsAnalysis(t *testing.T, entries []models.SessionEntry) (*models.SessionAnalysis, []models.SessionResult) {
	t.Helper()
	analysis, results, err := analyzer.AnalyzeMultipleSessions(entries)
	if err != nil {
		t.Fatalf("AnalyzeMultipleSessions: %v", err)
	}
	analysis.IsSummary = true
	analysis.Project = "~/src/app"
	analysis.SessionCount = len(entries)
	return analysis, results
}

func TestGoldenSummaryDetails(t *testing.T) {
	entries := summaryDetailsFixture(t)
	analysis, results := summaryDetailsAnalysis(t, entries)
	checkGolden(t, "summary_details", FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, false, 0, SortByTime))
}

func TestGoldenSummaryDetailsExpandAgents(t *testing.T) {
	entries := summaryDetailsFixture(t)
	analysis, results := summaryDetailsAnalysis(t, entries)
	checkGolden(t, "summary_details_expand", FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, true, 0, SortByTime))
}

// Under --sort-by cost the rows go costliest first, numbered in that order,
// and the session that couldn't be read goes last. The heading names the
// order. The chart above keeps time order.
func TestGoldenSummaryDetailsByCost(t *testing.T) {
	entries := summaryDetailsFixture(t)
	analysis, results := summaryDetailsAnalysis(t, entries)
	checkGolden(t, "summary_details_cost", FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, false, 0, SortByCost))
	checkGolden(t, "summary_details_cost_expand", FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, true, 0, SortByCost))
}

func TestGoldenSummaryDetailsColor(t *testing.T) {
	entries := summaryDetailsFixture(t)
	analysis, results := summaryDetailsAnalysis(t, entries)
	checkGolden(t, "summary_details_color", FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", false, false, 0, SortByTime))
}

// A session with no replies yet reads as list shows it, dashes and no cost,
// and its 0 doesn't color the other sessions' costs as if they stood out.
func TestGoldenSummaryDetailsNoReplies(t *testing.T) {
	entries := summaryDetailsFixture(t)
	path := filepath.Join(t.TempDir(), goldenEmptyID+".jsonl")
	user := `{"type":"user","timestamp":"2026-01-15T08:00:00Z","sessionId":"` + goldenEmptyID + `","message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := models.SessionEntry{
		SessionID: goldenEmptyID,
		FullPath:  path,
		Created:   time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC),
		Modified:  time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC),
	}
	analysis, results := summaryDetailsAnalysis(t, append(entries, empty))
	checkGolden(t, "summary_details_no_replies", FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, false, 0, SortByTime))
	// Beside one priced session, a 0 would make that session the costliest
	// of a range and color it red.
	analysis, results = summaryDetailsAnalysis(t, []models.SessionEntry{entries[1], empty})
	checkGolden(t, "summary_details_no_replies_color", FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", false, false, 0, SortByTime))
}

// --- golden tests: workflow agent grouping ---

// goldenShowWorkflowAnalysis extends goldenShowAnalysis with a workflow run:
// two workflow agents (different models) after the regular agents, plus run
// metadata, exercising the dim group-header line in the agent section.
func goldenShowWorkflowAnalysis() *models.SessionAnalysis {
	a := goldenShowAnalysis()
	a.Agents = append(a.Agents,
		models.AgentAnalysis{
			AgentID:    "w1a2b3c4d5e6",
			WorkflowID: "wf_2e7850b6-b19",
			// As long as a description gets, so the row cuts it
			AgentType:    "general-purpose",
			Description:  "Check every query in the billing module for N+1 calls",
			MessageCount: 21,
			TotalCost:    models.CostBreakdown{TotalCost: 1.10},
			CostByModel: map[string]models.CostBreakdown{
				"claude-opus-4-8": {TotalCost: 1.10},
			},
			StartTime: goldenTime(11, 5, 0),
			EndTime:   goldenTime(11, 15, 0),
			Duration:  models.Duration(10 * time.Minute),
			// In the warning band short of its 967K mark, so the color goldens show it orange
			Context: &models.ContextUsage{Tokens: 900000, Window: 1000000, Percent: 90, CompactAt: 967000},
		},
		models.AgentAnalysis{
			AgentID:      "w6e5d4c3b2a1",
			WorkflowID:   "wf_2e7850b6-b19",
			MessageCount: 1,
			TotalCost:    models.CostBreakdown{TotalCost: 0.55},
			CostByModel: map[string]models.CostBreakdown{
				"claude-sonnet-5": {TotalCost: 0.55},
			},
			StartTime: goldenTime(11, 10, 0),
			EndTime:   goldenTime(11, 20, 0),
			Duration:  models.Duration(10 * time.Minute),
		},
	)
	a.AgentCount = 4
	a.AgentMessageCount += 22
	a.MessageCount += 22
	a.AgentsCost.TotalCost += 1.65
	a.TotalCost.TotalCost += 1.65
	opus := a.CostByModel["claude-opus-4-8"]
	opus.TotalCost += 1.10
	a.CostByModel["claude-opus-4-8"] = opus
	sonnet := a.CostByModel["claude-sonnet-5"]
	sonnet.TotalCost += 0.55
	a.CostByModel["claude-sonnet-5"] = sonnet
	a.Workflows = []models.WorkflowMeta{
		{RunID: "wf_2e7850b6-b19", Name: "audit-codebase", Status: "completed"},
	}
	a.WorkflowCount = 1
	return a
}

// goldenShowLongWorkflowAnalysis sets a run whose name WorkflowLabel cuts
// beside a short-named run still going, so the widest heading is as wide as
// a heading gets.
func goldenShowLongWorkflowAnalysis() *models.SessionAnalysis {
	a := goldenShowWorkflowAnalysis()
	a.Workflows[0].Status = "running"
	a.Workflows = append(a.Workflows, models.WorkflowMeta{
		RunID: "wf_9c1d0e2f-a37", Name: "review-changes-across-the-billing-and-auth-modules", Status: "completed",
	})
	a.Agents = append(a.Agents, models.AgentAnalysis{
		AgentID:      "r1e2v3i4e5w6",
		WorkflowID:   "wf_9c1d0e2f-a37",
		MessageCount: 9,
		TotalCost:    models.CostBreakdown{TotalCost: 0.75},
		CostByModel:  map[string]models.CostBreakdown{"claude-sonnet-5": {TotalCost: 0.75}},
		StartTime:    goldenTime(11, 30, 0),
		EndTime:      goldenTime(11, 40, 0),
		Duration:     models.Duration(10 * time.Minute),
	})
	a.AgentCount++
	a.AgentMessageCount += 9
	a.MessageCount += 9
	a.AgentsCost.TotalCost += 0.75
	a.TotalCost.TotalCost += 0.75
	sonnet := a.CostByModel["claude-sonnet-5"]
	sonnet.TotalCost += 0.75
	a.CostByModel["claude-sonnet-5"] = sonnet
	a.WorkflowCount = 2
	return a
}

// Piped, the label column grows to the widest heading, so the short run
// keeps its "(running)".
func TestGoldenSessionTableShowLongAndRunningWorkflows(t *testing.T) {
	checkGolden(t, "session_table_show_long_running_workflows", FormatSessionTable(goldenShowLongWorkflowAnalysis(), true, 0))
}

// At 60 columns the column grows only as far as the cost still fits, and
// the headings lose their statuses together.
func TestGoldenSessionTableShowLongAndRunningWorkflowsNarrow(t *testing.T) {
	checkGolden(t, "session_table_show_long_running_workflows_w60", FormatSessionTable(goldenShowLongWorkflowAnalysis(), true, 60))
}

func TestGoldenSessionTableShowWorkflows(t *testing.T) {
	checkGolden(t, "session_table_show_workflows", FormatSessionTable(goldenShowWorkflowAnalysis(), true, 0))
}

func TestGoldenSessionTableShowWorkflowsColor(t *testing.T) {
	checkGolden(t, "session_table_show_workflows_color", FormatSessionTable(goldenShowWorkflowAnalysis(), false, 0))
}

const goldenWfSessionID = "dddd4444-5555-6666-7777-888899990000"

// summaryWorkflowFixture builds a project with one session that has a regular
// agent plus two workflow runs (two agents, then one, with metadata), for the
// expand-agents tree.
func summaryWorkflowFixture(t *testing.T) []models.SessionEntry {
	t.Helper()
	dir := t.TempDir()

	writeFixture := func(rel, content string) string {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		return path
	}

	msg := func(ts, id, model string, in, out int64) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"req_%s","message":{"id":%q,"model":%q,"usage":{"input_tokens":%d,"output_tokens":%d}}}`,
			ts, id, id, model, in, out)
	}

	sessionPath := writeFixture(goldenWfSessionID+".jsonl", strings.Join([]string{
		msg("2026-01-20T09:00:00Z", "p1", "claude-opus-4-8", 1000, 500),
	}, "\n")+"\n")

	writeFixture(filepath.Join(goldenWfSessionID, "subagents", "agent-reg1.jsonl"),
		msg("2026-01-20T09:05:00Z", "r1", "claude-haiku-4-5", 2000, 1000)+"\n")
	writeFixture(filepath.Join(goldenWfSessionID, "subagents", "agent-reg1.meta.json"),
		`{"agentType":"Explore","description":"Find the auth middleware"}`)

	wfRun := filepath.Join(goldenWfSessionID, "subagents", "workflows", "wf_golden-run")
	writeFixture(filepath.Join(wfRun, "agent-wf1.jsonl"),
		msg("2026-01-20T09:10:00Z", "w1", "claude-opus-4-8", 3000, 1500)+"\n")
	writeFixture(filepath.Join(wfRun, "agent-wf2.jsonl"),
		msg("2026-01-20T09:12:00Z", "w2", "claude-sonnet-5", 4000, 2000)+"\n")
	writeFixture(filepath.Join(wfRun, "journal.jsonl"), `{"type":"started","key":"v2:x","agentId":"wf1"}`+"\n")
	// wf1's task is as long as one gets, wf2 has only a type, and wf3 below
	// has no meta file.
	writeFixture(filepath.Join(wfRun, "agent-wf1.meta.json"),
		`{"agentType":"general-purpose","description":"Check every query in the billing module for N+1 calls"}`)
	writeFixture(filepath.Join(wfRun, "agent-wf2.meta.json"), `{"agentType":"general-purpose"}`)

	writeFixture(filepath.Join(goldenWfSessionID, "workflows", "wf_golden-run.json"),
		`{"runId":"wf_golden-run","workflowName":"audit-codebase","status":"completed","script":"export const meta = {}"}`)

	// A second run, so the first run's node has a sibling below it and its
	// agents hang off a rail.
	wfRun2 := filepath.Join(goldenWfSessionID, "subagents", "workflows", "wf_golden-run2")
	writeFixture(filepath.Join(wfRun2, "agent-wf3.jsonl"),
		msg("2026-01-20T09:20:00Z", "w3", "claude-haiku-4-5", 1500, 700)+"\n")
	writeFixture(filepath.Join(goldenWfSessionID, "workflows", "wf_golden-run2.json"),
		`{"runId":"wf_golden-run2","workflowName":"second-pass","status":"running"}`)

	return []models.SessionEntry{
		{
			SessionID: goldenWfSessionID,
			FullPath:  sessionPath,
			Created:   time.Date(2026, 1, 20, 9, 0, 0, 0, time.UTC),
			Modified:  time.Date(2026, 1, 20, 9, 30, 0, 0, time.UTC),
		},
	}
}

func TestGoldenSummaryDetailsExpandWorkflows(t *testing.T) {
	entries := summaryWorkflowFixture(t)
	analysis, results := summaryDetailsAnalysis(t, entries)
	checkGolden(t, "summary_details_expand_workflows", FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, true, 0, SortByTime))
}

func TestGoldenSummaryDetailsExpandWorkflowsASCII(t *testing.T) {
	useASCII(t)
	entries := summaryWorkflowFixture(t)
	analysis, results := summaryDetailsAnalysis(t, entries)
	checkGolden(t, "summary_details_expand_workflows_ascii", FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, true, 0, SortByTime))
}

func TestGoldenSummaryDetailsExpandWorkflowsColor(t *testing.T) {
	entries := summaryWorkflowFixture(t)
	analysis, results := summaryDetailsAnalysis(t, entries)
	checkGolden(t, "summary_details_expand_workflows_color", FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", false, true, 0, SortByTime))
}

// --- golden tests: glyph sets and mixed cost widths ---

// useASCII switches to the ASCII glyph set (--ascii) for one test.
func useASCII(t *testing.T) {
	t.Helper()
	styles.SetASCII(true)
	t.Cleanup(func() { styles.SetASCII(false) })
}

// --ascii --no-color: plain ASCII frames and symbols, no escapes.
func TestGoldenSessionTableShowASCII(t *testing.T) {
	useASCII(t)
	checkGolden(t, "session_table_show_ascii", FormatSessionTable(goldenShowAnalysis(), true, 0))
}

// --ascii alone keeps color; the colored branches must draw ASCII glyphs too.
func TestGoldenSessionTableShowASCIIColor(t *testing.T) {
	useASCII(t)
	checkGolden(t, "session_table_show_ascii_color", FormatSessionTable(goldenShowAnalysis(), false, 0))
}

func TestGoldenSummaryDetailsExpandASCII(t *testing.T) {
	useASCII(t)
	analysis, results := summaryDetailsAnalysis(t, summaryDetailsFixture(t))
	checkGolden(t, "summary_details_expand_ascii", FormatSummaryTableWithDetails(analysis, results, "/home/user/src/app", true, true, 0, SortByTime))
}

// goldenMixedWidthAnalysis gives every cost column values with different
// digit counts ($17.91, $5.22, $0.4231), so a no-color branch that skips the
// decimal padding shows up as ragged costs.
func goldenMixedWidthAnalysis() *models.SessionAnalysis {
	a := goldenShowAnalysis()
	a.CostByModel = map[string]models.CostBreakdown{
		"claude-opus-4-8":  {TotalCost: 17.908392},
		"claude-sonnet-5":  {TotalCost: 5.223775},
		"claude-haiku-4-5": {TotalCost: 0.423100},
	}
	a.ParentCost = models.CostBreakdown{TotalCost: 17.908392}
	a.Agents[0].TotalCost = models.CostBreakdown{TotalCost: 0.423100}
	a.Agents[1].TotalCost = models.CostBreakdown{TotalCost: 12.345678}
	a.AgentsCost = models.CostBreakdown{TotalCost: 12.768778}
	return a
}

func TestGoldenSessionTableShowMixedWidths(t *testing.T) {
	checkGolden(t, "session_table_show_mixed_widths", FormatSessionTable(goldenMixedWidthAnalysis(), true, 0))
}

// A workflow heading wider than the default label column widens it, and
// the cost column moves right with it.
func TestGoldenSessionTableShowLongWorkflow(t *testing.T) {
	a := goldenShowWorkflowAnalysis()
	a.Workflows[0].Name = "dependency-upgrade-audit"
	checkGolden(t, "session_table_show_long_workflow", FormatSessionTable(a, true, 0))
}
