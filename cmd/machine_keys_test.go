package cmd

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/bardisty/ficha/internal/models"
)

var update = flag.Bool("update", false, "rewrite .golden files with current output")

const (
	keysProjDir = "-home-test-keys"
	// keysFullID has everything a session can carry: a title, insights with a
	// highest-cost message, a skipped line, an estimated cost, a regular agent
	// and a workflow agent.
	keysFullID = "11111111-aaaa-bbbb-cccc-000000000001"
	// keysNoAgentsDirID has a plain file where its subagents directory
	// belongs, so its agents count as skipped.
	keysNoAgentsDirID = "11111111-aaaa-bbbb-cccc-000000000002"
	// keysBrokenID is a transcript that can't be opened, so it counts as a
	// skipped session.
	keysBrokenID = "11111111-aaaa-bbbb-cccc-000000000003"
)

// estimatedMsg is an assistant line whose cache-write tokens carry no TTL
// breakdown, so its write cost is estimated.
func estimatedMsg(ts, id, model string, in, out, cacheWrite int64) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"req_%s","message":{"id":%q,"model":%q,"usage":{"input_tokens":%d,"output_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":0}}}`,
		ts, id, id, model, in, out, cacheWrite)
}

// malformedLine is a line the parser rejects and counts as skipped.
const malformedLine = `{"type":"assistant","timestamp":"not-a-time"}`

// setupKeysFixture builds a project that makes every optional json key
// appear at least once, so the key-set goldens pin it. A fixture that never
// triggers a key leaves it unpinned: removing it would pass unnoticed.
func setupKeysFixture(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	proj := filepath.Join(root, "projects", keysProjDir)
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(proj, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lines := func(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

	// Seven parent messages compute a trend, and the fourth costs far more
	// than the rest, so insights carry highest_cost.
	write(keysFullID+".jsonl", lines(
		`{"type":"ai-title","aiTitle":"Key fixture"}`,
		e2eMsg("2026-02-01T10:00:00Z", "k1", "claude-opus-4-8", 100, 100, 100, 100, 1000),
		e2eMsg("2026-02-01T10:01:00Z", "k2", "claude-opus-4-8", 100, 100, 100, 0, 1000),
		e2eMsg("2026-02-01T10:02:00Z", "k3", "claude-opus-4-8", 100, 100, 100, 0, 1000),
		e2eMsg("2026-02-01T10:03:00Z", "k4", "claude-opus-4-8", 100, 90000, 200000, 0, 1000),
		malformedLine,
		e2eMsg("2026-02-01T10:04:00Z", "k5", "claude-opus-4-8", 100, 100, 100, 0, 1000),
		estimatedMsg("2026-02-01T10:05:00Z", "k6", "claude-opus-4-8", 100, 100, 100),
		e2eMsg("2026-02-01T10:06:00Z", "k7", "claude-opus-4-8", 100, 100, 100, 0, 1000),
	))
	write(filepath.Join(keysFullID, "subagents", "agent-r1.jsonl"), lines(
		e2eMsg("2026-02-01T10:02:30Z", "r1", "claude-sonnet-5", 100, 100, 100, 0, 0),
		malformedLine,
		estimatedMsg("2026-02-01T10:02:40Z", "r2", "claude-sonnet-5", 100, 100, 100),
	))
	write(filepath.Join(keysFullID, "subagents", "workflows", "wf_keys-run", "agent-w1.jsonl"), lines(
		e2eMsg("2026-02-01T10:03:30Z", "w1", "claude-sonnet-5", 100, 100, 0, 0, 0),
	))
	write(filepath.Join(keysFullID, "workflows", "wf_keys-run.json"),
		`{"runId":"wf_keys-run","workflowName":"keys-flow","status":"completed"}`)

	write(keysNoAgentsDirID+".jsonl", lines(
		e2eMsg("2026-02-02T09:00:00Z", "n1", "claude-haiku-4-5", 100, 100, 0, 0, 0),
	))
	write(filepath.Join(keysNoAgentsDirID, "subagents"), "not a directory\n")

	// list reads project_path only from sessions-index.json.
	write("sessions-index.json", `{"entries":[{"sessionId":"`+keysFullID+`","fullPath":"`+
		jsonEscape(filepath.Join(proj, keysFullID+".jsonl"))+`","projectPath":"/home/test/keys"}]}`)

	// A dangling symlink lists like a transcript and fails to open on every OS.
	if err := os.Symlink(filepath.Join(proj, "missing.jsonl"), filepath.Join(proj, keysBrokenID+".jsonl")); err != nil {
		t.Skipf("can't create a symlink for the unreadable session: %v", err)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", root)
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return strings.Trim(string(b), `"`)
}

// keyCase is one machine output. Its golden is the union of the keys every
// run in runs produces, so a key that only some invocations emit (window
// needs --since) is still pinned.
type keyCase struct {
	name string
	runs [][]string
}

var (
	keysProj   = "--project-dir=" + keysProjDir
	keysWindow = []string{"--since", "2026-01-01", "--until", "2026-03-01"}
)

func keyCases() []keyCase {
	withWindow := func(args ...string) [][]string {
		return [][]string{args, append(slices.Clone(args), keysWindow...)}
	}
	return []keyCase{
		{"show", [][]string{{"show", keysProj, keysFullID}, {"show", keysProj, keysNoAgentsDirID}}},
		{"show-messages", [][]string{{"show", keysProj, keysFullID, "--messages"}, {"show", keysProj, keysNoAgentsDirID, "--messages"}}},
		{"list", [][]string{{"list", keysProj}}},
		{"summary", withWindow("summary", keysProj)},
		{"summary-d", withWindow("summary", keysProj, "-d")},
		{"summary-d-expand-agents", withWindow("summary", keysProj, "-d", "--expand-agents")},
		{"global", withWindow("global")},
	}
}

// TestGoldenMachineKeys pins the key set of every json output and the header
// of every csv output. A renamed, removed or retyped key shows up as a golden
// diff in review instead of a silent null in someone's jq. Values aren't
// pinned: the formatter goldens and the e2e tests cover those.
//
// Regenerate with `make update-golden` and read the diff: every line that
// changes is a change to the machine interface.
func TestGoldenMachineKeys(t *testing.T) {
	setupKeysFixture(t)

	allPaths := map[string]bool{}
	for _, c := range keyCases() {
		t.Run(c.name+"/json", func(t *testing.T) {
			types := map[string]map[string]bool{}
			for _, args := range c.runs {
				out, stderr, err := executeCLISplit(t, append(args, "-f", "json")...)
				if err != nil {
					t.Fatalf("%v: %v\n%s", args, err, stderr)
				}
				var v any
				mustJSON(t, out, &v)
				collectKeyTypes("", v, types)
			}
			var lines []string
			for path, ts := range types {
				allPaths[path] = true
				lines = append(lines, path+" "+strings.Join(sortedKeys(ts), "|"))
			}
			sort.Strings(lines)
			checkKeysGolden(t, c.name+".json", strings.Join(lines, "\n")+"\n")
		})
		t.Run(c.name+"/csv", func(t *testing.T) {
			var header []string
			for _, args := range c.runs {
				out, stderr, err := executeCLISplit(t, append(args, "-f", "csv")...)
				if err != nil {
					t.Fatalf("%v: %v\n%s", args, err, stderr)
				}
				got, err := csv.NewReader(strings.NewReader(out)).Read()
				if err != nil {
					t.Fatalf("%v: reading csv header: %v", args, err)
				}
				if header != nil && !slices.Equal(header, got) {
					t.Fatalf("%v: header differs between runs:\n%v\n%v", args, header, got)
				}
				header = got
			}
			checkKeysGolden(t, c.name+".csv", strings.Join(header, ",")+"\n")
		})
	}

	// Every json name the models declare must turn up in some golden. One
	// that doesn't is a key the fixture never triggers, and so a key nothing
	// pins; extend setupKeysFixture until it appears.
	t.Run("fixture reaches every key", func(t *testing.T) {
		seen := map[string]bool{}
		for path := range allPaths {
			seen[path[strings.LastIndex(path, ".")+1:]] = true
		}
		for _, name := range modelJSONNames() {
			if !seen[name] {
				t.Errorf("no golden has a key named %q; the fixture never produces it", name)
			}
		}
	})
}

// collectKeyTypes records the json type of every path under v. Arrays
// collapse to [] with their elements' keys merged, and the model-ID keys of
// cost_by_model maps collapse to *, since they're data rather than schema.
func collectKeyTypes(path string, v any, types map[string]map[string]bool) {
	if path != "" {
		if types[path] == nil {
			types[path] = map[string]bool{}
		}
		types[path][jsonType(v)] = true
	}
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			if strings.HasSuffix(path, "cost_by_model") {
				k = "*"
			}
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			collectKeyTypes(childPath, child, types)
		}
	case []any:
		for _, child := range v {
			collectKeyTypes(path+"[]", child, types)
		}
	}
}

func jsonType(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	default:
		return "null"
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// modelJSONNames lists every json field name reachable from the documents
// the commands encode. list's records live in the formatter and aren't
// covered here.
func modelJSONNames() []string {
	names := map[string]bool{}
	visited := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || t.PkgPath() != reflect.TypeFor[models.SessionAnalysis]().PkgPath() || visited[t] {
			return
		}
		visited[t] = true
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			if f.Anonymous && tag == "" {
				walk(f.Type)
				continue
			}
			if tag == "" {
				tag = f.Name
			}
			names[tag] = true
			walk(f.Type)
		}
	}
	walk(reflect.TypeFor[models.SummaryDetail]())
	walk(reflect.TypeFor[models.GlobalAnalysis]())
	return sortedKeys(names)
}

// checkKeysGolden compares got with testdata/keys/<name>.golden.
func checkKeysGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "keys", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s (regenerate with `make update-golden`): %v", path, err)
	}
	if got == string(want) {
		return
	}
	gotLines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	wantLines := strings.Split(strings.TrimSuffix(string(want), "\n"), "\n")
	var diff []string
	for _, l := range wantLines {
		if !slices.Contains(gotLines, l) {
			diff = append(diff, "- "+l)
		}
	}
	for _, l := range gotLines {
		if !slices.Contains(wantLines, l) {
			diff = append(diff, "+ "+l)
		}
	}
	t.Errorf("%s: the machine interface changed (- golden, + now):\n%s\nIf that's intended, regenerate with `make update-golden`, and list each change in the PR.",
		path, strings.Join(diff, "\n"))
}
