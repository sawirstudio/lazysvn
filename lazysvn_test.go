package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	if code := runCLI([]string{"update", "--force"}); code != 2 {
		t.Errorf("exit = %d, want 2", code)
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

	am := actionCmd(opRevert, []string{"-dash.txt"}, "", "")().(actionMsg)
	if am.result.err != nil {
		t.Errorf("revert of dash-prefixed path failed: %s", am.result.transcript())
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
