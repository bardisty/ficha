---
name: audit-codebase
description: Reusable JSON-emitting codebase AUDIT (no fixes). Runs the audit-codebase workflow — Opus map, Fable reviewers, tiered cross-model refutation (Fable lenses + gpt-5.6-sol codex votes), Opus batch-verify, Fable synthesis — and writes audit.json + summary.json to .audits/. Use when asked to "audit", "review the codebase for <dimension>", or produce an audit reference for later fix planning. Args: dimension and/or area, e.g. "correctness parser" or "architecture".
---

# audit-codebase

Audit-only. Produces a JSON reference for LATER planning. NEVER edits code, writes to BACKLOG.md or progress docs, or runs the branch/PR flow — that is the planner's job downstream (`/plan-from-audit`).

The heavy lifting lives in the **`audit-codebase` workflow** (`.claude/workflows/audit-codebase.js`). This skill scouts scope, invokes it, and writes the returned JSON to disk byte-faithfully.

## Phase 0 — Scope
Resolve from args or AskUserQuestion:
- **dimension**: correctness | performance | security | architecture | data-integrity
- **area**: full | cmd | parser | analyzer | pricing | formatter | render | tui | models (default full)

## Phase 1 — Scout (cheap, in this session)
- **Date**: `date +%F`.
- **Commit**: `git rev-parse --short HEAD` (repo root `/home/bah/source/claude-code-usage`).
- **Skip-list** (the workflow does NOT re-scan prior reports — this scout is the sole source):
  - Glob `.audits/*/summary.json`; from each, read `severityIndex[].{id,title}` + `refuted[].{id,title}`.
  - Read the legacy `.audits/2026-07-02-codebase-audit.json` (pre-pair schema): harvest `findings[].{id,title}` — all 49 were shipped or explicitly backlogged in the 2026-07 remediation, so none may be re-reported.
  - Read `.audits/BACKLOG.md`: harvest ID-prefixed entry titles — deferred/declined work must not be re-reported.
  - Pass the union as `skipList` (array of `"ID: title"` strings).
- **Output dir**: `.audits/{date}-{dimension}-{areaSlug}` (areaSlug = area with any non-alnum → `-`).

## Phase 2 — Run the workflow
Call the **Workflow** tool:
- `name`: `"audit-codebase"`
- `args`: `{ dimension, area, dateSlug, gitCommit, skipList, repoRoot: "/home/bah/source/claude-code-usage" }`

It returns `{ audit, summary }`. (Runs in the background; you are notified on completion. Watch live progress with `/workflows`.)

## Phase 3 — Write the report
The completion notification truncates — use the task's output file. It is an ENVELOPE: the workflow's return value sits under `.result`, so extract `.result.audit` / `.result.summary`. Create the output dir, then write byte-faithfully via a node one-liner (do NOT let an agent re-serialize the JSON):
- `{outputDir}/audit.json`   = `JSON.stringify(envelope.result.audit, null, 2)`
- `{outputDir}/summary.json` = `JSON.stringify(envelope.result.summary, null, 2)`

## Phase 4 — Relay
Terse summary to the user:
- Counts: actionable by severity (`summary.counts.bySeverity` — actionable only) + `counts.unverified` + `counts.refuted`, then top CRITICAL/HIGH titles.
- **Coverage gaps**: any `coverage.unitsFailed` (a failed reviewer means that subsystem was NOT audited — surface it, never imply it was clean).
- **Verification value-add**: `verificationStats` — how many findings were refuted/downgraded (this is why the audit is trustworthy).
- `meta.complementaryManualReviews` (e.g. for the security dimension: deps/CI/gosec-class scanning is out of scope of the fan-out).
- State the `audit.json` path.
- Do NOT propose or apply fixes — note that the report feeds a separate planning step (`/plan-from-audit`).

## Notes
- **Cost / model tiering** (schema 2.0, per the global CLAUDE.md model table): Opus map (discovery) → Fable reviewers (the hard judgment) → tiered cross-model refutation — CRITICAL = 2 diverse-lens Fable (guard-hunter + reachability-tracer) + 1 gpt-5.6-sol codex vote at xhigh reasoning (free Claude tokens); HIGH = 1 Fable + 1 gpt-5.6-sol at high; impactful-MEDIUM = 1 Fable; remaining MEDIUM/LOW + refuter-surfaced = Opus **batch verifier** (~5 findings/agent) — so nearly everything leaves the audit verified. Scope tightly (a specific `area`) for cheap runs; use `full` only when warranted. The `maxUnits` arg (default 6, clamp 3–10) bounds fan-out.
- **Codex dependency**: gpt-5.6-sol votes shell out to the `codex` CLI (`codex exec -m gpt-5.6-sol -c model_reasoning_effort=xhigh|high -s read-only`). If codex is unavailable, those votes ABSTAIN and the panel degrades gracefully to Claude-only — an ABSTAIN never lowers the bar to refute a finding.
- **Audit-only by design**: no doc writes, no code changes. `audit.json` is the input to a later planning step.
- To plan/sequence fixes from the report, run **`/plan-from-audit`** (the planning half of the pair — sequences the findings into sessions with a kickoff + progress doc).
- **Output schema** (`audit.json`, schemaVersion 2.0): `meta`, `coverage`, `verificationStats` (incl. `batchVerified`), `overallAssessment`, `clusters[]`, `severityIndex[]` (REFUTED excluded; each entry carries `tier`: panel/batch/none), `findings[]` (actionable: CONFIRMED/ADJUSTED/DOWNGRADED with `verificationTier` panel|batch, each with a verbatim `locator.snippet` that survives line drift), `unverifiedFindings[]` (verification-agent failures only — rare), `refuted[]` (kept with reasons so a planner won't re-raise them), `verifiedClean[]`.
