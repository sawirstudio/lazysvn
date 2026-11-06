package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// svnResult is the outcome of one svn invocation. Success is decided by err
// alone, never by inspecting the output text.
type svnResult struct {
	args   []string
	stdout string
	stderr string
	err    error
}

func (r svnResult) commandLine() string {
	return "$ svn " + strings.Join(r.args, " ")
}

// transcript renders the command, its output and any error for the preview pane.
func (r svnResult) transcript() string {
	var b strings.Builder
	b.WriteString(r.commandLine())
	b.WriteString("\n\n")
	out := strings.TrimSpace(strings.Join(nonEmpty(r.stdout, r.stderr), "\n"))
	if out != "" {
		b.WriteString(out)
		b.WriteString("\n")
	}
	if r.err != nil {
		b.WriteString("\nerror: ")
		b.WriteString(r.err.Error())
		b.WriteString("\n")
	}
	if out == "" && r.err == nil {
		b.WriteString("(no output)\n")
	}
	return strings.TrimSpace(b.String())
}

func nonEmpty(parts ...string) []string {
	out := parts[:0:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimRight(p, "\n"))
		}
	}
	return out
}

// svnRunner is swapped out in tests.
var svnRunner = func(args ...string) svnResult {
	// --non-interactive: there is no terminal for svn to prompt on while the
	// TUI owns the screen, so auth or conflict prompts must fail fast instead.
	cmd := exec.Command("svn", append([]string{"--non-interactive"}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return svnResult{args: args, stdout: stdout.String(), stderr: stderr.String(), err: err}
}

// withPaths appends paths after "--" so a file named like "-x" is never read
// as an option.
func withPaths(args []string, paths []string) []string {
	out := append([]string(nil), args...)
	out = append(out, "--")
	return append(out, paths...)
}

// ---- messages -------------------------------------------------------------

// statusMsg answers a status request; seq lets the model drop stale answers.
type statusMsg struct {
	seq     int
	entries []statusEntry
	result  svnResult
	err     error
}

// previewMsg answers a diff or log request.
type previewMsg struct {
	seq    int
	kind   previewKind
	path   string
	result svnResult
}

// actionMsg reports the end of a working-copy-changing operation.
type actionMsg struct {
	op     opKind
	paths  []string
	note   string
	result svnResult
}

func fetchStatusCmd(seq int) tea.Cmd {
	return func() tea.Msg {
		res := svnRunner("status", "--xml")
		if res.err != nil {
			return statusMsg{seq: seq, result: res, err: res.err}
		}
		entries, err := parseStatusXML([]byte(res.stdout))
		if err != nil {
			return statusMsg{seq: seq, result: res, err: fmt.Errorf("parse svn status --xml: %w", err)}
		}
		return statusMsg{seq: seq, entries: entries, result: res}
	}
}

func diffCmd(seq int, path string) tea.Cmd {
	return func() tea.Msg {
		return previewMsg{seq: seq, kind: previewDiff, path: path, result: svnRunner(withPaths([]string{"diff"}, []string{path})...)}
	}
}

func logCmd(seq int, path string) tea.Cmd {
	return func() tea.Msg {
		return previewMsg{seq: seq, kind: previewLog, path: path, result: svnRunner(withPaths([]string{"log", "-l", "20"}, []string{path})...)}
	}
}

func actionCmd(op opKind, paths []string, message, note string) tea.Cmd {
	paths = append([]string(nil), paths...)
	var args []string
	switch op {
	case opUpdate:
		args = []string{"update"}
	case opAdd:
		args = withPaths([]string{"add", "--parents"}, paths)
	case opRevert:
		args = withPaths([]string{"revert"}, paths)
	case opCommit:
		args = withPaths([]string{"commit", "-m", message}, paths)
	}
	return func() tea.Msg {
		return actionMsg{op: op, paths: paths, note: note, result: svnRunner(args...)}
	}
}

// ---- status parsing -------------------------------------------------------

type statusEntry struct {
	Code       rune // primary status letter, as in the first svn status column
	PropCode   rune // property status letter, as in the second column (' ' if none)
	Path       string
	Changelist string
}

// Unversioned reports whether svn add applies to this entry.
func (e statusEntry) Unversioned() bool { return e.Code == '?' }

var errBadStatusXML = errors.New("malformed svn status XML")

// parseStatusXML reads `svn status --xml` in one pass over the bytes.
//
// The XML form is used because changelist headers, external-item banners and
// tree-conflict details are structure there rather than text that could be
// mistaken for a file. The parser is a small tag scanner rather than
// encoding/xml: it never materialises the <commit>, <author>, <date> and
// <lock> elements svn emits for every entry, and allocates only each entry's
// path (and each changelist name once) into a slice sized up front.
func parseStatusXML(data []byte) ([]statusEntry, error) {
	entries := make([]statusEntry, 0, bytes.Count(data, []byte("<entry")))
	var (
		path, changelist string
		sawStatus        bool
	)

	for i := 0; ; {
		lt := bytes.IndexByte(data[i:], '<')
		if lt < 0 {
			break
		}
		i += lt + 1
		if i >= len(data) {
			return nil, errBadStatusXML
		}

		// Declarations, comments, CDATA, DOCTYPE: skip whole.
		if c := data[i]; c == '?' || c == '!' {
			end := []byte(">")
			switch {
			case c == '?':
				end = []byte("?>")
			case bytes.HasPrefix(data[i:], []byte("!--")):
				end = []byte("-->")
			case bytes.HasPrefix(data[i:], []byte("![CDATA[")):
				end = []byte("]]>")
			}
			n := bytes.Index(data[i:], end)
			if n < 0 {
				return nil, errBadStatusXML
			}
			i += n + len(end)
			continue
		}

		closing := data[i] == '/'
		if closing {
			i++
		}
		name, n := scanXMLName(data[i:])
		if n == 0 {
			return nil, errBadStatusXML
		}
		i += n

		if closing {
			switch string(name) {
			case "changelist":
				changelist = ""
			case "entry":
				path = ""
			}
			gt := bytes.IndexByte(data[i:], '>')
			if gt < 0 {
				return nil, errBadStatusXML
			}
			i += gt + 1
			continue
		}

		elem := elemOf(name)
		var item, props, treeConflicted []byte
		for {
			i = skipXMLSpace(data, i)
			if i >= len(data) {
				return nil, errBadStatusXML
			}
			if data[i] == '/' {
				i++
				continue
			}
			if data[i] == '>' {
				i++
				break
			}
			attr, n := scanXMLName(data[i:])
			if n == 0 {
				return nil, errBadStatusXML
			}
			i = skipXMLSpace(data, i+n)
			if i >= len(data) || data[i] != '=' {
				return nil, errBadStatusXML
			}
			i = skipXMLSpace(data, i+1)
			if i >= len(data) || (data[i] != '"' && data[i] != '\'') {
				return nil, errBadStatusXML
			}
			q := data[i]
			end := bytes.IndexByte(data[i+1:], q)
			if end < 0 {
				return nil, errBadStatusXML
			}
			val := data[i+1 : i+1+end]
			i += end + 2

			switch {
			case elem == elemEntry && string(attr) == "path":
				path = xmlUnescape(val)
			case elem == elemChangelist && string(attr) == "name":
				changelist = xmlUnescape(val)
			case elem == elemWCStatus && string(attr) == "item":
				item = val
			case elem == elemWCStatus && string(attr) == "props":
				props = val
			case elem == elemWCStatus && string(attr) == "tree-conflicted":
				treeConflicted = val
			}
		}

		switch elem {
		case elemStatus:
			sawStatus = true
		case elemWCStatus:
			if e, ok := makeStatusEntry(path, changelist, item, props, treeConflicted); ok {
				entries = append(entries, e)
			}
		}
	}

	if !sawStatus {
		return nil, errBadStatusXML
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

type xmlElem int

const (
	elemOther xmlElem = iota
	elemStatus
	elemChangelist
	elemEntry
	elemWCStatus
)

func elemOf(name []byte) xmlElem {
	switch string(name) {
	case "status":
		return elemStatus
	case "changelist":
		return elemChangelist
	case "entry":
		return elemEntry
	case "wc-status":
		return elemWCStatus
	}
	return elemOther
}

func makeStatusEntry(path, changelist string, item, props, treeConflicted []byte) (statusEntry, bool) {
	if path == "" || string(item) == "external" { // externals are mount points, not changes
		return statusEntry{}, false
	}
	code := itemCode(item)
	prop := ' '
	switch string(props) {
	case "modified":
		prop = 'M'
	case "conflicted":
		prop = 'C'
	}
	if string(treeConflicted) == "true" {
		code = 'C'
	}
	if code == ' ' {
		code = prop // property-only change: show it in the main column
	}
	if code == ' ' {
		return statusEntry{}, false
	}
	return statusEntry{Code: code, PropCode: prop, Path: path, Changelist: changelist}, true
}

func itemCode(item []byte) rune {
	switch string(item) {
	case "added":
		return 'A'
	case "conflicted":
		return 'C'
	case "deleted":
		return 'D'
	case "ignored":
		return 'I'
	case "incomplete", "missing":
		return '!'
	case "merged":
		return 'G'
	case "modified":
		return 'M'
	case "obstructed":
		return '~'
	case "replaced":
		return 'R'
	case "unversioned":
		return '?'
	}
	return ' '
}

func scanXMLName(b []byte) ([]byte, int) {
	n := 0
	for n < len(b) {
		c := b[n]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '>' || c == '/' || c == '=' {
			break
		}
		n++
	}
	return b[:n], n
}

func skipXMLSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// xmlUnescape decodes the predefined entities and numeric character
// references; anything unrecognised is kept literally.
func xmlUnescape(b []byte) string {
	amp := bytes.IndexByte(b, '&')
	if amp < 0 {
		return string(b)
	}
	var sb strings.Builder
	sb.Grow(len(b))
	for amp >= 0 {
		sb.Write(b[:amp])
		b = b[amp:]
		semi := bytes.IndexByte(b, ';')
		if semi < 0 {
			break
		}
		ent := string(b[1:semi])
		switch ent {
		case "amp":
			sb.WriteByte('&')
		case "lt":
			sb.WriteByte('<')
		case "gt":
			sb.WriteByte('>')
		case "quot":
			sb.WriteByte('"')
		case "apos":
			sb.WriteByte('\'')
		default:
			r, ok := numericCharRef(ent)
			if !ok {
				sb.Write(b[:semi+1])
				b = b[semi+1:]
				amp = bytes.IndexByte(b, '&')
				continue
			}
			sb.WriteRune(r)
		}
		b = b[semi+1:]
		amp = bytes.IndexByte(b, '&')
	}
	sb.Write(b)
	return sb.String()
}

func numericCharRef(ent string) (rune, bool) {
	if len(ent) < 2 || ent[0] != '#' {
		return 0, false
	}
	base, digits := 10, ent[1:]
	if digits[0] == 'x' {
		base, digits = 16, digits[1:]
	}
	n, err := strconv.ParseUint(digits, base, 32)
	if err != nil || !utf8.ValidRune(rune(n)) {
		return 0, false
	}
	return rune(n), true
}
