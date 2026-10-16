package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/formatter"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/spf13/cobra"
)

func newListCmd(cfg *config) *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List available sessions",
		Long: `List all Claude Code sessions for the current project.

Sessions are sorted by their last message, most recent first. WHEN is that
message's time, the one show calls "Last active".

Examples:
  ficha list                    List all sessions
  ficha list --since 7d         Sessions with messages in the last 7 days
  ficha list --nested           Include the repo's worktrees
  ficha list -f json            Output as JSON
  ficha list -f csv             Output as CSV

` + windowForms + `

Both keep the sessions with a message inside the window, as summary
--details does. WHEN, LENGTH and COST cover only the messages inside it,
and so do message_count and the agent counts in json and csv.

A worktree under the repo, from claude --worktree or git worktree add, is a
project of its own. list notes when projects under this directory have
sessions, and --nested lists them too, each titled with its project.`,
		Args:              noArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cfg)
		},
	}

	addFormatFlag(listCmd, cfg, false)
	addProjectFlags(listCmd, cfg)
	addWindowFlags(listCmd, cfg)
	addNestedFlag(listCmd, cfg)

	return listCmd
}

func runList(cfg *config) error {
	if err := checkNested(cfg); err != nil {
		return err
	}
	window, err := cfg.timeWindow(time.Now())
	if err != nil {
		return err
	}
	if cfg.format == "table" {
		return runListTable(cfg, window)
	}

	sessions, root, nested, err := loadProjectAndNestedSessions(cfg)
	if err != nil {
		return err
	}
	if cfg.nested {
		sessions = append(sessions, nested.sessions...)
	}

	// Priced the way the table is, so costs match summary -d.
	aggregate, results, err := analyzer.AnalyzeMultipleSessionsInWindow(sessions, window)
	if err != nil {
		results = make([]models.SessionResult, len(sessions))
		for i, s := range sessions {
			results[i] = models.SessionResult{Entry: s}
		}
	}
	nested.mark(results)
	sortByLastActive(results)
	for i := range results {
		results[i].Entry = countedEntry(results[i], !window.IsZero())
	}

	// Warn about the inputs the parse dropped — otherwise a session whose
	// transcript could not be read is indistinguishable from one that holds
	// no messages. Counts, costs and the files -v names all come from one
	// read of each transcript, so the files add up to the count even for a
	// transcript still being written.
	skips := skipWarning{counts: "message counts and costs"}
	var details []models.SkipDetail
	for _, r := range results {
		s := r.Entry
		skips.sessions += s.SkippedSessions
		skips.agents += s.SkippedAgents
		skips.lines += s.SkippedLines
		if s.SkippedSessions+s.SkippedAgents+s.SkippedLines > 0 {
			details = append(details, models.SkipDetail{
				SessionID:  s.SessionID,
				Unreadable: s.SkippedSessions > 0,
				Lines:      s.SkippedLines,
				Agents:     s.SkippedAgents,
				Files:      s.SkippedFiles,
			})
		}
	}
	skips.details = labelSkips("", details)
	if !window.IsZero() && aggregate != nil {
		skips = windowSkips(skips.counts, aggregate)
	}
	var warnings bytes.Buffer
	skips.write(&warnings, cfg.verbose)
	if aggregate != nil {
		warnEstimatedCosts(&warnings, aggregate.EstimatedCostMessages)
		warnUnknownModels(&warnings, unpricedModels(aggregate.CostByModel))
	}
	if !cfg.nested {
		writeNestedNote(&warnings, cfg, nested, window, root.name)
	}

	// Cleaned as each transcript's directory is, which --project-dir isn't.
	originalPaths := map[string]string{filepath.Clean(root.dir): parser.ProjectOriginalPath(root.dir)}
	for dir, p := range nested.projects {
		originalPaths[dir] = p.path
	}
	var output string
	switch cfg.format {
	case "json":
		output, err = formatter.FormatSessionListJSON(results, originalPaths, true)
	case "csv":
		output, err = formatter.FormatSessionListCSV(results, originalPaths)
	}
	if err != nil {
		return fmt.Errorf("formatting output: %w", err)
	}

	printReport(cfg, &warnings, output)
	return nil
}

// countedEntry returns r's entry with the message counts and skip accounting
// json and csv print. They are show's counts for the session: its whole
// transcript, the messages a fork copied included, where the analysis leaves
// those out of its totals as already billed. Agents are never copied, so
// their figures are the analysis's own.
//
// Under a window, the message count is the window's share, as summary -d
// counts it, so the two agree.
//
// A session whose transcript can't be read has no analysis. Its agents still
// count, so they are parsed here for it.
func countedEntry(r models.SessionResult, windowed bool) models.SessionEntry {
	if r.Analysis == nil {
		return parser.CountSessionMessages(r.Entry)
	}
	entry := r.Entry
	entry.MessageCount = r.Analysis.TranscriptMessages + r.Analysis.AgentMessageCount
	if windowed {
		entry.MessageCount = r.Analysis.MessageCount
	}
	entry.AgentCount = r.Analysis.AgentCount
	entry.AgentMessageCount = r.Analysis.AgentMessageCount
	entry.SkippedAgents = r.Analysis.SkippedAgents
	entry.SkippedLines = r.Analysis.SkippedLines
	entry.SkippedFiles = r.Analysis.SkippedFiles
	return entry
}

// runListTable prices every session the way summary --details does, cross-
// session duplicates attributed once, so a session's cost here matches its
// row there.
func runListTable(cfg *config, window models.TimeWindow) error {
	sessions, root, nested, err := loadProjectAndNestedSessions(cfg)
	if err != nil {
		return err
	}
	if cfg.nested {
		sessions = append(sessions, nested.sessions...)
	}
	pickPalette(cfg)

	aggregate, results, err := analyzer.AnalyzeMultipleSessionsInWindow(sessions, window)
	if err != nil {
		// Every transcript failed to parse. List them anyway, as unreadable.
		results = make([]models.SessionResult, len(sessions))
		for i, s := range sessions {
			results[i] = models.SessionResult{Entry: s}
		}
	}
	nested.mark(results)
	sortByLastActive(results)

	skips := skipWarning{counts: "costs", details: labelSkips("", analyzer.SkipDetails(results))}
	for _, d := range skips.details {
		if d.Unreadable {
			skips.sessions++
		}
		skips.agents += d.Agents
		skips.lines += d.Lines
	}
	if !window.IsZero() && aggregate != nil {
		skips = windowSkips(skips.counts, aggregate)
	}
	var warnings bytes.Buffer
	skips.write(&warnings, cfg.verbose)
	if aggregate != nil {
		warnEstimatedCosts(&warnings, aggregate.EstimatedCostMessages)
		warnUnknownModels(&warnings, unpricedModels(aggregate.CostByModel))
	}
	if !cfg.nested {
		writeNestedNote(&warnings, cfg, nested, window, root.name)
	}

	opts := formatter.ListTableOptions{
		Project: nestedLabel(root.name, results),
		Width:   terminalWidth(cfg.stdout),
	}
	if !window.IsZero() {
		opts.Window = &window
	}
	output := formatter.FormatSessionListTable(results, cfg.noColor, opts)
	printReport(cfg, &warnings, output)
	return nil
}

// windowSkips is the skip warning under a window, the aggregate's as summary
// gives it. A session with nothing inside the window drops out of the rows,
// but what couldn't be read in it still counts: ficha can't tell whether it
// fell inside.
func windowSkips(counts string, aggregate *models.SessionAnalysis) skipWarning {
	return skipWarning{
		counts:   counts,
		sessions: aggregate.SkippedSessions,
		agents:   aggregate.SkippedAgents,
		lines:    aggregate.SkippedLines,
		details:  labelSkips("", aggregate.SkipDetails),
	}
}

// sortByLastActive sorts results by their last message, most recent first,
// with SessionID as the tiebreaker. It runs after the analysis, which gives
// each session its time: the analysis's results don't depend on the order
// its input came in.
func sortByLastActive(results []models.SessionResult) {
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i].LastActive(), results[j].LastActive()
		if a.Equal(b) {
			return results[i].Entry.SessionID < results[j].Entry.SessionID
		}
		return a.After(b)
	})
}

// sortSessionsByModified sorts sessions by modified time (most recent first).
// Uses SessionID as a tiebreaker for deterministic ordering.
func sortSessionsByModified(sessions []models.SessionEntry) {
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].Modified.Equal(sessions[j].Modified) {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return sessions[i].Modified.After(sessions[j].Modified)
	})
}
