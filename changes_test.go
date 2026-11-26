package main

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTabTogglesChangeOrdering(t *testing.T) {
	m := loaded(
		statusEntry{Code: 'A', Path: "a-added"},
		statusEntry{Code: 'D', Path: "b-deleted"},
		statusEntry{Code: 'M', Path: "c-modified"},
		statusEntry{Code: '?', Path: "d-unversioned"},
		statusEntry{Code: 'C', Path: "e-conflicted"},
		statusEntry{Code: 'M', Path: "f-modified"},
		statusEntry{Code: 'A', Path: "g-added"},
	)
	m, _ = press(t, m, "j", " ", "d")
	m = m.onPreview(previewMsg{seq: m.previewSeq, kind: previewDiff, path: "b-deleted"})
	previewSeq := m.previewSeq

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	want := []string{"c-modified", "f-modified", "a-added", "g-added", "d-unversioned", "e-conflicted", "b-deleted"}
	if got := visiblePaths(m); !slices.Equal(got, want) {
		t.Fatalf("status sort = %v, want %v", got, want)
	}
	if m.currentPath() != "b-deleted" || !slices.Equal(m.selectedTargets(), []string{"b-deleted"}) {
		t.Fatal("sort changed the selected file or its mark")
	}
	if cmd != nil || m.preview.path != "b-deleted" || m.previewSeq != previewSeq {
		t.Fatal("sort reloaded or replaced the preview")
	}
	if !strings.Contains(m.View(), "by status") {
		t.Fatal("status sort is missing from the Changes pane")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	got := visiblePaths(m)
	if m.sortOrder != sortByPath || !slices.IsSorted(got) || m.currentPath() != "b-deleted" {
		t.Fatalf("switching back to path sort gave %v with selection %q", got, m.currentPath())
	}
}

func TestStatusSortWithFilterAndRefresh(t *testing.T) {
	m := loaded(
		statusEntry{Code: 'A', Path: "a.go"},
		statusEntry{Code: 'M', Path: "b.txt"},
		statusEntry{Code: 'M', Path: "c.go"},
		statusEntry{Code: 'D', Path: "d.go"},
	)
	m.toggleSortOrder()
	m, _ = press(t, m, "/")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	if m.filterField != filterPathField || m.sortOrder != sortByStatus {
		t.Fatal("Tab in the filter editor should switch fields")
	}
	m, _ = press(t, m, "esc")
	m, _ = filterBy(t, m, ".go", "")
	if got := visiblePaths(m); !slices.Equal(got, []string{"c.go", "a.go", "d.go"}) {
		t.Fatalf("filtered status sort = %v", got)
	}
	m, _ = press(t, m, " ", "d")
	current := m.currentPath()
	previewSeq := m.previewSeq
	next, cmd := m.Update(statusMsg{seq: m.statusSeq, entries: []statusEntry{
		{Code: 'M', Path: "a.go"},
		{Code: 'M', Path: "b.txt"},
		{Code: 'M', Path: "c.go"},
		{Code: 'A', Path: "e.go"},
	}})
	m = next.(model)
	if got := visiblePaths(m); !slices.Equal(got, []string{"a.go", "c.go", "e.go"}) {
		t.Fatalf("after refresh = %v", got)
	}
	if m.currentPath() != current || !slices.Equal(m.selectedTargets(), []string{current}) ||
		m.previewSeq != previewSeq || m.previewPath != current || cmd != nil {
		t.Fatal("refresh lost the selected file, mark, or its pending diff")
	}

	m, _ = filterBy(t, m, "absent", "")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	if m.visibleCount() != 0 || m.currentPath() != "" {
		t.Fatal("switching sort revealed hidden rows in an empty filtered list")
	}
	m, _ = press(t, m, "esc")
	if got := visiblePaths(m); len(got) != len(m.entries) || !slices.IsSorted(got) {
		t.Fatalf("clearing the filter after switching sort gave %v", got)
	}
}

func visiblePaths(m model) []string {
	paths := make([]string, m.visibleCount())
	for i := range paths {
		paths[i] = m.visibleEntry(i).Path
	}
	return paths
}
