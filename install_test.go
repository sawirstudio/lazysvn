package main

import "testing"

func TestDetectPackageManager(t *testing.T) {
	cases := []struct {
		goos  string
		paths []string
		want  string // "" = not managed
	}{
		{"darwin", []string{"/opt/homebrew/bin/lazysvn", "/opt/homebrew/Caskroom/lazysvn/0.4.0/lazysvn"}, "Homebrew"},
		{"darwin", []string{"/usr/local/Cellar/lazysvn/0.3/bin/lazysvn"}, "Homebrew"},
		{"linux", []string{"/home/linuxbrew/.linuxbrew/bin/lazysvn", "/home/linuxbrew/.linuxbrew/Caskroom/lazysvn/0.4.0/lazysvn"}, "Homebrew"},
		{"windows", []string{`C:\Users\r\scoop\apps\lazysvn\current\lazysvn.exe`}, "Scoop"},
		{"windows", []string{`C:\ProgramData\scoop\apps\lazysvn\0.4.0\lazysvn.exe`}, "Scoop"},
		{"windows", []string{`C:\Users\r\AppData\Local\Microsoft\WinGet\Links\lazysvn.exe`}, "winget"},
		{"windows", []string{`C:\Users\r\AppData\Local\Microsoft\WinGet\Packages\SawirStudio.LazySVN_Microsoft.Winget.Source_8wekyb3d8bbwe\lazysvn.exe`}, "winget"},
		{"linux", []string{"/usr/bin/lazysvn"}, "your system package manager"},
		{"linux", []string{"/home/r/.local/bin/lazysvn"}, ""},
		{"linux", []string{"/usr/local/bin/lazysvn"}, ""},
		{"windows", []string{`C:\Users\r\AppData\Local\Programs\lazysvn\lazysvn.exe`}, ""},
		{"darwin", []string{"/usr/bin/lazysvn"}, ""},
	}
	for _, c := range cases {
		pm, ok := detectPackageManager(c.goos, c.paths...)
		if got := map[bool]string{true: pm.name}[ok]; got != c.want {
			t.Errorf("%s %v: got %q, want %q", c.goos, c.paths, got, c.want)
		}
	}
}
