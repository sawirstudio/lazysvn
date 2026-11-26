package main

import (
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type sortOrder uint8

const (
	sortByPath sortOrder = iota
	sortByStatus
)

func (order sortOrder) String() string {
	if order == sortByStatus {
		return "status"
	}
	return "path"
}

func statusSortRank(code rune) rune {
	switch code {
	case 'M':
		return -2
	case 'A':
		return -1
	default:
		return code
	}
}

func (m *model) toggleSortOrder() {
	previousPath := m.currentPath()
	if m.sortOrder == sortByPath {
		m.sortOrder = sortByStatus
	} else {
		m.sortOrder = sortByPath
	}
	m.rebuildVisibleRows(previousPath)
}

// Unfiltered path order can use entries directly because parseStatusXML sorts by path.
func (m model) usesVisibleRows() bool {
	return m.filter.active() || m.sortOrder == sortByStatus
}

func (m model) visibleCount() int {
	if m.usesVisibleRows() {
		return len(m.visibleRows)
	}
	return len(m.entries)
}

func (m model) visibleEntry(row int) statusEntry {
	if m.usesVisibleRows() {
		row = m.visibleRows[row]
	}
	return m.entries[row]
}

func (m model) visibleIndex(path string) int {
	if path != "" {
		for i := 0; i < m.visibleCount(); i++ {
			if m.visibleEntry(i).Path == path {
				return i
			}
		}
	}
	return -1
}

func (m *model) rebuildVisibleRows(previousPath string) {
	m.visibleRows = nil
	if m.usesVisibleRows() {
		query := strings.ToLower(filepath.ToSlash(m.filter.path))
		for i, entry := range m.entries {
			if m.filter.codes != "" && !strings.ContainsRune(m.filter.codes, entry.Code) {
				continue
			}
			if query == "" || strings.Contains(strings.ToLower(filepath.ToSlash(entry.Path)), query) {
				m.visibleRows = append(m.visibleRows, i)
			}
		}
		if m.sortOrder == sortByStatus {
			// Keep the original path order within each status group.
			sort.SliceStable(m.visibleRows, func(i, j int) bool {
				left := m.entries[m.visibleRows[i]].Code
				right := m.entries[m.visibleRows[j]].Code
				return statusSortRank(left) < statusSortRank(right)
			})
		}
	}

	if row := m.visibleIndex(previousPath); row >= 0 {
		m.cursor = row
	} else {
		m.cursor = clamp(m.cursor, 0, m.visibleCount()-1)
	}
}

func (m *model) syncVisiblePreview(previousPath string) tea.Cmd {
	if m.preview.path != "" && m.visibleIndex(m.preview.path) < 0 {
		m.preview = preview{kind: previewHelp, title: "Help"}.withText(helpText())
	}
	if m.loadingPreview && m.visibleIndex(m.previewPath) < 0 {
		m.cancelPreviewRequest()
	}
	if m.currentPath() != previousPath {
		return m.followCursor()
	}
	return nil
}
