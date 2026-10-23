package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func sidebarModel() *Model {
	m := New(Options{Notebook: &notebook.Notebook{Cells: []*notebook.Cell{
		{ID: "a", Type: notebook.Code, Source: "1"},
		{ID: "b", Type: notebook.Markdown, Source: "# Intro\n## Setup"},
		{ID: "c", Type: notebook.Code, Source: "// # not a heading"},
		{ID: "d", Type: notebook.Markdown, Source: "### Detail\n#### Deep\n```\n# no\n```"},
	}}})
	m.k = &kernel.Kernel{}
	m.width, m.height = 120, 20
	m.mode = modeCommand
	return m
}

func TestSidebarToggle(t *testing.T) {
	m := sidebarModel()
	full := m.outWidth()
	m.handleKey(press('b', tea.ModCtrl))
	if !m.side.open || !m.side.focused || m.sidebarWidth() != 30 || m.bodyWidth() != 90 || m.outWidth() != full-30 {
		t.Fatalf("toggle: %+v", m.side)
	}
	m.handleKey(press('b', tea.ModCtrl))
	if m.side.open || m.side.focused {
		t.Fatal("not hidden")
	}
	m.enterEdit()
	m.cur().ed.SetCursor(0, 1)
	m.handleKey(press('b', tea.ModCtrl))
	if m.side.open || m.cur().ed.col != 0 {
		t.Fatal("edit ctrl+b changed")
	}
}

func TestSidebarConfiguredBinding(t *testing.T) {
	m := sidebarModel()
	var warnings []string
	m.keys, warnings = newKeyMapFrom(map[string][]string{"sidebar": {"alt+b"}})
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}
	m.handleKey(press('b', tea.ModCtrl))
	if m.side.open {
		t.Fatal("old sidebar binding still active")
	}
	m.handleKey(press('b', tea.ModAlt))
	if !m.side.open || !m.side.focused {
		t.Fatal("configured binding failed")
	}
	m.handleKey(press('b', tea.ModAlt))
	if m.side.open {
		t.Fatal("configured binding did not close focused sidebar")
	}
	m.doAction(action{kind: actToggleSidebar})
	if !m.side.open {
		t.Fatal("toolbar must ignore rebinding")
	}
}

func TestSidebarAutoHide(t *testing.T) {
	m := sidebarModel()
	m.toggleSidebar()
	m.width = 80
	if m.sidebarWidth() != 0 {
		t.Fatal("narrow sidebar shown")
	}
	m.handleKey(press('b', tea.ModCtrl))
	if !strings.Contains(m.status, "too narrow") || !m.side.open {
		t.Fatal(m.status)
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	if m.sidebarWidth() != 30 {
		t.Fatal("choice not preserved")
	}
}

func TestSidebarContentsAndCurrentSection(t *testing.T) {
	m := sidebarModel()
	m.toggleSidebar()
	es := m.tocEntries()
	if len(es) != 3 || es[0].text != "Intro" || es[1].level != 2 || es[2].level != 3 {
		t.Fatal(es)
	}
	if m.currentSection(es) != -1 {
		t.Fatal("section before first heading")
	}
	m.sel = 2
	if m.currentSection(es) != 1 {
		t.Fatal("wrong section")
	}
	lines, _ := m.renderSidebar(18)
	if !strings.Contains(ansi.Strip(lines[3]), "▌  Setup") || !strings.Contains(ansi.Strip(lines[4]), "     Detail") {
		t.Fatal(lines)
	}
	m.cells[1].ed.SetValue("# Changed")
	if es = m.tocEntries(); len(es) != 2 || es[0].text != "Changed" {
		t.Fatal("stale cache", es)
	}
}

func TestSidebarWithCellSelection(t *testing.T) {
	m := sidebarModel()
	m.toggleSidebar()
	m.renderScreen()
	m.extendTo(2)
	m.handleMouseDown(tea.Mouse{X: m.width - 1, Y: m.height - footerHeight - 1, Button: tea.MouseLeft, Mod: tea.ModShift})
	if m.sel != 2 || m.anchor != 0 || !m.side.focused {
		t.Fatal("shift-click in sidebar changed the cell range")
	}
	m.jumpToCell(3)
	if m.sel != 3 || m.multiSelected() || m.side.focused {
		t.Fatal("sidebar jump retained the cell range")
	}
	m.extendTo(1)
	m.enterEdit()
	if m.anchor != -1 || m.side.focused {
		t.Fatal("editing did not clear range and sidebar focus")
	}
}

func TestSidebarJumpAndPanels(t *testing.T) {
	m := sidebarModel()
	m.toggleSidebar()
	m.renderScreen()
	m.handleKey(synthKey("j"))
	m.handleKey(synthKey("enter"))
	if m.sel != 1 || m.mode != modeCommand || m.offset != m.layout[1].top || m.side.focused {
		t.Fatal("keyboard jump failed")
	}
	m.side.focused = true
	m.handleKey(press(tea.KeyTab, 0))
	if m.side.panel != panelVariables {
		t.Fatal("tab")
	}
	m.handleKey(press(tea.KeyTab, tea.ModShift))
	if m.side.panel != panelContents {
		t.Fatal("shift tab")
	}
	m.renderScreen()
	for _, z := range m.zones {
		if z.act.kind == actSidebarPanel && z.act.cell == int(panelVariables) {
			m.handleMouseDown(tea.Mouse{X: z.rect.Min.X, Y: z.rect.Min.Y, Button: tea.MouseLeft})
			break
		}
	}
	if m.side.panel != panelVariables || !m.side.focused {
		t.Fatal("panel click")
	}
	m.setSidePanel(panelContents)
	m.renderScreen()
	for _, z := range m.zones {
		if z.act.kind == actSidebarItem && z.act.cell == 3 {
			m.handleMouseDown(tea.Mouse{X: z.rect.Min.X, Y: z.rect.Min.Y, Button: tea.MouseLeft})
			break
		}
	}
	if m.sel != 3 || m.side.focused {
		t.Fatal("mouse jump")
	}
}

func TestSidebarLayout(t *testing.T) {
	m := sidebarModel()
	before, _ := m.renderBody()
	m.toggleSidebar()
	m.zones = nil
	body, _ := m.renderBody()
	for _, line := range strings.Split(body, "\n") {
		if lipgloss.Width(line) != 120 {
			t.Fatalf("width %d: %q", lipgloss.Width(line), line)
		}
	}
	for _, z := range m.zones {
		if z.act.kind != actSidebarItem && z.act.kind != actSidebarPanel && z.rect.Max.X > m.bodyWidth()-1 {
			t.Fatalf("cell zone overlaps sidebar: %+v", z)
		}
	}
	sel := m.sel
	m.handleMouseDown(tea.Mouse{X: 119, Y: 18, Button: tea.MouseLeft})
	if m.sel != sel || !m.side.focused || m.drag.kind != dragNone {
		t.Fatal("blank sidebar click")
	}
	if _, _, cell := m.hoverAt(119, 4); cell != -1 {
		t.Fatal("hover selected notebook")
	}
	m.contentLines = 100
	m.handleMouseDown(tea.Mouse{X: m.bodyWidth() - 1, Y: 5, Button: tea.MouseLeft})
	if m.drag.kind != dragScroll {
		t.Fatal("notebook scrollbar didn't move")
	}
	m.drag = dragState{}
	m.offset = 0
	m.follow = true
	m.toggleSidebar()
	after, _ := m.renderBody()
	if before != after {
		t.Fatal("hidden rendering changed")
	}
}

func TestSidebarWheel(t *testing.T) {
	m := sidebarModel()
	var src strings.Builder
	for i := range 40 {
		fmt.Fprintf(&src, "# Heading %d\n", i)
	}
	m.cells[1].ed.SetValue(src.String())
	m.toggleSidebar()
	m.renderScreen()
	offset := m.offset
	m.handleWheel(tea.Mouse{X: 110, Y: 10, Button: tea.MouseWheelDown})
	if m.side.top != 3 || m.offset != offset {
		t.Fatal("sidebar wheel scrolled notebook")
	}
	m.handleWheel(tea.Mouse{X: 20, Y: 10, Button: tea.MouseWheelDown})
	if m.offset != offset+3 {
		t.Fatal("notebook wheel")
	}
	m.side.focused = true
	m.handleKey(synthKey("G"))
	if m.side.idx != 40 || m.side.top == 0 {
		t.Fatal("end did not reveal cursor", m.side)
	}
}

func TestSidebarVariables(t *testing.T) {
	m := sidebarModel()
	m.toggleSidebar()
	m.setSidePanel(panelVariables)
	m.side.vars = []kernel.Variable{
		{Name: "x", Kind: "var", Type: "int", Value: strings.Repeat("9", 100), CellID: "c"},
		{Name: "orphan", Kind: "const", Value: "3", CellID: "deleted"},
	}
	m.k = nil
	m.refreshVars() // nil kernel preserves fixture
	lines, zs := m.renderSidebar(18)
	if !strings.Contains(ansi.Strip(lines[2]), "x                         int") || !strings.Contains(lines[3], "…") {
		t.Fatal(lines)
	}
	for _, z := range zs {
		if z.act.kind == actSidebarItem && z.act.cell < 0 {
			t.Fatal("orphan zone", z)
		}
	}
	m.side.idx = 1
	m.handleKey(synthKey("enter"))
	if m.sel != 0 || !m.side.focused {
		t.Fatal("orphan jumped")
	}
	m.side.idx = 0
	m.k = &kernel.Kernel{}
	m.renderScreen()
	m.handleKey(synthKey("enter"))
	if m.sel != 2 || m.side.focused {
		t.Fatal("variable jump")
	}
}

func TestSidebarVariablesAfterRun(t *testing.T) {
	m, _, pump := startCell(t, "x := 41 + 1")
	pump(func() bool { return m.running == nil })
	if len(m.side.vars) != 1 || m.side.vars[0].Name != "x" || m.side.vars[0].Type != "int" || m.side.vars[0].Value != "42" {
		t.Fatal(m.side.vars)
	}
	m.handleKey(synthKey("R"))
	if len(m.side.vars) != 0 {
		t.Fatal("restart retained variables")
	}
}

func TestSidebarTheme(t *testing.T) {
	m := themeModel(t, Options{LightBackground: true})
	m.width = 120
	m.sel = 1
	m.toggleSidebar()
	before, _ := m.renderSidebar(10)
	m.applyTheme("dracula")
	after, _ := m.renderSidebar(10)
	marker := lipgloss.NewStyle().Foreground(colPrimary).Render("▌")
	if before[2] == after[2] || !strings.Contains(after[2], marker) {
		t.Fatal("sidebar palette did not update")
	}
}

func TestSidebarGlobalKeys(t *testing.T) {
	m, _, pump := startCell(t, "x := 41 + 1")
	pump(func() bool { return m.running == nil })
	m.width = 120
	m.toggleSidebar()
	m.path = t.TempDir() + "/sidebar.ipynb"
	m.dirty = true
	if cmd := m.handleKey(press('s', tea.ModCtrl)); cmd != nil {
		cmd()
	}
	if m.dirty {
		t.Fatal("global save swallowed")
	}
	m.handleKey(synthKey("A"))
	if m.running == nil {
		t.Fatal("run all swallowed")
	}
	rs := m.running
	m.interrupt()
	for range rs.ch {
	} // Wait for cancellation before the kernel cleanup.
}

func TestSidebarFocusFallThrough(t *testing.T) {
	m := sidebarModel()
	m.toggleSidebar()
	// Unhandled cell commands still act on the notebook.
	m.handleKey(synthKey("d"))
	m.handleKey(synthKey("d"))
	if len(m.cells) != 3 {
		t.Fatal("dd was swallowed")
	}
	m.handleKey(synthKey("esc"))
	if m.side.focused || !m.side.open {
		t.Fatal("escape closed sidebar")
	}
}
