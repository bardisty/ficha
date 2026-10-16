package cmd

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/spf13/cobra"
)

// addNestedFlag registers --nested, which summary and list take.
func addNestedFlag(cmd *cobra.Command, cfg *config) {
	cmd.Flags().BoolVar(&cfg.nested, "nested", false, "Include projects at any depth under this directory")
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
// its directory hold, in window. Pricing them costs a parse of each session.
//
// The cost is an upper bound. A worktree session can resume one of the
// directory's own and start with a copy of its history, which --nested
// counts once, under the directory's session. Pricing exactly what --nested
// adds would mean analyzing the directory's sessions again.
func writeNestedNote(w io.Writer, cfg *config, nested nestedSessions, window models.TimeWindow, project string) {
	if len(nested.sessions) == 0 {
		return
	}
	aggregate, results, err := analyzer.AnalyzeMultipleSessionsInWindow(nested.sessions, window)
	if err != nil || aggregate.SessionCount == 0 {
		return
	}
	projects := map[string]bool{}
	for _, r := range results {
		if r.Analysis != nil {
			projects[filepath.Dir(r.Entry.FullPath)] = true
		}
	}
	have := "have"
	if len(projects) == 1 {
		have = "has"
	}
	inWindow := ""
	if !window.IsZero() {
		inWindow = " in the window"
	}
	fmt.Fprintf(w, "Note: %s under %s %s %s%s, up to %s. Add --nested to count them in.\n",
		plural(len(projects), "project"), render.NoBreak(project), have, plural(aggregate.SessionCount, "session"),
		inWindow, render.Cost(aggregate.TotalCost.TotalCost))
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
