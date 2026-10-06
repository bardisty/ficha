# Installing by hand

The [install script](../README.md#install-script) does all of this for you. These steps do it one at a time, for when you'd rather not pipe a script into a shell.

## Prebuilt binaries

Each release on the [releases page](https://github.com/bardisty/ficha/releases) has one raw binary per platform, plus `checksums.txt`:

| File | Platform |
| --- | --- |
| `ficha-darwin-arm64` | macOS, Apple silicon |
| `ficha-darwin-amd64` | macOS, Intel |
| `ficha-linux-amd64` | Linux, x86-64 |
| `ficha-linux-arm64` | Linux, ARM64 |
| `ficha-windows-amd64.exe` | Windows, x86-64 |

There is no Windows ARM64 build. The Linux binaries are statically linked, so they also run on Alpine and other musl-based systems.

Each block below downloads the latest binary and `checksums.txt`, checks the binary's SHA-256, and installs it only if the check passes. Run the same block again later to upgrade.

## macOS and Linux

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

> [!IMPORTANT]
> Run `ficha version` next. If it says command not found, `~/.local/bin` isn't on your `PATH` yet. macOS never adds it, and Debian and Ubuntu add it at login only if it already existed. Add `export PATH="$HOME/.local/bin:$PATH"` to `~/.zshrc`, or for bash to `~/.bashrc` (`~/.bash_profile` on macOS), and open a new terminal.

If the check fails, the chain stops there. Nothing is installed, and both files stay where they are. To install for all users instead, swap the `mkdir` line for `sudo mkdir -p /usr/local/bin && sudo install -m 755 "$f" /usr/local/bin/ficha &&`.

The macOS binaries aren't notarized. That's fine with curl, but Gatekeeper blocks a binary downloaded in a browser on first run. Clear the flag before you install it, with `xattr -d com.apple.quarantine ficha-darwin-arm64` (or `-amd64`).

## Windows

This block is for Claude Code running on Windows itself. If Claude Code runs in WSL, use the Linux block inside WSL. In PowerShell, 5.1 or 7:

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

> [!IMPORTANT]
> The first time, add the folder to your user `PATH`, or `ficha` won't be found. `rundll32 sysdm.cpl,EditEnvironmentVariables` opens the Environment Variables window. Under the variables for your user, select `Path`, then Edit, New, and paste `%LOCALAPPDATA%\Programs\ficha`. Click OK in both windows, open a new terminal, and run `ficha version`.

If ficha is running, say in a `ficha watch` window, Windows won't let the block replace it. Quit ficha and run the block again.

## Verifying provenance

Releases from v0.24.0 on also carry a build provenance attestation, which ties each binary to the GitHub Actions run that built it from a tagged commit. Checking it needs the [GitHub CLI](https://cli.github.com), logged in with `gh auth login`:

```sh
gh attestation verify ~/.local/bin/ficha --repo bardisty/ficha
```

Point it at wherever you installed ficha. On Windows that's `"$env:LOCALAPPDATA\Programs\ficha\ficha.exe"`.
