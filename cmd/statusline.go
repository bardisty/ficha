package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/spf13/cobra"
)

func newStatuslineCmd(cfg *config) *cobra.Command {
	return &cobra.Command{
		Use:   "statusline",
		Short: "Print a line for Claude Code's status line",
		Long: `Print one line for Claude Code's status line: the model, how full the
context window is, what recent messages cost, and the session's total with
the agents' share.

Claude Code runs the command after each message and writes JSON about the
session on stdin. statusline reads transcript_path and model.display_name
from it. Add it to ~/.claude/settings.json:

  { "statusLine": { "type": "command", "command": "ficha statusline" } }`,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatusline(cfg, cmd.InOrStdin())
		},
	}
}

// statusLineInput is the part of Claude Code's status line JSON that
// statusline reads. Claude Code sends many more fields, and adds new ones;
// the decoder ignores whatever isn't named here.
type statusLineInput struct {
	TranscriptPath string `json:"transcript_path"`
	Model          struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
}

func runStatusline(cfg *config, stdin io.Reader) error {
	// Run by hand, statusline would wait for JSON nobody is going to type.
	if readsTerminal(stdin) {
		return usageErrorf("%s reads Claude Code's status line JSON on stdin, and stdin is a terminal. '%s --help' shows how to set it up.", cfg.command(), cfg.command())
	}
	var in statusLineInput
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("no JSON on stdin: statusline reads the JSON Claude Code writes for its status line")
		}
		return fmt.Errorf("reading Claude Code's status line JSON on stdin: %w", err)
	}
	if in.TranscriptPath == "" {
		return errors.New("no transcript_path in the status line JSON on stdin")
	}

	session, _, err := sessionFromPath(cfg, in.TranscriptPath)
	if err != nil {
		return err
	}
	cfg.tracef("session file %s", session.FullPath)
	analysis, err := analyzer.AnalyzeSession(session.FullPath, session.SessionID, analyzer.NoMessages)
	if err != nil {
		return fmt.Errorf("analyzing session: %w", err)
	}

	// Claude Code shows only stdout. The warnings go to stderr as they do for
	// show, where claude --debug logs them and a run by hand shows them.
	var warnings bytes.Buffer
	skipWarning{
		counts:    "totals",
		agents:    analysis.SkippedAgents,
		lines:     analysis.SkippedLines,
		sessionID: analysis.SessionID,
		files:     analysis.SkippedFiles,
	}.write(&warnings, cfg.verbose)
	warnEstimatedCosts(&warnings, analysis.EstimatedCostMessages)
	markUnpriced(analysis)
	analysis.Context = sessionContext(analysis)
	warnUnknownModels(&warnings, analysis.UnpricedModels)
	fmt.Fprint(cfg.stderr, wrapStderr(cfg.stderr, warnings.String()))

	model := in.Model.DisplayName
	if model == "" && analysis.LastMessageModel != "" {
		model = pricing.GetModelDisplayName(analysis.LastMessageModel)
	}
	fmt.Fprintln(cfg.stdout, statusLine(model, analysis))
	return nil
}

// statusLine is the line statusline prints, as in
// "Opus 5.5 · ctx 68% · $0.19/msg ▲ · $43.67 (agents $12.54)". A part with
// nothing behind it is left out: the context before the first reply, the
// recent cost before the trend has enough messages, the arrow while the
// trend is stable, the agents' share when they cost nothing. Amounts take two
// decimals and the context none, to keep the line short.
func statusLine(model string, a *models.SessionAnalysis) string {
	var parts []string
	if model != "" {
		parts = append(parts, model)
	}
	if a.Context != nil {
		parts = append(parts, fmt.Sprintf("ctx %.0f%%", a.Context.Percent))
	}
	if in := a.Insights; in != nil && in.HasTrend() {
		recent := fmt.Sprintf("$%.2f/msg", in.RecentAvgCost)
		if in.CostTrend != models.TrendStable {
			recent += " " + render.TrendSymbol(in.CostTrend)
		}
		parts = append(parts, recent)
	}
	total := fmt.Sprintf("$%.2f", a.TotalCost.TotalCost)
	if a.AgentsCost.TotalCost > 0 {
		total += fmt.Sprintf(" (agents $%.2f)", a.AgentsCost.TotalCost)
	}
	parts = append(parts, total)
	return strings.Join(parts, " "+styles.FieldSep+" ")
}
