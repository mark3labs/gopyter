package ui

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func TestCellCommandHelpFits(t *testing.T) {
	m := outputCellModel()
	m.width, m.height = 100, 30
	m.overlay = overlayHelp
	text, _ := m.renderOverlay()
	if lipgloss.Height(text) > 30 || lipgloss.Width(text) > 100 {
		t.Fatalf("help exceeds terminal: %dx%d", lipgloss.Width(text), lipgloss.Height(text))
	}
	for _, name := range []string{"hide/show input", "edit in $EDITOR", "save as", "restart & clear"} {
		if !strings.Contains(ansi.Strip(text), name) {
			t.Errorf("missing help: %s", name)
		}
	}
}

func TestConvertToRaw(t *testing.T) {
	m := outputCellModel()
	c := m.cur()
	c.appendOutput(notebook.Stdout, "output")
	m.handleKey(synthKey("r"))
	if c.kind != notebook.Raw || len(c.outputs) != 0 || c.count != 0 || !m.dirty {
		t.Fatal("raw conversion did not clear execution state")
	}
	m.renderBody()
	m.openContextMenu(30, headerHeight+1)
	labels := map[string]bool{}
	for _, item := range m.menu.items {
		labels[item.label] = true
	}
	if !labels["Convert to code"] || !labels["Convert to markdown"] || labels["Convert to raw"] {
		t.Fatalf("raw menu: %v", labels)
	}
	m.overlay = overlayNone
	for _, key := range []string{"y", "m", "r"} {
		m.handleKey(synthKey(key))
	}
	if c.kind != notebook.Raw {
		t.Fatal("conversion keys")
	}
	m.mode = modeEdit
	m.handleKey(press('t', tea.ModCtrl))
	if c.kind != notebook.Code {
		t.Fatal("ctrl+t on raw")
	}
	m.leaveEdit()
	m.running = &runState{cellID: c.id}
	m.handleKey(synthKey("r"))
	if c.kind != notebook.Code {
		t.Fatal("converted a running cell")
	}
}

func TestHideInputRenders(t *testing.T) {
	m := outputCellModel()
	c := m.cur()
	c.ed.SetValue("1\n2\n3\n4\n5")
	c.appendOutput(notebook.Stdout, "visible output")
	m.handleKey(synthKey("I"))
	r := m.renderCell(0, 100)
	z := cellZone(t, r, actToggleInput)
	if r.hasEdit || z.rect.Min.X != gutterWidth+2 || z.rect.Min.Y != r.edTop || !strings.Contains(ansi.Strip(r.lines[r.edTop]), "5 lines hidden") || !strings.Contains(strings.Join(r.lines, "\n"), "visible output") {
		t.Fatalf("hidden render: %+v", r)
	}
	m.handleKey(synthKey("I"))
	r = m.renderCell(0, 100)
	if !r.hasEdit || len(r.ev.lines) != 5 {
		t.Fatal("input not restored")
	}
	// Screen-space zones must take precedence over editor click handling.
	m.handleKey(synthKey("I"))
	m.renderBody()
	for _, z := range m.zones {
		if z.act.kind == actToggleInput {
			m.handleMouseDown(tea.Mouse{X: z.rect.Min.X, Y: z.rect.Min.Y, Button: tea.MouseLeft})
			break
		}
	}
	if c.inputHidden() {
		t.Fatal("placeholder click did not show input")
	}
	c.setInputHidden(true)
	m.handleKey(synthKey("enter"))
	if c.inputHidden() || m.mode != modeEdit {
		t.Fatal("enter left input hidden")
	}
	m.leaveEdit()
	c.setKind(notebook.Markdown)
	m.dirty = false
	m.handleKey(synthKey("I"))
	if c.inputHidden() || m.dirty || !strings.Contains(m.status, "markdown") {
		t.Fatal("markdown input toggle")
	}
}

func TestHideInputPersists(t *testing.T) {
	meta := map[string]any{"jupyter": map[string]any{"source_hidden": true}, "tags": []string{"x"}}
	original := &notebook.Cell{ID: "a", Type: notebook.Code, Metadata: meta}
	m := New(Options{Notebook: &notebook.Notebook{Cells: []*notebook.Cell{original}}})
	c := m.cur()
	if !c.inputHidden() || !reflect.DeepEqual(m.toNotebook().Cells[0].Metadata, meta) {
		t.Fatal("hidden metadata lost")
	}
	c.setInputHidden(false)
	c.setInputHidden(true)
	c.setInputHidden(false)
	if !reflect.DeepEqual(c.metadata, map[string]any{"tags": []string{"x"}}) {
		t.Fatalf("metadata: %v", c.metadata)
	}
	if !original.Metadata["jupyter"].(map[string]any)["source_hidden"].(bool) {
		t.Fatal("mutated original metadata")
	}
}

func TestHideInputReload(t *testing.T) {
	m, path := watchModel(t, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "1")}})
	c := codeCell("a", "1")
	c.Metadata = map[string]any{"jupyter": map[string]any{"source_hidden": true}}
	saveExternal(t, path, &notebook.Notebook{Cells: []*notebook.Cell{c}})
	poll(m)
	if !m.cur().inputHidden() {
		t.Fatal("reload lost hidden input")
	}
}

func TestScrollOutputSideways(t *testing.T) {
	m := outputCellModel()
	c := m.cur()
	wide := strings.Repeat("x", 300)
	c.appendOutput(notebook.Stdout, "\x1b[31m"+wide+"\x1b[m")
	c.appendOutput(notebook.HTMLOut, "<b>HTML stays wrapped</b>")
	width := m.outWidth()
	wrapped, _, _ := m.renderOutputs(c, width, false, "")
	m.handleKey(synthKey(">"))
	lines, _, _ := m.renderOutputs(c, width, false, "")
	if len(lines) != 2 || len(wrapped) <= len(lines) || lipgloss.Width(lines[0]) != width || lines[1] != wrapped[len(wrapped)-1] {
		t.Fatalf("scroll rendering: %q", lines)
	}
	r := m.renderCell(0, 100)
	for _, kind := range []actionKind{actScrollOutLeft, actScrollOutRight, actToggleWrap} {
		cellZone(t, r, kind)
	}
	m.handleKey(synthKey(">"))
	if c.scrollX != max(width/2, 4) {
		t.Fatalf("step: %d", c.scrollX)
	}
	lines, _, _ = m.renderOutputs(c, width, false, "")
	if ansi.Strip(lines[0]) != wide[:width] || !strings.Contains(lines[0], "31m") {
		t.Fatalf("cut lost color: %q", lines[0])
	}
	for range 30 {
		m.handleKey(synthKey(">"))
	}
	if c.scrollX != 300-width {
		t.Fatalf("clamp: %d width %d", c.scrollX, width)
	}
	for c.scrollX > 0 {
		m.handleKey(synthKey("<"))
	}
	m.handleKey(synthKey("<"))
	if c.noWrap {
		t.Fatal("left at zero did not restore wrap")
	}
	c.outputs = nil
	m.handleKey(synthKey(">"))
	if !strings.Contains(m.status, "no text output") {
		t.Fatal(m.status)
	}
}

func TestScrollOutputCacheKey(t *testing.T) {
	c := newCell(notebook.Code, "")
	a := c.outputFingerprint(80, "", false)
	c.noWrap = true
	b := c.outputFingerprint(80, "", false)
	c.scrollX = 4
	if a == b || b == c.outputFingerprint(80, "", false) {
		t.Fatal("scroll state missing from fingerprint")
	}
	m, c := runCell(t, "fmt.Println(strings.Repeat(\"x\", 300))")
	c.noWrap, c.scrollX = true, 30
	m.queue = []string{c.id}
	cmd := m.startNext()
	if c.scrollX != 0 || !c.noWrap {
		t.Fatal("rerun reset wrap preference")
	}
	for m.running != nil {
		cmd = m.handleRunEvents(cmd().(runEventsMsg))
	}
}

func TestEditorCommand(t *testing.T) {
	for _, tc := range []struct {
		visual, editor string
		args           []string
	}{
		{"code --wait", "nano", []string{"code", "--wait", "path.go"}},
		{"", "nano", []string{"nano", "path.go"}},
		{"", "", []string{"vi", "path.go"}},
	} {
		cmd := editorCommand(func(key string) string {
			if key == "VISUAL" {
				return tc.visual
			}
			return tc.editor
		}, "path.go")
		if !reflect.DeepEqual(cmd.Args, tc.args) {
			t.Fatalf("args: %v", cmd.Args)
		}
	}
}

func TestExternalEdit(t *testing.T) {
	for _, kind := range []notebook.CellType{notebook.Code, notebook.Markdown, notebook.Raw} {
		t.Run(string(kind), func(t *testing.T) {
			m := outputCellModel()
			c := m.cur()
			c.setKind(kind)
			orig := c.ed.Value()
			if m.handleKey(synthKey("E")) == nil {
				t.Fatal("no editor command")
			}
			path := m.extEdit.path
			t.Cleanup(func() { _ = os.Remove(path) }) // Cleanup also covers failures before completion.
			data, err := os.ReadFile(path)
			if err != nil || string(data) != orig {
				t.Fatalf("temp source: %q %v", data, err)
			}
			ext := map[notebook.CellType]string{notebook.Code: ".go", notebook.Markdown: ".md", notebook.Raw: ".txt"}[kind]
			if filepath.Ext(path) != ext {
				t.Fatal(path)
			}
			if err := os.WriteFile(path, []byte("changed\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			m.Update(editorDoneMsg{})
			if c.ed.Value() != "changed" || !m.dirty || m.busy() {
				t.Fatal("edit not applied")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("temp file not removed")
			}
			c.ed.Undo()
			if c.ed.Value() != orig {
				t.Fatal("undo")
			}
			m.startExternalEdit(0)
			m.finishExternalEdit(editorDoneMsg{errors.New("exit 1")})
			if c.ed.Value() != orig || !strings.Contains(m.status, "exit 1") {
				t.Fatal("failed editor changed cell")
			}
			m.startExternalEdit(0)
			path = m.extEdit.path
			m.cells = nil
			m.finishExternalEdit(editorDoneMsg{})
			if _, err := os.Stat(path); err != nil || !strings.Contains(m.status, path) {
				t.Fatal("deleted cell edit not preserved")
			}
		})
	}
}

func TestCopyOutputKey(t *testing.T) {
	m := outputCellModel()
	m.cur().appendOutput(notebook.Stdout, "hello")
	if m.handleKey(synthKey("C")) == nil || m.textClip != plainOutput(m.cur()) {
		t.Fatal("copy output key")
	}
	m.cur().outputs = nil
	m.handleKey(synthKey("C"))
	if m.textClip != "hello" || m.status != "no output to copy" {
		t.Fatalf("empty copy: %q %q", m.textClip, m.status)
	}
}

func TestRestartAndClear(t *testing.T) {
	for _, mode := range []mode{modeCommand, modeEdit} {
		m, c := runCell(t, "x := 1")
		m.mode = mode
		m.handleKey(press('r', tea.ModAlt))
		if c.count != 0 || len(c.outputs) != 0 || m.counter != 0 || m.status != "kernel restarted · outputs cleared" {
			t.Fatalf("restart: %s", m.status)
		}
		c.ed.SetValue("x")
		m.queue = []string{c.id}
		cmd := m.startNext()
		for m.running != nil {
			cmd = m.handleRunEvents(cmd().(runEventsMsg))
		}
		if c.status != statusFailed || !strings.Contains(plainOutput(c), "undefined: x") {
			t.Fatal("kernel retained declaration")
		}
	}
}

func TestSaveAsKey(t *testing.T) {
	m, old := watchModel(t, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "1")}})
	gen := m.watch.gen
	stale := pollDisk(old, m.watch.state, gen)().(diskPollMsg)
	m.handleKey(press('s', tea.ModAlt))
	if m.overlay != overlaySaveAs || m.input.Value() != old {
		t.Fatal("save-as did not prefill path")
	}
	path := filepath.Join(filepath.Dir(old), "fork.ipynb")
	m.input.SetValue(path)
	m.handleKey(synthKey("enter"))
	if _, err := os.Stat(path); err != nil || m.path != path || m.watch.gen <= gen {
		t.Fatal("save-as did not update watcher")
	}
	m.handleDiskPoll(stale)
	saveExternal(t, old, &notebook.Notebook{Cells: []*notebook.Cell{codeCell("a", "changed")}})
	poll(m)
	if m.cur().ed.Value() != "1" {
		t.Fatal("old file reloaded")
	}
	m.mode = modeEdit
	m.handleKey(press('s', tea.ModAlt))
	// Under a regular file: save creates missing directories, not this.
	m.input.SetValue(filepath.Join(old, "fail.ipynb"))
	m.handleKey(synthKey("enter"))
	if m.path != path || m.statusKind != statusError {
		t.Fatal("failed save changed path")
	}
}
