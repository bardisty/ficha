package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/bardisty/ficha/internal/analyzer"
	"github.com/bardisty/ficha/internal/models"
	"github.com/bardisty/ficha/internal/pricing"
	"github.com/bardisty/ficha/internal/render"
	"github.com/bardisty/ficha/internal/styles"
	"github.com/spf13/cobra"
)

func newStatuslineCmd(cfg *config) *cobra.Command {
	statuslineCmd := &cobra.Command{
		Use:   "statusline",
		Short: "Print a line for Claude Code's status line",
		Long: `Print one line for Claude Code's status line: the model, how full the
context window is, what recent messages cost, and the session's total with
the agents' share.

Claude Code runs the command after each message and writes JSON about the
session on stdin. statusline reads transcript_path and model.display_name
from it. Add it to ~/.claude/settings.json:

  { "statusLine": { "type": "command", "command": "ficha statusline" } }

Near ~compaction the context part says how much room is left, as in
"ctx 90%, 67.0K to ~compaction", and "past ~compaction" once it's over the
mark. It's colored; --no-color turns that off.`,
		Args:              noArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatusline(cfg, cmd.InOrStdin())
		},
	}
	addFormatFlag(statuslineCmd, cfg, true)
	return statuslineCmd
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
	data, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return errors.New("no JSON on stdin: statusline reads the JSON Claude Code writes for its status line")
	}
	var in statusLineInput
	if err := json.Unmarshal(data, &in); err != nil {
		// Go's own message names the struct the JSON was decoded into.
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			where := "the top level"
			if typeErr.Field != "" {
				where = typeErr.Field
			}
			return fmt.Errorf("reading Claude Code's status line JSON on stdin: unexpected %s at %s", typeErr.Value, where)
		}
		return fmt.Errorf("reading Claude Code's status line JSON on stdin: %w", err)
	}
	if in.TranscriptPath == "" {
		return errors.New("no transcript_path in the status line JSON on stdin")
	}
	// Claude Code always sends a .jsonl path. Any other file would read as a
	// session with no messages, and $0.00 on the status line looks like a
	// real reading where an error leaves it blank.
	if !strings.EqualFold(filepath.Ext(in.TranscriptPath), ".jsonl") {
		return fmt.Errorf("transcript_path %s isn't a .jsonl transcript", in.TranscriptPath)
	}
	abs, err := filepath.Abs(in.TranscriptPath)
	if err != nil {
		return err
	}
	// show names the session to run instead, as an argument statusline
	// doesn't take.
	if sessionDir := agentSessionDir(abs); sessionDir != "" {
		return fmt.Errorf("transcript_path %s is an agent's transcript, part of session %s", in.TranscriptPath, shortSessionID(filepath.Base(sessionDir)))
	}

	// Claude Code runs the command as a session starts, before it writes the
	// transcript, and a blank status line looks like a broken setup. A
	// missing file in a directory that's there is a session with no
	// messages yet. A missing directory is still bad input.
	if _, err := os.Stat(abs); errors.Is(err, fs.ErrNotExist) && isDir(filepath.Dir(abs)) {
		cfg.tracef("no transcript at %s yet", abs)
		fmt.Fprintln(cfg.stdout, statusLine(in.Model.DisplayName, &models.SessionAnalysis{}, cfg.noColor))
		return nil
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
	// Claude Code reads the line through a pipe and shows the escapes it
	// gets, so statusline colors on a pipe, unless --no-color, NO_COLOR or
	// CLICOLOR=0 says no. ficha can't ask a pipe for the terminal's
	// background, so it uses the 16 basic colors, which the user's own
	// terminal theme shades.
	if !cfg.noColor {
		styles.SetBasic(true)
		// Windows' classic console prints escapes as text until a program
		// asks it to interpret them, as a run by hand there would show.
		if f, ok := cfg.stdout.(*os.File); ok {
			lipgloss.EnableLegacyWindowsANSI(f)
		}
	}
	fmt.Fprintln(cfg.stdout, statusLine(model, analysis, cfg.noColor))
	return nil
}

// statusLine is the line statusline prints, as in
// "Opus 5.5 · ctx 68% · $0.19/msg ▲ · $43.67 (agents $12.54)". A part with
// nothing behind it is left out: the context before the first reply, the
// recent cost before the trend has enough messages, the arrow while the
// trend is stable, the agents' share when they cost nothing. Amounts take
// statusCost's precision and the context no decimals, to keep the line short.
// In the warning band and past the mark the context says how near
// ~compaction it is, as show's note does, and unless noColor its percentage
// takes the gauge's color. Below the band it stays plain, as an agent's row
// does, so color on the line means look here.
func statusLine(model string, a *models.SessionAnalysis, noColor bool) string {
	var parts []string
	if model != "" {
		parts = append(parts, model)
	}
	if c := a.Context; c != nil {
		reading := render.Number(c.Tokens)
		if c.WindowKnown() {
			reading = fmt.Sprintf("%.0f%%", c.Percent)
			if zone := pricing.ContextZoneOf(c.Tokens, c.CompactAt); !noColor && zone != pricing.ContextClear {
				reading = lipgloss.NewStyle().Foreground(styles.ContextZoneColor(zone)).Render(reading)
			}
			if words := render.ContextWords(*c); words != "" {
				reading += ", " + words
			}
		}
		parts = append(parts, "ctx "+reading)
	}
	if in := a.Insights; in != nil && in.HasTrend() {
		recent := statusCost(in.RecentAvgCost) + "/msg"
		if in.CostTrend != models.TrendStable {
			recent += " " + render.TrendSymbol(in.CostTrend)
		}
		parts = append(parts, recent)
	}
	total := statusCost(a.TotalCost.TotalCost)
	if a.AgentsCost.TotalCost > 0 {
		total += " (agents " + statusCost(a.AgentsCost.TotalCost) + ")"
	}
	parts = append(parts, total)
	return strings.Join(parts, " "+styles.FieldSep+" ")
}

// statusCost formats an amount for the status line: two decimals, which keep
// the line short, and four below a cent, where two would lose the amount or
// read as "$0.00", as a cheap model's per-message cost would. The cutoff is
// where four decimals would round up to "$0.0100", so 0.009996 prints
// "$0.01". Zero prints "$0.00". From $1,000 up it takes render.Cost's
// commas.
func statusCost(cost float64) string {
	if cost != 0 && math.Abs(cost) < 0.00995 {
		return fmt.Sprintf("$%.4f", cost)
	}
	if math.Abs(cost) >= 0.99995 {
		return render.Cost(cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}
