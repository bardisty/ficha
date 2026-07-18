# ficha

```
╔══════════════════════════════════════════════════════════════════════╗
║                                ficha                                 ║
║           Track your Claude Code API costs in real-time              ║
╚══════════════════════════════════════════════════════════════════════╝

─── QUICKSTART ────────────────────────────────────────────────────────

  Install:    go install github.com/bardisty/ficha@latest

  Usage:      ficha              View current session costs
              ficha watch        Live monitoring (auto-follows)

─── COMMANDS ──────────────────────────────────────────────────────────

  Command              Description
  ───────────────────  ───────────────────────────────────────────────
  ficha [show]       Show latest (or specified) session costs
  ficha watch        Live monitoring with auto-follow
  ficha breakdown    Per-message cost table (scrollable)
  ficha list         List all sessions
  ficha summary      Total costs across all sessions
  ficha global       Aggregated stats across ALL projects
  ficha version      Print version information

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

  ficha show abc123           View session by partial ID
  ficha watch abc123          Watch specific session (pinned)
  ficha -f json > out.json    Export to JSON
  ficha summary -d -f csv     Per-session rows as CSV
  ficha global --top 5        Top 5 projects by cost
  ficha show -f csv --messages Per-message rows as CSV
  ficha list -p /path/to/dir  List sessions for different project

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

  Two fields name where a record came from. project_path is the Claude
  project directory (~/.claude/projects/<encoded>) — identical on show,
  the summary aggregate, and every per-session record, so machine
  outputs join on it. session_file is the transcript .jsonl path; it is
  empty on the summary aggregate, which spans many files. In csv,
  session rows carry both columns and agent rows leave them empty (join
  back through session_id). (list names the same transcript path fullPath /
  full_path, and its projectPath is the original working directory, not this
  encoded project dir.)

  Every input ficha could not read is counted, never swallowed. Three
  counters travel together on json (omitted when zero) and csv, and each
  one also prints a stderr warning:

    skipped_sessions   session files that failed to parse
    skipped_agents     agent sub-sessions that could not be read —
                       an unparseable transcript, or a whole subagents/
                       or workflow-run directory that could not be
                       listed (there the count is a lower bound)
    skipped_lines      JSONL lines rejected as malformed or oversized

  Costs that could not be computed exactly are counted the same way. A
  message whose cache-write tokens carry no TTL attribution (sessions
  from before the cache_creation breakdown existed) is priced at the 5m
  write rate — the cheapest tier, so its cost is a lower-bound estimate.
  estimated_cost_messages counts them (json omitted when zero, csv
  column, stderr warning); zero means every cost is exact.

  Token usage is exported in one reconciled form: whenever write tokens
  exist, usage carries a cache_creation object whose 5m + 1h buckets sum
  exactly to cache_creation_input_tokens, and negative counts from
  corrupt lines are clamped to zero before costs and totals alike — the
  tokens you see are always the tokens that were billed.

  Counts describe only what the totals cover. session_count is the
  sessions that were analyzed, agent_count the agents that were: add
  the matching skipped_* to recover what was on disk. Every surface
  carries session_count: global at both levels, the summary aggregate
  (the analyzed count its totals span), and show (always 1 — it
  analyzed exactly one session). list and show agree on message_count
  and agent_count for the same session, because both derive them from
  the same parse.

  Agent spend is never hidden. show and summary both carry the split
  that adds up to the total they report:

    parent_cost + agents_cost = total_cost

  On the summary aggregate that split spans every session, alongside
  agent_count, workflow_count, has_agents and parent_cost_by_model; the
  per-agent records themselves live under summary --details. (global
  reports project totals only, with no parent/agent split.)

  first_active / last_active are message timestamps on every surface, so
  a project's span in global matches its span in summary. Only a project
  whose messages carry no timestamp at all falls back to file mtimes.

  show --messages likewise emits agent rows, not just the parent
  transcript, so the rows sum to the session's total_cost. An agent_id
  column (json: agent_id, omitted on parent records) says where each row
  came from:

    agent_id  timestamp             model            total_cost
              2026-02-02T09:00:00Z  claude-opus-4-8    0.031500   ─┐
              2026-02-02T09:10:00Z  claude-opus-4-8    0.004200    ├─ parent
    g1        2026-02-02T09:05:00Z  claude-sonnet-5    0.012000   ─┤
    w1        2026-02-02T09:07:00Z  claude-sonnet-5    0.009000   ─┘ agents

  Parent rows come first, then one block per agent in discovery order
  (regular subagents, then workflow runs). Sort by timestamp for a
  chronological view — or use `ficha breakdown`, which does it for you.

  One agent_id key space throughout: the same ID joins these rows, the
  json agents[] array, and summary --details --expand-agents rows.

  The accounting counters ride along in --messages csv too: each row
  ends with the session-level skipped_agents, skipped_lines and
  estimated_cost_messages, repeated verbatim (denormalized, like any
  flat export of a parent/child shape) — so a consumer summing rows can
  tell when the rows are incomplete or estimated without leaving the
  file. json --messages carries the same counters once, on the session
  object enclosing the messages array.

  Costs are floats, and csv prints six decimals. Reconcile a sum against
  a total with a tolerance, never with ==.

  A cell that would open as a spreadsheet formula (=, +, -, @) is
  prefixed with an apostrophe in csv. No real model or agent ID starts
  with one, so this only ever fires on a hostile transcript.

  cost_by_model keys are canonical model IDs: every dated snapshot and
  provider spelling of one model shares a key, so summing by key needs
  no normalization on your side.

    claude-sonnet-4-5-20250929  ─┐
    claude-sonnet-4-5@20251119   ├─▶  claude-sonnet-4-5
    ...claude-sonnet-4-5-v1:0   ─┘

  A model ficha cannot price keeps its raw ID as the key, and is named
  on stderr, so nothing unpriced is silently folded into a priced row.
  Per-message rows (show --messages) keep the raw ID either way.

─── HOW IT WORKS ──────────────────────────────────────────────────────

  Reads session files from ~/.claude/projects/ and calculates costs
  using Anthropic's pricing. Tracks prompt caching savings (cache
  reads cost 90% less than regular input tokens).

  Agent sub-sessions are included: both regular subagents and Claude
  Code Workflow agents (grouped by workflow run, with the run's name
  and status from its metadata).
```
