package ui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func selectionModel(src ...string) *Model {
	nb := notebook.New()
	nb.Cells = nil
	for _, s := range src {
		nb.Cells = append(nb.Cells, &notebook.Cell{ID: notebook.NewID(), Type: notebook.Code, Source: s})
	}
	m := New(Options{Notebook: nb})
	m.mode = modeCommand
	return m
}
func wantRange(t *testing.T, m *Model, lo, hi int) {
	t.Helper()
	a, b := m.selRange()
	if a != lo || b != hi {
		t.Fatalf("range %d..%d, want %d..%d", a, b, lo, hi)
	}
}
func wantSources(t *testing.T, m *Model, src ...string) {
	t.Helper()
	var got []string
	for _, c := range m.cells {
		got = append(got, c.ed.Value())
	}
	if !slices.Equal(got, src) {
		t.Fatalf("sources %q, want %q", got, src)
	}
}
func alt(m *Model, r rune) tea.Cmd { return m.handleKey(tea.KeyPressMsg{Code: r, Mod: tea.ModAlt}) }
func TestExtendSelectionKeys(t *testing.T) {
	m := selectionModel("a", "b", "c", "d", "e")
	m.sel = 2
	alt(m, tea.KeyDown)
	alt(m, tea.KeyDown)
	alt(m, tea.KeyDown)
	wantRange(t, m, 2, 4)
	for range 3 {
		alt(m, tea.KeyUp)
	}
	wantRange(t, m, 1, 2)
	m.handleKey(synthKey("j"))
	wantRange(t, m, 2, 2)
	alt(m, 'a')
	wantRange(t, m, 0, 4)
	m.handleKey(synthKey("esc"))
	wantRange(t, m, 4, 4)
	alt(m, 'a')
	m.handleKey(synthKey("enter"))
	if m.anchor != -1 || m.mode != modeEdit {
		t.Fatal("edit did not collapse")
	}
}
func TestEscCollapsesBeforeCancellingTask(t *testing.T) {
	m := selectionModel("a", "b")
	m.anchor = 0
	m.sel = 1
	cancelled := false
	m.task.active = true
	m.task.cancel = func() { cancelled = true }
	m.handleKey(synthKey("esc"))
	if cancelled || !m.task.active {
		t.Fatal("cancelled before collapse")
	}
	m.handleKey(synthKey("esc"))
	if !cancelled {
		t.Fatal("task not cancelled")
	}
}
func TestDeleteRangeOneUndo(t *testing.T) {
	m := selectionModel("a", "b", "c", "d", "e")
	orig := slices.Clone(m.cells)
	m.anchor = 1
	m.sel = 3
	m.handleKey(synthKey("d"))
	m.handleKey(synthKey("d"))
	wantSources(t, m, "a", "e")
	m.handleKey(synthKey("z"))
	if !slices.Equal(m.cells, orig) {
		t.Fatal("undo lost cells or order")
	}
	alt(m, 'a')
	m.runCommand("delete", true)
	if len(m.cells) != 1 {
		t.Fatal("no empty fallback")
	}
	m.undoCells()
	if !slices.Equal(m.cells, orig) {
		t.Fatal("undo all did not restore exact notebook")
	}
	for range 110 {
		m.deleteCell(0)
	}
	if len(m.undo) != 100 {
		t.Fatalf("undo not bounded: %d", len(m.undo))
	}
}
func TestCutCopyPasteRange(t *testing.T) {
	m := selectionModel("a", "b", "c", "d", "e")
	m.anchor = 1
	m.sel = 2
	cmd := m.handleKey(synthKey("c"))
	if cmd == nil || joinSources(m.clipboard) != "b\n\nc" {
		t.Fatal("missing joined clipboard")
	}
	// Clipboard is a batch containing terminal clipboard and status commands.
	if _, ok := cmd().(tea.BatchMsg); !ok {
		t.Fatal("not a clipboard batch")
	}
	m.sel = 4
	m.collapse()
	m.handleKey(synthKey("v"))
	wantSources(t, m, "a", "b", "c", "d", "e", "b", "c")
	wantRange(t, m, 5, 6)
	if m.cells[5].id == m.cells[1].id || m.cells[6].id == m.cells[2].id {
		t.Fatal("paste reused IDs")
	}
	m.anchor = 1
	m.sel = 2
	orig := slices.Clone(m.cells)
	m.handleKey(synthKey("x"))
	wantSources(t, m, "a", "d", "e", "b", "c")
	m.undoCells()
	if !slices.Equal(m.cells, orig) {
		t.Fatal("cut undo")
	}
}
func TestMoveBlock(t *testing.T) {
	m := selectionModel("a", "b", "c", "d")
	m.anchor = 1
	m.sel = 2
	m.handleKey(synthKey("K"))
	wantSources(t, m, "b", "c", "a", "d")
	wantRange(t, m, 0, 1)
	m.handleKey(synthKey("K"))
	wantRange(t, m, 0, 1)
	m.handleKey(synthKey("J"))
	m.handleKey(synthKey("J"))
	wantSources(t, m, "a", "d", "b", "c")
	wantRange(t, m, 2, 3)
}
func TestConvertAndClearRange(t *testing.T) {
	m := selectionModel("a", "b", "c", "d")
	m.anchor = 0
	m.sel = 2
	m.cells[1].status = statusRunning
	m.handleKey(synthKey("m"))
	for _, i := range []int{0, 2} {
		if m.cells[i].kind != notebook.Markdown {
			t.Fatal("not converted")
		}
	}
	if m.cells[1].kind != notebook.Code || m.cells[3].kind != notebook.Code {
		t.Fatal("converted wrong cells")
	}
	m.handleKey(synthKey("y"))
	wantRange(t, m, 0, 2)
	m = New(Options{Notebook: ranNotebook()})
	m.mode = modeCommand
	m.sel = 1
	m.anchor = 1
	m.handleKey(synthKey("O"))
	if len(m.cells[0].outputs) == 0 || len(m.cells[1].outputs) != 0 {
		t.Fatal("clear escaped range")
	}
}
func TestRunRange(t *testing.T) {
	for _, key := range []string{"run", "run_advance", "run_insert"} {
		t.Run(key, func(t *testing.T) {
			m := selectionModel("a", "# hi", "c", "d")
			m.cells[1].setKind(notebook.Markdown)
			m.cells[1].mdOut = "cached"
			m.running = &runState{cellID: "other"}
			m.anchor = 2
			m.sel = 0
			switch key {
			case "run":
				m.runSelected(false, false)
			case "run_advance":
				m.runSelected(true, false)
			case "run_insert":
				m.runSelected(false, true)
			}
			if !reflect.DeepEqual(m.queue, []string{m.cells[0].id, m.cells[2].id}) || m.cells[1].mdOut != "" {
				t.Fatal("bad run range")
			}
			if m.anchor != -1 {
				t.Fatal("run retained range")
			}
			if key != "run" && m.sel != 3 {
				t.Fatal("did not advance past last selected")
			}
		})
	}
}
func TestMergeCells(t *testing.T) {
	m := selectionModel("a", "# t", "b")
	m.cells[1].setKind(notebook.Markdown)
	m.cells[0].count = 4
	m.cells[0].outputs = []notebook.Output{{Kind: notebook.Stdout, Text: "old"}}
	orig := slices.Clone(m.cells)
	m.anchor = 2
	m.sel = 0
	alt(m, 'm')
	wantSources(t, m, "a\n\n// # t\n\nb")
	if m.cells[0] != orig[0] || m.cells[0].count != 0 || len(m.cells[0].outputs) != 0 {
		t.Fatal("merge identity/output")
	}
	m.handleKey(synthKey("z"))
	wantSources(t, m, "a", "# t", "b")
	if !slices.Equal(m.cells, orig) || m.cells[0].count != 4 || len(m.cells[0].outputs) != 1 {
		t.Fatal("merge undo")
	}
	m = selectionModel("# title", "x")
	m.cells[0].setKind(notebook.Markdown)
	alt(m, 'm')
	wantSources(t, m, "# title\n\n```go\nx\n```")
	m.status = ""
	alt(m, 'm')
	if m.status == "" || len(m.cells) != 1 {
		t.Fatal("last merge")
	}
	m = selectionModel("a", "raw\ntext")
	m.cells[1].setKind(notebook.Raw)
	alt(m, 'm')
	wantSources(t, m, "a\n\n// raw\n// text")
}
func TestMergeRefusesRunningCell(t *testing.T) {
	for _, status := range []cellStatus{statusRunning, statusQueued} {
		m := selectionModel("a", "b")
		m.cells[1].status = status
		alt(m, 'm')
		wantSources(t, m, "a", "b")
		if m.status == "" || len(m.undo) != 0 {
			t.Fatal("no refusal")
		}
	}
	m := selectionModel("a", "b")
	m.running = &runState{cellID: m.cells[1].id}
	alt(m, 'm')
	wantSources(t, m, "a", "b")
}
func TestSplitCell(t *testing.T) {
	for _, vim := range []bool{false, true} {
		m := selectionModel("a\nb\nc")
		m.vim.enabled = vim
		m.enterEdit()
		m.cur().ed.SetCursor(1, 0)
		m.cells[0].outputs = []notebook.Output{{Kind: notebook.Stdout, Text: "old"}}
		alt(m, '-')
		wantSources(t, m, "a", "b\nc")
		row, col := m.cur().ed.Cursor()
		if m.sel != 1 || m.mode != modeEdit || row != 0 || col != 0 || len(m.cells[0].outputs) != 1 || len(m.cells[1].outputs) != 0 || m.vim.mode != vimInsert {
			t.Fatal("split state")
		}
		m.leaveEdit()
		m.handleKey(synthKey("z"))
		wantSources(t, m, "a\nb\nc")
	}
	for _, pos := range []Pos{{0, 0}, {0, 2}, {1, 0}, {1, 2}} {
		m := selectionModel("αβ\n世界")
		m.enterEdit()
		m.cur().ed.SetCursor(pos.Row, pos.Col)
		m.cur().ed.SelectAll()
		m.cur().ed.SetCursor(pos.Row, pos.Col)
		alt(m, '-')
		if len(m.cells) != 2 {
			t.Fatal("split failed")
		}
		m.leaveEdit()
		m.undoCells()
		wantSources(t, m, "αβ\n世界")
	}
	m := selectionModel("a")
	m.enterEdit()
	m.cur().status = statusRunning
	alt(m, '-')
	wantSources(t, m, "a")
}
func TestUndoSplitAfterChange(t *testing.T) {
	m := selectionModel("a\nb")
	m.enterEdit()
	m.cur().ed.SetCursor(1, 0)
	alt(m, '-')
	m.leaveEdit()
	m.deleteCell(1)
	m.undoCells()
	m.undoCells()
	wantSources(t, m, "a\nb")
	m.enterEdit()
	m.cur().ed.SetCursor(1, 0)
	alt(m, '-')
	m.cur().ed.SetValue("changed")
	m.leaveEdit()
	m.undoCells()
	wantSources(t, m, "a", "changed")
	if m.status != "can't undo split: cell changed" {
		t.Fatal(m.status)
	}
}
func TestShiftClickExtends(t *testing.T) {
	m := selectionModel("abc", "def", "ghi")
	m.k = &kernel.Kernel{}
	m.width, m.height = 100, 40
	m.View()
	click := func(i int, mod tea.KeyMod) {
		m.handleMouseDown(tea.Mouse{X: 0, Y: headerHeight + m.layout[i].top, Button: tea.MouseLeft, Mod: mod})
	}
	click(0, 0)
	click(2, tea.ModShift)
	wantRange(t, m, 0, 2)
	click(1, 0)
	wantRange(t, m, 1, 1)
	m.enterEdit()
	m.View()
	r := m.layout[1].render
	m.cur().ed.CursorStart()
	m.click = clickState{}
	m.handleMouseDown(tea.Mouse{X: r.edLeft + r.ev.gutterW + 2, Y: headerHeight + m.layout[1].top + r.edTop, Button: tea.MouseLeft, Mod: tea.ModShift})
	if !m.cur().ed.HasSelection() || m.multiSelected() {
		t.Fatal("text selection lost")
	}
}
func TestContextMenuOnRange(t *testing.T) {
	m := selectionModel("a", "b", "c", "d")
	m.k = &kernel.Kernel{}
	m.anchor = 0
	m.sel = 2
	m.width, m.height = 100, 40
	m.View()
	m.openContextMenu(0, headerHeight+m.layout[1].top)
	wantRange(t, m, 0, 2)
	found := false
	for _, item := range m.menu.items {
		if item.label == "Delete 3 cells" {
			found = true
			m.doAction(item.act)
			break
		}
	}
	if !found {
		t.Fatal("missing range label")
	}
	wantSources(t, m, "d")
	m.undoCells()
	m.anchor = 0
	m.sel = 2
	m.overlay = overlayNone
	m.View()
	m.openContextMenu(0, headerHeight+m.layout[3].top)
	wantRange(t, m, 3, 3)
}
func TestRenderRangeSelected(t *testing.T) {
	m := selectionModel("a", "b", "c")
	m.k = &kernel.Kernel{}
	m.anchor = 1
	m.sel = 2
	m.width, m.height = 100, 40
	for _, i := range []int{1, 2} {
		r := m.renderCell(i, 100)
		if !strings.HasPrefix(ansi.Strip(r.lines[0]), "▌") {
			t.Fatal("missing range gutter")
		}
		for _, z := range r.zones {
			if z.act.kind == actRunCell && z.rect.Min.Y > r.edTop && i != m.sel {
				t.Fatal("noncursor action buttons")
			}
		}
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "cells 2–3/3") {
		t.Fatal("range footer missing")
	}
}
