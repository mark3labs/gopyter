package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// vimMode is the vim sub-mode of edit mode (only used with Options.Vim).
type vimMode int

const (
	vimNormal vimMode = iota
	vimInsert
	vimVisual
	vimVisualLine
)

func (v vimMode) String() string {
	switch v {
	case vimInsert:
		return "INSERT"
	case vimVisual:
		return "VISUAL"
	case vimVisualLine:
		return "V-LINE"
	}
	return "NORMAL"
}

// vimState holds the vim key state. Commands are parsed as
// [count] operator [count] motion, or [count] command.
type vimState struct {
	enabled bool
	mode    vimMode

	count   int    // count being typed (0 = none)
	opCount int    // count typed before the pending operator (0 = none)
	op      string // pending operator: "d", "c" or "y"
	prefix  string // pending multi-key prefix, e.g. "g"

	// reg is the unnamed register.
	reg struct {
		text     string
		linewise bool
	}
}

func (v *vimState) reset() { v.count, v.opCount, v.op, v.prefix = 0, 0, "", "" }

// pending renders the partially typed command for the footer.
func (v *vimState) pending() string {
	var b strings.Builder
	if v.opCount > 0 {
		b.WriteString(itoa(v.opCount))
	}
	b.WriteString(v.op)
	if v.count > 0 {
		b.WriteString(itoa(v.count))
	}
	b.WriteString(v.prefix)
	return b.String()
}

// vimMotion is the result of a motion: where the cursor goes and how an
// operator treats the covered text.
type vimMotion struct {
	to        Pos
	linewise  bool
	inclusive bool
}

// vimActive reports whether keys go to the vim normal/visual handler.
func (m *Model) vimActive() bool {
	return m.vim.enabled && m.mode == modeEdit && m.vim.mode != vimInsert
}

// vimInsertMode reports whether the editor behaves as a plain text editor:
// always without vim, and in vim's insert mode.
func (m *Model) vimInsertMode() bool {
	return !m.vim.enabled || m.vim.mode == vimInsert
}

// vimSetMode switches the vim sub-mode, updating the selection to match.
func (m *Model) vimSetMode(md vimMode) {
	ed := m.cur().ed
	m.vim.reset()
	switch md {
	case vimNormal, vimInsert:
		ed.ClearSelection()
	case vimVisual, vimVisualLine:
		if m.vim.mode != vimVisual && m.vim.mode != vimVisualLine {
			// Anchor a fresh selection at the cursor.
			ed.ClearSelection()
		}
		sm := selInclusive
		if md == vimVisualLine {
			sm = selLines
		}
		ed.SetSelectionMode(sm)
	}
	if m.vim.mode == vimInsert && md != vimInsert {
		ed.breakUndo()
	}
	m.vim.mode = md
	if md != vimInsert {
		ed.clampNormal()
	}
}

// vimEscapeInsert leaves insert mode like vim: the cursor steps back onto
// the last inserted character.
func (m *Model) vimEscapeInsert() {
	m.closeCompletion()
	ed := m.cur().ed
	row, col := ed.Cursor()
	m.vimSetMode(vimNormal)
	ed.SetCursor(row, max(col-1, 0))
}

// vimSync reconciles the vim mode with selections made by other means (the
// mouse, select all): a selection in normal mode becomes a visual one, and
// a visual mode whose selection went away drops back to normal.
func (m *Model) vimSync() {
	if !m.vim.enabled || m.mode != modeEdit || m.vim.mode == vimInsert {
		return
	}
	ed := m.cur().ed
	if ed.selecting && ed.selMode != selExclusive {
		return // our own visual selection
	}
	if a, b, ok := ed.Selection(); ok {
		// Exclusive [a, b) becomes inclusive [a, b-1].
		ed.anchor = a
		p := ed.prev(b)
		ed.row, ed.col, ed.goal = p.Row, p.Col, -1
		ed.selMode = selInclusive
		m.vim.reset()
		m.vim.mode = vimVisual
		return
	}
	if m.vim.mode != vimNormal {
		m.vim.reset()
		m.vim.mode = vimNormal
	}
	ed.clampNormal()
}

// handleVimKey handles a key in vim normal or visual mode.
func (m *Model) handleVimKey(msg tea.KeyPressMsg) tea.Cmd {
	ed := m.cur().ed
	before := ed.version
	m.vimSync()
	if m.vim.mode == vimNormal {
		// Drop an empty selection left by a mouse click, so v anchors at
		// the cursor.
		ed.ClearSelection()
	}
	cmd := m.vimKey(msg)
	if ed.version != before {
		m.dirty = true
	}
	if m.mode == modeEdit && m.vim.mode != vimInsert {
		m.cur().ed.clampNormal()
	}
	return cmd
}

func isDigitKey(ks string) bool { return len(ks) == 1 && ks[0] >= '0' && ks[0] <= '9' }

func (m *Model) vimKey(msg tea.KeyPressMsg) tea.Cmd {
	v := &m.vim
	ed := m.cur().ed
	ks := msg.String()
	visual := v.mode == vimVisual || v.mode == vimVisualLine

	if ks == "esc" {
		switch {
		case v.pending() != "":
			v.reset()
		case visual:
			m.vimSetMode(vimNormal)
		default:
			m.leaveEdit()
		}
		return nil
	}

	if v.prefix == "g" {
		v.prefix = ""
		if ks != "g" {
			v.reset()
			return nil
		}
		ks = "gg"
	} else if ks == "g" {
		v.prefix = "g"
		return nil
	}

	// Counts: a leading 0 is the motion to column 0.
	if isDigitKey(ks) && (ks != "0" || v.count > 0) {
		v.count = min(v.count*10+int(ks[0]-'0'), 99999)
		return nil
	}
	hasCount := v.count > 0 || v.opCount > 0
	count := max(v.count, 1) * max(v.opCount, 1)

	// Operator pending: the key is the doubled operator or a motion.
	if v.op != "" {
		op := v.op
		v.reset()
		row, _ := ed.Cursor()
		if ks == op {
			return m.vimLines(op, row, min(row+count-1, ed.LineCount()-1))
		}
		if mo, ok := ed.vimMotion(ks, count, hasCount, op); ok {
			return m.vimApply(op, mo)
		}
		return nil
	}
	v.count = 0

	if visual {
		return m.vimVisualKey(ks, count, hasCount)
	}

	row, col := ed.Cursor()
	switch ks {
	case "d", "c", "y":
		v.op, v.opCount = ks, count
		if !hasCount {
			v.opCount = 0
		}
	case "i", "insert":
		m.vimSetMode(vimInsert)
	case "a":
		m.vimSetMode(vimInsert)
		ed.SetCursor(row, col+1)
	case "I":
		m.vimSetMode(vimInsert)
		ed.SetCursor(row, ed.firstNonBlank(row))
	case "A":
		m.vimSetMode(vimInsert)
		ed.SetCursor(row, len(ed.lines[row]))
	case "o", "O":
		m.vimSetMode(vimInsert)
		ed.openLine(ks == "O")
	case "x", "delete":
		return m.vimOpMotion("d", "l", count)
	case "X":
		return m.vimOpMotion("d", "h", count)
	case "D":
		return m.vimOpMotion("d", "$", count)
	case "C":
		return m.vimOpMotion("c", "$", count)
	case "s":
		return m.vimOpMotion("c", "l", count)
	case "S":
		return m.vimLines("c", row, min(row+count-1, ed.LineCount()-1))
	case "Y":
		return m.vimLines("y", row, min(row+count-1, ed.LineCount()-1))
	case "p", "P":
		m.vimPaste(ks == "p", count)
	case "J":
		tok := ed.begin()
		for range max(count-1, 1) {
			if !ed.joinLines() {
				break
			}
		}
		ed.end(tok)
	case "u":
		for range count {
			ed.Undo()
		}
	case "ctrl+r":
		for range count {
			ed.Redo()
		}
	case "v":
		m.vimSetMode(vimVisual)
	case "V":
		m.vimSetMode(vimVisualLine)
	case "ctrl+d":
		m.moveCursor("pgdown", false)
	case "ctrl+u":
		m.moveCursor("pgup", false)
	case "ctrl+z", "ctrl+y", "ctrl+shift+z", "ctrl+x", "ctrl+t", "alt+a", "ctrl+shift+a":
		// Non-vim editor shortcuts that still make sense here.
		cmd := m.handleEditKey(msg)
		m.vimSync()
		return cmd
	default:
		m.vimMove(ks, count, hasCount, true)
	}
	return nil
}

// vimVisualKey handles a key in visual mode.
func (m *Model) vimVisualKey(ks string, count int, hasCount bool) tea.Cmd {
	ed := m.cur().ed
	switch ks {
	case "d", "x", "delete":
		return m.vimVisualOp("d")
	case "y":
		return m.vimVisualOp("y")
	case "c", "s":
		return m.vimVisualOp("c")
	case "v", "V":
		want := vimVisual
		if ks == "V" {
			want = vimVisualLine
		}
		if m.vim.mode == want {
			m.vimSetMode(vimNormal)
		} else {
			m.vimSetMode(want)
		}
	case "o":
		// Jump to the other end of the selection.
		a := ed.anchor
		ed.anchor = Pos{ed.row, ed.col}
		ed.SetCursor(a.Row, a.Col)
	default:
		m.vimMove(ks, count, hasCount, false)
	}
	return nil
}

// vimMove applies a motion to the cursor. In normal mode j/k continue into
// the neighbouring cells.
func (m *Model) vimMove(ks string, count int, hasCount, crossCells bool) {
	switch ks {
	case "j", "down", "ctrl+n", "enter":
		for range count {
			m.moveCursor("down", crossCells)
		}
		return
	case "k", "up", "ctrl+p":
		for range count {
			m.moveCursor("up", crossCells)
		}
		return
	}
	ed := m.cur().ed
	if mo, ok := ed.vimMotion(ks, count, hasCount, ""); ok {
		ed.SetCursor(mo.to.Row, mo.to.Col)
	}
}

// vimOpMotion runs an operator with a motion given by key, as used by the
// shorthands x, X, D, C and s.
func (m *Model) vimOpMotion(op, motion string, count int) tea.Cmd {
	mo, ok := m.cur().ed.vimMotion(motion, count, false, op)
	if !ok {
		return nil
	}
	return m.vimApply(op, mo)
}

// vimApply runs an operator over the text between the cursor and a motion.
func (m *Model) vimApply(op string, mo vimMotion) tea.Cmd {
	ed := m.cur().ed
	row, col := ed.Cursor()
	a, b := Pos{row, col}, mo.to
	if b.before(a) {
		a, b = b, a
	}
	if mo.linewise {
		return m.vimLines(op, a.Row, b.Row)
	}
	if mo.inclusive && b.Col < len(ed.lines[b.Row]) {
		b.Col++
	}
	return m.vimRange(op, a, b)
}

// vimVisualOp runs an operator over the visual selection.
func (m *Model) vimVisualOp(op string) tea.Cmd {
	ed := m.cur().ed
	a, b, ok := ed.Selection()
	linewise := m.vim.mode == vimVisualLine
	m.vimSetMode(vimNormal)
	if !ok {
		return nil
	}
	if linewise {
		last := b.Row
		if b.Col == 0 && b.Row > a.Row {
			last--
		}
		return m.vimLines(op, a.Row, last)
	}
	return m.vimRange(op, a, b)
}

// vimRange yanks, deletes or changes the characters in [a, b).
func (m *Model) vimRange(op string, a, b Pos) tea.Cmd {
	ed := m.cur().ed
	text := ed.textRange(a, b)
	m.vim.reg.text, m.vim.reg.linewise = text, false
	switch op {
	case "y":
		ed.SetCursor(a.Row, a.Col)
		return m.vimYanked(text, strings.Count(text, "\n")+1, false)
	case "d", "c":
		if a != b {
			tok := ed.begin()
			ed.deleteRange(a, b)
			ed.end(tok)
		} else {
			ed.SetCursor(a.Row, a.Col)
		}
		if op == "c" {
			m.vimSetMode(vimInsert)
		}
	}
	return nil
}

// vimLines yanks, deletes or changes rows r1..r2.
func (m *Model) vimLines(op string, r1, r2 int) tea.Cmd {
	ed := m.cur().ed
	text := ed.linesText(r1, r2)
	m.vim.reg.text, m.vim.reg.linewise = text, true
	switch op {
	case "y":
		if row, col := ed.Cursor(); r1 < row {
			ed.SetCursor(r1, col)
		}
		return m.vimYanked(text, r2-r1+1, true)
	case "d":
		tok := ed.begin()
		ed.deleteLines(r1, r2)
		ed.end(tok)
	case "c":
		// Keep the first line's indentation and start typing after it.
		tok := ed.begin()
		indent := string(leadingWS(ed.lines[r1]))
		if r2 > r1 {
			ed.deleteLines(r1+1, r2)
		}
		ed.lines[r1] = []rune(indent)
		ed.changed()
		ed.SetCursor(r1, len(ed.lines[r1]))
		ed.end(tok)
		m.vimSetMode(vimInsert)
	}
	return nil
}

// vimYanked publishes a yank to the system clipboard and the status line.
func (m *Model) vimYanked(text string, lines int, linewise bool) tea.Cmd {
	m.textClip = text
	msg := "yanked " + itoa(len([]rune(text))) + " characters"
	if linewise || lines > 1 {
		msg = "yanked " + itoa(lines) + " line" + strings.Repeat("s", min(lines-1, 1))
	}
	return tea.Batch(tea.SetClipboard(text), m.setStatus(statusSuccess, "%s", msg))
}

// vimPaste puts the unnamed register after (p) or before (P) the cursor.
func (m *Model) vimPaste(after bool, count int) {
	ed := m.cur().ed
	reg := m.vim.reg
	if reg.text == "" && !reg.linewise {
		return
	}
	row, col := ed.Cursor()
	tok := ed.begin()
	defer ed.end(tok)
	if reg.linewise {
		text := strings.Repeat(reg.text+"\n", count)
		at := row
		if after {
			at++
		}
		ed.insertLines(at, strings.TrimSuffix(text, "\n"))
		return
	}
	if after && len(ed.lines[row]) > 0 {
		ed.SetCursor(row, col+1)
	}
	ed.InsertText(strings.Repeat(reg.text, count))
	// The cursor rests on the last pasted character.
	if r, c := ed.Cursor(); c > 0 {
		ed.SetCursor(r, c-1)
	}
}

// vimMotion computes a motion from the cursor. op is the pending operator
// ("" for a plain cursor movement), which a few motions special-case like
// vim does. It reports false for unknown keys and impossible motions.
func (e *Editor) vimMotion(ks string, count int, hasCount bool, op string) (vimMotion, bool) {
	p := Pos{e.row, e.col}
	line := e.lines[e.row]
	last := len(e.lines) - 1
	switch ks {
	case "h", "left", "backspace", "ctrl+h":
		if p.Col == 0 {
			return vimMotion{}, false
		}
		return vimMotion{to: Pos{p.Row, max(p.Col-count, 0)}}, true
	case "l", "right", "space":
		if p.Col >= len(line) || (op == "" && p.Col >= len(line)-1) {
			return vimMotion{}, false
		}
		return vimMotion{to: Pos{p.Row, min(p.Col+count, len(line))}}, true
	case "j", "down", "ctrl+n", "enter":
		if p.Row+count > last {
			return vimMotion{}, false
		}
		return vimMotion{to: Pos{p.Row + count, p.Col}, linewise: true}, true
	case "k", "up", "ctrl+p":
		if p.Row-count < 0 {
			return vimMotion{}, false
		}
		return vimMotion{to: Pos{p.Row - count, p.Col}, linewise: true}, true
	case "0", "home":
		return vimMotion{to: Pos{p.Row, 0}}, true
	case "^":
		return vimMotion{to: Pos{p.Row, e.firstNonBlank(p.Row)}}, true
	case "$", "end":
		r := min(p.Row+count-1, last)
		return vimMotion{to: Pos{r, max(len(e.lines[r])-1, 0)}, inclusive: true}, true
	case "gg", "G":
		r := last
		if ks == "gg" {
			r = 0
		}
		if hasCount {
			r = clamp(count-1, 0, last)
		}
		return vimMotion{to: Pos{r, e.firstNonBlank(r)}, linewise: true}, true
	case "w", "W":
		big := ks == "W"
		if op == "c" && e.class(p, big) != 0 {
			// cw changes to the end of the word, like ce.
			q := e.wordEndHere(p, big)
			for range count - 1 {
				q = e.wordEndForward(q, big)
			}
			return vimMotion{to: q, inclusive: true}, true
		}
		q := p
		for i := range count {
			from := q
			q = e.wordStartForward(q, big)
			if op != "" && i == count-1 && q.Row > from.Row {
				// An operator stops at the end of the line of the last
				// word rather than eating the line break.
				q = Pos{from.Row, len(e.lines[from.Row])}
			}
		}
		return vimMotion{to: q}, true
	case "b", "B":
		q := p
		for range count {
			q = e.wordStartBackward(q, ks == "B")
		}
		return vimMotion{to: q}, true
	case "e", "E":
		q := p
		for range count {
			q = e.wordEndForward(q, ks == "E")
		}
		return vimMotion{to: q, inclusive: true}, true
	}
	return vimMotion{}, false
}
