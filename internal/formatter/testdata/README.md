# Golden rendering snapshots

`.golden` files pin the exact rendered output (including ANSI escape codes in
`*_color` variants) of the formatter's table renderers, compared byte-for-byte
by the `TestGolden*` tests in `golden_test.go`. Equivalent snapshots for the
TUI live in `internal/tui/testdata/`.

## Regenerating

After an **intentional** rendering change:

```bash
make update-golden        # or: go test ./internal/formatter ./internal/tui ./cmd -run TestGolden -update
git diff                  # review — the diff IS the behavior change
```

A golden diff you didn't intend means you regressed the layout.

## Determinism notes

- Fixtures use fixed UTC timestamps; `TestMain` pins `time.Local = time.UTC`
  because `FormatSessionTable` renders `EndTime.Local()` in its footer.
- A report formatted with `noColor` false carries its xterm-256 escapes
  whatever the terminal, and one with `noColor` true carries none. The
  `*_color` goldens are the first kind as formatted. `cmd` fits the escapes
  to what stdout takes, which isn't in a golden. The palette is the dark one
  unless a test calls `styles.SetDark(false)`.
- The `summary_details*` goldens render from a temp-dir JSONL fixture run
  through `analyzer.AnalyzeMultipleSessions`, the same path `cmd/summary`
  uses. Their costs come from the live pricing catalog, so a pricing-table
  change legitimately changes them.
- Editors must not strip trailing whitespace here, since chart and padded
  lines end in spaces, or add a final newline to a file that lacks one.
  `.editorconfig` says both for `*.golden`.
