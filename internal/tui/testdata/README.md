# Golden TUI snapshots

`.golden` files pin the exact frames watch and breakdown render, including
ANSI escape codes in the `*_color` variants and the `--ascii` glyph set in
the `*_ascii` ones. The `TestGolden*` tests in `golden_test.go` and
`agent_list_test.go` compare them byte for byte. Snapshots for the static
reports live in `internal/formatter/testdata/`.

## Regenerating

After an **intentional** rendering change:

```bash
make update-golden        # or: go test ./internal/formatter ./internal/tui ./cmd -run TestGolden -update
git diff internal/tui/testdata
```

The diff is the change on screen. A golden diff you didn't intend means you
broke the layout.

## Determinism notes

- The fixtures use fixed UTC timestamps, and `TestMain` pins
  `time.Local = time.UTC`, because message times and the header clock render
  in local time.
- The palette is the dark one unless a test calls `styles.SetDark(false)`.
  Nothing in this package looks at a terminal.
- Every test pins the view's clock (`m.now`), so ages like "12s ago" don't
  drift. The full-frame goldens also get a fixed size through a
  `tea.WindowSizeMsg` and a pinned last-update time. The `watch_agents_folded`
  ones render only the agent section, so they need neither.
- A view built with `noColor` false writes its xterm-256 escapes whatever
  the terminal, and one built with `noColor` true writes none. The
  `*_color` goldens are the first kind as rendered. Fitting the escapes to a
  terminal is Bubble Tea's job, and isn't in a golden.
- A test of the light palette switches before it builds the view: the
  spinner and the sparkline take their colors when the view is built and
  while messages are handled, not in `View()`.
- Editors must not strip trailing whitespace here, since padded lines end in
  spaces, or add a final newline to a file that lacks one. `.editorconfig`
  says both for `*.golden`.
