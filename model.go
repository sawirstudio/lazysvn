package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// mode is which input handler owns the keyboard. Exactly one is active.
//
//	normal ──c──▶ commit  ──enter (non-empty msg)──▶ normal (+ commit op)
//	              commit  ──esc──────────────────────▶ normal
//	normal ──v──▶ confirmRevert ──y──────────────────▶ normal (+ revert op)
//	              confirmRevert ──n / esc / q─────────▶ normal
//
// ctrl+c quits from every mode.
type mode int

const (
	modeNormal mode = iota
	modeCommit
	modeConfirmRevert
)

// opKind is a working-copy-changing svn operation. At most one runs at a time
// (model.running); while one runs, other svn commands are refused so they
// cannot race it for the working-copy lock or show pre-operation state.
type opKind int

const (
	opNone opKind = iota
	opUpdate
	opAdd
	opRevert
	opCommit
)

func (o opKind) String() string {
	switch o {
	case opUpdate:
		return "update"
	case opAdd:
		return "add"
	case opRevert:
		return "revert"
	case opCommit:
		return "commit"
	}
	return "none"
}

func (o opKind) title() string {
	s := o.String()
	return strings.ToUpper(s[:1]) + s[1:]
}

// previewKind says what the preview pane is showing. It replaces the old
// convention of checking whether the title string started with "Diff:".
type previewKind int

const (
	previewHelp previewKind = iota
	previewDiff
	previewLog
	previewAction
	previewError
)

// preview is prepared once when its content arrives (text split into lines,
// diffs parsed and paired) so that drawing a frame only touches what fits on
// screen.
type preview struct {
	kind  previewKind
	title string
	path  string   // file a diff/log belongs to
	lines []string // plain text, used when diff is nil
	diff  *diffDoc // prepared diff for a successful previewDiff
}

func (p preview) withText(s string) preview {
	p.lines = splitLines(s)
	return p
}

type noticeLevel int

const (
	noticeInfo noticeLevel = iota
	noticeError
)

// notice is the latest outcome message. It stays until the next key press, so
// results are not overwritten by background refreshes.
type notice struct {
	text  string
	level noticeLevel
}

type model struct {
	entries []statusEntry
	cursor  int
	marked  map[string]struct{}

	mode         mode
	pendingPaths []string // targets captured when entering commit/confirmRevert
	pendingMiss  []string // commit: pendingPaths that are missing ('!')
	pendingNote  string
	commitInput  textinput.Model

	preview preview
	// previewSeq identifies the newest preview request. Replies carrying an
	// older seq are dropped, so fast cursor movement can't leave the pane
	// showing a diff for a file the cursor has already left.
	previewSeq     int
	previewLoading previewKind // kind being loaded; meaningful only if loadingPreview
	loadingPreview bool
	followDiff     bool // cursor moves re-diff the file under the cursor

	statusSeq     int
	statusLoading bool
	statusErr     error // last refresh failed; entries are stale

	running opKind

	notice notice

	width  int
	height int
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Commit message"
	ti.Prompt = "> "
	ti.CharLimit = 200

	return model{
		marked:        map[string]struct{}{},
		preview:       preview{kind: previewHelp, title: "Help"}.withText(helpText()),
		statusSeq:     1,
		statusLoading: true,
		commitInput:   ti,
	}
}

func (m model) Init() tea.Cmd {
	return fetchStatusCmd(m.statusSeq)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case statusMsg:
		return m.onStatus(msg), nil
	case previewMsg:
		return m.onPreview(msg), nil
	case actionMsg:
		return m.onAction(msg)
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.mode {
		case modeCommit:
			return m.updateCommit(msg)
		case modeConfirmRevert:
			return m.updateConfirmRevert(msg)
		default:
			return m.updateNormal(msg)
		}
	}
	if m.mode == modeCommit { // e.g. cursor blink
		var cmd tea.Cmd
		m.commitInput, cmd = m.commitInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ---- async replies --------------------------------------------------------

func (m model) onStatus(msg statusMsg) model {
	if msg.seq != m.statusSeq {
		return m // superseded by a newer refresh or by an operation
	}
	m.statusLoading = false
	if msg.err != nil {
		m.statusErr = msg.err
		body := msg.result.transcript()
		if msg.result.err == nil {
			body = msg.err.Error()
		}
		m.showPreview(preview{kind: previewError, title: "SVN Error"}.withText(body))
		m.setNotice(noticeError, "svn status failed; the list may be stale.")
		return m
	}

	m.statusErr = nil
	prev := m.currentPath()
	m.entries = msg.entries
	m.pruneMarks()
	m.cursor = m.indexOf(prev, m.cursor)
	return m
}

func (m model) onPreview(msg previewMsg) model {
	if msg.seq != m.previewSeq {
		return m
	}
	m.loadingPreview = false
	p := preview{kind: msg.kind, path: msg.path}
	switch msg.kind {
	case previewDiff:
		p.title = "Diff: " + msg.path
	case previewLog:
		p.title = "Log: " + msg.path
	}
	if msg.result.err != nil {
		p = p.withText(msg.result.transcript())
		m.setNotice(noticeError, fmt.Sprintf("svn %s failed.", msg.result.args[0]))
	} else if msg.kind == previewDiff {
		p.diff = newDiffDoc(splitLines(msg.result.stdout))
	} else if strings.TrimSpace(msg.result.stdout) == "" {
		p = p.withText("(no output)")
	} else {
		p = p.withText(msg.result.stdout)
	}
	m.preview = p
	return m
}

func (m model) onAction(msg actionMsg) (model, tea.Cmd) {
	m.running = opNone
	ok := msg.result.err == nil
	title := msg.op.title()
	if !ok {
		title += " failed"
	}
	m.showPreview(preview{kind: previewAction, title: title}.withText(msg.transcript()))
	if ok {
		m.unmark(msg.paths)
		m.setNotice(noticeInfo, strings.TrimSpace(fmt.Sprintf("svn %s finished. %s", msg.op, msg.note)))
	} else {
		m.setNotice(noticeError, fmt.Sprintf("svn %s failed; see output.", msg.op))
	}
	return m, m.refresh()
}

// ---- key handling ---------------------------------------------------------

func (m model) updateNormal(msg tea.KeyMsg) (model, tea.Cmd) {
	m.notice = notice{}

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
		return m, m.followCursor()
	case "down", "j":
		m.moveCursor(1)
		return m, m.followCursor()
	case "g", "home":
		m.cursor = 0
		return m, m.followCursor()
	case "G", "end":
		m.cursor = max(0, len(m.entries)-1)
		return m, m.followCursor()
	case " ":
		m.toggleMark()
		return m, nil
	case "esc":
		if n := len(m.marked); n > 0 {
			m.marked = map[string]struct{}{}
			m.setNotice(noticeInfo, fmt.Sprintf("Cleared %d mark(s).", n))
		}
		return m, nil
	case "h", "?":
		m.followDiff = false
		m.showPreview(preview{kind: previewHelp, title: "Help"}.withText(helpText()))
		return m, nil
	}

	// Everything below runs svn, so it waits for a running operation.
	if m.running != opNone {
		switch msg.String() {
		case "r", "enter", "d", "l", "u", "a", "v", "c":
			m.setNotice(noticeError, fmt.Sprintf("svn %s is still running.", m.running))
		}
		return m, nil
	}

	switch msg.String() {
	case "r":
		return m, m.refresh()
	case "enter", "d":
		if m.currentPath() == "" {
			m.setNotice(noticeError, "No file selected.")
			return m, nil
		}
		m.followDiff = true
		return m, m.requestDiff()
	case "l":
		path := m.currentPath()
		if path == "" {
			m.setNotice(noticeError, "No file selected.")
			return m, nil
		}
		m.followDiff = false
		return m, m.requestPreview(previewLog, path, logCmd)
	case "u":
		return m, m.startOp(opRequest{op: opUpdate})
	case "a":
		targets, skipped := m.addTargets()
		if len(targets) == 0 {
			m.setNotice(noticeError, "Select unversioned file(s) with status '?'.")
			return m, nil
		}
		note := ""
		if skipped > 0 {
			note = fmt.Sprintf("Skipped %d selected file(s) that are already versioned.", skipped)
		}
		return m, m.startOp(opRequest{op: opAdd, paths: targets, note: note})
	case "v":
		sel := m.selectedTargets()
		if len(sel) == 0 {
			m.setNotice(noticeError, "No file selected.")
			return m, nil
		}
		m.mode = modeConfirmRevert
		m.pendingPaths = m.expandTargets(sel, false)
		return m, nil
	case "c":
		targets, missing, skipped := m.commitTargets()
		if len(targets) == 0 {
			if skipped > 0 {
				m.setNotice(noticeError, "Unversioned files can't be committed; press a to add them first.")
			} else {
				m.setNotice(noticeError, "Nothing to commit.")
			}
			return m, nil
		}
		m.mode = modeCommit
		m.pendingPaths = targets
		m.pendingMiss = missing
		m.pendingNote = ""
		if skipped > 0 {
			m.pendingNote = fmt.Sprintf("Skipped %d unversioned file(s); add them first to commit them.", skipped)
		}
		m.commitInput.SetValue("")
		return m, m.commitInput.Focus()
	}
	return m, nil
}

func (m model) updateCommit(msg tea.KeyMsg) (model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.leaveMode()
		m.setNotice(noticeInfo, "Commit canceled.")
		return m, nil
	case "enter":
		message := strings.TrimSpace(m.commitInput.Value())
		if message == "" {
			m.setNotice(noticeError, "Commit message cannot be empty.")
			return m, nil
		}
		req := opRequest{op: opCommit, paths: m.pendingPaths, missing: m.pendingMiss, message: message, note: m.pendingNote}
		m.leaveMode()
		return m, m.startOp(req)
	}
	m.notice = notice{}
	var cmd tea.Cmd
	m.commitInput, cmd = m.commitInput.Update(msg)
	return m, cmd
}

func (m model) updateConfirmRevert(msg tea.KeyMsg) (model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		req := opRequest{op: opRevert, paths: m.pendingPaths}
		m.leaveMode()
		return m, m.startOp(req)
	case "n", "N", "esc", "q":
		m.leaveMode()
		m.setNotice(noticeInfo, "Revert canceled.")
	}
	return m, nil
}

// ---- transitions ----------------------------------------------------------

func (m *model) leaveMode() {
	m.mode = modeNormal
	m.pendingPaths, m.pendingMiss, m.pendingNote = nil, nil, ""
	m.commitInput.Blur()
}

func (m *model) setNotice(level noticeLevel, text string) {
	m.notice = notice{text: text, level: level}
}

// showPreview replaces the pane immediately and invalidates in-flight requests.
func (m *model) showPreview(p preview) {
	m.previewSeq++
	m.loadingPreview = false
	m.preview = p
}

func (m *model) requestPreview(kind previewKind, path string, cmd func(int, string) tea.Cmd) tea.Cmd {
	m.previewSeq++
	m.loadingPreview = true
	m.previewLoading = kind
	return cmd(m.previewSeq, path)
}

func (m *model) requestDiff() tea.Cmd {
	e, ok := m.currentEntry()
	if !ok {
		return nil
	}
	if e.Unversioned() {
		m.showPreview(preview{kind: previewDiff, title: "Diff: " + e.Path, path: e.Path}.
			withText("Unversioned: not under version control yet, so there is nothing to diff.\nPress a to add it."))
		return nil
	}
	return m.requestPreview(previewDiff, e.Path, diffCmd)
}

func (m *model) followCursor() tea.Cmd {
	if !m.followDiff || m.running != opNone {
		return nil
	}
	if e, ok := m.currentEntry(); ok && (m.preview.path != e.Path || m.loadingPreview) {
		return m.requestDiff()
	}
	return nil
}

func (m *model) refresh() tea.Cmd {
	m.statusSeq++
	m.statusLoading = true
	return fetchStatusCmd(m.statusSeq)
}

// startOp begins a working-copy-changing operation. note is appended to the
// success notice.
func (m *model) startOp(req opRequest) tea.Cmd {
	op, paths, note := req.op, req.paths, req.note
	m.running = op
	// Any status reply still in flight predates the operation.
	m.statusSeq++
	m.statusLoading = false
	m.followDiff = false

	body := fmt.Sprintf("Running svn %s...", op)
	if len(paths) > 0 {
		body += "\n\n" + bulletList(paths)
	}
	if note != "" {
		body += "\n\n" + note
	}
	m.showPreview(preview{kind: previewAction, title: op.title()}.withText(body))
	return actionCmd(req)
}

// ---- selection ------------------------------------------------------------

func (m *model) moveCursor(delta int) {
	m.cursor = clamp(m.cursor+delta, 0, len(m.entries)-1)
}

func (m *model) toggleMark() {
	path := m.currentPath()
	if path == "" {
		return
	}
	if _, ok := m.marked[path]; ok {
		delete(m.marked, path)
		return
	}
	m.marked[path] = struct{}{}
}

func (m *model) unmark(paths []string) {
	for _, p := range paths {
		delete(m.marked, p)
	}
}

func (m *model) pruneMarks() {
	active := make(map[string]struct{}, len(m.entries))
	for _, e := range m.entries {
		active[e.Path] = struct{}{}
	}
	for p := range m.marked {
		if _, ok := active[p]; !ok {
			delete(m.marked, p)
		}
	}
}

func (m model) currentEntry() (statusEntry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return statusEntry{}, false
	}
	return m.entries[m.cursor], true
}

func (m model) currentPath() string {
	e, _ := m.currentEntry()
	return e.Path
}

// indexOf finds path in entries, falling back to the clamped old index.
func (m model) indexOf(path string, fallback int) int {
	for i, e := range m.entries {
		if e.Path == path {
			return i
		}
	}
	return clamp(fallback, 0, len(m.entries)-1)
}

// selectedTargets is the marked paths if any, else the file under the cursor.
func (m model) selectedTargets() []string {
	if len(m.marked) > 0 {
		targets := make([]string, 0, len(m.marked))
		for p := range m.marked {
			targets = append(targets, p)
		}
		sort.Strings(targets)
		return targets
	}
	if p := m.currentPath(); p != "" {
		return []string{p}
	}
	return nil
}

// addTargets is selectedTargets filtered to unversioned entries, plus how many
// selected paths were left out.
func (m model) addTargets() (targets []string, skipped int) {
	byPath := m.entriesByPath()
	for _, p := range m.selectedTargets() {
		if byPath[p].Unversioned() {
			targets = append(targets, p)
		} else {
			skipped++
		}
	}
	return targets, skipped
}

// commitTargets is selectedTargets made committable: unversioned paths are
// left out (counted in skipped) and expandTargets adds what svn needs or will
// touch anyway. missing lists the '!' targets, which must be deleted first.
func (m model) commitTargets() (targets, missing []string, skipped int) {
	byPath := m.entriesByPath()
	var sel []string
	for _, p := range m.selectedTargets() {
		if byPath[p].Unversioned() {
			skipped++
			continue
		}
		sel = append(sel, p)
	}
	if len(sel) == 0 {
		return nil, nil, skipped
	}
	targets = m.expandTargets(sel, true)
	for _, p := range targets {
		if byPath[p].Code == '!' {
			missing = append(missing, p)
		}
	}
	return targets, missing, skipped
}

// expandTargets returns sel plus every status entry the svn operation will
// act on or requires, so the confirmation lists exactly what changes:
//
//   - changed entries below a selected directory: commit and revert both
//     recurse, and showing only the directory would hide them;
//   - the other side of a move: svn refuses to commit one side alone, and
//     reverting one side leaves the other as a plain add or delete;
//   - for commit, added or replaced ancestors: svn refuses to commit a path
//     whose parent is not yet in the repository unless the parent is
//     committed too (which in turn pulls in the parent's other changes).
//
// Unversioned entries are never added; svn skips them. The result is sorted.
func (m model) expandTargets(sel []string, forCommit bool) []string {
	byPath := m.entriesByPath()
	in := make(map[string]struct{}, len(sel))
	queue := make([]string, 0, len(sel))
	add := func(p string) {
		if _, ok := in[p]; !ok {
			in[p] = struct{}{}
			queue = append(queue, p)
		}
	}
	for _, p := range sel {
		add(p)
	}
	for len(queue) > 0 {
		// Drain moves and ancestors for every newly added path, then sweep
		// for descendants of everything so far; repeat until nothing is new.
		for len(queue) > 0 {
			p := queue[0]
			queue = queue[1:]
			e := byPath[p]
			for _, q := range [2]string{e.MovedFrom, e.MovedTo} {
				if pe, ok := byPath[q]; ok && q != "" && !pe.Unversioned() {
					add(q)
				}
			}
			if forCommit {
				for a := parentPath(p); a != ""; a = parentPath(a) {
					if ae := byPath[a]; ae.Code == 'A' || ae.Code == 'R' {
						add(a)
					}
				}
			}
		}
		_, all := in["."]
		for _, e := range m.entries {
			if _, ok := in[e.Path]; ok || e.Unversioned() {
				continue
			}
			if all && e.Path != "." {
				add(e.Path)
				continue
			}
			for a := parentPath(e.Path); a != ""; a = parentPath(a) {
				if _, ok := in[a]; ok {
					add(e.Path)
					break
				}
			}
		}
	}
	out := make([]string, 0, len(in))
	for p := range in {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (m model) entriesByPath() map[string]statusEntry {
	byPath := make(map[string]statusEntry, len(m.entries))
	for _, e := range m.entries {
		byPath[e.Path] = e
	}
	return byPath
}

// parentPath strips the last element of a status path, which uses "/" or, on
// Windows, "\". A top-level path has no parent ("").
func parentPath(p string) string {
	i := strings.LastIndexAny(p, `/\`)
	if i <= 0 {
		return ""
	}
	return p[:i]
}

// ---- helpers --------------------------------------------------------------

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return min(max(v, lo), hi)
}

func splitLines(s string) []string {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func bulletList(paths []string) string {
	var b strings.Builder
	for i, p := range paths {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("  - ")
		b.WriteString(p)
	}
	return b.String()
}
