package ui

import (
	"io"
	"os"
	"slices"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/htmlview"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// The running cell's program reads two pipes fed by the UI: its standard
// input, from the input line under the cell, and events for its widgets
// (GoNB's gonbui/widgets), from the widgets drawn in its HTML output. The
// keyboard can focus any of them: tab moves between them.

// focusStdin is the focus of the input line; widgets are focused by
// address.
const focusStdin = "\x00stdin"

// pipeQueue writes to a pipe from a goroutine, so the UI never blocks.
type pipeQueue struct{ lines chan string }

func newPipeQueue(w *os.File) *pipeQueue {
	q := &pipeQueue{lines: make(chan string, 256)}
	go func() {
		defer func() { _ = w.Close() }() // the program sees end of file
		for l := range q.lines {
			if _, err := io.WriteString(w, l); err != nil {
				for range q.lines {
					// The program is gone: drop the rest.
				}
				return
			}
		}
	}()
	return q
}

// send queues text. If the program doesn't read and the queue is full, the
// text is dropped and send returns false.
func (q *pipeQueue) send(text string) bool {
	if q == nil {
		return false
	}
	select {
	case q.lines <- text:
		return true
	default:
		return false
	}
}

func (q *pipeQueue) close() {
	if q != nil {
		close(q.lines)
	}
}

// progInput is the input of the running cell's program.
type progInput struct {
	stdin, events *pipeQueue // nil without a run; stdin is nil once closed
	done          bool       // the user is done: widgets are inactive
	focus         string     // "", focusStdin or a widget's address
	line          textinput.Model
	prompt        string // from gonbui.RequestInput
	password      bool
}

func newInputLine() textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "input for the program"
	return ti
}

// start takes the write ends of a new run's pipes.
func (in *progInput) start(stdin, events *os.File) {
	in.stdin, in.events = newPipeQueue(stdin), newPipeQueue(events)
	in.done, in.focus, in.prompt, in.password = false, "", "", false
	in.line.EchoMode = textinput.EchoNormal
	in.line.Reset()
	in.line.Blur()
}

// stop closes the pipes at the end of a run.
func (in *progInput) stop() {
	in.stdin.close()
	in.events.close()
	in.stdin, in.events = nil, nil
	in.focus = ""
	in.line.Blur()
}

func (in *progInput) closeStdin() {
	in.stdin.close()
	in.stdin = nil
	if in.focus == focusStdin {
		in.focus = ""
		in.line.Blur()
	}
}

// startInput creates the pipes of a new run: it returns the input for the
// kernel, and a function closing the program's ends once it has exited.
func (m *Model) startInput() (kernel.Input, func()) {
	sr, sw, err := os.Pipe()
	if err != nil {
		return kernel.Input{}, func() {}
	}
	er, ew, err := os.Pipe()
	if err != nil {
		_ = sr.Close()
		_ = sw.Close()
		return kernel.Input{}, func() {}
	}
	m.in.start(sw, ew)
	return kernel.Input{Stdin: sr, Events: er}, func() {
		_ = sr.Close() // the program had its own copy
		_ = er.Close()
	}
}

// runningCell returns the running cell, if any.
func (m *Model) runningCell() *Cell {
	if m.running == nil {
		return nil
	}
	_, c := m.cellByID(m.running.cellID)
	return c
}

// stdinCell reports whether cell c shows the input line: it's running and
// its input is still open.
func (m *Model) stdinCell(c *Cell) bool {
	return c != nil && c == m.runningCell() && m.in.stdin != nil
}

// liveWidgets reports whether cell c's widgets are interactive.
func (m *Model) liveWidgets(c *Cell) bool {
	return c != nil && c == m.runningCell() && m.in.events != nil && !m.in.done
}

// inputItems are the focusable items of the running cell, in order.
func (m *Model) inputItems() []string {
	c := m.runningCell()
	if c == nil {
		return nil
	}
	var items []string
	if m.liveWidgets(c) {
		for _, w := range c.outWidgets {
			if !slices.Contains(items, w.Address) {
				items = append(items, w.Address)
			}
		}
	}
	if m.stdinCell(c) {
		items = append(items, focusStdin)
	}
	return items
}

// inputFocused reports whether the keyboard is on the program's input.
func (m *Model) inputFocused() bool { return m.in.focus != "" && m.running != nil }

// focusInput moves the keyboard to an item of the running cell's input.
func (m *Model) focusInput(item string) tea.Cmd {
	if !slices.Contains(m.inputItems(), item) {
		return nil
	}
	m.closeCompletion()
	m.closeInfo()
	if i, _ := m.cellByID(m.running.cellID); i >= 0 {
		m.sel = i
	}
	m.leaveEdit()
	m.in.focus = item
	if item == focusStdin {
		return m.in.line.Focus()
	}
	m.in.line.Blur()
	return nil
}

// focusStdinLine focuses the input line (alt+i).
func (m *Model) focusStdinLine() tea.Cmd { return m.focusInput(focusStdin) }

// focusFirstInput focuses the first widget, or the input line.
func (m *Model) focusFirstInput() tea.Cmd {
	if items := m.inputItems(); len(items) > 0 {
		return m.focusInput(items[0])
	}
	return nil
}

func (m *Model) blurInput() {
	m.in.focus = ""
	m.in.line.Blur()
}

// cycleInput moves the focus by d items.
func (m *Model) cycleInput(d int) tea.Cmd {
	items := m.inputItems()
	if len(items) == 0 {
		return nil
	}
	i := slices.Index(items, m.in.focus)
	return m.focusInput(items[((i+d)%len(items)+len(items))%len(items)])
}

// focusedWidget returns the focused widget.
func (m *Model) focusedWidget() (htmlview.Widget, bool) {
	return m.widget(m.in.focus)
}

// widget returns the running cell's live widget at address.
func (m *Model) widget(address string) (htmlview.Widget, bool) {
	c := m.runningCell()
	if !m.liveWidgets(c) {
		return htmlview.Widget{}, false
	}
	for _, w := range c.outWidgets {
		if w.Address == address {
			return w, true
		}
	}
	return htmlview.Widget{}, false
}

// handleInputKey handles keys while the program's input has the focus.
func (m *Model) handleInputKey(msg tea.KeyPressMsg) tea.Cmd {
	ks := msg.String()
	switch ks {
	case "ctrl+c":
		return m.interrupt()
	case "esc":
		m.blurInput()
		return nil
	case "tab":
		return m.cycleInput(1)
	case "shift+tab":
		return m.cycleInput(-1)
	case "ctrl+d":
		return m.endInput()
	}
	if m.in.focus == focusStdin {
		if ks == "enter" {
			return m.sendStdin(m.in.line.Value() + "\n")
		}
		var cmd tea.Cmd
		m.in.line, cmd = m.in.line.Update(msg)
		return cmd
	}
	w, ok := m.focusedWidget()
	if !ok {
		m.blurInput()
		return nil
	}
	switch w.Kind {
	case htmlview.Button:
		if ks == "enter" || ks == "space" {
			return m.clickWidget(w.Address)
		}
	case htmlview.Select:
		switch ks {
		case "enter", "space":
			return m.openWidgetMenu(w.Address)
		case "left", "up", "h", "k":
			return m.setWidget(w.Address, w.Value-1)
		case "right", "down", "l", "j":
			return m.setWidget(w.Address, w.Value+1)
		}
	case htmlview.Slider:
		step := max((w.Max-w.Min)/100, 1)
		big := max((w.Max-w.Min)/10, 1)
		switch ks {
		case "left", "down", "h", "j":
			return m.setWidget(w.Address, w.Value-step)
		case "right", "up", "l", "k":
			return m.setWidget(w.Address, w.Value+step)
		case "pgdown", "shift+left":
			return m.setWidget(w.Address, w.Value-big)
		case "pgup", "shift+right":
			return m.setWidget(w.Address, w.Value+big)
		case "home":
			return m.setWidget(w.Address, w.Min)
		case "end":
			return m.setWidget(w.Address, w.Max)
		}
	}
	return nil
}

// sendStdin writes text to the program, echoing it into the cell's output
// like a terminal does (a password isn't echoed).
func (m *Model) sendStdin(text string) tea.Cmd {
	c := m.runningCell()
	var cmd tea.Cmd
	if text != "" {
		if m.in.stdin.send(text) {
			if c != nil {
				if m.in.password {
					c.appendOutput(notebook.Stdout, "\n")
				} else {
					c.appendOutput(notebook.Stdout, text)
				}
			}
		} else {
			cmd = m.setStatus(statusError, "the program isn't reading its input")
		}
	}
	m.in.line.Reset()
	if m.in.prompt != "" || m.in.password {
		// A RequestInput is for one line.
		m.in.prompt, m.in.password = "", false
		m.in.line.EchoMode = textinput.EchoNormal
	}
	return cmd
}

// endInput tells the program the user is done: a pending line is sent,
// stdin is closed (end of file) and widgets stop (their Listen channels
// are closed). The program keeps running until it returns.
func (m *Model) endInput() tea.Cmd {
	if m.running == nil {
		return nil
	}
	var cmd tea.Cmd
	if m.in.stdin != nil {
		if pending := m.in.line.Value(); pending != "" {
			cmd = m.sendStdin(pending)
		}
		m.in.closeStdin()
	}
	if !m.in.done {
		m.in.done = true
		m.in.events.send(htmlview.DoneEvent)
		if c := m.runningCell(); c != nil {
			c.outRev++ // redraw the widgets as inactive
		}
	}
	m.blurInput()
	return cmd
}

// clickWidget clicks a button.
func (m *Model) clickWidget(address string) tea.Cmd {
	if _, ok := m.widget(address); !ok {
		return nil
	}
	m.in.events.send(string(m.running.session.Click(address)))
	return m.focusInput(address)
}

// setWidget sets the value of a slider or select.
func (m *Model) setWidget(address string, v int) tea.Cmd {
	w, ok := m.widget(address)
	if !ok {
		return nil
	}
	switch w.Kind {
	case htmlview.Slider:
		v = clamp(v, min(w.Min, w.Max), max(w.Min, w.Max))
	case htmlview.Select:
		if len(w.Options) == 0 {
			return nil
		}
		v = clamp(v, 0, len(w.Options)-1)
	default:
		return nil
	}
	c := m.runningCell()
	if v != w.Value {
		outs, ev := m.running.session.SetValue(c.outputs, address, v)
		c.outputs = outs
		c.outRev++
		m.dirty = true
		m.in.events.send(string(ev))
	}
	return m.focusInput(address)
}

// openWidgetMenu lists a select's options in a menu.
func (m *Model) openWidgetMenu(address string) tea.Cmd {
	w, ok := m.widget(address)
	if !ok || w.Kind != htmlview.Select {
		return nil
	}
	cmd := m.focusInput(address)
	x, y := m.pointer.x, m.pointer.y
	for _, z := range m.zones {
		if z.act.kind == actWidgetMenu && z.act.id == address {
			x, y = z.rect.Min.X, z.rect.Max.Y
		}
	}
	var items []menuItem
	for i, o := range w.Options {
		label := "  " + o
		if i == w.Value {
			label = "✓ " + o
		}
		items = append(items, menuItem{label: label, act: action{kind: actWidgetSet, id: address, n: i}})
	}
	m.menu = menuState{items: items, x: x, y: y, idx: clamp(w.Value, 0, len(items)-1)}
	m.overlay = overlayMenu
	return cmd
}

// applyOp applies an operation of the running program (a DOM change, a
// widget value, a request) to cell c.
func (m *Model) applyOp(c *Cell, text string) tea.Cmd {
	r := m.running.session.Apply(c.outputs, text)
	if r.Changed {
		c.outputs = r.Outputs
		c.outRev++
	}
	if r.Reply != nil {
		m.in.events.send(string(r.Reply))
	}
	if r.Input != nil && m.in.stdin != nil {
		m.in.prompt, m.in.password = r.Input.Prompt, r.Input.Password
		m.in.line.EchoMode = textinput.EchoNormal
		if r.Input.Password {
			m.in.line.EchoMode = textinput.EchoPassword
		}
		return m.focusInput(focusStdin)
	}
	return nil
}
