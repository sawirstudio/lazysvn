package main

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestRangeSelectionFollowsCursorAndMergesMarks(t *testing.T) {
	m := loaded(
		statusEntry{Code: 'M', Path: "alpha"},
		statusEntry{Code: 'M', Path: "bravo"},
		statusEntry{Code: 'M', Path: "charlie"},
		statusEntry{Code: 'M', Path: "delta"},
	)
	m, _ = press(t, m, "G", " ", "g", "j", "V")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(model)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "+ [M] bravo") || !strings.Contains(view, "+ [M] charlie") ||
		!strings.Contains(view, ">* [M] delta") || strings.Contains(view, "+ [M] alpha") {
		t.Fatalf("unexpected expanded range:\n%s", view)
	}
	if got := m.selectedTargets(); !slices.Equal(got, []string{"delta"}) {
		t.Fatalf("targets before confirmation = %q, want only the existing mark", got)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = next.(model)
	view = ansi.Strip(m.View())
	if !strings.Contains(view, ">+ [M] alpha") || !strings.Contains(view, "+ [M] bravo") ||
		strings.Contains(view, "+ [M] charlie") {
		t.Fatalf("unexpected range after moving across the anchor:\n%s", view)
	}
	m, _ = press(t, m, "V", "v")
	if want := []string{"alpha", "bravo", "delta"}; m.mode != modeConfirmRevert || !slices.Equal(m.pendingPaths, want) {
		t.Fatalf("revert prompt mode = %v, paths = %q, want %q", m.mode, m.pendingPaths, want)
	}
}

func TestRangeSelectionRespectsFilterAndEscape(t *testing.T) {
	m := loaded(
		statusEntry{Code: 'M', Path: "alpha"},
		statusEntry{Code: '?', Path: "bravo"},
		statusEntry{Code: 'M', Path: "charlie"},
		statusEntry{Code: '?', Path: "delta"},
	)
	m, _ = press(t, m, "G", " ")
	m, _ = filterBy(t, m, "", "M")
	m, _ = press(t, m, "V", "k", "v")
	if m.mode != modeSelectRange {
		t.Fatal("lowercase v interrupted range selection")
	}
	if _, cmd := press(t, m, "ctrl+c"); !isQuit(cmd) {
		t.Fatal("Ctrl+C did not quit from range selection")
	}
	if _, cmd := press(t, m, "q"); !isQuit(cmd) {
		t.Fatal("q did not quit from range selection")
	}
	m, _ = press(t, m, "esc")
	if _, marked := m.marked["delta"]; m.mode != modeNormal || !m.filter.active() || !marked || len(m.marked) != 1 {
		t.Fatalf("Esc changed the filter or marks: mode=%v filter=%+v marks=%v", m.mode, m.filter, m.marked)
	}
	m, _ = press(t, m, "V", "G", " ")
	if want := []string{"alpha", "charlie"}; m.mode != modeNormal || !slices.Equal(m.selectedTargets(), want) {
		t.Fatalf("filtered selection = %q, want %q", m.selectedTargets(), want)
	}
	m, _ = press(t, m, "esc")
	if want := []string{"alpha", "charlie", "delta"}; !slices.Equal(m.selectedTargets(), want) {
		t.Fatalf("selection after clearing filter = %q, want %q", m.selectedTargets(), want)
	}
}

func TestRangeSelectionSingleRowAndEmptyList(t *testing.T) {
	m, _ := press(t, loaded(), "V")
	if m.mode != modeNormal || len(m.marked) != 0 {
		t.Fatal("Shift+V started a range in an empty list")
	}
	m, _ = press(t, loaded(abc...), "j", "V", "V")
	if _, marked := m.marked["b"]; m.mode != modeNormal || !marked || len(m.marked) != 1 {
		t.Fatalf("single-row selection mode = %v, marks = %v", m.mode, m.marked)
	}
	m, _ = filterBy(t, m, "absent", "")
	m, _ = press(t, m, "V")
	if m.mode != modeNormal || len(m.marked) != 1 {
		t.Fatal("Shift+V started a range with no visible changes")
	}
}

func TestRangeSelectionRefreshCancelsPreview(t *testing.T) {
	m, _ := press(t, loaded(abc...), " ", "r", "V", "G")
	next, _ := m.Update(statusMsg{seq: m.statusSeq - 1, entries: abc})
	m = next.(model)
	if m.mode != modeSelectRange {
		t.Fatal("superseded status reply canceled the range")
	}
	next, _ = m.Update(statusMsg{seq: m.statusSeq, entries: abc[:2]})
	m = next.(model)
	if m.mode != modeNormal || !slices.Equal(m.selectedTargets(), []string{"a"}) {
		t.Fatalf("after refresh, mode = %v, targets = %q", m.mode, m.selectedTargets())
	}
}

func TestRangeSelectionUsesDisplayedSortOrder(t *testing.T) {
	m := loaded(
		statusEntry{Code: 'A', Path: "alpha"},
		statusEntry{Code: 'M', Path: "bravo"},
		statusEntry{Code: 'D', Path: "charlie"},
		statusEntry{Code: 'M', Path: "delta"},
	)
	tab := tea.KeyMsg{Type: tea.KeyTab}
	next, _ := m.Update(tab)
	m, _ = press(t, next.(model), "g", "V", "j")
	next, _ = m.Update(tab)
	m, _ = press(t, next.(model), "V")
	if want := []string{"bravo", "delta"}; m.sortOrder != sortByStatus || !slices.Equal(m.selectedTargets(), want) {
		t.Fatalf("sort order = %v, targets = %q, want %q", m.sortOrder, m.selectedTargets(), want)
	}
}

func TestRangeSelectionWheelMovementFollowsDiff(t *testing.T) {
	m, _ := press(t, loaded(abc...), "d", "V")
	next, _ := m.Update(tea.MouseMsg{
		X: 1, Y: 2, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	m = next.(model)
	if m.currentPath() != "c" || m.preview.path != "c" {
		t.Fatalf("after scrolling, current path = %q, diff path = %q", m.currentPath(), m.preview.path)
	}
	m, _ = press(t, m, " ")
	if want := []string{"a", "b", "c"}; !slices.Equal(m.selectedTargets(), want) {
		t.Fatalf("wheel selection = %q, want %q", m.selectedTargets(), want)
	}
}
