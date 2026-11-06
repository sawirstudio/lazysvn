package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	defaultInstallScriptURL     = "https://lazysvn.sawirstudio.com/install.sh"
	defaultWindowsInstallScript = "https://lazysvn.sawirstudio.com/install.ps1"
	wingetID                    = "SawirStudio.LazySVN"
)

// packageManager is whoever owns an installed lazysvn binary. Self-update and
// self-uninstall must not touch those installs: overwriting a Homebrew or
// winget-managed file leaves the package manager's records wrong.
type packageManager struct {
	name      string
	upgrade   string
	uninstall string
}

// detectPackageManager recognises installs by where their binary lives. Pass
// both the executable path and its symlink-resolved form: Homebrew and winget
// expose a link in a bin directory that points into their own store.
func detectPackageManager(goos string, paths ...string) (packageManager, bool) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		lp := strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
		switch {
		case strings.Contains(lp, "/cellar/") || strings.Contains(lp, "/caskroom/"):
			return packageManager{"Homebrew", "brew upgrade lazysvn", "brew uninstall lazysvn"}, true
		case strings.Contains(lp, "/scoop/apps/") || strings.Contains(lp, "/scoop/shims/"):
			return packageManager{"Scoop", "scoop update lazysvn", "scoop uninstall lazysvn"}, true
		case strings.Contains(lp, "/microsoft/winget/packages/") || strings.Contains(lp, "/microsoft/winget/links/"):
			return packageManager{"winget", "winget upgrade " + wingetID, "winget uninstall " + wingetID}, true
		case goos == "linux" && strings.HasPrefix(lp, "/usr/bin/"):
			return packageManager{"your system package manager",
				"sudo apt upgrade lazysvn (or dnf upgrade / apk upgrade)",
				"sudo apt remove lazysvn (or dnf remove / apk del)"}, true
		}
	}
	return packageManager{}, false
}

func executablePaths() (exe, resolved string, err error) {
	exe, err = os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("resolve executable path: %w", err)
	}
	resolved = exe
	if p, err := filepath.EvalSymlinks(exe); err == nil && p != "" {
		resolved = p
	}
	return exe, resolved, nil
}

func runSelfUpdate(tag string) error {
	exe, resolved, err := executablePaths()
	if err != nil {
		return err
	}
	if pm, ok := detectPackageManager(runtime.GOOS, exe, resolved); ok {
		return fmt.Errorf("lazysvn was installed with %s; update it with: %s", pm.name, pm.upgrade)
	}

	windows := runtime.GOOS == "windows"
	installURL := strings.TrimSpace(os.Getenv("LAZYSVN_INSTALL_URL"))
	if installURL == "" {
		installURL = defaultInstallScriptURL
		if windows {
			installURL = defaultWindowsInstallScript
		}
	}
	script, err := fetchInstaller(installURL)
	if err != nil {
		return err
	}

	env := append(os.Environ(), "VERSION="+tag)
	if strings.TrimSpace(os.Getenv("INSTALL_DIR")) == "" {
		env = append(env, "INSTALL_DIR="+filepath.Dir(exe))
	}

	fmt.Printf("Updating lazysvn to %s...\n", tag)
	var cmd *exec.Cmd
	if windows {
		// Windows PowerShell can't run a script from stdin reliably, so stage it.
		f, err := os.CreateTemp("", "lazysvn-install-*.ps1")
		if err != nil {
			return fmt.Errorf("stage installer: %w", err)
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(script); err != nil {
			f.Close()
			return fmt.Errorf("stage installer: %w", err)
		}
		f.Close()
		cmd = exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", f.Name())
	} else {
		cmd = exec.Command("sh")
		cmd.Stdin = bytes.NewReader(script)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run installer: %w", err)
	}

	fmt.Println("Update complete.")
	return nil
}

func fetchInstaller(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create installer request: %w", err)
	}
	req.Header.Set("User-Agent", "lazysvn-self-update")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch installer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("installer request failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	script, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read installer: %w", err)
	}
	if len(script) == 0 {
		return nil, fmt.Errorf("installer script is empty")
	}
	return script, nil
}

func runSelfUninstall() error {
	exe, resolved, err := executablePaths()
	if err != nil {
		return err
	}
	if pm, ok := detectPackageManager(runtime.GOOS, exe, resolved); ok {
		return fmt.Errorf("lazysvn was installed with %s; remove it with: %s", pm.name, pm.uninstall)
	}

	if runtime.GOOS == "windows" {
		// A running .exe can't be deleted on Windows. Hand the deletion to a
		// short-lived PowerShell that waits for this process to exit. The path
		// travels in an environment variable so no quoting is involved.
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command",
			"Start-Sleep -Seconds 2; Remove-Item -LiteralPath $env:LAZYSVN_UNINSTALL_PATH -Force")
		cmd.Env = append(os.Environ(), "LAZYSVN_UNINSTALL_PATH="+exe)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("schedule removal of %s: %w", exe, err)
		}
		fmt.Printf("Removing %s after lazysvn exits.\n", exe)
		fmt.Printf("If install.ps1 added %s to your PATH, remove it in Settings > Environment Variables.\n", filepath.Dir(exe))
		return nil
	}

	if err := os.Remove(exe); err != nil {
		return fmt.Errorf("remove %s: %w", exe, err)
	}
	fmt.Printf("Removed %s\n", exe)
	return nil
}
