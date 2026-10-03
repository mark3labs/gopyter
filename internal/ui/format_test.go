package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/notebook"
	"testing"
)

func TestFormatCellActions(t *testing.T) {
	for _, mode := range []mode{modeCommand, modeEdit} {
		for _, mouse := range []bool{false, true} {
			nb := &notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code, Source: "x:=1\nx+2"}}}
			m := New(Options{Notebook: nb})
			m.mode = mode
			before := m.cur().ed.Value()
			if mouse {
				m.doAction(action{kind: actFormatCell, cell: 0})
			} else {
				m.handleKey(press('F', tea.ModAlt|tea.ModShift))
			}
			if got := m.cur().ed.Value(); got != "x := 1\nx + 2\n" {
				t.Fatalf("mode %v mouse %v: %q", mode, mouse, got)
			}
			if !m.dirty || m.running != nil {
				t.Fatal("format should dirty source without executing")
			}
			m.cur().ed.Undo()
			if m.cur().ed.Value() != before {
				t.Fatal("format must be one undo step")
			}
		}
	}
}

func TestFormatCellNoChange(t *testing.T) {
	for _, tt := range []struct {
		kind   notebook.CellType
		source string
	}{
		{notebook.Code, "x := 1\n"}, {notebook.Code, "x :="},
		{notebook.Markdown, "x:=1"}, {notebook.Raw, "x:=1"},
	} {
		m := New(Options{Notebook: &notebook.Notebook{Cells: []*notebook.Cell{{Type: tt.kind, Source: tt.source}}}})
		m.formatCell(0)
		if m.cur().ed.Value() != tt.source || m.dirty {
			t.Fatalf("changed %v %q", tt.kind, tt.source)
		}
	}
}

func TestFormatCellButton(t *testing.T) {
	m := New(Options{Notebook: &notebook.Notebook{Cells: []*notebook.Cell{{Type: notebook.Code, Source: "x:=1"}}}})
	m.width, m.height = 100, 30
	r := m.renderCell(0, 100)
	found := false
	for _, z := range r.zones {
		if z.act.kind == actFormatCell && z.act.cell == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("missing format button zone")
	}
	m.cur().kind = notebook.Markdown
	r = m.renderCell(0, 100)
	for _, z := range r.zones {
		if z.act.kind == actFormatCell {
			t.Fatal("format button on markdown")
		}
	}
}
