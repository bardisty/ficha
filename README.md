# ficha

Track your Claude Code API-equivalent costs, token usage, and context window in real time.

ficha is an independent project. It is not affiliated with or endorsed by Anthropic.

## Screenshots

<table>
  <tr>
    <td width="50%" valign="top"><a href="docs/ficha-watch.webp"><img src="docs/ficha-watch.webp" alt="ficha watch, the live session cost dashboard"></a></td>
    <td width="50%" valign="top"><a href="docs/ficha-breakdown.webp"><img src="docs/ficha-breakdown.webp" alt="ficha breakdown, the per-message cost table"></a></td>
  </tr>
  <tr>
    <td valign="top"><em><code>ficha watch</code> is the live dashboard: token counts by type (input, output, cache write/read), context-window usage, cache economics, cost trend, per-model and agent sub-session costs.</em></td>
    <td valign="top"><em><code>ficha breakdown</code> is a live, scrollable per-message table: cost, token counts, and originating agent for every message, with parent and agents interleaved chronologically and updating as the session runs.</em></td>
  </tr>
</table>

## Install

### Prebuilt binaries

Each release on the [releases page](https://github.com/bardisty/ficha/releases) ships one raw binary per platform, plus `checksums.txt`:

| File | Platform |
| --- | --- |
| `ficha-darwin-arm64` | macOS, Apple silicon |
| `ficha-darwin-amd64` | macOS, Intel |
| `ficha-linux-amd64` | Linux, x86-64 |
| `ficha-linux-arm64` | Linux, ARM64 |
| `ficha-windows-amd64.exe` | Windows, x86-64 |

There is no Windows ARM64 build. From v0.24.0 the Linux binaries are statically linked, so they also run on Alpine and other musl-based systems.

Download the file for your machine, make it executable, and move it somewhere on your `PATH`:

```sh
chmod +x ficha-darwin-arm64
xattr -d com.apple.quarantine ficha-darwin-arm64   # macOS only, see below
sudo mkdir -p /usr/local/bin
sudo mv ficha-darwin-arm64 /usr/local/bin/ficha
```

The macOS binaries aren't notarized, so Gatekeeper blocks them on first run. The `xattr` line clears the quarantine flag that browsers set on download. If you fetched the file with `curl` there is no flag to clear, and the command complains and does nothing.

On Windows, rename the file to `ficha.exe` and put it in a folder on your `PATH`.

### Verifying a download

`checksums.txt` lists the SHA-256 of every binary. Check yours before you rename it, with `checksums.txt` in the same directory:

```sh
sha256sum -c --ignore-missing checksums.txt        # Linux
shasum -a 256 -c --ignore-missing checksums.txt    # macOS
```

On Windows, `Get-FileHash ficha-windows-amd64.exe` in PowerShell prints the hash to compare against that file's line in `checksums.txt`.

Releases from v0.24.0 onward also carry a build provenance attestation, which ties each binary to the GitHub Actions run that built it from a tagged commit:

```sh
gh attestation verify ficha-darwin-arm64 --repo bardisty/ficha
```

### From source

Requires Go 1.25.6 or newer.

```sh
go install github.com/bardisty/ficha@latest
```

## Usage

Run ficha from the same directory Claude Code is running in. It finds that project's sessions automatically:

```sh
cd /path/to/your/project
ficha            # view current session costs
ficha watch      # live monitoring (auto-follows new sessions)
ficha breakdown  # live per-message cost table
```

`watch` and `breakdown` pair well in side-by-side terminals: session totals in one, per-message costs in the other.

To analyze a different project without cd'ing, pass its directory with `-p` / `--project`.

## Commands

| Command | Description |
| --- | --- |
| `ficha [show]` | Show latest (or specified) session costs |
| `ficha watch` | Live monitoring with auto-follow |
| `ficha breakdown` | Live per-message cost table (scrollable) |
| `ficha list` | List all sessions |
| `ficha summary` | Total costs across all sessions |
| `ficha global` | Aggregated stats across all projects |
| `ficha version` | Print version information |

## Flags

Global flags, accepted by every command:

| Flag | Description |
| --- | --- |
| `-f, --format <fmt>` | Output format: table, json, csv |
| `-v, --verbose` | Show debug information |
| `--no-color` | Disable colored output (same as setting `NO_COLOR`) |
| `--ascii` | Draw frames and symbols in plain ASCII instead of Unicode |

`watch`, `breakdown` and `show --live` render a terminal UI, so they reject `-f json` and `-f csv`. `version` prints plain text and ignores `--format`.

Per-command flags:

| Command | Flag | Description |
| --- | --- | --- |
| `show`, `watch`, `breakdown`, `list`, `summary` | `-p, --project <dir>` | Project directory (default: current dir) |
| `show`, `watch`, `breakdown`, `list`, `summary` | `--project-dir <name>` | Claude project dir name, instead of `-p` (bypass auto-detect) |
| `show` | `-l, --live` | Watch the session live (same as `ficha watch`) |
| `show`, `watch`, `breakdown` | `--no-follow` | Stay on the starting session instead of following new ones (live mode only) |
| `show` | `--messages` | Per-message rows (json/csv only) |
| `summary` | `-d, --details` | Add a per-session breakdown |
| `summary` | `--expand-agents` | Per-agent records (requires `--details`) |
| `global` | `-n, --top <n>` | Top N projects in table (default 10) |
| `global` | `--sort-by <key>` | Sort: cost, sessions, name, activity |
| `global` | `-d, --details` | All projects + cumulative column (table) |

Bare `ficha` works like `show` and takes the same project flags. A flag on a command that doesn't use it is an error, and `--no-follow` or `--messages` where they'd have no effect print a warning.

## Examples

```sh
ficha show abc123             # view session by partial ID
ficha watch abc123            # watch specific session (pinned)
ficha -f json > out.json      # export to JSON
ficha summary -d -f csv       # per-session rows as CSV
ficha global --top 5          # top 5 projects by cost
ficha show -f csv --messages  # per-message rows as CSV
ficha list -p /path/to/dir    # list sessions for different project
```

## Shell completion

`ficha completion <shell>` prints a completion script for bash, zsh, fish or PowerShell. It completes commands, flag values (`-f`, `--sort-by`, directories for `-p`) and session IDs. In shells that show descriptions, each session ID shows how long ago it was modified and, when the matching transcripts are small enough to read quickly, its message count.

Claude project directory names start with `-`, so complete them with `--project-dir=<Tab>`. Without the `=`, the shell reads what you've typed as a flag.

```sh
source <(ficha completion bash)                                   # bash, current shell
ficha completion zsh > "${fpath[1]}/_ficha"                       # zsh, then restart the shell
ficha completion fish > ~/.config/fish/completions/ficha.fish     # fish
```

`ficha completion <shell> --help` has the persistent setup for each shell.

## Machine output (json / csv)

`-f` only picks the encoding. json and csv always export the complete dataset, as a single object or a uniform-column table, safe for `jq` and pandas.

- Unreadable input is counted, never swallowed: `skipped_sessions`, `skipped_agents`, `skipped_lines`, `estimated_cost_messages`.
- Agent spend is always split out: `parent_cost + agents_cost = total_cost`.
- `cost_by_model` keys are canonical model IDs, so summing by key needs no normalization.

The full export contract is in [docs/machine-output.md](docs/machine-output.md). It covers flag interactions, record provenance, the counters, per-message rows, and csv safety.

## Privacy

ficha only reads. It opens the transcripts Claude Code writes under `~/.claude/projects/`, or under `$CLAUDE_CONFIG_DIR/projects/` if you have set that variable. `CLAUDE_CONFIG_DIR` is Claude Code's own override, and ficha honors it so the two always agree on where sessions live. It makes no network requests, runs no subprocesses, and writes no files. Nothing in the non-test code imports `net/http` or `os/exec` or opens a file for writing, and I intend to keep it that way.

Those transcripts contain your prompts and whatever code Claude read, so they are sensitive. The only thing that leaves your machine is what you choose to paste from ficha's output.

## How it works

ficha parses the JSONL transcripts and multiplies each message's token counts by the bundled list price for its model, with cache writes and cache reads priced separately. Cache reads carry a steep discount, and ficha tracks the savings: 90% off the input rate on most models, 95% on Opus 5.5, and 97.5% on Fable 5.1 and Mythos 5.1.

Agent sub-sessions are included, both regular subagents and Claude Code Workflow agents. Workflow agents are grouped by workflow run, with the run's name and status taken from its metadata.

### What the numbers mean

The figures are API-equivalent estimates from list prices. On a Claude subscription you aren't billed per token, so read the total as what the same work would have cost through the API, not as a bill.

A few things ficha does not model:

- Long-context premium pricing. On models older than Claude 4.6, a 1M-context request whose input passes 200K tokens is billed at a higher rate. ficha widens the context window it reports but prices every token at the base rate. Claude 4.6 and later have no premium.
- Fast mode, which Anthropic bills at a premium.
- Web search, which Anthropic charges per search on top of the tokens.
- Bedrock and Vertex billing. Their model IDs are recognized and priced at Anthropic's first-party rates. Both platforms bill on their own terms, so treat the figures as a proxy.

Unknown models are priced at $3 input and $15 output per million tokens with a 200K context, and ficha prints a warning naming the model so you know a number is a placeholder. When a new model ships, its prices need a ficha update. Open a [pricing update issue](https://github.com/bardisty/ficha/issues/new?template=pricing_update.yml) with the model ID and the published rates, or send a PR. It's one catalog row plus tests, and [CONTRIBUTING.md](CONTRIBUTING.md#adding-a-models-pricing) walks through it.

Claude Code's transcript format is undocumented and can change between releases. ficha counts what it could not parse instead of guessing, so if the skip warnings or the `skipped_*` counters jump after a Claude Code update, that is the signal to file a bug.

> [!NOTE]
> Claude Code deletes session transcripts older than 30 days by default (`cleanupPeriodDays` in `~/.claude/settings.json`), so ficha can only report what still exists on disk. To keep longer history, raise the setting, e.g. `"cleanupPeriodDays": 365`. Avoid `0`, which has [known bugs](https://github.com/anthropics/claude-code/issues/59248).

## Contributing

Bug reports and PRs are welcome. [CONTRIBUTING.md](CONTRIBUTING.md) covers setup, the `make check` gate, versioning, and how to add a model's pricing.

## License

[MIT](LICENSE)
