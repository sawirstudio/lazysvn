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

// rect is a pane's on-screen box in terminal cells, with the origin at the
// top-left of the rendered view. contains treats it as [x, x+w) × [y, y+h).
type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// paneBox is one bordered pane. rect is what the user sees; innerW and innerH
// are the sizes handed to renderChangesPane and renderPreviewPane. lipgloss
// draws the border outside Width/Height, hence the +border in the rect.
type paneBox struct {
	rect   rect
	innerW int
	innerH int
}

// textWidth is the room left for pane text once padding is taken out.
func (b paneBox) textWidth() int {
	return max(8, b.innerW-paneStyle().GetHorizontalPadding())
}

// textHeight is the number of text rows the pane body holds.
func (b paneBox) textHeight() int { return max(1, b.innerH) }

type appLayout struct {
	changes paneBox
	preview paneBox
	stacked bool
}

// layout is the single source of truth for where the panes land: View renders
// from it and mouse hit-testing reads it, so the two cannot drift apart.
func (m model) layout() appLayout {
	bodyH := max(6, m.height-4)
	frameW := paneStyle().GetHorizontalFrameSize()
	frameH := paneStyle().GetVerticalFrameSize()
	borderW := paneStyle().GetHorizontalBorderSize()
	borderH := paneStyle().GetVerticalBorderSize()
	box := func(x, y, innerW, innerH int) paneBox {
		return paneBox{
			rect:   rect{x: x, y: y, w: innerW + borderW, h: innerH + borderH},
			innerW: innerW,
			innerH: innerH,
		}
	}

	// Use a stacked layout in narrower terminals to avoid right-edge clipping.
	if m.width < 110 {
		contentW := max(8, m.width-frameW)
		topOuterH := bodyH * 45 / 100
		if topOuterH < 4 {
			topOuterH = bodyH / 2
		}
		bottomOuterH := bodyH - topOuterH
		top := box(0, 1, contentW, max(3, topOuterH-frameH))
		bottom := box(0, 1+top.rect.h, contentW, max(3, bottomOuterH-frameH))
		return appLayout{changes: top, preview: bottom, stacked: true}
	}

	leftTotalW := m.width * 45 / 100
	if leftTotalW < 40 {
		leftTotalW = m.width / 2
	}
	rightTotalW := m.width - leftTotalW
	innerH := max(3, bodyH-frameH)
	left := box(0, 1, max(8, leftTotalW-frameW), innerH)
	right := box(left.rect.w, 1, max(8, rightTotalW-frameW), innerH)
	return appLayout{changes: left, preview: right}
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	header := lipgloss.NewStyle().Bold(true).Render("LazySVN - SVN TUI")
	l := m.layout()

	var body string
	if l.stacked {
		body = lipgloss.JoinVertical(lipgloss.Left,
			m.renderChangesPane(l.changes),
			m.renderPreviewPane(l.preview))
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderChangesPane(l.changes),
			m.renderPreviewPane(l.preview))
	}

	return strings.Join([]string{header, body, m.renderFooter(m.width)}, "\n")
}

var paneBase = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1)

func paneStyle() lipgloss.Style { return paneBase }

func (m model) renderChangesPane(p paneBox) string {
	border := paneStyle().Width(p.innerW).Height(p.innerH)
	rows := p.textHeight()
	// lipgloss Width includes padding, so text gets less room than the block.
	width := p.textWidth()

	title := "Changes"
	total, visible := len(m.entries), m.visibleCount()
	if m.filter.active() {
		title += fmt.Sprintf(" %d/%d", visible, total)
	}
	title += " · by " + m.sortOrder.String()
	if n := len(m.marked); n > 0 {
		title += fmt.Sprintf(" · %d marked", n)
		if m.filter.active() {
			title += " total"
		}
	}
	if m.statusErr != nil {
		title += " " + errorStyle.Render("(stale)")
	}
	lines := []string{truncate(title, width)}
	showFilter := m.mode == modeFilter || m.filter.active()
	listRows := rows - 1
	if showFilter {
		listRows--
	}

	switch {
	case total == 0 && m.statusErr != nil:
		lines = append(lines, errorStyle.Render(truncate("svn status failed; see preview.", width)))
	case total == 0 && m.statusLoading:
		lines = append(lines, dimStyle.Render(truncate("Loading svn status...", width)))
	case total == 0:
		lines = append(lines, truncate("Working copy clean.", width))
	case visible == 0:
		lines = append(lines, truncate("No matching changes.", width))
	}

	start := m.scrollStart(listRows)
	end := min(visible, start+listRows)
	first, last := m.rangeBounds()
	for i := start; i < end; i++ {
		e := m.visibleEntry(i)
		cursor, mark := " ", " "
		if i == m.cursor {
			cursor = ">"
		}
		if _, ok := m.marked[e.Path]; ok {
			mark = "*"
		} else if m.mode == modeSelectRange && first <= i && i <= last {
			mark = "+"
		}
		line := fmt.Sprintf("%s%s [%s] %s", cursor, mark, statusCodeStyle(e.Code).Render(string(e.Code)), e.Path)
		if e.Changelist != "" {
			line += dimStyle.Render(" (" + e.Changelist + ")")
		}
		lines = append(lines, truncate(line, width))
	}

	if showFilter {
		for len(lines) < rows-1 {
			lines = append(lines, "")
		}
		lines = append(lines, m.renderFilterBar(width))
	}

	return border.Render(strings.Join(lines, "\n"))
}

func (m model) renderPreviewPane(p paneBox) string {
	border := paneStyle().Width(p.innerW).Height(p.innerH)
	rows := p.textHeight()
	// lipgloss Width includes padding, so text gets less room than the block.
	width := p.textWidth()

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
			lines = append(lines, renderDiffAt(m.preview.diff, width, rows-1, m.preview.scroll)...)
		} else {
			lines = append(lines, m.previewTextLines(width, rows-1)...)
		}
	}
	if len(lines) > rows {
		lines = lines[:rows]
	}
	return border.Render(strings.Join(lines, "\n"))
}

// previewTextLines draws the body rows of a plain-text preview at the current
// scroll offset. The offset is clamped here too, so shrinking the window can
// never leave the pane blank past the end of the text.
func (m model) previewTextLines(width, rows int) []string {
	if rows <= 0 {
		return nil
	}
	lines := m.preview.lines
	off := clamp(m.preview.scroll, 0, max(0, len(lines)-rows))
	out := make([]string, 0, min(rows, len(lines)-off))
	for _, l := range lines[off:min(len(lines), off+rows)] {
		out = append(out, truncate(expandTabs(l), width))
	}
	return out
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

const filterBarGap = " │ "

func filterBarWidths(width int) [filterFieldCount]int {
	available := width - ansi.StringWidth(filterBarGap)
	status := max(available/3, min(20, (available+1)/2))
	return [filterFieldCount]int{status, available - status}
}

func filterFieldLabel(field, width int) string {
	if field == filterStatusField {
		if width < 17 {
			return "S "
		}
		return "Status "
	}
	if width < 10 {
		return "F "
	}
	return "File "
}

func (m model) renderFilterBar(width int) string {
	widths := filterBarWidths(width)
	values := [filterFieldCount]string{m.filter.statusText, m.filter.path}
	var fields [filterFieldCount]string
	for i, size := range widths {
		label := filterFieldLabel(i, size)
		prefix := dimStyle.Render("  " + label)
		value := values[i]
		if m.mode == modeFilter {
			if i == m.filterField {
				prefix = promptStyle.Render("> " + label)
			}
			value = m.filterInputs[i].View()
		} else if value == "" {
			value = dimStyle.Render("any")
		}
		fields[i] = padRight(truncate(prefix+value, size), size)
	}
	return strings.Join(fields[:], dimStyle.Render(filterBarGap))
}

func (m model) renderFooter(width int) string {
	width = max(8, width)
	switch m.mode {
	case modeCommit:
		return promptStyle.Render(truncate(fmt.Sprintf("Commit (%d path(s)): %s", len(m.pendingPaths), m.commitInput.View()), width))
	case modeConfirmRevert:
		return promptStyle.Render(truncate(fmt.Sprintf("Revert %d path(s)? Press y or n.", len(m.pendingPaths)), width))
	case modeSelectRange:
		return promptStyle.Render(truncate("Range select  V/Space mark  Esc cancel  j/k move", width))
	case modeFilter:
		if m.notice.level == noticeError && m.notice.text != "" {
			return errorStyle.Render(truncate(m.notice.text, width))
		}
		return dimStyle.Render(truncate("Tab switch fields  Enter apply  Esc cancel", width))
	}

	filterHelp := "/ filter  esc unmark"
	if m.filter.active() {
		filterHelp = "/ filter  esc clear filter"
	}
	help := "tab sort  " + filterHelp + "  j/k  Space/V mark  d diff  l log  c commit  a add  v revert  u update  r refresh  q quit"
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
	case m.filter.active():
		return fmt.Sprintf("%d of %d change(s) match.", m.visibleCount(), len(m.entries)), dimStyle
	}
	return fmt.Sprintf("%d change(s).", len(m.entries)), dimStyle
}

func (m model) scrollStart(rows int) int {
	visible := m.visibleCount()
	if visible <= rows {
		return 0
	}
	return clamp(m.cursor-rows/2, 0, visible-rows)
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
