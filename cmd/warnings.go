package cmd

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
)

// printReport writes a static report and the warnings gathered while building
// it. On a terminal the warnings follow the report: they qualify its totals,
// and printed first they scroll off the top of a short terminal before the
// prompt returns. Redirected, they keep coming first, so a log that captures
// both streams reads cause before effect.
//
// Every output ends in exactly one newline, which writeReport adds. csv
// arrives with its own, because csv.Writer ends the last record too.
func printReport(cfg *config, warnings *bytes.Buffer, output string) {
	if cfg.format == "csv" {
		output = strings.TrimSuffix(output, "\n")
	}
	warnings = bytes.NewBufferString(wrapStderr(cfg.stderr, warnings.String()))
	writeReport(cfg.stdout, cfg.stderr, warnings, output, isTerminal(cfg.stderr))
}

// stderrWidth is terminalWidth, swappable because tests can't give ficha a
// terminal.
var stderrWidth = terminalWidth

// wrapStderr wraps notes and warnings between words to the width of the
// terminal stderr is on, each continuation under the text after its label.
// Redirected, each stays one line for logs and scripts. Everything bound for
// stderr that may hold a path marked with render.NoBreak comes through here,
// wrapped or not, since this is where the path's spaces are put back.
func wrapStderr(stderr io.Writer, text string) string {
	return render.WrapHanging(text, stderrWidth(stderr))
}

// writeNote writes "Note: " and the formatted text to stderr as one line,
// wrapped like the report's warnings.
func writeNote(cfg *config, format string, args ...any) {
	writeLabeled(cfg, "Note: ", format, args...)
}

// writeWarning is writeNote for a warning printed outside a report.
func writeWarning(cfg *config, format string, args ...any) {
	writeLabeled(cfg, "Warning: ", format, args...)
}

func writeLabeled(cfg *config, label, format string, args ...any) {
	fmt.Fprint(cfg.stderr, wrapStderr(cfg.stderr, label+fmt.Sprintf(format, args...)+"\n"))
}

func writeReport(stdout, stderr io.Writer, warnings *bytes.Buffer, output string, warningsLast bool) {
	if warningsLast {
		fmt.Fprintln(stdout, output)
		_, _ = warnings.WriteTo(stderr)
		return
	}
	_, _ = warnings.WriteTo(stderr)
	fmt.Fprintln(stdout, output)
}

// skipWarning describes the inputs a report could not read. counts names what
// the skipped lines would have fed ("totals" or "message counts").
type skipWarning struct {
	counts                  string
	sessions, agents, lines int
	// details names the affected sessions on reports that span several; nil
	// on single-session reports, where the session is the one on screen.
	details []namedSkip
	// sessionID and files are a single-session report's session and the
	// transcripts in it with skipped lines, listed under -v.
	sessionID string
	files     []models.FileSkips
}

// maxListedFiles caps the transcripts -v names in one warning, so a report
// over many damaged sessions doesn't bury the terminal.
const maxListedFiles = 10

// namedSkip is a SkipDetail labeled for display: "09ccdf05", or
// "webapp/09ccdf05" on reports that span projects.
type namedSkip struct {
	label string
	models.SkipDetail
}

func (s skipWarning) write(w io.Writer, verbose bool) {
	if s.sessions > 0 {
		fmt.Fprintf(w, "Warning: %d session(s) could not be parsed\n", s.sessions)
	}
	// "read" rather than "parsed": the count also covers agent directories
	// that could not be listed, whose agent files were never seen, so there
	// the number is a lower bound.
	if s.agents > 0 {
		fmt.Fprintf(w, "Warning: %d agent sub-session(s) could not be read\n", s.agents)
	}
	if s.lines > 0 {
		fmt.Fprintf(w, "Warning: %d unparseable line(s) skipped; %s may be undercounted\n", s.lines, s.counts)
	}
	if s.sessions+s.agents+s.lines == 0 {
		return
	}
	listed := 0
	if len(s.details) == 0 {
		if verbose {
			listed = writeSkippedFiles(w, "  ", s.sessionID, s.files, listed)
			writeUnlisted(w, "  ", listed, len(s.files))
		}
		return
	}
	if !verbose {
		fmt.Fprintln(w, "  Run with -v to list the affected sessions.")
		return
	}
	total := 0
	for _, d := range s.details {
		var parts []string
		if d.Unreadable {
			parts = append(parts, "unreadable")
		}
		if d.Lines > 0 {
			parts = append(parts, plural(d.Lines, "line"))
		}
		if d.Agents > 0 {
			parts = append(parts, plural(d.Agents, "agent"))
		}
		fmt.Fprintf(w, "  %s: %s\n", render.NoBreak(d.label), strings.Join(parts, ", "))
		listed = writeSkippedFiles(w, "    ", d.SessionID, d.Files, listed)
		total += len(d.Files)
	}
	writeUnlisted(w, "  ", listed, total)
}

// writeSkippedFiles writes a line per transcript naming its skipped lines and
// why, then its path, which a bug reporter needs to open those lines and
// redact them. It stops at maxListedFiles, counting the ones already listed,
// and returns the new count.
func writeSkippedFiles(w io.Writer, indent, sessionID string, files []models.FileSkips, listed int) int {
	for _, f := range files {
		if listed == maxListedFiles {
			break
		}
		label := "session " + shortSessionID(sessionID)
		if f.AgentID != "" {
			label = "agent " + f.AgentID
		}
		fmt.Fprintf(w, "%s%s: %s\n%s  %s\n", indent, label, describeSkippedLines(f), indent, render.NoBreak(f.Path))
		listed++
	}
	return listed
}

func writeUnlisted(w io.Writer, indent string, listed, total int) {
	if total > listed {
		fmt.Fprintf(w, "%sand %s with skipped lines\n", indent, plural(total-listed, "more file"))
	}
}

// describeSkippedLines reads "skipped lines 1203, 1207 (malformed), 1500
// (oversized)": line numbers in file order, each run of one reason labeled
// once. The parser records only the first lines, so a long tail is a count.
func describeSkippedLines(f models.FileSkips) string {
	var b strings.Builder
	if f.Count == 1 {
		b.WriteString("skipped line ")
	} else {
		b.WriteString("skipped lines ")
	}
	for i := 0; i < len(f.Lines); {
		j := i
		var nums []string
		for ; j < len(f.Lines) && f.Lines[j].Reason == f.Lines[i].Reason; j++ {
			nums = append(nums, strconv.Itoa(f.Lines[j].Line))
		}
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s (%s)", strings.Join(nums, ", "), f.Lines[i].Reason)
		i = j
	}
	if more := f.Count - len(f.Lines); more > 0 {
		fmt.Fprintf(&b, ", and %d more", more)
	}
	return b.String()
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// shortSessionID is the 8-character prefix of a UUID session ID, which the
// partial-ID lookup accepts. Other IDs are short enough to show whole.
func shortSessionID(id string) string {
	if len(id) == 36 {
		return id[:8]
	}
	return id
}

// projectLabel names a project in per-session warning details. The original
// path is what people know a project by. Without one, the encoded directory
// name is the exact value --project-dir takes. Either reads the same on every
// OS, unlike DisplayName, whose decoding of the encoded name depends on the OS
// ficha runs on.
func projectLabel(p models.ProjectInfo) string {
	if p.OriginalPath != "" {
		return p.OriginalPath
	}
	return p.EncodedPath
}

// labelSkips labels one project's skip details, prefixed with the project
// name when the report spans projects.
func labelSkips(project string, details []models.SkipDetail) []namedSkip {
	out := make([]namedSkip, len(details))
	for i, d := range details {
		label := shortSessionID(d.SessionID)
		if project != "" {
			label = project + "/" + label
		}
		out[i] = namedSkip{label: label, SkipDetail: d}
	}
	return out
}

// warnEstimatedCosts warns when some messages' cache-write tokens carried no
// TTL attribution and were priced at the 5m rate, the cheapest write tier, so
// the affected totals are lower-bound estimates rather than exact.
func warnEstimatedCosts(w io.Writer, estimatedCostMessages int) {
	if estimatedCostMessages > 0 {
		fmt.Fprintf(w, "Warning: %d message(s) lack cache-write TTL detail; their write cost assumes the 5m rate and may be underestimated\n", estimatedCostMessages)
	}
}

// unpricedModels returns the cost_by_model keys ficha has no price for,
// sorted, or nil. json's unpriced_models and the stderr warning both come
// from here, so they always name the same models.
func unpricedModels(costByModel map[string]models.CostBreakdown) []string {
	var unknown []string
	for model := range costByModel {
		if !pricing.IsKnownModel(model) {
			unknown = append(unknown, model)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// markUnpriced fills unpriced_models on a session and each of its agents.
func markUnpriced(a *models.SessionAnalysis) {
	a.UnpricedModels = unpricedModels(a.CostByModel)
	for i := range a.Agents {
		a.Agents[i].UnpricedModels = unpricedModels(a.Agents[i].CostByModel)
	}
}

// releasesURL is where a model ficha can't price may already have one.
const releasesURL = "https://github.com/bardisty/ficha/releases"

// warnUnknownModels warns about models priced at the fallback rate, and says
// what that rate is, so the reader can judge how far off the total may be.
// Prices ship with the binary, so a second line names this build and points
// at the releases: a newer ficha often knows the model already, and ficha
// makes no network requests to find out for itself. unknownModels comes from
// unpricedModels.
func warnUnknownModels(w io.Writer, unknownModels []string) {
	writeUnknownModels(w, unknownModels, version())
}

func writeUnknownModels(w io.Writer, unknownModels []string, ver string) {
	if len(unknownModels) == 0 {
		return
	}
	// Every unknown model gets the same fallback row.
	p := pricing.GetModelPricing(unknownModels[0])
	rates := fmt.Sprintf("$%g/$%g per MTok", p.InputRate, p.OutputRate)
	them := "it"
	if len(unknownModels) == 1 {
		fmt.Fprintf(w, "Warning: unknown model %q priced at fallback %s\n", unknownModels[0], rates)
	} else {
		fmt.Fprintf(w, "Warning: %d unknown models priced at fallback %s: %s\n", len(unknownModels), rates, strings.Join(unknownModels, ", "))
		them = "them"
	}
	// A build with no version was built from source, most likely from a
	// checkout, where the fix is a catalog row rather than an upgrade.
	if ver == "dev" {
		fmt.Fprintf(w, "  This development build of ficha has no price for %s. Add %s to modelCatalog in internal/pricing/pricing.go.\n", them, them)
		return
	}
	fmt.Fprintf(w, "  ficha %s has no price for %s. A newer release may know %s: %s\n", ver, them, them, releasesURL)
}
