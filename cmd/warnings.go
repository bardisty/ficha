package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/charmbracelet/colorprofile"
)

// printReport writes a static report and the warnings gathered while building
// it. On a terminal the warnings follow the report: they qualify its totals,
// and printed first they scroll off the top of a short terminal before the
// prompt returns. Redirected, they keep coming first, so a log that captures
// both streams reads cause before effect.
//
// Every output ends in exactly one newline, which writeReport adds. csv
// arrives with its own, because csv.Writer ends the last record too.
//
// The formatter writes a colored table's escapes without knowing what stdout
// is. The writers here fit them to the run's profile: none at all on a pipe
// or under CI, the 16 basic colors where that's the terminal's limit. The
// one styled note among the warnings takes stdout's profile too. json, csv
// and --no-color carry no styling, and go out untouched.
func printReport(cfg *config, warnings *bytes.Buffer, output string) {
	if cfg.format == "csv" {
		output = strings.TrimSuffix(output, "\n")
	}
	warnings = bytes.NewBufferString(wrapStderr(cfg.stderr, warnings.String()))
	stdout, stderr := cfg.stdout, cfg.stderr
	if cfg.format == "table" && !cfg.noColor {
		// Windows' classic console prints escapes as text until a program
		// asks it to interpret them. Elsewhere this does nothing.
		if f, ok := cfg.stdout.(*os.File); ok {
			lipgloss.EnableLegacyWindowsANSI(f)
		}
		stdout = &colorprofile.Writer{Forward: stdout, Profile: cfg.profile}
		stderr = &colorprofile.Writer{Forward: stderr, Profile: cfg.profile}
	}
	writeReport(stdout, stderr, warnings, output, isTerminal(cfg.stderr))
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

// errorLabel starts every error ficha prints, and indents the lines after
// an error's first on a terminal.
const errorLabel = "Error: "

// formatError is err as Execute prints it. Off a terminal it's "Error: "
// and the message, one line per line of it, the same bytes whatever names
// the message holds, for logs and scripts to match on. On one, a
// displayError's paths under home read as ~ and keep whole with its
// commands, every line after the first hangs under the message, and each
// wraps between words like a note.
func formatError(stderr io.Writer, err error) string {
	msg := err.Error()
	width := stderrWidth(stderr)
	if width <= 0 {
		return errorLabel + msg + "\n"
	}
	// A wrapper that adds words of its own would lose them to the display
	// form, so only an error whose message is the displayError's takes it.
	var d *displayError
	if errors.As(err, &d) && d.Error() == msg {
		msg = d.onTerminal()
	}
	msg = strings.ReplaceAll(msg, "\n", "\n"+strings.Repeat(" ", len(errorLabel)))
	return render.WrapHanging(errorLabel+msg+"\n", width)
}

// displayError is an error message that names paths and commands, built a
// piece at a time with add. Error() is the plain message. onTerminal is the
// one a terminal shows, where errPath, errWhole and errCommand arguments
// take the forms their types describe.
type displayError struct {
	pieces []errorPiece
}

type errorPiece struct {
	format string
	args   []any
}

func newDisplayError(format string, args ...any) *displayError {
	e := &displayError{}
	e.add(format, args...)
	return e
}

func (e *displayError) add(format string, args ...any) {
	e.pieces = append(e.pieces, errorPiece{format, args})
}

func (e *displayError) Error() string {
	var sb strings.Builder
	for _, p := range e.pieces {
		fmt.Fprintf(&sb, p.format, p.args...)
	}
	return sb.String()
}

func (e *displayError) onTerminal() string {
	var sb strings.Builder
	for _, p := range e.pieces {
		args := make([]any, len(p.args))
		for i, a := range p.args {
			switch a := a.(type) {
			case errPath:
				args[i] = render.NoBreak(tildePath(string(a)))
			case errWhole:
				args[i] = render.NoBreak(string(a))
			case errCommand:
				args[i] = render.NoBreak(a.onTerminal())
			default:
				args[i] = a
			}
		}
		fmt.Fprintf(&sb, p.format, args...)
	}
	return sb.String()
}

// errPath is a path. A terminal shows it as ~/… when it's under home, which
// is how a typed ~/… reads too, as the shell expanded it.
type errPath string

// errWhole is text a terminal keeps on one line, as a command with no path
// in it.
type errWhole string

// errCommand is a command to paste back, as in "ficha list -p ~/work/webapp":
// text, then path quoted for the shell. A terminal shows path under home as
// ~/… unless the shell needs it quoted, since a shell doesn't expand ~
// inside quotes.
type errCommand struct {
	text, path string
}

func (c errCommand) String() string {
	return c.text + shellQuote(c.path)
}

func (c errCommand) onTerminal() string {
	if shellQuote(c.path) != c.path {
		return c.String()
	}
	return c.text + tildePath(c.path)
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

// warnIgnoredIndexes writes the line show, list and summary print for an
// unusable sessions-index.json, once per project that has one. A report over
// every project can hit hundreds when one cause damages them all, so past
// maxListedFiles the rest are a count unless -v asks for them. The count is
// a warning of its own, like the lines it stands in for.
func warnIgnoredIndexes(w io.Writer, ignored []models.IgnoredIndex, verbose bool) {
	for i, index := range ignored {
		if i == maxListedFiles && !verbose {
			fmt.Fprintf(w, "Warning: ignoring %s. Run with -v to list all %d.\n", plural(len(ignored)-i, "more sessions-index.json file"), len(ignored))
			return
		}
		fmt.Fprintf(w, "Warning: %s\n", ignoredIndexMessage(index.Path, index.Err))
	}
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
