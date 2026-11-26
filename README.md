# LazySVN

A lightweight, LazyGit-style terminal UI for Subversion (SVN), written in Go.

LazySVN drives the `svn` command-line client, so you need one on your `PATH`
(Homebrew and the Linux packages pull it in for you).

## Install

| Platform | Command |
|---|---|
| macOS / Linux | `curl -fsSL https://lazysvn.sawirstudio.com/install.sh \| sh` |
| Windows (PowerShell) | `irm https://lazysvn.sawirstudio.com/install.ps1 \| iex` |
| Homebrew (macOS / Linux) | `brew install sawirstudio/tap/lazysvn` |
| winget | `winget install SawirStudio.LazySVN` |
| Scoop | `scoop bucket add sawirstudio https://github.com/sawirstudio/scoop-bucket`<br>`scoop install lazysvn` |
| Debian / Ubuntu | `sudo apt install ./lazysvn_<version>_amd64.deb` (from [Releases](https://github.com/sawirstudio/lazysvn/releases)) |
| Fedora / RHEL | `sudo dnf install ./lazysvn-<version>-1.x86_64.rpm` |
| Alpine | `sudo apk add --allow-untrusted ./lazysvn_<version>_x86_64.apk` |
| Go | `go install github.com/sawirstudio/lazysvn@latest` |

Prebuilt binaries for linux, macOS and Windows on amd64 and arm64 are attached
to every [release](https://github.com/sawirstudio/lazysvn/releases) along with
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
- Filters changes by file name, path, and SVN status
- Shows `svn diff` for the selected file
- Shows `svn log -l 20` for the selected file
- Marks multiple files for actions
- Runs `svn commit`, `svn add`, `svn revert`, and `svn update`

Run `lazysvn` from inside an SVN working copy, or pass a relative or absolute
directory path, such as `lazysvn product`. If the path starts with a dash or
matches a command name, put `--` before it, as in `lazysvn -- update`.

## Develop

```bash
go run . /path/to/working-copy
go test ./...
go test -run '^$' -bench . -benchmem   # hot-path benchmarks
```

Each branch push runs the [Build workflow](.github/workflows/ci.yml). After
the tests pass, download the Linux, macOS, and Windows binaries for amd64 and
arm64 from the run's **Artifacts** section.

Releases are built by [GoReleaser](https://goreleaser.com) from
`.goreleaser.yaml` when a `v*` tag is pushed. Try the whole pipeline locally
with `goreleaser release --snapshot --clean`.

## Keybindings

- `j` / `k` or arrows: move
- `g` / `G`: top / bottom
- `Tab`: toggle between sorting by path and by SVN status
- `space`: mark/unmark file
- `Shift+V`: start a range at the current row. Move the cursor to preview the
  range (`+`), then press `Shift+V` or `space` to add it to the existing marks.
  Press `Esc` to cancel the range and keep the existing marks. With a filter
  active, the range includes only visible rows.
- `/`: edit the status and path filters at the bottom of the Changes pane
- `esc`: clear the active filter, or clear all marks when no filter is active
- `d` or `Enter`: diff selected file (the diff follows the cursor)
- `l`: show log for selected file
- `c`: commit marked files (or current file)
- `a`: add marked/selected unversioned files (`?`); already-versioned selections are skipped and reported
- `v`: revert marked/current files (the confirmation lists every path)
- `u`: update working copy
- `r`: refresh status
- `h` or `?`: help
- `q` or `ctrl+c`: quit (`ctrl+c` works inside prompts too)
- mouse wheel: scroll the pane under the pointer (the file list moves the
  selection; diff, log and help panes scroll on their own). Mouse reporting
  is on, so some terminals need `Shift` held to select text.

Only one working-copy-changing command (`update`, `add`, `revert`, `commit`) runs at a time; other svn commands wait until it finishes.

Path sorting uses working-copy-relative paths. Status sorting lists modified
(`M`) entries first, added (`A`) entries next, then the other status codes in
ascending order. Each status group is sorted by path. LazySVN remembers the
sort order across working copies after a normal exit.

### Filtering changes

Press `/` to edit the filter bar at the bottom of the Changes pane. The
status field is focused first. Use `Tab` or `Shift+Tab` to switch between
status and file path, `Enter` to apply, and `Esc` to cancel editing. The bar
shows the active filters after applying them. Leave either field blank to
match any value in that field. Filters apply to the current session.

Path matching is case-insensitive. Enter `camera.go` to match
`src/camera.go`, or enter `src/` to match changes under that directory.
The status field accepts SVN codes such as `M`, `A`, and `?` or names such as
`modified` (also `modify`), `added`, and `unversioned`. Separate statuses with
commas or spaces to match any of them. For example, path `src/` and status
`modify,A` show modified or added entries under `src/`.

While filtering, commit, add, and revert select visible marked paths or fall
back to the current row. Clear the filter to see hidden marks again. Commit
and revert confirmations also show any paths required by SVN, such as the
other side of a move.

## More from Sawir Studio

lazysvn is made by [Sawir Studio LLC](https://sawirstudio.com/?utm_source=lazysvn&utm_medium=referral), which also makes
[Sawirly](https://sawirly.com/?utm_source=lazysvn&utm_medium=referral) (a privacy-first URL shortener),
[SawirPost](https://sawirpost.com/?utm_source=lazysvn&utm_medium=referral) (publish one post to 25+ social channels),
[Rindraw](https://rindraw.com/?utm_source=lazysvn&utm_medium=referral) (drawing with editable strokes for iPad, iPhone and Mac)
and [more](https://sawirstudio.com/projects/?utm_source=lazysvn&utm_medium=referral).
