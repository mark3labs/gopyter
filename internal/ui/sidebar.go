package ui

import (
	"image"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/markdown"
	"github.com/mark3labs/gopyter/internal/notebook"
)

type sidePanel int

const (
	panelContents sidePanel = iota
	panelVariables
)
const (
	sidebarMinW  = 24
	sidebarMaxW  = 36
	notebookMinW = 60
)

type sidebarState struct {
	open, focused bool
	panel         sidePanel
	idx, top      int
	vars          []kernel.Variable
}
type tocEntry struct {
	cell, level int
	text        string
}

func (m *Model) sidebarWidth() int {
	w := clamp(m.width/4, sidebarMinW, sidebarMaxW)
	if !m.side.open || m.width-w < notebookMinW {
		return 0
	}
	return w
}
func (m *Model) bodyWidth() int { return m.width - m.sidebarWidth() }
func (m *Model) toggleSidebar() tea.Cmd {
	w := clamp(m.width/4, sidebarMinW, sidebarMaxW)
	if m.width-w < notebookMinW {
		return m.setStatus(statusInfo, "terminal too narrow for the sidebar")
	}
	m.side.open = !m.side.open
	m.side.focused = m.side.open
	if m.side.open {
		m.leaveEdit()
	}
	return nil
}
func (m *Model) refreshVars() {
	if m.k != nil {
		m.side.vars = m.k.Variables()
	}
	m.clampSidebar(false)
}
func (m *Model) jumpToCell(i int) {
	if i < 0 || i >= len(m.cells) {
		return
	}
	m.leaveEdit()
	m.collapse()
	m.sel = i
	m.follow = false
	if i < len(m.layout) {
		m.offset = m.layout[i].top
	}
	m.side.focused = false
}
func (m *Model) tocEntries() []tocEntry {
	var out []tocEntry
	for i, c := range m.cells {
		if c.kind != notebook.Markdown {
			continue
		}
		src := c.ed.Value()
		if c.tocSrc != src {
			c.toc = markdown.Headings(src)
			c.tocSrc = src
		}
		for _, h := range c.toc {
			if h.Level <= 3 {
				out = append(out, tocEntry{i, h.Level, h.Text})
			}
		}
	}
	return out
}
func (m *Model) currentSection(es []tocEntry) int {
	idx := -1
	for i, e := range es {
		if e.cell > m.sel {
			break
		}
		idx = i
	}
	return idx
}
func (m *Model) setSidePanel(p sidePanel) {
	m.leaveEdit()
	m.side.panel = p
	m.side.idx, m.side.top = 0, 0
	m.side.focused = true
}
func (m *Model) sideCount() int {
	if m.side.panel == panelVariables {
		return len(m.side.vars)
	}
	return len(m.tocEntries())
}
func (m *Model) sideStride() int {
	if m.side.panel == panelVariables {
		return 2
	}
	return 1
}
func (m *Model) clampSidebar(follow bool) {
	n, stride := m.sideCount(), m.sideStride()
	m.side.idx = clamp(m.side.idx, 0, max(n-1, 0))
	rows := max(m.viewHeight()-2, 1)
	if follow {
		start := m.side.idx * stride
		if start < m.side.top {
			m.side.top = start
		}
		if start+stride > m.side.top+rows {
			m.side.top = start + stride - rows
		}
	}
	m.side.top = clamp(m.side.top, 0, max(n*stride-rows, 0))
}
func (m *Model) handleSidebarKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if key.Matches(msg, m.keys.Sidebar) {
		return m.toggleSidebar(), true
	}
	switch msg.String() {
	case "esc":
		m.side.focused = false
	case "tab", "shift+tab":
		m.setSidePanel((m.side.panel + 1) % 2)
	case "up", "k":
		m.side.idx--
		m.clampSidebar(true)
	case "down", "j":
		m.side.idx++
		m.clampSidebar(true)
	case "g", "home":
		m.side.idx = 0
		m.clampSidebar(true)
	case "G", "end":
		m.side.idx = m.sideCount() - 1
		m.clampSidebar(true)
	case "enter":
		m.clampSidebar(false)
		if m.sideCount() > 0 {
			if m.side.panel == panelContents {
				m.jumpToCell(m.tocEntries()[m.side.idx].cell)
			} else {
				i, c := m.cellByID(m.side.vars[m.side.idx].CellID)
				if c != nil {
					m.jumpToCell(i)
				}
			}
		}
	default:
		return nil, false
	}
	m.pendingKey = ""
	return nil, true
}

// Only unstyled headings are cached; all sidebar styles are rebuilt each
// frame, so applyTheme needs no additional invalidation.
func (m *Model) renderSidebar(h int) ([]string, []zone) {
	w := m.sidebarWidth() - 1
	out := make([]string, h)
	var zs []zone
	sep := lipgloss.NewStyle().Foreground(colFaint).Render("│")
	for y := range out {
		out[y] = sep + strings.Repeat(" ", w)
	}
	if h == 0 {
		return out, zs
	}
	lb := &lineBuilder{}
	for p, label := range []string{"Contents", "Variables"} {
		st := m.theme.muted
		if sidePanel(p) == m.side.panel {
			st = m.theme.link.Bold(true)
		}
		a := action{kind: actSidebarPanel, cell: p}
		if m.hovered(a) {
			st = m.theme.linkHover
		}
		lb.button(a, "switch panel", st.Render(" "+label+" "))
	}
	out[0] = sep + padRight(ansi.Truncate(lb.String(), w, ""), w)
	zs = append(zs, translate(lb.zones, 1, 0)...)
	m.clampSidebar(false)
	es := m.tocEntries()
	current := m.currentSection(es)
	n := m.sideCount()
	if n == 0 && h > 2 {
		label, hint := "No headings", "add # headings to markdown cells"
		if m.side.panel == panelVariables {
			label, hint = "No variables yet", "run a cell to see its declarations"
		}
		out[2] = sep + padRight(m.theme.muted.Render(ansi.Truncate(label, w, "…")), w)
		if h > 3 {
			out[3] = sep + padRight(m.theme.dim.Render(ansi.Truncate(hint, w, "…")), w)
		}
	}
	for i := range n {
		y := 2 + i*m.sideStride() - m.side.top
		cell := -1
		var lines []string
		st := m.theme.text
		if m.side.panel == panelContents {
			e := es[i]
			cell = e.cell
			marker := " "
			if i == current {
				marker = lipgloss.NewStyle().Foreground(colPrimary).Render("▌")
				st = st.Bold(true)
			}
			a := action{kind: actSidebarItem, cell: cell}
			if m.hovered(a) {
				st = m.theme.linkHover
			}
			label := strings.Repeat(" ", 2*(e.level-1)) + e.text
			lines = []string{marker + st.Render(ansi.Truncate(label, w-1, "…"))}
		} else {
			v := m.side.vars[i]
			if j, c := m.cellByID(v.CellID); c != nil {
				cell = j
			}
			typ := v.Type
			if v.Kind != "var" && v.Kind != "func" {
				typ = strings.TrimSpace(v.Kind + " " + typ)
			}
			typ = ansi.Truncate(typ, max(w-lipgloss.Width(v.Name)-2, 0), "…")
			name := ansi.Truncate(v.Name, w, "…")
			if cell >= 0 && m.hovered(action{kind: actSidebarItem, cell: cell}) {
				st = m.theme.linkHover
			}
			line := st.Bold(true).Render(name) + strings.Repeat(" ", max(w-lipgloss.Width(name)-lipgloss.Width(typ), 0)) + m.theme.muted.Italic(true).Render(typ)
			lines = []string{line, m.theme.dim.Render("  " + ansi.Truncate(v.Value, w-2, "…"))}
		}
		for dy, line := range lines {
			row := y + dy
			if row < 2 || row >= h {
				continue
			}
			line = padRight(line, w)
			if m.side.focused && i == m.side.idx {
				line = lipgloss.NewStyle().Background(colSelection).Render(line)
			}
			out[row] = sep + line
			if cell >= 0 {
				zs = append(zs, zone{rect: image.Rect(1, row, w+1, row+1), act: action{kind: actSidebarItem, cell: cell}})
			}
		}
	}
	return out, zs
}
