# Contributing

Thanks for looking. This is a small Go CLI with one job, so the rules below are short.

## Setup

You need Go 1.26.8 or newer. Any later release works, 1.27 included. There's nothing else to install.

`make lint` builds golangci-lint v1.64.8 with `go run` the first time you call it, and so does `make fmt`, which uses the linter to fix imports. The build uses the Go named on go.mod's `go` line. So the first run may download that Go, go1.26.8 today, before it builds the linter. Later runs use the cached build.

Don't lint with a golangci-lint you installed yourself. With Go 1.27 or later, v1.64.8 can't read the standard library and reports dozens of bogus typecheck errors. v2 rejects this repo's config.

## Where things live

`main.go` only calls `cmd`. Outside tests, each package imports only the ones listed below it:

```
cmd                 commands and flags, terminal detection, project resolution
internal/tui        watch and breakdown, and the file watching behind them
internal/formatter  whole reports for show, list, summary and global: tables, json, csv
internal/render     the pieces formatter and tui both draw: costs, tokens, rules
internal/analyzer   loads each session and its agents through parser, then prices and totals them
internal/parser     reads the projects dir, sessions-index.json and JSONL transcripts
internal/paths      finds the config dir, maps working dirs to project dirs
internal/styles     palette, lipgloss styles, Unicode and ASCII glyphs
internal/pricing    the model catalog
internal/models     the types the rest pass around, and most of the json encoding
```

`go doc ./internal/tui` and the like print more on each. Two splits are easy to miss. `internal/styles` holds every color and lipgloss style, even the ones only the live views use. `internal/tui/styles.go` just gives them short local names and adds the live views' spinner and footnote helpers. And the static reports never look at the terminal. `cmd` checks it and passes `internal/formatter` a width and a `noColor` flag.

## The gate

While you work, run:

```sh
make check
```

That runs `gofmt -w` and fixes imports the way lint wants them, then lints, then runs the tests. It's the quick loop.

Before you push, run what CI runs:

```sh
make ci
```

That lists any file gofmt would change instead of rewriting it, then lints, runs the tests with `-race`, and cross-compiles all five release binaries. `-race` needs cgo and a C compiler. Without them, `make ci` runs the plain tests and prints a line saying why it skipped `-race`. In CI that's an error.

CI's `check` job runs `make ci` and then govulncheck. govulncheck isn't in `make ci`, because a fresh advisory isn't your PR's fault. CI also runs the plain tests on macOS and Windows.

`make test` prints one line per package. Pass `go test` flags through `TESTFLAGS` when you want more, as in `make test TESTFLAGS=-v` or `make test TESTFLAGS='-run TestGolden'`.

If you changed how something renders, regenerate the golden files and read the diff before committing:

```sh
make update-golden
git diff internal/formatter/testdata internal/tui/testdata cmd/testdata
```

A golden diff is a rendering change you are asserting is correct. Don't commit one you haven't looked at.

### Without make

On Windows, or anywhere without make, run the gate's steps from the repo root yourself:

```sh
gofmt -l cmd internal main.go
GOTOOLCHAIN=go1.26.8 go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8 run ./...
go test ./...
```

The lint line runs the linter under the Go named on go.mod's `go` line, as `make lint` does. PowerShell and cmd don't take the `VAR=value` prefix. Set the variable for the session instead, with `$env:GOTOOLCHAIN = "go1.26.8"` in PowerShell or `set GOTOOLCHAIN=go1.26.8` in cmd, and drop the prefix.

`gofmt -l` names the files it would change, and `go fmt ./...` rewrites them. Don't point gofmt at `.`: it descends into hidden directories, where other worktrees may be nested. Adding `--fix --enable-only goimports` to the lint line fixes imports, which is the rest of `make fmt`. `make update-golden` is `go test ./internal/formatter ./internal/tui ./cmd -run TestGolden -update`. On Windows, build with `go build -o bin/ficha.exe .`, since `make build` names the binary `bin/ficha` everywhere.

## Seeing your change

Tests and goldens don't show what a change looks like in a terminal. Your own `~/.claude` would, but it holds your prompts and your code, and that's what would end up in a screenshot. Run ficha against the synthetic fixture instead:

```sh
make build fixture
```

That writes fake transcripts to `bin/fixture`: one project, `work/webapp`, with one 424-message session and nine agents, five of them in two workflow runs. Point ficha at it and run it from the project's directory:

```sh
FX="$PWD/bin/fixture" PATH="$PWD/bin:$PATH"
cd "$FX/work/webapp"
HOME="$FX" CLAUDE_CONFIG_DIR="$FX/config" TZ=UTC ficha watch
```

Swap `watch` for `breakdown`, `show` or anything else. With `HOME` inside the fixture, the paths ficha prints read `~/work/webapp`, and `TZ=UTC` keeps your timezone out of the clock times. `docs/screenshots/watch.tape` uses the same setup. Take PR captures this way, never from your own transcripts.

The fixture's timestamps are relative to when you built it, so about five minutes later watch's header says idle. To start over, quit watch, run `make fixture` again at the repo root, then `cd` back in and restart watch. The rebuild replaces every file, so a watch left running stays on the deleted ones. Or keep the session going with `live.py` from a second terminal at the repo root:

```sh
python3 docs/screenshots/live.py bin/fixture/config/projects/*/*.jsonl
```

In a fresh fixture that glob matches the one session. With no flags, `live.py` appends a message every 2 seconds, 20 in all. The flags cover the rest of what watch and breakdown react to:

- `--agent` starts a new subagent in the session and appends to it.
- `--new-session`, given the project directory `bin/fixture/config/projects/*/` in place of the session, starts a new session. watch follows it.
- `--burst 50` appends 50 messages at once, for scroll anchoring and large jumps.

`--interval` and `--count` change the pace.

For a change that affects how fast ficha reads many sessions, the fixture is too small to time. `docs/screenshots/mkhistory.py <dir>` writes roughly 400 MB of history: some 300 sessions across six projects, spread over the last 60 days. Point `CLAUDE_CONFIG_DIR` at `<dir>/config` and time `ficha global -f json`, with and without `--since today`, before and after your change.

That history is even: six projects of one size, and no line over 3 KB. Real histories aren't. One project holds most of the bytes, and most bytes sit in a few long tool results. `python3 docs/screenshots/mkhistory.py <dir> --skew` writes that shape in about 800 MB. Time against it too when your change touches how ficha reads lines or spreads the work over threads.

`make screenshots` regenerates the README's images from a fresh fixture, so every run changes the webp files even when nothing on screen did. Run it only when the layout changed.

## Comments

Comments explain why, and note caveats the code can't express on its own. They never describe history ("used to...", "was changed to...") and never carry ticket or issue IDs. Git has the history.

## Versioning

Semantic versioning. The version lives in the `VERSION` file.

VERSION bumps are for CLI changes only. Docs, CI, tooling, and repo hygiene never bump it.

- PATCH (0.0.X): CLI bug fixes, and refactors with no behavior change.
- MINOR (0.X.0): new features, commands, and flags, without breaking anything.
- MAJOR (X.0.0): breaking changes. These need a maintainer's sign-off first.

Bump `VERSION` in the same commit as the change. Pre-1.0, a minor bump may include breaking changes.

## Pull requests

Title a PR `area: what changed`, lower case after the colon, as in `watch: order agents by start` or `csv: end every csv output in one newline, not two`. The release notes list each PR by its title as it reads when the release is tagged, so write it for someone who uses ficha. PRs are squash-merged, and the squash uses the PR title as the commit subject on main. Get it right before the merge: retitling afterwards changes the release notes but not the log.

The area is the command the change is about (`show`, `list`, `summary`, `global`, `watch`, `breakdown`), several joined with commas (`summary, global`), or one of `cli` for flags, help, errors, completion and project resolution, `json`, `csv`, `pricing`, `docs`, `tests`, `make`, `ci` and `deps`. A change no user can see, such as a refactor, takes its package as the area, as in `parser` or `tui`.

If your change alters output, flags, exit codes, or json keys or csv columns that scripts rely on, say how in the description; the template asks. Labels need write access, so the maintainer applies them while reviewing: `breaking change` on those PRs and `pricing` on catalog changes. Dependabot labels its own PRs `dependencies`. The release notes group PRs by these labels, breaking changes first.

## Adding a model's pricing

Every known model is one row in `modelCatalog` in `internal/pricing/pricing.go`. The pricing map, display names, and prefix matching are all derived from it, so a new model is a single added row.

Get the rates from [Anthropic's pricing page](https://platform.claude.com/docs/en/about-claude/pricing). If the model's cache-read rate isn't the default 10% of input, set `CacheReadRate` on the row.

Then add tests in `internal/pricing/pricing_test.go` for:

- the exact ID (`claude-opus-5-5`),
- a dated ID (`claude-opus-5-5-20260301`),
- the `[1m]` long-context variant,
- and a negative neighbor that must not resolve to the new row, such as `claude-opus-5-6` or `claude-opus-5-5-fast`. Add it to `TestUnlistedFamilyVersionsNeverResolve`.

The negative case matters. Prefix matching is how dated IDs find their row, and a sloppy prefix would silently price the next model in the family at the old rate.

A new model is a MINOR bump.

## Cutting a release

For maintainers.

Releases are cut at milestones, not at every `VERSION` bump. The versions in between exist only on main, so a build from main can report a version that has no release page.

1. Read the PRs merged since the last tag. The notes take each PR's title as it is when you tag, so retitle any that read badly, and check that every breaking one carries the `breaking change` label:

   ```sh
   gh pr list -s merged -L 30 --json number,title,labels --jq '.[] | "\(.number)\t\([.labels[].name] | join(","))\t\(.title)"'
   ```

2. Tag the latest commit on main with the version in `VERSION`, and push the tag:

   ```sh
   git switch main && git pull
   v="v$(cat VERSION)" && git tag "$v" && git push origin "$v"
   ```

The release workflow reruns lint and the tests, and refuses a tag that doesn't match `VERSION`. Then it builds the five static binaries, writes `checksums.txt`, attests build provenance, and publishes the GitHub release with notes generated from the PR titles, grouped by `.github/release.yml`.

A published tag is never moved or deleted. Go's module proxy and checksum database record it the first time anyone runs `go install`. After that, a moved tag either keeps serving the old code or fails installs with a checksum mismatch. Fix a bad release by shipping a new patch version.
