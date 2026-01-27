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
- **internal/analyzer/**: Core business logic - `cost.go` calculates costs from token usage, `session.go` orchestrates analysis, `breakdown.go` provides per-message analysis
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
