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
  ccusage              Show latest session costs
  ccusage watch        Live monitoring with auto-follow
  ccusage breakdown    Per-message cost table (scrollable)
  ccusage list         List all sessions
  ccusage summary      Total costs across all sessions

─── FLAGS ─────────────────────────────────────────────────────────────

  -f, --format <fmt>   Output format: table, json, csv
  -p, --project <dir>  Specify project directory
  -l, --live           Enable live mode (auto-updates)
  --no-follow          Disable auto-follow in live mode
  --no-color           Disable colored output

─── EXAMPLES ──────────────────────────────────────────────────────────

  ccusage show abc123           View session by partial ID
  ccusage watch abc123          Watch specific session (pinned)
  ccusage -f json > out.json    Export to JSON
  ccusage list -p /path/to/dir  List sessions for different project

─── HOW IT WORKS ──────────────────────────────────────────────────────

  Reads session files from ~/.claude/projects/ and calculates costs
  using Anthropic's pricing. Tracks prompt caching savings (cache
  reads cost 90% less than regular input tokens).
```
