# Installs the latest ficha release for Windows, in PowerShell 5.1 or 7:
#
#   irm https://raw.githubusercontent.com/bardisty/ficha/main/install.ps1 | iex
#
# $env:FICHA_VERSION picks a release, as in 'v0.65.20'.
# $env:FICHA_INSTALL_DIR picks the folder, %LOCALAPPDATA%\Programs\ficha by
# default. The folder goes on your user PATH if it isn't there yet.
#
# The script block keeps its variables out of the session iex runs it in.

& {
    $ErrorActionPreference = 'Stop'
    # The progress bar makes 5.1 download very slowly.
    $ProgressPreference = 'SilentlyContinue'
    # 5.1 on older .NET may not offer TLS 1.2 by default, and GitHub needs it.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = 'bardisty/ficha'
    $file = 'ficha-windows-amd64.exe'

    $version = $env:FICHA_VERSION
    if (-not $version -or $version -eq 'latest') {
        $version = 'latest'
        $url = "https://github.com/$repo/releases/latest/download"
    } else {
        if (-not $version.StartsWith('v')) { $version = "v$version" }
        $url = "https://github.com/$repo/releases/download/$version"
    }
    $dir = $env:FICHA_INSTALL_DIR
    if (-not $dir) { $dir = Join-Path $env:LOCALAPPDATA 'Programs\ficha' }
    # An absolute path, since a relative or ~ entry in PATH means nothing
    # outside this PowerShell session.
    $dir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($dir).TrimEnd('\')
    if ($dir -match '^[A-Za-z]:$') { $dir += '\' }
    $exe = Join-Path $dir 'ficha.exe'

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
    $new = Join-Path $tmp $file
    New-Item -ItemType Directory $tmp | Out-Null
    try {
        "Downloading $file ($version)"
        foreach ($name in $file, 'checksums.txt') {
            try {
                Invoke-WebRequest "$url/$name" -OutFile (Join-Path $tmp $name) -UseBasicParsing
            } catch {
                throw "ficha install: couldn't download $url/$name ($($_.Exception.Message))"
            }
        }

        $want = Get-Content -LiteralPath (Join-Path $tmp 'checksums.txt') |
            ForEach-Object { $f = -split $_; if ($f[1] -eq $file -or $f[1] -eq "*$file") { $f[0] } } |
            Select-Object -First 1
        if (-not $want) { throw "ficha install: checksums.txt has no entry for $file" }
        if ((Get-FileHash -LiteralPath (Join-Path $tmp $file) -Algorithm SHA256).Hash -ne $want) {
            throw "ficha install: $file doesn't match checksums.txt, so nothing was installed"
        }

        # Stage the new exe beside the old one, so the swap below is a rename
        # on one volume and the trial run sees the same execution policy as
        # the installed copy.
        New-Item -ItemType Directory -Force $dir | Out-Null
        # Windows runs only files named .exe, so the staged copy keeps the
        # extension.
        $new = Join-Path $dir "ficha.new.$([IO.Path]::GetRandomFileName() -replace '\.', '').exe"
        Copy-Item -LiteralPath (Join-Path $tmp $file) $new
        # Try it before it replaces a working one. An exe that can't start at
        # all throws rather than setting LASTEXITCODE.
        $installed = $null
        try { $installed = & $new version } catch { }
        if (-not $installed -or $LASTEXITCODE -ne 0) {
            throw "ficha install: the new ficha won't run here, so nothing was changed"
        }

        # Windows won't overwrite a running exe, but it will rename one. A
        # ficha that's running keeps going from its parked copy, which a later
        # install deletes once nothing runs from it.
        Get-ChildItem -LiteralPath $dir -Filter 'ficha.exe.*.old' |
            ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force -ErrorAction SilentlyContinue }
        $parked = $null
        if (Test-Path -LiteralPath $exe) {
            $parked = "$exe.$([IO.Path]::GetRandomFileName()).old"
            Move-Item -LiteralPath $exe $parked
        }
        try {
            Move-Item -LiteralPath $new $exe
        } catch {
            if ($parked) { Move-Item -LiteralPath $parked $exe }
            throw
        }
        if ($parked) { Remove-Item -LiteralPath $parked -Force -ErrorAction SilentlyContinue }
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
        if (Test-Path -LiteralPath $new) { Remove-Item -LiteralPath $new -Force -ErrorAction SilentlyContinue }
    }
    "Installed $installed to $exe"

    # Edit the registry value rather than calling SetEnvironmentVariable,
    # which would expand entries like %USERPROFILE% for good and store the
    # value as a plain string.
    $key = Get-Item 'HKCU:\Environment'
    $userPath = $key.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
    $entries = @($userPath -split ';' | Where-Object { $_ })
    # A folder already on the machine PATH needs no user entry.
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $expanded = @($entries) + @($machinePath -split ';') | Where-Object { $_ } |
        ForEach-Object { [Environment]::ExpandEnvironmentVariables($_).TrimEnd('\') }
    if ($expanded -notcontains $dir.TrimEnd('\')) {
        $kind = 'ExpandString'
        if ($userPath) { $kind = $key.GetValueKind('Path') }
        Set-ItemProperty 'HKCU:\Environment' Path (($entries + $dir) -join ';') -Type $kind
        # Tell running programs the environment changed, so terminals opened
        # from Explorer or the Start menu see the new PATH.
        if (-not ('FichaInstall.Env' -as [type])) {
            Add-Type -Namespace FichaInstall -Name Env -MemberDefinition @'
[DllImport("user32.dll", CharSet = CharSet.Unicode)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@
        }
        $result = [UIntPtr]::Zero
        [void][FichaInstall.Env]::SendMessageTimeout([IntPtr]0xffff, 0x1a, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
        $env:Path = "$env:Path;$dir"
        ''
        "Added $dir to your user PATH. Terminals that were already open need a restart to find ficha."
    }

    $found = Get-Command ficha -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($found -and [IO.Path]::GetFullPath($found.Source) -ne [IO.Path]::GetFullPath($exe)) {
        ''
        "But 'ficha' runs $($found.Source), which comes earlier on your PATH."
        "Remove that one, or put $dir earlier on your PATH."
    }
}
