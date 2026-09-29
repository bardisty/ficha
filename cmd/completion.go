package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/spf13/cobra"
)

// Values offered by flag completion. Validation lives with each flag's command.
var (
	formatValues = []string{"table", "json", "csv"}
	sortByValues = []string{"cost", "sessions", "name", "activity"}
)

// countBudgetBytes bounds how much transcript data session-ID completion
// parses for message counts. Counting reads each file in full, and a single
// long session can run to hundreds of MB, so when the candidates together are
// larger than this the descriptions show the age alone and Tab stays fast.
const countBudgetBytes = 8 << 20

// completeSessionIDs completes the positional session ID of show, watch and
// breakdown with this project's sessions, newest first.
func completeSessionIDs(cfg *config) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// A note or warning from the lookup would land in the shell's
		// candidate list.
		cfg.stdout, cfg.stderr = io.Discard, io.Discard
		sessions, err := loadProjectSessions(cfg, false)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		sortSessionsByModified(sessions)

		var candidates []models.SessionEntry
		var totalBytes int64
		for _, s := range sessions {
			if strings.HasPrefix(s.SessionID, toComplete) {
				candidates = append(candidates, s)
				if info, err := os.Stat(s.FullPath); err == nil {
					totalBytes += info.Size()
				}
			}
		}
		count := totalBytes <= countBudgetBytes

		now := time.Now()
		out := make([]cobra.Completion, 0, len(candidates))
		for _, s := range candidates {
			desc := "modified " + ageString(now.Sub(s.Modified))
			if count {
				if res, err := parser.ParseJSONLFileWithResult(s.FullPath); err == nil {
					desc += fmt.Sprintf(" · %d msgs", len(res.Messages))
				}
			}
			out = append(out, cobra.CompletionWithDesc(completionValue(s.SessionID, sessions, len(toComplete)), desc))
		}
		return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
	}
}

// completionValue shortens a session ID to its first 8 characters, which the
// partial-ID lookup accepts, unless another session in the project shares
// that prefix or the user has already typed past it.
func completionValue(id string, sessions []models.SessionEntry, typed int) string {
	const short = 8
	if len(id) <= short || typed > short {
		return id
	}
	for _, s := range sessions {
		if s.SessionID != id && strings.HasPrefix(s.SessionID, id[:short]) {
			return id
		}
	}
	return id[:short]
}

// ageString renders a duration as a coarse age: "just now", "5m ago",
// "3h ago", "2d ago".
func ageString(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// completeProjectDirs completes --project-dir with the Claude project
// directory names, described by the path Claude Code ran in when it's known.
func completeProjectDirs(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	projects, err := parser.DiscoverAllProjects()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []cobra.Completion
	for _, p := range projects {
		if !strings.HasPrefix(p.EncodedPath, toComplete) {
			continue
		}
		// The name decoded from the directory alone just repeats the value.
		if p.OriginalPath == "" {
			out = append(out, p.EncodedPath)
			continue
		}
		out = append(out, cobra.CompletionWithDesc(p.EncodedPath, p.OriginalPath))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// fixedValues completes a flag from a closed set of values.
func fixedValues(values []string) cobra.CompletionFunc {
	return func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	}
}
