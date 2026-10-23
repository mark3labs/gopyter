// Package ui implements the gopyter terminal notebook.
package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/htmlview"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/markdown"
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
	overlayTheme
	overlayReload
	overlayReview
	overlayAsk
	overlayModel
)

type statusKind int

const (
	statusInfo statusKind = iota
	statusSuccess
	statusError
)

type runState struct {
	id      int
	cellID  string
	cancel  context.CancelFunc
	ch      chan kernel.Event
	err     error
	session *htmlview.Session // front-end state of the program's HTML and widgets
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
	// Terminal overrides the process terminal (for example, an SSH PTY).
	Terminal []tea.ProgramOption
	// FixedTheme means UseTheme set the palette before any models started.
	// The picker is disabled and New only builds model-local styles.
	FixedTheme bool
	// ResolvePath validates and maps save-as paths. Nil uses paths as typed.
	ResolvePath func(string) (string, error)
	// Notice is shown when the notebook opens.
	Notice string
	// Keys replaces the defaults for named commands from config.json.
	Keys     map[string][]string
	Path     string
	Notebook *notebook.Notebook
	Kernel   *kernel.Kernel
	// Theme is the UI color theme (see ThemeNames); empty means
	// DefaultTheme.
	Theme string
	// SyntaxTheme is a chroma style name overriding the theme's syntax
	// highlighting; empty follows the theme.
	SyntaxTheme string
	// LightBackground selects the light variant of the theme.
	LightBackground bool
	// SaveTheme persists a theme picked in the UI (optional).
	SaveTheme func(name string) error
	// Completer provides code completion (optional).
	Completer Completer
	// Vim enables vim key bindings in edit mode.
	Vim bool
	// SaveVim persists vim bindings toggled in the UI (optional).
	SaveVim func(on bool) error
	// AI configures the optional AI features. When nil, none is shown.
	AI *AIConfig
}

// Model is the root Bubble Tea model.
type Model struct {
	extEdit struct{ cellID, path, orig string }
	k       *kernel.Kernel

	completer   Completer
	comp        completionState
	diag        diagState
	info        infoState
	ai          *AIConfig // nil: no AI support at all
	aiModel     string    // selected model, kept while AI is off
	assistant   Assistant // nil while AI is off
	task        aiTask    // an AI fix or edit: in flight, then awaiting review
	ask         askState
	picker      modelPicker
	vim         vimState
	path        string
	resolvePath func(string) (string, error)
	meta        map[string]any
	cells       []*Cell
	sel         int
	anchor      int // other end of the command-mode range; -1 for a single cell
	mode        mode

	width, height int
	offset        int
	follow        bool
	layout        []cellLayout
	contentLines  int

	dirty   bool
	watch   watchState
	counter int
	queue   []string
	running *runState
	runSeq  int
	in      progInput // the running program's input: stdin and widgets

	spinner     spinner.Model
	spinning    bool
	help        help.Model
	keys        keyMap
	keyWarnings []string
	hl          *highlighter
	theme       theme
	themes      themeState
	// htmlStyles draw HTML outputs; derived from the theme.
	htmlStyles htmlview.Styles
	// md renders markdown outputs and the info popup; mdPanel renders
	// markdown cells, which sit on a panel of their own background.
	md, mdPanel *markdown.Renderer

	status     string
	statusKind statusKind
	statusID   int

	overlay    overlay
	dlgFocus   int
	input      textinput.Model
	quitAfter  bool
	pendingKey string
	clipboard  []*Cell
	undo       []cellUndo
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

// New creates the root model.
func New(opts Options) *Model {
	nb := opts.Notebook
	if nb == nil {
		nb = notebook.New()
	}
	m := &Model{
		k: opts.Kernel, path: opts.Path, meta: nb.Metadata, completer: opts.Completer,
		follow: true, hoverCell: -1, anchor: -1,
	}
	m.keys, m.keyWarnings = newKeyMapFrom(opts.Keys)
	if opts.AI != nil {
		cfg := *opts.AI
		m.ai, m.aiModel = &cfg, cfg.Model
		if cfg.On && cfg.Model != "" && cfg.New != nil {
			m.assistant = cfg.New(cfg.Model)
		}
	}
	m.resolvePath = opts.ResolvePath
	m.status = opts.Notice
	m.themes.fixed = opts.FixedTheme
	m.themes.dark = !opts.LightBackground
	m.themes.syntax = opts.SyntaxTheme
	m.themes.save = opts.SaveTheme
	m.vim.enabled = opts.Vim
	m.vim.save = opts.SaveVim
	for _, c := range nb.Cells {
		m.cells = append(m.cells, fromNotebook(c))
		m.counter = max(m.counter, c.ExecutionCount)
	}
	if len(m.cells) == 0 {
		m.cells = []*Cell{newCell(notebook.Code, "")}
	}
	m.spinner = spinner.New(spinner.WithSpinner(spinner.MiniDot))
	m.help = help.New()
	m.help.ShortSeparator = " · "

	m.input = textinput.New()
	m.input.Prompt = "❯ "
	m.input.Placeholder = "notebook.ipynb"
	m.input.SetWidth(40)
	m.picker.filter = newFilterInput()
	m.in.line = newInputLine()

	name := opts.Theme
	if !ValidTheme(name) {
		name = DefaultTheme
	}
	m.applyTheme(name)
	m.snapshotDisk()

	// Start in edit mode on a fresh, empty notebook.
	if len(m.cells) == 1 && m.cells[0].ed.Value() == "" {
		m.mode = modeEdit
		m.vim.mode = vimInsert
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.RequestWindowSize, watchTick()}
	if n := len(m.keyWarnings); n > 0 {
		msg := m.keyWarnings[0]
		if n > 1 {
			msg += fmt.Sprintf(" (+%d more · gopyter keys)", n-1)
		}
		cmds = append(cmds, m.setStatus(statusError, "keys: %s", msg))
	}
	return tea.Batch(cmds...)
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
	mm, cmd := m.update(msg)
	return mm, tea.Batch(cmd, m.diagnoseIfChanged())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		if !m.busy() && !m.comp.loading && !m.info.loading && !m.task.active && !m.picker.loading {
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case diagTickMsg:
		if msg.seq == m.diag.seq {
			return m, m.requestDiagnostics()
		}
		return m, nil
	case diagResultMsg:
		m.handleDiagResult(msg)
		return m, nil

	case completionTickMsg:
		if msg.seq != m.comp.seq {
			return m, nil
		}
		return m, m.requestCompletion(msg.trigger, false)

	case completionResultMsg:
		return m, m.handleCompletionResult(msg)

	case infoResultMsg:
		return m, m.handleInfoResult(msg)

	case taskMsg:
		return m, m.handleTaskMsg(msg)

	case ollamaModelsMsg:
		m.handleOllamaModels(msg)
		return m, nil

	case editorDoneMsg:
		return m, m.finishExternalEdit(msg)
	case runEventsMsg:
		return m, m.handleRunEvents(msg)

	case watchTickMsg:
		return m, m.handleWatchTick()

	case diskPollMsg:
		return m, m.handleDiskPoll(msg)

	case tea.PasteMsg:
		if m.dialogHasInput() {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if m.overlay == overlayModel {
			var cmd tea.Cmd
			m.picker.filter, cmd = m.picker.filter.Update(msg)
			m.refilterPicker()
			return m, cmd
		}
		if m.overlay == overlayNone && m.in.focus == focusStdin {
			var cmd tea.Cmd
			m.in.line, cmd = m.in.line.Update(msg)
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

	if m.dialogHasInput() {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	if m.overlay == overlayModel {
		var cmd tea.Cmd
		m.picker.filter, cmd = m.picker.filter.Update(msg)
		return m, cmd
	}
	if m.in.focus == focusStdin {
		// e.g. cursor blinks
		var cmd tea.Cmd
		m.in.line, cmd = m.in.line.Update(msg)
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
	case overlayQuit, overlaySaveAs, overlayReload, overlayReview, overlayAsk:
		return m.handleDialogKey(msg)
	case overlayTheme:
		return m.handleThemeKey(msg)
	case overlayModel:
		return m.handleModelPickerKey(msg)
	}

	m.follow = true
	k := m.keys

	// The running program's input takes the keys while it has the focus.
	if m.inputFocused() {
		return m.handleInputKey(msg)
	}
	if key.Matches(msg, k.Input) && m.running != nil {
		return m.focusStdinLine()
	}
	// The symbol info popup closes on any key; some are its own.
	if m.infoKey(msg) {
		return nil
	}
	if m.isInfoKey(msg) && m.cur().kind == notebook.Code {
		return m.requestInfo()
	}

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
	case m.mode == modeEdit && key.Matches(msg, k.Split):
		m.closeCompletion()
		return m.splitCell(m.sel)
	case msg.String() == "alt+shift+f" || msg.String() == "alt+shift+F" || msg.String() == "alt+F":
		return m.formatCell(m.sel)
	case key.Matches(msg, k.RunAdvance) && (!m.vimActive() || msg.String() != "ctrl+r"):
		// In vim normal mode ctrl+r is redo; shift+enter still runs.
		return m.runSelected(true, false)
	case key.Matches(msg, k.Run):
		return m.runSelected(false, false)
	case key.Matches(msg, k.RunInsert):
		return m.runSelected(false, true)
	case key.Matches(msg, k.RestartClear):
		return m.doAction(action{kind: actRestartClear})
	case key.Matches(msg, k.SaveAs):
		return m.doAction(action{kind: actSaveAs})
	case key.Matches(msg, k.Save):
		return m.saveCmd()
	case key.Matches(msg, k.Interrupt):
		if m.mode == modeEdit && m.cur().ed.HasSelection() {
			return m.doAction(action{kind: actCopyText, cell: m.sel})
		}
		if m.busy() {
			return m.interrupt()
		}
		if m.task.active {
			return m.cancelTask()
		}
		if m.mode == modeEdit {
			return m.setStatus(statusInfo, "%s", withKey(withKey("command mode", m.keys.Escape)+"; quit", m.keys.Quit))
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

	// Configurable edit commands take priority over fixed editor keys.
	switch {
	case key.Matches(msg, m.keys.Escape):
		if m.vim.enabled {
			m.vimEscapeInsert()
		} else if ed.HasSelection() {
			ed.ClearSelection()
		} else {
			m.leaveEdit()
		}
		return nil
	case key.Matches(msg, m.keys.Undo):
		ed.Undo()
		m.dirty = m.dirty || ed.version != before
		return nil
	case key.Matches(msg, m.keys.Redo):
		ed.Redo()
		m.dirty = m.dirty || ed.version != before
		return nil
	}

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
		to := notebook.Code
		if c.kind == notebook.Code {
			to = notebook.Markdown
		}
		m.convertCell(m.sel, to)
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
	m.clearDiagnostics()
	m.diag.cellID = ""
	m.closeCompletion()
	m.closeInfo()
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
	m.collapse()
	if m.mode != modeEdit && m.vim.enabled {
		m.vim.reset()
		m.vim.mode = vimNormal
		m.cur().ed.clampNormal()
	}
	m.mode = modeEdit
	c := m.cur()
	if c.inputHidden() {
		c.setInputHidden(false)
		m.dirty = true
	}
	c.mdOut = "" // re-render markdown after editing
}

func (m *Model) handleCommandKey(msg tea.KeyPressMsg) tea.Cmd {
	pending := m.pendingKey
	m.pendingKey = ""
	if name, ok := m.keys.commandFor(msg); ok {
		repeat := pending == msg.String()
		if name == "delete" && !repeat {
			m.pendingKey = msg.String()
			return nil
		}
		return m.runCommand(name, repeat)
	}
	if msg.String() == "esc" {
		if m.multiSelected() {
			m.collapse()
			return nil
		}
		return m.cancelTask()
	}
	return nil
}

// runCommand is shared by keyboard lookup and direct mouse actions.
func (m *Model) runCommand(name string, repeat bool) tea.Cmd {
	lo, hi := m.selRange()
	switch name {
	case "extend_up":
		m.extendTo(m.sel - 1)
	case "extend_down":
		m.extendTo(m.sel + 1)
	case "select_all":
		m.anchor, m.sel = 0, len(m.cells)-1
	case "merge":
		return m.mergeCells()
	case "up":
		m.collapse()
		m.sel = max(m.sel-1, 0)
	case "down":
		m.collapse()
		m.sel = min(m.sel+1, len(m.cells)-1)
	case "top":
		m.collapse()
		m.sel = 0
	case "bottom":
		m.collapse()
		m.sel = len(m.cells) - 1
	case "page_up":
		m.follow = false
		m.offset -= m.viewHeight() / 2
	case "page_down":
		m.follow = false
		m.offset += m.viewHeight() / 2
	case "edit":
		m.collapse()
		if len(m.inputItems()) > 0 && m.cur() == m.runningCell() {
			// The running cell: use its widgets or type its input.
			return m.focusFirstInput()
		}
		m.enterEdit()
	case "insert_above":
		m.insertCell(m.sel, newCell(notebook.Code, ""))
	case "insert_below":
		m.insertCell(m.sel+1, newCell(notebook.Code, ""))
	case "delete":
		if repeat {
			m.deleteCells(lo, hi)
			return m.deletedStatus(hi - lo + 1)
		}

	case "undelete":
		return m.undoCells()
	case "cut", "copy":
		m.clipboard = nil
		for _, c := range m.cells[lo : hi+1] {
			m.clipboard = append(m.clipboard, c.clone())
		}
		src := joinSources(m.clipboard)
		if name == "cut" {
			m.deleteCells(lo, hi)
		}
		verb := "copied"
		if name == "cut" {
			verb = "cut"
		}
		label := "cell"
		if hi > lo {
			label = fmt.Sprintf("%d cells", hi-lo+1)
		}
		return tea.Batch(tea.SetClipboard(src), m.setStatus(statusInfo, "%s %s", label, verb))
	case "paste":
		if len(m.clipboard) > 0 {
			var cs []*Cell
			for _, c := range m.clipboard {
				cs = append(cs, c.clone())
			}
			m.insertCells(hi+1, cs)
			m.anchor, m.sel = hi+1, hi+len(cs)
			if len(cs) == 1 {
				m.collapse()
			}
		}
	case "move_up":
		m.moveBlock(lo, hi, -1)
	case "move_down":
		m.moveBlock(lo, hi, 1)
	case "to_markdown", "to_code":
		kind := notebook.Markdown
		if name == "to_code" {
			kind = notebook.Code
		}
		for i := lo; i <= hi; i++ {
			m.setCellKind(i, kind)
		}
	case "to_raw":
		return m.doAction(action{kind: actToRaw, cell: m.sel})
	case "toggle_input":
		return m.doAction(action{kind: actToggleInput, cell: m.sel})
	case "scroll_output_left":
		return m.doAction(action{kind: actScrollOutLeft, cell: m.sel})
	case "scroll_output_right":
		return m.doAction(action{kind: actScrollOutRight, cell: m.sel})
	case "external_edit":
		return m.doAction(action{kind: actExtEdit, cell: m.sel})
	case "copy_output":
		return m.doAction(action{kind: actCopyOutput, cell: m.sel})
	case "toggle_output":
		m.cur().expanded = !m.cur().expanded
	case "clear_output":
		for _, c := range m.cells[lo : hi+1] {
			m.clearCellOutput(c)
		}
	case "clear_all_output":
		return m.clearAllOutputs()
	case "run_all":
		return m.runAll()
	case "restart":
		return m.restart()
	case "help":
		m.overlay = overlayHelp
	case "theme":
		return m.openThemePicker()
	case "toggle_vim":
		return m.toggleVim()
	case "fix":
		if m.assistant == nil {
			return nil
		}
		return m.requestFix(m.sel)
	case "ask":
		if m.assistant == nil {
			return nil
		}
		return m.requestEdit(m.sel)
	case "ai_model":
		if m.ai == nil {
			return nil
		}
		return m.openModelPicker()
	case "quit":
		return m.requestQuit()
	}
	return nil
}

// convertCell switches kind, keeping mode and selection. Running cells keep their kind.
func (m *Model) convertCell(i int, kind notebook.CellType) {
	m.setCellKind(i, kind)
	m.sel = i
}

func (m *Model) setCellKind(i int, kind notebook.CellType) {
	c := m.cells[i]
	if c.kind == kind {
		return
	}
	if m.cellBusy(c) {
		return
	}
	c.setKind(kind)
	c.mdOut = ""
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

func (m *Model) deleteCell(i int) { m.deleteCells(i, i) }

// runSelected executes (or renders, for markdown) the selected cell.
func (m *Model) runSelected(advance, insert bool) tea.Cmd {
	lo, hi := m.selRange()
	var cmds []tea.Cmd
	for _, c := range m.cells[lo : hi+1] {
		if c.kind == notebook.Code {
			cmds = append(cmds, m.enqueue(c))
		} else {
			c.mdOut = ""
		}
	}
	m.leaveEdit()
	m.collapse()
	m.sel = hi
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
	return tea.Batch(cmds...)
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
		if c.id == m.diag.cellID {
			m.clearDiagnostics()
		}
		m.counter++
		m.runSeq++
		c.count = m.counter
		c.ran = true
		c.outputs = nil
		c.outRev++
		c.status = statusRunning
		c.errMsg = ""
		c.started = time.Now()
		c.expanded = false
		c.scrollX = 0
		m.dirty = true

		ctx, cancel := context.WithCancel(context.Background())
		rs := &runState{id: m.runSeq, cellID: c.id, cancel: cancel, ch: make(chan kernel.Event, 1024)}
		m.running = rs
		src, name := c.ed.Value(), fmt.Sprintf("In[%d]", c.count)
		// The program's stdin and widget events are pipes fed by the UI.
		rs.session = htmlview.NewSession()
		in, closeIn := m.startInput()
		// Shell commands of the cell run in a terminal as wide as its
		// output, so programs colorize and redraw as they would in a
		// terminal.
		m.k.SetTerminal(m.outWidth())
		go func() {
			rs.err = m.k.ExecuteInput(ctx, c.id, name, src, in, func(e kernel.Event) { rs.ch <- e })
			closeIn()
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
	var cmds []tea.Cmd
	if c != nil {
		for _, e := range msg.events {
			if e.Kind == kernel.Op {
				cmds = append(cmds, m.applyOp(c, e.Text))
				continue
			}
			kind, ok := e.Kind.OutputKind()
			if !ok {
				continue
			}
			if e.ID != "" {
				c.setOutput(kind, e.Text, e.ID)
			} else {
				c.appendOutput(kind, e.Text)
			}
		}
	}
	if !msg.done {
		return tea.Batch(append(cmds, waitRun(rs))...)
	}

	m.running = nil
	m.in.stop()
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

// clearCellOutput drops one cell's output, its run count and its status,
// leaving the source alone. A running cell keeps them: its program is still
// writing to it.
func (m *Model) clearCellOutput(c *Cell) {
	if c.status == statusRunning {
		return
	}
	c.outputs, c.status, c.count = nil, statusIdle, 0
	c.outRev++
	m.dirty = true
}

// clearAllOutputs clears the output of every cell, like Jupyter's "Clear
// Outputs". Cells that are still running keep theirs. It reports how many
// cells were cleared.
func (m *Model) clearAllOutputs() tea.Cmd {
	n := m.clearOutputs()
	if n == 0 {
		return nil
	}
	return m.setStatus(statusInfo, "outputs cleared · %d cells", n)
}

// clearOutputs clears idle cells and returns the number changed.
func (m *Model) clearOutputs() int {
	n := 0
	for _, c := range m.cells {
		if c.status == statusRunning {
			continue
		}
		if len(c.outputs) == 0 && c.count == 0 {
			continue
		}
		m.clearCellOutput(c)
		n++
	}
	return n
}

// hasOutputs reports whether any idle cell holds output or a run count, so
// the context menu only offers the clear actions when they would do
// something.
func (m *Model) hasOutputs() bool {
	for _, c := range m.cells {
		if c.status != statusRunning && (len(c.outputs) > 0 || c.count > 0) {
			return true
		}
	}
	return false
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
	err := m.k.Reset()
	m.counter = 0
	for _, c := range m.cells {
		c.ran = false
	}
	if err != nil {
		return m.setStatus(statusError, "kernel restarted, but saved variables remain: %v", err)
	}
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
	m.snapshotDisk()
	return nil
}

func (m *Model) openSaveAs() tea.Cmd {
	m.input.Placeholder = "notebook.ipynb"
	m.input.SetWidth(40)
	m.input.SetValue(m.path)
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
	if m.task.active {
		m.task.cancel()
	}
	m.quitting = true
	return tea.Quit
}

// NewProgram builds a TUI for a terminal. The caller must call cleanup after
// Run returns, before closing the completion engine and kernel.
func NewProgram(ctx context.Context, opts Options) (*tea.Program, func()) {
	m := New(opts)
	if wd, err := os.Getwd(); err == nil && opts.Kernel.RunDir == "" {
		opts.Kernel.RunDir = wd
		if opts.Path != "" {
			if abs, err := filepath.Abs(filepath.Dir(opts.Path)); err == nil {
				opts.Kernel.RunDir = abs
			}
		}
	}
	terminal := append(slices.Clone(opts.Terminal), tea.WithContext(ctx), tea.WithFilter(m.filter))
	return tea.NewProgram(m, terminal...), m.shutdown
}

// shutdown runs after Bubble Tea stops; drain the cell before closing its kernel.
func (m *Model) shutdown() {
	if m.task.active {
		m.task.cancel()
	}
	if rs := m.running; rs != nil {
		rs.cancel()
		for range rs.ch {
		}
	}
}

// Run starts the TUI and cancels pending work before returning.
func Run(ctx context.Context, opts Options) error {
	p, cleanup := NewProgram(ctx, opts)
	defer cleanup()
	_, err := p.Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}
