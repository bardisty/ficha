package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/spf13/cobra"
)

// addNestedFlag registers --nested, which summary and list take.
func addNestedFlag(cmd *cobra.Command, cfg *config) {
	cmd.Flags().BoolVar(&cfg.nested, "nested", false, "Include the projects under this directory at any depth, such as the worktrees claude --worktree makes")
}

// checkNested rejects --nested with --project-dir, which names a Claude
// project directory, not the directory Claude Code ran in, so there's no
// path to look under.
func checkNested(cfg *config) error {
	if cfg.nested && cfg.projectDir != "" {
		return usageErrorf("--nested looks under the directory Claude Code ran in, and --project-dir names a Claude project directory instead. Give the directory with -p.")
	}
	return nil
}

// writeNestedNote tells a report without --nested what the projects under
// its directory would add to it, in window. own are the report's sessions
// and ownTotal their total cost.
//
// A worktree session can resume one of the directory's own and start with a
// copy of its history, which --nested counts once. So the note prices the
// report as --nested would and takes away what the report already counts,
// at the cost of a second parse of every session.
func writeNestedNote(w io.Writer, cfg *config, own []models.SessionEntry, ownTotal float64, nested nestedSessions, window models.TimeWindow, project string) {
	if len(nested.sessions) == 0 {
		return
	}
	combined := append(slices.Clip(own), nested.sessions...)
	aggregate, results, err := analyzer.AnalyzeMultipleSessionsInWindow(combined, window)
	if err != nil {
		return
	}
	sessions := 0
	projects := map[string]bool{}
	for _, r := range results {
		dir := filepath.Dir(r.Entry.FullPath)
		if _, ok := nested.projects[dir]; ok && r.Analysis != nil {
			sessions++
			projects[dir] = true
		}
	}
	if sessions == 0 {
		return
	}
	// Floating-point sums in two orders can leave a hair below zero.
	added := max(aggregate.TotalCost.TotalCost-ownTotal, 0)
	have := "have"
	if len(projects) == 1 {
		have = "has"
	}
	inWindow := ""
	if !window.IsZero() {
		inWindow = " in the window"
	}
	fmt.Fprintf(w, "Note: %s under %s %s %s%s, %s. Add --nested to count them in.\n",
		plural(len(projects), "project"), render.NoBreak(project), have, plural(sessions, "session"),
		inWindow, render.Cost(added))
}

// nestedLabel names a --nested report's project in its header: the
// directory's, and how many projects under it the report counts in.
func nestedLabel(project string, results []models.SessionResult) string {
	names := map[string]bool{}
	for _, r := range results {
		if r.Nested != "" {
			names[r.Nested] = true
		}
	}
	if len(names) == 0 {
		return project
	}
	return fmt.Sprintf("%s +%s", project, plural(len(names), "project"))
}
