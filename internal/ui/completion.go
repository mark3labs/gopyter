package ui

import (
	"context"
	"image"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/complete"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// Completer produces code completions.
type Completer interface {
	Complete(ctx context.Context, req complete.Request) (complete.Result, error)
}

// completerStatus is optionally implemented by completers to report their
// backend in the header.
type completerStatus interface {
	Status() (gopls bool, reason string)
}

const (
	completionDebounce = 90 * time.Millisecond
	completionRows     = 8
	completionLabelMax = 34
	completionDetailW  = 30
	completionDocW     = 52
	completionDocRows  = 12
)

type completionState struct {
	open    bool
	loading bool
	manual  bool
	cellID  string
	row     int
	reqCol  int // cursor column of the request that produced all
	all     []complete.Item
	items   []complete.Item // filtered view of all
	idx     int
	top     int
	seq     int
	source  string
	rect    image.Rectangle // screen area of the list, for the mouse
	rows    int             // visible rows, adapted to the screen space
}

func (c *completionState) visibleRows() int {
	if c.rows > 0 {
		return c.rows
	}
	return completionRows
}

type completionTickMsg struct {
	seq     int
	trigger rune
}

type completionResultMsg struct {
	seq    int
	cellID string
	row    int
	col    int
	manual bool
	res    complete.Result
	err    error
}

func (m *Model) closeCompletion() {
	m.comp.open, m.comp.loading = false, false
	m.comp.all, m.comp.items = nil, nil
	m.comp.seq++ // drop in-flight responses
}

// scheduleCompletion requests completions after a short pause in typing.
func (m *Model) scheduleCompletion(trigger rune) tea.Cmd {
	if m.completer == nil {
		return nil
	}
	m.comp.seq++
	seq := m.comp.seq
	return tea.Tick(completionDebounce, func(time.Time) tea.Msg {
		return completionTickMsg{seq: seq, trigger: trigger}
	})
}

// requestCompletion asks the completer for completions at the cursor.
func (m *Model) requestCompletion(trigger rune, manual bool) tea.Cmd {
	if m.completer == nil || m.mode != modeEdit {
		return nil
	}
	c := m.cur()
	row, col := c.ed.Cursor()
	if manual {
		m.comp.seq++
		m.comp.manual = true
		if !m.comp.open {
			m.comp.open, m.comp.loading = true, true
			m.comp.cellID, m.comp.row = c.id, row
			word, _ := c.ed.WordBeforeCursor()
			m.comp.reqCol = col - len([]rune(word))
		}
	}
	seq := m.comp.seq
	req := complete.Request{CellID: c.id, Src: c.ed.Value(), Row: row, Col: col, Manual: manual, Trigger: trigger}
	completer := m.completer
	timeout := 3 * time.Second
	if manual {
		timeout = 30 * time.Second // the first request may wait for gopls to load
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		res, err := completer.Complete(ctx, req)
		return completionResultMsg{seq: seq, cellID: req.CellID, row: row, col: col, manual: manual, res: res, err: err}
	}
}

func (m *Model) handleCompletionResult(msg completionResultMsg) tea.Cmd {
	if msg.seq != m.comp.seq || m.mode != modeEdit || m.cur().id != msg.cellID {
		return nil
	}
	row, col := m.cur().ed.Cursor()
	if row != msg.row || col < msg.col-msg.res.Replace {
		m.closeCompletion()
		return nil
	}
	if msg.err != nil {
		m.closeCompletion()
		if msg.manual {
			return m.setStatus(statusError, "completion failed: %v", msg.err)
		}
		return nil
	}
	m.comp.open, m.comp.loading = true, false
	m.comp.manual = m.comp.manual || msg.manual
	m.comp.cellID, m.comp.row, m.comp.reqCol = msg.cellID, msg.row, msg.col
	m.comp.all = msg.res.Items
	m.comp.source = msg.res.Source
	m.refilterCompletion(true)

	// A lone manual match is inserted directly, like an IDE would.
	if msg.manual && len(m.comp.items) == 1 {
		return m.acceptCompletion(0)
	}
	if len(m.comp.items) == 0 && msg.manual {
		return m.setStatus(statusInfo, "no completions")
	}
	return nil
}

// typedSinceRequest returns the word typed from the start of the completed
// word to the cursor, and whether the cursor is still within it.
func (m *Model) typedSinceRequest() (string, int, bool) {
	c := m.cur()
	row, col := c.ed.Cursor()
	if row != m.comp.row || c.id != m.comp.cellID {
		return "", 0, false
	}
	start := m.comp.reqCol
	if len(m.comp.all) > 0 {
		start = m.comp.reqCol - m.comp.all[0].Replace
	}
	line := c.ed.lines[row]
	if start < 0 || start > col || col > len(line) {
		return "", 0, false
	}
	typed := line[start:col]
	for _, r := range typed {
		if !isWord(r) {
			return "", 0, false
		}
	}
	return string(typed), start, true
}

// refilterCompletion narrows the last results to what has been typed.
func (m *Model) refilterCompletion(resetSelection bool) {
	typed, _, ok := m.typedSinceRequest()
	if !ok {
		m.closeCompletion()
		return
	}
	var prefix, fuzzy []complete.Item
	lower := strings.ToLower(typed)
	for _, it := range m.comp.all {
		l := strings.ToLower(it.Label)
		switch {
		case strings.HasPrefix(l, lower):
			prefix = append(prefix, it)
		case fuzzyMatch(l, lower):
			fuzzy = append(fuzzy, it)
		}
	}
	items := append(prefix, fuzzy...)
	// Nothing left to offer if the only candidate is exactly what's typed.
	if len(items) == 1 && items[0].Insert == typed && !m.comp.manual {
		items = nil
	}
	m.comp.items = items
	if len(items) == 0 && !m.comp.loading {
		m.comp.open = false
		return
	}
	if resetSelection || m.comp.idx >= len(items) {
		m.comp.idx, m.comp.top = 0, 0
	}
	m.clampCompletionScroll()
}

func fuzzyMatch(s, pattern string) bool {
	i := 0
	for _, r := range s {
		if i < len(pattern) && r == rune(pattern[i]) {
			i++
		}
	}
	return i == len(pattern)
}

func (m *Model) clampCompletionScroll() {
	n := len(m.comp.items)
	if n == 0 {
		m.comp.idx, m.comp.top = 0, 0
		return
	}
	m.comp.idx = (m.comp.idx%n + n) % n
	if m.comp.idx < m.comp.top {
		m.comp.top = m.comp.idx
	}
	rows := m.comp.visibleRows()
	if m.comp.idx >= m.comp.top+rows {
		m.comp.top = m.comp.idx - rows + 1
	}
	m.comp.top = clamp(m.comp.top, 0, max(n-rows, 0))
}

// acceptCompletion inserts the i-th visible item.
func (m *Model) acceptCompletion(i int) tea.Cmd {
	if i < 0 || i >= len(m.comp.items) {
		return nil
	}
	it := m.comp.items[i]
	_, start, ok := m.typedSinceRequest()
	if !ok {
		m.closeCompletion()
		return nil
	}
	ed := m.cur().ed
	_, col := ed.Cursor()
	// Use the item's own replacement range when it reaches further back.
	if s := m.comp.reqCol - it.Replace; s < start && s >= 0 {
		start = s
	}
	text := it.Insert
	line := ed.lines[ed.row]
	nextIsParen := col < len(line) && line[col] == '('
	callable := (it.Kind == complete.KindFunc || it.Kind == complete.KindMethod) && strings.HasPrefix(it.Detail, "func")
	back := 0
	if callable && !nextIsParen && !strings.HasSuffix(text, ")") {
		text += "()"
		if !strings.HasPrefix(it.Detail, "func()") {
			back = 1 // place the cursor between the parentheses
		}
	}
	ed.ReplaceBeforeCursor(col-start, text)
	for range back {
		ed.Left()
	}
	m.dirty = true
	m.closeCompletion()
	return nil
}

// completionKey handles keys while the popup is open. It reports whether
// the key was consumed.
func (m *Model) completionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !m.comp.open {
		return nil, false
	}
	switch msg.String() {
	case "up", "ctrl+p":
		m.comp.idx--
	case "down", "ctrl+n":
		m.comp.idx++
	case "pgup":
		m.comp.idx = max(m.comp.idx-m.comp.visibleRows(), 0)
	case "pgdown":
		m.comp.idx = min(m.comp.idx+m.comp.visibleRows(), len(m.comp.items)-1)
	case "tab", "enter":
		if len(m.comp.items) == 0 {
			if msg.String() == "enter" {
				m.closeCompletion()
				return nil, false
			}
			return nil, true
		}
		return m.acceptCompletion(m.comp.idx), true
	case "esc":
		m.closeCompletion()
	default:
		return nil, false
	}
	m.clampCompletionScroll()
	return nil, true
}

// isCompletionTrigger reports whether a manual trigger key should complete
// (tab only completes after an identifier or a dot).
func (m *Model) isCompletionTrigger(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "ctrl+space", "ctrl+@":
		return m.completer != nil && m.cur().kind == notebook.Code
	case "tab":
		ed := m.cur().ed
		if m.completer == nil || m.cur().kind != notebook.Code || ed.SelectionSpansLines() || !ed.InCodeContext() {
			return false
		}
		word, prev := ed.WordBeforeCursor()
		return word != "" || prev == '.'
	}
	return false
}

// afterEditKey decides whether an edit should open, update or close the
// completion popup.
func (m *Model) afterEditKey(msg tea.KeyPressMsg, cellID string, version int) tea.Cmd {
	if m.completer == nil {
		return nil
	}
	if m.mode != modeEdit || m.cur().id != cellID || m.cur().kind != notebook.Code {
		m.closeCompletion()
		return nil
	}
	ed := m.cur().ed
	if ed.version == version {
		// Cursor movement and the like.
		if m.comp.open {
			m.closeCompletion()
		}
		return nil
	}
	ks := msg.String()
	switch {
	case ks == "backspace" || ks == "ctrl+h":
		if !m.comp.open {
			return nil
		}
		m.refilterCompletion(false)
		if m.comp.open {
			return m.scheduleCompletion(0)
		}
		return nil
	case msg.Text == ".":
		if !ed.InCodeContext() {
			return nil
		}
		m.closeCompletion()
		return m.scheduleCompletion('.')
	case len([]rune(msg.Text)) == 1 && isWord([]rune(msg.Text)[0]):
		if !ed.InCodeContext() {
			m.closeCompletion()
			return nil
		}
		word, _ := ed.WordBeforeCursor()
		if word == "" || unicode.IsDigit([]rune(word)[0]) {
			m.closeCompletion()
			return nil
		}
		if m.comp.open {
			m.refilterCompletion(false)
		}
		return m.scheduleCompletion(0)
	default:
		m.closeCompletion()
		return nil
	}
}

var kindIcons = map[complete.Kind]struct {
	icon string
	col  string
}{
	complete.KindFunc:      {"ƒ", "#60A5FA"},
	complete.KindMethod:    {"m", "#A78BFA"},
	complete.KindVar:       {"v", "#FDDD00"},
	complete.KindConst:     {"c", "#FB923C"},
	complete.KindField:     {"f", "#5DC9E2"},
	complete.KindType:      {"T", "#4ADE80"},
	complete.KindInterface: {"I", "#34D399"},
	complete.KindPackage:   {"p", "#F472B6"},
	complete.KindKeyword:   {"k", "#CE3262"},
	complete.KindOther:     {"·", "#A1A1AA"},
}

// highlightMatch emphasizes the characters of label matching typed.
func highlightMatch(label, typed string, base, hi lipgloss.Style) string {
	if typed == "" {
		return base.Render(label)
	}
	lower := strings.ToLower(typed)
	var b strings.Builder
	i := 0
	for _, r := range label {
		if i < len(lower) && unicode.ToLower(r) == rune(lower[i]) {
			b.WriteString(hi.Render(string(r)))
			i++
			continue
		}
		b.WriteString(base.Render(string(r)))
	}
	return b.String()
}

// renderCompletion draws the popup anchored at the cursor. It returns the
// layer, its position, and clickable zones relative to the layer.
func (m *Model) renderCompletion(cx, cy int) (string, int, int, []zone) {
	t := m.theme
	frame := lipgloss.NewStyle().Foreground(colMuted)
	typed, _, _ := m.typedSinceRequest()

	// Fit the list in the larger of the spaces below and above the cursor
	// (2 rows go to the frame).
	bodyBottom := m.height - footerHeight
	space := max(bodyBottom-(cy+1), cy-headerHeight) - 2
	m.comp.rows = clamp(space, 1, completionRows)
	m.clampCompletionScroll()

	var rows []string
	var zones []zone
	listW := 0
	if len(m.comp.items) == 0 {
		msg := " " + m.spinner.View() + t.muted.Render(" loading completions… ")
		rows = append(rows, msg)
		listW = lipgloss.Width(msg)
	} else {
		labelW, detailW := 0, 0
		end := min(m.comp.top+m.comp.rows, len(m.comp.items))
		for _, it := range m.comp.items[m.comp.top:end] {
			labelW = max(labelW, min(lipgloss.Width(it.Label), completionLabelMax))
			detailW = max(detailW, min(lipgloss.Width(it.Detail), completionDetailW))
		}
		listW = 3 + labelW + 1
		if detailW > 0 {
			listW += 2 + detailW + 1
		}
		for j, it := range m.comp.items[m.comp.top:end] {
			i := m.comp.top + j
			sel := i == m.comp.idx
			ki := kindIcons[it.Kind]
			bg := lipgloss.NewStyle()
			if sel {
				bg = bg.Background(colSelection)
			}
			icon := bg.Foreground(lipgloss.Color(ki.col)).Bold(true).Render(" " + ki.icon + " ")
			base := bg.Foreground(colText)
			hi := bg.Foreground(colPrimary).Bold(true)
			label := highlightMatch(ansi.Truncate(it.Label, labelW, "…"), typed, base, hi)
			label += bg.Render(strings.Repeat(" ", labelW-lipgloss.Width(ansi.Strip(label))+1))
			row := icon + label
			if detailW > 0 {
				d := ansi.Truncate(it.Detail, detailW, "…")
				row += bg.Render("  ") + bg.Foreground(colMuted).Render(padLeft(d, detailW)) + bg.Render(" ")
			}
			rows = append(rows, row)
			zones = append(zones, zone{
				rect: image.Rect(1, 1+j, 1+listW, 2+j),
				act:  action{kind: actCompletionItem, cell: i},
			})
		}
	}

	// Frame, with a position counter and the backend in the bottom border.
	var b strings.Builder
	b.WriteString(frame.Render("╭" + strings.Repeat("─", listW) + "╮"))
	side := frame.Render("│")
	for _, r := range rows {
		b.WriteByte('\n')
		b.WriteString(side)
		b.WriteString(r)
		b.WriteString(side)
	}
	footer := ""
	if n := len(m.comp.items); n > 0 {
		footer = " " + itoa(m.comp.idx+1) + "/" + itoa(n) + " "
		if m.comp.source != "" {
			footer += "· " + m.comp.source + " "
		}
	}
	if lipgloss.Width(footer)+2 > listW {
		footer = ""
	}
	b.WriteByte('\n')
	b.WriteString(frame.Render("╰" + strings.Repeat("─", listW-lipgloss.Width(footer)-1)))
	b.WriteString(t.muted.Render(footer))
	b.WriteString(frame.Render("─╯"))
	list := b.String()
	lw, lh := lipgloss.Size(list)

	// Place the list under the cursor, aligned with the word's start, or
	// above it when there isn't room below.
	x := cx - len([]rune(typed)) - 4
	y := cy + 1
	if y+lh > bodyBottom && cy-lh >= headerHeight {
		y = cy - lh
	}
	x = clamp(x, 0, max(m.width-lw, 0))

	// Documentation panel for the selected item, on whichever side of the
	// list has more room.
	right, left := m.width-(x+lw), x
	docW := min(completionDocW, max(right, left)-4)
	doc := ""
	if docW >= 20 {
		doc = m.completionDoc(docW)
	}

	layer := list
	if doc != "" {
		dw, dh := lipgloss.Size(doc)
		// The doc panel is aligned with the list's edge nearest the cursor
		// and must fit without covering the cursor's line.
		dy := y
		if y < cy {
			dy = y + lh - dh // list is above the cursor: align bottoms
		}
		fits := dy >= headerHeight && dy+dh <= bodyBottom
		switch {
		case !fits:
		case x+lw+dw <= m.width:
			layer = placeSide(list, doc, y < cy, false)
		case x-dw >= 0:
			layer = placeSide(list, doc, y < cy, true)
			x -= dw
			zones = translate(zones, dw, 0)
		}
		if layer != list && dy < y {
			zones = translate(zones, 0, y-dy)
			y = dy
		}
	}
	m.comp.rect = image.Rect(x, y, x+lipgloss.Width(layer), y+lipgloss.Height(layer))
	return layer, x, y, zones
}

// completionDoc renders the documentation panel for the selected item,
// wrapping text at width columns.
func (m *Model) completionDoc(width int) string {
	if m.comp.idx >= len(m.comp.items) {
		return ""
	}
	it := m.comp.items[m.comp.idx]
	// Skip the panel when it would only repeat what the list shows.
	if strings.TrimSpace(it.Doc) == "" && lipgloss.Width(it.Detail) <= completionDetailW &&
		lipgloss.Width(it.Label) <= completionLabelMax {
		return ""
	}
	var parts []string
	if it.Detail != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(colInfo).Render(ansi.Wrap(it.Label+" "+it.Detail, width, "")))
	}
	if d := strings.TrimSpace(it.Doc); d != "" {
		wrapped := strings.Split(ansi.Wrap(d, width, ""), "\n")
		if len(wrapped) > completionDocRows {
			wrapped = append(wrapped[:completionDocRows], "…")
		}
		parts = append(parts, m.theme.dim.Render(strings.Join(wrapped, "\n")))
	}
	if len(parts) == 0 {
		return ""
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colSubtle).
		Padding(0, 1).Render(strings.Join(parts, "\n\n"))
}

// placeSide joins the list and doc panel horizontally. When the popup is
// above the cursor the panels are bottom-aligned so both hug the cursor.
func placeSide(list, doc string, above, docLeft bool) string {
	pos := lipgloss.Top
	if above {
		pos = lipgloss.Bottom
	}
	if docLeft {
		return lipgloss.JoinHorizontal(pos, doc, list)
	}
	return lipgloss.JoinHorizontal(pos, list, doc)
}
