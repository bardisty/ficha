package cmd

import (
	"fmt"
	"sort"

	"github.com/bardisty/ficha/internal/formatter"
	"github.com/bardisty/ficha/internal/models"
	"github.com/spf13/cobra"
)

func newListCmd(cfg *config) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available sessions",
		Long: `List all Claude Code sessions for the current project.

Sessions are sorted by modification time (most recent first).

Examples:
  ficha list                    List all sessions
  ficha list -f json            Output as JSON
  ficha list -f csv             Output as CSV`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cfg)
		},
	}
}

func runList(cfg *config) error {
	// list displays per-session message counts, so request the discovery-time scan.
	sessions, err := loadProjectSessions(cfg, true)
	if err != nil {
		return err
	}

	// Sort sessions by modified time (most recent first)
	sortSessionsByModified(sessions)

	// The counts below come from the same parse `show` runs, so warn about the
	// inputs it dropped — otherwise a session whose transcript could not be read
	// is indistinguishable from one that holds no messages.
	skippedSessions := 0
	skippedAgents := 0
	skippedLines := 0
	for _, s := range sessions {
		skippedSessions += s.SkippedSessions
		skippedAgents += s.SkippedAgents
		skippedLines += s.SkippedLines
	}
	warnSkippedSessions(cfg.stderr, skippedSessions)
	warnSkippedAgents(cfg.stderr, skippedAgents)
	warnSkippedLines(cfg.stderr, skippedLines)

	// Output in requested format
	var output string
	switch cfg.format {
	case "json":
		output, err = formatter.FormatSessionListJSON(sessions, true)
		if err != nil {
			return fmt.Errorf("formatting output: %w", err)
		}
	case "csv":
		output, err = formatter.FormatSessionListCSV(sessions)
		if err != nil {
			return fmt.Errorf("formatting output: %w", err)
		}
	default:
		output = formatter.FormatSessionListTable(sessions, cfg.noColor)
	}

	fmt.Fprintln(cfg.stdout, output)
	return nil
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
