package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	diffMetaStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	diffHunkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	diffDelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	diffAddStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	diffContextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	diffSepStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	diffHeadStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)

	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	promptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
)

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	header := lipgloss.NewStyle().Bold(true).Render("LazySVN - SVN TUI")
	bodyH := max(6, m.height-4)

	frameW := paneStyle().GetHorizontalFrameSize()
	frameH := paneStyle().GetVerticalFrameSize()
	var body string

	// Use a stacked layout in narrower terminals to avoid right-edge clipping.
	if m.width < 110 {
		contentW := max(8, m.width-frameW)
		topOuterH := bodyH * 45 / 100
		if topOuterH < 4 {
			topOuterH = bodyH / 2
		}
		bottomOuterH := bodyH - topOuterH
		top := m.renderChangesPane(contentW, max(3, topOuterH-frameH))
		bottom := m.renderPreviewPane(contentW, max(3, bottomOuterH-frameH))
		body = lipgloss.JoinVertical(lipgloss.Left, top, bottom)
	} else {
		leftTotalW := m.width * 45 / 100
		if leftTotalW < 40 {
			leftTotalW = m.width / 2
		}
		rightTotalW := m.width - leftTotalW
		left := m.renderChangesPane(max(8, leftTotalW-frameW), max(3, bodyH-frameH))
		right := m.renderPreviewPane(max(8, rightTotalW-frameW), max(3, bodyH-frameH))
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}

	return strings.Join([]string{header, body, m.renderFooter(m.width)}, "\n")
}

var paneBase = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1)

func paneStyle() lipgloss.Style { return paneBase }

func (m model) renderChangesPane(width, height int) string {
	border := paneStyle().Width(width).Height(height)
	rows := max(1, height)
	// lipgloss Width includes padding, so text gets less room than the block.
	width = max(8, width-paneStyle().GetHorizontalPadding())

	title := "Changes"
	if n := len(m.marked); n > 0 {
		title += fmt.Sprintf(" · %d marked", n)
	}
	if m.statusErr != nil {
		title += " " + errorStyle.Render("(stale)")
	}
	lines := []string{title}

	switch {
	case len(m.entries) == 0 && m.statusErr != nil:
		lines = append(lines, errorStyle.Render("svn status failed; see preview."))
	case len(m.entries) == 0 && m.statusLoading:
		lines = append(lines, dimStyle.Render("Loading svn status..."))
	case len(m.entries) == 0:
		lines = append(lines, "Working copy clean.")
	}

	start := m.scrollStart(rows - 1)
	end := min(len(m.entries), start+rows-1)
	for i := start; i < end; i++ {
		e := m.entries[i]
		cursor, mark := " ", " "
		if i == m.cursor {
			cursor = ">"
		}
		if _, ok := m.marked[e.Path]; ok {
			mark = "*"
		}
		line := fmt.Sprintf("%s%s [%s] %s", cursor, mark, statusCodeStyle(e.Code).Render(string(e.Code)), e.Path)
		if e.Changelist != "" {
			line += dimStyle.Render(" (" + e.Changelist + ")")
		}
		lines = append(lines, truncate(line, width))
	}

	return border.Render(strings.Join(lines, "\n"))
}

func (m model) renderPreviewPane(width, height int) string {
	border := paneStyle().Width(width).Height(height)
	rows := max(1, height)
	// lipgloss Width includes padding, so text gets less room than the block.
	width = max(8, width-paneStyle().GetHorizontalPadding())

	var lines []string
	switch m.mode {
	case modeCommit:
		lines = m.commitOverlay(width)
	case modeConfirmRevert:
		lines = m.revertOverlay(width)
	default:
		title := m.preview.title
		if m.loadingPreview {
			title += dimStyle.Render("  (loading...)")
		}
		lines = make([]string, 0, rows)
		lines = append(lines, truncate(title, width))
		if m.preview.kind == previewDiff && m.preview.diff != nil {
			lines = append(lines, renderDiff(m.preview.diff, width, rows-1)...)
		} else {
			for _, l := range m.preview.lines[:min(len(m.preview.lines), rows-1)] {
				lines = append(lines, truncate(expandTabs(l), width))
			}
		}
	}
	if len(lines) > rows {
		lines = lines[:rows]
	}
	return border.Render(strings.Join(lines, "\n"))
}

func (m model) commitOverlay(width int) []string {
	lines := []string{
		promptStyle.Render(fmt.Sprintf("Commit %d path(s):", len(m.pendingPaths))),
	}
	lines = append(lines, truncateLines(splitLines(bulletList(m.pendingPaths)), width)...)
	lines = append(lines, "", dimStyle.Render("Enter commit · Esc cancel"))
	if m.notice.level == noticeError && m.notice.text != "" {
		lines = append(lines, errorStyle.Render(truncate(m.notice.text, width)))
	}
	return lines
}

func (m model) revertOverlay(width int) []string {
	lines := []string{
		warnStyle.Render(fmt.Sprintf("Revert %d path(s)? Local changes will be lost:", len(m.pendingPaths))),
	}
	lines = append(lines, truncateLines(splitLines(bulletList(m.pendingPaths)), width)...)
	lines = append(lines, "", dimStyle.Render("y revert · n / Esc cancel"))
	return lines
}

func (m model) renderFooter(width int) string {
	width = max(8, width)
	switch m.mode {
	case modeCommit:
		return promptStyle.Render(truncate(fmt.Sprintf("Commit (%d path(s)): %s", len(m.pendingPaths), m.commitInput.View()), width))
	case modeConfirmRevert:
		return promptStyle.Render(truncate(fmt.Sprintf("Revert %d path(s)? Press y or n.", len(m.pendingPaths)), width))
	}

	help := "j/k move  space mark  esc unmark  d diff  l log  c commit  a add  v revert  u update  r refresh  q quit"
	status, style := m.statusText()
	return dimStyle.Render(truncate(help+" | ", width)) + style.Render(truncate(status, max(0, width-ansi.StringWidth(help+" | "))))
}

// statusText derives the footer message from state, in priority order:
// a running operation, the latest notice, a pending load, then a summary.
func (m model) statusText() (string, lipgloss.Style) {
	switch {
	case m.running != opNone:
		return fmt.Sprintf("Running svn %s...", m.running), promptStyle
	case m.notice.text != "":
		if m.notice.level == noticeError {
			return m.notice.text, errorStyle
		}
		return m.notice.text, dimStyle
	case m.statusLoading:
		return "Refreshing status...", dimStyle
	case m.loadingPreview && m.previewLoading == previewDiff:
		return "Loading diff...", dimStyle
	case m.loadingPreview && m.previewLoading == previewLog:
		return "Loading log...", dimStyle
	case m.statusErr != nil:
		return "svn status failed; the list may be stale.", errorStyle
	case len(m.entries) == 0:
		return "Working copy clean.", dimStyle
	}
	return fmt.Sprintf("%d change(s).", len(m.entries)), dimStyle
}

func (m model) scrollStart(rows int) int {
	if len(m.entries) <= rows {
		return 0
	}
	return clamp(m.cursor-rows/2, 0, len(m.entries)-rows)
}

var (
	codeStyleM       = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	codeStyleA       = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	codeStyleD       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	codeStyleUnver   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	codeStyleMissing = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	codeStyleC       = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	codeStyleOther   = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
)

func statusCodeStyle(code rune) lipgloss.Style {
	switch code {
	case 'M':
		return codeStyleM
	case 'A':
		return codeStyleA
	case 'D':
		return codeStyleD
	case '?':
		return codeStyleUnver
	case '!':
		return codeStyleMissing
	case 'C':
		return codeStyleC
	default:
		return codeStyleOther
	}
}

// truncate cuts s to maxLen terminal cells. It is ANSI-aware, so styled text
// is never cut inside an escape sequence, and counts wide runes correctly.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	return ansi.Truncate(s, maxLen, "…")
}

func truncateLines(lines []string, width int) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, truncate(expandTabs(line), width))
	}
	return out
}

func padRight(s string, width int) string {
	if w := ansi.StringWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}
