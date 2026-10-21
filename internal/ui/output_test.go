package ui

import (
	"reflect"
	"testing"

	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func ranNotebook() *notebook.Notebook {
	return &notebook.Notebook{Cells: []*notebook.Cell{
		{ID: "a", Type: notebook.Code, Source: "1", ExecutionCount: 1, Outputs: []notebook.Output{{Kind: notebook.Stdout, Text: "1\n"}}},
		{ID: "b", Type: notebook.Code, Source: "2", ExecutionCount: 2, Outputs: []notebook.Output{{Kind: notebook.Error, Text: "boom"}}},
		{ID: "c", Type: notebook.Markdown, Source: "# hi"},
	}}
}

func TestClearAllOutputsKey(t *testing.T) {
	m := New(Options{Notebook: ranNotebook()})
	m.dirty = false

	// A running cell keeps its output: the program is still writing to it.
	m.cells[0].status = statusRunning

	m.handleKey(synthKey("D"))

	if len(m.cells[1].outputs) != 0 || m.cells[1].count != 0 || m.cells[1].status != statusIdle {
		t.Fatalf("failed cell not cleared: outputs=%d count=%d status=%v",
			len(m.cells[1].outputs), m.cells[1].count, m.cells[1].status)
	}
	if len(m.cells[0].outputs) != 1 || m.cells[0].status != statusRunning {
		t.Fatalf("running cell was cleared: outputs=%d status=%v", len(m.cells[0].outputs), m.cells[0].status)
	}
	if m.status == "" {
		t.Fatal("clearing outputs should report it in the status bar")
	}

	// Only the running cell has output left, so there's nothing to clear.
	m.status = ""
	if cmd := m.clearAllOutputs(); cmd != nil {
		t.Fatal("nothing left to clear, want no status message")
	}
}

func TestClearAllOutputsFromMenu(t *testing.T) {
	m := New(Options{Notebook: ranNotebook()})
	if !m.hasOutputs() {
		t.Fatal("hasOutputs: want true for a notebook with saved outputs")
	}
	m.doAction(action{kind: actClearAllOutput})
	for i, c := range m.cells {
		if len(c.outputs) != 0 || c.count != 0 || c.status != statusIdle {
			t.Fatalf("cell %d not cleared: outputs=%d count=%d status=%v", i, len(c.outputs), c.count, c.status)
		}
	}
	if m.hasOutputs() {
		t.Fatal("hasOutputs: want false once every output is gone")
	}
}

func TestHandleRunEventsOutputs(t *testing.T) {
	c := newCell(notebook.Code, "")
	m := &Model{cells: []*Cell{c}, running: &runState{id: 1, cellID: c.id}}
	m.handleRunEvents(runEventsMsg{id: 1, events: []kernel.Event{
		{Kind: kernel.Stdout, Text: "a"},
		{Kind: kernel.Stdout, Text: "b"},
		{Kind: kernel.Result, Text: "1", ID: "x"},
		{Kind: kernel.Result, Text: "2", ID: "x"},
		{Kind: kernel.Info, Text: "i"},
	}})
	want := []notebook.Output{
		{Kind: notebook.Stdout, Text: "ab"},
		{Kind: notebook.Result, Text: "2", ID: "x"},
		{Kind: notebook.Info, Text: "i"},
	}
	if !reflect.DeepEqual(c.outputs, want) {
		t.Fatalf("outputs = %+v, want %+v", c.outputs, want)
	}
	if c.outRev != 5 {
		t.Fatalf("output revision = %d, want 5", c.outRev)
	}
}
