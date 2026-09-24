package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/notebook"
)

const (
	headerHeight = 2
	footerHeight = 1
	gutterWidth  = 9 // selection bar + execution label
)

func (m *Model) viewHeight() int { return max(m.height-headerHeight-footerHeight, 1) }

func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	v.WindowTitle = "gopyter · " + m.displayName()
	if m.quitting || m.width == 0 || m.height == 0 {
		return v
	}

	screen, cursor := m.renderScreen()
	// Hover depends on the zones produced by rendering. If content moved
	// under a stationary pointer (scrolling, clicks, output), re-derive the
	// hover state and render once more so highlights and tooltips are exact.
	if m.pointer.seen {
		act, tip, cell := m.hoverAt(m.pointer.x, m.pointer.y)
		if act != m.hover || cell != m.hoverCell || tip != m.hoverTip {
			m.hover, m.hoverTip, m.hoverCell = act, tip, cell
			screen, cursor = m.renderScreen()
		}
	}
	v.SetContent(screen)
	v.Cursor = cursor
	return v
}

// renderScreen renders the full screen, rebuilding the clickable zones.
func (m *Model) renderScreen() (string, *tea.Cursor) {
	m.zones = m.zones[:0]
	body, cursor := m.renderBody()
	// The info popup is laid out first: the footer hints depend on it.
	var info string
	var ix, iy int
	if m.infoVisible() && cursor != nil {
		info, ix, iy = m.renderInfo(cursor.X, cursor.Y)
	}
	screen := strings.Join([]string{m.renderHeader(), body, m.renderFooter()}, "\n")
	if info != "" {
		screen = m.composite(screen, info, ix, iy, false)
	}

	if m.comp.open && m.overlay == overlayNone && m.mode == modeEdit && cursor != nil {
		layer, x, y, zones := m.renderCompletion(cursor.X, cursor.Y)
		screen = m.composite(screen, layer, x, y, false)
		// The popup sits above everything else for hit-testing.
		m.zones = append(m.zones, translate(zones, x, y)...)
	}

	if m.overlay != overlayNone {
		layer, zones := m.renderOverlay()
		lw, lh := lipgloss.Size(layer)
		x, y := max((m.width-lw)/2, 0), max((m.height-lh)/2, 0)
		if m.overlay == overlayMenu {
			x = clamp(m.menu.x, 0, max(m.width-lw, 0))
			y = m.menu.y + 1
			if y+lh > m.height {
				y = max(m.menu.y-lh, 0)
			}
		}
		m.overlayRect = image.Rect(x, y, x+lw, y+lh)
		screen = m.composite(screen, layer, x, y, m.overlay != overlayMenu)
		// Only the overlay is interactive while it's open.
		m.zones = translate(zones, x, y)
		cursor = nil
	}
	return screen, cursor
}

func (m *Model) displayName() string {
	if m.path == "" {
		return "untitled.ipynb"
	}
	return m.path
}

func (m *Model) renderHeader() string {
	t := m.theme
	logoText := " ◆ gopyter "
	runes := []rune(logoText)
	colors := lipgloss.Blend1D(len(runes), colPrimary, colAccent)
	var logo strings.Builder
	for i, r := range runes {
		logo.WriteString(lipgloss.NewStyle().Background(colors[i]).Foreground(colInk).Bold(true).Render(string(r)))
	}

	name := t.text.Bold(true).Render(m.displayName())
	if m.path == "" {
		name = t.muted.Italic(true).Render(m.displayName())
	}
	if m.dirty {
		name += lipgloss.NewStyle().Foreground(colWarning).Render(" ●")
	}
	left := logo.String() + "  " + name

	var state string
	switch {
	case m.running != nil:
		q := ""
		if n := len(m.queue); n > 0 {
			q = fmt.Sprintf(" +%d", n)
		}
		state = m.spinner.View() + t.statusRun.Render(" busy"+q)
	default:
		state = t.statusOK.Render("●") + t.dim.Render(" idle")
	}
	right := t.muted.Render(m.k.GoVersion()) + t.subtle.Render("  │  ") + state + " "
	if cs, ok := m.completer.(completerStatus); ok {
		label := t.muted.Render("gopls")
		if gopls, reason := cs.Status(); !gopls {
			label = t.subtle.Render("basic completion")
			if reason == "gopls starting" {
				label = t.subtle.Render("gopls…")
			}
		}
		right = label + t.subtle.Render("  │  ") + right
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	line := left + strings.Repeat(" ", max(gap, 1)) + right
	return ansi.Truncate(line, m.width, "") + "\n" + m.renderToolbar()
}

type toolItem struct {
	act         action
	full, short string
	tip         string
	enabled     bool
	sep         bool
}

// renderToolbar renders clickable buttons embedded in the header rule.
func (m *Model) renderToolbar() string {
	t := m.theme
	rule := lipgloss.NewStyle().Foreground(colFaint)
	items := []toolItem{
		{act: action{kind: actRunSelected}, full: "▶ run", short: "▶", tip: "run the selected cell · shift+enter / ctrl+r", enabled: true},
		{act: action{kind: actRunAll}, full: "▶▶ run all", short: "▶▶", tip: "run all cells · A", enabled: true},
		{act: action{kind: actInterrupt}, full: "■ stop", short: "■", tip: "interrupt execution · ctrl+c", enabled: m.busy()},
		{act: action{kind: actRestart}, full: "↻ restart", short: "↻", tip: "restart the kernel, clearing all declarations · R", enabled: true},
		{sep: true},
		{act: action{kind: actAddCode, cell: m.sel + 1}, full: "+ code", short: "+go", tip: "insert a code cell below · b", enabled: true},
		{act: action{kind: actAddMarkdown, cell: m.sel + 1}, full: "+ markdown", short: "+md", tip: "insert a markdown cell below", enabled: true},
		{sep: true},
		{act: action{kind: actSave}, full: "save", short: "save", tip: "save the notebook · ctrl+s", enabled: true},
		{act: action{kind: actTheme}, full: "◐ theme", short: "◐", tip: "change the color theme · T", enabled: true},
		{act: action{kind: actHelp}, full: "? help", short: "?", tip: "keyboard shortcuts · ?", enabled: true},
	}
	build := func(short bool) *lineBuilder {
		lb := &lineBuilder{}
		lb.add(rule.Render("──"))
		for _, it := range items {
			if it.sep {
				lb.add(rule.Render("──┼─"))
				continue
			}
			label := " " + it.full + " "
			if short {
				label = " " + it.short + " "
			}
			switch {
			case !it.enabled:
				lb.add(t.btnDisabled.Render(label))
			case m.hovered(it.act):
				lb.button(it.act, it.tip, t.btnHover.Render(label))
			default:
				lb.button(it.act, it.tip, t.btn.Render(label))
			}
			lb.add(rule.Render("─"))
		}
		return lb
	}
	lb := build(false)
	if lb.x > m.width {
		lb = build(true)
	}
	if lb.x > m.width {
		return rule.Render(strings.Repeat("─", m.width))
	}
	lb.add(rule.Render(strings.Repeat("─", m.width-lb.x)))
	m.zones = append(m.zones, translate(lb.zones, 0, 1)...)
	return lb.String()
}

func (m *Model) renderFooter() string {
	t := m.theme
	pill := t.modeCmd.Render("COMMAND")
	bindings := m.keys.commandShort(m.cur().kind, m.fixable(m.cur()))
	tip := "switch to edit mode · enter"
	if m.mode == modeEdit {
		pill = t.modeEdit.Render("EDIT")
		bindings = m.keys.editShort()
		tip = "switch to command mode · esc"
		if m.vim.enabled {
			st := t.modeEdit
			switch m.vim.mode {
			case vimNormal:
				bindings = m.keys.vimNormalShort()
			case vimInsert:
				st = t.modeInsert
				bindings = m.keys.vimInsertShort()
			case vimVisual, vimVisualLine:
				st = t.modeVisual
				bindings = m.keys.vimVisualShort()
			}
			pill = st.Render(m.vim.mode.String())
		}
	}
	toggle := action{kind: actToggleMode}
	if m.hovered(toggle) {
		pill = t.btnHover.Render(ansi.Strip(pill))
	}
	m.zones = append(m.zones, zone{rect: image.Rect(0, m.height-1, lipgloss.Width(pill), m.height), act: toggle, tip: tip})
	pos := t.muted.Render(fmt.Sprintf("cell %d/%d ", m.sel+1, len(m.cells)))
	avail := m.width - lipgloss.Width(pill) - lipgloss.Width(pos) - 5

	var mid string
	if m.status != "" {
		st := t.dim
		icon := "•"
		switch m.statusKind {
		case statusSuccess:
			st, icon = t.statusOK, "✓"
		case statusError:
			st, icon = t.statusErr, "✗"
		}
		mid = st.Render(icon + " " + m.status)
	} else if m.fix.active {
		mid = m.spinner.View() + lipgloss.NewStyle().Foreground(colPrimary).Render(" ✦ fixing "+m.fix.name) +
			t.dim.Render(" · "+m.fix.progress+" · ") + t.helpKey.Render("esc") + t.helpDesc.Render(" cancel")
	} else if m.infoVisible() && !m.info.loading {
		mid = t.helpKey.Render("esc") + t.helpDesc.Render(" dismiss")
		if len(m.info.lines) > m.info.rows {
			mid = t.helpKey.Render("pgup/pgdn") + t.helpDesc.Render(" scroll · ") + mid
		}
	} else if m.comp.open && len(m.comp.items) > 0 {
		mid = t.helpKey.Render("↑↓") + t.helpDesc.Render(" select · ") +
			t.helpKey.Render("tab/↵") + t.helpDesc.Render(" accept · ") +
			t.helpKey.Render("esc") + t.helpDesc.Render(" dismiss")
	} else if m.hoverTip != "" {
		mid = lipgloss.NewStyle().Foreground(colInfo).Render("› " + m.hoverTip)
	} else if m.pendingKey != "" {
		mid = t.dim.Render(m.pendingKey + "…")
	} else if p := m.vim.pending(); p != "" && m.vimActive() {
		mid = t.dim.Render(p + "…")
	} else {
		m.help.SetWidth(avail)
		mid = m.help.ShortHelpView(bindings)
	}
	mid = ansi.Truncate(mid, max(avail, 0), "…")
	gap := m.width - lipgloss.Width(pill) - lipgloss.Width(mid) - lipgloss.Width(pos) - 2
	return pill + "  " + mid + strings.Repeat(" ", max(gap, 0)) + pos
}

// renderBody renders the scrollable list of cells and computes the cursor.
func (m *Model) renderBody() (string, *tea.Cursor) {
	w := m.width - 1 // leave room for the scrollbar
	vh := m.viewHeight()

	var all []string
	m.layout = m.layout[:0]
	for i := range m.cells {
		r := m.renderCell(i, w)
		m.layout = append(m.layout, cellLayout{top: len(all), height: len(r.lines), render: r})
		all = append(all, r.lines...)
		if !r.ownSpacer {
			all = append(all, "")
		}
	}

	// "Add cell" buttons at the end of the notebook.
	addTop := len(all)
	add := &lineBuilder{}
	add.add(strings.Repeat(" ", gutterWidth))
	for _, b := range []struct {
		act   action
		label string
		tip   string
	}{
		{action{kind: actAddCode, cell: len(m.cells)}, "+ code", "append a code cell"},
		{action{kind: actAddMarkdown, cell: len(m.cells)}, "+ markdown", "append a markdown cell"},
	} {
		st := m.theme.link
		if m.hovered(b.act) {
			st = m.theme.linkHover
		}
		add.button(b.act, b.tip, st.Render(b.label))
		add.add("   ")
	}
	all = append(all, add.String())
	m.contentLines = len(all)

	// Keep the focus visible.
	if m.follow && m.sel < len(m.layout) {
		l := m.layout[m.sel]
		top, bottom := l.top, l.top+l.height
		if m.mode == modeEdit && l.render.hasEdit {
			cy := l.top + l.render.edTop + l.render.ev.curY
			top, bottom = max(cy-2, l.top), cy+3
			if cy+3 > l.top+l.height {
				bottom = l.top + l.height + 1
			}
		}
		if bottom-m.offset > vh {
			m.offset = bottom - vh
		}
		if top < m.offset {
			m.offset = top
		}
	}
	m.offset = clamp(m.offset, 0, max(len(all)-vh, 0))

	// Register the clickable zones that are visible.
	visible := func(zs []zone, top int) {
		for _, z := range translate(zs, 0, headerHeight+top-m.offset) {
			if z.rect.Min.Y >= headerHeight && z.rect.Min.Y < headerHeight+vh {
				z.rect.Max.X = min(z.rect.Max.X, w)
				m.zones = append(m.zones, z)
			}
		}
	}
	for _, l := range m.layout {
		visible(l.render.zones, l.top)
	}
	visible(add.zones, addTop)

	// Scrollbar geometry.
	thumbStart, thumbLen := -1, 0
	if len(all) > vh {
		thumbLen = max(vh*vh/len(all), 1)
		thumbStart = m.offset * (vh - thumbLen) / max(len(all)-vh, 1)
	}
	track := lipgloss.NewStyle().Foreground(colFaint).Render("│")
	thumb := lipgloss.NewStyle().Foreground(colMuted).Render("┃")

	out := make([]string, vh)
	for y := range vh {
		line := ""
		if i := m.offset + y; i < len(all) {
			line = all[i]
		}
		line = padRight(ansi.Truncate(line, w, ""), w)
		switch {
		case thumbStart < 0:
			line += " "
		case y >= thumbStart && y < thumbStart+thumbLen:
			line += thumb
		default:
			line += track
		}
		out[y] = line
	}

	var cursor *tea.Cursor
	if m.mode == modeEdit && m.sel < len(m.layout) {
		l := m.layout[m.sel]
		if l.render.hasEdit {
			y := l.top + l.render.edTop + l.render.ev.curY - m.offset
			if y >= 0 && y < vh {
				cursor = tea.NewCursor(l.render.edLeft+l.render.ev.curX, y+headerHeight)
				cursor.Shape = tea.CursorBar
				if m.vimActive() {
					cursor.Shape = tea.CursorBlock
				}
				cursor.Color = colSuccess
			}
		}
	}
	return strings.Join(out, "\n"), cursor
}

func (m *Model) markdownRenderer(width int) *glamour.TermRenderer {
	if m.md == nil || m.mdWidth != width {
		r, err := glamour.NewTermRenderer(glamour.WithStyles(m.mdStyle), glamour.WithWordWrap(width))
		if err != nil {
			return nil
		}
		m.md, m.mdWidth = r, width
	}
	return m.md
}

func (m *Model) renderMarkdown(src string, width int) []string {
	r := m.markdownRenderer(width)
	if r == nil {
		return strings.Split(src, "\n")
	}
	out, err := r.Render(src)
	if err != nil {
		return strings.Split(src, "\n")
	}
	lines := strings.Split(out, "\n")
	// Trim blank leading/trailing lines added by glamour.
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func (m *Model) renderCell(i, width int) cellRender {
	t := m.theme
	c := m.cells[i]
	selected := i == m.sel
	editing := selected && m.mode == modeEdit

	borderColor := t.borderIdle
	switch {
	case editing:
		borderColor = t.borderEdit
	case selected:
		borderColor = t.borderCmd
	case c.status == statusRunning:
		borderColor = t.borderRun
	case i == m.hoverCell:
		borderColor = colMuted
	}
	border := lipgloss.NewStyle().Foreground(borderColor)

	bar := " "
	if selected {
		bar = border.Render("▌")
	}
	blank := strings.Repeat(" ", gutterWidth-1)
	boxW := width - gutterWidth
	innerW := max(boxW-4, 8)

	r := cellRender{edTop: -1}

	// Rendered markdown (not editing).
	if c.kind == notebook.Markdown && !editing {
		src := c.ed.Value()
		mdW := boxW - 2
		if c.mdOut == "" || c.mdSrc != src || c.mdWidth != mdW {
			c.mdSrc, c.mdWidth = src, mdW
			if strings.TrimSpace(src) == "" {
				c.mdOut = t.muted.Italic(true).Render("  empty markdown cell · press enter to edit")
			} else {
				c.mdOut = strings.Join(m.renderMarkdown(src, mdW), "\n")
			}
		}
		for l := range strings.SplitSeq(c.mdOut, "\n") {
			r.lines = append(r.lines, bar+blank+l)
		}
		// The spacer line below a rendered markdown cell hosts its action
		// buttons when selected or hovered, so the layout never shifts.
		r.ownSpacer = true
		if selected || i == m.hoverCell {
			probe := &lineBuilder{}
			m.cellActions(probe, i, lipgloss.NewStyle(), true)
			lb := &lineBuilder{}
			lb.add(bar + blank + strings.Repeat(" ", max(width-gutterWidth-probe.x, 0)))
			m.cellActions(lb, i, lipgloss.NewStyle(), true)
			r.zones = append(r.zones, translate(lb.zones, 0, len(r.lines))...)
			r.lines = append(r.lines, lb.String())
		} else {
			r.lines = append(r.lines, "")
		}
		return r
	}

	// Execution label; it doubles as a run/stop button.
	label := ""
	labelAct := action{kind: actRunCell, cell: i}
	labelTip := "run cell"
	if c.kind == notebook.Code {
		isRunning := m.running != nil && m.running.cellID == c.id
		if isRunning {
			labelAct, labelTip = action{kind: actInterrupt}, "stop execution"
		}
		switch {
		case m.hovered(labelAct) && m.hoverCell == i && isRunning:
			label = lipgloss.NewStyle().Foreground(colOrange).Bold(true).Render("[■]")
		case m.hovered(labelAct) && m.hoverCell == i:
			label = lipgloss.NewStyle().Foreground(colSuccess).Bold(true).Render("[▶]")
		case c.status == statusRunning:
			label = t.statusRun.Render("[") + m.spinner.View() + t.statusRun.Render("]")
		case c.status == statusQueued:
			label = t.statusRun.Render("[*]")
		default:
			n := " "
			if c.count > 0 {
				n = itoa(c.count)
			}
			label = t.inLabel.Render("[" + n + "]")
		}
	}
	labelW := lipgloss.Width(label)
	label = padLeft(label, gutterWidth-2) + " "

	// Top border with language title and status badge.
	// The type label doubles as a code ⇄ markdown toggle.
	convAct, convTip, convTo := m.convertAction(i)
	title := " " + t.muted.Render(langTitle(c.kind)) + " "
	switch {
	case m.hovered(convAct):
		title = t.btnHover.Render(" " + langTitle(c.kind) + " → " + convTo + " ")
	case editing:
		title = " " + lipgloss.NewStyle().Foreground(colSuccess).Render(langTitle(c.kind)) + " "
	}
	// boxTop places the title right after "╭─".
	titleX := gutterWidth + 2
	r.zones = append(r.zones, zone{
		rect: image.Rect(titleX, len(r.lines), titleX+lipgloss.Width(title), len(r.lines)+1),
		act:  convAct, tip: convTip,
	})
	badge := ""
	switch c.status {
	case statusRunning:
		badge = t.statusRun.Render("running " + fmtDuration(time.Since(c.started).Truncate(100*time.Millisecond)))
	case statusQueued:
		badge = t.statusRun.Render("queued")
	case statusOK:
		if c.duration > 0 {
			badge = t.statusOK.Render("✓ ") + t.muted.Render(fmtDuration(c.duration))
		}
	case statusFailed:
		badge = t.statusErr.Render("✗ " + c.errMsg)
	case statusInterrupted:
		badge = lipgloss.NewStyle().Foreground(colOrange).Render("■ interrupted")
	}
	r.lines = append(r.lines, bar+blank+boxTop(boxW, border, title, badge))

	ev := c.ed.Render(innerW, editing, m.hl, t)
	r.ev, r.hasEdit = ev, true
	r.edTop = len(r.lines)
	if c.kind == notebook.Code && labelW > 0 {
		x1 := gutterWidth - 2
		r.zones = append(r.zones, zone{rect: image.Rect(x1-labelW, r.edTop, x1, r.edTop+1), act: labelAct, tip: labelTip})
	}
	r.edLeft = gutterWidth + 2
	side := border.Render("│")
	for j, l := range ev.lines {
		lbl := blank
		if j == 0 {
			lbl = label
		}
		r.lines = append(r.lines, bar+lbl+side+" "+padRight(l, innerW)+" "+side)
	}
	if c.ed.Value() == "" && !editing {
		// Placeholder hint on empty cells.
		idx := r.edTop
		hint := t.subtle.Italic(true).Render("Go code… (enter to edit, shift+enter to run)")
		r.lines[idx] = bar + label + side + " " + padRight(ev.lines[0]+hint, innerW) + " " + side
	}
	if selected || i == m.hoverCell {
		// Bottom border with action buttons.
		lb := &lineBuilder{}
		lb.add(bar + blank)
		probe := &lineBuilder{}
		m.cellActions(probe, i, border, false)
		fill := boxW - 2 - probe.x - 1
		if fill >= 1 {
			lb.add(border.Render("╰" + strings.Repeat("─", fill)))
			m.cellActions(lb, i, border, false)
			lb.add(border.Render("─╯"))
			r.zones = append(r.zones, translate(lb.zones, 0, len(r.lines))...)
			r.lines = append(r.lines, lb.String())
		} else {
			r.lines = append(r.lines, bar+blank+boxBottom(boxW, border))
		}
	} else {
		r.lines = append(r.lines, bar+blank+boxBottom(boxW, border))
	}

	// Outputs.
	if c.kind == notebook.Code && len(c.outputs) > 0 {
		if key := c.outputFingerprint(boxW - 2); key != c.outKey || c.outLines == nil {
			c.outLines, c.outResultAt = m.renderOutputs(c, boxW-2)
			c.outKey = key
		}
		out, resultAt := c.outLines, c.outResultAt
		hidden := 0
		if !c.expanded && len(out) > maxOutputLines {
			hidden = len(out) - maxOutputLines
			out = out[hidden:]
			resultAt -= hidden
		}
		foldAct := action{kind: actToggleOutput, cell: i}
		foldStyle := t.muted
		if m.hovered(foldAct) {
			foldStyle = t.linkHover
		}
		foldAt := -1
		if hidden > 0 {
			out = append([]string{foldStyle.Render(fmt.Sprintf("⋯ %d earlier lines hidden · click or o to expand", hidden))}, out...)
			resultAt++
			foldAt = 0
		} else if c.expanded && len(out) > maxOutputLines {
			out = append(out, foldStyle.Render("⋯ click or o to collapse"))
			foldAt = len(out) - 1
		}
		if foldAt >= 0 {
			y := len(r.lines) + foldAt
			x0 := gutterWidth + 2
			r.zones = append(r.zones, zone{rect: image.Rect(x0, y, x0+lipgloss.Width(out[foldAt]), y+1), act: foldAct, tip: "toggle full output"})
		}
		for j, l := range out {
			lbl := blank
			if j == resultAt {
				lbl = padLeft(t.resultLabel.Render(fmt.Sprintf("Out[%d]", c.count)), gutterWidth-2) + " "
			}
			r.lines = append(r.lines, bar+lbl+"  "+l)
		}
	}
	return r
}

// convertAction returns the action converting cell i to the other type,
// its tooltip, and the target type's name.
func (m *Model) convertAction(i int) (action, string, string) {
	if m.cells[i].kind == notebook.Code {
		return action{kind: actToMarkdown, cell: i}, "convert to markdown · m", "markdown"
	}
	return action{kind: actToCode, cell: i}, "convert to go code · y", "go"
}

// cellActions appends the per-cell action buttons.
func (m *Model) cellActions(lb *lineBuilder, i int, frame lipgloss.Style, markdown bool) {
	t := m.theme
	c := m.cells[i]
	type btn struct {
		act    action
		icon   string
		tip    string
		danger bool
	}
	run := btn{action{kind: actRunCell, cell: i}, "▶", "run cell · ctrl+enter", false}
	switch {
	case markdown:
		run = btn{action{kind: actEditCell, cell: i}, "✎", "edit cell · enter", false}
	case c.kind == notebook.Markdown:
		run.tip = "render markdown · ctrl+enter"
	case m.running != nil && m.running.cellID == c.id:
		run = btn{action{kind: actInterrupt}, "■", "stop execution · ctrl+c", false}
	}
	convAct, convTip, convTo := m.convertAction(i)
	convIcon := "md"
	if convTo == "go" {
		convIcon = "go"
	}
	btns := []btn{
		run,
		{convAct, "⇄" + convIcon, convTip, false},
		{action{kind: actMoveUp, cell: i}, "↑", "move cell up · K", false},
		{action{kind: actMoveDown, cell: i}, "↓", "move cell down · J", false},
		{action{kind: actDuplicate, cell: i}, "⧉", "duplicate cell", false},
		{action{kind: actDeleteCell, cell: i}, "✕", "delete cell · dd", true},
	}
	if !markdown && m.fixable(c) {
		btns = append([]btn{{action{kind: actFixCell, cell: i}, "✦ fix", "fix the error with AI (" + m.fixer.Model() + ") · f", false}}, btns...)
	}
	sep := frame.Render("─")
	if markdown {
		sep = " "
	}
	for j, b := range btns {
		if j > 0 {
			lb.add(sep)
		}
		label := " " + b.icon + " "
		switch {
		case m.hovered(b.act) && b.danger:
			lb.button(b.act, b.tip, t.btnDanger.Render(label))
		case m.hovered(b.act):
			lb.button(b.act, b.tip, t.btnHover.Render(label))
		default:
			lb.button(b.act, b.tip, t.btn.Render(label))
		}
	}
}

func langTitle(kind notebook.CellType) string {
	switch kind {
	case notebook.Markdown:
		return "markdown"
	case notebook.Raw:
		return "raw"
	}
	return "go"
}

// renderOutputs renders the outputs of a cell, also returning the index of
// the first result line (or -1).
func (m *Model) renderOutputs(c *Cell, width int) ([]string, int) {
	t := m.theme
	var out []string
	resultAt := -1
	for _, o := range c.outputs {
		if o.Kind == notebook.Result && resultAt < 0 {
			resultAt = len(out)
		}
		switch o.Kind {
		case notebook.Stdout:
			for _, l := range wrapLines(normalizeOutput(o.Text), width) {
				out = append(out, t.stdout.Render(l))
			}
		case notebook.Stderr:
			for _, l := range wrapLines(normalizeOutput(o.Text), width) {
				out = append(out, t.stderr.Render(l))
			}
		case notebook.Result:
			for _, l := range wrapLines(normalizeOutput(o.Text), width) {
				out = append(out, t.result.Render(l))
			}
		case notebook.MarkdownOut:
			out = append(out, m.renderMarkdown(o.Text, width)...)
		case notebook.Info:
			for _, l := range wrapLines(normalizeOutput(o.Text), width-2) {
				out = append(out, t.info.Render("› "+l))
			}
		case notebook.Error:
			for _, l := range wrapLines(normalizeOutput(o.Text), width-2) {
				out = append(out, t.errorBar.Render("┃ ")+t.errorText.Render(l))
			}
		}
	}
	return out, resultAt
}

// Dialog box geometry: border (1) + padding (1 vertical, 3 horizontal).
const dlgPadX, dlgPadY = 3, 1

// renderOverlay renders the active dialog or menu and its clickable zones
// (relative to the overlay's top-left corner).
func (m *Model) renderOverlay() (string, []zone) {
	t := m.theme
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colPrimary).
		Padding(dlgPadY, dlgPadX)
	title := func(s string) string {
		return lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render(s)
	}
	// dialog lays out rows and a final row of buttons.
	// dialog lays out rows, a row with the dialog's buttons, and a hint.
	// The keyboard-focused button is drawn solid; mouse hover only lifts
	// the others slightly, so there's never any doubt what enter will do.
	dialog := func(border color.Color, rows []string) (string, []zone) {
		lb := &lineBuilder{}
		for j, b := range m.dialogButtons() {
			if j > 0 {
				lb.add("  ")
			}
			st := t.dlgBtn
			switch {
			case j == m.dlgFocus && b.danger:
				st = t.dlgDanger
			case j == m.dlgFocus:
				st = t.dlgFocus
			case m.hovered(b.act):
				st = t.dlgBtnHover
			}
			lb.button(b.act, b.tip, st.Render(b.label))
		}
		rows = append(rows, "", lb.String())
		zones := translate(lb.zones, 1+dlgPadX, 1+dlgPadY+len(rows)-1)
		hint := t.helpKey.Render("tab") + t.helpDesc.Render(" move  ") +
			t.helpKey.Render("enter") + t.helpDesc.Render(" select  ") +
			t.helpKey.Render("esc") + t.helpDesc.Render(" cancel")
		rows = append(rows, "", hint)
		return box.BorderForeground(border).Render(lipgloss.JoinVertical(lipgloss.Left, rows...)), zones
	}

	switch m.overlay {
	case overlayHelp:
		var cols []string
		var grid string
		for _, sec := range m.keys.fullHelp(m.vim.enabled, m.ai != nil, m.fixer != nil) {
			var rows []string
			rows = append(rows, lipgloss.NewStyle().Foreground(colWarning).Bold(true).Render(sec.title), "")
			for _, b := range sec.keys {
				h := b.Help()
				rows = append(rows, t.helpKey.Width(8).Render(h.Key)+t.helpDesc.Render(h.Desc))
			}
			cols = append(cols, lipgloss.NewStyle().Width(26).Render(strings.Join(rows, "\n")))
		}
		// Lay the sections out in as many columns as fit.
		perRow := clamp((m.width-10)/26, 1, len(cols))
		var rowsOut []string
		for i := 0; i < len(cols); i += perRow {
			if i > 0 {
				rowsOut = append(rowsOut, "")
			}
			rowsOut = append(rowsOut, lipgloss.JoinHorizontal(lipgloss.Top, cols[i:min(i+perRow, len(cols))]...))
		}
		grid = lipgloss.JoinVertical(lipgloss.Left, rowsOut...)
		tips := t.muted.Render("Top-level func/type/var/const/import declarations persist across cells.\n" +
			"Other statements run in main(); a trailing expression is displayed.\n" +
			"Use !cmd for shell commands (e.g. !go get …) and %help for magics.\n" +
			"Mouse: click to select/edit, drag to select text, right-click for a menu.")
		content := lipgloss.JoinVertical(lipgloss.Left, title("◆ Keyboard shortcuts"), "", grid, "", tips, "",
			t.subtle.Render("click or press any key to close"))
		return box.Render(content), nil

	case overlayQuit:
		return dialog(colWarning, []string{
			title("Unsaved changes"), "",
			t.text.Render("Save " + m.displayName() + " before quitting?"),
		})

	case overlayReload:
		// Without unsaved changes, the prompt is about the cell being edited.
		lost := "your unsaved changes"
		if !m.dirty {
			lost = "the cell you are editing"
		}
		return dialog(colWarning, []string{
			title("File changed on disk"), "",
			t.text.Render(m.displayName() + " was changed outside gopyter."),
			t.text.Render("Reload it and replace " + lost + "?"),
		})

	case overlaySaveAs:
		return dialog(colPrimary, []string{
			title("Save notebook as"), "",
			m.input.View(),
		})

	case overlayFix:
		border := colPrimary
		if m.fix.err != "" {
			border = colError
		}
		return dialog(border, m.renderFix())

	case overlayMenu:
		return m.renderMenu()

	case overlayTheme:
		return m.renderThemePicker()

	case overlayModel:
		return m.renderModelPicker()
	}
	return "", nil
}

// renderMenu renders the context menu.
func (m *Model) renderMenu() (string, []zone) {
	t := m.theme
	labelW, keyW := 0, 0
	for _, it := range m.menu.items {
		labelW = max(labelW, lipgloss.Width(it.label))
		keyW = max(keyW, lipgloss.Width(it.key))
	}
	inner := 1 + labelW + 3 + keyW + 1
	var rows []string
	var zones []zone
	for j, it := range m.menu.items {
		if it.sep {
			rows = append(rows, t.subtle.Render(strings.Repeat("─", inner)))
			continue
		}
		label := padRight(it.label, labelW)
		keyStr := padLeft(it.key, keyW)
		var row string
		switch {
		case j == m.menu.idx && it.danger:
			row = t.btnDanger.Render(" " + label + "   " + keyStr + " ")
		case j == m.menu.idx:
			row = t.btnHover.Render(" " + label + "   " + keyStr + " ")
		case it.danger:
			row = " " + lipgloss.NewStyle().Foreground(colError).Render(label) + "   " + t.muted.Render(keyStr) + " "
		default:
			row = " " + t.text.Render(label) + "   " + t.muted.Render(keyStr) + " "
		}
		rows = append(rows, row)
		zones = append(zones, zone{rect: image.Rect(1, 1+j, 1+inner, 2+j), act: action{kind: actMenuItem, cell: j}})
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colMuted)
	return box.Render(strings.Join(rows, "\n")), zones
}

// composite draws a layer on top of the base screen using an ultraviolet
// screen buffer, optionally dimming the backdrop.
func (m *Model) composite(base, layer string, x, y int, dim bool) string {
	w, h := m.width, m.height
	buf := uv.NewScreenBuffer(w, h)
	uv.NewStyledString(base).Draw(buf, buf.Bounds())

	if dim {
		for yy := range h {
			for xx := range w {
				c := buf.CellAt(xx, yy)
				if c == nil || c.Width == 0 {
					continue
				}
				nc := *c
				nc.Style = uv.Style{Fg: colSubtle}
				buf.SetCell(xx, yy, &nc)
			}
		}
	}

	lw, lh := lipgloss.Size(layer)
	uv.NewStyledString(layer).Draw(buf, uv.Rect(x, y, lw, lh))
	return buf.Render()
}
