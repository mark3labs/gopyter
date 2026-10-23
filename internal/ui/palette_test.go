package ui

import (
	"image"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func paletteFind(m *Model, desc string) int {
	return slices.IndexFunc(m.palette.rows, func(h paletteHit) bool { return h.cmd.binding.Help().Desc == desc })
}

func paletteQuery(m *Model, q string) {
	m.palette.filter.SetValue(q)
	m.palette.filter.CursorEnd()
	m.refilterPalette()
}

func TestPaletteOpens(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{press('p', tea.ModCtrl), synthKey(":")} {
		m, _ := newPickerModel(t, "", false)
		m.handleKey(k)
		if m.overlay != overlayPalette || !m.palette.filter.Focused() || m.palette.idx != 0 {
			t.Fatal("palette not focused at first row")
		}
		for _, s := range []string{"Commands", "run & next", "⇧↵/^r"} {
			if !strings.Contains(screenText(m), s) {
				t.Errorf("missing %q", s)
			}
		}
		m.handleKey(press(tea.KeyEscape, 0))
		if m.overlay != overlayNone || m.palette.filter.Focused() {
			t.Fatal("escape did not blur")
		}
	}
	m, _ := newPickerModel(t, "", false)
	m.enterEdit()
	m.cur().ed.SetValue("first\nsecond")
	m.cur().ed.Down()
	m.handleKey(press('p', tea.ModCtrl))
	if m.overlay != overlayNone || m.cur().ed.row != 0 {
		t.Fatal("edit ctrl+p changed meaning")
	}
	m.handleKey(synthKey(":"))
	if !strings.Contains(m.cur().ed.Value(), ":") || m.overlay != overlayNone {
		t.Fatal("colon did not edit")
	}
	m.leaveEdit()
	m.running = &runState{cellID: m.cur().id}
	m.in.focus = focusStdin
	m.in.line.Focus()
	m.handleKey(synthKey(":"))
	if m.in.line.Value() != ":" || m.overlay != overlayNone {
		t.Fatal("stdin colon intercepted")
	}
}

func TestPaletteFilterRanks(t *testing.T) {
	m, _ := newPickerModel(t, "", false)
	m.openPalette()
	for q, want := range map[string]string{"markdown": "to markdown", "run all": "run all", "rs": "restart kernel", "dd": "delete"} {
		paletteQuery(m, q)
		if paletteFind(m, want) < 0 {
			t.Fatalf("%q missing %q", q, want)
		}
		if q != "rs" && paletteFind(m, want) != 0 {
			t.Errorf("%q not first for %q", want, q)
		}
	}
	paletteQuery(m, "")
	m.movePalette(4)
	typePicker(m, "qj")
	if m.palette.filter.Value() != "qj" || m.palette.idx != 0 {
		t.Fatal("letters must filter and reset selection")
	}
	paletteQuery(m, "zzzz")
	before := m.cur()
	m.handleKey(press(tea.KeyEnter, 0))
	if m.overlay != overlayPalette || m.cur() != before || !strings.Contains(screenText(m), "no match") {
		t.Fatal("empty results ran a command")
	}
	paletteQuery(m, "markdownx")
	m.handleKey(press(tea.KeyBackspace, 0))
	if paletteFind(m, "to markdown") != 0 {
		t.Fatal("backspace did not refilter")
	}
	if got := fuzzyIndices("é界", "aé世界"); !slices.Equal(got, []int{1, 3}) {
		t.Fatalf("rune indices: %v", got)
	}
}

func TestPaletteRunsCommand(t *testing.T) {
	m, _ := newPickerModel(t, "", false)
	m.openPalette()
	paletteQuery(m, "to markdown")
	m.handleKey(press(tea.KeyEnter, 0))
	if m.overlay != overlayNone || m.cur().kind != notebook.Markdown || !m.dirty {
		t.Fatal("conversion not run")
	}
	original := m.cur()
	m.openPalette()
	paletteQuery(m, "delete")
	m.handleKey(press(tea.KeyEnter, 0))
	if len(m.trash) != 1 || m.cur() == original {
		t.Fatal("delete must run at once")
	}
	m.openPalette()
	paletteQuery(m, "undo delete")
	m.handleKey(press(tea.KeyEnter, 0))
	if len(m.trash) != 0 || m.cur() != original {
		t.Fatal("undelete failed")
	}
}

func TestPaletteOpensOverlays(t *testing.T) {
	for q, want := range map[string]overlay{"color theme": overlayTheme, "help": overlayHelp, "quit": overlayQuit, "command palette": overlayPalette, "AI model / on-off": overlayModel} {
		m, _ := newPickerModel(t, "", false)
		m.dirty = true
		m.openPalette()
		paletteQuery(m, q)
		m.handleKey(press(tea.KeyEnter, 0))
		if m.overlay != want {
			t.Errorf("%q: overlay %v want %v", q, m.overlay, want)
		}
	}
}

func TestPaletteAvailability(t *testing.T) {
	m, _ := newPickerModel(t, "", false)
	m.openPalette()
	for _, s := range []string{"interrupt", "paste below", "undo delete", "input line", "fold output", "clear output", "copy output", "clear all outputs", "to code"} {
		if paletteFind(m, s) >= 0 {
			t.Errorf("unavailable: %s", s)
		}
	}
	m.commandKey(m.sel, "copy")
	m.openPalette()
	if paletteFind(m, "paste below") < 0 {
		t.Fatal("no paste after copy")
	}
	m.convertCell(0, false)
	m.openPalette()
	if paletteFind(m, "to code") < 0 || paletteFind(m, "to markdown") >= 0 || paletteFind(m, "format Go code") >= 0 {
		t.Fatal("markdown availability")
	}
	m.themes.fixed = true
	m.openPalette()
	if paletteFind(m, "color theme") >= 0 {
		t.Fatal("fixed theme must not offer picker")
	}
}

func TestPaletteAI(t *testing.T) {
	m := newInfoModel(t, nil, false, "x:=1")
	m.leaveEdit()
	m.openPalette()
	for _, c := range m.palette.all {
		if c.section == "AI" {
			t.Fatal("AI without support")
		}
	}
	if strings.Contains(screenText(m), "AI") {
		t.Fatal("AI rendered without support")
	}
	for _, on := range []bool{false, true} {
		m, _ := newPickerModel(t, "anthropic/claude-sonnet-5", on)
		m.openPalette()
		if paletteFind(m, "AI model / on-off") < 0 || (paletteFind(m, "ask AI to edit") >= 0) != on || paletteFind(m, "AI fix error") >= 0 {
			t.Fatal("AI availability")
		}
		m.cur().status = statusFailed
		m.openPalette()
		if (paletteFind(m, "AI fix error") >= 0) != on {
			t.Fatal("failed cell availability")
		}
	}
}

func TestPaletteVimToggle(t *testing.T) {
	m, _ := newPickerModel(t, "", false)
	m.openPalette()
	paletteQuery(m, "vim keys")
	if strings.Contains(screenText(m), "✓") {
		t.Fatal("vim initially checked")
	}
	m.handleKey(press(tea.KeyEnter, 0))
	m.openPalette()
	paletteQuery(m, "vim keys")
	if !m.vim.enabled || !strings.Contains(screenText(m), "✓") {
		t.Fatal("vim not checked")
	}
}

func TestPaletteCoversKeyMap(t *testing.T) {
	m, _ := newPickerModel(t, "anthropic/claude-sonnet-5", true)
	m.commandKey(0, "copy")
	m.deleteCell(0)
	m.cur().status = statusFailed
	m.cur().outputs = []notebook.Output{{Kind: notebook.Error, Text: "boom"}}
	// Queueing is sufficient to make interrupt available, without executing code.
	m.queue = []string{m.cur().id}
	m.running = &runState{cellID: "other"}
	m.openPalette()
	seen := map[string]bool{}
	for _, c := range m.palette.all {
		d := c.binding.Help().Desc
		if seen[d] {
			t.Errorf("duplicate %q", d)
		}
		seen[d] = true
	}
	excluded := map[string]bool{"up": true, "down": true, "page up": true, "page down": true, "command mode": true, "use widgets": true, "done (input)": true}
	for _, sec := range m.keys.baseHelp() {
		if sec.title == "Editing" {
			continue
		}
		for _, b := range sec.keys {
			d := b.Help().Desc
			// The mutually exclusive conversion is checked in a second state below.
			if d == "to code" {
				continue
			}
			if !excluded[d] && !seen[d] {
				t.Errorf("missing %q", d)
			}
		}
	}
	for _, b := range []string{"AI model / on-off", "AI fix error", "ask AI to edit"} {
		if !seen[b] {
			t.Errorf("missing %q", b)
		}
	}
	m.convertCell(0, false)
	m.openPalette()
	if paletteFind(m, "to code") < 0 {
		t.Fatal("missing to code")
	}
}

func TestPaletteMouse(t *testing.T) {
	m, _ := newPickerModel(t, "", false)
	m.openPalette()
	screenText(m)
	for _, z := range m.zones {
		if z.act.kind != actPaletteItem {
			t.Fatal("background zone interactive")
		}
	}
	z := m.zones[2]
	m.handleMouseMotion(tea.Mouse{X: z.rect.Min.X, Y: z.rect.Min.Y})
	if m.palette.idx != 2 {
		t.Fatal("hover")
	}
	m.handleWheel(tea.Mouse{Button: tea.MouseWheelDown})
	if m.palette.idx != 3 {
		t.Fatal("wheel")
	}
	paletteQuery(m, "to markdown")
	screenText(m)
	z = m.zones[0]
	m.handleMouseDown(tea.Mouse{X: z.rect.Min.X, Y: z.rect.Min.Y, Button: tea.MouseLeft})
	if m.overlay != overlayNone || m.cur().kind != notebook.Markdown {
		t.Fatal("click")
	}
	m.openPalette()
	screenText(m)
	pt := image.Pt(0, 0)
	m.handleMouseDown(tea.Mouse{X: pt.X, Y: pt.Y, Button: tea.MouseLeft})
	if m.overlay != overlayNone {
		t.Fatal("outside click")
	}
}

func TestPaletteScrolls(t *testing.T) {
	m, _ := newPickerModel(t, "", false)
	m.width, m.height = 40, 14
	m.openPalette()
	box, _ := m.renderPalette()
	height := lipgloss.Height(box)
	m.handleKey(press(tea.KeyPgDown, 0))
	for range 30 {
		m.handleKey(press(tea.KeyDown, 0))
	}
	screen := screenText(m)
	if m.palette.idx < m.palette.top || m.palette.idx >= m.palette.top+m.palette.visible {
		t.Fatal("selection offscreen")
	}
	if !strings.Contains(screen, itoa(m.palette.idx+1)+"/"+itoa(len(m.palette.rows))) {
		t.Fatal("counter")
	}
	paletteQuery(m, "restart kernel")
	box, _ = m.renderPalette()
	if lipgloss.Height(box) != height {
		t.Fatal("height changed with filter")
	}
	paletteQuery(m, "zzzzz")
	box, _ = m.renderPalette()
	if lipgloss.Height(box) != height {
		t.Fatal("height changed without matches")
	}
}

func TestPalettePaste(t *testing.T) {
	m, _ := newPickerModel(t, "", false)
	m.openPalette()
	m.Update(tea.PasteMsg{Content: "theme"})
	if m.palette.filter.Value() != "theme" || paletteFind(m, "color theme") != 0 {
		t.Fatal("paste")
	}
}

func TestPaletteHelp(t *testing.T) {
	m, _ := newPickerModel(t, "", false)
	m.overlay = overlayHelp
	s := screenText(m)
	if !strings.Contains(s, "^p/:") || !strings.Contains(s, "command palette") {
		t.Fatal("palette not in help")
	}
}

func TestPaletteConfiguredKeys(t *testing.T) {
	m, _ := newPickerModel(t, "", false)
	m.keys, _ = newKeyMapFrom(map[string][]string{"palette": {"ctrl+o"}, "top": {}, "to_markdown": {"alt+m"}})
	m.handleKey(press('p', tea.ModCtrl))
	if m.overlay != overlayNone {
		t.Fatal("old key still bound")
	}
	m.handleKey(press('o', tea.ModCtrl))
	if m.overlay != overlayPalette {
		t.Fatal("rebound key failed")
	}
	paletteQuery(m, "to markdown")
	if m.palette.rows[0].cmd.binding.Help().Key != "alt+m" {
		t.Fatal("configured key not shown")
	}
	m.runPalette(0)
	if m.cur().kind != notebook.Markdown {
		t.Fatal("configured command failed")
	}
	m.openPalette()
	paletteQuery(m, "first cell")
	if len(m.palette.rows) == 0 {
		t.Fatal("unbound command disappeared")
	}
	m.runPalette(0)
	if m.overlay != overlayNone {
		t.Fatal("unbound command did not run")
	}
}
