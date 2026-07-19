# Machine output (json / csv)

`-f` only picks the encoding; json/csv always export the complete dataset as a single object / uniform-column table (safe for `jq` and pandas).

## How flags interact with json/csv

One rule decides it:

**Flags that ADD records apply to json/csv:**

| Flag | Effect |
| --- | --- |
| `summary --details` | per-session records |
| `summary --expand-agents` | per-agent records (nested / rows) |
| `show --messages` | per-message rows, not the summary |

**Flags that only shape the table do NOT change json/csv:**

| Flag | Effect |
| --- | --- |
| `global --top` / `--details` | json/csv always list every project; subset downstream with `jq` / `head` |

Sessions that fail to parse are omitted from detail output and reported on stderr (stdout stays clean for piping).

## Record provenance

Two fields name where a record came from. `project_path` is the Claude project directory (`~/.claude/projects/<encoded>`) — identical on `show`, the `summary` aggregate, and every per-session record, so machine outputs join on it. `session_file` is the transcript `.jsonl` path; it is empty on the `summary` aggregate, which spans many files. In csv, session rows carry both columns and agent rows leave them empty (join back through `session_id`). (`list` names the same transcript path `fullPath` / `full_path`, and its `projectPath` is the original working directory, not this encoded project dir.)

## Skip counters

Every input ficha could not read is counted, never swallowed. Three counters travel together on json (omitted when zero) and csv, and each one also prints a stderr warning:

| Counter | Counts |
| --- | --- |
| `skipped_sessions` | session files that failed to parse |
| `skipped_agents` | agent sub-sessions that could not be read — an unparseable transcript, or a whole `subagents/` or workflow-run directory that could not be listed (there the count is a lower bound) |
| `skipped_lines` | JSONL lines rejected as malformed or oversized |

## Estimated costs

Costs that could not be computed exactly are counted the same way. A message whose cache-write tokens carry no TTL attribution (sessions from before the `cache_creation` breakdown existed) is priced at the 5m write rate — the cheapest tier, so its cost is a lower-bound estimate. `estimated_cost_messages` counts them (json omitted when zero, csv column, stderr warning); zero means every cost is exact.

## Reconciled token usage

Token usage is exported in one reconciled form: whenever write tokens exist, `usage` carries a `cache_creation` object whose 5m + 1h buckets sum exactly to `cache_creation_input_tokens`, and negative counts from corrupt lines are clamped to zero before costs and totals alike — the tokens you see are always the tokens that were billed.

## Coverage counts

Counts describe only what the totals cover. `session_count` is the sessions that were analyzed, `agent_count` the agents that were: add the matching `skipped_*` to recover what was on disk. The analyzed count is everywhere: `session_count` on `global` (both levels), the `summary` aggregate (the count its totals span), and `show` (always 1 — it analyzed exactly one session); `global` csv's per-project column names it `sessions`. `list` and `show` agree on `message_count` and `agent_count` for the same session, because both derive them from the same parse.

## Parent/agent split

Agent spend is never hidden. `show` and `summary` both carry the split that adds up to the total they report:

```
parent_cost + agents_cost = total_cost
```

On the `summary` aggregate that split spans every session, alongside `agent_count`, `workflow_count`, `has_agents` and `parent_cost_by_model`; the per-agent records themselves live under `summary --details`. (`global` reports project totals only, with no parent/agent split.)

## Timestamps

`first_active` / `last_active` are message timestamps on every surface, so a project's span in `global` matches its span in `summary`. Only a project whose messages carry no timestamp at all falls back to file mtimes.

## Per-message rows (`show --messages`)

`show --messages` likewise emits agent rows, not just the parent transcript, so the rows sum to the session's `total_cost`. An `agent_id` column (json: `agent_id`, omitted on parent records) says where each row came from:

```
agent_id  timestamp             model            total_cost
          2026-02-02T09:00:00Z  claude-opus-4-8    0.031500   ─┐
          2026-02-02T09:10:00Z  claude-opus-4-8    0.004200    ├─ parent
g1        2026-02-02T09:05:00Z  claude-sonnet-5    0.012000   ─┤
w1        2026-02-02T09:07:00Z  claude-sonnet-5    0.009000   ─┘ agents
```

Parent rows come first, then one block per agent in discovery order (regular subagents, then workflow runs). Sort by timestamp for a chronological view — or use `ficha breakdown`, which does it for you.

One `agent_id` key space throughout: the same ID joins these rows, the json `agents[]` array, and `summary --details --expand-agents` rows.

The accounting counters ride along in `--messages` csv too: each row ends with the session-level `skipped_agents`, `skipped_lines` and `estimated_cost_messages`, repeated verbatim (denormalized, like any flat export of a parent/child shape) — so a consumer summing rows can tell when the rows are incomplete or estimated without leaving the file. json `--messages` carries the same counters once, on the session object enclosing the messages array.

## Floats and csv safety

Costs are floats, and csv prints six decimals. Reconcile a sum against a total with a tolerance, never with `==`.

A cell that would open as a spreadsheet formula (`=`, `+`, `-`, `@`) is prefixed with an apostrophe in csv. No real model or agent ID starts with one, so this only ever fires on a hostile transcript.

## Canonical model IDs

`cost_by_model` keys are canonical model IDs: every dated snapshot and provider spelling of one model shares a key, so summing by key needs no normalization on your side.

```
claude-sonnet-4-5-20250929  ─┐
claude-sonnet-4-5@20251119   ├─▶  claude-sonnet-4-5
...claude-sonnet-4-5-v1:0   ─┘
```

A model ficha cannot price keeps its raw ID as the key, and is named on stderr, so nothing unpriced is silently folded into a priced row. Per-message rows (`show --messages`) keep the raw ID either way.
