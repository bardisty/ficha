package cmd

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
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
	writeReport(cfg.stdout, cfg.stderr, warnings, output, isTerminal(cfg.stderr))
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
}

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
	if len(s.details) == 0 || s.sessions+s.agents+s.lines == 0 {
		return
	}
	if !verbose {
		fmt.Fprintln(w, "  Run with -v to list the affected sessions.")
		return
	}
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
		fmt.Fprintf(w, "  %s: %s\n", d.label, strings.Join(parts, ", "))
	}
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

// warnUnknownModels warns about models priced at the fallback rate, and says
// what that rate is, so the reader can judge how far off the total may be.
// unknownModels comes from unpricedModels.
func warnUnknownModels(w io.Writer, unknownModels []string) {
	if len(unknownModels) == 0 {
		return
	}
	// Every unknown model gets the same fallback row.
	p := pricing.GetModelPricing(unknownModels[0])
	rates := fmt.Sprintf("$%g/$%g per MTok", p.InputRate, p.OutputRate)
	if len(unknownModels) == 1 {
		fmt.Fprintf(w, "Warning: unknown model %q priced at fallback %s\n", unknownModels[0], rates)
		return
	}
	fmt.Fprintf(w, "Warning: %d unknown models priced at fallback %s: %s\n", len(unknownModels), rates, strings.Join(unknownModels, ", "))
}
