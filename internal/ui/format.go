package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// formatCell is shared by the shortcut and the cell button. Like an edit,
// formatting changes only the source, leaving execution to the user.
func (m *Model) formatCell(i int) tea.Cmd {
	if i < 0 || i >= len(m.cells) || m.cells[i].kind != notebook.Code {
		return nil
	}
	c := m.cells[i]
	source, err := kernel.FormatCell(c.ed.Value())
	if err != nil {
		return m.setStatus(statusError, "format: %v", err)
	}
	if source == c.ed.Value() {
		return m.setStatus(statusInfo, "Code already formatted")
	}
	c.ed.breakUndo()
	c.ed.SetValue(source)
	c.ed.breakUndo()
	m.closeCompletion()
	m.dirty, m.follow = true, true
	return m.setStatus(statusSuccess, "Code formatted · ctrl+z while editing to undo")
}
