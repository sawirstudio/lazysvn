# LazySVN

A lightweight, LazyGit-style terminal UI for Subversion (SVN), written in Go.

LazySVN drives the `svn` command-line client, so you need one on your `PATH`
(Homebrew and the Linux packages pull it in for you).

## Install

| Platform | Command |
|---|---|
| macOS / Linux | `curl -fsSL https://lazysvn.sawirstudio.com/install.sh \| sh` |
| Windows (PowerShell) | `irm https://lazysvn.sawirstudio.com/install.ps1 \| iex` |
| Homebrew (macOS / Linux) | `brew install sawirricardo/tap/lazysvn` |
| winget | `winget install SawirStudio.LazySVN` |
| Scoop | `scoop bucket add sawirricardo https://github.com/sawirricardo/scoop-bucket`<br>`scoop install lazysvn` |
| Debian / Ubuntu | `sudo apt install ./lazysvn_<version>_amd64.deb` (from [Releases](https://github.com/sawirricardo/lazysvn/releases)) |
| Fedora / RHEL | `sudo dnf install ./lazysvn-<version>-1.x86_64.rpm` |
| Alpine | `sudo apk add --allow-untrusted ./lazysvn_<version>_x86_64.apk` |
| Go | `go install github.com/sawirricardo/lazysvn@latest` |

Prebuilt binaries for linux, macOS and Windows on amd64 and arm64 are attached
to every [release](https://github.com/sawirricardo/lazysvn/releases) along with
`checksums.txt`.

The install scripts take options as environment variables:

```bash
# Install to a custom directory
curl -fsSL https://lazysvn.sawirstudio.com/install.sh | INSTALL_DIR=/usr/local/bin sh

# Install a specific tag
curl -fsSL https://lazysvn.sawirstudio.com/install.sh | VERSION=v0.4.0 sh
```

```powershell
$env:VERSION = 'v0.4.0'; irm https://lazysvn.sawirstudio.com/install.ps1 | iex
```

`install.ps1` installs to `%LOCALAPPDATA%\Programs\lazysvn`, adds it to your
user `PATH`, and verifies the download against `checksums.txt`.

### Updating and uninstalling

Use the tool you installed with: `brew upgrade lazysvn`, `winget upgrade
SawirStudio.LazySVN`, `scoop update lazysvn`, or your system package manager.

Installs made with the curl or PowerShell scripts can update and remove
themselves:

```bash
lazysvn update          # latest release
lazysvn update v0.4.0   # a specific tag
lazysvn uninstall
```

If `lazysvn update` or `lazysvn uninstall` finds that a package manager owns
the binary, it prints that package manager's command instead of touching it.

## What it does

- Shows `svn status` in a navigable list
- Shows `svn diff` for the selected file
- Shows `svn log -l 20` for the selected file
- Marks multiple files for actions
- Runs `svn commit`, `svn add`, `svn revert`, and `svn update`

Run `lazysvn` from inside an SVN working copy.

## Develop

```bash
go run .
go test ./...
go test -run '^$' -bench . -benchmem   # hot-path benchmarks
```

Releases are built by [GoReleaser](https://goreleaser.com) from
`.goreleaser.yaml` when a `v*` tag is pushed. Try the whole pipeline locally
with `goreleaser release --snapshot --clean`.

## Keybindings

- `j` / `k` or arrows: move
- `g` / `G`: top / bottom
- `space`: mark/unmark file
- `esc`: clear all marks
- `d` or `Enter`: diff selected file (the diff follows the cursor)
- `l`: show log for selected file
- `c`: commit marked files (or current file)
- `a`: add marked/selected unversioned files (`?`); already-versioned selections are skipped and reported
- `v`: revert marked/current files (the confirmation lists every path)
- `u`: update working copy
- `r`: refresh status
- `h` or `?`: help
- `q` or `ctrl+c`: quit (`ctrl+c` works inside prompts too)

Only one working-copy-changing command (`update`, `add`, `revert`, `commit`) runs at a time; other svn commands wait until it finishes.
