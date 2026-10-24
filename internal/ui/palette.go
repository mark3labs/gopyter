package ui

import (
	"image"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// paletteCmd shares bindings with help and execution paths with keyboard/mouse.
type paletteCmd struct {
	section string
	binding key.Binding
	run     func(*Model) tea.Cmd
	on      func(*Model) bool
}

func cmdOnly(desc string) key.Binding { return key.NewBinding(key.WithHelp("", desc)) }

func (m *Model) paletteCommands() []paletteCmd {
	k, c := m.keys, m.cur()
	var cmds []paletteCmd
	add := func(sec string, b key.Binding, name string, ok bool) {
		if ok {
			cmds = append(cmds, paletteCmd{section: sec, binding: b, run: func(m *Model) tea.Cmd { return m.commandKey(m.sel, name) }})
		}
	}
	act := func(sec string, b key.Binding, kind actionKind, ok bool) {
		if ok {
			cmds = append(cmds, paletteCmd{section: sec, binding: b, run: func(m *Model) tea.Cmd { return m.doAction(action{kind: kind, cell: m.sel}) }})
		}
	}
	cmds = append(cmds, paletteCmd{section: "Running", binding: k.RunAdvance, run: func(m *Model) tea.Cmd { return m.runSelected(true, false) }})
	act("Running", k.Run, actRunSelected, true)
	cmds = append(cmds, paletteCmd{section: "Running", binding: k.RunInsert, run: func(m *Model) tea.Cmd { return m.runSelected(false, true) }})
	act("Running", k.RunAll, actRunAll, true)
	act("Running", k.Interrupt, actInterrupt, m.busy())
	act("Running", k.Restart, actRestart, true)
	act("Running", k.RestartClear, actRestartClear, true)
	act("Running", k.Input, actFocusStdin, m.running != nil)
	add("Navigation", k.Top, "top", true)
	add("Navigation", k.Bottom, "bottom", true)
	act("Navigation", k.Edit, actEditCell, true)
	add("Cells", k.ExtendUp, "extend_up", true)
	add("Cells", k.ExtendDown, "extend_down", true)
	add("Cells", k.SelectAll, "select_all", true)
	add("Cells", k.Merge, "merge", true)
	add("Cells", k.InsertAbove, "insert_above", true)
	add("Cells", k.InsertBelow, "insert_below", true)
	act("Cells", k.Delete, actDeleteCell, true)
	add("Cells", k.Undelete, "undelete", len(m.undo) > 0)
	add("Cells", k.Cut, "cut", true)
	add("Cells", k.Copy, "copy", true)
	add("Cells", k.Paste, "paste", m.clipboard != nil)
	act("Cells", cmdOnly("duplicate cell"), actDuplicate, true)
	add("Cells", k.MoveUp, "move_up", true)
	add("Cells", k.MoveDown, "move_down", true)
	add("Misc", k.ToMarkdown, "to_markdown", c.kind != notebook.Markdown)
	add("Misc", k.ToCode, "to_code", c.kind != notebook.Code)
	add("Misc", k.ToggleOutput, "toggle_output", len(c.outputs) > 0)
	add("Misc", k.ClearOutput, "clear_output", len(c.outputs) > 0)
	add("Misc", k.ClearAllOutput, "clear_all_output", m.hasOutputs())
	add("Misc", k.Save, "save", true)
	act("Misc", k.SaveAs, actSaveAs, true)
	add("Misc", k.Sidebar, "sidebar", true)
	add("Misc", k.Theme, "theme", !m.themes.fixed)
	add("Misc", k.ToggleVim, "toggle_vim", true)
	cmds[len(cmds)-1].on = func(m *Model) bool { return m.vim.enabled }
	add("Misc", k.Palette, "palette", true)
	add("Misc", k.Help, "help", true)
	add("Misc", k.Quit, "quit", true)
	act("Cell", k.ToRaw, actToRaw, c.kind != notebook.Raw)
	act("Cell", k.ToggleInput, actToggleInput, c.kind != notebook.Markdown)
	act("Cell", k.ScrollLeft, actScrollOutLeft, c.hasTextOutput())
	act("Cell", k.ScrollRight, actScrollOutRight, c.hasTextOutput())
	act("Cell", k.ExtEdit, actExtEdit, m.extEdit.path == "")
	act("Cell", k.CopyOutput, actCopyOutput, len(c.outputs) > 0)
	act("Editing", cmdOnly("format Go code"), actFormatCell, c.kind == notebook.Code)
	add("AI", k.AIModel, "ai_model", m.ai != nil)
	add("AI", k.Fix, "fix", m.assistant != nil && m.fixable(c))
	add("AI", k.Ask, "ask", m.assistant != nil && m.editable(c))
	return cmds
}

type paletteState struct {
	all               []paletteCmd
	rows              []paletteHit
	idx, top, visible int
	filter            textinput.Model
	query             string
}

type paletteHit struct {
	cmd   paletteCmd
	score int
	match []int // rune indices, not byte offsets
}

func (m *Model) openPalette() tea.Cmd {
	m.closeCompletion()
	m.pendingKey = ""
	p := &m.palette
	p.all = m.paletteCommands()
	p.filter.SetValue("")
	p.query = "\x00"
	m.refilterPalette()
	m.overlay = overlayPalette
	return p.filter.Focus()
}

func (m *Model) closePalette() {
	m.overlay = overlayNone
	m.palette.filter.Blur()
}

func (m *Model) runPalette(i int) tea.Cmd {
	if i < 0 || i >= len(m.palette.rows) {
		return nil
	}
	run := m.palette.rows[i].cmd.run
	// Close first: commands may open another overlay, including this one.
	m.closePalette()
	return run(m)
}

func (m *Model) movePalette(d int) {
	p := &m.palette
	if len(p.rows) == 0 {
		return
	}
	p.idx = clamp(p.idx+d, 0, len(p.rows)-1)
	if p.visible > 0 {
		p.top = clamp(p.top, max(p.idx-p.visible+1, 0), p.idx)
	}
}

func (m *Model) refilterPalette() {
	p := &m.palette
	q := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	if q == p.query {
		return
	}
	p.query, p.idx, p.top = q, 0, 0
	p.rows = nil
	for _, c := range p.all {
		if q == "" {
			p.rows = append(p.rows, paletteHit{cmd: c})
			continue
		}
		if score, match := scorePalette(q, c); score > 0 {
			p.rows = append(p.rows, paletteHit{c, score, match})
		}
	}
	slices.SortStableFunc(p.rows, func(a, b paletteHit) int { return b.score - a.score })
}

func scorePalette(q string, c paletteCmd) (int, []int) {
	desc := strings.ToLower(c.binding.Help().Desc)
	score := 0
	switch {
	case desc == q:
		score = 1000
	case strings.HasPrefix(desc, q):
		score = 900
	default:
		for _, word := range strings.Fields(desc) {
			if strings.HasPrefix(word, q) {
				score = 800
				break
			}
		}
		if score == 0 && strings.Contains(desc, q) {
			score = 600
		}
	}
	if score == 0 {
		score = min(fuzzyScore(q, desc), 599)
	}
	if score > 0 {
		return score, fuzzyIndices(q, desc)
	}
	for _, k := range append(slices.Clone(c.binding.Keys()), c.binding.Help().Key) {
		if q == strings.ToLower(k) {
			return 700, nil
		}
	}
	if strings.Contains(strings.ToLower(c.section), q) {
		return 300, nil
	}
	return 0, nil
}

func fuzzyIndices(q, s string) []int {
	qr := []rune(q)
	if len(qr) == 0 {
		return nil
	}
	var indices []int
	for i, r := range []rune(s) {
		if r == qr[len(indices)] {
			indices = append(indices, i)
			if len(indices) == len(qr) {
				return indices
			}
		}
	}
	return nil
}

func (m *Model) handlePaletteKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &m.palette
	page := max(p.visible-1, 1)
	switch msg.String() {
	case "up", "ctrl+p", "shift+tab":
		m.movePalette(-1)
	case "down", "ctrl+n", "tab":
		m.movePalette(1)
	case "pgup":
		m.movePalette(-page)
	case "pgdown":
		m.movePalette(page)
	case "enter":
		return m.runPalette(p.idx)
	case "esc", "ctrl+c":
		m.closePalette()
	default:
		var cmd tea.Cmd
		p.filter, cmd = p.filter.Update(msg)
		m.refilterPalette()
		return cmd
	}
	return nil
}

func (m *Model) renderPalette() (string, []zone) {
	t, p := m.theme, &m.palette
	width := clamp(m.width-2-2*dlgPadX-4, 30, 72)
	head := lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render("◆ Commands")
	n := 0
	if len(p.rows) > 0 {
		n = p.idx + 1
	}
	head += t.muted.Render("    " + itoa(n) + "/" + itoa(len(p.rows)))
	p.filter.SetWidth(width - 2)
	lines := []string{head, "", p.filter.View(), ""}
	p.visible = clamp(m.height-2-2*dlgPadY-6, 3, 16)
	p.top = clamp(p.top, max(p.idx-p.visible+1, 0), p.idx)
	p.top = clamp(p.top, 0, max(len(p.rows)-p.visible, 0))
	// Size the section and key columns over all rows so they line up, and
	// reserve them before truncating descriptions on narrow terminals.
	keyW, secW := 0, 0
	for _, hit := range p.rows {
		keyW = max(keyW, lipgloss.Width(hit.cmd.binding.Help().Key))
		secW = max(secW, lipgloss.Width(hit.cmd.section))
	}
	keyW, secW = min(keyW, width/3), min(secW, max(width/4-1, 0))
	descW := width - 3 - keyW - secW - 2
	var zones []zone
	for row := 0; row < p.visible; row++ {
		i := p.top + row
		if i >= len(p.rows) {
			text := ""
			if row == 0 {
				text = t.muted.Render("no match")
			}
			lines = append(lines, padRight(text, width))
			continue
		}
		hit := p.rows[i]
		h := hit.cmd.binding.Help()
		base := t.text
		if i == p.idx {
			base = t.btnHover
		}
		mark := "  "
		if hit.cmd.on != nil && hit.cmd.on(m) {
			mark = "✓ "
		}
		desc := lipgloss.StyleRunes(h.Desc, hit.match, base.Underline(true), base)
		text := base.Render(mark) + padRight(ansi.Truncate(desc, descW, "…"), descW) + base.Render(" ") + t.muted.Render(padRight(ansi.Truncate(hit.cmd.section, secW, "…"), secW)+"  "+padLeft(ansi.Truncate(h.Key, keyW, "…"), keyW))
		text = base.Render(padRight(text, width))
		y := 1 + dlgPadY + len(lines)
		zones = append(zones, zone{rect: image.Rect(1+dlgPadX, y, 1+dlgPadX+width, y+1), act: action{kind: actPaletteItem, cell: i}})
		lines = append(lines, text)
	}
	hint := t.helpKey.Render("↑↓") + t.helpDesc.Render(" move  ")
	if len(p.rows) > 0 {
		hint += t.helpKey.Render("enter") + t.helpDesc.Render(" "+p.rows[p.idx].cmd.binding.Help().Desc+"  ")
	}
	hint += t.helpKey.Render("esc") + t.helpDesc.Render(" close")
	lines = append(lines, "", ansi.Truncate(hint, width, "…"))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colPrimary).Padding(dlgPadY, dlgPadX)
	return box.Render(lipgloss.JoinVertical(lipgloss.Left, lines...)), zones
}
