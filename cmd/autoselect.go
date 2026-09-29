package cmd

import (
	"fmt"
	"io"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
)

// newestSessionWithReplies walks the project's sessions from newest to oldest
// and returns the first with an assistant reply, plus how many newer ones it
// passed over. A common flow is to open a new Claude Code session and run
// ficha to see what the last one cost; the new session is newest by mtime
// but has nothing to report yet. It returns nil when no session has a reply,
// or when one it would pass over can't be analyzed, since skipping that would
// silently report an older session in its place.
func newestSessionWithReplies(cfg *config, scope analyzer.MessageScope) (*models.SessionAnalysis, int, error) {
	sessions, err := loadProjectSessions(cfg, false)
	if err != nil {
		return nil, 0, err
	}
	sortSessionsByModified(sessions)
	for i, s := range sessions {
		a, err := analyzer.AnalyzeSession(s.FullPath, s.SessionID, scope)
		if err != nil {
			return nil, 0, nil
		}
		if a.MessageCount > 0 {
			return a, i, nil
		}
	}
	return nil, 0, nil
}

// writeSkippedNote says the report isn't about the newest session, and
// names that session when there's one, so 'ficha show <id>' can reach it.
func writeSkippedNote(w io.Writer, skipped int, newestID string, noColor bool) {
	note := fmt.Sprintf("(skipped %d newer sessions with no replies yet)", skipped)
	if skipped == 1 {
		note = fmt.Sprintf("(skipped 1 newer session with no replies yet: %s)", render.TruncateID(newestID, 8))
	}
	if !noColor {
		note = styles.DimStyle.Render(note)
	}
	fmt.Fprintln(w, note)
}
