// Package ui implements the gopyter terminal notebook.
package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

type mode int

const (
	modeCommand mode = iota
	modeEdit
)

type overlay int

const (
	overlayNone overlay = iota
	overlayHelp
	overlayQuit
	overlaySaveAs
	overlayMenu
)

type statusKind int

const (
	statusInfo statusKind = iota
	statusSuccess
	statusError
)

type runState struct {
	id     int
	cellID string
	cancel context.CancelFunc
	ch     chan kernel.Event
	err    error
}

type runEventsMsg struct {
	id     int
	events []kernel.Event
	done   bool
}

type clearStatusMsg struct{ id int }

type cellLayout struct {
	top, height int
	render      cellRender
}

// Options configure the notebook UI.
type Options struct {
	Path     string
	Notebook *notebook.Notebook
	Kernel   *kernel.Kernel
	// SyntaxTheme is a chroma style name.
	SyntaxTheme string
	// Completer provides code completion (optional).
	Completer Completer
	// Vim enables vim key bindings in edit mode.
	Vim bool
}

// Model is the root Bubble Tea model.
type Model struct {
	k *kernel.Kernel

	completer Completer
	comp      completionState
	vim       vimState
	path      string
	meta      map[string]any
	cells     []*Cell
	sel       int
	mode      mode

	width, height int
	offset        int
	follow        bool
	layout        []cellLayout
	contentLines  int

	dirty   bool
	counter int
	queue   []string
	running *runState
	runSeq  int

	spinner  spinner.Model
	spinning bool
	help     help.Model
	keys     keyMap
	hl       *highlighter
	theme    theme
	md       *glamour.TermRenderer
	mdWidth  int

	status     string
	statusKind statusKind
	statusID   int

	overlay    overlay
	dlgFocus   int
	input      textinput.Model
	quitAfter  bool
	pendingKey string
	clipboard  *Cell
	trash      []trashed
	quitting   bool

	// Mouse state.
	zones       []zone
	hover       action
	hoverTip    string
	hoverCell   int
	drag        dragState
	click       clickState
	menu        menuState
	overlayRect image.Rectangle
	textClip    string
	pointer     struct {
		x, y int
		seen bool
	}
}

type trashed struct {
	cell  *Cell
	index int
}

// New creates the root model.
func New(opts Options) *Model {
	nb := opts.Notebook
	if nb == nil {
		nb = notebook.New()
	}
	if opts.SyntaxTheme == "" {
		opts.SyntaxTheme = "catppuccin-mocha"
	}
	m := &Model{
		k: opts.Kernel, path: opts.Path, meta: nb.Metadata, completer: opts.Completer,
		keys: newKeyMap(), hl: newHighlighter(opts.SyntaxTheme), theme: newTheme(),
		follow: true, hoverCell: -1,
	}
	m.vim.enabled = opts.Vim
	for _, c := range nb.Cells {
		m.cells = append(m.cells, fromNotebook(c))
		m.counter = max(m.counter, c.ExecutionCount)
	}
	if len(m.cells) == 0 {
		m.cells = []*Cell{newCell(notebook.Code, "")}
	}
	m.spinner = spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(colYellow)),
	)
	m.help = help.New()
	m.help.Styles.ShortKey = m.theme.helpKey
	m.help.Styles.ShortDesc = m.theme.helpDesc
	m.help.Styles.ShortSeparator = m.theme.helpSep
	m.help.Styles.Ellipsis = m.theme.helpSep
	m.help.ShortSeparator = " · "

	m.input = textinput.New()
	m.input.Prompt = "❯ "
	m.input.Placeholder = "notebook.ipynb"
	st := m.input.Styles()
	st.Focused.Prompt = lipgloss.NewStyle().Foreground(colGopher).Bold(true)
	st.Focused.Text = lipgloss.NewStyle().Foreground(colText)
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(colSubtle)
	st.Blurred.Prompt = lipgloss.NewStyle().Foreground(colMuted)
	st.Blurred.Text = lipgloss.NewStyle().Foreground(colDim)
	st.Blurred.Placeholder = lipgloss.NewStyle().Foreground(colSubtle)
	m.input.SetStyles(st)
	m.input.SetWidth(40)

	// Start in edit mode on a fresh, empty notebook.
	if len(m.cells) == 1 && m.cells[0].ed.Value() == "" {
		m.mode = modeEdit
		m.vim.mode = vimInsert
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.RequestWindowSize
}

func (m *Model) cur() *Cell { return m.cells[m.sel] }

func (m *Model) cellByID(id string) (int, *Cell) {
	for i, c := range m.cells {
		if c.id == id {
			return i, c
		}
	}
	return -1, nil
}

func (m *Model) setStatus(kind statusKind, format string, args ...any) tea.Cmd {
	m.statusID++
	m.status = fmt.Sprintf(format, args...)
	m.statusKind = kind
	id := m.statusID
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{id} })
}

func (m *Model) busy() bool { return m.running != nil || len(m.queue) > 0 }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case clearStatusMsg:
		if msg.id == m.statusID {
			m.status = ""
		}
		return m, nil

	case spinner.TickMsg:
		if !m.busy() && !m.comp.loading {
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case completionTickMsg:
		if msg.seq != m.comp.seq {
			return m, nil
		}
		return m, m.requestCompletion(msg.trigger, false)

	case completionResultMsg:
		return m, m.handleCompletionResult(msg)

	case runEventsMsg:
		return m, m.handleRunEvents(msg)

	case tea.PasteMsg:
		if m.overlay == overlaySaveAs {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayNone && m.mode == modeEdit {
			m.closeCompletion()
			m.cur().ed.InsertText(msg.Content)
			m.dirty, m.follow = true, true
		}
		return m, nil

	case tea.MouseMsg:
		ms := msg.Mouse()
		m.pointer.x, m.pointer.y, m.pointer.seen = ms.X, ms.Y, true
	}

	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		return m, m.handleWheel(msg.Mouse())

	case tea.MouseClickMsg:
		cmd := m.handleMouseDown(msg.Mouse())
		m.vimSync()
		return m, cmd

	case tea.MouseMotionMsg:
		return m, m.handleMouseMotion(msg.Mouse())

	case tea.MouseReleaseMsg:
		cmd := m.handleMouseRelease()
		m.vimSync()
		return m, cmd

	case tea.ClipboardMsg:
		if m.overlay == overlayNone && m.mode == modeEdit && msg.Content != "" {
			m.cur().ed.InsertText(msg.Content)
			m.dirty, m.follow = true, true
		}
		return m, nil

	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	}

	if m.overlay == overlaySaveAs {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch m.overlay {
	case overlayHelp:
		m.overlay = overlayNone
		return nil
	case overlayMenu:
		switch msg.String() {
		case "up", "k", "shift+tab":
			m.menu.move(-1)
		case "down", "j", "tab":
			m.menu.move(1)
		case "enter", "space":
			return m.doAction(action{kind: actMenuItem, cell: m.menu.idx})
		case "esc", "q", "ctrl+c":
			m.overlay = overlayNone
		}
		return nil
	case overlayQuit, overlaySaveAs:
		return m.handleDialogKey(msg)
	}

	m.follow = true
	k := m.keys

	// The completion popup gets first pick of keys while it's open.
	if m.mode == modeEdit && m.vimInsertMode() {
		if cmd, ok := m.completionKey(msg); ok {
			return cmd
		}
		if m.isCompletionTrigger(msg) {
			return tea.Batch(m.requestCompletion(0, true), m.startSpinner())
		}
	}

	// Global bindings.
	switch {
	case key.Matches(msg, k.RunAdvance) && (!m.vimActive() || msg.String() != "ctrl+r"):
		// In vim normal mode ctrl+r is redo; shift+enter still runs.
		return m.runSelected(true, false)
	case key.Matches(msg, k.Run):
		return m.runSelected(false, false)
	case key.Matches(msg, k.RunInsert):
		return m.runSelected(false, true)
	case key.Matches(msg, k.Save):
		return m.saveCmd()
	case key.Matches(msg, k.Interrupt):
		if m.mode == modeEdit && m.cur().ed.HasSelection() {
			return m.doAction(action{kind: actCopyText, cell: m.sel})
		}
		if m.busy() {
			return m.interrupt()
		}
		if m.mode == modeEdit {
			return m.setStatus(statusInfo, "press esc then q to quit")
		}
		return m.requestQuit()
	}

	if m.vimActive() {
		m.closeCompletion()
		return m.handleVimKey(msg)
	}
	if m.mode == modeEdit {
		c := m.cur()
		id, version := c.id, c.ed.version
		cmd := m.handleEditKey(msg)
		if !m.vimInsertMode() {
			// esc just left vim's insert mode.
			m.closeCompletion()
			return cmd
		}
		return tea.Batch(cmd, m.afterEditKey(msg, id, version))
	}
	return m.handleCommandKey(msg)
}

// movementKeys are editor motions; with shift held they extend the selection.
var movementKeys = map[string]bool{
	"left": true, "right": true, "up": true, "down": true,
	"home": true, "end": true, "ctrl+home": true, "ctrl+end": true,
	"alt+left": true, "alt+right": true, "ctrl+left": true, "ctrl+right": true,
	"pgup": true, "pgdown": true,
}

func (m *Model) handleEditKey(msg tea.KeyPressMsg) tea.Cmd {
	c := m.cur()
	ed := c.ed
	before := ed.version
	ks := msg.String()

	// Motions: shift extends the selection, otherwise it's cleared.
	if base := strings.Replace(ks, "shift+", "", 1); movementKeys[base] {
		if base != ks {
			ed.StartSelection()
		} else if a, b, ok := ed.Selection(); ok && (base == "left" || base == "right") {
			// Collapse the selection to the side of the motion.
			ed.ClearSelection()
			if base == "left" {
				ed.SetCursor(a.Row, a.Col)
			} else {
				ed.SetCursor(b.Row, b.Col)
			}
			return nil
		} else {
			ed.ClearSelection()
		}
		m.moveCursor(base, base == ks)
		return nil
	}

	switch ks {
	case "esc":
		if m.vim.enabled {
			m.vimEscapeInsert()
			return nil
		}
		if ed.HasSelection() {
			ed.ClearSelection()
			return nil
		}
		m.leaveEdit()
	case "enter":
		ed.Newline()
	case "backspace", "ctrl+h":
		ed.Backspace()
	case "delete", "ctrl+d":
		ed.Delete()
	case "ctrl+b":
		ed.ClearSelection()
		ed.Left()
	case "ctrl+f":
		ed.ClearSelection()
		ed.Right()
	case "ctrl+p":
		ed.ClearSelection()
		m.moveCursor("up", true)
	case "ctrl+n":
		ed.ClearSelection()
		m.moveCursor("down", true)
	case "ctrl+a":
		ed.ClearSelection()
		ed.Home()
	case "ctrl+e":
		ed.ClearSelection()
		ed.End()
	case "alt+b":
		ed.ClearSelection()
		ed.WordLeft()
	case "alt+f":
		ed.ClearSelection()
		ed.WordRight()
	case "ctrl+x":
		return m.doAction(action{kind: actCutText, cell: m.sel})
	case "ctrl+t":
		m.convertCell(m.sel, c.kind != notebook.Code)
	case "alt+a", "ctrl+shift+a":
		ed.SelectAll()
	case "alt+backspace", "ctrl+w", "ctrl+backspace":
		ed.DeleteWordBackward()
	case "alt+delete", "alt+d", "ctrl+delete":
		ed.DeleteWordForward()
	case "ctrl+k":
		ed.KillToEnd()
	case "ctrl+u":
		ed.KillToStart()
	case "tab":
		if ed.SelectionSpansLines() {
			ed.IndentSelection(false)
		} else {
			ed.InsertText("\t")
		}
	case "shift+tab":
		if ed.SelectionSpansLines() {
			ed.IndentSelection(true)
		} else {
			ed.Dedent()
		}
	case "ctrl+z":
		ed.Undo()
	case "ctrl+y", "ctrl+shift+z":
		ed.Redo()
	default:
		if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			for _, r := range msg.Text {
				ed.InsertRune(r)
			}
		}
	}
	if ed.version != before {
		m.dirty = true
	}
	return nil
}

// moveCursor applies an editor motion. crossCells lets up/down continue
// into the neighbouring cells (disabled while extending a selection).
func (m *Model) moveCursor(motion string, crossCells bool) {
	ed := m.cur().ed
	switch motion {
	case "left":
		ed.Left()
	case "right":
		ed.Right()
	case "up":
		if !ed.Up() && crossCells && m.sel > 0 {
			m.sel--
			m.cur().ed.CursorEnd()
			m.enterEdit()
		}
	case "down":
		if !ed.Down() && crossCells && m.sel < len(m.cells)-1 {
			m.sel++
			m.cur().ed.CursorStart()
			m.enterEdit()
		}
	case "home":
		ed.Home()
	case "end":
		ed.End()
	case "ctrl+home":
		ed.CursorStart()
	case "ctrl+end":
		ed.CursorEnd()
	case "alt+left", "ctrl+left":
		ed.WordLeft()
	case "alt+right", "ctrl+right":
		ed.WordRight()
	case "pgup":
		for range 10 {
			ed.Up()
		}
	case "pgdown":
		for range 10 {
			ed.Down()
		}
	}
}

// leaveEdit switches to command mode.
func (m *Model) leaveEdit() {
	m.closeCompletion()
	if m.mode == modeEdit {
		ed := m.cur().ed
		ed.ClearSelection()
		ed.breakUndo()
	}
	m.mode = modeCommand
	m.vim.reset()
}

// enterEdit switches to edit mode. With vim bindings, entering from
// command mode starts in normal mode; moving between cells keeps the mode.
func (m *Model) enterEdit() {
	if m.mode != modeEdit && m.vim.enabled {
		m.vim.reset()
		m.vim.mode = vimNormal
		m.cur().ed.clampNormal()
	}
	m.mode = modeEdit
	c := m.cur()
	c.mdOut = "" // re-render markdown after editing
}

func (m *Model) handleCommandKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys
	pending := m.pendingKey
	m.pendingKey = ""

	switch {
	case key.Matches(msg, k.Up):
		m.sel = max(m.sel-1, 0)
	case key.Matches(msg, k.Down):
		m.sel = min(m.sel+1, len(m.cells)-1)
	case key.Matches(msg, k.Top):
		m.sel = 0
	case key.Matches(msg, k.Bottom):
		m.sel = len(m.cells) - 1
	case key.Matches(msg, k.PageUp):
		m.follow = false
		m.offset -= m.viewHeight() / 2
	case key.Matches(msg, k.PageDown):
		m.follow = false
		m.offset += m.viewHeight() / 2
	case key.Matches(msg, k.Edit):
		m.enterEdit()
	case key.Matches(msg, k.InsertAbove):
		m.insertCell(m.sel, newCell(notebook.Code, ""))
	case key.Matches(msg, k.InsertBelow):
		m.insertCell(m.sel+1, newCell(notebook.Code, ""))
	case key.Matches(msg, k.Delete):
		if pending == "d" {
			m.deleteCell(m.sel)
			return m.setStatus(statusInfo, "cell deleted · z to undo")
		}
		m.pendingKey = "d"
	case key.Matches(msg, k.Undelete):
		if n := len(m.trash); n > 0 {
			t := m.trash[n-1]
			m.trash = m.trash[:n-1]
			m.insertCell(min(t.index, len(m.cells)), t.cell)
			m.mode = modeCommand
		}
	case key.Matches(msg, k.Cut):
		m.clipboard = m.cur().clone()
		src := m.cur().ed.Value()
		m.deleteCell(m.sel)
		return tea.Batch(tea.SetClipboard(src), m.setStatus(statusInfo, "cell cut"))
	case key.Matches(msg, k.Copy):
		m.clipboard = m.cur().clone()
		return tea.Batch(tea.SetClipboard(m.cur().ed.Value()), m.setStatus(statusInfo, "cell copied"))
	case key.Matches(msg, k.Paste):
		if m.clipboard != nil {
			m.insertCell(m.sel+1, m.clipboard.clone())
			m.mode = modeCommand
		}
	case key.Matches(msg, k.MoveUp):
		if m.sel > 0 {
			m.cells[m.sel], m.cells[m.sel-1] = m.cells[m.sel-1], m.cells[m.sel]
			m.sel--
			m.dirty = true
		}
	case key.Matches(msg, k.MoveDown):
		if m.sel < len(m.cells)-1 {
			m.cells[m.sel], m.cells[m.sel+1] = m.cells[m.sel+1], m.cells[m.sel]
			m.sel++
			m.dirty = true
		}
	case key.Matches(msg, k.ToMarkdown):
		m.convertCell(m.sel, false)
	case key.Matches(msg, k.ToCode):
		m.convertCell(m.sel, true)
	case key.Matches(msg, k.ToggleOutput):
		m.cur().expanded = !m.cur().expanded
	case key.Matches(msg, k.ClearOutput):
		c := m.cur()
		if c.status != statusRunning {
			c.outputs, c.status, c.count = nil, statusIdle, 0
			m.dirty = true
		}
	case key.Matches(msg, k.RunAll):
		return m.runAll()
	case key.Matches(msg, k.Restart):
		return m.restart()
	case key.Matches(msg, k.Help):
		m.overlay = overlayHelp
	case key.Matches(msg, k.Quit):
		return m.requestQuit()
	case msg.String() == "esc":
		m.pendingKey = ""
	}
	return nil
}

// convertCell switches a cell between code and markdown, keeping the
// current mode and selection.
func (m *Model) convertCell(i int, toCode bool) {
	c := m.cells[i]
	kind := notebook.Markdown
	if toCode {
		kind = notebook.Code
	}
	if c.kind == kind {
		return
	}
	if m.running != nil && m.running.cellID == c.id {
		return
	}
	c.setKind(kind)
	c.mdOut = ""
	m.sel = i
	m.dirty = true
}

func (m *Model) insertCell(i int, c *Cell) {
	m.cells = append(m.cells[:i], append([]*Cell{c}, m.cells[i:]...)...)
	m.sel = i
	m.dirty = true
	m.enterEdit()
	// A new cell is for typing.
	m.vim.mode = vimInsert
}

func (m *Model) deleteCell(i int) {
	c := m.cells[i]
	m.trash = append(m.trash, trashed{cell: c, index: i})
	m.cells = append(m.cells[:i], m.cells[i+1:]...)
	if len(m.cells) == 0 {
		m.cells = []*Cell{newCell(notebook.Code, "")}
	}
	m.sel = clamp(m.sel, 0, len(m.cells)-1)
	m.dirty = true
}

// runSelected executes (or renders, for markdown) the selected cell.
func (m *Model) runSelected(advance, insert bool) tea.Cmd {
	c := m.cur()
	var cmd tea.Cmd
	if c.kind == notebook.Code {
		cmd = m.enqueue(c)
	} else {
		c.mdOut = ""
	}
	m.leaveEdit()
	switch {
	case insert:
		m.insertCell(m.sel+1, newCell(notebook.Code, ""))
	case advance:
		if m.sel == len(m.cells)-1 {
			m.insertCell(m.sel+1, newCell(notebook.Code, ""))
		} else {
			m.sel++
		}
	}
	return cmd
}

func (m *Model) runAll() tea.Cmd {
	var cmds []tea.Cmd
	for _, c := range m.cells {
		if c.kind == notebook.Code && strings.TrimSpace(c.ed.Value()) != "" {
			cmds = append(cmds, m.enqueue(c))
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) enqueue(c *Cell) tea.Cmd {
	if strings.TrimSpace(c.ed.Value()) == "" {
		return nil
	}
	if c.status == statusQueued || (m.running != nil && m.running.cellID == c.id) {
		return nil
	}
	c.status = statusQueued
	m.queue = append(m.queue, c.id)
	return tea.Batch(m.startNext(), m.startSpinner())
}

func (m *Model) startSpinner() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return m.spinner.Tick
}

func (m *Model) startNext() tea.Cmd {
	for m.running == nil && len(m.queue) > 0 {
		id := m.queue[0]
		m.queue = m.queue[1:]
		_, c := m.cellByID(id)
		if c == nil || c.kind != notebook.Code {
			continue
		}
		m.counter++
		m.runSeq++
		c.count = m.counter
		c.outputs = nil
		c.status = statusRunning
		c.errMsg = ""
		c.started = time.Now()
		c.expanded = false
		m.dirty = true

		ctx, cancel := context.WithCancel(context.Background())
		rs := &runState{id: m.runSeq, cellID: c.id, cancel: cancel, ch: make(chan kernel.Event, 1024)}
		m.running = rs
		src, name := c.ed.Value(), fmt.Sprintf("In[%d]", c.count)
		go func() {
			rs.err = m.k.Execute(ctx, c.id, name, src, func(e kernel.Event) { rs.ch <- e })
			cancel()
			close(rs.ch)
		}()
		return waitRun(rs)
	}
	return nil
}

func waitRun(rs *runState) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-rs.ch
		if !ok {
			return runEventsMsg{id: rs.id, done: true}
		}
		events := []kernel.Event{e}
		for len(events) < 1024 {
			select {
			case e, ok := <-rs.ch:
				if !ok {
					return runEventsMsg{id: rs.id, events: events, done: true}
				}
				events = append(events, e)
			default:
				return runEventsMsg{id: rs.id, events: events}
			}
		}
		return runEventsMsg{id: rs.id, events: events}
	}
}

func (m *Model) handleRunEvents(msg runEventsMsg) tea.Cmd {
	rs := m.running
	if rs == nil || rs.id != msg.id {
		return nil
	}
	_, c := m.cellByID(rs.cellID)
	if c != nil {
		for _, e := range msg.events {
			switch e.Kind {
			case kernel.Stdout:
				c.appendOutput(notebook.Stdout, e.Text)
			case kernel.Stderr:
				c.appendOutput(notebook.Stderr, e.Text)
			case kernel.Result:
				c.appendOutput(notebook.Result, e.Text)
			case kernel.Markdown:
				c.appendOutput(notebook.MarkdownOut, e.Text)
			case kernel.Error:
				c.appendOutput(notebook.Error, e.Text)
			case kernel.Info:
				c.appendOutput(notebook.Info, e.Text)
			}
		}
	}
	if !msg.done {
		return waitRun(rs)
	}

	m.running = nil
	if c != nil {
		c.duration = time.Since(c.started)
		switch {
		case rs.err == nil:
			c.status = statusOK
		case errors.Is(rs.err, kernel.ErrInterrupted):
			c.status = statusInterrupted
			c.errMsg = "interrupted"
		case errors.Is(rs.err, kernel.ErrCompile):
			c.status = statusFailed
			c.errMsg = "compile error"
		default:
			c.status = statusFailed
			c.errMsg = rs.err.Error()
			c.appendOutput(notebook.Error, rs.err.Error())
		}
		if c.status == statusFailed && errors.Is(rs.err, kernel.ErrCompile) {
			// Don't keep running the rest of a "run all" after a compile error.
			for _, id := range m.queue {
				if _, qc := m.cellByID(id); qc != nil {
					qc.status = statusIdle
				}
			}
			m.queue = nil
		}
	}
	return m.startNext()
}

func (m *Model) interrupt() tea.Cmd {
	for _, id := range m.queue {
		if _, c := m.cellByID(id); c != nil {
			c.status = statusIdle
		}
	}
	m.queue = nil
	if m.running != nil {
		m.running.cancel()
	}
	return m.setStatus(statusInfo, "interrupted")
}

func (m *Model) restart() tea.Cmd {
	m.interrupt()
	m.k.Reset()
	m.counter = 0
	return m.setStatus(statusSuccess, "kernel restarted · all declarations cleared")
}

func (m *Model) toNotebook() *notebook.Notebook {
	nb := &notebook.Notebook{Metadata: m.meta}
	for _, c := range m.cells {
		nb.Cells = append(nb.Cells, c.toNotebook())
	}
	return nb
}

func (m *Model) save() error {
	if err := m.toNotebook().Save(m.path); err != nil {
		return err
	}
	m.dirty = false
	return nil
}

func (m *Model) openSaveAs() tea.Cmd {
	m.input.SetValue("")
	return m.openDialog(overlaySaveAs)
}

func (m *Model) saveCmd() tea.Cmd {
	if m.path == "" {
		return m.openSaveAs()
	}
	if err := m.save(); err != nil {
		return m.setStatus(statusError, "save failed: %v", err)
	}
	return m.setStatus(statusSuccess, "saved %s", m.path)
}

func (m *Model) requestQuit() tea.Cmd {
	if m.dirty && m.hasContent() {
		return m.openDialog(overlayQuit)
	}
	return m.quit()
}

func (m *Model) hasContent() bool {
	for _, c := range m.cells {
		if strings.TrimSpace(c.ed.Value()) != "" {
			return true
		}
	}
	return false
}

func (m *Model) quit() tea.Cmd {
	if m.running != nil {
		m.running.cancel()
	}
	m.quitting = true
	return tea.Quit
}

// Run starts the TUI.
func Run(ctx context.Context, opts Options) error {
	m := New(opts)
	if wd, err := os.Getwd(); err == nil && opts.Kernel.RunDir == "" {
		opts.Kernel.RunDir = wd
		if opts.Path != "" {
			if abs, err := filepath.Abs(filepath.Dir(opts.Path)); err == nil {
				opts.Kernel.RunDir = abs
			}
		}
	}
	_, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithFilter(m.filter)).Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}
