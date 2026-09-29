package cmd

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// machineOutputs is every command shape that has json and csv output.
var machineOutputs = [][]string{
	{"show", projFlag, e2eBetaID},
	{"show", projFlag, e2eBetaID, "--messages"},
	{"list", projFlag},
	{"summary", projFlag},
	{"summary", projFlag, "-d"},
	{"summary", projFlag, "-d", "--expand-agents"},
	{"global"},
}

// TestE2EMachineOutputEndsInOneNewline: a second newline after the last csv
// record reads as an empty record to csv.reader, and as a blank last line to
// tail and wc.
func TestE2EMachineOutputEndsInOneNewline(t *testing.T) {
	setupE2EFixture(t)
	for _, args := range machineOutputs {
		for _, format := range []string{"json", "csv"} {
			out, _, err := executeCLISplit(t, append(args, "-f", format)...)
			if err != nil {
				t.Fatalf("%v -f %s: %v", args, format, err)
			}
			if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
				t.Errorf("%v -f %s: want exactly one trailing newline, got ending %q", args, format, out[max(0, len(out)-20):])
			}
		}
	}
}

// TestE2ECostTrendIsAName: cost_trend is written by name, and left out, with
// the other trend fields, on a session too short to have a trend.
func TestE2ECostTrendIsAName(t *testing.T) {
	setupKeysFixture(t)
	insights := func(id string) map[string]any {
		t.Helper()
		out, _, err := executeCLISplit(t, "show", keysProj, id, "-f", "json")
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Insights map[string]any `json:"insights"`
		}
		mustJSON(t, out, &v)
		return v.Insights
	}

	long := insights(keysFullID)
	if trend := long["cost_trend"]; !slices.Contains([]any{"increasing", "decreasing", "stable"}, trend) {
		t.Errorf("cost_trend = %#v, want increasing, decreasing or stable", trend)
	}
	short := insights(keysNoAgentsDirID)
	for _, k := range []string{"cost_trend", "recent_avg_cost", "trend_window"} {
		if v, ok := short[k]; ok {
			t.Errorf("one-message session: %s = %v, want it absent", k, v)
		}
	}
}

// TestE2EExpandAgentsCSVReconciles: summary -d --expand-agents csv has
// session rows, whose total_cost includes their agents, and agent rows.
// Summing session rows gives the total, summing agent rows gives agent
// spend, and each session row splits into parent_cost + agents_cost.
func TestE2EExpandAgentsCSVReconciles(t *testing.T) {
	setupE2EFixture(t)
	out, _, err := executeCLISplit(t, "summary", projFlag, "-d", "--expand-agents", "-f", "csv")
	if err != nil {
		t.Fatal(err)
	}
	records := mustCSV(t, out)
	col := func(name string) int {
		i := slices.Index(records[0], name)
		if i < 0 {
			t.Fatalf("no %s column in %v", name, records[0])
		}
		return i
	}
	rowType, sessionID := col("row_type"), col("session_id")
	total, parent, agents := col("total_cost"), col("parent_cost"), col("agents_cost")

	// Each cell is rounded to 6 dp, so a sum of two can be off by one in the
	// last place.
	const tol = 2e-6
	var sessionSum, agentRowSum, agentsCostSum float64
	var agentRows int
	for _, r := range records[1:] {
		switch r[rowType] {
		case "session":
			p, a, tc := mustFloat(t, r[parent]), mustFloat(t, r[agents]), mustFloat(t, r[total])
			if math.Abs(p+a-tc) > tol {
				t.Errorf("session %s: parent_cost %v + agents_cost %v != total_cost %v", r[sessionID], p, a, tc)
			}
			sessionSum += tc
			agentsCostSum += a
		case "agent":
			agentRows++
			if r[parent] != "" || r[agents] != "" {
				t.Errorf("agent row %v: parent_cost and agents_cost should be empty", r)
			}
			agentRowSum += mustFloat(t, r[total])
		default:
			t.Errorf("unexpected row_type %q", r[rowType])
		}
	}
	if agentRows == 0 {
		t.Fatal("the fixture should produce agent rows")
	}
	if math.Abs(agentRowSum-agentsCostSum) > float64(agentRows)*tol {
		t.Errorf("agent rows sum to %v, session agents_cost to %v", agentRowSum, agentsCostSum)
	}

	jsonOut, _, err := executeCLISplit(t, "summary", projFlag, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		TotalCost struct {
			TotalCost float64 `json:"total_cost"`
		} `json:"total_cost"`
	}
	mustJSON(t, jsonOut, &summary)
	if math.Abs(sessionSum-summary.TotalCost.TotalCost) > float64(len(records))*tol {
		t.Errorf("session rows sum to %v, summary -f json total_cost.total_cost is %v", sessionSum, summary.TotalCost.TotalCost)
	}
}

// TestE2EUnpricedModelsAgree: a model ficha can't price is named in json's
// unpriced_models, csv's unpriced_models column and the stderr warning, the
// same way on every surface, and spelled as its cost_by_model key.
func TestE2EUnpricedModelsAgree(t *testing.T) {
	root := setupE2EFixture(t)
	const zetaID = "dddddddd-1111-2222-3333-444444444444"
	// Only the session's agent runs on the unknown model.
	sessionPath := filepath.Join(root, "projects", e2eProjDir, zetaID+".jsonl")
	agentPath := filepath.Join(root, "projects", e2eProjDir, zetaID, "subagents", "agent-z.jsonl")
	if err := os.MkdirAll(filepath.Dir(agentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, line := range map[string]string{
		sessionPath: e2eMsg("2026-02-04T10:00:00Z", "z1", "claude-opus-4-8", 1000, 500, 0, 0, 0),
		agentPath:   e2eMsg("2026-02-04T10:01:00Z", "z2", "claude-zeta-9", 1000, 500, 0, 0, 0),
	} {
		if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"claude-zeta-9"}
	const warning = `Warning: unknown model "claude-zeta-9" priced at fallback`

	type priced struct {
		CostByModel    map[string]any `json:"cost_by_model"`
		UnpricedModels []string       `json:"unpriced_models"`
	}
	check := func(what string, p priced, want []string) {
		t.Helper()
		if !slices.Equal(p.UnpricedModels, want) {
			t.Errorf("%s: unpriced_models = %q, want %q", what, p.UnpricedModels, want)
		}
		for _, id := range p.UnpricedModels {
			if _, ok := p.CostByModel[id]; !ok {
				t.Errorf("%s: %q isn't a cost_by_model key", what, id)
			}
		}
	}
	run := func(args ...string) (string, string) {
		t.Helper()
		out, stderr, err := executeCLISplit(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out, stderr
	}

	out, stderr := run("show", projFlag, zetaID, "-f", "json")
	var show struct {
		priced
		Agents []priced `json:"agents"`
	}
	mustJSON(t, out, &show)
	check("show", show.priced, want)
	if len(show.Agents) != 1 {
		t.Fatalf("want the one agent, got %d", len(show.Agents))
	}
	check("show agent", show.Agents[0], want)
	mustContainAll(t, stderr, warning)

	out, _ = run("show", projFlag, e2eAlphaID, "-f", "json")
	if strings.Contains(out, "unpriced_models") {
		t.Errorf("a session with only known models should leave unpriced_models out:\n%s", out)
	}

	out, stderr = run("summary", projFlag, "-d", "-f", "json")
	var detail struct {
		Summary  priced `json:"summary"`
		Sessions []struct {
			SessionID string `json:"session_id"`
			priced
		} `json:"sessions"`
	}
	mustJSON(t, out, &detail)
	check("summary", detail.Summary, want)
	for _, s := range detail.Sessions {
		if s.SessionID == zetaID {
			check("summary -d session", s.priced, want)
		} else {
			check("summary -d session", s.priced, nil)
		}
	}
	mustContainAll(t, stderr, warning)

	out, stderr = run("global", "-f", "json")
	var global struct {
		priced
		Projects []struct {
			EncodedPath string `json:"encoded_path"`
			priced
		} `json:"projects"`
	}
	mustJSON(t, out, &global)
	check("global", global.priced, want)
	for _, p := range global.Projects {
		if p.EncodedPath == e2eProjDir {
			check("global project", p.priced, want)
		} else {
			check("global project", p.priced, nil)
		}
	}
	mustContainAll(t, stderr, warning)

	// Each cell is a json array of IDs. Rows that name nothing hold [].
	csvColumn := func(args ...string) [][]string {
		t.Helper()
		out, _ := run(append(args, "-f", "csv")...)
		records := mustCSV(t, out)
		i := slices.Index(records[0], "unpriced_models")
		if i < 0 {
			t.Fatalf("%v: no unpriced_models column in %v", args, records[0])
		}
		var cells [][]string
		for _, r := range records[1:] {
			var ids []string
			mustJSON(t, r[i], &ids)
			cells = append(cells, ids)
		}
		return cells
	}
	// One row per session or project; only the one with the model names it.
	named := func(cells [][]string) []string {
		var ids []string
		for _, c := range cells {
			ids = append(ids, c...)
		}
		return ids
	}
	for _, args := range [][]string{
		{"show", projFlag, zetaID},
		{"summary", projFlag},
		{"summary", projFlag, "-d"},
		{"global"},
	} {
		if got := named(csvColumn(args...)); !slices.Equal(got, want) {
			t.Errorf("%v csv unpriced_models = %q, want %q", args, got, want)
		}
	}
	// With agent rows, the agent and its session both name it.
	if got := named(csvColumn("summary", projFlag, "-d", "--expand-agents")); !slices.Equal(got, []string{"claude-zeta-9", "claude-zeta-9"}) {
		t.Errorf("summary -d --expand-agents csv unpriced_models = %q, want the session row and the agent row", got)
	}
}

// TestE2EListCarriesTheTableFields: list json prices each session the way
// summary -d does, cross-session duplicates counted once, and names its
// project the way show does.
func TestE2EListCarriesTheTableFields(t *testing.T) {
	root := setupE2EFixture(t)
	// A resumed session repeats alpha's first message; summary -d counts it
	// under alpha only, and list must agree.
	const resumedID = "eeeeeeee-1111-2222-3333-444444444444"
	resumed := `{"type":"ai-title","aiTitle":"Resumed work"}` + "\n" +
		e2eMsg("2026-02-01T10:00:00Z", "a1", "claude-opus-4-8", 1200, 800, 3000, 1000, 20000) + "\n" +
		e2eMsg("2026-02-05T10:00:00Z", "r1", "claude-opus-4-8", 100, 100, 0, 0, 0) + "\n"
	if err := os.WriteFile(filepath.Join(root, "projects", e2eProjDir, resumedID+".jsonl"), []byte(resumed), 0o644); err != nil {
		t.Fatal(err)
	}

	type cost struct {
		TotalCost float64 `json:"total_cost"`
	}
	type record struct {
		SessionID       string  `json:"session_id"`
		ProjectPath     string  `json:"project_path"`
		OriginalPath    *string `json:"original_path"`
		Title           string  `json:"title"`
		Model           string  `json:"model"`
		StartTime       string  `json:"start_time"`
		DurationSeconds float64 `json:"duration_seconds"`
		TotalCost       *cost   `json:"total_cost"`
	}
	run := func(args ...string) string {
		t.Helper()
		out, stderr, err := executeCLISplit(t, args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr)
		}
		return out
	}

	var list []record
	mustJSON(t, run("list", projFlag, "-f", "json"), &list)
	var detail struct {
		Sessions []record `json:"sessions"`
	}
	mustJSON(t, run("summary", projFlag, "-d", "-f", "json"), &detail)
	summaryCost := map[string]float64{}
	summaryRecord := map[string]record{}
	for _, s := range detail.Sessions {
		summaryCost[s.SessionID] = s.TotalCost.TotalCost
		summaryRecord[s.SessionID] = s
	}
	if len(list) != 3 || len(summaryCost) != 3 {
		t.Fatalf("want 3 sessions in list and summary -d, got %d and %d", len(list), len(summaryCost))
	}
	var show record
	mustJSON(t, run("show", projFlag, e2eBetaID, "-f", "json"), &show)
	for _, r := range list {
		if r.TotalCost == nil {
			t.Fatalf("%s: no total_cost", r.SessionID)
		}
		if got, want := r.TotalCost.TotalCost, summaryCost[r.SessionID]; math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: list total_cost %v, summary -d %v", r.SessionID, got, want)
		}
		if r.ProjectPath != show.ProjectPath {
			t.Errorf("%s: project_path %q, show has %q", r.SessionID, r.ProjectPath, show.ProjectPath)
		}
		if r.OriginalPath == nil {
			t.Errorf("%s: original_path missing", r.SessionID)
		}
		if r.Model == "" || r.StartTime == "" {
			t.Errorf("%s: model %q, start_time %q", r.SessionID, r.Model, r.StartTime)
		}
		// The analysis fields are the summary -d record's. For the resumed
		// session that means the part it added, not the history it repeats.
		if sd := summaryRecord[r.SessionID]; r.StartTime != sd.StartTime || r.DurationSeconds != sd.DurationSeconds {
			t.Errorf("%s: start_time %s, duration %v; summary -d has %s, %v",
				r.SessionID, r.StartTime, r.DurationSeconds, sd.StartTime, sd.DurationSeconds)
		}
		if r.SessionID == e2eBetaID && (r.StartTime != show.StartTime || r.DurationSeconds != show.DurationSeconds) {
			t.Errorf("beta: start_time %s / duration %v, show has %s / %v", r.StartTime, r.DurationSeconds, show.StartTime, show.DurationSeconds)
		}
	}
	// The dedup is what the comparison above tests, so make sure it happened.
	if resumedAlone := summaryCost[resumedID]; resumedAlone <= 0 {
		t.Fatalf("resumed session cost %v", resumedAlone)
	}
	var resumedShow struct {
		TotalCost cost   `json:"total_cost"`
		Title     string `json:"title"`
	}
	mustJSON(t, run("show", projFlag, resumedID, "-f", "json"), &resumedShow)
	// The title isn't deduplicated: it's the transcript's latest, as on show.
	for _, r := range list {
		if r.SessionID == resumedID && (r.Title != resumedShow.Title || r.Title != "Resumed work") {
			t.Errorf("resumed session title %q, show has %q", r.Title, resumedShow.Title)
		}
	}
	if resumedShow.TotalCost.TotalCost <= summaryCost[resumedID] {
		t.Error("the fixture should give the resumed session a duplicate that show counts and summary -d doesn't")
	}

	// A --project-dir typed with a trailing separator still joins with show.
	dirFlag := "--project-dir=" + filepath.Join(root, "projects", e2eProjDir) + string(filepath.Separator)
	var slashed []record
	mustJSON(t, run("list", dirFlag, "-f", "json"), &slashed)
	var slashedShow record
	mustJSON(t, run("show", dirFlag, e2eBetaID, "-f", "json"), &slashedShow)
	if len(slashed) == 0 {
		t.Fatal("list with a trailing separator found no sessions")
	}
	if slashed[0].ProjectPath != slashedShow.ProjectPath {
		t.Errorf("with a trailing separator, list project_path %q, show %q", slashed[0].ProjectPath, slashedShow.ProjectPath)
	}

	// csv carries the same cost.
	records := mustCSV(t, run("list", projFlag, "-f", "csv"))
	id, total := slices.Index(records[0], "session_id"), slices.Index(records[0], "total_cost")
	for _, r := range records[1:] {
		if got := mustFloat(t, r[total]); math.Abs(got-summaryCost[r[id]]) > 1e-6 {
			t.Errorf("%s: list csv total_cost %v, summary -d %v", r[id], got, summaryCost[r[id]])
		}
	}
}

// TestE2EListKeepsUnreadableSessions: the table lists a session that failed
// to parse as unreadable, and json and csv keep it too, with its skip
// counter and without the fields only a parse can give.
func TestE2EListKeepsUnreadableSessions(t *testing.T) {
	setupKeysFixture(t)
	out, _, err := executeCLISplit(t, "list", keysProj, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	mustJSON(t, out, &list)
	i := slices.IndexFunc(list, func(r map[string]any) bool { return r["session_id"] == keysBrokenID })
	if i < 0 {
		t.Fatalf("unreadable session missing from list json:\n%s", out)
	}
	broken := list[i]
	if broken["skipped_sessions"] != 1.0 {
		t.Errorf("skipped_sessions = %v, want 1", broken["skipped_sessions"])
	}
	for _, k := range []string{"total_cost", "model", "start_time", "duration_seconds"} {
		if v, ok := broken[k]; ok {
			t.Errorf("unreadable session: %s = %v, want it absent", k, v)
		}
	}

	out, _, err = executeCLISplit(t, "list", keysProj, "-f", "csv")
	if err != nil {
		t.Fatal(err)
	}
	records := mustCSV(t, out)
	col := func(name string) int { return slices.Index(records[0], name) }
	row := slices.IndexFunc(records, func(r []string) bool { return r[0] == keysBrokenID })
	if row < 0 {
		t.Fatalf("unreadable session missing from list csv:\n%s", out)
	}
	if got := records[row][col("skipped_sessions")]; got != "1" {
		t.Errorf("csv skipped_sessions = %q, want 1", got)
	}
	if got := records[row][col("total_cost")]; got != "" {
		t.Errorf("csv total_cost = %q, want empty", got)
	}
}

// TestE2EContextMatchesTheGauge: show json's context is the table's gauge
// reading. The summary aggregate spans sessions, so it has none; each
// summary -d session has its own.
func TestE2EContextMatchesTheGauge(t *testing.T) {
	setupE2EFixture(t)
	out, _, err := executeCLISplit(t, "show", projFlag, e2eBetaID, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	var show struct {
		Context *models.ContextUsage `json:"context"`
		Last    models.TokenUsage    `json:"last_message_usage"`
	}
	mustJSON(t, out, &show)
	if show.Context == nil {
		t.Fatalf("show json has no context:\n%s", out)
	}
	c := show.Context
	if c.Tokens != show.Last.ContextWindowSize() || c.Window <= 0 {
		t.Errorf("context %+v, last message's context size %d", c, show.Last.ContextWindowSize())
	}
	if want := float64(c.Tokens) / float64(c.Window) * 100; math.Abs(c.Percent-want) > 1e-9 {
		t.Errorf("percent %v, want %v", c.Percent, want)
	}
	table, _, err := executeCLISplit(t, "show", projFlag, e2eBetaID, "--ascii")
	if err != nil {
		t.Fatal(err)
	}
	if gauge := fmt.Sprintf("Context %3.0f%%", c.Percent); !strings.Contains(table, gauge) {
		t.Errorf("table should show %q:\n%s", gauge, table)
	}

	out, _, err = executeCLISplit(t, "summary", projFlag, "-d", "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Summary  map[string]any `json:"summary"`
		Sessions []struct {
			Context *models.ContextUsage `json:"context"`
		} `json:"sessions"`
	}
	mustJSON(t, out, &detail)
	if _, ok := detail.Summary["context"]; ok {
		t.Error("the summary aggregate should have no context")
	}
	for i, s := range detail.Sessions {
		if s.Context == nil {
			t.Errorf("summary -d session %d has no context", i)
		}
	}
}

// TestE2ETimestampsAreUTCSeconds: every timestamp in json and csv is UTC at
// whole seconds with a Z, the form jq's fromdate reads, even when local time
// isn't UTC and a relative --since bound carries nanoseconds.
func TestE2ETimestampsAreUTCSeconds(t *testing.T) {
	setupE2EFixture(t)
	orig := time.Local
	time.Local = time.FixedZone("EDT", -4*3600)
	t.Cleanup(func() { time.Local = orig })

	utcSeconds := regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$`)
	timeKeys := map[string]bool{
		"start_time": true, "end_time": true, "timestamp": true, "modified": true,
		"first_active": true, "last_active": true, "since": true, "until": true,
	}
	check := func(where, key, v string) {
		t.Helper()
		if timeKeys[key] && v != "" && !utcSeconds.MatchString(v) {
			t.Errorf("%s: %s = %q, want UTC seconds with Z", where, key, v)
		}
	}
	var walk func(where string, v any)
	walk = func(where string, v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, child := range v {
				if s, ok := child.(string); ok {
					check(where, k, s)
				}
				walk(where, child)
			}
		case []any:
			for _, child := range v {
				walk(where, child)
			}
		}
	}

	runs := slices.Clone(machineOutputs)
	runs = append(runs,
		[]string{"summary", projFlag, "--since", "2h"},
		[]string{"summary", projFlag, "--since", "2026-02-01", "--until", "2026-02-03"},
		[]string{"global", "--since", "2h"},
	)
	var seen int
	for _, args := range runs {
		where := strings.Join(args, " ")
		out, _, err := executeCLISplit(t, append(args, "-f", "json")...)
		if err != nil {
			t.Fatalf("%s -f json: %v", where, err)
		}
		var v any
		mustJSON(t, out, &v)
		walk(where+" -f json", v)
		seen += strings.Count(out, `Z"`)

		out, _, err = executeCLISplit(t, append(args, "-f", "csv")...)
		if err != nil {
			t.Fatalf("%s -f csv: %v", where, err)
		}
		records := mustCSV(t, out)
		for _, r := range records[1:] {
			for i, col := range records[0] {
				check(where+" -f csv", col, r[i])
			}
		}
	}
	if seen == 0 {
		t.Fatal("no timestamps checked")
	}
}

// TestE2EGlobalCSVJoinsSummary: global csv's full_path is summary's
// project_path, so the two exports join without rewriting either side.
func TestE2EGlobalCSVJoinsSummary(t *testing.T) {
	setupE2EFixture(t)
	out, _, err := executeCLISplit(t, "summary", projFlag, "-f", "json")
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		ProjectPath string `json:"project_path"`
	}
	mustJSON(t, out, &summary)

	out, _, err = executeCLISplit(t, "global", "-f", "csv")
	if err != nil {
		t.Fatal(err)
	}
	records := mustCSV(t, out)
	fullPath, encoded := slices.Index(records[0], "full_path"), slices.Index(records[0], "encoded_path")
	if fullPath < 0 || encoded < 0 {
		t.Fatalf("global csv header %v lacks full_path or encoded_path", records[0])
	}
	var found bool
	for _, r := range records[1:] {
		if r[fullPath] == summary.ProjectPath {
			found = true
			// csvCell guards the leading dash of a Unix encoded name. The
			// fixture's names are Unix-style on every OS.
			if r[encoded] != "'"+e2eProjDir {
				t.Errorf("encoded_path = %q, want %q", r[encoded], "'"+e2eProjDir)
			}
		}
	}
	if !found {
		t.Errorf("no global csv row has full_path %q", summary.ProjectPath)
	}
}
