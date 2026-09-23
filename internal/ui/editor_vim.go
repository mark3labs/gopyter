package ui

import (
	"strings"
	"unicode"
)

// selMode controls how the selection bounds are interpreted.
type selMode int

const (
	// selExclusive is the regular editor selection: [anchor, cursor).
	selExclusive selMode = iota
	// selInclusive is vim's charwise visual mode: both ends are included.
	selInclusive
	// selLines is vim's linewise visual mode: every touched line is included.
	selLines
)

// SetSelectionMode starts a selection at the cursor (if none is active) and
// sets how its bounds are interpreted.
func (e *Editor) SetSelectionMode(m selMode) {
	e.StartSelection()
	e.selMode = m
}

// begin starts a compound edit that undoes as one step. It returns a token
// for end.
func (e *Editor) begin() int {
	e.breakUndo()
	e.push("txn")
	e.txn++
	return e.version
}

// end finishes a compound edit started by begin. A transaction that changed
// nothing leaves no undo step behind.
func (e *Editor) end(version int) {
	e.txn--
	if e.txn > 0 {
		return
	}
	if e.version == version && len(e.undo) > 0 {
		e.undo = e.undo[:len(e.undo)-1]
	}
	e.breakUndo()
}

// clampNormal keeps the cursor on a character, as vim's normal mode does:
// it can't rest past the end of a non-empty line.
func (e *Editor) clampNormal() {
	if n := len(e.lines[e.row]); e.col >= n {
		e.col = max(n-1, 0)
	}
}

// firstNonBlank returns the column of the first non-blank rune in row.
func (e *Editor) firstNonBlank(row int) int {
	return len(leadingWS(e.lines[row]))
}

// textRange returns the text in [a, b).
func (e *Editor) textRange(a, b Pos) string {
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

// linesText returns rows r1..r2 joined by newlines.
func (e *Editor) linesText(r1, r2 int) string {
	parts := make([]string, 0, r2-r1+1)
	for r := r1; r <= r2; r++ {
		parts = append(parts, string(e.lines[r]))
	}
	return strings.Join(parts, "\n")
}

// deleteLines removes rows r1..r2 and puts the cursor on the first
// non-blank of the line that took their place.
func (e *Editor) deleteLines(r1, r2 int) {
	e.push("delete-lines")
	e.lines = append(e.lines[:r1:r1], e.lines[r2+1:]...)
	if len(e.lines) == 0 {
		e.lines = [][]rune{{}}
	}
	e.selecting, e.selMode = false, selExclusive
	e.row = min(r1, len(e.lines)-1)
	e.changed()
	e.col = e.firstNonBlank(e.row)
}

// insertLines inserts text as whole lines before row at (at == row) and
// moves the cursor to the first non-blank of the first inserted line.
func (e *Editor) insertLines(at int, text string) {
	e.push("insert-lines")
	var add [][]rune
	for l := range strings.SplitSeq(text, "\n") {
		add = append(add, []rune(l))
	}
	e.lines = append(e.lines[:at], append(add, e.lines[at:]...)...)
	e.row = at
	e.changed()
	e.col = e.firstNonBlank(at)
}

// openLine inserts an empty line above (or below) the cursor, indented like
// the current line (one level deeper below a line that opens a block), and
// puts the cursor at its end.
func (e *Editor) openLine(above bool) {
	e.push("open")
	e.breakUndo()
	cur := e.lines[e.row]
	indent := append([]rune(nil), leadingWS(cur)...)
	at := e.row
	if !above {
		at++
		trimmed := strings.TrimRight(string(cur), " \t")
		if trimmed != "" && strings.ContainsRune("{([", rune(trimmed[len(trimmed)-1])) {
			indent = append(indent, '\t')
		}
	}
	e.lines = append(e.lines[:at], append([][]rune{indent}, e.lines[at:]...)...)
	e.row, e.col = at, len(indent)
	e.changed()
}

// joinLines joins the cursor line with the next one, vim style: leading
// whitespace of the next line collapses to one space.
func (e *Editor) joinLines() bool {
	if e.row >= len(e.lines)-1 {
		return false
	}
	e.push("join")
	cur := []rune(strings.TrimRight(string(e.lines[e.row]), " \t"))
	next := []rune(strings.TrimLeft(string(e.lines[e.row+1]), " \t"))
	col := len(cur)
	if len(cur) > 0 && len(next) > 0 && next[0] != ')' {
		cur = append(cur, ' ')
	}
	e.lines[e.row] = append(cur, next...)
	e.lines = append(e.lines[:e.row+1], e.lines[e.row+2:]...)
	e.changed()
	e.col = col
	return true
}

// Word motions treat the text as one stream in which the line break is a
// blank. Classes: 0 blank, 1 word characters, 2 other punctuation (with
// big words, everything non-blank is class 1).

func (e *Editor) runeAt(p Pos) rune {
	if l := e.lines[p.Row]; p.Col < len(l) {
		return l[p.Col]
	}
	return '\n'
}

func (e *Editor) class(p Pos, big bool) int {
	r := e.runeAt(p)
	switch {
	case unicode.IsSpace(r):
		return 0
	case big || isWord(r):
		return 1
	}
	return 2
}

func (e *Editor) atEnd(p Pos) bool {
	return p.Row == len(e.lines)-1 && p.Col >= len(e.lines[p.Row])
}

func (e *Editor) next(p Pos) Pos {
	if p.Col < len(e.lines[p.Row]) {
		return Pos{p.Row, p.Col + 1}
	}
	if p.Row < len(e.lines)-1 {
		return Pos{p.Row + 1, 0}
	}
	return p
}

func (e *Editor) prev(p Pos) Pos {
	if p.Col > 0 {
		return Pos{p.Row, p.Col - 1}
	}
	if p.Row > 0 {
		return Pos{p.Row - 1, len(e.lines[p.Row-1])}
	}
	return p
}

func (e *Editor) emptyLine(p Pos) bool { return p.Col == 0 && len(e.lines[p.Row]) == 0 }

// wordStartForward implements w/W: the start of the next word, stopping at
// empty lines.
func (e *Editor) wordStartForward(p Pos, big bool) Pos {
	if c := e.class(p, big); c != 0 {
		for !e.atEnd(p) && e.class(p, big) == c && p.Col < len(e.lines[p.Row]) {
			p = e.next(p)
		}
	}
	for !e.atEnd(p) && e.class(p, big) == 0 {
		p = e.next(p)
		if e.emptyLine(p) {
			break
		}
	}
	return p
}

// wordStartBackward implements b/B.
func (e *Editor) wordStartBackward(p Pos, big bool) Pos {
	if p.Row == 0 && p.Col == 0 {
		return p
	}
	p = e.prev(p)
	for e.class(p, big) == 0 && !e.emptyLine(p) && (p.Row > 0 || p.Col > 0) {
		p = e.prev(p)
	}
	if c := e.class(p, big); c != 0 {
		for p.Col > 0 && e.class(Pos{p.Row, p.Col - 1}, big) == c {
			p.Col--
		}
	}
	return p
}

// wordEndForward implements e/E: the last character of the next word end.
func (e *Editor) wordEndForward(p Pos, big bool) Pos {
	if e.atEnd(p) {
		return p
	}
	p = e.next(p)
	for !e.atEnd(p) && e.class(p, big) == 0 {
		p = e.next(p)
	}
	c := e.class(p, big)
	for p.Col+1 < len(e.lines[p.Row]) && e.class(Pos{p.Row, p.Col + 1}, big) == c {
		p.Col++
	}
	return p
}

// wordEndHere returns the last character of the word under p, or p itself
// when it is at the end of a word (used by cw, which changes to the end of
// the current word).
func (e *Editor) wordEndHere(p Pos, big bool) Pos {
	c := e.class(p, big)
	for p.Col+1 < len(e.lines[p.Row]) && e.class(Pos{p.Row, p.Col + 1}, big) == c {
		p.Col++
	}
	return p
}
