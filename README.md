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

## Requirements

ficha runs on Linux, macOS and Windows, and CI tests all three. It has been checked against the transcripts of Claude Code 2.1. If a newer Claude Code makes it start skipping lines, see [Troubleshooting](#troubleshooting).

## Install

Install ficha where Claude Code runs, since it reads the transcripts Claude Code keeps in that machine's home directory. Over SSH or in a dev container, that means the remote machine or the container. If Claude Code runs in WSL, use the Linux binary inside WSL, not the Windows one.

If Claude Code runs on Windows and you want ficha's reports from inside WSL, set `CLAUDE_CONFIG_DIR` for that one command, as in `CLAUDE_CONFIG_DIR=/mnt/c/Users/<you>/.claude ficha global`. For a single project, run it from a folder with the same name as the Windows project, such as the project's own folder under `/mnt/c`. ficha matches it by name and prints a note saying so. `show`, `list`, `summary` and `global` work this way. So do `watch` and `breakdown`, but across the mount a new message takes up to 2 seconds to show, and neither of them notices a new session starting. To follow sessions as they start, run those two on Windows. Don't export the variable from your shell profile. Claude Code in WSL reads it too, and would start keeping its own settings and sessions in the Windows folder.

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

This build is for Claude Code running on Windows itself. If Claude Code runs in WSL, use the Linux block inside WSL. In PowerShell, 5.1 or 7:

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

### Upgrading and uninstalling

ficha never checks for updates, because it makes no network requests. To hear about new releases, watch the repository for releases only (Watch, Custom, Releases), follow the [releases feed](https://github.com/bardisty/ficha/releases.atom), or run `gh release list -R bardisty/ficha`. `ficha version` prints the version you have.

To upgrade, run your install block again, or `go install github.com/bardisty/ficha@latest`.

If you script against `-f json` or `-f csv`, read the [release notes](https://github.com/bardisty/ficha/releases) before you upgrade. Before 1.0, a minor release can rename or remove keys, and [Compatibility](docs/machine-output.md#compatibility) says how that's announced.

To uninstall, delete the binary and undo the completion setup, if you did it: remove the completion file, or in PowerShell the `ficha completion` line from each `$PROFILE` you added it to. On Windows, also delete the `%LOCALAPPDATA%\Programs\ficha` folder and take it off your user `PATH` with `rundll32 sysdm.cpl,EditEnvironmentVariables`. ficha writes nothing else.

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

ficha can only report sessions whose transcripts still exist, and Claude Code deletes them after 30 days by default. [Troubleshooting](#troubleshooting) says how to keep more.

### Keys

`watch` and `breakdown` share these keys, and `breakdown` adds `p` and `s`. Press `?` in either view for this list. `?` or `esc` closes it. Any other key closes it too, and does what it always does. `ficha watch --help` and `ficha breakdown --help` list the keys as well.

| Key | Does |
| --- | --- |
| `q`, `ctrl+c` | quit |
| `?` | key list on/off; esc closes |
| `j/k`, `up/down` | scroll a line |
| `space/b`, `pgdn/pgup` | scroll a page |
| `d/u`, `ctrl+d/u` | scroll half a page |
| `g/G`, `home/end` | go to the top or bottom |
| `p` | breakdown: go to the next costliest row |
| `s` | breakdown: sort by cost on/off |
| `f` | follow new sessions on/off |
| `n` | switch to the newer session |
| `-` | back to the previous session |
| `r` | reload or retry |
| `ctrl+z` | suspend; needs a shell with job control, so not on Windows |

### Scrolling in tmux

ficha doesn't capture the mouse, so click-and-drag selection keeps working. Most terminals turn the wheel into arrow keys for full-screen programs, and those scroll `watch` and `breakdown`. tmux with `set -g mouse on` doesn't. In tmux 3.5 and older, wheel-up puts the pane in copy mode: the clock and totals stop, `[0/0]` shows in the corner, and ficha looks hung until you press `q` or scroll back down. From 3.6 the wheel does nothing. These two lines in `~/.tmux.conf` make the wheel send arrow keys to full-screen programs that don't use the mouse, and leave the rest of tmux's wheel handling as it was:

```tmux
bind -n WheelUpPane if -Ft= '#{||:#{mouse_any_flag},#{pane_in_mode}}' 'send -M' "if -Ft= '#{alternate_on}' 'send -t= -N 3 Up' 'copy-mode -et='"
bind -n WheelDownPane if -Ft= '#{||:#{mouse_any_flag},#{pane_in_mode}}' 'send -M' "if -Ft= '#{alternate_on}' 'send -t= -N 3 Down'"
```

Then, inside tmux, run `tmux source-file ~/.tmux.conf`. The bindings need tmux 2.6 or newer. `-t=` sends each command to the pane under the mouse. Without it, tmux before 3.0a runs the inner commands on the focused pane, which is often the other half of a `watch` and `breakdown` split.

## Commands

| Command | Description |
| --- | --- |
| `ficha [show]` | Show latest (or specified) session costs |
| `ficha watch` | Live monitoring with auto-follow |
| `ficha breakdown` | Live per-message cost table (scrollable) |
| `ficha list` | List sessions: when, length, model, agents, cost and title |
| `ficha summary` | Totals across this project's sessions |
| `ficha global` | Aggregated stats across all projects |
| `ficha version` | Print version information |

## Flags

Global flags, accepted by every command except `version` and `completion`:

| Flag | Description |
| --- | --- |
| `-f, --format <fmt>` | Output format: table, json, csv |
| `-v, --verbose` | Show debug information |
| `--no-color` | Disable colored output (same as setting `NO_COLOR`) |
| `--ascii` | Draw frames and symbols in plain ASCII instead of Unicode |

`watch` and `breakdown` render a terminal UI, so they reject `-f json` and `-f csv`.

On macOS and Linux, colors adapt to a light or dark terminal background, which ficha asks the terminal for. Inside tmux or screen it can't ask and assumes dark, so on a light background set `COLORFGBG='0;15'`. Quote it: the shell reads an unquoted `;` as the end of the command. On Windows it always uses the dark palette.

Color is off when output goes to a pipe or file. To keep it in a pager, set `CLICOLOR_FORCE=1`, as in `CLICOLOR_FORCE=1 ficha summary | less -R`; the pager gets the 16 basic colors. `ficha help environment` describes the environment variables ficha reads.

Per-command flags:

| Command | Flag | Description |
| --- | --- | --- |
| `show`, `watch`, `breakdown`, `list`, `summary` | `-p, --project <dir>` | Project directory (default: current dir) |
| `show`, `watch`, `breakdown`, `list`, `summary` | `--project-dir <name>` | Claude project dir name, instead of `-p` (bypass auto-detect) |
| `watch`, `breakdown` | `--no-follow` | Stay on the starting session instead of following new ones (live mode only) |
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

Each setup below writes only to your home directory. Run it once, then open a new shell.

bash:

```bash
mkdir -p ~/.local/share/bash-completion/completions
ficha completion bash > ~/.local/share/bash-completion/completions/ficha
```

The bash-completion package, version 2, loads that file the first time you press Tab after `ficha`. Without the package, Tab completes file names instead, and the older `source <(ficha completion bash)` fails with `_get_comp_words_by_ref: command not found`. Most desktop Linux installs have it loaded already. If `type _init_completion` in a new shell says not found, install it (`sudo apt-get install bash-completion` on Debian and Ubuntu), and if a new shell still says not found, add `. /usr/share/bash-completion/bash_completion` to `~/.bashrc`. On macOS, the system bash is 3.2, too old for bash-completion 2, so use zsh, the macOS default. Homebrew's `bash` and `bash-completion@2` work too, but only once that bash is the shell your terminal starts.

zsh:

```zsh
mkdir -p ~/.zfunc
ficha completion zsh > ~/.zfunc/_ficha
```

Then add `fpath=(~/.zfunc $fpath)` to `~/.zshrc`, above the line that runs `compinit` (with oh-my-zsh, above the line that sources `oh-my-zsh.sh`). If nothing in `~/.zshrc` runs `compinit`, add `autoload -Uz compinit && compinit` below the `fpath` line. If the new shell still doesn't complete `ficha`, `compinit` is working from a stale cache. Run `rm -f ~/.zcompdump*` and open another shell.

fish:

```fish
mkdir -p ~/.config/fish/completions
ficha completion fish > ~/.config/fish/completions/ficha.fish
```

PowerShell. Windows PowerShell 5.1 and PowerShell 7 keep separate profiles, so run this in each one you use:

```powershell
New-Item -ItemType Directory -Force (Split-Path $PROFILE) | Out-Null
Add-Content $PROFILE '', 'ficha completion powershell | Out-String | Invoke-Expression'
```

The empty string starts a new line, in case your profile doesn't end with one. If the new PowerShell then says running scripts is disabled, the execution policy is blocking your profile. `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned` lets it load.

For bash and zsh, `ficha completion <shell> --help` describes a system-wide setup instead, which needs root on Linux.

## Machine output (json / csv)

`-f` only picks the encoding. json is one document per run and csv one table with the same columns on every row, safe for `jq` and pandas. Table-only flags like `global --top` don't cut either one down. csv is the flat view: it leaves out nested detail such as the per-model split, which only json has.

- Unreadable input is counted, never swallowed: `skipped_sessions`, `skipped_agents`, `skipped_lines`, `estimated_cost_messages`.
- Agent spend is always split out: `jq '.parent_cost.total_cost + .agents_cost.total_cost == .total_cost.total_cost'` holds, up to float rounding.
- `cost_by_model` keys are canonical model IDs, so summing by key needs no normalization.
- ficha exits 0 when it wrote a report, even one that skipped unreadable input, 1 when it couldn't, and 2 when the command line is wrong. [Exit status](docs/machine-output.md#exit-status) has the details.

The full export contract is in [docs/machine-output.md](docs/machine-output.md). It covers each command's shape, flag interactions, record provenance, the counters, timestamps, per-message rows, what csv leaves out, and csv safety.

Before 1.0, a minor release can rename or remove keys. [Compatibility](docs/machine-output.md#compatibility) says how those changes are announced and how to pin a version.

## Scripting

`show`, `watch` and `breakdown` take a session ID or the path to a session's transcript. A path, or a full 36-character ID, works from any directory. Claude Code passes both to hooks and to the status line command, as `transcript_path` and `session_id` in the JSON on stdin.

In Claude Code's own status line, that JSON already has the session's total as `cost.total_cost_usd`. What ficha adds there is the split between the main conversation and its agents. It needs `jq`. Save it as `~/.claude/statusline.sh` and make it executable:

```sh
#!/bin/sh
ficha show "$(jq -r .transcript_path)" -f json 2>/dev/null |
  jq -r '"\(.parent_cost.total_cost) \(.agents_cost.total_cost)"' | {
  read -r main agents && LC_ALL=C printf 'main $%.2f, agents $%.2f\n' "$main" "$agents"
}
```

Then add the `statusLine` key to `~/.claude/settings.json`:

```json
{ "statusLine": { "type": "command", "command": "~/.claude/statusline.sh" } }
```

Outside Claude Code, a tmux status bar can show today's spend for the project in the current pane:

```tmux
set -g status-right '#(cd "#{pane_current_path}" && ficha summary --since today -f json 2>/dev/null | jq ".total_cost.total_cost * 100 | round / 100")'
```

In a directory with no sessions, ficha exits 1 and prints nothing on stdout, so the segment stays empty. If it's empty in a project directory too, the tmux server can't find `ficha` or `jq`. Check `tmux show-environment -g PATH`.

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

Unknown models are priced at $3 input and $15 output per million tokens with a 200K context, and ficha prints a warning naming the model so you know a number is a placeholder. The `<synthetic>` model Claude Code writes on API-error lines isn't unknown: those lines cost nothing and get no warning. When a new model ships, its prices need a ficha update, so the warning also names your version and links the releases page. [Upgrade](#upgrading-and-uninstalling) first. If the warning is still there afterwards, open a [pricing update issue](https://github.com/bardisty/ficha/issues/new?template=pricing_update.yml) with the model ID and the published rates, or send a PR. It's one catalog row plus tests, and [CONTRIBUTING.md](CONTRIBUTING.md#adding-a-models-pricing) walks through it.

Claude Code's transcript format is undocumented and can change between releases. ficha counts what it could not parse instead of guessing, so if the skip warnings or the `skipped_*` counters jump after a Claude Code update, that is the signal to file a bug.

## Troubleshooting

**"Claude Code has no sessions for …".** ficha looked in the wrong place. Run it from the directory Claude Code was started in, or pass that directory with `-p`. `-v` shows each place ficha looked.

**You run Claude Code in WSL.** Use the Linux build inside WSL, not the Windows one. [Install](#install) has the details, including reading a Windows install's sessions from WSL.

**Colors are hard to read on a light background in tmux or screen.** ficha can't ask the terminal for its background there and assumes dark. Run `export COLORFGBG='0;15'` first, as [Flags](#flags) describes.

**The mouse wheel freezes `watch` or `breakdown` in tmux.** See [Scrolling in tmux](#scrolling-in-tmux).

**Tab prints `_get_comp_words_by_ref: command not found`, or completes file names.** bash-completion isn't loaded. See [Shell completion](#shell-completion).

**History stops after 30 days.** Claude Code deletes transcripts older than that by default, through `cleanupPeriodDays` in `~/.claude/settings.json`. To keep more, raise it, as in `"cleanupPeriodDays": 365`. Don't use `0`. Current Claude Code rejects it, and older versions had [bugs](https://github.com/anthropics/claude-code/issues/59248) with it. Raising it doesn't bring back what's already been deleted.

**"Warning: unknown model".** Your ficha predates that model's prices. [Upgrade](#upgrading-and-uninstalling), and if the warning stays, open a [pricing update issue](https://github.com/bardisty/ficha/issues/new?template=pricing_update.yml).

**"Warning: … unparseable line(s) skipped".** Some transcript lines didn't match the format ficha knows, usually after a Claude Code update. `list`, `summary` and `global` name the affected sessions under `-v`. [File a bug](https://github.com/bardisty/ficha/issues/new?template=bug_report.yml) with your `claude --version`.

**Frames and symbols come out garbled**, for example as `lqqqk` and `x`. Something between ficha and the screen isn't in UTF-8 mode, usually a tmux or screen client started under a non-UTF-8 locale, which is common in containers and over SSH. Pass `--ascii`. To fix the locale instead, set it, for example `LANG=C.UTF-8`, in the shell you start or attach tmux or screen from, or start them with `tmux -u` or `screen -U`. Setting `LANG` inside the session changes nothing.

## Contributing

Bug reports and PRs are welcome. [CONTRIBUTING.md](CONTRIBUTING.md) covers setup, the `make check` and `make ci` gates, versioning, and how to add a model's pricing.

## License

[MIT](LICENSE)
