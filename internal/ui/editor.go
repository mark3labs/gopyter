package ui

import (
	"go/scanner"
	"go/token"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/mattn/go-runewidth"
)

const tabWidth = 4

// Pos is a logical position in the editor.
type Pos struct{ Row, Col int }

func (p Pos) before(o Pos) bool {
	return p.Row < o.Row || (p.Row == o.Row && p.Col < o.Col)
}

type snapshot struct {
	text     string
	row, col int
}

// Editor is a small multi-line code editor with syntax highlighting.
type Editor struct {
	lines    [][]rune
	row, col int
	goal     int
	lang     string
	version  int

	undo, redo []snapshot
	lastOp     string

	// Selection: anchor is the fixed end, the cursor is the moving end.
	anchor    Pos
	selecting bool

	tokVersion int
	tokens     [][]chroma.TokenType
}

// NewEditor creates an editor for the given chroma language.
func NewEditor(lang, text string) *Editor {
	e := &Editor{lang: lang, goal: -1, tokVersion: -1}
	e.setText(text)
	return e
}

func (e *Editor) setText(text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	e.lines = nil
	for l := range strings.SplitSeq(text, "\n") {
		e.lines = append(e.lines, []rune(l))
	}
	e.selecting = false
	e.row = min(e.row, len(e.lines)-1)
	e.col = min(e.col, len(e.lines[e.row]))
	e.version++
}

// SetValue replaces the content and records an undo step.
func (e *Editor) SetValue(s string) {
	e.push("set")
	e.setText(s)
}

// Value returns the editor content.
func (e *Editor) Value() string {
	var b strings.Builder
	for i, l := range e.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(l))
	}
	return b.String()
}

// SetLang changes the highlighting language.
func (e *Editor) SetLang(lang string) { e.lang = lang; e.version++ }

// Cursor returns the cursor's logical position.
func (e *Editor) Cursor() (int, int) { return e.row, e.col }

// LineCount returns the number of logical lines.
func (e *Editor) LineCount() int { return len(e.lines) }

// SetCursor moves the cursor, clamping to the content.
func (e *Editor) SetCursor(row, col int) {
	e.row = clamp(row, 0, len(e.lines)-1)
	e.col = clamp(col, 0, len(e.lines[e.row]))
	e.goal = -1
}

func (e *Editor) CursorStart() { e.SetCursor(0, 0) }
func (e *Editor) CursorEnd() {
	e.SetCursor(len(e.lines)-1, len(e.lines[len(e.lines)-1]))
}

// push records an undo snapshot. Consecutive operations of the same kind
// are coalesced.
func (e *Editor) push(op string) {
	if op == e.lastOp && op != "set" && len(e.undo) > 0 {
		return
	}
	e.lastOp = op
	e.undo = append(e.undo, snapshot{e.Value(), e.row, e.col})
	if len(e.undo) > 500 {
		e.undo = e.undo[1:]
	}
	e.redo = nil
}

func (e *Editor) breakUndo() { e.lastOp = "" }

func (e *Editor) Undo() {
	if len(e.undo) == 0 {
		return
	}
	s := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.redo = append(e.redo, snapshot{e.Value(), e.row, e.col})
	e.setText(s.text)
	e.SetCursor(s.row, s.col)
	e.lastOp = ""
}

func (e *Editor) Redo() {
	if len(e.redo) == 0 {
		return
	}
	s := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.undo = append(e.undo, snapshot{e.Value(), e.row, e.col})
	e.setText(s.text)
	e.SetCursor(s.row, s.col)
	e.lastOp = ""
}

func (e *Editor) changed() { e.version++; e.goal = -1 }

// InsertText inserts arbitrary (possibly multi-line) text at the cursor.
func (e *Editor) InsertText(s string) {
	if s == "" {
		return
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	op := "insert"
	if strings.ContainsRune(s, '\n') {
		op = "paste"
	} else if strings.ContainsFunc(s, unicode.IsSpace) {
		op = "space"
	}
	if e.replaceSelection() {
		e.lastOp = op // the replacement snapshot covers this insertion
	} else {
		e.push(op)
	}
	if op == "paste" {
		e.breakUndo()
	}
	parts := strings.Split(s, "\n")
	line := e.lines[e.row]
	tail := append([]rune(nil), line[e.col:]...)
	first := append(append([]rune(nil), line[:e.col]...), []rune(parts[0])...)
	if len(parts) == 1 {
		e.lines[e.row] = append(first, tail...)
		e.col += len([]rune(parts[0]))
		e.changed()
		return
	}
	newLines := [][]rune{first}
	for _, p := range parts[1:] {
		newLines = append(newLines, []rune(p))
	}
	last := len(newLines) - 1
	e.col = len(newLines[last])
	newLines[last] = append(newLines[last], tail...)
	e.lines = append(e.lines[:e.row], append(newLines, e.lines[e.row+1:]...)...)
	e.row += last
	e.changed()
}

// InsertRune inserts a typed rune, with a few conveniences for code.
func (e *Editor) InsertRune(r rune) {
	line := e.lines[e.row]
	// Dedent a closing brace typed on a whitespace-only line.
	if !e.HasSelection() && (r == '}' || r == ')' || r == ']') && strings.TrimSpace(string(line[:e.col])) == "" && e.col > 0 {
		e.push("insert")
		indent := line[:e.col]
		if indent[len(indent)-1] == '\t' {
			indent = indent[:len(indent)-1]
		} else {
			n := 0
			for n < tabWidth && n < len(indent) && indent[len(indent)-1-n] == ' ' {
				n++
			}
			indent = indent[:len(indent)-n]
		}
		e.lines[e.row] = append(append(append([]rune(nil), indent...), r), line[e.col:]...)
		e.col = len(indent) + 1
		e.changed()
		return
	}
	e.InsertText(string(r))
}

func leadingWS(l []rune) []rune {
	i := 0
	for i < len(l) && (l[i] == ' ' || l[i] == '\t') {
		i++
	}
	return l[:i]
}

// Newline splits the line at the cursor with automatic indentation.
func (e *Editor) Newline() {
	if !e.replaceSelection() {
		e.push("newline")
	}
	e.breakUndo()
	line := e.lines[e.row]
	indent := append([]rune(nil), leadingWS(line)...)
	before := append([]rune(nil), line[:e.col]...)
	after := append([]rune(nil), line[e.col:]...)
	trimmed := strings.TrimRight(string(before), " \t")
	open := len(trimmed) > 0 && strings.ContainsRune("{([", rune(trimmed[len(trimmed)-1]))
	inner := indent
	if open {
		inner = append(append([]rune(nil), indent...), '\t')
	}
	afterTrim := strings.TrimLeft(string(after), " \t")
	if open && len(afterTrim) > 0 && strings.ContainsRune("})]", rune(afterTrim[0])) {
		e.lines = append(e.lines[:e.row], append([][]rune{before, inner, append(append([]rune(nil), indent...), []rune(afterTrim)...)}, e.lines[e.row+1:]...)...)
		e.row++
		e.col = len(inner)
		e.changed()
		return
	}
	next := append(append([]rune(nil), inner...), []rune(afterTrim)...)
	e.lines = append(e.lines[:e.row], append([][]rune{before, next}, e.lines[e.row+1:]...)...)
	e.row++
	e.col = len(inner)
	e.changed()
}

func (e *Editor) Backspace() {
	if e.DeleteSelection() {
		return
	}
	if e.col == 0 && e.row == 0 {
		return
	}
	e.push("delete")
	if e.col > 0 {
		l := e.lines[e.row]
		e.lines[e.row] = append(l[:e.col-1:e.col-1], l[e.col:]...)
		e.col--
	} else {
		prev := e.lines[e.row-1]
		e.col = len(prev)
		e.lines[e.row-1] = append(prev[:len(prev):len(prev)], e.lines[e.row]...)
		e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
		e.row--
	}
	e.changed()
}

func (e *Editor) Delete() {
	if e.DeleteSelection() {
		return
	}
	l := e.lines[e.row]
	if e.col == len(l) && e.row == len(e.lines)-1 {
		return
	}
	e.push("delete-fwd")
	if e.col < len(l) {
		e.lines[e.row] = append(l[:e.col:e.col], l[e.col+1:]...)
	} else {
		e.lines[e.row] = append(l[:len(l):len(l)], e.lines[e.row+1]...)
		e.lines = append(e.lines[:e.row+1], e.lines[e.row+2:]...)
	}
	e.changed()
}

func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

func (e *Editor) wordLeftCol() int {
	l, c := e.lines[e.row], e.col
	for c > 0 && !isWord(l[c-1]) {
		c--
	}
	for c > 0 && isWord(l[c-1]) {
		c--
	}
	return c
}

func (e *Editor) wordRightCol() int {
	l, c := e.lines[e.row], e.col
	for c < len(l) && !isWord(l[c]) {
		c++
	}
	for c < len(l) && isWord(l[c]) {
		c++
	}
	return c
}

func (e *Editor) DeleteWordBackward() {
	if e.DeleteSelection() {
		return
	}
	if e.col == 0 {
		e.Backspace()
		return
	}
	e.push("delete-word")
	e.breakUndo()
	c := e.wordLeftCol()
	l := e.lines[e.row]
	e.lines[e.row] = append(l[:c:c], l[e.col:]...)
	e.col = c
	e.changed()
}

func (e *Editor) DeleteWordForward() {
	if e.DeleteSelection() {
		return
	}
	l := e.lines[e.row]
	if e.col == len(l) {
		e.Delete()
		return
	}
	e.push("delete-word")
	e.breakUndo()
	c := e.wordRightCol()
	e.lines[e.row] = append(l[:e.col:e.col], l[c:]...)
	e.changed()
}

func (e *Editor) KillToEnd() {
	if e.DeleteSelection() {
		return
	}
	l := e.lines[e.row]
	if e.col == len(l) {
		e.Delete()
		return
	}
	e.push("kill")
	e.breakUndo()
	e.lines[e.row] = l[:e.col:e.col]
	e.changed()
}

func (e *Editor) KillToStart() {
	if e.DeleteSelection() {
		return
	}
	if e.col == 0 {
		return
	}
	e.push("kill")
	e.breakUndo()
	e.lines[e.row] = append([]rune(nil), e.lines[e.row][e.col:]...)
	e.col = 0
	e.changed()
}

// Dedent removes one indentation level from the current line.
func (e *Editor) Dedent() {
	l := e.lines[e.row]
	n := 0
	if len(l) > 0 && l[0] == '\t' {
		n = 1
	} else {
		for n < tabWidth && n < len(l) && l[n] == ' ' {
			n++
		}
	}
	if n == 0 {
		return
	}
	e.push("indent")
	e.lines[e.row] = append([]rune(nil), l[n:]...)
	e.col = max(0, e.col-n)
	e.changed()
}

// WordBeforeCursor returns the identifier characters immediately before
// the cursor, and the rune just before that word (0 if none).
func (e *Editor) WordBeforeCursor() (string, rune) {
	l := e.lines[e.row]
	start := e.col
	for start > 0 && isWord(l[start-1]) {
		start--
	}
	var prev rune
	if start > 0 {
		prev = l[start-1]
	}
	return string(l[start:e.col]), prev
}

// ReplaceBeforeCursor replaces the n runes before the cursor with text,
// as a single undo step.
func (e *Editor) ReplaceBeforeCursor(n int, text string) {
	e.ClearSelection()
	n = min(n, e.col)
	e.push("complete")
	e.breakUndo()
	l := e.lines[e.row]
	e.lines[e.row] = append(append(append([]rune(nil), l[:e.col-n]...), []rune(text)...), l[e.col:]...)
	e.col += len([]rune(text)) - n
	e.changed()
}

// InCodeContext reports whether the cursor is in Go code, as opposed to a
// string, rune literal or comment (where completion is unwanted). It scans
// the text before the cursor with the Go scanner, which, unlike syntax
// highlighters, understands literals that aren't terminated yet.
func (e *Editor) InCodeContext() bool {
	var b strings.Builder
	for i := 0; i < e.row; i++ {
		b.WriteString(string(e.lines[i]))
		b.WriteByte('\n')
	}
	b.WriteString(string(e.lines[e.row][:e.col]))
	src := []byte(b.String())

	var s scanner.Scanner
	fset := token.NewFileSet()
	s.Init(fset.AddFile("", fset.Base(), len(src)), src, func(token.Position, string) {}, scanner.ScanComments)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return true
		}
		off := fset.Position(pos).Offset
		if off+len(lit) < len(src) {
			continue
		}
		// This token runs up to the cursor.
		switch tok {
		case token.COMMENT:
			return strings.HasPrefix(lit, "/*") && len(lit) >= 4 && strings.HasSuffix(lit, "*/")
		case token.STRING, token.CHAR:
			return literalClosed(lit)
		}
	}
}

// literalClosed reports whether a string or rune literal is terminated.
func literalClosed(lit string) bool {
	if len(lit) < 2 {
		return false
	}
	q := lit[0]
	if lit[len(lit)-1] != q {
		return false
	}
	if q == '`' {
		return true
	}
	// The closing quote must not be escaped.
	n := 0
	for i := len(lit) - 2; i > 0 && lit[i] == '\\'; i-- {
		n++
	}
	return n%2 == 0
}

// StartSelection anchors a selection at the cursor, if none is active.
func (e *Editor) StartSelection() {
	if !e.selecting {
		e.anchor = Pos{e.row, e.col}
		e.selecting = true
	}
}

// ClearSelection drops the selection.
func (e *Editor) ClearSelection() { e.selecting = false }

// HasSelection reports whether a non-empty selection is active.
func (e *Editor) HasSelection() bool {
	return e.selecting && (e.anchor.Row != e.row || e.anchor.Col != e.col)
}

// Selection returns the ordered selection bounds.
func (e *Editor) Selection() (start, end Pos, ok bool) {
	if !e.HasSelection() {
		return Pos{}, Pos{}, false
	}
	a, b := e.anchor, Pos{e.row, e.col}
	if b.before(a) {
		a, b = b, a
	}
	return a, b, true
}

// SelectRange selects from a (anchor) to b (cursor).
func (e *Editor) SelectRange(a, b Pos) {
	a.Row = clamp(a.Row, 0, len(e.lines)-1)
	a.Col = clamp(a.Col, 0, len(e.lines[a.Row]))
	e.anchor, e.selecting = a, true
	e.SetCursor(b.Row, b.Col)
}

// SelectAll selects the whole content.
func (e *Editor) SelectAll() {
	last := len(e.lines) - 1
	e.SelectRange(Pos{0, 0}, Pos{last, len(e.lines[last])})
}

// SelectWordAt selects the word (or single symbol) at the given position.
func (e *Editor) SelectWordAt(row, col int) {
	row = clamp(row, 0, len(e.lines)-1)
	l := e.lines[row]
	col = clamp(col, 0, len(l))
	if col == len(l) && col > 0 {
		col--
	}
	if len(l) == 0 {
		e.SelectRange(Pos{row, 0}, Pos{row, 0})
		return
	}
	start, end := col, col+1
	if isWord(l[col]) {
		for start > 0 && isWord(l[start-1]) {
			start--
		}
		for end < len(l) && isWord(l[end]) {
			end++
		}
	} else if l[col] == ' ' || l[col] == '\t' {
		for start > 0 && (l[start-1] == ' ' || l[start-1] == '\t') {
			start--
		}
		for end < len(l) && (l[end] == ' ' || l[end] == '\t') {
			end++
		}
	}
	e.SelectRange(Pos{row, start}, Pos{row, end})
}

// SelectLine selects a whole line, including its newline when possible.
func (e *Editor) SelectLine(row int) {
	row = clamp(row, 0, len(e.lines)-1)
	if row < len(e.lines)-1 {
		e.SelectRange(Pos{row, 0}, Pos{row + 1, 0})
	} else {
		e.SelectRange(Pos{row, 0}, Pos{row, len(e.lines[row])})
	}
}

// SelectedText returns the selected text.
func (e *Editor) SelectedText() string {
	a, b, ok := e.Selection()
	if !ok {
		return ""
	}
	if a.Row == b.Row {
		return string(e.lines[a.Row][a.Col:b.Col])
	}
	var sb strings.Builder
	sb.WriteString(string(e.lines[a.Row][a.Col:]))
	for r := a.Row + 1; r < b.Row; r++ {
		sb.WriteByte('\n')
		sb.WriteString(string(e.lines[r]))
	}
	sb.WriteByte('\n')
	sb.WriteString(string(e.lines[b.Row][:b.Col]))
	return sb.String()
}

func (e *Editor) deleteRange(a, b Pos) {
	merged := append(append([]rune(nil), e.lines[a.Row][:a.Col]...), e.lines[b.Row][b.Col:]...)
	rest := append([][]rune{merged}, e.lines[b.Row+1:]...)
	e.lines = append(e.lines[:a.Row], rest...)
	e.row, e.col = a.Row, a.Col
	e.selecting = false
	e.changed()
}

// DeleteSelection deletes the selected text, reporting whether there was one.
func (e *Editor) DeleteSelection() bool {
	a, b, ok := e.Selection()
	if !ok {
		e.selecting = false
		return false
	}
	e.push("cut")
	e.breakUndo()
	e.deleteRange(a, b)
	return true
}

// replaceSelection deletes the selection in preparation for an insertion,
// recording a single undo step for the whole replacement.
func (e *Editor) replaceSelection() bool {
	a, b, ok := e.Selection()
	if !ok {
		e.selecting = false
		return false
	}
	e.push("replace")
	e.deleteRange(a, b)
	return true
}

// SelectionSpansLines reports whether the selection covers several lines.
func (e *Editor) SelectionSpansLines() bool {
	a, b, ok := e.Selection()
	return ok && a.Row != b.Row
}

// IndentSelection indents (or dedents) every line touched by the selection.
func (e *Editor) IndentSelection(dedent bool) {
	a, b, ok := e.Selection()
	if !ok {
		return
	}
	last := b.Row
	if b.Col == 0 && b.Row > a.Row {
		last--
	}
	e.push("indent")
	e.breakUndo()
	for r := a.Row; r <= last; r++ {
		l := e.lines[r]
		if !dedent {
			if len(l) > 0 {
				e.lines[r] = append([]rune{'\t'}, l...)
			}
			continue
		}
		n := 0
		if len(l) > 0 && l[0] == '\t' {
			n = 1
		} else {
			for n < tabWidth && n < len(l) && l[n] == ' ' {
				n++
			}
		}
		e.lines[r] = append([]rune(nil), l[n:]...)
	}
	e.changed()
	e.SelectRange(Pos{a.Row, 0}, Pos{last, len(e.lines[last])})
}

func (e *Editor) inSelection(a, b Pos, row, col int) bool {
	p := Pos{row, col}
	return !p.before(a) && p.before(b)
}

func (e *Editor) Left() {
	e.breakUndo()
	if e.col > 0 {
		e.col--
	} else if e.row > 0 {
		e.row--
		e.col = len(e.lines[e.row])
	}
	e.goal = -1
}

func (e *Editor) Right() {
	e.breakUndo()
	if e.col < len(e.lines[e.row]) {
		e.col++
	} else if e.row < len(e.lines)-1 {
		e.row++
		e.col = 0
	}
	e.goal = -1
}

func (e *Editor) WordLeft() {
	e.breakUndo()
	if e.col == 0 {
		e.Left()
		return
	}
	e.col = e.wordLeftCol()
	e.goal = -1
}

func (e *Editor) WordRight() {
	e.breakUndo()
	if e.col == len(e.lines[e.row]) {
		e.Right()
		return
	}
	e.col = e.wordRightCol()
	e.goal = -1
}

// Up moves the cursor up; returns false if already on the first line.
func (e *Editor) Up() bool {
	e.breakUndo()
	if e.row == 0 {
		return false
	}
	e.moveVert(-1)
	return true
}

// Down moves the cursor down; returns false if already on the last line.
func (e *Editor) Down() bool {
	e.breakUndo()
	if e.row == len(e.lines)-1 {
		return false
	}
	e.moveVert(1)
	return true
}

func (e *Editor) moveVert(d int) {
	if e.goal < 0 {
		e.goal = displayCol(e.lines[e.row], e.col)
	}
	e.row += d
	e.col = colForDisplay(e.lines[e.row], e.goal)
}

func (e *Editor) Home() {
	e.breakUndo()
	ws := len(leadingWS(e.lines[e.row]))
	if e.col == ws {
		e.col = 0
	} else {
		e.col = ws
	}
	e.goal = -1
}

func (e *Editor) End() {
	e.breakUndo()
	e.col = len(e.lines[e.row])
	e.goal = -1
}

func runeWidth(r rune, x int) int {
	if r == '\t' {
		return tabWidth - x%tabWidth
	}
	return max(runewidth.RuneWidth(r), 1)
}

func displayCol(l []rune, col int) int {
	x := 0
	for i := 0; i < col && i < len(l); i++ {
		x += runeWidth(l[i], x)
	}
	return x
}

func colForDisplay(l []rune, target int) int {
	x := 0
	for i, r := range l {
		w := runeWidth(r, x)
		if x+w > target {
			return i
		}
		x += w
	}
	return len(l)
}

// visualLine maps a rendered row back to a logical position.
type visualLine struct {
	row, startCol int
}

// editorView is the rendered output of the editor.
type editorView struct {
	lines   []string
	vmap    []visualLine
	curX    int
	curY    int
	gutterW int
}

// Render renders the editor at the given width.
func (e *Editor) Render(width int, focused bool, hl *highlighter, t theme) editorView {
	if e.tokVersion != e.version {
		e.tokens = tokenize(e.lang, e.lines)
		e.tokVersion = e.version
	}
	digits := len(itoa(len(e.lines)))
	gutterW := digits + 2
	textW := max(width-gutterW, 4)

	var v editorView
	v.gutterW = gutterW
	selA, selB, hasSel := e.Selection()
	for row, l := range e.lines {
		var toks []chroma.TokenType
		if row < len(e.tokens) {
			toks = e.tokens[row]
		}
		numStyle := t.lineNo
		if focused && row == e.row {
			numStyle = t.lineNoActive
		}
		num := numStyle.Render(padLeft(itoa(row+1), digits)) + "  "
		cont := strings.Repeat(" ", gutterW)

		var b strings.Builder
		var seg strings.Builder
		var segTok chroma.TokenType = -1
		segSel := false
		flush := func() {
			if seg.Len() > 0 {
				st := hl.styleFor(segTok)
				if segSel {
					st = st.Background(colSelection)
				}
				b.WriteString(st.Render(seg.String()))
				seg.Reset()
			}
		}
		x, vx := 0, 0
		startCol := 0
		emit := func(first bool) {
			flush()
			prefix := cont
			if first {
				prefix = num
			}
			v.lines = append(v.lines, prefix+b.String())
			v.vmap = append(v.vmap, visualLine{row: row, startCol: startCol})
			b.Reset()
		}
		first := true
		for i, r := range l {
			w := runeWidth(r, x)
			if vx+w > textW {
				emit(first)
				first = false
				vx = 0
				startCol = i
			}
			if focused && row == e.row && i == e.col {
				v.curX, v.curY = gutterW+vx, len(v.lines)
			}
			tok := chroma.Text
			if i < len(toks) {
				tok = toks[i]
			}
			sel := hasSel && e.inSelection(selA, selB, row, i)
			if tok != segTok || sel != segSel {
				flush()
				segTok, segSel = tok, sel
			}
			if r == '\t' {
				seg.WriteString(strings.Repeat(" ", w))
			} else {
				seg.WriteRune(r)
			}
			x += w
			vx += w
		}
		// Show a selected newline as a highlighted cell at the end of the line.
		if hasSel && row >= selA.Row && row < selB.Row && vx < textW {
			flush()
			b.WriteString(lipgloss.NewStyle().Background(colSelection).Render(" "))
		}
		if focused && row == e.row && e.col >= len(l) {
			if vx >= textW {
				emit(first)
				first = false
				vx = 0
				startCol = len(l)
			}
			v.curX, v.curY = gutterW+vx, len(v.lines)
		}
		emit(first)
	}
	return v
}

// PositionAt converts a click inside the rendered editor into a cursor
// position.
func (e *Editor) PositionAt(v editorView, x, y int) (int, int) {
	if len(v.vmap) == 0 {
		return 0, 0
	}
	y = clamp(y, 0, len(v.vmap)-1)
	vl := v.vmap[y]
	l := e.lines[vl.row]
	target := max(x-v.gutterW, 0)
	base := displayCol(l, vl.startCol)
	col := colForDisplay(l, base+target)
	if y+1 < len(v.vmap) && v.vmap[y+1].row == vl.row {
		col = min(col, v.vmap[y+1].startCol)
	}
	return vl.row, col
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func padLeft(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return strings.Repeat(" ", n-w) + s
	}
	return s
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
