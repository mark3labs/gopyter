package ui

import (
	"context"
	"image/color"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mark3labs/gopyter/internal/complete"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// Diagnoser reports problems in a cell without executing it.
type Diagnoser interface {
	Diagnose(context.Context, complete.Request) ([]complete.Diagnostic, error)
}

const diagDebounce = 350 * time.Millisecond

type diagState struct {
	seq     int
	cellID  string
	version int
	items   []complete.Diagnostic
	cancel  context.CancelFunc
}
type diagTickMsg struct{ seq int }
type diagResultMsg struct {
	seq     int
	cellID  string
	version int
	items   []complete.Diagnostic
	err     error
}

func (m *Model) diagnoser() Diagnoser { d, _ := m.completer.(Diagnoser); return d }

func (m *Model) clearDiagnostics() {
	if m.diag.cancel != nil {
		m.diag.cancel()
		m.diag.cancel = nil
	}
	m.diag.items = nil
	m.diag.seq++
}

func (m *Model) diagnoseIfChanged() tea.Cmd {
	if m.diagnoser() == nil || m.mode != modeEdit || m.cur().kind != notebook.Code {
		if m.diag.cellID != "" {
			m.clearDiagnostics()
			m.diag.cellID = ""
		}
		return nil
	}
	c := m.cur()
	if c.id == m.diag.cellID && c.ed.version == m.diag.version {
		return nil
	}
	m.clearDiagnostics()
	m.diag.cellID, m.diag.version = c.id, c.ed.version
	seq := m.diag.seq
	return tea.Tick(diagDebounce, func(time.Time) tea.Msg { return diagTickMsg{seq} })
}

func (m *Model) requestDiagnostics() tea.Cmd {
	d := m.diagnoser()
	if d == nil || m.mode != modeEdit || m.cur().kind != notebook.Code {
		return nil
	}
	c := m.cur()
	row, col := c.ed.Cursor()
	req := complete.Request{CellID: c.id, Src: c.ed.Value(), Row: row, Col: col}
	seq, version := m.diag.seq, c.ed.version
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	m.diag.cancel = cancel
	return func() tea.Msg {
		defer cancel()
		items, err := d.Diagnose(ctx, req)
		return diagResultMsg{seq: seq, cellID: req.CellID, version: version, items: items, err: err}
	}
}

func (m *Model) handleDiagResult(msg diagResultMsg) {
	if msg.err != nil || msg.seq != m.diag.seq || m.mode != modeEdit || m.cur().id != msg.cellID || m.cur().ed.version != msg.version {
		return
	}
	m.diag.items = msg.items
}

func (m *Model) diagAtCursor() (complete.Diagnostic, bool) {
	var best complete.Diagnostic
	found := false
	if m.mode != modeEdit || m.cur().id != m.diag.cellID {
		return best, false
	}
	row, col := m.cur().ed.Cursor()
	for _, d := range m.diag.items {
		if row < d.Row || row > d.EndRow || row == d.Row && col < d.Col || row == d.EndRow && col > d.EndCol {
			continue
		}
		if !found || d.Severity < best.Severity {
			best, found = d, true
		}
	}
	return best, found
}

func diagColor(s complete.Severity) color.Color {
	if s == complete.SeverityWarning {
		return colWarning
	}
	return colError
}

func diagIcon(s complete.Severity) string {
	icon := "✗"
	if s == complete.SeverityWarning {
		icon = "⚠"
	}
	return lipgloss.NewStyle().Foreground(diagColor(s)).Render(icon)
}

func (m *Model) diagMarks(c *Cell) []Mark {
	if m.mode != modeEdit || m.cur() != c || c.id != m.diag.cellID {
		return nil
	}
	var marks []Mark
	// Errors win overlaps regardless of publication order.
	for _, sev := range []complete.Severity{complete.SeverityError, complete.SeverityWarning} {
		for _, d := range m.diag.items {
			if d.Severity != sev {
				continue
			}
			for row := max(d.Row, 0); row <= d.EndRow && row < len(c.ed.lines); row++ {
				start, end := 0, len(c.ed.lines[row])
				if row == d.Row {
					start = d.Col
				}
				if row == d.EndRow {
					end = d.EndCol
				}
				marks = append(marks, Mark{Row: row, Start: start, End: end, Color: diagColor(sev)})
			}
		}
	}
	return marks
}
