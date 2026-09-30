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
- `TestMain` also pins a dark background. Left to detect, lipgloss would ask
  whatever terminal the tests run in.
- Every test pins the view's clock (`m.now`), so ages like "12s ago" don't
  drift. The full-frame goldens also get a fixed size through a
  `tea.WindowSizeMsg` and a pinned last-update time. The `watch_agents_folded`
  ones render only the agent section, so they need neither.
- Colored goldens force the lipgloss renderer to `termenv.ANSI256`, and
  no-color goldens to `termenv.Ascii`. Set the profile before the first
  `Update`: the sparkline is drawn while messages are handled, not in
  `View()`.
- Editors must not strip trailing whitespace here, since padded lines end in
  spaces, or add a final newline to a file that lacks one. `.editorconfig`
  says both for `*.golden`.
