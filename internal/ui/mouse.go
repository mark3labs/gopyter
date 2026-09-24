package ui

import (
	"image"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mark3labs/gopyter/internal/htmlview"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// actionKind identifies something a mouse click can trigger.
type actionKind int

const (
	actNone actionKind = iota

	// Toolbar / global.
	actRunSelected
	actRunAll
	actInterrupt
	actRestart
	actSave
	actHelp
	actTheme
	actToggleMode

	// Per cell (action.cell is the cell index).
	actRunCell
	actEditCell
	actInsertAbove
	actInsertBelow
	actCutCell
	actCopyCell
	actPasteCell
	actMoveUp
	actMoveDown
	actDuplicate
	actDeleteCell
	actToMarkdown
	actToCode
	actClearOutput
	actToggleOutput
	actCopyOutput
	actFixCell     // ask the AI to fix the cell's error
	actFocusStdin  // type input for the running program
	actInputDone   // end the running program's input
	actWidgetClick // click a button widget (action.id is its address)
	actWidgetSet   // set a slider or select (action.id) to action.n
	actWidgetMenu  // list a select's options (action.id)
	actAskCell     // ask the AI to change the cell
	actAddCode     // insert a code cell at index action.cell
	actAddMarkdown // insert a markdown cell at index action.cell

	// Editor text.
	actCopyText
	actCutText
	actPasteText
	actSelectAll

	// Dialogs and menus.
	actDialogYes
	actDialogNo
	actDialogCancel
	actDialogConfirm
	actReload
	actReloadKeep
	actReviewApply
	actReviewDiscard
	actAskSend
	actMenuItem       // action.cell is the item index
	actCompletionItem // action.cell is the completion item index
	actThemeItem      // action.cell is the theme picker entry index
	actModelItem      // action.cell is the model picker row index
)

type action struct {
	kind actionKind
	cell int
	id   string // a widget's address
	n    int    // a widget value
}

// zone is a clickable screen region.
type zone struct {
	rect image.Rectangle
	act  action
	tip  string
}

// lineBuilder concatenates styled segments on a single line while keeping
// track of where clickable buttons land.
type lineBuilder struct {
	b     strings.Builder
	x     int
	zones []zone // y is always 0; callers translate
}

func (lb *lineBuilder) add(s string) {
	lb.b.WriteString(s)
	lb.x += lipgloss.Width(s)
}

func (lb *lineBuilder) button(act action, tip, s string) {
	w := lipgloss.Width(s)
	lb.zones = append(lb.zones, zone{rect: image.Rect(lb.x, 0, lb.x+w, 1), act: act, tip: tip})
	lb.add(s)
}

func (lb *lineBuilder) String() string { return lb.b.String() }

// translate returns zones shifted by (dx, dy).
func translate(zs []zone, dx, dy int) []zone {
	out := make([]zone, len(zs))
	for i, z := range zs {
		z.rect = z.rect.Add(image.Pt(dx, dy))
		out[i] = z
	}
	return out
}

type dragKind int

const (
	dragNone dragKind = iota
	dragText
	dragScroll
)

type dragState struct {
	kind dragKind
	cell int
}

type clickState struct {
	at    time.Time
	x, y  int
	count int
}

const multiClickInterval = 400 * time.Millisecond

// menuItem is an entry of the context menu.
type menuItem struct {
	label, key string
	act        action
	danger     bool
	sep        bool
}

type menuState struct {
	items []menuItem
	x, y  int
	idx   int
}

func (ms *menuState) move(d int) {
	n := len(ms.items)
	for range n {
		ms.idx = (ms.idx + d + n) % n
		if !ms.items[ms.idx].sep {
			return
		}
	}
}

func (m *Model) hovered(a action) bool { return m.hover == a && a.kind != actNone }

func (m *Model) zoneAt(x, y int) (zone, bool) {
	pt := image.Pt(x, y)
	for _, v := range slices.Backward(m.zones) {
		if pt.In(v.rect) {
			return v, true
		}
	}
	return zone{}, false
}

// cellAtScreen returns the index of the cell under screen row y, or -1.
func (m *Model) cellAtScreen(y int) int {
	if y < headerHeight || y >= m.height-footerHeight {
		return -1
	}
	cy := y - headerHeight + m.offset
	for i, l := range m.layout {
		if cy >= l.top && cy < l.top+l.height {
			return i
		}
	}
	return -1
}

// hoverAt computes the hover state for a mouse position.
func (m *Model) hoverAt(x, y int) (action, string, int) {
	z, _ := m.zoneAt(x, y)
	cell := -1
	if m.overlay == overlayNone {
		cell = m.cellAtScreen(y)
	}
	return z.act, z.tip, cell
}

// filter drops mouse motion events that don't change anything, so that
// hovering doesn't trigger needless re-renders.
func (m *Model) filter(_ tea.Model, msg tea.Msg) tea.Msg {
	if mm, ok := msg.(tea.MouseMotionMsg); ok && m.drag.kind == dragNone {
		act, _, cell := m.hoverAt(mm.X, mm.Y)
		if act == m.hover && cell == m.hoverCell {
			return nil
		}
	}
	return msg
}

func (m *Model) handleMouseMotion(ms tea.Mouse) tea.Cmd {
	switch m.drag.kind {
	case dragText:
		m.dragTextTo(ms.X, ms.Y)
		return nil
	case dragScroll:
		m.scrollTo(ms.Y)
		return nil
	}
	m.hover, m.hoverTip, m.hoverCell = m.hoverAt(ms.X, ms.Y)
	if m.overlay == overlayMenu && m.hover.kind == actMenuItem {
		m.menu.idx = m.hover.cell
	}
	if m.overlay == overlayTheme && m.hover.kind == actThemeItem {
		m.previewTheme(m.hover.cell)
	}
	if m.overlay == overlayModel && m.hover.kind == actModelItem {
		m.picker.idx = m.hover.cell
	}
	if m.comp.open && m.hover.kind == actCompletionItem {
		m.comp.idx = m.hover.cell
	}
	return nil
}

func (m *Model) handleMouseRelease() tea.Cmd {
	m.drag = dragState{}
	return nil
}

func (m *Model) handleMouseDown(ms tea.Mouse) tea.Cmd {
	// Multi-click detection.
	now := time.Now()
	if now.Sub(m.click.at) < multiClickInterval && abs(ms.X-m.click.x) <= 1 && ms.Y == m.click.y {
		m.click.count = m.click.count%3 + 1
	} else {
		m.click.count = 1
	}
	m.click.at, m.click.x, m.click.y = now, ms.X, ms.Y

	if m.overlay != overlayNone {
		return m.overlayClick(ms)
	}
	m.pendingKey = ""

	if m.infoVisible() && image.Pt(ms.X, ms.Y).In(m.info.rect) {
		return nil // clicks on the popup don't reach the cells beneath
	}
	m.closeInfo()
	// A click elsewhere leaves the program input (a click on it refocuses).
	if m.in.focus != "" {
		m.blurInput()
	}

	if m.comp.open {
		if z, ok := m.zoneAt(ms.X, ms.Y); ok && z.act.kind == actCompletionItem && ms.Button == tea.MouseLeft {
			return m.acceptCompletion(z.act.cell)
		}
		if image.Pt(ms.X, ms.Y).In(m.comp.rect) {
			return nil // e.g. a click on the documentation panel
		}
		m.closeCompletion()
	}

	if ms.Button == tea.MouseRight {
		return m.openContextMenu(ms.X, ms.Y)
	}
	if ms.Button != tea.MouseLeft {
		return nil
	}
	if z, ok := m.zoneAt(ms.X, ms.Y); ok {
		return m.doAction(z.act)
	}

	// Scrollbar.
	if ms.X == m.width-1 && ms.Y >= headerHeight && ms.Y < m.height-footerHeight && m.contentLines > m.viewHeight() {
		m.drag = dragState{kind: dragScroll}
		m.scrollTo(ms.Y)
		return nil
	}

	i := m.cellAtScreen(ms.Y)
	if i < 0 {
		return nil
	}
	m.follow = false
	m.sel = i
	c := m.cells[i]
	l := m.layout[i]
	r := l.render
	ly := ms.Y - headerHeight + m.offset - l.top
	if !r.hasEdit || ly < r.edTop || ly >= r.edTop+len(r.ev.lines) {
		// Outside the editor.
		if c.kind == notebook.Markdown && !r.hasEdit && m.click.count >= 2 {
			m.enterEdit()
			return nil
		}
		m.leaveEdit()
		return nil
	}

	if m.mode != modeEdit {
		m.enterEdit()
	}
	ed := c.ed
	vx, vy := ms.X-r.edLeft, ly-r.edTop
	row, col := ed.PositionAt(r.ev, vx, vy)
	switch {
	case vx < r.ev.gutterW:
		// Line numbers select whole lines.
		ed.SelectLine(row)
	case m.click.count == 2:
		ed.SelectWordAt(row, col)
	case m.click.count == 3:
		ed.SelectLine(row)
	case ms.Mod&tea.ModShift != 0:
		ed.StartSelection()
		ed.SetCursor(row, col)
	default:
		ed.ClearSelection()
		ed.SetCursor(row, col)
		ed.StartSelection()
	}
	ed.breakUndo()
	m.drag = dragState{kind: dragText, cell: i}
	return nil
}

// dragTextTo extends the selection while dragging, auto-scrolling when the
// pointer leaves the notebook area.
func (m *Model) dragTextTo(x, y int) {
	i := m.drag.cell
	if i >= len(m.layout) || i >= len(m.cells) || !m.layout[i].render.hasEdit {
		return
	}
	m.follow = false
	bodyTop, bodyBottom := headerHeight, m.height-footerHeight-1
	if y < bodyTop {
		m.offset--
		y = bodyTop
	} else if y > bodyBottom {
		m.offset++
		y = bodyBottom
	}
	l := m.layout[i]
	r := l.render
	ly := y - headerHeight + m.offset - l.top - r.edTop
	ed := m.cells[i].ed
	ed.StartSelection()
	if ly < 0 {
		ed.SetCursor(0, 0)
		return
	}
	if ly >= len(r.ev.lines) {
		ed.CursorEnd()
		return
	}
	row, col := ed.PositionAt(r.ev, x-r.edLeft, ly)
	ed.SetCursor(row, col)
}

func (m *Model) scrollTo(y int) {
	vh := m.viewHeight()
	m.follow = false
	rel := clamp(y-headerHeight, 0, vh-1)
	m.offset = rel * max(m.contentLines-vh, 0) / max(vh-1, 1)
}

func (m *Model) handleWheel(ms tea.Mouse) tea.Cmd {
	if m.overlay == overlayMenu {
		m.overlay = overlayNone
	}
	if m.overlay == overlayTheme {
		switch ms.Button {
		case tea.MouseWheelUp:
			m.previewTheme(max(m.themes.idx-1, 0))
		case tea.MouseWheelDown:
			m.previewTheme(min(m.themes.idx+1, len(m.themes.names)-1))
		}
		return nil
	}
	if m.overlay == overlayModel {
		switch ms.Button {
		case tea.MouseWheelUp:
			m.movePicker(-1)
		case tea.MouseWheelDown:
			m.movePicker(1)
		}
		return nil
	}
	if m.overlay != overlayNone {
		return nil
	}
	if m.infoVisible() && image.Pt(ms.X, ms.Y).In(m.info.rect) {
		switch ms.Button {
		case tea.MouseWheelUp:
			m.scrollInfo(-1)
		case tea.MouseWheelDown:
			m.scrollInfo(1)
		}
		return nil
	}
	m.closeInfo()
	if m.comp.open {
		if image.Pt(ms.X, ms.Y).In(m.comp.rect) {
			switch ms.Button {
			case tea.MouseWheelUp:
				m.comp.idx = max(m.comp.idx-1, 0)
			case tea.MouseWheelDown:
				m.comp.idx = min(m.comp.idx+1, len(m.comp.items)-1)
			}
			m.clampCompletionScroll()
			return nil
		}
		m.closeCompletion()
	}
	m.follow = false
	switch ms.Button {
	case tea.MouseWheelUp:
		m.offset -= 3
	case tea.MouseWheelDown:
		m.offset += 3
	}
	if m.drag.kind == dragText {
		m.dragTextTo(ms.X, ms.Y)
	}
	return nil
}

func (m *Model) overlayClick(ms tea.Mouse) tea.Cmd {
	z, ok := m.zoneAt(ms.X, ms.Y)
	if ok && ms.Button == tea.MouseLeft {
		return m.doAction(z.act)
	}
	inside := image.Pt(ms.X, ms.Y).In(m.overlayRect)
	switch m.overlay {
	case overlayHelp:
		m.overlay = overlayNone
	case overlayTheme:
		if !inside {
			return m.cancelTheme()
		}
	case overlayModel:
		if !inside {
			m.closeModelPicker()
		}
	case overlayMenu:
		if !inside {
			m.overlay = overlayNone
			// Right-clicking elsewhere reopens the menu there.
			if ms.Button == tea.MouseRight {
				return m.openContextMenu(ms.X, ms.Y)
			}
		}
	default:
		if !inside {
			return m.doAction(action{kind: actDialogCancel})
		}
	}
	return nil
}

// openContextMenu shows a context menu for whatever is under the pointer.
func (m *Model) openContextMenu(x, y int) tea.Cmd {
	m.closeCompletion()
	i := m.cellAtScreen(y)
	if i < 0 {
		return nil
	}
	m.sel = i
	c := m.cells[i]
	a := func(k actionKind) action { return action{kind: k, cell: i} }
	var items []menuItem

	// Text actions when right-clicking inside the editor being edited.
	l := m.layout[i]
	ly := y - headerHeight + m.offset - l.top
	inEditor := l.render.hasEdit && ly >= l.render.edTop && ly < l.render.edTop+len(l.render.ev.lines)
	if inEditor && m.mode == modeEdit {
		if c.ed.HasSelection() {
			items = append(items,
				menuItem{label: "Cut", key: "^x", act: a(actCutText)},
				menuItem{label: "Copy", key: "^c", act: a(actCopyText)})
		} else {
			// Move the cursor to the click so paste lands there.
			row, col := c.ed.PositionAt(l.render.ev, x-l.render.edLeft, ly-l.render.edTop)
			c.ed.SetCursor(row, col)
		}
		items = append(items,
			menuItem{label: "Paste", key: "", act: a(actPasteText)},
			menuItem{label: "Select all", act: a(actSelectAll)},
			menuItem{sep: true})
	}

	if c.kind == notebook.Code {
		if m.running != nil && m.running.cellID == c.id {
			items = append(items, menuItem{label: "■ Stop", key: "^c", act: a(actInterrupt)})
		} else {
			items = append(items, menuItem{label: "▶ Run cell", key: "^↵", act: a(actRunCell)})
		}
		if m.fixable(c) {
			items = append(items, menuItem{label: "✦ Fix with AI", key: "f", act: a(actFixCell)})
		}
		if m.editable(c) {
			items = append(items, menuItem{label: "✦ Ask AI…", key: "e", act: a(actAskCell)})
		}
	} else {
		items = append(items, menuItem{label: "▶ Render", key: "^↵", act: a(actRunCell)})
	}
	if m.mode != modeEdit || i != m.sel {
		items = append(items, menuItem{label: "✎ Edit", key: "↵", act: a(actEditCell)})
	}
	items = append(items,
		menuItem{sep: true},
		menuItem{label: "Insert above", key: "a", act: a(actInsertAbove)},
		menuItem{label: "Insert below", key: "b", act: a(actInsertBelow)},
		menuItem{label: "Duplicate", act: a(actDuplicate)},
		menuItem{sep: true},
		menuItem{label: "Cut cell", key: "x", act: a(actCutCell)},
		menuItem{label: "Copy cell", key: "c", act: a(actCopyCell)},
	)
	if m.clipboard != nil {
		items = append(items, menuItem{label: "Paste cell below", key: "v", act: a(actPasteCell)})
	}
	items = append(items,
		menuItem{label: "Move up", key: "K", act: a(actMoveUp)},
		menuItem{label: "Move down", key: "J", act: a(actMoveDown)},
		menuItem{sep: true})
	if c.kind == notebook.Code {
		items = append(items, menuItem{label: "Convert to markdown", key: "m", act: a(actToMarkdown)})
		if len(c.outputs) > 0 {
			items = append(items,
				menuItem{label: "Copy output", act: a(actCopyOutput)},
				menuItem{label: "Clear output", key: "O", act: a(actClearOutput)})
		}
	} else {
		items = append(items, menuItem{label: "Convert to code", key: "y", act: a(actToCode)})
	}
	items = append(items,
		menuItem{sep: true},
		menuItem{label: "Delete cell", key: "dd", act: a(actDeleteCell), danger: true})

	m.menu = menuState{items: items, x: x, y: y}
	m.menu.idx = -1
	m.menu.move(1)
	m.overlay = overlayMenu
	return nil
}

// synthKey builds a key press for reusing keyboard handlers.
func synthKey(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

// commandKey runs a command-mode key binding against a given cell, so that
// mouse actions behave exactly like their keyboard equivalents.
func (m *Model) commandKey(cell int, k string) tea.Cmd {
	if cell >= 0 && cell < len(m.cells) {
		m.sel = cell
	}
	m.leaveEdit()
	m.pendingKey = ""
	return m.handleCommandKey(synthKey(k))
}

// doAction executes a clickable action.
func (m *Model) doAction(a action) tea.Cmd {
	if a.kind == actMenuItem {
		if a.cell < 0 || a.cell >= len(m.menu.items) || m.menu.items[a.cell].sep {
			return nil
		}
		m.overlay = overlayNone
		return m.doAction(m.menu.items[a.cell].act)
	}
	validCell := a.cell >= 0 && a.cell < len(m.cells)
	switch a.kind {
	case actRunSelected:
		return m.runSelected(false, false)
	case actFocusStdin:
		return m.focusStdinLine()
	case actInputDone:
		return m.endInput()
	case actWidgetClick:
		return m.clickWidget(a.id)
	case actWidgetSet:
		return m.setWidget(a.id, a.n)
	case actWidgetMenu:
		return m.openWidgetMenu(a.id)
	case actRunAll:
		return m.runAll()
	case actInterrupt:
		if m.busy() {
			return m.interrupt()
		}
	case actRestart:
		return m.restart()
	case actSave:
		return m.saveCmd()
	case actHelp:
		m.overlay = overlayHelp
	case actTheme:
		return m.openThemePicker()
	case actThemeItem:
		return m.confirmTheme(a.cell)
	case actModelItem:
		return m.choosePicker(a.cell)
	case actToggleMode:
		if m.mode == modeEdit {
			m.leaveEdit()
		} else {
			m.enterEdit()
		}

	case actRunCell:
		if validCell {
			m.sel = a.cell
			return m.runSelected(false, false)
		}
	case actEditCell:
		if validCell {
			m.sel = a.cell
			m.follow = true
			m.enterEdit()
		}
	case actInsertAbove:
		return m.commandKey(a.cell, "a")
	case actInsertBelow:
		return m.commandKey(a.cell, "b")
	case actCutCell:
		return m.commandKey(a.cell, "x")
	case actCopyCell:
		return m.commandKey(a.cell, "c")
	case actPasteCell:
		return m.commandKey(a.cell, "v")
	case actMoveUp:
		return m.commandKey(a.cell, "K")
	case actMoveDown:
		return m.commandKey(a.cell, "J")
	case actToMarkdown, actToCode:
		if validCell {
			m.convertCell(a.cell, a.kind == actToCode)
		}
	case actClearOutput:
		return m.commandKey(a.cell, "O")
	case actFixCell:
		return m.commandKey(a.cell, "f")
	case actAskCell:
		return m.commandKey(a.cell, "e")
	case actAskSend:
		return m.sendAsk()
	case actReviewApply:
		return m.applyProposal()
	case actReviewDiscard:
		return m.dialogCancel()
	case actToggleOutput:
		if validCell {
			m.cells[a.cell].expanded = !m.cells[a.cell].expanded
		}
	case actDuplicate:
		if validCell {
			m.cells = append(m.cells[:a.cell+1], append([]*Cell{m.cells[a.cell].clone()}, m.cells[a.cell+1:]...)...)
			m.sel = a.cell + 1
			m.dirty = true
			m.leaveEdit()
		}
	case actDeleteCell:
		if validCell {
			m.leaveEdit()
			m.deleteCell(a.cell)
			return m.setStatus(statusInfo, "cell deleted · z to undo")
		}
	case actCopyOutput:
		if validCell {
			text := plainOutput(m.cells[a.cell])
			m.textClip = text
			return tea.Batch(tea.SetClipboard(text), m.setStatus(statusSuccess, "output copied to clipboard"))
		}
	case actAddCode, actAddMarkdown:
		kind := notebook.Code
		if a.kind == actAddMarkdown {
			kind = notebook.Markdown
		}
		m.follow = true
		m.insertCell(clamp(a.cell, 0, len(m.cells)), newCell(kind, ""))

	case actCopyText, actCutText:
		if !validCell {
			return nil
		}
		ed := m.cells[a.cell].ed
		text := ed.SelectedText()
		if text == "" {
			return nil
		}
		m.textClip = text
		verb := "copied"
		if a.kind == actCutText {
			ed.DeleteSelection()
			m.dirty = true
			verb = "cut"
		}
		return tea.Batch(tea.SetClipboard(text), m.setStatus(statusSuccess, "%s %d characters", verb, len([]rune(text))))
	case actPasteText:
		if !validCell {
			return nil
		}
		m.sel = a.cell
		m.enterEdit()
		if m.textClip != "" {
			m.cells[a.cell].ed.InsertText(m.textClip)
			m.dirty = true
			return nil
		}
		// Fall back to asking the terminal for the system clipboard.
		return tea.ReadClipboard
	case actSelectAll:
		if validCell {
			m.sel = a.cell
			m.enterEdit()
			m.cells[a.cell].ed.SelectAll()
		}

	case actDialogYes:
		return m.dialogYes()
	case actDialogNo:
		return m.quit()
	case actDialogCancel:
		return m.dialogCancel()
	case actDialogConfirm:
		return m.saveAsConfirm()
	case actReload:
		return m.reloadConfirm()
	case actReloadKeep:
		return m.reloadKeep()
	}
	return nil
}

// plainOutput returns the textual outputs of a cell.
func plainOutput(c *Cell) string {
	var b strings.Builder
	for _, o := range c.outputs {
		text := o.Text
		switch o.Kind {
		case notebook.ImageOut:
			text = "[image]" // base64 would be useless as text
		case notebook.HTMLOut:
			text = htmlview.Text(text)
		}
		b.WriteString(text)
		if !strings.HasSuffix(text, "\n") {
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
