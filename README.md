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

The links below always point at the latest release, so running a block again upgrades ficha. Each block downloads the binary and `checksums.txt`, checks the binary's SHA-256, and installs it only if the check passes.

#### macOS and Linux

In zsh or bash, set `f` to your file from the table, then run the rest as is. On macOS:

```sh
f=ficha-darwin-arm64
curl -fLO "https://github.com/bardisty/ficha/releases/latest/download/$f"
curl -fLO https://github.com/bardisty/ficha/releases/latest/download/checksums.txt
shasum -a 256 -c --ignore-missing checksums.txt &&
  mkdir -p ~/.local/bin && install -m 755 "$f" ~/.local/bin/ficha &&
  rm "$f" checksums.txt
```

On Linux, the same with `sha256sum` for the check:

```sh
f=ficha-linux-amd64
curl -fLO "https://github.com/bardisty/ficha/releases/latest/download/$f"
curl -fLO https://github.com/bardisty/ficha/releases/latest/download/checksums.txt
sha256sum -c --ignore-missing checksums.txt &&
  mkdir -p ~/.local/bin && install -m 755 "$f" ~/.local/bin/ficha &&
  rm "$f" checksums.txt
```

If the check fails, the chain stops there. Nothing is installed, and both files stay where they are. To install for all users instead, swap the `mkdir` line for `sudo mkdir -p /usr/local/bin && sudo install -m 755 "$f" /usr/local/bin/ficha &&`.

Then run `ficha version`. If it says command not found, `~/.local/bin` isn't on your `PATH` yet. macOS never adds it, and Debian and Ubuntu add it at login only if it already existed. Add `export PATH="$HOME/.local/bin:$PATH"` to `~/.zshrc`, or for bash to `~/.bashrc` (`~/.bash_profile` on macOS), and open a new terminal.

The macOS binaries aren't notarized. curl doesn't mark what it downloads as quarantined, so Gatekeeper lets them run. If you downloaded the file in a browser instead, Gatekeeper blocks it on first run. Clear the flag before you install it, with `xattr -d com.apple.quarantine ficha-darwin-arm64` (or `-amd64`).

#### Windows

In PowerShell, 5.1 or 7:

```powershell
$url = 'https://github.com/bardisty/ficha/releases/latest/download'
$dir = "$env:LOCALAPPDATA\Programs\ficha"
$ProgressPreference = 'SilentlyContinue'   # the progress bar makes 5.1 download very slowly
Invoke-WebRequest "$url/ficha-windows-amd64.exe" -OutFile ficha-windows-amd64.exe -UseBasicParsing
Invoke-WebRequest "$url/checksums.txt" -OutFile checksums.txt -UseBasicParsing
$want = ((Select-String -SimpleMatch ficha-windows-amd64.exe checksums.txt).Line -split ' ')[0]
if ((Get-FileHash ficha-windows-amd64.exe).Hash -eq $want) {
    New-Item -ItemType Directory -Force $dir -ErrorAction Stop | Out-Null
    Move-Item -Force ficha-windows-amd64.exe "$dir\ficha.exe" -ErrorAction Stop
    Remove-Item checksums.txt
    "Installed $dir\ficha.exe"
} else {
    Write-Error 'ficha-windows-amd64.exe does not match checksums.txt, or a download failed. Nothing was installed.'
}
```

`-eq` ignores case, so the uppercase hash from `Get-FileHash` matches the lowercase one in `checksums.txt`. If ficha is running, say in a `ficha watch` window, Windows won't let the block replace it. Quit ficha and run the block again.

The first time, add the folder to your user `PATH`. `rundll32 sysdm.cpl,EditEnvironmentVariables` opens the Environment Variables window. Under the variables for your user, select `Path`, then Edit, New, and paste `%LOCALAPPDATA%\Programs\ficha`. Click OK in both windows, open a new terminal, and run `ficha version`.

### Verifying provenance

Releases from v0.24.0 onward also carry a build provenance attestation, which ties each binary to the GitHub Actions run that built it from a tagged commit. Checking it needs the [GitHub CLI](https://cli.github.com), logged in with `gh auth login`:

```sh
gh attestation verify ~/.local/bin/ficha --repo bardisty/ficha
```

Point it at wherever you installed ficha. On Windows that's `"$env:LOCALAPPDATA\Programs\ficha\ficha.exe"`.

### From source

Requires Go 1.25.6 or newer.

```sh
go install github.com/bardisty/ficha@latest
```

This puts `ficha` in `$(go env GOPATH)/bin`, usually `~/go/bin`, or in `$GOBIN` if you set it. Add that folder to your `PATH` if `ficha` isn't found. To install a particular release, replace `@latest` with its tag from the releases page, such as `@v0.52.0`.

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
| `ficha list` | List sessions: when, length, model, agents, cost and title |
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

On macOS and Linux, colors adapt to a light or dark terminal background, which ficha asks the terminal for. Inside tmux or screen it can't ask and assumes dark, so on a light background set `COLORFGBG=0;15`. On Windows it always uses the dark palette.

Per-command flags:

| Command | Flag | Description |
| --- | --- | --- |
| `show`, `watch`, `breakdown`, `list`, `summary` | `-p, --project <dir>` | Project directory (default: current dir) |
| `show`, `watch`, `breakdown`, `list`, `summary` | `--project-dir <name>` | Claude project dir name, instead of `-p` (bypass auto-detect) |
| `show` | `-l, --live` | Watch the session live (same as `ficha watch`) |
| `show`, `watch`, `breakdown` | `--no-follow` | Stay on the starting session instead of following new ones (live mode only) |
| `show` | `--messages` | Add per-message records: a `messages` array in json, one row per message in csv |
| `summary` | `-d, --details` | Add a per-session breakdown |
| `summary` | `--expand-agents` | Per-agent records (requires `--details`) |
| `summary`, `global` | `--since <when>` | Only messages from then on: `2026-09-01`, `today`, `7d`, `12h` |
| `summary`, `global` | `--until <when>` | Only messages before then; a date includes that day |
| `global` | `-n, --top <n>` | Top N projects in table (default 10) |
| `global` | `--sort-by <key>` | Sort: cost, sessions, name, activity |
| `global` | `-d, --details` | All projects in the table, with a cumulative column when sorted by cost |

Bare `ficha` works like `show` and takes the same project flags. A flag on a command that doesn't use it is an error, and `--no-follow` or `--messages` where they'd have no effect print a warning.

## Examples

```sh
ficha show abc123             # view session by partial ID
ficha watch abc123            # watch specific session (pinned)
ficha -f json > out.json      # export to JSON
ficha summary -d -f csv       # per-session rows as CSV
ficha global --top 5          # top 5 projects by cost
ficha global --since 7d       # every project, the last 7 days
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

`-f` only picks the encoding. json is one document per run and csv one table with the same columns on every row, safe for `jq` and pandas. Table-only flags like `global --top` don't cut either one down. csv is the flat view: it leaves out nested detail such as the per-model split, which only json has.

- Unreadable input is counted, never swallowed: `skipped_sessions`, `skipped_agents`, `skipped_lines`, `estimated_cost_messages`.
- Agent spend is always split out: `jq '.parent_cost.total_cost + .agents_cost.total_cost == .total_cost.total_cost'` holds, up to float rounding.
- `cost_by_model` keys are canonical model IDs, so summing by key needs no normalization.

The full export contract is in [docs/machine-output.md](docs/machine-output.md). It covers each command's shape, flag interactions, record provenance, the counters, timestamps, per-message rows, what csv leaves out, and csv safety.

Before 1.0, a minor release can rename or remove keys. [Compatibility](docs/machine-output.md#compatibility) says how those changes are announced and how to pin a version.

## Privacy

ficha only reads. It opens the transcripts Claude Code writes under `~/.claude/projects/`, or under `$CLAUDE_CONFIG_DIR/projects/` if you have set that variable. `CLAUDE_CONFIG_DIR` is Claude Code's own override, and ficha honors it so the two always agree on where sessions live. It makes no network requests, runs no subprocesses, and writes no files. Nothing in the non-test code imports `net/http` or `os/exec` or opens a file for writing, and I intend to keep it that way.

Those transcripts contain your prompts and whatever code Claude read, so they are sensitive. The only thing that leaves your machine is what you choose to paste from ficha's output.

## How it works

ficha parses the JSONL transcripts and multiplies each message's token counts by the bundled list price for its model, with cache writes and cache reads priced separately. Cache reads carry a steep discount, and ficha tracks the savings: 90% off the input rate on most models, 95% on Opus 5.5, and 97.5% on Fable 5.1 and Mythos 5.1.

Agent sub-sessions are included, both regular subagents and Claude Code Workflow agents. Workflow agents are grouped by workflow run, with the run's name and status taken from its metadata.

### What the numbers mean

The figures are API-equivalent estimates from list prices. On a Claude subscription you aren't billed per token, so read the total as what the same work would have cost through the API, not as a bill. `ficha help output` explains the rest of the terms on screen: cache TTLs, Savings, messages, insights and Context.

A few things ficha does not model:

- Long-context premium pricing. On models older than Claude 4.6, a 1M-context request whose input passes 200K tokens is billed at a higher rate. ficha widens the context window it reports but prices every token at the base rate. Claude 4.6 and later have no premium.
- Fast mode, which Anthropic bills at a premium.
- Web search, which Anthropic charges per search on top of the tokens.
- Bedrock and Vertex billing. Their model IDs are recognized and priced at Anthropic's first-party rates. Both platforms bill on their own terms, so treat the figures as a proxy.

Unknown models are priced at $3 input and $15 output per million tokens with a 200K context, and ficha prints a warning naming the model so you know a number is a placeholder. The `<synthetic>` model Claude Code writes on API-error lines isn't unknown: those lines cost nothing and get no warning. When a new model ships, its prices need a ficha update. Open a [pricing update issue](https://github.com/bardisty/ficha/issues/new?template=pricing_update.yml) with the model ID and the published rates, or send a PR. It's one catalog row plus tests, and [CONTRIBUTING.md](CONTRIBUTING.md#adding-a-models-pricing) walks through it.

Claude Code's transcript format is undocumented and can change between releases. ficha counts what it could not parse instead of guessing, so if the skip warnings or the `skipped_*` counters jump after a Claude Code update, that is the signal to file a bug.

> [!NOTE]
> Claude Code deletes session transcripts older than 30 days by default (`cleanupPeriodDays` in `~/.claude/settings.json`), so ficha can only report what still exists on disk. To keep longer history, raise the setting, e.g. `"cleanupPeriodDays": 365`. Avoid `0`, which has [known bugs](https://github.com/anthropics/claude-code/issues/59248).

## Contributing

Bug reports and PRs are welcome. [CONTRIBUTING.md](CONTRIBUTING.md) covers setup, the `make check` and `make ci` gates, versioning, and how to add a model's pricing.

## License

[MIT](LICENSE)
