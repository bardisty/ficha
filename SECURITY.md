# Security

## Reporting

Report vulnerabilities privately through [GitHub's private vulnerability reporting](https://github.com/bardisty/ficha/security/advisories/new), not in a public issue. You'll get a reply there.

Only the latest release is supported. Fixes ship as a new version rather than a patch to an old one.

## Threat model

ficha reads the session transcripts Claude Code keeps on your disk, which can contain your prompts and the code Claude looked at. It makes no network requests, runs no subprocesses, and writes no files. So the bugs worth reporting are the ones that break that contract or turn hostile input into a problem: a malformed transcript that crashes or hangs it, output that injects into whatever consumes it, or a path that makes ficha read outside the projects directory it is meant to stay in. CSV formula injection is already mitigated in `internal/formatter/csv_safe.go`, so anything that gets past that is a bug too.
