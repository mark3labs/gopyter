package ui

import (
	"io"
	"os"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// stdinState is the standard input of the running cell. The program reads
// a pipe; lines typed in the input under the running cell are written to
// it, and ctrl+d closes it (end of file), like in a terminal.
type stdinState struct {
	lines chan string // to the writer goroutine; nil when there's no pipe
	open  bool
	focus bool
	input textinput.Model
}

func newStdinInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "input for the program"
	return ti
}

// start takes the write end of a new run's stdin pipe.
func (s *stdinState) start(w *os.File) {
	lines := make(chan string, 64)
	s.lines, s.open, s.focus = lines, true, false
	s.input.Reset()
	s.input.Blur()
	go func() {
		defer func() { _ = w.Close() }() // the program sees end of file
		for l := range lines {
			if _, err := io.WriteString(w, l); err != nil {
				for range lines {
					// The program is gone: drop the rest.
				}
				return
			}
		}
	}()
}

// send queues text for the program. It never blocks the UI: if the
// program doesn't read and the queue is full, text is dropped.
func (s *stdinState) send(text string) bool {
	if !s.open {
		return false
	}
	select {
	case s.lines <- text:
		return true
	default:
		return false
	}
}

// close ends the input (end of file for the program).
func (s *stdinState) close() {
	if s.open {
		close(s.lines)
		s.open = false
	}
	s.focus = false
	s.input.Blur()
}

// stdinCell reports whether cell c shows the input line: it's running
// and its input is still open.
func (m *Model) stdinCell(c *Cell) bool {
	return m.running != nil && m.running.cellID == c.id && m.stdin.open
}

// focusStdin moves the keyboard to the running cell's input.
func (m *Model) focusStdin() tea.Cmd {
	if m.running == nil || !m.stdin.open {
		return nil
	}
	m.closeCompletion()
	m.closeInfo()
	m.stdin.focus = true
	if i, _ := m.cellByID(m.running.cellID); i >= 0 {
		m.sel = i
	}
	return m.stdin.input.Focus()
}

// handleStdinKey handles keys while the input has the focus.
func (m *Model) handleStdinKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return m.interrupt()
	case "esc":
		m.stdin.focus = false
		m.stdin.input.Blur()
		return nil
	case "enter":
		return m.sendStdin(m.stdin.input.Value()+"\n", false)
	case "ctrl+d":
		// As in a terminal: a pending line is sent, then end of file.
		return m.sendStdin(m.stdin.input.Value(), true)
	}
	var cmd tea.Cmd
	m.stdin.input, cmd = m.stdin.input.Update(msg)
	return cmd
}

// sendStdin writes text to the program, echoing it into the cell's output
// like a terminal does, and optionally ends the input.
func (m *Model) sendStdin(text string, eof bool) tea.Cmd {
	var cmd tea.Cmd
	if text != "" {
		if m.stdin.send(text) {
			if _, c := m.cellByID(m.running.cellID); c != nil {
				c.appendOutput(notebook.Stdout, text)
			}
		} else {
			cmd = m.setStatus(statusError, "the program isn't reading its input")
		}
	}
	m.stdin.input.Reset()
	if eof {
		m.stdin.close()
	}
	return cmd
}
