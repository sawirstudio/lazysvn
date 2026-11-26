package main

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type changeFilter struct {
	path       string
	statusText string
	codes      string
}

const (
	filterStatusField = iota
	filterPathField
	filterFieldCount
)

func (f changeFilter) active() bool {
	return f.path != "" || f.codes != ""
}

func parseChangeFilter(path, statuses string) (changeFilter, error) {
	filter := changeFilter{path: strings.TrimSpace(path)}
	for _, token := range strings.FieldsFunc(statuses, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	}) {
		var code rune
		upper := strings.ToUpper(token)
		switch {
		case len(upper) == 1 && strings.ContainsRune("MADRCGI?!~", rune(upper[0])):
			code = rune(upper[0])
		case strings.EqualFold(token, "modify"):
			code = 'M'
		default:
			code = itemCode([]byte(strings.ToLower(token)))
		}
		if code == ' ' {
			return changeFilter{}, fmt.Errorf("Unknown SVN status %q", token)
		}
		if !strings.ContainsRune(filter.codes, code) {
			filter.codes += string(code)
		}
	}
	if filter.codes != "" {
		filter.statusText = strings.TrimSpace(statuses)
	}
	return filter, nil
}

func (m *model) applyFilter(filter changeFilter) tea.Cmd {
	previousPath := m.currentPath()
	m.filter = filter
	m.rebuildVisibleRows(previousPath)
	return m.syncVisiblePreview(previousPath)
}

func (m *model) openFilter() tea.Cmd {
	m.mode = modeFilter
	m.filterField = filterStatusField
	for i := range m.filterInputs {
		m.filterInputs[i] = textinput.New()
		m.filterInputs[i].Prompt = ""
	}
	m.filterInputs[filterStatusField].Placeholder = "modify,A"
	m.filterInputs[filterPathField].Placeholder = "src/camera.go"
	m.resizeFilterInputs()
	m.filterInputs[filterStatusField].SetValue(m.filter.statusText)
	m.filterInputs[filterPathField].SetValue(m.filter.path)
	return m.filterInputs[filterStatusField].Focus()
}

func (m *model) resizeFilterInputs() {
	widths := filterBarWidths(m.layout().changes.textWidth())
	for i, width := range widths {
		input := &m.filterInputs[i]
		// Reserve a focus marker and room for the cursor after the value.
		inputWidth := max(1, width-len(filterFieldLabel(i, width))-3)
		if input.Width != inputWidth {
			input.Width = inputWidth
			pos := input.Position()
			input.CursorEnd()
			input.CursorStart()
			input.SetCursor(pos)
		}
	}
}

func (m model) updateFilter(msg tea.KeyMsg) (model, tea.Cmd) {
	m.notice = notice{}
	switch msg.String() {
	case "esc":
		m.filterInputs[m.filterField].Blur()
		m.mode = modeNormal
		return m, nil
	case "enter":
		filter, err := parseChangeFilter(
			m.filterInputs[filterPathField].Value(),
			m.filterInputs[filterStatusField].Value(),
		)
		if err != nil {
			m.setNotice(noticeError, err.Error())
			return m, nil
		}
		m.filterInputs[m.filterField].Blur()
		m.mode = modeNormal
		cmd := m.applyFilter(filter)
		return m, cmd
	case "tab", "shift+tab":
		m.filterInputs[m.filterField].Blur()
		m.filterField = (m.filterField + 1) % len(m.filterInputs)
		cmd := m.filterInputs[m.filterField].Focus()
		return m, cmd
	}
	cmd := m.updateFilterInput(msg)
	return m, cmd
}

func (m *model) updateFilterInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	field := m.filterField
	m.filterInputs[field], cmd = m.filterInputs[field].Update(msg)
	return cmd
}
