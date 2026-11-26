package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ---- status parsing -------------------------------------------------------

// Shape captured from svn 1.15 with an external, a changelist and a
// property-only change. Plain `svn status` prints banner lines for these that
// the old column parser turned into bogus entries.
const statusXMLFixture = `<?xml version="1.0" encoding="UTF-8"?>
<status>
<target path=".">
<entry path="a.txt"><wc-status item="modified" revision="3" props="none"></wc-status></entry>
<entry path="ext"><wc-status item="external" props="none"></wc-status></entry>
<entry path="new.txt"><wc-status item="unversioned" props="none"></wc-status></entry>
<entry path="ext/l.txt"><wc-status item="modified" revision="3" props="none"></wc-status></entry>
<entry path="tc.txt"><wc-status item="normal" props="none" tree-conflicted="true"></wc-status></entry>
</target>
<changelist name="mycl">
<entry path="keep.txt"><wc-status item="normal" revision="3" props="modified"></wc-status></entry>
</changelist>
</status>`

func TestParseStatusXML(t *testing.T) {
	got, err := parseStatusXML([]byte(statusXMLFixture))
	if err != nil {
		t.Fatal(err)
	}
	want := []statusEntry{
		{Code: 'M', PropCode: ' ', Path: "a.txt"},
		{Code: 'M', PropCode: ' ', Path: "ext/l.txt"},
		{Code: 'M', PropCode: 'M', Path: "keep.txt", Changelist: "mycl"},
		{Code: '?', PropCode: ' ', Path: "new.txt"},
		{Code: 'C', PropCode: ' ', Path: "tc.txt"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// ---- diff parsing ---------------------------------------------------------

func kinds(lines []diffLine) []diffKind {
	out := make([]diffKind, len(lines))
	for i, l := range lines {
		out[i] = l.kind
	}
	return out
}

func TestParseDiffHeaderLookalikesInsideHunk(t *testing.T) {
	raw := []string{
		"Index: q.sql",
		"===================================================================",
		"--- q.sql\t(revision 3)",
		"+++ q.sql\t(working copy)",
		"@@ -1,3 +1,3 @@",
		" select 1;",
		"--- old comment", // removed line whose content is "-- old comment"
		"+++ new thing",   // added line whose content is "++ new thing"
		" select 2;",
	}
	got := parseDiff(raw)
	want := []diffKind{diffMeta, diffMeta, diffMeta, diffMeta, diffHunk, diffContext, diffDel, diffAdd, diffContext}
	if g := kinds(got); !equalKinds(g, want) {
		t.Fatalf("kinds = %v, want %v", g, want)
	}
	if got[6].text != "-- old comment" || got[6].oldNo != 2 {
		t.Errorf("del = %+v", got[6])
	}
	if got[7].text != "++ new thing" || got[7].newNo != 2 {
		t.Errorf("add = %+v", got[7])
	}
	if got[8].oldNo != 3 || got[8].newNo != 3 {
		t.Errorf("trailing context numbers = %d/%d, want 3/3", got[8].oldNo, got[8].newNo)
	}
}

func TestParseDiffPropertySectionAfterHunk(t *testing.T) {
	raw := []string{
		"@@ -1 +1,2 @@",
		" x",
		"+y",
		"",
		"Property changes on: keep.txt",
		"___________________________________________________________________",
		"Added: svn:executable",
		"## -0,0 +1 ##",
		"+*",
		`\ No newline at end of property`,
	}
	got := parseDiff(raw)
	want := []diffKind{diffHunk, diffContext, diffAdd, diffMeta, diffMeta, diffMeta, diffMeta, diffHunk, diffAdd, diffNote}
	if g := kinds(got); !equalKinds(g, want) {
		t.Fatalf("kinds = %v, want %v", g, want)
	}
	if got[8].newNo != 0 {
		t.Errorf("property line got file line number %d, want none", got[8].newNo)
	}
}

func TestParseDiffSecondFileHeadersAreMeta(t *testing.T) {
	raw := []string{
		"@@ -1 +1 @@",
		"-a",
		"+b",
		"Index: other.txt",
		"--- other.txt\t(revision 1)",
		"+++ other.txt\t(working copy)",
		"@@ -5,0 +6 @@",
		"+new",
	}
	got := parseDiff(raw)
	want := []diffKind{diffHunk, diffDel, diffAdd, diffMeta, diffMeta, diffMeta, diffHunk, diffAdd}
	if g := kinds(got); !equalKinds(g, want) {
		t.Fatalf("kinds = %v, want %v", g, want)
	}
	if got[7].newNo != 6 {
		t.Errorf("newNo = %d, want 6", got[7].newNo)
	}
}

func equalKinds(a, b []diffKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRenderDiffRowsFitWidth(t *testing.T) {
	doc := newDiffDoc([]string{"@@ -1 +1 @@", "-\tindented old line that is quite long indeed", "+新しい行 wide runes"})
	for _, width := range []int{40, 80, 120} {
		for _, row := range renderDiff(doc, width, 100) {
			if w := ansi.StringWidth(row); w > width {
				t.Errorf("width %d: row is %d cells: %q", width, w, row)
			}
		}
	}
}

func TestTruncateIsANSISafe(t *testing.T) {
	s := statusCodeStyle('M').Render("M") + " some/long/path.txt"
	got := truncate(s, 6)
	if w := ansi.StringWidth(got); w > 6 {
		t.Fatalf("width = %d", w)
	}
	if strings.Count(got, "\x1b[") != strings.Count(s, "\x1b[") {
		t.Errorf("escape sequences were cut: %q", got)
	}
}

// ---- state machine --------------------------------------------------------

func loaded(entries ...statusEntry) model {
	m := initialModel()
	m.width, m.height = 120, 40
	m = m.onStatus(statusMsg{seq: m.statusSeq, entries: entries})
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(t *testing.T, m model, keys ...string) (model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var next tea.Model
		next, cmd = m.Update(key(k))
		m = next.(model)
	}
	return m, cmd
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

var abc = []statusEntry{{Code: 'M', Path: "a"}, {Code: 'M', Path: "b"}, {Code: '?', Path: "c"}}

func TestCtrlCQuitsFromEveryMode(t *testing.T) {
	m := loaded(abc...)
	m, _ = press(t, m, "c")
	if m.mode != modeCommit {
		t.Fatalf("mode = %v", m.mode)
	}
	if _, cmd := press(t, m, "ctrl+c"); !isQuit(cmd) {
		t.Error("ctrl+c did not quit from commit prompt")
	}
	m, _ = press(t, loaded(abc...), "v")
	if m.mode != modeConfirmRevert {
		t.Fatalf("mode = %v", m.mode)
	}
	if _, cmd := press(t, m, "ctrl+c"); !isQuit(cmd) {
		t.Error("ctrl+c did not quit from revert confirm")
	}
}

func TestStaleDiffRepliesAreDropped(t *testing.T) {
	m := loaded(abc...)
	m, _ = press(t, m, "d") // diff a (seq n)
	first := m.previewSeq
	m, _ = press(t, m, "j") // follows to b (seq n+1)
	if m.previewSeq == first {
		t.Fatal("moving did not issue a new diff request")
	}
	m = m.onPreview(previewMsg{seq: first, kind: previewDiff, path: "a", result: svnResult{args: []string{"diff"}, stdout: "@@ -1 +1 @@\n-x\n+y\n"}})
	if m.preview.path == "a" {
		t.Error("stale diff for a replaced the pane while cursor is on b")
	}
	m = m.onPreview(previewMsg{seq: m.previewSeq, kind: previewDiff, path: "b", result: svnResult{args: []string{"diff"}}})
	if m.preview.path != "b" || m.loadingPreview {
		t.Errorf("preview = %+v loading=%v", m.preview, m.loadingPreview)
	}
	if s, _ := m.statusText(); strings.Contains(s, "Loading") {
		t.Errorf("status still says %q after diff arrived", s)
	}
}

func TestHelpWhileDiffLoadingStopsFollowing(t *testing.T) {
	m := loaded(abc...)
	m, _ = press(t, m, "d")
	seq := m.previewSeq
	m, _ = press(t, m, "h")
	m = m.onPreview(previewMsg{seq: seq, kind: previewDiff, path: "a", result: svnResult{args: []string{"diff"}}})
	if m.preview.kind != previewHelp {
		t.Errorf("late diff overwrote help: %+v", m.preview)
	}
	if _, cmd := press(t, m, "j"); cmd != nil {
		t.Error("cursor move re-diffed after leaving diff view")
	}
}

func TestOperationsAreExclusive(t *testing.T) {
	m := loaded(abc...)
	m, cmd := press(t, m, "u")
	if m.running != opUpdate || cmd == nil {
		t.Fatalf("running = %v", m.running)
	}
	for _, k := range []string{"u", "r", "d", "l", "c", "v", "a"} {
		next, cmd := press(t, m, k)
		if cmd != nil || next.mode != modeNormal || next.running != opUpdate {
			t.Errorf("%q during update: cmd=%v mode=%v", k, cmd != nil, next.mode)
		}
	}
}

func TestStatusReplyFromBeforeOperationIsDropped(t *testing.T) {
	m := loaded(abc...)
	m, _ = press(t, m, "r")
	oldSeq := m.statusSeq
	m, _ = press(t, m, "u")
	m = m.onStatus(statusMsg{seq: oldSeq, entries: []statusEntry{{Code: 'M', Path: "zzz"}}})
	if len(m.entries) != 3 {
		t.Errorf("pre-operation status reply was applied: %+v", m.entries)
	}
}

func TestFailureIsDecidedByExitStatusNotText(t *testing.T) {
	m := loaded(abc...)
	m.running = opCommit
	m, _ = m.onAction(actionMsg{op: opCommit, paths: []string{"a"}, result: svnResult{args: []string{"commit"}, stdout: "Sending        error_handler.go\nCommitted revision 4."}})
	if m.notice.level != noticeInfo {
		t.Errorf("successful commit mentioning 'error' reported as failure: %+v", m.notice)
	}
	m.running = opCommit
	m, _ = m.onAction(actionMsg{op: opCommit, paths: []string{"a"}, result: svnResult{args: []string{"commit"}, err: errors.New("exit status 1")}})
	if m.notice.level != noticeError {
		t.Errorf("failed commit not reported as failure: %+v", m.notice)
	}
}

func TestNoticeSurvivesBackgroundRefresh(t *testing.T) {
	m := loaded(abc...)
	m.running = opRevert
	m, _ = m.onAction(actionMsg{op: opRevert, result: svnResult{args: []string{"revert"}, err: errors.New("exit status 1")}})
	m = m.onStatus(statusMsg{seq: m.statusSeq, entries: abc})
	if s, _ := m.statusText(); !strings.Contains(s, "failed") {
		t.Errorf("failure notice overwritten by refresh: %q", s)
	}
}

func TestMarksClearedOnlyAfterSuccess(t *testing.T) {
	m := loaded(abc...)
	m, _ = press(t, m, " ", "j", " ")
	m, _ = press(t, m, "v")
	if strings.Join(m.pendingPaths, ",") != "a,b" {
		t.Fatalf("pending = %v", m.pendingPaths)
	}
	m, _ = press(t, m, "y")
	if m.running != opRevert {
		t.Fatalf("running = %v", m.running)
	}
	failed, _ := m.onAction(actionMsg{op: opRevert, paths: []string{"a", "b"}, result: svnResult{args: []string{"revert"}, err: errors.New("boom")}})
	if len(failed.marked) != 2 {
		t.Errorf("marks dropped after failed revert: %v", failed.marked)
	}
	ok, _ := m.onAction(actionMsg{op: opRevert, paths: []string{"a", "b"}, result: svnResult{args: []string{"revert"}}})
	if len(ok.marked) != 0 {
		t.Errorf("marks kept after successful revert: %v", ok.marked)
	}
}

func TestRevertConfirmListsPaths(t *testing.T) {
	m := loaded(abc...)
	m, _ = press(t, m, " ", "j", " ", "v")
	view := m.View()
	for _, p := range []string{"Revert 2 path(s)?", "  - a", "  - b"} {
		if !strings.Contains(view, p) {
			t.Errorf("revert confirm does not show %q", p)
		}
	}
}

func TestAddSkipsVersionedSelectionAndSaysSo(t *testing.T) {
	m := loaded(abc...)
	m, _ = press(t, m, " ", "j", "j", " ") // mark a (versioned) and c (unversioned)
	m, cmd := press(t, m, "a")
	if cmd == nil || m.running != opAdd {
		t.Fatalf("add did not start")
	}
	body := strings.Join(m.preview.lines, "\n")
	if !strings.Contains(body, "  - c") || strings.Contains(body, "  - a") || !strings.Contains(body, "Skipped 1") {
		t.Errorf("add preview = %q", body)
	}
	m, _ = m.onAction(actionMsg{op: opAdd, paths: []string{"c"}, note: "Skipped 1 selected file(s) that are already versioned.", result: svnResult{args: []string{"add"}}})
	if !strings.Contains(m.notice.text, "Skipped 1") {
		t.Errorf("notice = %q", m.notice.text)
	}
	if _, ok := m.marked["c"]; ok {
		t.Error("added file still marked")
	}
}

func TestCursorStaysOnSamePathAcrossRefresh(t *testing.T) {
	m := loaded(abc...)
	m, _ = press(t, m, "j") // on b
	m, _ = press(t, m, "r")
	m = m.onStatus(statusMsg{seq: m.statusSeq, entries: []statusEntry{{Code: '?', Path: "0new"}, {Code: 'M', Path: "a"}, {Code: 'M', Path: "b"}}})
	if m.currentPath() != "b" {
		t.Errorf("cursor on %q, want b", m.currentPath())
	}
}

func TestStatusFailureMarksListStale(t *testing.T) {
	m := loaded(abc...)
	m, _ = press(t, m, "r")
	m = m.onStatus(statusMsg{seq: m.statusSeq, result: svnResult{args: []string{"status", "--xml"}, stderr: "svn: E155007: not a working copy", err: errors.New("exit status 1")}, err: errors.New("exit status 1")})
	if !strings.Contains(m.View(), "(stale)") {
		t.Error("stale list not flagged")
	}
	if m.preview.kind != previewError {
		t.Errorf("preview = %+v", m.preview)
	}
}

func TestCommitPromptShowsEmptyMessageError(t *testing.T) {
	m := loaded(abc...)
	m, _ = press(t, m, "c", "enter")
	if m.mode != modeCommit {
		t.Fatal("left commit mode on empty message")
	}
	if !strings.Contains(m.View(), "cannot be empty") {
		t.Error("empty-message error not visible while prompt is open")
	}
}

func TestCommitOnCleanCopyIsRefused(t *testing.T) {
	m := loaded()
	m, cmd := press(t, m, "c")
	if m.mode != modeNormal || cmd != nil {
		t.Error("commit prompt opened on a clean working copy")
	}
}

func TestVersionIgnoresCurrentDirectory(t *testing.T) {
	if got := currentVersion(); got != "dev" {
		t.Errorf("currentVersion() = %q in a test binary, want dev", got)
	}
	for in, want := range map[string]string{"v1.2.3": "1.2.3", "v0.1": "0.1.0", "dev": "", "(devel)": "", "v0.0.0-20260101000000-abcdefabcdef": ""} {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUpdateRejectsOptions(t *testing.T) {
	if code := runCLI([]string{"update", "--force"}, nil); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestCLIOpensWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"product", "work copy", "update", "-product"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		name string
		args []string
		dir  string
	}{
		{"current directory", nil, "."},
		{"relative path", []string{"product"}, "product"},
		{"absolute path with spaces", []string{filepath.Join(root, "work copy")}, "work copy"},
		{"command name as path", []string{"--", "update"}, "update"},
		{"dash-prefixed path", []string{"--", "-product"}, "-product"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(root)
			want, err := os.Stat(filepath.Join(root, tc.dir))
			if err != nil {
				t.Fatal(err)
			}
			opened := false
			code := runCLI(tc.args, func() int {
				opened = true
				got, err := os.Stat(".")
				if err != nil {
					t.Fatal(err)
				}
				if !os.SameFile(got, want) {
					cwd, _ := os.Getwd()
					t.Errorf("TUI opened in %q, want %q", cwd, filepath.Join(root, tc.dir))
				}
				return 0
			})
			if code != 0 || !opened {
				t.Errorf("exit = %d, TUI opened = %v", code, opened)
			}
		})
	}
}

func TestCLIRejectsInvalidDirectoryArguments(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"missing directory", []string{filepath.Join(root, "missing")}, 1},
		{"file path", []string{file}, 1},
		{"extra path", []string{root, root}, 2},
		{"missing path after --", []string{"--"}, 2},
		{"unknown option", []string{"--unknown"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := runCLI(tc.args, func() int {
				t.Fatal("opened TUI with invalid arguments")
				return 1
			})
			if code != tc.code {
				t.Errorf("exit = %d, want %d", code, tc.code)
			}
		})
	}
}

// ---- against real svn -----------------------------------------------------

func TestAgainstRealSVN(t *testing.T) {
	for _, bin := range []string{"svn", "svnadmin"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
	dir := t.TempDir()
	run := func(wd string, name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = wd
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run(dir, "svnadmin", "create", "repo")
	url := "file://" + filepath.Join(dir, "repo")
	run(dir, "svn", "mkdir", "-q", "-m", "init", url+"/trunk")
	run(dir, "svn", "co", "-q", url+"/trunk", "wc")
	wc := filepath.Join(dir, "wc")
	write(filepath.Join(wc, "q.sql"), "select 1;\n-- old comment\nselect 2;\n")
	write(filepath.Join(wc, "-dash.txt"), "x\n")
	run(wc, "svn", "add", "-q", "q.sql")
	run(wc, "svn", "add", "-q", "--", "-dash.txt")
	run(wc, "svn", "ci", "-q", "-m", "one")
	write(filepath.Join(wc, "q.sql"), "select 1;\nselect 2;\n")
	write(filepath.Join(wc, "-dash.txt"), "y\n")
	write(filepath.Join(wc, "new.txt"), "n\n")
	run(wc, "svn", "changelist", "-q", "cl", "--", "-dash.txt")

	t.Chdir(wc)

	msg := fetchStatusCmd(1)().(statusMsg)
	if msg.err != nil {
		t.Fatalf("status: %v\n%s", msg.err, msg.result.transcript())
	}
	var paths []string
	for _, e := range msg.entries {
		paths = append(paths, string(e.Code)+" "+e.Path+" "+e.Changelist)
	}
	if got := strings.Join(paths, "|"); got != "M -dash.txt cl|? new.txt |M q.sql " {
		t.Errorf("entries = %q", got)
	}

	pm := diffCmd(1, "-dash.txt")().(previewMsg)
	if pm.result.err != nil {
		t.Errorf("diff of dash-prefixed path failed: %s", pm.result.transcript())
	}

	pm = diffCmd(1, "q.sql")().(previewMsg)
	lines := parseDiff(splitLines(pm.result.stdout))
	var del *diffLine
	for i := range lines {
		if lines[i].kind == diffDel {
			del = &lines[i]
		}
	}
	if del == nil || del.text != "-- old comment" || del.oldNo != 2 {
		t.Errorf("removed SQL comment parsed as %+v from:\n%s", del, pm.result.stdout)
	}

	am := actionCmd(opRequest{op: opRevert, paths: []string{"-dash.txt"}})().(actionMsg)
	if am.result.err != nil {
		t.Errorf("revert of dash-prefixed path failed: %s", am.result.transcript())
	}
}

// svnFixture is a fresh repository and checkout. run fails the test on
// error and returns the combined output.
type svnFixture struct {
	t  *testing.T
	wc string
}

func newSVNFixture(t *testing.T) svnFixture {
	t.Helper()
	for _, bin := range []string{"svn", "svnadmin"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
	dir := t.TempDir()
	f := svnFixture{t: t, wc: filepath.Join(dir, "wc")}
	f.runIn(dir, "svnadmin", "create", "repo")
	url := "file://" + filepath.ToSlash(filepath.Join(dir, "repo"))
	f.runIn(dir, "svn", "mkdir", "-q", "-m", "init", url+"/trunk")
	f.runIn(dir, "svn", "co", "-q", url+"/trunk", "wc")
	return f
}

func (f svnFixture) on(t *testing.T) svnFixture { f.t = t; return f }

func (f svnFixture) runIn(wd, name string, args ...string) string {
	f.t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = wd
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

func (f svnFixture) svn(args ...string) string {
	f.t.Helper()
	return f.runIn(f.wc, "svn", args...)
}

func (f svnFixture) write(rel, body string) {
	f.t.Helper()
	p := filepath.Join(f.wc, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// model loads the working copy's real status.
func (f svnFixture) model() model {
	f.t.Helper()
	msg := fetchStatusCmd(1)().(statusMsg)
	if msg.err != nil {
		f.t.Fatalf("status: %v\n%s", msg.err, msg.result.transcript())
	}
	return loaded(msg.entries...)
}

// act marks sel, presses keys and runs the svn operation that results.
func (f svnFixture) act(m model, sel []string, keys ...string) (model, actionMsg) {
	f.t.Helper()
	m.marked = map[string]struct{}{}
	for _, p := range sel {
		m.marked[p] = struct{}{}
	}
	m, cmd := press(f.t, m, keys...)
	if cmd == nil {
		f.t.Fatalf("keys %v started nothing; notice: %q", keys, m.notice.text)
	}
	am, ok := cmd().(actionMsg)
	if !ok {
		f.t.Fatalf("keys %v did not run an svn operation", keys)
	}
	if am.result.err != nil {
		f.t.Fatalf("keys %v failed:\n%s", keys, am.transcript())
	}
	return m, am
}

func (f svnFixture) commit(sel []string, message string) actionMsg {
	f.t.Helper()
	m, _ := press(f.t, f.model())
	m.marked = map[string]struct{}{}
	for _, p := range sel {
		m.marked[p] = struct{}{}
	}
	m, _ = press(f.t, m, "c")
	if m.mode != modeCommit {
		f.t.Fatalf("commit of %v did not open the prompt; notice: %q", sel, m.notice.text)
	}
	m.commitInput.SetValue(message)
	_, am := f.act(m, sel, "enter")
	return am
}

func (f svnFixture) status() string {
	f.t.Helper()
	var b []string
	for _, e := range f.model().entries {
		b = append(b, string(e.Code)+" "+filepath.ToSlash(e.Path))
	}
	return strings.Join(b, "|")
}

// Each case here failed against real svn before: the arguments looked right
// but svn read them differently.
func TestSVNArgumentEdgeCases(t *testing.T) {
	f := newSVNFixture(t)
	f.write("icon@2x.png", "a\n")
	f.write("keep.txt", "k\n")
	f.write("moveme.txt", "m\n")
	f.write("gone.txt", "g\n")
	f.write("pdir/child.txt", "c\n")
	f.svn("add", "-q", "--", "icon@2x.png@", "keep.txt", "moveme.txt", "gone.txt", "pdir")
	f.svn("ci", "-q", "-m", "one")
	t.Chdir(f.wc)

	t.Run("at sign in a path is not a peg revision", func(t *testing.T) {
		f := f.on(t)
		f.write("icon@2x.png", "b\n")
		if pm := diffCmd(1, "icon@2x.png")().(previewMsg); pm.result.err != nil || !strings.Contains(pm.result.stdout, "+b") {
			t.Errorf("diff: %s", pm.result.transcript())
		}
		if pm := logCmd(1, "icon@2x.png")().(previewMsg); pm.result.err != nil {
			t.Errorf("log: %s", pm.result.transcript())
		}
		if am := actionCmd(opRequest{op: opRevert, paths: []string{"icon@2x.png"}})().(actionMsg); am.result.err != nil {
			t.Errorf("revert: %s", am.transcript())
		}
		f.write("icon@2x.png", "c\n")
		f.commit([]string{"icon@2x.png"}, "at")
		f.write("new@x.txt", "n\n")
		if am := actionCmd(opRequest{op: opAdd, paths: []string{"new@x.txt"}})().(actionMsg); am.result.err != nil {
			t.Errorf("add: %s", am.transcript())
		}
		f.svn("revert", "-q", "--", "new@x.txt@")
		os.Remove("new@x.txt")
	})

	t.Run("revert an added directory with its children", func(t *testing.T) {
		f := f.on(t)
		f.write("nd/f.txt", "q\n")
		f.svn("add", "-q", "nd")
		m := f.model()
		m.marked = map[string]struct{}{"nd": {}}
		m, _ = press(t, m, "v")
		if got := strings.Join(m.pendingPaths, ","); got != "nd,"+filepath.Join("nd", "f.txt") {
			t.Errorf("revert listed %q", got)
		}
		_, _ = f.act(m, nil, "y")
		if got := f.status(); got != "? nd" {
			t.Errorf("status after revert = %q", got)
		}
		os.RemoveAll("nd")
	})

	t.Run("commit a missing file deletes it", func(t *testing.T) {
		f := f.on(t)
		os.Remove("gone.txt")
		f.commit([]string{"gone.txt"}, "gone")
		if got := f.status(); got != "" {
			t.Errorf("status after commit = %q", got)
		}
	})

	t.Run("message that names a file, non-ASCII under the C locale", func(t *testing.T) {
		f := f.on(t)
		t.Setenv("LC_ALL", "C")
		t.Setenv("LANG", "")
		f.write("keep.txt", "1\n")
		f.commit([]string{"keep.txt"}, "keep.txt")
		f.write("keep.txt", "2\n")
		f.commit([]string{"keep.txt"}, "héllo ✓ -m --")
		t.Setenv("LC_ALL", "en_US.UTF-8")
		if out := f.svn("log", "-l", "1", "--xml", "keep.txt"); !strings.Contains(out, "<msg>héllo ✓ -m --</msg>") {
			t.Errorf("log message not kept:\n%s", out)
		}
	})

	t.Run("commit one side of a move", func(t *testing.T) {
		f := f.on(t)
		f.svn("mv", "-q", "moveme.txt", "moved.txt")
		f.commit([]string{"moved.txt"}, "move")
		if got := f.status(); got != "" {
			t.Errorf("status after commit = %q", got)
		}
	})

	t.Run("commit a file inside an added directory", func(t *testing.T) {
		f := f.on(t)
		f.write("adir/f.txt", "1\n")
		f.write("adir/g.txt", "2\n")
		f.svn("add", "-q", "adir")
		f.commit([]string{filepath.Join("adir", "f.txt")}, "adir")
		if got := f.status(); got != "" {
			t.Errorf("status after commit = %q", got)
		}
	})

	t.Run("directory diff shows only its own properties", func(t *testing.T) {
		f := f.on(t)
		f.svn("propset", "-q", "p", "v", "pdir")
		f.write("pdir/child.txt", "changed\n")
		pm := diffCmd(1, "pdir")().(previewMsg)
		if pm.result.err != nil || !strings.Contains(pm.result.stdout, "Property changes on: pdir") || strings.Contains(pm.result.stdout, "child.txt") {
			t.Errorf("diff of pdir:\n%s", pm.result.transcript())
		}
		m, _ := press(t, f.model())
		m.marked = map[string]struct{}{"pdir": {}}
		m, _ = press(t, m, "c")
		if want := []string{"pdir", filepath.Join("pdir", "child.txt")}; !slices.Equal(m.pendingPaths, want) {
			t.Errorf("commit listed %v, want %v", m.pendingPaths, want)
		}
	})
}

func TestExpandTargets(t *testing.T) {
	m := loaded(
		statusEntry{Code: 'A', Path: "adir"},
		statusEntry{Code: 'A', Path: "adir/f"},
		statusEntry{Code: 'A', Path: "adir/g"},
		statusEntry{Code: '?', Path: "adir/u"},
		statusEntry{Code: '!', Path: "gone"},
		statusEntry{Code: 'A', Path: "new", MovedFrom: "old"},
		statusEntry{Code: 'D', Path: "old", MovedTo: "new"},
		statusEntry{Code: 'M', Path: "pdir"},
		statusEntry{Code: 'M', Path: "pdir-x"},
		statusEntry{Code: 'M', Path: "pdir/c"},
		statusEntry{Code: '?', Path: "u"},
		statusEntry{Code: 'A', Path: `w`},
		statusEntry{Code: 'A', Path: `w\f`},
		statusEntry{Code: 'M', Path: `w\g`},
	)
	cases := []struct {
		sel       []string
		forCommit bool
		want      string
	}{
		{[]string{"pdir"}, false, "pdir,pdir/c"},
		{[]string{"adir/f"}, true, "adir,adir/f,adir/g"},
		{[]string{"adir/f"}, false, "adir/f"},
		{[]string{"new"}, true, "new,old"},
		{[]string{"old"}, false, "new,old"},
		{[]string{`w\f`}, true, `w,w\f,w\g`},
		{[]string{"."}, false, ".,adir,adir/f,adir/g,gone,new,old,pdir,pdir-x,pdir/c,w,w\\f,w\\g"},
	}
	for _, c := range cases {
		if got := strings.Join(m.expandTargets(c.sel, c.forCommit), ","); got != c.want {
			t.Errorf("expandTargets(%v, %v) = %s, want %s", c.sel, c.forCommit, got, c.want)
		}
	}

	m.marked = map[string]struct{}{"gone": {}, "u": {}, "pdir/c": {}}
	targets, missing, skipped := m.commitTargets()
	if strings.Join(targets, ",") != "gone,pdir/c" || strings.Join(missing, ",") != "gone" || skipped != 1 {
		t.Errorf("commitTargets = %v, %v, %d", targets, missing, skipped)
	}

	m.marked = map[string]struct{}{"u": {}}
	if m, cmd := press(t, m, "c"); m.mode != modeNormal || cmd != nil || !strings.Contains(m.notice.text, "press a") {
		t.Errorf("commit of only unversioned files: mode %v, notice %q", m.mode, m.notice.text)
	}
}

func TestViewNeverWrapsPaneLines(t *testing.T) {
	long := strings.Repeat("=", 200)
	for _, w := range []int{60, 90, 130, 200} {
		m := loaded(abc...)
		m.width, m.height = w, 24
		baseline := len(strings.Split(m.View(), "\n"))
		m.preview = preview{kind: previewDiff, title: "Diff: a", path: "a", diff: newDiffDoc([]string{long, "@@ -1 +1 @@", "-" + long, "+" + long})}
		lines := strings.Split(m.View(), "\n")
		if len(lines) != baseline {
			t.Errorf("width %d: long lines wrapped (%d lines, want %d)", w, len(lines), baseline)
		}
		for _, l := range lines {
			if sw := ansi.StringWidth(l); sw > w {
				t.Errorf("width %d: line is %d cells", w, sw)
			}
		}
	}
}

func TestDiffDocPairsRuns(t *testing.T) {
	doc := newDiffDoc([]string{"@@ -1,5 +1,4 @@", " ctx", "-a", "+b", "-c", "+d", "-e", "-f", "+g"})
	type pair struct{ l, r string }
	var got []pair
	for _, r := range doc.rows {
		if r.pair {
			got = append(got, pair{r.left.text, r.right.text})
		}
	}
	want := []pair{{"ctx", "ctx"}, {"a", "b"}, {"c", "d"}, {"e", "g"}, {"f", ""}}
	if len(got) != len(want) {
		t.Fatalf("rows = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRenderDiffDrawsOnlyWhatFits(t *testing.T) {
	raw := []string{"@@ -1,5000 +1,5000 @@"}
	for i := 0; i < 5000; i++ {
		raw = append(raw, " line")
	}
	doc := newDiffDoc(raw)
	for _, width := range []int{30, 120} { // unified and side by side
		if got := len(renderDiff(doc, width, 40)); got != 40 {
			t.Errorf("width %d: rendered %d rows, want 40", width, got)
		}
	}
}

// ---- mouse scrolling ------------------------------------------------------

func wheelMsg(button tea.MouseButton, x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: button, Action: tea.MouseActionPress}
}

func wheel(t *testing.T, m model, button tea.MouseButton, x, y int) model {
	t.Helper()
	next, _ := m.Update(wheelMsg(button, x, y))
	return next.(model)
}

func numberedEntries(n int) []statusEntry {
	entries := make([]statusEntry, n)
	for i := range entries {
		entries[i] = statusEntry{Code: 'M', Path: fmt.Sprintf("f%02d", i)}
	}
	return entries
}

// diffPreviewModel is a wide (left/right) model whose preview holds a 60-pair
// side-by-side diff, so there is more content than the pane can show.
func diffPreviewModel(t *testing.T) model {
	t.Helper()
	raw := []string{"Index: a", "===", "--- a\t(revision 1)", "+++ a\t(working copy)", "@@ -1,60 +1,60 @@"}
	for i := 1; i <= 60; i++ {
		raw = append(raw, fmt.Sprintf("-%02d old", i), fmt.Sprintf("+%02d new", i))
	}
	m := loaded(numberedEntries(10)...)
	m.preview = preview{kind: previewDiff, title: "Diff: a", path: "a", diff: newDiffDoc(raw)}
	return m
}

func TestWheelOverListMovesCursor(t *testing.T) {
	m := loaded(numberedEntries(10)...)
	l := m.layout()
	x, y := l.changes.rect.x+2, l.changes.rect.y+2

	m = wheel(t, m, tea.MouseButtonWheelDown, x, y)
	if m.cursor != wheelStep {
		t.Errorf("cursor = %d, want %d", m.cursor, wheelStep)
	}
	if m.preview.scroll != 0 {
		t.Errorf("list wheel moved the preview: scroll = %d", m.preview.scroll)
	}
	m = wheel(t, m, tea.MouseButtonWheelUp, x, y)
	if m.cursor != 0 {
		t.Errorf("cursor = %d after wheel up, want 0", m.cursor)
	}

	// The border belongs to the pane, so the last column of the list still
	// scrolls the list rather than falling through to the preview.
	m = wheel(t, m, tea.MouseButtonWheelDown, l.changes.rect.x+l.changes.rect.w-1, y)
	if m.cursor != wheelStep {
		t.Errorf("wheel on the border moved cursor to %d, want %d", m.cursor, wheelStep)
	}
}

func TestWheelOverPreviewScrollsDiff(t *testing.T) {
	m := diffPreviewModel(t)
	l := m.layout()
	x, y := l.preview.rect.x+2, l.preview.rect.y+2

	m = wheel(t, m, tea.MouseButtonWheelDown, x, y)
	m = wheel(t, m, tea.MouseButtonWheelDown, x, y)
	if m.preview.scroll != 2*wheelStep {
		t.Fatalf("scroll = %d, want %d", m.preview.scroll, 2*wheelStep)
	}
	if m.cursor != 0 {
		t.Errorf("preview wheel moved the cursor to %d", m.cursor)
	}

	view := m.View()
	if !strings.Contains(view, "OLD") || !strings.Contains(view, "NEW") {
		t.Error("diff header scrolled away; it should stay pinned")
	}
	if strings.Contains(view, "- 01 old") {
		t.Error("first diff row still visible after scrolling")
	}
	if !strings.Contains(view, "- 02 old") {
		t.Errorf("expected the row after the scroll offset, got:\n%s", view)
	}

	m = wheel(t, m, tea.MouseButtonWheelUp, x, y)
	m = wheel(t, m, tea.MouseButtonWheelUp, x, y)
	if m.preview.scroll != 0 {
		t.Errorf("scroll = %d after wheel up, want 0", m.preview.scroll)
	}
	if !strings.Contains(m.View(), "- 01 old") {
		t.Error("first diff row missing after scrolling back to the top")
	}
}

func TestWheelScrollClampedToContent(t *testing.T) {
	m := diffPreviewModel(t)
	l := m.layout()
	x, y := l.preview.rect.x+2, l.preview.rect.y+2

	for i := 0; i < 100; i++ {
		m = wheel(t, m, tea.MouseButtonWheelDown, x, y)
	}
	want := diffMaxOffset(m.preview.diff, l.preview.textWidth(), m.previewBodyRows())
	if m.preview.scroll != want {
		t.Errorf("scroll = %d, want clamped max %d", m.preview.scroll, want)
	}
	if got := len(strings.Split(m.View(), "\n")); got != m.height-2 {
		t.Errorf("view has %d lines at max scroll, want %d", got, m.height-2)
	}

	for i := 0; i < 100; i++ {
		m = wheel(t, m, tea.MouseButtonWheelUp, x, y)
	}
	if m.preview.scroll != 0 {
		t.Errorf("scroll = %d after scrolling up past the top, want 0", m.preview.scroll)
	}
}

func TestWheelOutsidePanesIgnored(t *testing.T) {
	m := diffPreviewModel(t)
	l := m.layout()

	for _, pt := range [][2]int{
		{l.preview.rect.x + 2, 0},                                   // header
		{l.preview.rect.x + 2, m.height - 1},                        // footer
		{l.preview.rect.x + l.preview.rect.w, l.preview.rect.y + 2}, // right of everything
	} {
		next := wheel(t, m, tea.MouseButtonWheelDown, pt[0], pt[1])
		if next.cursor != m.cursor || next.preview.scroll != m.preview.scroll {
			t.Errorf("wheel at (%d,%d) changed state: cursor %d->%d scroll %d->%d",
				pt[0], pt[1], m.cursor, next.cursor, m.preview.scroll, next.preview.scroll)
		}
	}
}

func TestWheelIgnoresNonWheelButtons(t *testing.T) {
	m := diffPreviewModel(t)
	l := m.layout()
	x, y := l.preview.rect.x+2, l.preview.rect.y+2
	next := wheel(t, m, tea.MouseButtonLeft, x, y)
	if next.preview.scroll != 0 || next.cursor != 0 {
		t.Errorf("left click changed state: cursor=%d scroll=%d", next.cursor, next.preview.scroll)
	}
}

func TestWheelInStackedLayout(t *testing.T) {
	m := diffPreviewModel(t)
	m.width, m.height = 80, 30
	l := m.layout()
	if !l.stacked {
		t.Fatal("80 columns should use the stacked layout")
	}

	m = wheel(t, m, tea.MouseButtonWheelDown, l.preview.rect.x+2, l.preview.rect.y+2)
	if m.preview.scroll != wheelStep {
		t.Errorf("preview scroll = %d, want %d", m.preview.scroll, wheelStep)
	}
	if m.cursor != 0 {
		t.Errorf("preview wheel moved the cursor to %d", m.cursor)
	}

	m = wheel(t, m, tea.MouseButtonWheelDown, l.changes.rect.x+2, l.changes.rect.y+2)
	if m.cursor != wheelStep {
		t.Errorf("cursor = %d, want %d", m.cursor, wheelStep)
	}
}

func TestWheelScrollsTextPreview(t *testing.T) {
	m := loaded(numberedEntries(2)...)
	lines := make([]string, 0, 80)
	for i := 1; i <= 80; i++ {
		lines = append(lines, fmt.Sprintf("log line %02d", i))
	}
	m.preview = preview{kind: previewLog, title: "Log: a"}.withText(strings.Join(lines, "\n"))

	l := m.layout()
	m = wheel(t, m, tea.MouseButtonWheelDown, l.preview.rect.x+2, l.preview.rect.y+2)
	if m.preview.scroll != wheelStep {
		t.Fatalf("scroll = %d, want %d", m.preview.scroll, wheelStep)
	}
	view := m.View()
	if strings.Contains(view, "log line 01") {
		t.Error("first text row still visible after scrolling")
	}
	if !strings.Contains(view, "log line 04") {
		t.Error("text preview did not scroll to the next rows")
	}
}

// TestLayoutMatchesRenderedPanes pins the mouse hit boxes to what View
// actually draws: if the two ever drift, wheel events land on the wrong pane.
func TestLayoutMatchesRenderedPanes(t *testing.T) {
	m := diffPreviewModel(t)
	for _, size := range [][2]int{{120, 40}, {132, 50}, {80, 30}, {40, 20}} {
		m.width, m.height = size[0], size[1]
		l := m.layout()

		changes := m.renderChangesPane(l.changes)
		preview := m.renderPreviewPane(l.preview)
		if got, want := lipgloss.Width(changes), l.changes.rect.w; got != want {
			t.Errorf("%dx%d: list pane renders %d columns, rect says %d", size[0], size[1], got, want)
		}
		if got, want := lipgloss.Height(changes), l.changes.rect.h; got != want {
			t.Errorf("%dx%d: list pane renders %d rows, rect says %d", size[0], size[1], got, want)
		}
		if got, want := lipgloss.Width(preview), l.preview.rect.w; got != want {
			t.Errorf("%dx%d: preview pane renders %d columns, rect says %d", size[0], size[1], got, want)
		}
		if got, want := lipgloss.Height(preview), l.preview.rect.h; got != want {
			t.Errorf("%dx%d: preview pane renders %d rows, rect says %d", size[0], size[1], got, want)
		}
		if l.stacked {
			if got, want := l.changes.rect.y+l.changes.rect.h, l.preview.rect.y; got != want {
				t.Errorf("%dx%d: preview starts at row %d, want %d", size[0], size[1], got, want)
			}
		} else if got, want := l.changes.rect.x+l.changes.rect.w, l.preview.rect.x; got != want {
			t.Errorf("%dx%d: preview starts at column %d, want %d", size[0], size[1], got, want)
		}
	}
}

func TestWheelIgnoredInCommitMode(t *testing.T) {
	m := loaded(numberedEntries(4)...)
	m, _ = press(t, m, "c")
	if m.mode != modeCommit {
		t.Fatal("commit prompt did not open")
	}
	next := wheel(t, m, tea.MouseButtonWheelDown, m.width-4, 10)
	if next.preview.scroll != m.preview.scroll || next.cursor != m.cursor {
		t.Error("wheel scrolled while the commit prompt was open")
	}
}

func TestPreviewScrollResetsOnNewPreview(t *testing.T) {
	m := diffPreviewModel(t)
	l := m.layout()
	m = wheel(t, m, tea.MouseButtonWheelDown, l.preview.rect.x+2, l.preview.rect.y+2)
	if m.preview.scroll == 0 {
		t.Fatal("setup: preview did not scroll")
	}
	m = m.onPreview(previewMsg{
		seq:    m.previewSeq,
		kind:   previewLog,
		path:   "a",
		result: svnResult{args: []string{"log"}, stdout: "r1 | one\n"},
	})
	if m.preview.scroll != 0 {
		t.Errorf("new preview started at scroll %d, want 0", m.preview.scroll)
	}
}

func TestPreviewScrollAfterResize(t *testing.T) {
	m := diffPreviewModel(t)
	l := m.layout()
	for i := 0; i < 20; i++ {
		m = wheel(t, m, tea.MouseButtonWheelDown, l.preview.rect.x+2, l.preview.rect.y+2)
	}

	m.height = 12 // shrink: the old offset no longer fits
	view := m.View()
	if view == "" {
		t.Fatal("empty view after resize")
	}
	l = m.layout()
	for i := 0; i < 100; i++ {
		m = wheel(t, m, tea.MouseButtonWheelDown, l.preview.rect.x+2, l.preview.rect.y+2)
	}
	if got, want := m.preview.scroll, diffMaxOffset(m.preview.diff, l.preview.textWidth(), m.previewBodyRows()); got != want {
		t.Errorf("scroll = %d after resize, want %d", got, want)
	}
}

func TestRenderDiffAtClampsOffset(t *testing.T) {
	doc := diffPreviewModel(t).preview.diff
	for _, off := range []int{-100, 0, 3, 10_000} {
		if got := len(renderDiffAt(doc, 80, 20, off)); got > 20 {
			t.Errorf("offset %d: %d rows, want at most 20", off, got)
		}
	}
	if !slices.Equal(renderDiff(doc, 80, 20), renderDiffAt(doc, 80, 20, -5)) {
		t.Error("negative offsets should render the top page")
	}
	last := renderDiffAt(doc, 80, 20, 10_000)
	if got := len(last); got != 20 {
		t.Errorf("past-the-end offset rendered %d rows, want a full 20-row page", got)
	}
	if !strings.Contains(strings.Join(last, "\n"), "- 60 old") {
		t.Error("last page does not reach the end of the diff")
	}
}
