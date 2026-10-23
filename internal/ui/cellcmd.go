package ui

import tea "charm.land/bubbletea/v2"

// restartAndClear preserves reset errors while clearing every idle cell's output.
func (m *Model) restartAndClear() tea.Cmd {
	cmd := m.restart()
	m.clearOutputs()
	if m.statusKind == statusError {
		return cmd
	}
	return m.setStatus(statusSuccess, "kernel restarted · outputs cleared")
}

func (m *Model) scrollOutput(i int, kind actionKind) tea.Cmd {
	c := m.cells[i]
	if !c.hasTextOutput() {
		return m.setStatus(statusInfo, "no text output to scroll")
	}
	width := m.outWidth()
	step := max(width/2, 4)
	switch kind {
	case actToggleWrap:
		c.noWrap = !c.noWrap
		c.scrollX = 0
	case actScrollOutRight:
		if !c.noWrap {
			c.noWrap, c.scrollX = true, 0
		} else {
			c.scrollX = min(c.scrollX+step, max(c.textW-width, 0))
		}
	case actScrollOutLeft:
		if c.scrollX > 0 {
			c.scrollX = max(c.scrollX-step, 0)
		} else {
			c.noWrap = false
		}
	}
	return nil
}
