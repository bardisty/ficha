package cmd

import (
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
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
	// list's and summary's --sort-by, for sessions.
	sessionSortByValues = []string{"time", "cost"}
	// The common --since and --until values. Dates and other ages are
	// typed out.
	windowValues = []string{"today", "yesterday", "7d", "30d"}
)

// countBudgetBytes bounds how much transcript data Tab parses for its
// descriptions' titles and message counts. Parsing reads each file in full,
// and a single long session can run to hundreds of MB, so when the sessions
// together are larger than this each description is the age alone and Tab
// stays fast.
const countBudgetBytes = 8 << 20

// describedTitleWidth caps a title in a session's description. zsh cuts a
// description at the terminal's edge, and the age and count come after it.
const describedTitleWidth = 40

// describeSessions describes each session the way list would pick it out,
// as in "add billing page · 2d ago · 64 msgs": its title when it has one,
// how long ago it was last active, and its message count, agents' included.
// active holds each session's lastActive, the time the pickers order by, as
// sortSessionsByLastActive returns it. The title and count take a full
// parse, so they're left out when the transcripts, agents included, add up
// to more than budget bytes. A negative budget has no limit.
func describeSessions(sessions []models.SessionEntry, active map[string]time.Time, now time.Time, budget int64) []string {
	var totalBytes int64
	for _, s := range sessions {
		for _, path := range append([]string{s.FullPath}, s.AgentPaths...) {
			if info, err := os.Stat(path); err == nil {
				totalBytes += info.Size()
			}
		}
	}
	parse := budget < 0 || totalBytes <= budget
	out := make([]string, len(sessions))
	// Each session's parse writes only its own line, so they run side by
	// side: ten long transcripts take as long as the longest, given the cores.
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for i, s := range sessions {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = describeSession(s, render.Ago(active[s.FullPath], now), parse)
		})
	}
	wg.Wait()
	return out
}

// describeSession is describeSessions for one session last active ago, with
// its title and count when parse is set.
func describeSession(s models.SessionEntry, ago string, parse bool) string {
	if !parse {
		return ago
	}
	res, err := parser.ParseJSONLFileWithResult(s.FullPath)
	if err != nil {
		return ago
	}
	n := len(res.Messages)
	for _, path := range s.AgentPaths {
		if agent, err := parser.ParseJSONLFileWithResult(path); err == nil {
			n += len(agent.Messages)
		}
	}
	desc := ago + " · " + plural(n, "msg")
	if title := models.OneLine(res.Title); title != "" {
		desc = render.TruncateRight(title, describedTitleWidth) + " · " + desc
	}
	return desc
}

// completeSessionIDs completes the positional session ID of show, watch and
// breakdown with this project's sessions, latest active first.
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
		var candidates []models.SessionEntry
		for _, s := range sessions {
			if strings.HasPrefix(s.SessionID, strings.ToLower(toComplete)) {
				candidates = append(candidates, s)
			}
		}
		active := sortSessionsByLastActive(candidates)
		descs := describeSessions(candidates, active, time.Now(), countBudgetBytes)
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
