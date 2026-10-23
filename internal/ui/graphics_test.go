package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
	"github.com/mark3labs/gopyter/internal/termimg"
)

func imageModel(t *testing.T) *Model {
	t.Helper()
	nb := &notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code, Source: "img", ExecutionCount: 1, Outputs: []notebook.Output{
		{Kind: notebook.Stdout, Text: "rendering\n"}, {Kind: notebook.ImageOut, Text: pngBase64(t, 640, 480), ID: "p"},
	}}}}
	m := New(Options{Notebook: nb, Kernel: &kernel.Kernel{}, Graphics: termimg.NewKitty(false, false)})
	m.width, m.height = 100, 60
	return m
}

func TestImageOutputKitty(t *testing.T) {
	m := imageModel(t)
	lines, at, _ := m.renderOutputs(m.cells[0], 80, false, "")
	if len(lines) != 31 || at != 1 {
		t.Fatal(len(lines), at)
	}
	for _, l := range lines[1:] {
		if ansi.StringWidth(l) != 80 || !strings.ContainsRune(l, termimg.Placeholder) {
			t.Fatal("invalid image line")
		}
	}
	// renderOutputs above used another box; sweep it after rendering the real view.
	if !strings.Contains(ansi.Strip(m.View().Content), "Out[1]") {
		t.Fatal("missing label")
	}
	m.gfx.Flush()
	m.View()
	if m.gfx.Flush() != "" {
		t.Fatal("second view resent image")
	}
}

func TestKittyThemeChangeKeepsImages(t *testing.T) {
	m := imageModel(t)
	m.View()
	m.gfx.Flush()
	m.applyTheme("dracula")
	m.View()
	if got := m.gfx.Flush(); got != "" {
		t.Fatal(got)
	}
}

func TestKittyFreesClearedImages(t *testing.T) {
	for _, name := range []string{"clear", "delete", "reload", "replace", "convert"} {
		t.Run(name, func(t *testing.T) {
			m := imageModel(t)
			m.View()
			m.gfx.Flush()
			c := m.cells[0]
			switch name {
			case "clear":
				c.outputs = nil
				c.outRev++
			case "convert":
				m.convertCell(0, false)
			case "delete":
				m.deleteCell(0)
			case "reload":
				m.applyReload(&notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code, Source: "img"}}}, diskState{})
			case "replace":
				c.setOutput(notebook.ImageOut, pngBase64(t, 4, 6), "p")
			}
			m.View()
			got := m.gfx.Flush()
			if name == "delete" {
				if got != "" {
					t.Fatal(got)
				}
				return
			}
			if !strings.Contains(got, "a=d,d=I,i=1,q=2") {
				t.Fatal("missing deletion", got)
			}
			if name == "replace" && !strings.Contains(got, "a=t,") {
				t.Fatal("replacement not sent")
			}
		})
	}
}

func TestKittyDimKeepsImageIDs(t *testing.T) {
	m := imageModel(t)
	lines, _, _ := m.renderOutputs(m.cells[0], 80, false, "")
	got := m.composite(lines[1], "", 0, 0, true)
	buf := uv.NewScreenBuffer(m.width, m.height)
	uv.NewStyledString(got).Draw(buf, buf.Bounds())
	c := buf.CellAt(0, 0)
	r, g, b, _ := c.Style.Fg.RGBA()
	if !strings.HasPrefix(c.Content, string(termimg.Placeholder)) || r != 0 || g != 0 || b != 257 {
		t.Fatalf("lost image color: %+v", c)
	}
}

func TestKittyGraphicsMsg(t *testing.T) {
	m := imageModel(t)
	m.View()
	_, cmd := m.Update(graphicsMsg{})
	raw, ok := cmd().(tea.RawMsg)
	if !ok || !strings.Contains(raw.Msg.(string), "a=t,") {
		t.Fatal(raw)
	}
}

func TestKittyQuitDeletesImages(t *testing.T) {
	m := imageModel(t)
	m.View()
	m.gfx.Flush()
	// Sequence's concrete message type is private; inspect its commands just as
	// Bubble Tea does, and verify raw deletion precedes Quit.
	seq := reflect.ValueOf(m.quit()())
	if seq.Len() != 2 {
		t.Fatal(seq)
	}
	raw := seq.Index(0).Interface().(tea.Cmd)().(tea.RawMsg)
	if !strings.Contains(raw.Msg.(string), "a=d,d=I") {
		t.Fatal(raw)
	}
	if _, ok := seq.Index(1).Interface().(tea.Cmd)().(tea.QuitMsg); !ok {
		t.Fatal("not Quit")
	}
}

func TestKittyFoldKeepsRowCoordinates(t *testing.T) {
	m := imageModel(t)
	c := m.cells[0]
	c.outputs = append(c.outputs, notebook.Output{Kind: notebook.Stdout, Text: strings.Repeat("tail\n", 25)})
	c.outRev++
	lines, _ := m.cellOutputs(0, 82, "", "")
	if !strings.Contains(ansi.Strip(lines[0]), "earlier lines hidden") {
		t.Fatal("not folded")
	}
	// The first surviving image row is row 16 (31 image/prefix lines +
	// 25 trailing lines - 40 visible lines); it must not become row zero.
	original, _, _ := m.renderOutputs(c, 80, false, "")
	visible := ""
	for _, line := range lines {
		if strings.ContainsRune(line, termimg.Placeholder) {
			visible = line
			break
		}
	}
	if visible == "" || !strings.Contains(visible, original[16]) {
		t.Fatal("fold renumbered image row")
	}
}

func TestKittyCellSizeInvalidatesCache(t *testing.T) {
	m := imageModel(t)
	m.View()
	m.gfx.Flush()
	m.gfx.SetCellSize(8, 16)
	m.gfxGen++
	m.View()
	if !strings.Contains(m.gfx.Flush(), "c=80,r=30") {
		t.Fatal("pixel box not rebuilt")
	}
}
