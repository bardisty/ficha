# ficha

ficha shows what your Claude Code sessions would cost at API prices. It breaks the cost down by message and by agent, totals it per project, and tracks token usage and how full the context window is. `watch` and `breakdown` update live as Claude works.

The figures use Anthropic's list prices. If you pay per token, they should come close to your bill, apart from the few charges [ficha doesn't model](#how-ficha-prices-a-session). On a subscription, they show what the same work would cost through the API.

ficha only reads the transcripts Claude Code keeps on your machine. It makes no network requests and writes no files. It's an independent project, not affiliated with or endorsed by Anthropic.

<table>
  <tr>
    <td width="50%" valign="top"><a href="docs/ficha-watch.webp"><img src="docs/ficha-watch.webp" alt="ficha watch, the live session cost dashboard"></a></td>
    <td width="50%" valign="top"><a href="docs/ficha-breakdown.webp"><img src="docs/ficha-breakdown.webp" alt="ficha breakdown, the per-message cost table"></a></td>
  </tr>
  <tr>
    <td valign="top"><em><code>ficha watch</code> is the live dashboard: cost by token type, context-window use, cache savings, the cost trend, and cost per model and per agent.</em></td>
    <td valign="top"><em><code>ficha breakdown</code> is a live, scrollable table of every message, with its cost, its tokens and the agent it belongs to. Rows from the main conversation and its agents interleave in time order.</em></td>
  </tr>
</table>

## Quick start

1. Install ficha on the machine where Claude Code runs, or inside WSL if Claude Code runs there. On macOS and Linux:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/bardisty/ficha/main/install.sh | sh
   ```

   If it prints a line to add to your `PATH`, run that line and open a new terminal. [Install](#install) has the line for Windows and other ways to install.

2. Go to a directory you've started Claude Code in, and look at its sessions:

   ```sh
   cd ~/code/my-project
   ficha            # the latest session: cost, tokens, context, agents
   ficha list       # every session here, newest first
   ficha summary    # this project's totals
   ficha global     # totals for every project
   ```

3. While Claude works, open a second terminal in the same directory and start a live view:

   ```sh
   ficha watch      # session totals, following each new session
   ficha breakdown  # each message's cost, tokens and agent, as it arrives
   ```

   Press `?` for the keys and `q` to quit.

If ficha says Claude Code has no sessions there, run it from the directory you started Claude Code in, not a subdirectory. [Troubleshooting](#troubleshooting) covers this and the other errors a first run can hit.

## Install

Install ficha where Claude Code runs, since it reads the transcripts Claude Code keeps in that machine's home directory.

ficha runs on Linux, macOS and Windows, and CI tests all three. It has been checked against the transcripts of Claude Code 2.1.

### Install script

On macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/bardisty/ficha/main/install.sh | sh
```

On Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/bardisty/ficha/main/install.ps1 | iex
```

The script downloads the latest binary for your platform, checks it against the release's `checksums.txt`, and installs it only if they match. It installs to `~/.local/bin`. On Windows it installs to `%LOCALAPPDATA%\Programs\ficha` and adds that folder to your user `PATH`. The shell script doesn't edit your startup files, so if `~/.local/bin` isn't on your `PATH`, it prints the line to add. Run the script again to upgrade.

To install a particular release, put `FICHA_VERSION=v0.65.20` before `sh`. `FICHA_INSTALL_DIR` picks the folder the same way. In PowerShell, set them first, as in `$env:FICHA_VERSION = 'v0.65.20'`.

You can read [install.sh](install.sh) and [install.ps1](install.ps1) before running them. To install without them, or to check a binary's build provenance, see [Installing by hand](docs/install.md).

### mise

With [mise](https://mise.jdx.dev):

```sh
mise use -g github:bardisty/ficha
```

### From source

With Go 1.26.8 or newer:

```sh
go install github.com/bardisty/ficha@latest
```

This puts `ficha` in `$(go env GOPATH)/bin`, usually `~/go/bin`, or in `$GOBIN` if you set it. Add that folder to your `PATH` if `ficha` isn't found. To install a particular release, replace `@latest` with its tag from the [releases page](https://github.com/bardisty/ficha/releases), such as `@v0.52.0`.

### WSL

If Claude Code runs on Windows and you want ficha's reports inside WSL, set `CLAUDE_CONFIG_DIR` for that one command:

```sh
CLAUDE_CONFIG_DIR=/mnt/c/Users/<you>/.claude ficha global
```

Don't export the variable from your shell profile. Claude Code in WSL reads it too, and would start keeping its own settings and sessions in the Windows folder.

For a single project, run ficha from a folder with the same name as the Windows project, such as the project's own folder under `/mnt/c`. ficha matches it by name and prints a note saying so. Every command works this way, but across the mount `watch` and `breakdown` check for changes every 2 seconds, so a new message or session can take up to 2 seconds to show.

### Upgrading and uninstalling

To upgrade, run the install script again, `mise upgrade`, or `go install github.com/bardisty/ficha@latest`. `ficha version` prints the version you have.

ficha never checks for updates, because it makes no network requests. To hear about new releases, watch the repository for releases only (Watch, Custom, Releases), follow the [releases feed](https://github.com/bardisty/ficha/releases.atom), or run `gh release list -R bardisty/ficha`.

If you script against `-f json` or `-f csv`, read the [release notes](https://github.com/bardisty/ficha/releases) before you upgrade. Before 1.0, a minor release can rename or remove keys.

To uninstall, delete the binary, or with mise run `mise unuse -g github:bardisty/ficha`. If you set up shell completion, remove the completion file, or in PowerShell the `ficha completion` line from each `$PROFILE` you added it to. On Windows, also delete the `%LOCALAPPDATA%\Programs\ficha` folder and remove it from your user `PATH` in the Environment Variables window, which `rundll32 sysdm.cpl,EditEnvironmentVariables` opens. ficha writes nothing else.

## Usage

ficha shows the sessions of the project you're in, so run it from the directory you started Claude Code in. To check on another project without leaving this one, add `-p` and that project's directory.

| Command | Shows |
| --- | --- |
| `ficha [show]` | The latest session, or the one you name: cost, tokens, context and agents |
| `ficha watch` | The latest session as a live dashboard, switching to each new session as it starts |
| `ficha breakdown` | Every message, live, with its agent, model, cost and tokens. Sort by cost or step through the costliest to see what drove the total |
| `ficha list` | This project's sessions: when, length, model, agents, cost and title |
| `ficha summary` | Totals across this project's sessions |
| `ficha global` | Totals across every project |
| `ficha statusline` | A line for Claude Code's status line: model, context, recent cost per message and the session's total |
| `ficha version` | The version you have |

`show`, `watch` and `breakdown` take a session ID, the start of one, or the path to a transcript. A path, or a full 36-character ID, works from any directory.

```sh
ficha show abc123             # the session whose ID starts with abc123
ficha watch abc123            # watch that session and no other
ficha list -p ~/code/api      # another project's sessions
ficha summary --since 7d      # this project, the last 7 days
ficha global --top 5          # the 5 costliest projects
ficha global --since today    # every project, today
ficha -f json > out.json      # the latest session as json
ficha summary -d -f csv       # one csv row per session
ficha show -f csv --messages  # one csv row per message
```

ficha can only report sessions whose transcripts still exist, and Claude Code deletes them after 30 days by default. [Troubleshooting](#troubleshooting) says how to keep more.

### Live views

Run `watch` and `breakdown` side by side, with session totals in one terminal and per-message costs in the other. When a new session starts, after `/clear` or a restart, both switch to it. A session ID, a transcript path or `--no-follow` keeps them on one session.

Press `?` in either view for the keys. `breakdown` has all of `watch`'s keys, plus `p` and `s`.

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

In tmux with `set -g mouse on`, the mouse wheel freezes both views or does nothing until you add two lines to `~/.tmux.conf`. [Scrolling in tmux](#scrolling-in-tmux) has them.

### Flags

Every command except `version` and `completion` takes these:

| Flag | Does |
| --- | --- |
| `-f, --format <fmt>` | Output format: `table` (the default), `json` or `csv` |
| `-v, --verbose` | Show debug information, such as each place ficha looked for sessions |
| `--no-color` | Turn color off, the same as setting `NO_COLOR` |
| `--ascii` | Draw frames and symbols in plain ASCII instead of Unicode |

`watch` and `breakdown` draw a terminal UI, so they reject `-f json` and `-f csv`.

The rest belong to particular commands:

| Flag | Commands | Does |
| --- | --- | --- |
| `-p, --project <dir>` | `show`, `watch`, `breakdown`, `list`, `summary` | The project directory (default: the current one) |
| `--project-dir <name>` | `show`, `watch`, `breakdown`, `list`, `summary` | The project's folder name under `~/.claude/projects`, used as is instead of `-p` |
| `--no-follow` | `watch`, `breakdown` | Stay on the starting session instead of following new ones |
| `--messages` | `show` | Add per-message records: a `messages` array in json, one row per message in csv |
| `-d, --details` | `summary` | Add a per-session breakdown |
| `--expand-agents` | `summary` | Add each session's agents (needs `--details`) |
| `-d, --details` | `global` | Show every project in the table, with a cumulative column when sorted by cost |
| `-n, --top <n>` | `global` | How many projects the table shows (default 10) |
| `--sort-by <key>` | `global` | Sort by `cost` (the default), `sessions`, `name` or `activity` |
| `--since <when>` | `summary`, `global` | Count only messages from then on: `2026-09-01`, `today`, `7d`, `12h` |
| `--until <when>` | `summary`, `global` | Count only messages before then; a date includes that whole day |

Bare `ficha` works like `show` and takes the same project flags. A flag on a command that doesn't use it is an error. `--no-follow` and `--messages` print a warning where they'd have no effect.

### Color

On macOS and Linux, ficha asks the terminal for its background and picks a light or dark palette to match. Inside tmux or screen it can't ask and assumes dark, so on a light background set `COLORFGBG='0;15'`. Quote it, because the shell reads an unquoted `;` as the end of the command. On Windows ficha always uses the dark palette.

Color is off when output goes to a pipe or file. To keep it in a pager, set `CLICOLOR_FORCE=1`, as in `CLICOLOR_FORCE=1 ficha summary | less -R`. `ficha help environment` lists the other variables that turn color on or off.

## Shell completion

`ficha completion <shell>` prints a completion script for bash, zsh, fish or PowerShell. It completes commands, flag values (`-f`, `--sort-by`, directories for `-p`) and session IDs. In shells that show descriptions, each session ID shows how long ago it was modified, and often its message count.

Each setup below writes only to your home directory. Run it once, then open a new shell. For bash and zsh, `ficha completion <shell> --help` describes a system-wide setup instead, which needs root on Linux.

<details>
<summary>bash</summary>

```bash
mkdir -p ~/.local/share/bash-completion/completions
ficha completion bash > ~/.local/share/bash-completion/completions/ficha
```

The bash-completion package, version 2, loads that file the first time you press Tab after `ficha`. Most desktop Linux installs have it loaded already. To check, run `type _init_completion` in a new shell. If it says not found, install the package (`sudo apt-get install bash-completion` on Debian and Ubuntu). If a new shell still says not found, add `. /usr/share/bash-completion/bash_completion` to `~/.bashrc`.

Without the package, Tab completes file names instead, and the older `source <(ficha completion bash)` fails with `_get_comp_words_by_ref: command not found`.

On macOS, the system bash is 3.2, too old for bash-completion 2, so use zsh, the macOS default. Homebrew's `bash` and `bash-completion@2` work too, but only once that bash is the shell your terminal starts.

</details>

<details>
<summary>zsh</summary>

```zsh
mkdir -p ~/.zfunc
ficha completion zsh > ~/.zfunc/_ficha
```

Then add `fpath=(~/.zfunc $fpath)` to `~/.zshrc`, above the line that runs `compinit`. With oh-my-zsh, put it above the line that sources `oh-my-zsh.sh`. If nothing in `~/.zshrc` runs `compinit`, add `autoload -Uz compinit && compinit` below the `fpath` line.

If the new shell still doesn't complete `ficha`, `compinit` is working from a stale cache. Run `rm -f ~/.zcompdump*` and open another shell.

</details>

<details>
<summary>fish</summary>

```fish
mkdir -p ~/.config/fish/completions
ficha completion fish > ~/.config/fish/completions/ficha.fish
```

</details>

<details>
<summary>PowerShell</summary>

Windows PowerShell 5.1 and PowerShell 7 keep separate profiles, so run this in each one you use:

```powershell
New-Item -ItemType Directory -Force (Split-Path $PROFILE) | Out-Null
Add-Content $PROFILE '', 'ficha completion powershell | Out-String | Invoke-Expression'
```

The empty string starts a new line, in case your profile doesn't end with one. If the new PowerShell then says running scripts is disabled, the execution policy is blocking your profile. `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned` lets it load.

</details>

Claude project directory names start with `-`, so complete them with `--project-dir=<Tab>`. Without the `=`, the shell reads what you've typed as a flag.

## Scripting

### json and csv

`-f` only picks the encoding, and table-only flags like `global --top` don't shorten json or csv. json is one document per run, ready for `jq`. csv is one table with the same columns on every row, ready for pandas. csv is also flat, so it leaves out nested detail such as the per-model split, which only json has.

- ficha counts unreadable input instead of dropping it: `skipped_sessions`, `skipped_agents`, `skipped_lines`, `estimated_cost_messages`.
- Agent spend is always split out: `jq '.parent_cost.total_cost + .agents_cost.total_cost == .total_cost.total_cost'` holds, up to float rounding.
- `cost_by_model` keys are canonical model IDs, so summing by key needs no normalization.
- ficha exits 0 when it wrote a report, even one that skipped unreadable input, 1 when it couldn't, and 2 when the command line is wrong. [Exit status](docs/machine-output.md#exit-status) has the details.

[docs/machine-output.md](docs/machine-output.md) is the full export contract. Before 1.0, a minor release can rename or remove keys. [Compatibility](docs/machine-output.md#compatibility) says how those changes are announced and how to pin a version.

### Claude Code's status line and hooks

Claude Code passes `session_id` and `transcript_path` to hooks and to the status line command, in the JSON on stdin. `show`, `watch` and `breakdown` take either one.

`ficha statusline` prints a line for Claude Code's status line: the model, how full the context window is, what recent messages cost, and the session's total with the agents' share:

```text
Opus 5.5 · ctx 68% · $0.19/msg ▲ · $49.20 (agents $18.07)
```

`$0.19/msg` is the average cost of recent messages in the main conversation. `▲` or `▼` means that's more than 20% above or below the session's average. The line leaves out the context before the first reply, the recent cost before the sixth message, and the agents' share when no agents ran.

Add it to `~/.claude/settings.json`:

```json
{ "statusLine": { "type": "command", "command": "ficha statusline" } }
```

### tmux status bar

A tmux status bar can show today's spend for the project in the current pane:

```tmux
set -g status-right '#(cd "#{pane_current_path}" && ficha summary --since today -f json 2>/dev/null | jq ".total_cost.total_cost * 100 | round / 100")'
```

In a directory with no sessions, ficha exits 1 and prints nothing on stdout, so the segment stays empty. If it's empty in a project directory too, the tmux server can't find `ficha` or `jq`. Check `tmux show-environment -g PATH`.

## How ficha prices a session

ficha parses the JSONL transcripts and multiplies each message's token counts by the bundled list price for its model. Cache writes and cache reads have prices of their own. Cache reads get a steep discount: 90% off the input rate on most models, 95% on Opus 5.5, and 97.5% on Fable 5.1 and Mythos 5.1. ficha shows the difference as Savings.

Agents count too, both regular subagents and Claude Code Workflow agents. ficha groups workflow agents by run, with the run's name and status taken from its metadata.

`ficha help output` explains the terms on screen: cache TTLs, Savings, messages, insights and Context.

ficha does not model:

- Long-context premium pricing. On models older than Claude 4.6, a 1M-context request whose input passes 200K tokens is billed at a higher rate. ficha widens the context window it reports but prices every token at the base rate. Claude 4.6 and later have no premium.
- Fast mode, which Anthropic bills at a premium.
- Web search, which Anthropic charges per search on top of the tokens.
- Bedrock and Vertex billing. ficha recognizes their model IDs and prices them at Anthropic's first-party rates. Both platforms bill on their own terms, so treat the figures as a proxy.

ficha prices unknown models at $3 input and $15 output per million tokens with a 200K context, and prints a warning naming the model so you know the number is a placeholder. [Troubleshooting](#troubleshooting) says what to do about it. Claude Code writes the model `<synthetic>` on API-error lines. Those lines cost nothing and get no warning.

Claude Code's transcript format is undocumented and can change between releases. ficha counts what it could not parse instead of guessing, so if the skip warnings or the `skipped_*` counters jump after a Claude Code update, that is the signal to file a bug.

## Privacy

ficha only reads. It opens the transcripts Claude Code writes under `~/.claude/projects/`, or under `$CLAUDE_CONFIG_DIR/projects/` if you have set that variable. `CLAUDE_CONFIG_DIR` is Claude Code's own override, and ficha honors it so the two always agree on where sessions live. ficha makes no network requests, runs no subprocesses, and writes no files. Nothing in the non-test code imports `net/http` or `os/exec` or opens a file for writing.

Those transcripts contain your prompts and whatever code Claude read, so they are sensitive. The only thing that leaves your machine is what you choose to paste from ficha's output.

## Troubleshooting

**"Claude Code has no sessions for …".** ficha looked in the wrong place. Run it from the directory you started Claude Code in, or pass that directory with `-p`. `-v` shows each place ficha looked. If the message ends in "yet", ficha found the project, but it holds no transcripts. Claude Code hasn't written one yet, or deleted them after 30 days, as described below.

**"no Claude Code data in …".** ficha found no projects where it looked. Usually ficha runs somewhere Claude Code doesn't, such as outside the container or WSL install Claude Code runs in. Or `CLAUDE_CONFIG_DIR` points at the wrong folder. See [Install](#install).

**History stops after 30 days.** Claude Code deletes transcripts older than that by default, through `cleanupPeriodDays` in `~/.claude/settings.json`. To keep more, raise it, as in `"cleanupPeriodDays": 365`. Don't use `0`. Current Claude Code rejects it, and older versions had [bugs](https://github.com/anthropics/claude-code/issues/59248) with it. Raising it doesn't bring back what's already been deleted.

**"Warning: unknown model".** Your ficha predates that model's prices, so it's using placeholder rates. The warning names your version and links the releases page. [Upgrade](#upgrading-and-uninstalling) first. If the warning is still there afterwards, open a [pricing update issue](https://github.com/bardisty/ficha/issues/new?template=pricing_update.yml) with the model ID and the published rates, or send a PR. It's one catalog row plus tests, and [CONTRIBUTING.md](CONTRIBUTING.md#adding-a-models-pricing) walks through it.

**"Warning: … unparseable line(s) skipped".** Some transcript lines didn't match the format ficha knows, usually after a Claude Code update. `list`, `summary` and `global` name the affected sessions under `-v`. [File a bug](https://github.com/bardisty/ficha/issues/new?template=bug_report.yml) with your `claude --version`.

**You run Claude Code in WSL.** Use the Linux build inside WSL. [WSL](#wsl) covers reading a Windows install's sessions from there.

<a name="scrolling-in-tmux"></a>**In tmux, the mouse wheel freezes `watch` or `breakdown`, or doesn't scroll them.** ficha doesn't capture the mouse, so click-and-drag selection keeps working. Most terminals turn the wheel into arrow keys for full-screen programs, and those scroll `watch` and `breakdown`. tmux with `set -g mouse on` doesn't. In tmux 3.5 and older, wheel-up puts the pane in copy mode: the clock and totals stop, `[0/0]` shows in the corner, and ficha looks hung until you press `q` or scroll back down. From 3.6 the wheel does nothing. These two lines in `~/.tmux.conf` make the wheel send arrow keys to full-screen programs that don't use the mouse, and leave the rest of tmux's wheel handling as it was:

```tmux
bind -n WheelUpPane if -Ft= '#{||:#{mouse_any_flag},#{pane_in_mode}}' 'send -M' "if -Ft= '#{alternate_on}' 'send -t= -N 3 Up' 'copy-mode -et='"
bind -n WheelDownPane if -Ft= '#{||:#{mouse_any_flag},#{pane_in_mode}}' 'send -M' "if -Ft= '#{alternate_on}' 'send -t= -N 3 Down'"
```

Then, inside tmux, run `tmux source-file ~/.tmux.conf`. The bindings need tmux 2.6 or newer. `-t=` sends each command to the pane under the mouse. Without it, tmux before 3.0a runs the inner commands on the focused pane, which is often the other half of a `watch` and `breakdown` split.

**Colors are hard to read on a light background in tmux or screen.** ficha can't ask the terminal for its background there and assumes dark. Run `export COLORFGBG='0;15'` first, as [Color](#color) describes.

**Frames and symbols come out garbled**, for example as `lqqqk` and `x`. Something between ficha and the screen isn't in UTF-8 mode, usually a tmux or screen client started under a non-UTF-8 locale, which is common in containers and over SSH. Pass `--ascii`. To fix the locale instead, set it, for example `LANG=C.UTF-8`, in the shell you start or attach tmux or screen from, or start them with `tmux -u` or `screen -U`. Setting `LANG` inside the session changes nothing.

**Tab prints `_get_comp_words_by_ref: command not found`, or completes file names.** bash-completion isn't loaded. Expand the bash block under [Shell completion](#shell-completion).

## Contributing

Bug reports and PRs are welcome. [CONTRIBUTING.md](CONTRIBUTING.md) covers setup, the `make check` and `make ci` gates, versioning, and how to add a model's pricing.

## License

[MIT](LICENSE)
