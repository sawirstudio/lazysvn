# LazySVN installer for Windows.
#
#   irm https://lazysvn.sawirstudio.com/install.ps1 | iex
#
# Options via environment variables:
#   VERSION=<tag>              Install a specific release tag (default: latest)
#   INSTALL_DIR=<path>         Install directory (default: %LOCALAPPDATA%\Programs\lazysvn)
#   LAZYSVN_REPO=<owner/repo>  Release source repo (default: sawirstudio/lazysvn)
#   GITHUB_TOKEN=<token>       Optional token to avoid GitHub API rate limits
#
# Works on Windows PowerShell 5.1 and PowerShell 7. Everything runs inside a
# function so `iex` does not change preferences in the caller's session, and
# errors are thrown (never `exit`) so a failure can't close the user's window.

function Install-LazySVN {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # the progress bar makes 5.1 downloads crawl
    Set-StrictMode -Version 2.0

    # Windows PowerShell 5.1 may not offer TLS 1.2 by default; GitHub requires it.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = if ($env:LAZYSVN_REPO) { $env:LAZYSVN_REPO } else { 'sawirstudio/lazysvn' }
    $version = if ($env:VERSION) { $env:VERSION } else { 'latest' }
    $installDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\lazysvn' }

    # A 32-bit PowerShell on 64-bit Windows reports x86 and puts the real
    # architecture in PROCESSOR_ARCHITEW6432.
    $machine = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    switch ($machine) {
        'AMD64' { $arch = 'amd64' }
        'ARM64' { $arch = 'arm64' }
        default { throw "lazysvn: unsupported architecture '$machine' (supported: AMD64, ARM64)" }
    }

    $headers = @{ 'User-Agent' = 'lazysvn-installer'; 'Accept' = 'application/vnd.github+json' }
    if ($env:GITHUB_TOKEN) { $headers['Authorization'] = "Bearer $($env:GITHUB_TOKEN)" }
    $api = if ($version -eq 'latest') {
        "https://api.github.com/repos/$repo/releases/latest"
    } else {
        "https://api.github.com/repos/$repo/releases/tags/$version"
    }

    Write-Host "==> Resolving $repo release ($version) for windows/$arch"
    $release = Invoke-RestMethod -Uri $api -Headers $headers -UseBasicParsing
    $asset = $release.assets | Where-Object { $_.name -match "^lazysvn_.+_windows_$arch\.zip$" } | Select-Object -First 1
    if (-not $asset) { throw "lazysvn: release $($release.tag_name) has no windows/$arch build" }
    $sums = $release.assets | Where-Object { $_.name -eq 'checksums.txt' } | Select-Object -First 1

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("lazysvn-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        $zip = Join-Path $tmp $asset.name
        Write-Host "==> Downloading $($asset.browser_download_url)"
        Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zip -Headers @{ 'User-Agent' = 'lazysvn-installer' } -UseBasicParsing

        if ($sums) {
            $sumsFile = Join-Path $tmp 'checksums.txt'
            Invoke-WebRequest -Uri $sums.browser_download_url -OutFile $sumsFile -Headers @{ 'User-Agent' = 'lazysvn-installer' } -UseBasicParsing
            $line = Get-Content $sumsFile | Where-Object { $_ -match "\s\*?$([regex]::Escape($asset.name))$" } | Select-Object -First 1
            if (-not $line) { throw "lazysvn: $($asset.name) is missing from checksums.txt" }
            $expected = ($line -split '\s+')[0].ToLowerInvariant()
            $actual = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash.ToLowerInvariant()
            if ($expected -ne $actual) { throw "lazysvn: checksum mismatch for $($asset.name) (expected $expected, got $actual)" }
            Write-Host '==> Checksum verified'
        }

        $extract = Join-Path $tmp 'x'
        Expand-Archive -Path $zip -DestinationPath $extract
        $exe = Get-ChildItem -Path $extract -Recurse -Filter 'lazysvn.exe' | Select-Object -First 1
        if (-not $exe) { throw 'lazysvn: lazysvn.exe not found in the release archive' }

        New-Item -ItemType Directory -Force -Path $installDir | Out-Null
        $target = Join-Path $installDir 'lazysvn.exe'
        $old = "$target.old"
        # A running .exe can't be overwritten, but it can be renamed. This is
        # what lets `lazysvn update` replace itself.
        if (Test-Path -LiteralPath $old) { Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue }
        if (Test-Path -LiteralPath $target) { Move-Item -LiteralPath $target -Destination $old -Force }
        Copy-Item -LiteralPath $exe.FullName -Destination $target -Force
        Write-Host "==> Installed lazysvn to $target"
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = @()
    if ($userPath) { $entries = $userPath -split ';' | Where-Object { $_ } }
    $onPath = $entries | Where-Object { $_.TrimEnd('\') -ieq $installDir.TrimEnd('\') }
    if (-not $onPath) {
        [Environment]::SetEnvironmentVariable('Path', (($entries + $installDir) -join ';'), 'User')
        $env:Path = "$env:Path;$installDir"
        Write-Host "==> Added $installDir to your user PATH (open a new terminal to pick it up)"
    }

    if (-not (Get-Command svn -ErrorAction SilentlyContinue)) {
        Write-Host '==> Note: no svn command found. lazysvn needs a Subversion command-line client on PATH,'
        Write-Host '    for example SlikSVN, or TortoiseSVN installed with its command line client tools.'
    }
    Write-Host "==> Run: lazysvn  (uninstall: lazysvn uninstall)"
}

Install-LazySVN
