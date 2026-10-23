package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/notebook"
	"github.com/mark3labs/gopyter/internal/termimg"
)

func (m *Model) selRange() (int, int) {
	if m.anchor < 0 || m.mode == modeEdit {
		return m.sel, m.sel
	}
	a := clamp(m.anchor, 0, len(m.cells)-1)
	return min(a, m.sel), max(a, m.sel)
}
func (m *Model) inRange(i int) bool  { lo, hi := m.selRange(); return i >= lo && i <= hi }
func (m *Model) multiSelected() bool { lo, hi := m.selRange(); return hi > lo }
func (m *Model) collapse()           { m.anchor = -1 }
func (m *Model) extendTo(i int) {
	if m.anchor < 0 {
		m.anchor = m.sel
	}
	m.sel = clamp(i, 0, len(m.cells)-1)
	// Keep the anchor even when crossing it, so repeated extension reverses direction.
}
func (m *Model) focusCell(i int) {
	if i < 0 || i >= len(m.cells) {
		return
	}
	if !m.inRange(i) {
		m.sel = i
		m.collapse()
	}
}
func (m *Model) insertCells(i int, cs []*Cell) {
	m.cells = slices.Insert(m.cells, i, cs...)
	m.dirty = true
}
func (m *Model) moveBlock(lo, hi, d int) {
	if lo+d < 0 || hi+d >= len(m.cells) {
		return
	}
	if d < 0 {
		c := m.cells[lo-1]
		copy(m.cells[lo-1:hi], m.cells[lo:hi+1])
		m.cells[hi] = c
	} else {
		c := m.cells[hi+1]
		copy(m.cells[lo+1:hi+2], m.cells[lo:hi+1])
		m.cells[lo] = c
	}
	m.sel += d
	if m.anchor >= 0 {
		m.anchor += d
	}
	m.dirty = true
}
func joinSources(cs []*Cell) string {
	var b strings.Builder
	for i, c := range cs {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(c.ed.Value())
	}
	return b.String()
}

type undoKind int

const (
	undoDelete undoKind = iota
	undoMerge
	undoSplit
)

// cellUndo is one atomic destructive cell operation, separate from editor undo.
type cellUndo struct {
	kind                  undoKind
	cells                 []*Cell
	index                 int
	target                *Cell
	src                   string
	outputs               []notebook.Output
	images                []termimg.Key // saved target outputs for merge undo
	count                 int
	status                cellStatus
	errMsg                string
	ran                   bool
	splitTop, splitBottom string
	placeholder           *Cell
}

func (m *Model) pushCellUndo(u cellUndo) {
	m.undo = append(m.undo, u)
	if len(m.undo) > 100 {
		m.undo = slices.Clone(m.undo[len(m.undo)-100:])
	}
}
func (m *Model) deleteCells(lo, hi int) {
	u := cellUndo{kind: undoDelete, cells: slices.Clone(m.cells[lo : hi+1]), index: lo}
	m.cells = slices.Delete(m.cells, lo, hi+1)
	if len(m.cells) == 0 {
		u.placeholder = newCell(notebook.Code, "")
		m.cells = []*Cell{u.placeholder}
	}
	m.pushCellUndo(u)
	m.sel = clamp(m.sel, 0, len(m.cells)-1)
	m.collapse()
	m.dirty = true
}
func (m *Model) deletedStatus(n int) tea.Cmd {
	s := "cell deleted · undo delete"
	if n > 1 {
		s = fmt.Sprintf("%d cells deleted", n)
		return m.setStatus(statusInfo, "%s", withKey(s, m.keys.Undelete)+" to undo")
	}
	return m.setStatus(statusInfo, "%s", withKey(s, m.keys.Undelete))
}
func (m *Model) undoCells() tea.Cmd {
	if len(m.undo) == 0 {
		return nil
	}
	u := m.undo[len(m.undo)-1]
	m.undo[len(m.undo)-1] = cellUndo{}
	m.undo = m.undo[:len(m.undo)-1]
	i := slices.Index(m.cells, u.target)
	switch u.kind {
	case undoDelete:
		if p := slices.Index(m.cells, u.placeholder); p >= 0 && u.placeholder.ed.Value() == "" && !m.cellBusy(u.placeholder) {
			m.cells = slices.Delete(m.cells, p, p+1)
		}
		i = min(u.index, len(m.cells))
		m.insertCells(i, u.cells)
	case undoMerge:
		at := min(u.index, len(m.cells))
		if i >= 0 {
			if m.cellBusy(u.target) {
				m.pushCellUndo(u)
				return m.setStatus(statusInfo, "can't undo merge: cell is running or queued")
			}
			u.target.ed.SetValue(u.src)
			u.target.ed.breakUndo()
			u.target.outputs, u.target.count, u.target.status = u.outputs, u.count, u.status
			u.target.errMsg, u.target.ran = u.errMsg, u.ran
			u.target.outRev++
			u.target.mdOut = ""
			at = i + 1
		}
		m.insertCells(at, u.cells)
		i = max(0, at-1)
	case undoSplit:
		j := slices.Index(m.cells, u.cells[0])
		if i < 0 || j < 0 || m.cellBusy(u.target) || m.cellBusy(u.cells[0]) || u.target.ed.Value() != u.splitTop || u.cells[0].ed.Value() != u.splitBottom {
			return m.setStatus(statusInfo, "can't undo split: cell changed")
		}
		u.target.ed.SetValue(u.src)
		u.target.ed.breakUndo()
		u.target.mdOut = ""
		m.cells = slices.Delete(m.cells, j, j+1)
		i = slices.Index(m.cells, u.target)
	}
	m.sel = i
	m.collapse()
	m.dirty = true
	return nil
}
func (m *Model) cellBusy(c *Cell) bool {
	return c.status == statusRunning || c.status == statusQueued || slices.Contains(m.queue, c.id) || (m.running != nil && m.running.cellID == c.id)
}

// mergedSource adapts mixed types, following euporie's Notebook.merge.
func mergedSource(kind notebook.CellType, cs []*Cell) string {
	var b strings.Builder
	for i, c := range cs {
		if i > 0 {
			b.WriteString("\n\n")
		}
		src := c.ed.Value()
		switch {
		case kind == notebook.Code && c.kind != notebook.Code:
			for j, line := range strings.Split(src, "\n") {
				if j > 0 {
					b.WriteByte('\n')
				}
				b.WriteString("// ")
				b.WriteString(line)
			}
		case kind == notebook.Markdown && c.kind == notebook.Code:
			// Use a longer fence when the source itself contains backticks.
			fence := "```"
			for strings.Contains(src, fence) {
				fence += "`"
			}
			b.WriteString(fence)
			b.WriteString("go\n")
			b.WriteString(src)
			b.WriteByte('\n')
			b.WriteString(fence)
		default:
			b.WriteString(src)
		}
	}
	return b.String()
}
func (m *Model) mergeCells() tea.Cmd {
	lo, hi := m.selRange()
	if lo == hi {
		hi++
		if hi >= len(m.cells) {
			return m.setStatus(statusInfo, "nothing below to merge")
		}
	}
	for _, c := range m.cells[lo : hi+1] {
		if m.cellBusy(c) {
			return m.setStatus(statusInfo, "can't merge: cell is running or queued")
		}
	}
	c := m.cells[lo]
	m.pushCellUndo(cellUndo{kind: undoMerge, cells: slices.Clone(m.cells[lo+1 : hi+1]), index: lo + 1, target: c, src: c.ed.Value(), outputs: c.outputs, images: slices.Clone(c.images), count: c.count, status: c.status, errMsg: c.errMsg, ran: c.ran})
	c.ed.SetValue(mergedSource(c.kind, m.cells[lo:hi+1]))
	c.ed.breakUndo()
	m.clearCellOutput(c)
	c.errMsg, c.ran = "", false
	c.mdOut = ""
	m.cells = slices.Delete(m.cells, lo+1, hi+1)
	m.sel = lo
	m.collapse()
	m.dirty = true
	return m.setStatus(statusInfo, "%s", withKey(fmt.Sprintf("%d cells merged", hi-lo+1), m.keys.Undelete)+" to undo")
}

// splitCell follows euporie's cursor split, retaining exact source for undo.
func (m *Model) splitCell(i int) tea.Cmd {
	if i < 0 || i >= len(m.cells) {
		return nil
	}
	c := m.cells[i]
	if m.cellBusy(c) {
		return m.setStatus(statusInfo, "can't split: cell is running or queued")
	}
	row, col := c.ed.Cursor()
	offset := col
	for _, line := range c.ed.lines[:row] {
		offset += len(line) + 1
	}
	src := c.ed.Value()
	rs := []rune(src)
	top := strings.TrimSuffix(string(rs[:offset]), "\n")
	bottom := strings.TrimPrefix(string(rs[offset:]), "\n")
	n := newCell(c.kind, bottom)
	m.pushCellUndo(cellUndo{kind: undoSplit, target: c, cells: []*Cell{n}, src: src, splitTop: top, splitBottom: bottom})
	c.ed.SetValue(top)
	c.ed.breakUndo()
	c.mdOut = ""
	m.insertCell(i+1, n)
	n.ed.CursorStart()
	return m.setStatus(statusInfo, "%s", withKey("cell split · undo in command mode", m.keys.Undelete))
}
