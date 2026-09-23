package ui

import (
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// themeState holds the active theme and the theme picker.
type themeState struct {
	name   string             // active (possibly previewed) theme
	dark   bool               // use the dark variants
	syntax string             // chroma style overriding the theme's, if set
	save   func(string) error // persists a confirmed choice; may be nil
	names  []string           // picker entries
	idx    int                // picker selection
	top    int                // first visible picker row
	rows   int                // visible picker rows (set while rendering)
	orig   string             // theme to restore when the picker is cancelled
}

// applyTheme makes the named theme active and restyles everything derived
// from it. Unknown names are ignored.
func (m *Model) applyTheme(name string) {
	def, ok := lookupTheme(name)
	if !ok {
		return
	}
	p := def.palette(m.themes.dark)
	setPalette(p)
	m.themes.name = name
	m.theme = newTheme()

	st := syntaxStyle(name, p, m.themes.dark)
	if m.themes.syntax != "" {
		m.hl = newHighlighter(m.themes.syntax)
	} else {
		m.hl = newHighlighterStyle(st)
	}
	codeTheme := st.Name
	if m.themes.syntax != "" {
		codeTheme = m.themes.syntax
	}
	m.mdStyle = markdownStyle(p, m.themes.dark, codeTheme)
	m.md, m.infoMD = nil, nil
	m.info.lines = nil // rendered with the old style

	m.spinner.Style = lipgloss.NewStyle().Foreground(colWarning)
	m.help.Styles.ShortKey = m.theme.helpKey
	m.help.Styles.ShortDesc = m.theme.helpDesc
	m.help.Styles.ShortSeparator = m.theme.helpSep
	m.help.Styles.Ellipsis = m.theme.helpSep

	is := m.input.Styles()
	is.Focused.Prompt = lipgloss.NewStyle().Foreground(colPrimary).Bold(true)
	is.Focused.Text = lipgloss.NewStyle().Foreground(colText)
	is.Focused.Placeholder = lipgloss.NewStyle().Foreground(colSubtle)
	is.Blurred.Prompt = lipgloss.NewStyle().Foreground(colMuted)
	is.Blurred.Text = lipgloss.NewStyle().Foreground(colDim)
	is.Blurred.Placeholder = lipgloss.NewStyle().Foreground(colSubtle)
	m.input.SetStyles(is)

	// Rendered markdown and outputs are cached with their styles baked in.
	for _, c := range m.cells {
		c.mdOut, c.outLines = "", nil
	}
}

// openThemePicker shows the theme picker with the active theme selected.
func (m *Model) openThemePicker() tea.Cmd {
	m.closeCompletion()
	ts := &m.themes
	ts.names = ThemeNames()
	ts.orig = ts.name
	ts.idx, ts.top = 0, 0
	for i, n := range ts.names {
		if n == ts.name {
			ts.idx = i
		}
	}
	m.overlay = overlayTheme
	return nil
}

// previewTheme selects picker entry i and applies it without saving.
func (m *Model) previewTheme(i int) {
	ts := &m.themes
	if i < 0 || i >= len(ts.names) {
		return
	}
	ts.idx = i
	if ts.rows > 0 {
		ts.top = clamp(ts.top, i-ts.rows+1, i)
	}
	if ts.names[i] != ts.name {
		m.applyTheme(ts.names[i])
	}
}

// confirmTheme keeps the selected theme and persists it.
func (m *Model) confirmTheme(i int) tea.Cmd {
	m.previewTheme(i)
	m.overlay = overlayNone
	name := m.themes.name
	if m.themes.save != nil {
		if err := m.themes.save(name); err != nil {
			return m.setStatus(statusError, "theme %s applied but not saved: %v", name, err)
		}
	}
	return m.setStatus(statusSuccess, "theme: %s", name)
}

// cancelTheme closes the picker and restores the previous theme.
func (m *Model) cancelTheme() tea.Cmd {
	m.overlay = overlayNone
	if m.themes.orig != m.themes.name {
		m.applyTheme(m.themes.orig)
	}
	return nil
}

// handleThemeKey handles keys while the theme picker is open.
func (m *Model) handleThemeKey(msg tea.KeyPressMsg) tea.Cmd {
	ts := &m.themes
	page := max(ts.rows-1, 1)
	switch msg.String() {
	case "up", "k", "shift+tab":
		m.previewTheme(max(ts.idx-1, 0))
	case "down", "j", "tab":
		m.previewTheme(min(ts.idx+1, len(ts.names)-1))
	case "pgup", "ctrl+u":
		m.previewTheme(max(ts.idx-page, 0))
	case "pgdown", "ctrl+d":
		m.previewTheme(min(ts.idx+page, len(ts.names)-1))
	case "home", "g":
		m.previewTheme(0)
	case "end", "G":
		m.previewTheme(len(ts.names) - 1)
	case "enter", "space":
		return m.confirmTheme(ts.idx)
	case "esc", "q", "ctrl+c":
		return m.cancelTheme()
	}
	return nil
}

// renderThemePicker renders the picker and its clickable rows, relative to
// the overlay's top-left corner.
func (m *Model) renderThemePicker() (string, []zone) {
	t := m.theme
	ts := &m.themes

	// Rows available inside the box: border (2), padding (2), title and
	// hint with their spacers (4).
	ts.rows = clamp(m.height-2-2*dlgPadY-4, 1, len(ts.names))
	ts.top = clamp(ts.top, max(ts.idx-ts.rows+1, 0), ts.idx)
	ts.top = clamp(ts.top, 0, max(len(ts.names)-ts.rows, 0))

	nameW := 0
	for _, n := range ts.names {
		nameW = max(nameW, lipgloss.Width(n))
	}

	var rows []string
	var zones []zone
	end := min(ts.top+ts.rows, len(ts.names))
	for i := ts.top; i < end; i++ {
		n := ts.names[i]
		mark := "  "
		if n == ts.orig {
			mark = "● "
		}
		label := " " + mark + padRight(n, nameW) + " "
		switch {
		case i == ts.idx:
			label = t.btnHover.Render(label)
		case n == ts.orig:
			label = lipgloss.NewStyle().Foreground(colPrimary).Render(label)
		default:
			label = t.text.Render(label)
		}
		row := label + " " + themeSwatch(n, ts.dark)
		zones = append(zones, zone{
			rect: image.Rect(1+dlgPadX, 1+dlgPadY+2+len(rows), 1+dlgPadX+lipgloss.Width(row), 2+dlgPadY+2+len(rows)),
			act:  action{kind: actThemeItem, cell: i},
		})
		rows = append(rows, row)
	}

	title := lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render("◆ Theme")
	if len(ts.names) > ts.rows {
		title += t.muted.Render(strings.Repeat(" ", 4) + itoa(ts.idx+1) + "/" + itoa(len(ts.names)))
	}
	hint := t.helpKey.Render("↑↓") + t.helpDesc.Render(" preview  ") +
		t.helpKey.Render("enter") + t.helpDesc.Render(" apply  ") +
		t.helpKey.Render("esc") + t.helpDesc.Render(" cancel")
	content := lipgloss.JoinVertical(lipgloss.Left, append(append([]string{title, ""}, rows...), "", hint)...)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colPrimary).Padding(dlgPadY, dlgPadX)
	return box.Render(content), zones
}

// themeSwatch previews a theme's main colors.
func themeSwatch(name string, dark bool) string {
	def, ok := lookupTheme(name)
	if !ok {
		return ""
	}
	p := def.palette(dark)
	bg := lipgloss.NewStyle().Background(lipgloss.Color(p.ink))
	var b strings.Builder
	b.WriteString(bg.Render(" "))
	for _, c := range []string{p.primary, p.info, p.accent, p.success, p.warning, p.error} {
		b.WriteString(bg.Foreground(lipgloss.Color(c)).Render("■"))
	}
	b.WriteString(bg.Render(" "))
	return b.String()
}
