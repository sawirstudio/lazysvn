package main

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func filterBy(t *testing.T, m model, path, statuses string) (model, tea.Cmd) {
	t.Helper()
	m, _ = press(t, m, "/")
	m.filterInputs[filterPathField].SetValue(path)
	m.filterInputs[filterStatusField].SetValue(statuses)
	m, cmd := press(t, m, "enter")
	if m.mode != modeNormal {
		t.Fatalf("apply filter: %s", m.notice.text)
	}
	return m, cmd
}

func TestChangeFilterMatchesPathAndStatus(t *testing.T) {
	entries := []statusEntry{
		{Code: 'M', Path: "src/Camera.go"},
		{Code: 'A', Path: "src/audio.go"},
		{Code: 'D', Path: "src/old.go"},
		{Code: '?', Path: "src/new.go"},
		{Code: '!', Path: "src/missing.go"},
		{Code: 'M', Path: "test/camera.go"},
		{Code: 'C', PropCode: 'M', Path: "src/conflict.go"},
	}
	for _, tc := range []struct {
		path, statuses string
		want           []string
	}{
		{path: " camera.GO ", want: []string{"src/Camera.go", "test/camera.go"}},
		{statuses: "modified", want: []string{"src/Camera.go", "test/camera.go"}},
		{path: "SRC/", statuses: "MoDiFy, a", want: []string{"src/Camera.go", "src/audio.go"}},
		{statuses: "? !", want: []string{"src/new.go", "src/missing.go"}},
		{statuses: "conflicted", want: []string{"src/conflict.go"}},
		{statuses: "replaced"},
	} {
		m, _ := filterBy(t, loaded(entries...), tc.path, tc.statuses)
		var got []string
		for i := 0; i < m.visibleCount(); i++ {
			got = append(got, m.visibleEntry(i).Path)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("filter %q / %q = %q, want %q", tc.path, tc.statuses, got, tc.want)
		}
	}
}

func TestChangeFilterEditorAndEmptyResults(t *testing.T) {
	m := loaded(abc...)
	m, _ = press(t, m, "/", "modified")
	if m.filterField != filterStatusField || !m.filterInputs[filterStatusField].Focused() {
		t.Fatal("opening the filter did not focus status")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = press(t, next.(model), "a")
	if m.filterField != filterPathField || !m.filterInputs[filterPathField].Focused() {
		t.Fatal("Tab did not focus the file input")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(model)
	if m.filterField != filterStatusField || !m.filterInputs[filterStatusField].Focused() || m.filterInputs[filterPathField].Focused() {
		t.Fatal("Shift+Tab did not return focus to status")
	}
	if _, cmd := press(t, m, "ctrl+c"); !isQuit(cmd) {
		t.Fatal("Ctrl+C did not quit from filter mode")
	}
	m, _ = press(t, m, "enter")
	if m.visibleCount() != 1 || m.currentPath() != "a" || !strings.Contains(m.View(), "Changes 1/3") {
		t.Fatal("editor did not apply both fields")
	}

	m, _ = press(t, m, "/")
	if m.filterInputs[filterPathField].Value() != "a" || m.filterInputs[filterStatusField].Value() != "modified" {
		t.Fatal("editor did not reopen with the active filter")
	}
	m.filterInputs[filterStatusField].SetValue("unknown")
	m, _ = press(t, m, "enter")
	if m.mode != modeFilter || m.visibleCount() != 1 || !strings.Contains(m.View(), "Unknown SVN status") {
		t.Fatal("invalid input did not preserve the active filter and report an error")
	}
	m, _ = press(t, m, "esc")
	if m.mode != modeNormal || m.visibleCount() != 1 {
		t.Fatal("cancel did not preserve the active filter")
	}

	m, _ = filterBy(t, m, "absent", "")
	if m.visibleCount() != 0 || m.currentPath() != "" || !strings.Contains(m.View(), "No matching changes.") {
		t.Fatal("empty filtered list has a selection or lacks an empty state")
	}
	clean, _ := filterBy(t, loaded(), "absent", "")
	if !strings.Contains(clean.View(), "Working copy clean.") {
		t.Fatal("clean working copy was displayed as a filter miss")
	}
	m, _ = press(t, m, "esc")
	if m.visibleCount() != len(abc) || m.filter.active() {
		t.Fatal("Esc did not clear the active filter")
	}
}

func TestChangeFilterNavigationAndRefreshPreview(t *testing.T) {
	m := loaded(
		statusEntry{Code: 'M', Path: "a"},
		statusEntry{Code: 'A', Path: "b"},
		statusEntry{Code: 'M', Path: "c"},
		statusEntry{Code: 'A', Path: "d"},
		statusEntry{Code: 'M', Path: "e"},
	)
	m, _ = press(t, m, "j", "d")
	oldSeq := m.previewSeq
	m, cmd := filterBy(t, m, "", "M")
	if m.currentPath() != "c" || cmd == nil || m.previewPath != "c" {
		t.Fatal("applying a filter did not move the cursor and diff to the matching row")
	}
	next, _ := m.Update(previewMsg{seq: oldSeq, kind: previewDiff, path: "b"})
	if next.(model).preview.path == "b" {
		t.Fatal("an old diff was shown for a hidden row")
	}
	m, _ = press(t, m, "G", "k")
	if m.currentPath() != "c" {
		t.Fatalf("keyboard navigated to %q, want c", m.currentPath())
	}
	list := m.layout().changes.rect
	m = wheel(t, m, tea.MouseButtonWheelDown, list.x+1, list.y+1)
	if m.currentPath() != "e" {
		t.Fatalf("mouse wheel navigated to %q, want e", m.currentPath())
	}
	next, _ = m.Update(statusMsg{seq: m.statusSeq, entries: []statusEntry{
		{Code: 'M', Path: "e"}, {Code: 'M', Path: "f"},
	}})
	m = next.(model)
	if m.currentPath() != "e" || m.cursor != 0 {
		t.Fatalf("refresh did not keep visible path e, got %q at %d", m.currentPath(), m.cursor)
	}
	m = m.onPreview(previewMsg{seq: m.previewSeq, kind: previewDiff, path: "e"})
	next, cmd = m.Update(statusMsg{seq: m.statusSeq, entries: []statusEntry{
		{Code: 'A', Path: "e"}, {Code: 'M', Path: "f"},
	}})
	m = next.(model)
	if m.currentPath() != "f" || m.previewPath != "f" || cmd == nil || m.preview.path == "e" {
		t.Fatal("refresh did not switch the diff after the selected path stopped matching")
	}
	oldSeq = m.previewSeq
	next, cmd = m.Update(statusMsg{seq: m.statusSeq, entries: []statusEntry{{Code: 'A', Path: "e"}}})
	m = next.(model)
	if m.currentPath() != "" || m.loadingPreview || m.preview.path != "" || m.previewSeq == oldSeq || cmd != nil {
		t.Fatal("empty results did not clear the file preview and invalidate its in-flight request")
	}
}

func TestChangeFilterActionTargets(t *testing.T) {
	m := loaded(
		statusEntry{Code: 'M', Path: "hidden"},
		statusEntry{Code: 'D', Path: "old.go", MovedTo: "new.go"},
		statusEntry{Code: 'A', Path: "new.go", MovedFrom: "old.go"},
		statusEntry{Code: '?', Path: "added-later"},
		statusEntry{Code: 'A', Path: "unmarked.go"},
	)
	m.marked["hidden"] = struct{}{}
	m.marked["new.go"] = struct{}{}
	m, _ = filterBy(t, m, "", "A")
	m, _ = press(t, m, "G") // Current row is visible but unmarked.
	for _, shortcut := range []string{"c", "v"} {
		prompt, _ := press(t, m, shortcut)
		if !slices.Equal(prompt.pendingPaths, []string{"new.go", "old.go"}) {
			t.Errorf("%s targets = %q, want visible mark and other side of move", shortcut, prompt.pendingPaths)
		}
	}

	m, _ = filterBy(t, m, "", "?")
	if targets, skipped := m.addTargets(); !slices.Equal(targets, []string{"added-later"}) || skipped != 0 {
		t.Fatalf("add targets = %q, skipped = %d, want current row and no skips", targets, skipped)
	}
	m, _ = filterBy(t, m, "", "!")
	if targets := m.selectedTargets(); len(targets) != 0 {
		t.Fatalf("empty filtered list selected hidden targets %q", targets)
	}
	m, _ = press(t, m, "esc")
	if targets := m.selectedTargets(); !slices.Equal(targets, []string{"hidden", "new.go"}) {
		t.Fatalf("clearing the filter did not restore hidden marks, got %q", targets)
	}
	m, _ = press(t, m, "esc")
	if len(m.marked) != 0 {
		t.Fatal("Esc without a filter did not clear marks")
	}
}

func TestChangeFilterEditorFitsAfterResize(t *testing.T) {
	m := loaded(abc...)
	m, _ = press(t, m, "/", "modify,A")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = press(t, next.(model), strings.Repeat("摄像机", 30))
	for _, width := range []int{48, 130, 34} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m = next.(model)
		bar := ansi.Strip(m.renderFilterBar(m.layout().changes.textWidth()))
		if !strings.Contains(bar, "modify,A") || !strings.Contains(bar, "│ > File") || !strings.Contains(bar, "机") {
			t.Fatalf("input fields at width %d = %q", width, bar)
		}
		for _, line := range strings.Split(m.View(), "\n") {
			if ansi.StringWidth(line) > width {
				t.Errorf("width %d: overlong line %q", width, line)
			}
		}
	}
}

func TestChangeFilterBarStaysAtBottomOfChangesPane(t *testing.T) {
	for _, width := range []int{80, 130} {
		m := loaded(numberedEntries(40)...)
		m.width, m.height = width, 24
		m.preview = preview{kind: previewLog, title: "Log", path: "f39"}.withText("Recent commit")
		m, _ = press(t, m, "G")
		layout := m.layout()
		originalPreview := m.renderPreviewPane(layout.preview)

		m, _ = press(t, m, "/", "M")
		if got := m.renderPreviewPane(layout.preview); got != originalPreview {
			t.Fatal("editing the filter changed the preview")
		}
		checkBottomRow := func(want string) {
			t.Helper()
			lines := strings.Split(ansi.Strip(m.renderChangesPane(layout.changes)), "\n")
			if len(lines) != layout.changes.rect.h || !strings.Contains(lines[len(lines)-2], want) {
				t.Fatalf("width %d: expected %q directly above the bottom border, got %q", width, want, lines)
			}
			if !strings.Contains(strings.Join(lines[:len(lines)-2], "\n"), "f39") {
				t.Fatalf("width %d: last selected file is hidden by the bar", width)
			}
		}
		checkBottomRow("> Status M")

		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m, _ = press(t, next.(model), "f39", "enter")
		checkBottomRow("File f39")
		if bar := ansi.Strip(m.renderFilterBar(layout.changes.textWidth())); !strings.Contains(bar, "Status M") || strings.Contains(bar, ">") {
			t.Fatalf("applied filter bar = %q", bar)
		}
		m, _ = press(t, m, "esc")
		if strings.Contains(m.renderChangesPane(layout.changes), "Status") {
			t.Fatal("cleared filter bar is still visible")
		}
	}
}
