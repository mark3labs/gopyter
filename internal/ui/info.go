package ui

import (
	"context"
	"image"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mark3labs/gopyter/internal/complete"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// Documenter looks up documentation for the symbol at the cursor. The
// completion engine implements it with gopls hover.
type Documenter interface {
	Hover(ctx context.Context, req complete.Request) (string, error)
}

const (
	infoMaxW = 84 // popup width limit, including the frame
	infoMaxH = 18 // popup height limit, including the frame
)

// infoState is the symbol documentation popup. It is tied to the cursor
// position it was requested at and closes as soon as anything else
// happens.
type infoState struct {
	open    bool
	loading bool
	seq     int
	cellID  string
	row     int
	col     int
	md      string          // gopls hover markdown
	lines   []string        // md rendered at width
	width   int             // content width lines were rendered at
	top     int             // first visible line
	rows    int             // visible lines at the last render
	rect    image.Rectangle // screen area, for the mouse
}

type infoResultMsg struct {
	seq int
	md  string
	err error
}

func (m *Model) closeInfo() {
	m.info = infoState{seq: m.info.seq + 1} // drop in-flight responses
}

// documenter returns the completer's documentation backend, if any.
func (m *Model) documenter() Documenter {
	d, _ := m.completer.(Documenter)
	return d
}

// requestInfo shows documentation for the symbol at the cursor.
func (m *Model) requestInfo() tea.Cmd {
	c := m.cur()
	if m.mode != modeEdit || c.kind != notebook.Code {
		return nil
	}
	d := m.documenter()
	if d == nil {
		return m.setStatus(statusInfo, "symbol info needs gopls")
	}
	m.closeCompletion()
	row, col := c.ed.Cursor()
	m.closeInfo()
	m.info.open, m.info.loading = true, true
	m.info.cellID, m.info.row, m.info.col = c.id, row, col
	seq := m.info.seq
	req := complete.Request{CellID: c.id, Src: c.ed.Value(), Row: row, Col: col, Manual: true}
	return tea.Batch(func() tea.Msg {
		// The first request may wait for gopls to load the workspace.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		md, err := d.Hover(ctx, req)
		return infoResultMsg{seq: seq, md: md, err: err}
	}, m.startSpinner())
}

func (m *Model) handleInfoResult(msg infoResultMsg) tea.Cmd {
	if msg.seq != m.info.seq || !m.infoVisible() {
		return nil
	}
	m.info.loading = false
	switch {
	case msg.err != nil:
		m.closeInfo()
		return m.setStatus(statusError, "symbol info: %v", msg.err)
	case strings.TrimSpace(msg.md) == "":
		m.closeInfo()
		return m.setStatus(statusInfo, "no info at cursor")
	}
	m.info.md = msg.md
	m.info.lines, m.info.width = nil, 0
	return nil
}

// infoVisible reports whether the popup still belongs where the cursor is.
func (m *Model) infoVisible() bool {
	if !m.info.open || m.mode != modeEdit || m.overlay != overlayNone {
		return false
	}
	c := m.cur()
	row, col := c.ed.Cursor()
	return c.id == m.info.cellID && row == m.info.row && col == m.info.col
}

// infoKey handles keys while the popup is open: it scrolls with page keys
// and closes on esc or a repeated info key, which are consumed. Any other
// key closes the popup and is handled normally.
func (m *Model) infoKey(msg tea.KeyPressMsg) bool {
	if !m.info.open {
		return false
	}
	switch msg.String() {
	case "pgup":
		m.scrollInfo(-max(m.info.rows-1, 1))
		return true
	case "pgdown":
		m.scrollInfo(max(m.info.rows-1, 1))
		return true
	case "esc":
		m.closeInfo()
		return true
	}
	if m.isInfoKey(msg) {
		m.closeInfo()
		return true
	}
	m.closeInfo()
	return false
}

// isInfoKey reports whether msg asks for symbol info: the Info binding in
// any edit sub-mode, or K in vim's normal mode.
func (m *Model) isInfoKey(msg tea.KeyPressMsg) bool {
	if m.mode != modeEdit {
		return false
	}
	if msg.String() == "K" && m.vimActive() && m.vim.mode == vimNormal && m.vim.pending() == "" {
		return true
	}
	return key.Matches(msg, m.keys.Info)
}

func (m *Model) scrollInfo(delta int) {
	m.info.top = clamp(m.info.top+delta, 0, max(len(m.info.lines)-m.info.rows, 0))
}

// infoLines renders the documentation at width, caching the result.
func (m *Model) infoLines(width int) []string {
	if m.info.lines != nil && m.info.width == width {
		return m.info.lines
	}
	// The renderer trims blank edges and collapses runs of blank lines.
	// Doc comments are hard-wrapped for a wider page than the popup, so
	// their paragraphs are reflowed rather than broken where the source is.
	out := m.md.Reflowing().Render(m.info.md, width, nil)
	m.info.lines, m.info.width = out, width
	m.info.top = clamp(m.info.top, 0, max(len(out)-1, 0))
	return out
}

// renderInfo draws the popup next to the cursor at (cx, cy), below it when
// there is room and above it otherwise. It returns the layer and its
// position.
func (m *Model) renderInfo(cx, cy int) (string, int, int) {
	t := m.theme
	frame := lipgloss.NewStyle().Foreground(colMuted)
	bodyBottom := m.height - footerHeight
	below, above := bodyBottom-(cy+1), cy-headerHeight

	var body []string
	footer := ""
	if m.info.loading {
		body = []string{" " + m.spinner.View() + t.muted.Render(" loading symbol info… ")}
	} else {
		contentW := clamp(m.width-4, 10, infoMaxW-4)
		lines := m.infoLines(contentW)
		// Fit in the larger space around the cursor; 2 rows are frame.
		m.info.rows = clamp(max(below, above)-2, 1, infoMaxH-2)
		m.info.rows = min(m.info.rows, len(lines))
		m.scrollInfo(0)
		w := 0
		for _, l := range lines {
			w = max(w, lipgloss.Width(l))
		}
		for _, l := range lines[m.info.top : m.info.top+m.info.rows] {
			body = append(body, " "+l+strings.Repeat(" ", w-lipgloss.Width(l))+" ")
		}
		if len(lines) > m.info.rows {
			footer = " " + itoa(m.info.top+1) + "–" + itoa(m.info.top+m.info.rows) + "/" + itoa(len(lines)) + " · pgup/pgdn "
		}
	}

	w := 0
	for _, l := range body {
		w = max(w, lipgloss.Width(l))
	}
	// Widen short content so the scroll position fits.
	if fw := lipgloss.Width(footer) + 2; fw > w {
		if fw <= m.width-2 {
			for i, l := range body {
				body[i] = l + strings.Repeat(" ", fw-lipgloss.Width(l))
			}
			w = fw
		} else {
			footer = ""
		}
	}
	var b strings.Builder
	b.WriteString(frame.Render("╭" + strings.Repeat("─", w) + "╮"))
	side := frame.Render("│")
	for _, l := range body {
		b.WriteByte('\n')
		b.WriteString(side)
		b.WriteString(l)
		b.WriteString(side)
	}
	b.WriteByte('\n')
	b.WriteString(frame.Render("╰" + strings.Repeat("─", w-lipgloss.Width(footer)-1)))
	b.WriteString(t.muted.Render(footer))
	b.WriteString(frame.Render("─╯"))
	layer := b.String()

	lw, lh := lipgloss.Size(layer)
	x := clamp(cx-2, 0, max(m.width-lw, 0))
	y := cy + 1
	if y+lh > bodyBottom && cy-lh >= headerHeight {
		y = cy - lh
	}
	m.info.rect = image.Rect(x, y, x+lw, y+lh)
	return layer, x, y
}
