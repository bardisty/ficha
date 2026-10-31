package cmd

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"time"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/parser"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// newestSessionWithReplies walks the project's sessions from latest to
// oldest, in latestSession's order, and returns the first with an assistant
// reply, plus how many later ones it passed over. A common flow is to open a
// new Claude Code session and run ficha to see what the last one cost; the
// new session comes first but has nothing to report yet. It only passes over
// a session that is truly empty: one it couldn't fully read may hold replies,
// and reporting an older session in its place would hide that. It returns nil
// when it stops there, or when no session has a reply.
func newestSessionWithReplies(cfg *config, scope analyzer.MessageScope) (*models.SessionAnalysis, int, error) {
	sessions, err := loadProjectSessions(cfg)
	if err != nil {
		return nil, 0, err
	}
	sortSessionsByModified(sessions)
	for skipped := 0; len(sessions) > 0; skipped++ {
		i := latestSession(sessions)
		s := sessions[i]
		a, err := analyzer.AnalyzeSession(s.FullPath, s.SessionID, scope)
		if err != nil {
			return nil, 0, nil
		}
		if a.MessageCount > 0 {
			return a, skipped, nil
		}
		if !trulyEmpty(a) {
			return nil, 0, nil
		}
		sessions = slices.Delete(sessions, i, i+1)
	}
	return nil, 0, nil
}

// lastActive is when a session last did something: its newest reply, or for
// a session with none yet, its file's mtime. list orders sessions the same
// way, though list also counts the agents' replies.
func lastActive(s models.SessionEntry) time.Time {
	if t, ok := parser.NewestReplyTimestamp(s.FullPath); ok {
		return t
	}
	return s.Modified
}

// latestSession returns the index of the session that was last active, the
// one bare ficha, watch and breakdown open. sessions must be sorted by
// sortSessionsByModified. A file's mtime is never earlier than its last
// reply, so the walk stops at the first file older than the best reply found,
// which is usually the second. A transcript whose mtime moved backwards, as a
// copy or restore can do, may be missed.
func latestSession(sessions []models.SessionEntry) int {
	best, bestAt := 0, time.Time{}
	for i, s := range sessions {
		if i > 0 && s.Modified.Before(bestAt) {
			break
		}
		if at := lastActive(s); i == 0 || at.After(bestAt) {
			best, bestAt = i, at
		}
	}
	return best
}

// sortSessionsByLastActive sorts sessions by lastActive, latest first, with
// SessionID as the tiebreaker. Unlike latestSession it reads every session's
// tail.
func sortSessionsByLastActive(sessions []models.SessionEntry) {
	at := make(map[string]time.Time, len(sessions))
	for _, s := range sessions {
		at[s.FullPath] = lastActive(s)
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		a, b := at[sessions[i].FullPath], at[sessions[j].FullPath]
		if a.Equal(b) {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return a.After(b)
	})
}

// trulyEmpty reports whether a has no replies and nothing ficha failed to
// read, which could have held some.
func trulyEmpty(a *models.SessionAnalysis) bool {
	return a.MessageCount == 0 && a.SkippedLines == 0 && a.SkippedAgents == 0
}

// writeSkippedNote says the report isn't about the newest session, and
// names that session when there's one, so 'ficha show <id>' can reach it.
func writeSkippedNote(w io.Writer, skipped int, newestID string, noColor bool) {
	note := "(skipped " + render.Count(skipped) + " newer sessions with no replies yet)"
	if skipped == 1 {
		note = fmt.Sprintf("(skipped 1 newer session with no replies yet: %s)", render.TruncateID(newestID, 8))
	}
	if !noColor {
		note = styles.DimStyle.Render(note)
	}
	fmt.Fprintln(w, note)
}
