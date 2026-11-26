package main

import (
	"fmt"
	"os"
	"regexp"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

var pseudoVersionRE = regexp.MustCompile(`^v\d+\.\d+\.\d+-\d{14}-[0-9a-f]{12,}(?:\+incompatible)?(?:\+dirty)?$`)

// version is set at build time via -ldflags "-X main.version=<value>".
var version = "dev"

func main() {
	os.Exit(runCLI(os.Args[1:], runTUI))
}

func runTUI() int {
	m := initialModel()
	path, configErr := sortOrderPath()
	if configErr == nil {
		m.sortOrder = loadSortOrder(path)
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lazysvn failed: %v\n", err)
		return 1
	}
	if configErr == nil {
		configErr = saveSortOrder(path, result.(model).sortOrder)
	}
	if configErr != nil {
		fmt.Fprintf(os.Stderr, "lazysvn could not save sort order: %v\n", configErr)
	}
	return 0
}

func isHelpArg(s string) bool {
	return s == "-h" || s == "--help" || s == "help"
}

func runCLI(args []string, openTUI func() int) int {
	if len(args) == 0 {
		return openTUI()
	}

	switch cmd := args[0]; {
	case isHelpArg(cmd):
		fmt.Println(cliHelp())
		return 0
	case cmd == "-v" || cmd == "--version" || cmd == "version":
		fmt.Println(currentVersion())
		return 0
	case cmd == "update":
		if len(args) > 2 {
			fmt.Fprintf(os.Stderr, "too many arguments for update\n\n%s\n", updateHelp())
			return 2
		}
		tag := "latest"
		if len(args) == 2 {
			switch a := strings.TrimSpace(args[1]); {
			case isHelpArg(a):
				fmt.Println(updateHelp())
				return 0
			case strings.HasPrefix(a, "-"):
				fmt.Fprintf(os.Stderr, "unknown option for update: %s\n\n%s\n", a, updateHelp())
				return 2
			case a != "":
				tag = a
			}
		}
		if err := runSelfUpdate(tag); err != nil {
			fmt.Fprintf(os.Stderr, "lazysvn update failed: %v\n", err)
			return 1
		}
		return 0
	case cmd == "uninstall":
		if len(args) > 2 {
			fmt.Fprintf(os.Stderr, "too many arguments for uninstall\n\n%s\n", uninstallHelp())
			return 2
		}
		if len(args) == 2 {
			if isHelpArg(args[1]) {
				fmt.Println(uninstallHelp())
				return 0
			}
			fmt.Fprintf(os.Stderr, "unknown argument for uninstall: %s\n\n%s\n", args[1], uninstallHelp())
			return 2
		}
		if err := runSelfUninstall(); err != nil {
			fmt.Fprintf(os.Stderr, "lazysvn uninstall failed: %v\n", err)
			return 1
		}
		return 0
	default:
		if cmd == "--" {
			args = args[1:]
		} else if strings.HasPrefix(cmd, "-") {
			fmt.Fprintf(os.Stderr, "unknown option: %s\n\n%s\n", cmd, cliHelp())
			return 2
		}
		if len(args) != 1 {
			fmt.Fprintf(os.Stderr, "expected exactly one directory path\n\n%s\n", cliHelp())
			return 2
		}
		if err := os.Chdir(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "lazysvn: %v\n", err)
			return 1
		}
		return openTUI()
	}
}

func helpText() string {
	return strings.TrimSpace(`
LazySVN keys:
  j/k or arrows  Move
  g / G          Top / Bottom
  Tab            Toggle path/status sort (M, then A first)
  space          Mark/unmark file
  Shift+V        Select a range of visible files with the movement keys
                 Shift+V or Space marks the range, Esc cancels it
  /              Edit filters at the bottom of Changes (status first, Tab for path)
  esc            Clear filter, or clear marks if no filter is active
  d or Enter     Diff selected file (follows the cursor)
  l              Show recent log (20 entries) for selected file
  c              Commit marked files (or current file)
  a              svn add marked/current unversioned (?) files
  v              Revert marked/current files (lists them first)
  u              svn update
  r              Refresh status
  h or ?         Show help
  mouse wheel    Scroll the pane under the pointer
  q / ctrl+c     Quit
	`)
}

func cliHelp() string {
	return strings.TrimSpace(`
LazySVN

A LazyGit-style terminal UI for Subversion (SVN).

Usage:
  lazysvn [path]
  lazysvn -- <path>
  lazysvn --version
  lazysvn update [tag]
  lazysvn uninstall
  lazysvn --help

Open the SVN working copy at path, or use the current directory by default.
Use -- before a path that starts with a dash or matches a command name.

Command:
  version      Show lazysvn version
  update       Self-update from hosted installer (defaults to latest)
  uninstall    Remove the current lazysvn binary
`)
}

// currentVersion reports the version baked into this binary: the -X ldflag,
// else the module version Go stamps from the VCS tag at build time, else "dev".
// It never inspects the environment the binary runs in.
func currentVersion() string {
	if v := normalizeVersion(version); v != "" {
		return v
	}
	if info, ok := debug.ReadBuildInfo(); ok && info != nil {
		if v := normalizeVersion(info.Main.Version); v != "" {
			return v
		}
	}
	return "dev"
}

func normalizeVersion(v string) string {
	out := strings.TrimSpace(v)
	if out == "" || out == "(devel)" || out == "dev" {
		return ""
	}
	out = strings.TrimPrefix(out, "refs/tags/")
	if pseudoVersionRE.MatchString(out) {
		return ""
	}
	if len(out) > 1 && out[0] == 'v' && out[1] >= '0' && out[1] <= '9' {
		out = out[1:]
	}
	out = strings.SplitN(out, "-", 2)[0]
	out = strings.SplitN(out, "+", 2)[0]

	parts := strings.Split(out, ".")
	if len(parts) > 3 {
		return ""
	}
	for _, p := range parts {
		if p == "" {
			return ""
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return ""
			}
		}
	}
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".")
}

func updateHelp() string {
	return strings.TrimSpace(`
Usage:
  lazysvn update
  lazysvn update <tag>

Examples:
  lazysvn update
  lazysvn update v0.1.0

Env:
  INSTALL_DIR            Override install target directory
  LAZYSVN_INSTALL_URL    Override installer URL (default: https://lazysvn.sawirstudio.com/install.sh,
                         or install.ps1 on Windows)

Installs made by Homebrew, Scoop, winget or a Linux package are updated
with that package manager instead; lazysvn update tells you the command.
	`)
}

func uninstallHelp() string {
	return strings.TrimSpace(`
Usage:
  lazysvn uninstall

Notes:
  - Removes the currently running lazysvn binary.
  - Installs made by Homebrew, Scoop, winget or a Linux package are removed
    with that package manager instead; lazysvn uninstall tells you the command.
	`)
}
