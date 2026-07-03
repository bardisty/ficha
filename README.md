# ccusage

```
╔══════════════════════════════════════════════════════════════════════╗
║                              ccusage                                 ║
║           Track your Claude Code API costs in real-time              ║
╚══════════════════════════════════════════════════════════════════════╝

─── QUICKSTART ────────────────────────────────────────────────────────

  Install:    go install github.com/bardisty/ccusage@latest

  Usage:      ccusage              View current session costs
              ccusage watch        Live monitoring (auto-follows)

─── COMMANDS ──────────────────────────────────────────────────────────

  Command              Description
  ───────────────────  ───────────────────────────────────────────────
  ccusage [show]       Show latest (or specified) session costs
  ccusage watch        Live monitoring with auto-follow
  ccusage breakdown    Per-message cost table (scrollable)
  ccusage list         List all sessions
  ccusage summary      Total costs across all sessions
  ccusage global       Aggregated stats across ALL projects
  ccusage version      Print version information

─── FLAGS ─────────────────────────────────────────────────────────────

  -f, --format <fmt>   Output format: table, json, csv
  -p, --project <dir>  Project directory (default: current dir)
  --project-dir <name> Claude project dir name (bypass auto-detect)
  -v, --verbose        Show debug information
  -l, --live           Enable live mode (auto-updates)
  --no-follow          Disable auto-follow in live mode
  --no-color           Disable colored output

─── COMMAND FLAGS ─────────────────────────────────────────────────────

  summary -d, --details    Add a per-session breakdown
  summary --expand-agents  Per-agent records (requires --details)
  global  -n, --top <n>    Top N projects in table (default 10)
  global  --sort-by <key>  Sort: cost, sessions, name, activity
  global  -d, --details    All projects + cumulative column (table)
  show    --messages       Per-message rows (json/csv only)

─── EXAMPLES ──────────────────────────────────────────────────────────

  ccusage show abc123           View session by partial ID
  ccusage watch abc123          Watch specific session (pinned)
  ccusage -f json > out.json    Export to JSON
  ccusage summary -d -f csv     Per-session rows as CSV
  ccusage global --top 5        Top 5 projects by cost
  ccusage show -f csv --messages Per-message rows as CSV
  ccusage list -p /path/to/dir  List sessions for different project

─── MACHINE OUTPUT (json / csv) ───────────────────────────────────────

  -f only picks the encoding; json/csv always export the complete
  dataset as a single object / uniform-column table (safe for jq and
  pandas). One rule decides how flags interact with them:

    Flags that ADD records apply to json/csv:
      summary --details          per-session records
      summary --expand-agents    per-agent records (nested / rows)
      show --messages            per-message rows, not the summary
    Flags that only shape the table do NOT change json/csv:
      global --top / --details   json/csv always list every project;
                                 subset downstream with jq / head

  Sessions that fail to parse are omitted from detail output and
  reported on stderr (stdout stays clean for piping).

─── HOW IT WORKS ──────────────────────────────────────────────────────

  Reads session files from ~/.claude/projects/ and calculates costs
  using Anthropic's pricing. Tracks prompt caching savings (cache
  reads cost 90% less than regular input tokens).
```
