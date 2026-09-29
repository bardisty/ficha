# Machine output (json / csv)

`-f` only picks the encoding. json is one document per run, and csv one table with the same columns on every row, safe for `jq` and pandas. Neither is cut down by table-only flags like `global --top`. csv is the flat view, so it leaves out the nested detail json has. [What csv leaves out](#what-csv-leaves-out) lists it.

Every json key is snake_case on every command, and csv columns use the same names. Model IDs under `cost_by_model` and `parent_cost_by_model` are map keys, not field names, and keep their own spelling.

## Compatibility

Until 1.0, a minor release may rename, remove or retype a json key or csv column. Its release notes list each such change at the top, with the old and new jq path. New keys and columns can arrive in any release, so read by name, not by position, and ignore keys you don't know.

A test pins every command's json keys and csv header, so a change to either turns up in review as a diff instead of reaching your script as a silent `null`.

To keep a script working across upgrades, pin the version you tested it against:

```sh
go install github.com/bardisty/ficha@vX.Y.Z
```

## Shapes

| Command | json | csv |
| --- | --- | --- |
| `show` | a session object | one row for the session |
| `show --messages` | the same session object, plus a `messages` array | one row per message |
| `list` | an array, one record per session | one row per session |
| `summary` | the aggregate: a session object whose `session_id` is `"aggregate"` | one row for the aggregate |
| `summary -d` | `{"summary": ..., "sessions": [...]}`: the aggregate moves under `.summary`, and `.sessions[]` has one record per session | one `session` row per session |
| `summary -d --expand-agents` | as `summary -d`, and each session gains `agents[]` | each `session` row followed by an `agent` row per agent |
| `global` | an object: totals at the top level, one record per project in `.projects[]` | one row per project, and no totals row |

A few keys only make sense in one place:

- The `summary` aggregate has `session_id: "aggregate"`, and no `session_file`, `title`, `insights` or `context`, since it spans many sessions.
- `global` always has `skipped_projects`, even at zero: the project directories it couldn't analyze. The other skip counters are left out at zero.
- `summary -d` csv has `row_type`, `session` or `agent`. See [Per-session rows](#per-session-rows-summary--d-csv).

## How flags interact with json/csv

One rule decides it:

**Flags that ADD records apply to json/csv:**

| Flag | Effect |
| --- | --- |
| `summary --details` | per-session records |
| `summary --expand-agents` | per-agent records (nested / rows) |
| `show --messages` | per-message records: json keeps the session object and adds `messages`, csv switches to one row per message |

**Flags that only shape the table do NOT change json/csv:**

| Flag | Effect |
| --- | --- |
| `global --top` / `--details` | json/csv always list every project; subset downstream with `jq` / `head` |

Sessions that fail to parse are omitted from detail output and reported on stderr (stdout stays clean for piping).

## Record provenance

Two fields name where a record came from. `project_path` is the Claude project directory (`~/.claude/projects/<encoded>`) — identical on `show`, the `summary` aggregate, every per-session record and every `list` record, so machine outputs join on it. `session_file` is the transcript `.jsonl` path; it is empty on the `summary` aggregate, which spans many files. In csv, session rows carry both columns and agent rows leave them empty (join back through `session_id`). (`list` names the transcript path `full_path`, and adds `original_path`, the working directory the project stands for, empty when no transcript records it. `global` names each project by `encoded_path`, `full_path`, `original_path` and `display_name`.)

## Agent order

`agents` lists plain agents first, then each workflow run's agents together. Each group goes by first message, and runs go by their first agent's start. An agent with no timestamped message yet comes last in its group. `workflows` lists runs in the same order. The tables and `ficha watch` list agents this way too. The order is for reading and isn't part of the json contract. A script that needs a fixed order should sort by `start_time` or `agent_id` itself.

## Per-session rows (`summary -d` csv)

`summary -d -f csv` writes one row per session. `row_type` says which kind of row it is: `session`, or `agent` for the rows `--expand-agents` adds after each session.

A session row's `total_cost` already includes its agents, so summing the whole column counts agent spend twice. Filter on `row_type` first:

- Sum `session` rows for the project's totals. They match `summary -f json`'s `total_cost.total_cost`.
- Sum `agent` rows for agent spend.

Each session row also carries `parent_cost` and `agents_cost`, which add up to its `total_cost`, so the file reconciles against itself. Agent rows leave both empty, as they leave `cumulative_cost`, `project_path` and `session_file`. Join an agent row to its session through `session_id`.

## Session list (`list`)

`list -f json` is an array with one record per session, newest first, and `list -f csv` has a row per session with the same fields. Besides the discovery fields (`session_id`, `full_path`, `modified`, the counts and the skip counters), each record carries what the table shows: `title`, `model`, `start_time`, `duration_seconds` and `total_cost`.

- `total_cost` is the same breakdown object as on `show` and `summary`, so `.total_cost.total_cost` reads the same on all three. csv has only the total.
- `model`, `start_time`, `duration_seconds` and `total_cost` come from the same analysis as the session's `summary -d` record and the list table. That analysis counts a message repeated across sessions once, under the session it first appeared in. So for a resumed or forked session, which starts with a copy of earlier history, these fields describe what it added: `start_time` is its first new message, and its `total_cost` can be lower than `show`'s, which analyzes the session alone.
- `title` is the transcript's latest `ai-title` record, as on `show`. A fork that hasn't been retitled yet carries the original's title.
- `message_count` and `agent_count` come from the same parse as `show`'s, so the two agree, repeated history included.
- `model` is the session's costliest model in the parent transcript, spelled as a `cost_by_model` key, like `summary -d` csv's `model`.
- A session that fails to parse is still listed, with `skipped_sessions: 1`. It has no `title`, `model`, `start_time`, `duration_seconds` or `total_cost`, and csv leaves those cells empty. A session that holds nothing but repeated history parses fine, so it has `total_cost` (zero) and `duration_seconds`, but no `start_time` or `model`.

## Context window

`context` on `show` json and on each per-session record in `summary -d` json is the reading the table's Context gauge shows: how full the context window was at the session's last parent request.

| Key | Meaning |
| --- | --- |
| `tokens` | input tokens of that request, cache reads and writes included, as Claude Code's `/context` counts them |
| `window` | the model's context window, from ficha's price list |
| `percent` | `tokens / window * 100`, unrounded. The table rounds it |

It's absent when the session has no parent request, and on the `summary` aggregate, which spans many sessions. A `summary -d` session made only of history repeated from an earlier session has no request of its own, so it has no `context` there, though `show` gives it one.

## Workflow runs

Each entry in `workflows` (on `show` json and each per-session record in `summary -d` json) names a workflow run whose agents appear in `agents`, with `cost`: the sum of those agents' `total_cost.total_cost`. It's derived from the agent records, which remain the unit every total is summed from, so don't add it to `agents_cost` or `total_cost` again.

## Session titles

`title` on `show` json and on each per-session record in `summary -d` json is the session's latest `ai-title` record, as Claude Code wrote it. Claude Code rewrites the title as a session goes on, and the key is undocumented, so `title` is absent when a transcript has none. It's raw transcript text: escape it before you print it to a terminal.

## Cost insights

`insights` on `show` json and on each per-session record in `summary -d` json describes the parent transcript's messages. Agents' messages aren't in it.

| Key | Meaning |
| --- | --- |
| `message_count` | parent messages the insights cover |
| `average_cost` | mean cost per message |
| `first_message`, `last_message` | a snapshot: `index` (1-based), `timestamp`, `cost`, and `main_cost_component` (`input`, `output`, `cache_write_5m`, `cache_write_1h` or `cache_read`) with its `main_cost_value` |
| `highest_cost` | the same snapshot for the costliest message, present only when it costs more than 1.5 times the average |
| `cost_trend` | `increasing`, `decreasing` or `stable`: `recent_avg_cost` against `average_cost`, with more than 20% either way counting as a change |
| `recent_avg_cost` | mean cost of the last `trend_window` messages |
| `trend_window` | how many recent messages the trend averages: 20, or half the session if that's fewer |

A session with fewer than 6 parent messages has no trend, so `cost_trend`, `recent_avg_cost` and `trend_window` are absent. Check for `cost_trend` before reading the other two.

## Skip counters

Every input ficha could not read is counted, never swallowed. Three counters travel together on json (omitted when zero) and csv, and each one also prints a stderr warning:

| Counter | Counts |
| --- | --- |
| `skipped_sessions` | session files that failed to parse |
| `skipped_agents` | agent sub-sessions that could not be read — an unparseable transcript, or a whole `subagents/` or workflow-run directory that could not be listed (there the count is a lower bound) |
| `skipped_lines` | JSONL lines rejected as malformed or oversized |

## Estimated costs

Costs that could not be computed exactly are counted the same way. A message whose cache-write tokens carry no TTL attribution (sessions from before the `cache_creation` breakdown existed) is priced at the 5m write rate — the cheapest tier, so its cost is a lower-bound estimate. `estimated_cost_messages` counts them (json omitted when zero, csv column, stderr warning). Zero means no cache write was estimated. It doesn't cover models ficha has no price for. Those are in `unpriced_models`.

## Unpriced models

A model missing from ficha's price list is priced at a fallback rate, which the stderr warning states, so every cost that includes it is a guess. `unpriced_models` names those models, sorted, spelled exactly as their `cost_by_model` keys so you can look up how much of the total each one accounts for. json leaves it out when every model is priced. The stderr warning names the same models.

It's on every record that has a `cost_by_model`: `show` and each of its `agents`, the `summary` aggregate, each `summary -d` session and its agents, the `global` object and each of its projects. A session's list covers its agents' models too.

csv has an `unpriced_models` column on `show`, `summary`, `summary -d` rows (session and agent) and `global` project rows. Each cell is a json array, `[]` when every model is priced, because an unpriced ID is raw transcript text and can hold anything, a comma or a space included. Read it with `json.loads` or `jq`'s `fromjson`. `show --messages` rows don't carry it. They name each message's model, and the session's `unpriced_models` in `show -f csv` or json says which of those are unpriced.

## Reconciled token usage

Token usage is exported in one reconciled form: whenever write tokens exist, `usage` carries a `cache_creation` object whose 5m + 1h buckets sum exactly to `cache_creation_input_tokens`, and negative counts from corrupt lines are clamped to zero before costs and totals alike — the tokens you see are always the tokens that were billed.

## Coverage counts

Counts describe only what the totals cover. `session_count` is the sessions that were analyzed, `agent_count` the agents that were: add the matching `skipped_*` to recover what was on disk. The analyzed count is everywhere: `session_count` on `global` (both levels), the `summary` aggregate (the count its totals span), and `show` (always 1 — it analyzed exactly one session); `global` csv's per-project column names it `sessions`. `list` and `show` agree on `message_count` and `agent_count` for the same session, because both derive them from the same parse.

## Parent/agent split

Agent spend is never hidden. `show` and `summary` both carry the split that adds up to the total they report. `parent_cost`, `agents_cost` and `total_cost` are objects, so compare their `total_cost` fields:

```sh
jq '.parent_cost.total_cost + .agents_cost.total_cost == .total_cost.total_cost'
```

The sum can be off in the last float digit, so a script should compare with a tolerance: `((.parent_cost.total_cost + .agents_cost.total_cost - .total_cost.total_cost) | fabs) < 1e-9`. Adding the objects themselves (`.parent_cost + .agents_cost`) merges them in jq instead of adding them. The other costs, `workflows[].cost` and the costs under `insights`, are plain numbers.

On the `summary` aggregate that split spans every session, alongside `agent_count`, `workflow_count`, `has_agents` and `parent_cost_by_model`; the per-agent records themselves live under `summary --details`. (`global` reports project totals only, with no parent/agent split.)

## Durations

`duration` is a Go duration string (`"3h12m5s"`) for reading. `duration_seconds` beside it, on sessions, agents and the `global` totals, is the same span as a number, for arithmetic. Where csv has a duration, it's `duration_seconds`. Both measure first message to last, not time spent.

## Time windows (`--since`, `--until`)

On `summary` and `global`, `--since` and `--until` limit every format to the messages timestamped inside the window, parent and agents alike, so a session that crosses a bound counts only its part inside. Sessions, agents and projects with nothing inside drop out of the records and the counts. A message with no timestamp can't be placed, so a window leaves it out. json names the window as `window: {since, until}` (timestamps as below, either one absent when open) on the `summary` aggregate and the `global` object; csv doesn't carry it.

With `--since`, ficha doesn't read a session whose transcript and agent files were all last written more than a day before that bound, because none of their messages can fall inside it. The skip counters then leave out any malformed lines in those files. A file ficha can't open, or a directory it can't list, still counts.

## Timestamps

Every timestamp in json and csv is UTC to the second, with a `Z`: `2026-09-29T17:18:43Z`. That covers message timestamps, `start_time` and `end_time`, `first_active` and `last_active`, list's `modified`, and `window.since` and `until`, whatever your local time zone. jq's `fromdate` reads the format. Relative `--since` bounds such as `2h` lose their fraction of a second.

Whole seconds mean `end_time` minus `start_time` can differ from `duration_seconds` by up to a second, and two messages in the same second get the same timestamp in `--messages` output. Rows keep ficha's order, so don't re-sort on `timestamp` alone if order within a second matters.

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

Parent rows come first, then one block per agent, in the order of `agents`. Sort by timestamp for a chronological view — or use `ficha breakdown`, which does it for you.

One `agent_id` key space throughout: the same ID joins these rows, the json `agents[]` array, and `summary --details --expand-agents` rows.

The accounting counters ride along in `--messages` csv too: each row ends with the session-level `skipped_agents`, `skipped_lines` and `estimated_cost_messages`, repeated verbatim (denormalized, like any flat export of a parent/child shape) — so a consumer summing rows can tell when the rows are incomplete or estimated without leaving the file. json `--messages` carries the same counters once, on the session object enclosing the messages array.

## What csv leaves out

csv is one flat table per command, so anything nested stays json-only:

- `show` and `summary`: the per-model split (`cost_by_model`, `parent_cost_by_model`), token counts, `insights`, `context`, `agents`, `workflows`, `title`, start and end times, and the window.
- `summary -d`: the same, except that each row names its costliest `model`, and `--expand-agents` adds agent rows.
- `global`: the totals row, the per-model split and token counts, and `original_path`. Projects are named by `project` (the display name), `encoded_path` and `full_path`. `full_path` is the same value as `summary`'s and `show`'s `project_path`, so join on it.
- `list`: `agent_paths`, and `total_cost` is the total only, not the breakdown.
- `show --messages`: the session summary. The rows carry the session's skip and estimate counters, but not its totals.

Use json when you need any of these.

## Floats and csv safety

Costs are floats, and csv prints six decimals. Reconcile a sum against a total with a tolerance, never with `==`.

A cell that would open as a spreadsheet formula (`=`, `+`, `-`, `@`) is prefixed with an apostrophe in csv. No real model or agent ID starts with one. `global`'s `encoded_path` does on macOS and Linux, where encoded names start with `-`, so it reads `'-home-you-work-webapp` in csv. On Windows it starts with the drive letter (`C--Users-you-work-webapp`) and gets no apostrophe. Strip a leading apostrophe only when there is one before comparing it with json, or join on `full_path` instead.

## Canonical model IDs

`cost_by_model` keys are canonical model IDs: every dated snapshot and provider spelling of one model shares a key, so summing by key needs no normalization on your side.

```
claude-sonnet-4-5-20250929  ─┐
claude-sonnet-4-5@20251119   ├─▶  claude-sonnet-4-5
...claude-sonnet-4-5-v1:0   ─┘
```

A model ficha cannot price keeps its raw ID as the key, and is named in `unpriced_models` and on stderr, so nothing unpriced is silently folded into a priced row. Per-message rows (`show --messages`) keep the raw ID either way.

`<synthetic>` is the model Claude Code writes on lines it records itself, such as API errors. Those lines cost nothing, so they have no `cost_by_model` or `parent_cost_by_model` key and no stderr warning. They still count in `message_count`, the same in `list` and `show`, and per-message rows keep them with `model: "<synthetic>"` and zero cost.
