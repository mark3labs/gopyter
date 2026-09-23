package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// watchModel saves nb to a temp file and opens it.
func watchModel(t *testing.T, nb *notebook.Notebook) (*Model, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nb.ipynb")
	if err := nb.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := notebook.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Path: path, Notebook: loaded})
	m.width, m.height = 100, 30
	return m, path
}

// writeExternal writes raw content as another program would, moving the
// mtime forward so a same-size write is still noticed.
func writeExternal(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
}

func saveExternal(t *testing.T, path string, nb *notebook.Notebook) {
	t.Helper()
	b, err := nb.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	writeExternal(t, path, string(b))
}

// poll runs one watcher check synchronously.
func poll(m *Model) {
	msg := pollDisk(m.path, m.watch.state, m.watch.gen)().(diskPollMsg)
	m.handleDiskPoll(msg)
}

func codeCell(id, src string) *notebook.Cell {
	return &notebook.Cell{ID: id, Type: notebook.Code, Source: src}
}

func TestWatchReloadsCleanNotebook(t *testing.T) {
	m, path := watchModel(t, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 1"), codeCell("b", "y := 2")}})
	m.sel = 1
	b := m.cells[1]

	saveExternal(t, path, &notebook.Notebook{Cells: []*notebook.Cell{
		codeCell("new", "fmt.Println()"), codeCell("a", "x := 10"), codeCell("b", "y := 2"),
	}})
	poll(m)

	if m.overlay != overlayNone || m.dirty {
		t.Fatalf("clean notebook should reload silently: overlay=%v dirty=%v", m.overlay, m.dirty)
	}
	if len(m.cells) != 3 || m.cells[1].ed.Value() != "x := 10" {
		t.Fatalf("cells not reloaded: %d cells", len(m.cells))
	}
	if m.cells[2] != b || m.sel != 2 {
		t.Fatalf("selection should follow cell b: sel=%d", m.sel)
	}
	// The external edit is undoable.
	m.cells[1].ed.Undo()
	if got := m.cells[1].ed.Value(); got != "x := 1" {
		t.Fatalf("undo after reload = %q", got)
	}
}

func TestWatchIgnoresOwnSave(t *testing.T) {
	m, _ := watchModel(t, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 1")}})
	m.cells[0].ed.SetValue("x := 2")
	m.dirty = true
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	c := m.cells[0]
	poll(m)
	if m.overlay != overlayNone || m.cells[0] != c || m.status != "" {
		t.Fatalf("own save treated as external change: overlay=%v status=%q", m.overlay, m.status)
	}
}

func TestWatchTouchDoesNotReload(t *testing.T) {
	m, path := watchModel(t, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 1")}})
	m.dirty = true
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeExternal(t, path, string(b))
	poll(m)
	if m.overlay != overlayNone {
		t.Fatal("identical content should not prompt")
	}
}

func TestWatchConflictDialog(t *testing.T) {
	m, path := watchModel(t, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 1")}})
	m.cells[0].ed.SetValue("mine")
	m.dirty = true

	saveExternal(t, path, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "theirs")}})
	poll(m)
	if m.overlay != overlayReload {
		t.Fatalf("unsaved changes should prompt, overlay=%v", m.overlay)
	}

	// Keep mine: stays dirty and isn't asked again for the same version.
	m.handleKey(press(tea.KeyEscape, 0))
	if m.overlay != overlayNone || !m.dirty || m.cells[0].ed.Value() != "mine" {
		t.Fatalf("keep mine: overlay=%v dirty=%v value=%q", m.overlay, m.dirty, m.cells[0].ed.Value())
	}
	poll(m)
	if m.overlay != overlayNone {
		t.Fatal("should not ask again for an already seen version")
	}

	// A newer external version asks again; Reload takes it.
	saveExternal(t, path, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "theirs v2")}})
	poll(m)
	if m.overlay != overlayReload {
		t.Fatal("newer version should prompt")
	}
	m.handleKey(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if m.overlay != overlayNone || m.dirty || m.cells[0].ed.Value() != "theirs v2" {
		t.Fatalf("reload: overlay=%v dirty=%v value=%q", m.overlay, m.dirty, m.cells[0].ed.Value())
	}
}

func TestWatchEditingCell(t *testing.T) {
	m, path := watchModel(t, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 1"), codeCell("b", "y := 2")}})
	m.enterEdit()

	// Another cell changed: reload silently and keep editing.
	saveExternal(t, path, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 1"), codeCell("b", "y := 3")}})
	poll(m)
	if m.overlay != overlayNone || m.mode != modeEdit || m.cells[1].ed.Value() != "y := 3" {
		t.Fatalf("overlay=%v mode=%v", m.overlay, m.mode)
	}

	// The edited cell changed: ask even without unsaved changes.
	saveExternal(t, path, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 5"), codeCell("b", "y := 3")}})
	poll(m)
	if m.overlay != overlayReload {
		t.Fatal("changing the edited cell should prompt")
	}
}

func TestWatchDefersWhileBusy(t *testing.T) {
	m, path := watchModel(t, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 1")}})
	m.running = &runState{}
	saveExternal(t, path, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 2")}})
	poll(m)
	if m.cells[0].ed.Value() != "x := 1" {
		t.Fatal("must not reload while a cell runs")
	}
	m.running = nil
	poll(m)
	if m.cells[0].ed.Value() != "x := 2" {
		t.Fatal("deferred change should apply once idle")
	}
}

func TestWatchPartialWriteRetried(t *testing.T) {
	m, path := watchModel(t, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 1")}})
	writeExternal(t, path, `{"cells": [`)
	poll(m)
	if m.cells[0].ed.Value() != "x := 1" || m.status != "" {
		t.Fatal("unparsable file should be ignored")
	}
	saveExternal(t, path, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 2")}})
	poll(m)
	if m.cells[0].ed.Value() != "x := 2" {
		t.Fatal("complete file should reload")
	}
}

func TestWatchDeletedFile(t *testing.T) {
	m, path := watchModel(t, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 1")}})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	poll(m)
	if !m.dirty || m.statusKind != statusError || len(m.cells) != 1 {
		t.Fatalf("deleted file: dirty=%v status=%q", m.dirty, m.status)
	}
}

func TestWatchMatchesCellsWithoutIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.ipynb")
	const v1 = `{"cells": [{"cell_type": "code", "source": "x := 1", "metadata": {}, "outputs": [], "execution_count": null}], "metadata": {}, "nbformat": 4, "nbformat_minor": 4}`
	writeExternal(t, path, v1)
	nb, err := notebook.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Path: path, Notebook: nb})
	first := m.cells[0]

	const v2 = `{"cells": [{"cell_type": "code", "source": "x := 1", "metadata": {}, "outputs": [], "execution_count": null},` +
		`{"cell_type": "markdown", "source": "# hi", "metadata": {}}], "metadata": {}, "nbformat": 4, "nbformat_minor": 4}`
	if err := os.WriteFile(path, []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	poll(m)
	if len(m.cells) != 2 || m.cells[0] != first {
		t.Fatalf("unchanged ID-less cell should be kept: %d cells", len(m.cells))
	}
}

func TestWatchReloadUpdatesOutputs(t *testing.T) {
	m, path := watchModel(t, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "1+1")}})
	c := m.cells[0]
	saveExternal(t, path, &notebook.Notebook{Cells: []*notebook.Cell{{
		ID: "a", Type: notebook.Code, Source: "1+1", ExecutionCount: 3,
		Outputs: []notebook.Output{{Kind: notebook.Result, Text: "2"}},
	}}})
	poll(m)
	if m.cells[0] != c || c.count != 3 || len(c.outputs) != 1 || c.status != statusOK || m.counter != 3 {
		t.Fatalf("outputs not reloaded: count=%d outputs=%v status=%v", c.count, c.outputs, c.status)
	}
}

func TestWatchDropsPollFromBeforeSave(t *testing.T) {
	m, path := watchModel(t, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "x := 1")}})
	saveExternal(t, path, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "theirs")}})
	stale := pollDisk(m.path, m.watch.state, m.watch.gen)().(diskPollMsg)

	// We save over it before the poll result arrives.
	m.cells[0].ed.SetValue("mine")
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	m.handleDiskPoll(stale)
	if m.overlay != overlayNone || m.cells[0].ed.Value() != "mine" {
		t.Fatal("a poll from before our save must be dropped")
	}
}
