# CLAUDE.md

## Versioning

Semantic versioning; version lives in the `VERSION` file.

**VERSION bumps are for CLI changes only.** Docs/README, `.claude/` tooling, and repo hygiene never bump.

- **PATCH** (0.0.X): CLI bug fixes, refactors with no behavior change
- **MINOR** (0.X.0): new features, commands, flags (non-breaking)
- **MAJOR** (X.0.0): breaking changes — requires explicit user confirmation first

Update `VERSION` in the same commit as the change. Pre-1.0: minor bumps may include breaking changes.
