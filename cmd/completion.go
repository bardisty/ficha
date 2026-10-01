package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/render"
	"github.com/spf13/cobra"
)

// Values offered by flag completion. Validation lives with each flag's command.
var (
	formatValues = []string{"table", "json", "csv"}
	sortByValues = []string{"cost", "sessions", "name", "activity"}
)

// countBudgetBytes bounds how much transcript data a session description
// parses for message counts. Counting reads each file in full, and a single
// long session can run to hundreds of MB, so when the sessions together are
// larger than this the descriptions show the age alone and Tab stays fast.
const countBudgetBytes = 8 << 20

// describeSessions describes each session as "modified 4d ago · 10 msgs",
// counting agent messages too, as `list` does. The count is left out when the
// transcripts, agents included, exceed countBudgetBytes.
func describeSessions(sessions []models.SessionEntry, now time.Time) []string {
	var totalBytes int64
	for _, s := range sessions {
		for _, path := range append([]string{s.FullPath}, s.AgentPaths...) {
			if info, err := os.Stat(path); err == nil {
				totalBytes += info.Size()
			}
		}
	}
	count := totalBytes <= countBudgetBytes
	out := make([]string, len(sessions))
	for i, s := range sessions {
		out[i] = "modified " + render.Ago(s.Modified, now)
		if !count {
			continue
		}
		res, err := parser.ParseJSONLFileWithResult(s.FullPath)
		if err != nil {
			continue
		}
		n := len(res.Messages)
		for _, path := range s.AgentPaths {
			if agent, err := parser.ParseJSONLFileWithResult(path); err == nil {
				n += len(agent.Messages)
			}
		}
		out[i] += fmt.Sprintf(" · %d msgs", n)
	}
	return out
}

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
		sessions, err := loadProjectSessions(cfg)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		sortSessionsByModified(sessions)

		var candidates []models.SessionEntry
		for _, s := range sessions {
			if strings.HasPrefix(s.SessionID, strings.ToLower(toComplete)) {
				candidates = append(candidates, s)
			}
		}

		descs := describeSessions(candidates, time.Now())
		out := make([]cobra.Completion, 0, len(candidates))
		for i, s := range candidates {
			value := inTypedCase(completionValue(s.SessionID, sessions, len(toComplete)), toComplete)
			out = append(out, cobra.CompletionWithDesc(value, descs[i]))
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

// inTypedCase returns a completion that starts with exactly what was typed.
// The lookup ignores case, but bash and zsh drop any candidate that doesn't
// match the typed prefix byte for byte, so "BDD" must complete to "BDD640FB",
// not "bdd640fb". An all-uppercase prefix gets an uppercase remainder.
func inTypedCase(value, typed string) string {
	if len(typed) > len(value) {
		return value
	}
	rest := value[len(typed):]
	if typed == strings.ToUpper(typed) && typed != strings.ToLower(typed) {
		rest = strings.ToUpper(rest)
	}
	return typed + rest
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
