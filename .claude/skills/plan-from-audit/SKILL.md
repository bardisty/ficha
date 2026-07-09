---
name: plan-from-audit
description: Turn an audit-codebase audit.json into a verified, sequenced fix plan + per-session kickoff wired to this repo's branch/PR flow. Report-and-plan only — it sequences the audit's already-verified findings into implementation sessions and writes _action-plan.md/_kickoff.md/_progress.md into the audit dir (+ appends deferred items to .audits/BACKLOG.md); it applies code only via the sessions it kicks off. Use when asked to "plan fixes from the audit", "turn the audit into tasks", "sequence the findings", "what do I fix first", or after running /audit-codebase. Args: a path to an audit.json, or a dimension/area to locate the newest one (e.g. "correctness" or "data-integrity parser").
---

# plan-from-audit

The planning half of the **/audit-codebase → /plan-from-audit** pair. Consumes the verified findings from `/audit-codebase` and turns them into a sequenced action plan + kickoff/progress docs wired to this repo's dev flow. It does NOT re-review the codebase — the audit workflow already did the map / review / tiered adversarial-verify fan-out (cross-model panels + batch tier). (No audit yet? Run `/audit-codebase <dimension> <area>` first.)

**Pure in-session skill — no Workflow fan-out.** Sequencing is judgment, and the audit already paid for the expensive verification. Re-verifying each finding against current code happens at *implementation* time (the kickoff contract), not here. Judgment-heavy → run this in a **Fable session** (per the global CLAUDE.md model table).

## Phase 0 — Locate the audit + load conventions
- **Input**: an `audit.json` under `.audits/{date}-{dimension}-{area}/`. From args: take an explicit path, or Glob `.audits/*/audit.json` and pick the newest dir matching the requested dimension/area. **Read the sibling `summary.json` FIRST** (orientation: counts, severityIndex, clusterTitles, verificationStats), then pull from `audit.json` what planning needs: `findings[]` (actionable) · `unverifiedFindings[]` · `clusters[]` · `overallAssessment` · `verifiedClean[]` · `refuted[]` — don't dump the whole file into context when the index suffices. These are already verified — do NOT re-litigate `refuted[]` (kept with reasons precisely so you don't re-raise them; some are `tier: batch` single-Opus-verdict refutes of LOW/MEDIUM findings — still don't re-raise).
- **Conventions (verify against the live files, don't assume):**
  - Planning artifacts live in the SAME dated `.audits/{date}-{dimension}-{area}/` dir that holds `audit.json`. The whole `.audits/` directory is **gitignored — these docs are disk-only, never committed or included in any PR**.
  - The proven format precedent is the 2026-07 remediation: `.audits/2026-07-02-kickoff.md` (per-session fenced Prompt blocks) + `.audits/2026-07-02-progress.md` (Status Board / Decisions Log / Session Log). **Read both before writing** — mirror their structure and tone.
  - Deferred/unscheduled findings go to the central **`.audits/BACKLOG.md`** (create if missing) — the cross-audit ledger the `/audit-codebase` scout reads as a skip-list source.
- **Dedup baseline** (don't replan known / shipped / deferred): prior `summary.json` files under `.audits/*/`, the legacy 2026-07-02 audit + its progress Status Board (all 49 findings shipped or backlogged), `.audits/BACKLOG.md`, and the audit's own `refuted[]`.
- **Dimension notes** (fold in `audit.meta.complementaryManualReviews`):
  - **performance**: every perf task needs a *measurement* (`go test -bench`, pprof profile, or a timed run on a realistic multi-session fixture — the Session-8 precedent: 60-session/135k-line synthetic fixture, best-of-5), not a vibe.
  - **security**: the audit fan-out skips dependency supply chain, CI/CD hardening, and gosec-class static scanning — list any such gaps as coverage gaps in the plan, don't pretend they were audited.
  - **data-integrity / correctness**: weight by trigger reachability — an UNVERIFIED finding is a *verify-first* task, not a blind fix.

## Phase 1 — Sequence + write the action plan
Write into the audit's dated dir:
- `_action-plan.md` — tasks in **compact format** (`<FINDING-ID> (Effort) Title — rationale + cross-refs`), grouped into **sessions** each sized to **one cohesive shippable PR** (one PR per session), ordered by urgency, with per-session read-first files, dependency order, and a verify gate. Include a **Decisions needed** section for design-choice findings (see promotion tiers). End with a **deferred list** — log every finding you dropped and WHY (silent truncation reads as "covered everything").
- **Promotion tiers** (which findings become tasks):
  - `findings[]` (CONFIRMED / ADJUSTED / DOWNGRADED) → sessioned tasks. `verificationTier: batch` findings are normal tasks too — the kickoff's mandatory re-verify-against-current-code covers the single-vote residual risk; just carry the tier into the task context.
  - `unverifiedFindings[]` → a cheap **verify-first** task (confirm reachability against current code, then fix or drop) OR the deferred list — never a blind fix.
  - Fix hinges on an unresolved design choice → a **Decisions needed** entry in `_action-plan.md`, pre-seeded as a D-row in `_progress.md`'s Decisions Log for the implementing session (or the user) to answer — never force-fit into a session. (Precedent: the 2026-07 remediation pre-seeded D1–D6 this way.)
  - **Severity ≠ priority.** A DOWNGRADED-LOW finding that is a *cheap, high-leverage defensive fix* can outrank a MEDIUM. Weight by severity × reachability × fix-cost × cluster cohesion; explicitly flag cheap fixes that punch above their severity.
- **Per-session model tier** (global CLAUDE.md model table) — assign every session one of `opus-4.8` · `opus-4.8 + fable review` · `fable`, recorded in the action plan, the kickoff session table, AND each session contract. Criterion = **spec clarity × blast radius**, NOT the effort letter (S measures size, not difficulty):
  - Fully-prescribed mechanical fixes, contained failure modes (validation, clamps, deletions, config/CI wiring, plumbing with explicit constraints) → **opus-4.8**.
  - Prescribed fixes whose blast radius is scary (machine-format shapes, parser choke points, anything all six commands flow through) → **opus-4.8 + fable review** — Opus implements, a Fable session (or `/code-review` run on fable) reviews the diff before the PR.
  - Judgment calls: cost-math/dedup/aggregation write paths, unresolved contingencies the session must investigate, anywhere a subtle mistake is *silent* (wrong dollars look authoritative) → **fable**.
  - Standing rule: escalate to Fable mid-session the moment Opus output misses the bar — judge the output, not the price tag.
- `_synthesis.md` is **optional** — `summary.json` already holds `overallAssessment` + `verificationStats` + `severityIndex` + `clusterTitles`; point at it rather than duplicating. Write one only if you need narrative beyond it.

## Phase 2 — Write the kickoff + progress doc + backlog
- **Write `_kickoff.md`** — give each session a short **slug** (e.g. `parser-robustness`) and include ALL of: a one-line **session table** (slug · findings · verification tier · **model tier** · effort · VERSION bump guess · branch `audit/sN-<slug>`), the **Global rules** block (below), one **`[Sx] <slug>`** contract per session (finding IDs · **model tier + why** · read-first · files touched · constraints · verify gate), a **recommended order** with the dependency rationale (which session changes numbers/outputs that later sessions pin), and — **REQUIRED, not optional** — a **paste-per-session Prompt template**: one fenced code block per session that the user pastes verbatim into a fresh session. Each prompt must name the read-first files (this dir's `audit.json` findings by ID + `_progress.md`) then spell out the do-steps verbatim (below) plus the session's specific tasks. This block is what the user actually drives from. Each implementation session runs the **native flow**:
  1. Fresh session; read the cited `audit.json` findings + `_progress.md` (prior sessions + decisions — don't relitigate answered D-rows); **re-verify each finding against current code** — line numbers drift, so anchor on the verbatim `locator.snippet`; verify-first sessions confirm reachability before planning the fix.
  2. Branch workflow (no exceptions): never commit to `main`. `git checkout main && git pull && git checkout -b audit/sN-<slug>`.
  3. Implement **test-first** — every fix lands with a regression test reproducing the finding's trigger. Gates: `make check` (fmt + golangci-lint + all tests); golden changes via `make update-golden` with the diff reviewed; TUI-touching changes smoke-tested live in tmux.
  4. Bump VERSION per project CLAUDE.md semver rules (patch = fix; minor = new flag/command/behavior; pre-1.0 minor may break).
  5. Update `_progress.md` (Status Board row, Session Log entry, any new D-row — **disk only, gitignored, never committed**), commit to the branch, push, `gh pr create`, then `gh pr merge --rebase --delete-branch` (standing rule: merge immediately — CI runs `make check` and the session already ran it; wait for green only if the change touches CI itself), `git checkout main && git pull`. Record the PR # + resulting main commit hash in the Status Board (status: merged).
- **Write `_progress.md`** — mirror `.audits/2026-07-02-progress.md`: the how-to-use header (session start/finish instructions), **Status Board** table (Session · Focus · Findings · Branch · Status · VERSION · PR · Date), **Decisions Log** (D-rows, pre-seeded with this plan's Decisions-needed questions), **Session Log** template + empty section. Number new D-rows continuing from the highest existing D-number across `.audits/` progress docs (the 2026-07 remediation used D1–D10) so decision IDs stay unique repo-wide.
- **Append deferred/unscheduled findings to `.audits/BACKLOG.md`** (create from the existing entries' shape if missing): one entry per finding — ID, title, `locator.file` + verbatim snippet, the source `audit.json` path, and WHY it was deferred.
- **Global rules to embed in the kickoff (ficha-specific):**
  - **Goldens are the reviewable evidence**: any rendering/output change → `make update-golden`, review the diff in the same PR; an unexplained golden diff is a regression. `.golden` files contain trailing spaces — editors that strip trailing whitespace corrupt them.
  - **Machine-format contract (decision D3)**: json/csv always emit the COMPLETE dataset; `-f` picks encoding only. A flag changes machine output iff it adds records not otherwise present; table-only flags must be documented as such, never a silent no-op.
  - **Warnings → stderr, never stdout** for `-f json/csv` (`TestE2EWarningsGoToStderr` guards this). Output routes through `cfg.stdout`/`cfg.stderr` (decision D10 — factory config, no package-level flag state; new flags bind in the relevant `newXxxCmd(cfg)` constructor).
  - **README is gated**: `cmd/readme_test.go` fails if a command or persistent flag isn't documented; keep README boxes ≤71 display cols.
  - **No finding-IDs or changelog-style comments in source** (project memory: comments explain why/caveats, never history) — finding IDs live in commits + these docs only.
  - **Known test gotchas** (project memory): forced color in non-TTY tests = `lipgloss.DefaultRenderer().SetColorProfile(termenv.ANSI256)`; TUI smoke tests via tmux + `CLAUDE_CONFIG_DIR` fixture sandbox (`tmux new-session -d -x <cols> -y <rows> '... ficha watch'` + `capture-pane -p`); `--project-dir=-encoded-name` needs the `=` form (leading-dash value breaks flag parsing).
  - Don't fix unlisted code — note it in `.audits/BACKLOG.md` instead.
- **Recommended-order heuristic** (from the 2026-07 remediation): sessions that change every reported number (cost math, dedup, parsing) come FIRST, before anything that pins outputs (goldens) or builds on the numbers; test-harness sessions precede refactors they protect.

## Phase 3 — Close-out (after the last session ships)
- Ensure every shipped row in `_progress.md`'s Status Board reads `merged` with PR # + main hash.
- Status stamp on `_action-plan.md` / `_kickoff.md` (PR map, beyond-plan fixes, live remainder → BACKLOG.md).
- One concise memory roll-up line (project memory dir) linking the audit dir + shipped PRs. (MEMORY.md is size-budgeted — one line; detail in a topic file.)

## Notes
- **Audit-only upstream, plan-only here.** `/audit-codebase` emits `audit.json` (no fixes); this skill sequences it (no fixes); the sessions it kicks off are the only thing that touches code.
- **Lineage**: the kickoff/progress conventions descend from the 2026-07-02 one-shot remediation (`.audits/2026-07-02-{kickoff,progress}.md` — 11 sessions, all merged); the `/audit-codebase → /plan-from-audit` split replaces the one-shot with a reusable JSON audit you can re-plan from without re-reviewing.
