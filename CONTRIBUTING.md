# Contributing

Thanks for looking. This is a small Go CLI with one job, so the rules below are short.

## Setup

You need Go 1.25.6 or newer. Any later release works, 1.27 included. There's nothing else to install.

`make lint` builds golangci-lint v1.64.8 with `go run` the first time you call it, using the Go named on go.mod's `toolchain` line. So the first run may download that Go, 1.25.14 today, before it builds the linter. Later runs use the cached build.

Don't lint with a golangci-lint you installed yourself. With Go 1.27 or later, v1.64.8 can't read the standard library and reports dozens of bogus typecheck errors. v2 rejects this repo's config.

## The gate

While you work, run:

```sh
make check
```

That runs `gofmt -w`, then the linter, then the tests. It's the quick loop.

Before you push, run what CI runs:

```sh
make ci
```

That lists any file gofmt would change instead of rewriting it, then lints, runs the tests with `-race`, and cross-compiles all five release binaries. `-race` needs cgo and a C compiler. Without them, `make ci` runs the plain tests and prints a line saying why it skipped `-race`. In CI that's an error.

CI's `check` job runs `make ci` and then govulncheck. govulncheck isn't in `make ci`, because it needs a newer Go than go.mod and a fresh advisory isn't your PR's fault. CI also runs the plain tests on macOS and Windows.

`make test` prints one line per package. Pass `go test` flags through `TESTFLAGS` when you want more, as in `make test TESTFLAGS=-v` or `make test TESTFLAGS='-run TestGolden'`.

If you changed how something renders, regenerate the golden files and read the diff before committing:

```sh
make update-golden
git diff internal/formatter/testdata internal/tui/testdata
```

A golden diff is a rendering change you are asserting is correct. Don't commit one you haven't looked at.

## Comments

Comments explain why, and note caveats the code can't express on its own. They never describe history ("used to...", "was changed to...") and never carry ticket or issue IDs. Git has the history.

## Versioning

Semantic versioning. The version lives in the `VERSION` file.

VERSION bumps are for CLI changes only. Docs, CI, tooling, and repo hygiene never bump it.

- PATCH (0.0.X): CLI bug fixes, and refactors with no behavior change.
- MINOR (0.X.0): new features, commands, and flags, without breaking anything.
- MAJOR (X.0.0): breaking changes. These need a maintainer's sign-off first.

Bump `VERSION` in the same commit as the change. Pre-1.0, a minor bump may include breaking changes.

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

1. Merge the PR that bumps `VERSION`.
2. Tag that commit on main and push the tag:

   ```sh
   git tag vX.Y.Z && git push origin vX.Y.Z
   ```

The release workflow reruns lint and the tests, and refuses a tag that doesn't match `VERSION`. Then it builds the five static binaries, writes `checksums.txt`, attests build provenance, and publishes the GitHub release with generated notes.

A published tag is never moved or deleted. Go's module proxy and checksum database record it the first time anyone runs `go install`. After that, a moved tag either keeps serving the old code or fails installs with a checksum mismatch. Fix a bad release by shipping a new patch version.
