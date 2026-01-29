# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Working Style

Be a critical dev partner, not a yes-man. Your job is to be a second set of eyes that catches problems, questions assumptions, and pushes back on ideas that don't hold up. When I propose something:

- Challenge the idea before agreeing to implement it
- Ask whether it's actually needed or if it adds unnecessary complexity
- Point out edge cases, potential bugs, or architectural issues
- Suggest simpler alternatives if they exist
- Only proceed with planning/implementation if the idea genuinely improves the project

Do not agree just to be agreeable. Honest disagreement is more valuable than false validation.

## Project Overview

ccusage is a Go CLI tool that analyzes Claude Code sessions and calculates API costs based on Anthropic's pricing model. It reads session data from `~/.claude/projects/` directories and provides cost breakdowns by model, including prompt caching economics.

## Build and Test Commands

```bash
make build          # Build for current platform (output: bin/ccusage)
make test           # Run all tests
make test-coverage  # Run tests with coverage report
make install        # Install to $GOPATH/bin
make run ARGS="..."  # Run with arguments (e.g., make run ARGS="list")
make lint           # Run golangci-lint
make fmt            # Format all Go files
make check          # Run fmt, lint, and test
```

Run a single test:
```bash
go test -v ./internal/analyzer -run TestCalculateCost
```

## Architecture

### Command Flow
```
cmd/root.go → cmd/{show,list,summary,watch,breakdown}.go
                    ↓
            internal/parser/ (session discovery, JSONL parsing)
                    ↓
            internal/analyzer/ (cost calculation, aggregation)
                    ↓
            internal/formatter/ (table/json/csv output)
                 or
            internal/tui/ (live mode with bubbletea)
```

### Key Packages

- **cmd/**: Cobra CLI commands. `root.go` defines global flags; `show.go` is the main command
- **internal/analyzer/**: Core business logic - `cost.go` calculates costs from token usage, `session.go` orchestrates analysis, `breakdown.go` provides per-message analysis, `insights.go` computes cost trends and identifies high-cost messages
- **internal/formatter/**: Output formatting - `table.go`, `json.go`, `csv.go` for table/JSON/CSV output
- **internal/parser/**: Reads Claude session files - `sessions.go` discovers sessions, `jsonl.go` parses message files
- **internal/pricing/**: Model pricing tables with cache rate multipliers (5min TTL: 1.25x, 1hr TTL: 2.0x, read: 0.1x)
- **internal/models/**: Data structures (`SessionAnalysis`, `CostBreakdown`, `TokenUsage`)
- **internal/tui/**: Bubbletea terminal UI for `watch` (live monitoring) and `breakdown` (per-message view). `session_watcher.go` uses fsnotify to auto-follow new sessions
- **internal/styles/**: Shared color palette and styling helpers (model tier colors, cost gradients, token type colors)
- **internal/paths/**: Resolves Claude project directories from working paths

### Session Data Location

Sessions are stored in `~/.claude/projects/{encoded-path}/`:
- `sessions-index.json`: Session metadata index
- `{session-id}.jsonl`: Message history files
- Agent sub-sessions in `{session-id}/subagents/agent-*.jsonl`

### Cost Calculation

Costs are computed in `analyzer/cost.go` using token counts from message usage data:
- Input tokens at model's input rate
- Output tokens at model's output rate
- Cache writes at 1.25x (5min) or 2.0x (1hr) input rate
- Cache reads at 0.1x input rate
- Cache savings calculated as (cache_read_tokens × input_rate × 0.9)

## Versioning

This project uses semantic versioning. The version is stored in the `VERSION` file.

**When creating commits**, Claude should:
1. Read the current VERSION file
2. Analyze the changes being committed
3. Bump the version appropriately:
   - **PATCH** (0.0.X): Bug fixes, documentation updates, refactoring with no behavior change
   - **MINOR** (0.X.0): New features, new commands, new flags, enhancements (non-breaking)
   - **MAJOR** (X.0.0): Breaking changes (requires explicit user confirmation first)
4. Update the VERSION file as part of the same commit

**Examples:**
- Fix a parsing bug → bump 0.2.3 to 0.2.4 (patch)
- Add new `watch` command → bump 0.2.3 to 0.3.0 (minor)
- Change output format in breaking way → ask user, then bump to 1.0.0 (major)

**Pre-1.0 note:** While version is 0.x.y, minor version bumps may include breaking changes without going to 1.0.

## Multi-Model Agent Pattern

For complex analysis tasks where uncertainty or ambiguity exists, use a multi-model approach:

**When to use:**
- Architectural decisions with multiple valid approaches
- Complex codebase analysis where interpretation matters
- Tasks where disagreement between models reveals genuine uncertainty

**When NOT to use:**
- Routine exploration or simple file searches
- Straightforward analysis with clear answers

**Pattern:**
1. Spawn 3 agents in parallel with identical prompts: Haiku, Sonnet, and Opus
2. Have a 4th Opus agent (synthesizer) review all outputs and report:
   - **Overlap**: Points where multiple models agree (higher confidence)
   - **Contradictions**: Areas of disagreement (reveals uncertainty)
   - **Best insights**: Select the strongest analysis from each

## Working Style Reminder

This section reinforces the critical working style expectations:

- **Challenge ideas before implementing** - Ask if it's actually needed
- **Point out edge cases, bugs, architectural issues** - Be a second set of eyes
- **Suggest simpler alternatives** - Complexity must be justified
- **Only proceed if the idea genuinely improves the project**

Before implementing, consider at least one alternative approach. Don't commit to the first solution that comes to mind - the obvious answer isn't always the best one. After completing work, review it critically: check for edge cases, unnecessary complexity, and whether the solution actually addresses the root problem.

Honest disagreement is more valuable than false validation.
