package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type diffKind int

const (
	diffMeta    diffKind = iota // anything outside a hunk: Index:, ===, ---/+++ headers, property banners
	diffHunk                    // @@ ... @@ or ## ... ## header
	diffContext                 // unchanged line inside a hunk
	diffDel                     // removed line inside a hunk
	diffAdd                     // added line inside a hunk
	diffNote                    // "\ No newline at end of file"
)

// diffLine is one classified line of unified diff output. For content kinds
// text excludes the leading ' ', '-' or '+'; otherwise it is the whole line.
// oldNo/newNo are 1-based file line numbers, or 0 when not applicable
// (property hunks have no file line numbers).
type diffLine struct {
	kind  diffKind
	text  string
	oldNo int
	newNo int
}

// @@ for file content, ## for svn property changes. Omitted counts mean 1.
var hunkHeaderRE = regexp.MustCompile(`^(@@|##) -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? (?:@@|##)`)

// parseDiff classifies unified diff lines with an explicit two-state machine:
//
//	outside ──hunk header──▶ inside(oldLeft, newLeft)
//	inside  ──both counts exhausted or a line that doesn't fit──▶ outside
//
// Because hunk bodies are consumed by count, a removed line whose content starts
// with "--" (so it reads "--- …") or an added "++…" line is still content, and
// headers that follow a hunk (a second file, a property section) are not
// mistaken for hunk lines.
func parseDiff(raw []string) []diffLine {
	var out []diffLine
	var (
		inHunk           bool
		numbered         bool
		oldNo, newNo     int
		oldLeft, newLeft int
		afterContent     bool // previous line was hunk content, so "\" is a note
	)
	lineNo := func(n int) int {
		if numbered {
			return n
		}
		return 0
	}

	for _, line := range raw {
		if afterContent && strings.HasPrefix(line, `\`) {
			out = append(out, diffLine{kind: diffNote, text: line})
			continue
		}
		afterContent = false

		if inHunk && (oldLeft > 0 || newLeft > 0) {
			prefix, body := byte(' '), "" // a blank line in a hunk is an empty context line
			if line != "" {
				prefix, body = line[0], line[1:]
			}
			switch {
			case prefix == ' ' && oldLeft > 0 && newLeft > 0:
				out = append(out, diffLine{kind: diffContext, text: body, oldNo: lineNo(oldNo), newNo: lineNo(newNo)})
				oldNo, newNo, oldLeft, newLeft = oldNo+1, newNo+1, oldLeft-1, newLeft-1
				afterContent = true
				continue
			case prefix == '-' && oldLeft > 0:
				out = append(out, diffLine{kind: diffDel, text: body, oldNo: lineNo(oldNo)})
				oldNo, oldLeft = oldNo+1, oldLeft-1
				afterContent = true
				continue
			case prefix == '+' && newLeft > 0:
				out = append(out, diffLine{kind: diffAdd, text: body, newNo: lineNo(newNo)})
				newNo, newLeft = newNo+1, newLeft-1
				afterContent = true
				continue
			}
			// The line doesn't fit the hunk's remaining counts: treat the hunk
			// as truncated and reclassify the line from the outside state.
		}
		inHunk = false

		if !strings.HasPrefix(line, "@@") && !strings.HasPrefix(line, "##") {
			out = append(out, diffLine{kind: diffMeta, text: line})
			continue
		}
		if m := hunkHeaderRE.FindStringSubmatch(line); m != nil {
			inHunk = true
			numbered = m[1] == "@@"
			oldNo, oldLeft = atoi(m[2]), hunkCount(m[3])
			newNo, newLeft = atoi(m[4]), hunkCount(m[5])
			out = append(out, diffLine{kind: diffHunk, text: line})
			continue
		}
		out = append(out, diffLine{kind: diffMeta, text: line})
	}
	return out
}

func hunkCount(s string) int {
	if s == "" {
		return 1
	}
	return atoi(s)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// ---- document -------------------------------------------------------------

// diffDoc is a diff prepared once, when it arrives, into the two flat row
// lists the renderer walks. Building it is width-independent; per frame the
// renderer only formats the rows that fit on screen, so frame cost depends on
// the pane height, not on the size of the diff.
type diffDoc struct {
	lines   []diffLine // unified order, for narrow panes
	rows    []diffRow  // side-by-side order: removed/added runs paired up
	hasHunk bool
}

// diffRow is one side-by-side row: either a pair of cells or a full-width line
// (hunk header, file header, note).
type diffRow struct {
	pair        bool
	left, right diffCell
	line        diffLine
}

type diffCell struct {
	no   int
	text string
	sign rune // ' ', '-', '+', or 0 for an empty cell
}

func newDiffDoc(raw []string) *diffDoc {
	lines := parseDiff(raw)
	d := &diffDoc{lines: lines, rows: make([]diffRow, 0, len(lines))}

	// The pending removed and added runs are index ranges into lines. A run is
	// always contiguous: a removed line arriving after added ones closes the
	// current block first, so "-a +b -c +d" pairs as (a,b) then (c,d).
	delStart, delEnd, addStart, addEnd := 0, 0, 0, 0
	flush := func() {
		nDel, nAdd := delEnd-delStart, addEnd-addStart
		for i := 0; i < max(nDel, nAdd); i++ {
			r := diffRow{pair: true}
			if i < nDel {
				l := lines[delStart+i]
				r.left = diffCell{no: l.oldNo, text: l.text, sign: '-'}
			}
			if i < nAdd {
				l := lines[addStart+i]
				r.right = diffCell{no: l.newNo, text: l.text, sign: '+'}
			}
			d.rows = append(d.rows, r)
		}
		delStart, delEnd, addStart, addEnd = 0, 0, 0, 0
	}

	for i, l := range lines {
		switch l.kind {
		case diffDel:
			if addEnd != addStart {
				flush()
			}
			if delEnd == delStart {
				delStart = i
			}
			delEnd = i + 1
		case diffAdd:
			if addEnd == addStart {
				addStart = i
			}
			addEnd = i + 1
		case diffContext:
			flush()
			d.rows = append(d.rows, diffRow{pair: true,
				left:  diffCell{no: l.oldNo, text: l.text, sign: ' '},
				right: diffCell{no: l.newNo, text: l.text, sign: ' '}})
		default:
			flush()
			d.hasHunk = d.hasHunk || l.kind == diffHunk
			d.rows = append(d.rows, diffRow{line: l})
		}
	}
	flush()
	return d
}

// ---- rendering ------------------------------------------------------------

const diffSeparator = " | "

// renderDiff draws at most maxRows lines of d, side by side when there is
// room, else unified.
func renderDiff(d *diffDoc, width, maxRows int) []string {
	if maxRows <= 0 {
		return nil
	}
	if d == nil || len(d.lines) == 0 {
		return []string{diffContextStyle.Render("(no changes)")}
	}
	leftW := (width - len(diffSeparator)) / 2
	rightW := width - len(diffSeparator) - leftW
	if !d.hasHunk || leftW < 18 || rightW < 18 {
		return renderUnified(d.lines, width, maxRows)
	}

	out := make([]string, 0, maxRows)
	out = append(out,
		diffHeadStyle.Render(formatDiffRow(leftW, rightW, diffCell{text: "OLD"}, diffCell{text: "NEW"}, false)),
		diffSepStyle.Render(truncate(strings.Repeat("-", leftW)+diffSeparator+strings.Repeat("-", rightW), width)),
	)
	for _, r := range d.rows {
		if len(out) >= maxRows {
			break
		}
		switch {
		case r.pair:
			out = append(out, formatDiffRow(leftW, rightW, r.left, r.right, true))
		case r.line.kind == diffHunk:
			out = append(out, diffHunkStyle.Render(truncate("  "+r.line.text, width)))
		default:
			out = append(out, styleDiffLine(r.line, truncate(expandTabs(r.line.text), width)))
		}
	}
	return out[:min(len(out), maxRows)]
}

func renderUnified(lines []diffLine, width, maxRows int) []string {
	out := make([]string, 0, min(len(lines), maxRows))
	for _, l := range lines[:min(len(lines), maxRows)] {
		text := l.text
		switch l.kind {
		case diffContext:
			text = " " + text
		case diffDel:
			text = "-" + text
		case diffAdd:
			text = "+" + text
		}
		out = append(out, styleDiffLine(l, truncate(expandTabs(text), width)))
	}
	return out
}

func styleDiffLine(l diffLine, s string) string {
	switch l.kind {
	case diffHunk:
		return diffHunkStyle.Render(s)
	case diffAdd:
		return diffAddStyle.Render(s)
	case diffDel:
		return diffDelStyle.Render(s)
	case diffMeta:
		if isDiffHeader(l.text) {
			return diffMetaStyle.Render(s)
		}
	}
	return diffContextStyle.Render(s)
}

var diffHeaderPrefixes = []string{"Index:", "===", "---", "+++", "Property changes on:", "___", "Added:", "Modified:", "Deleted:"}

func isDiffHeader(line string) bool {
	for _, p := range diffHeaderPrefixes {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

func formatDiffRow(leftW, rightW int, left, right diffCell, styled bool) string {
	l := formatDiffCell(leftW, left)
	r := formatDiffCell(rightW, right)
	if !styled {
		return l + diffSeparator + r
	}
	return styleDiffCell(l, left.sign) + diffSepStyle.Render(diffSeparator) + styleDiffCell(r, right.sign)
}

func formatDiffCell(width int, c diffCell) string {
	label := ""
	if c.no > 0 {
		label = strconv.Itoa(c.no)
	}
	sign := c.sign
	if sign == 0 {
		sign = ' '
	}
	return padRight(truncate(fmt.Sprintf("%5s %c %s", label, sign, expandTabs(c.text)), width), width)
}

func styleDiffCell(cell string, sign rune) string {
	switch sign {
	case '+':
		return diffAddStyle.Render(cell)
	case '-':
		return diffDelStyle.Render(cell)
	default:
		return diffContextStyle.Render(cell)
	}
}

func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	return strings.ReplaceAll(s, "\t", "    ")
}
