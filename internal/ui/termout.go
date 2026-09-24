package ui

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
)

// Program output is interpreted like a terminal would, for the parts that
// matter in a notebook: colors and text attributes (SGR), carriage
// returns and backspaces, cursor movement and erasing lines or the screen.
// That's what progress bars, spinners and redrawing demos use. The screen
// has no fixed size: it grows with the output, line 1 is the first line
// of the output and lines are only wrapped when drawn.

// maxTermInput bounds how much output is interpreted. Only the end of
// longer output is, which keeps re-rendering streaming output cheap.
const maxTermInput = 1 << 20

// termStyle is an SGR state: attributes plus colors, kept as their SGR
// parameters (e.g. "31", "38;5;208", "48;2;1;2;3").
type termStyle struct {
	attrs  uint8
	fg, bg string
}

const (
	attrBold uint8 = 1 << iota
	attrFaint
	attrItalic
	attrUnderline
	attrBlink
	attrReverse
	attrStrike
)

var attrCodes = []struct {
	attr uint8
	code string
}{
	{attrBold, "1"}, {attrFaint, "2"}, {attrItalic, "3"}, {attrUnderline, "4"},
	{attrBlink, "5"}, {attrReverse, "7"}, {attrStrike, "9"},
}

func (s termStyle) sgr() string {
	var parts []string
	for _, a := range attrCodes {
		if s.attrs&a.attr != 0 {
			parts = append(parts, a.code)
		}
	}
	if s.fg != "" {
		parts = append(parts, s.fg)
	}
	if s.bg != "" {
		parts = append(parts, s.bg)
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

// apply updates the style with the parameters of an SGR sequence.
func (s *termStyle) apply(params string) {
	ps := strings.Split(strings.ReplaceAll(params, ":", ";"), ";")
	for i := 0; i < len(ps); i++ {
		n, err := strconv.Atoi(ps[i])
		if ps[i] == "" {
			n, err = 0, nil
		}
		if err != nil {
			continue
		}
		switch {
		case n == 0:
			*s = termStyle{}
		case n == 1:
			s.attrs |= attrBold
		case n == 2:
			s.attrs |= attrFaint
		case n == 3:
			s.attrs |= attrItalic
		case n == 4 || n == 21:
			s.attrs |= attrUnderline
		case n == 5 || n == 6:
			s.attrs |= attrBlink
		case n == 7:
			s.attrs |= attrReverse
		case n == 9:
			s.attrs |= attrStrike
		case n == 22:
			s.attrs &^= attrBold | attrFaint
		case n == 23:
			s.attrs &^= attrItalic
		case n == 24:
			s.attrs &^= attrUnderline
		case n == 25:
			s.attrs &^= attrBlink
		case n == 27:
			s.attrs &^= attrReverse
		case n == 29:
			s.attrs &^= attrStrike
		case n >= 30 && n <= 37, n >= 90 && n <= 97:
			s.fg = ps[i]
		case n == 39:
			s.fg = ""
		case n >= 40 && n <= 47, n >= 100 && n <= 107:
			s.bg = ps[i]
		case n == 49:
			s.bg = ""
		case n == 38 || n == 48:
			color, used := extendedColor(ps[i+1:])
			i += used
			if color == "" {
				continue
			}
			if n == 38 {
				s.fg = "38;" + color
			} else {
				s.bg = "48;" + color
			}
		}
	}
}

// extendedColor parses the rest of a 38/48 color ("5;n" or "2;r;g;b"),
// returning it and how many parameters it used.
func extendedColor(ps []string) (string, int) {
	valid := func(xs []string) bool {
		for _, x := range xs {
			if n, err := strconv.Atoi(x); err != nil || n < 0 || n > 255 {
				return false
			}
		}
		return true
	}
	switch {
	case len(ps) >= 2 && ps[0] == "5" && valid(ps[1:2]):
		return "5;" + ps[1], 2
	case len(ps) >= 4 && ps[0] == "2" && valid(ps[1:4]):
		return "2;" + strings.Join(ps[1:4], ";"), 4
	}
	return "", len(ps)
}

// termCell is one column of the screen. A wide rune's second column has
// r == 0; extra holds zero-width runes that combine with r.
type termCell struct {
	r     rune
	style uint16 // index in termScreen.styles; 0 is the default style
	extra string
}

type termScreen struct {
	lines    [][]termCell
	row, col int
	style    uint16
	styles   []termStyle
	styleIdx map[termStyle]uint16
	saved    [2]int
}

func newTermScreen() *termScreen {
	return &termScreen{styles: []termStyle{{}}, styleIdx: map[termStyle]uint16{{}: 0}}
}

func (t *termScreen) setStyle(s termStyle) {
	i, ok := t.styleIdx[s]
	if !ok {
		if len(t.styles) == 1<<16 {
			return // absurdly many styles: keep the current one
		}
		i = uint16(len(t.styles))
		t.styles = append(t.styles, s)
		t.styleIdx[s] = i
	}
	t.style = i
}

func (t *termScreen) line() []termCell {
	for len(t.lines) <= t.row {
		t.lines = append(t.lines, nil)
	}
	return t.lines[t.row]
}

func (t *termScreen) put(r rune, w int) {
	l := t.line()
	for len(l) < t.col+w {
		l = append(l, termCell{r: ' '})
	}
	l[t.col] = termCell{r: r, style: t.style}
	if w == 2 {
		l[t.col+1] = termCell{style: t.style}
	}
	t.lines[t.row] = l
	t.col += w
}

// erase blanks columns [from, to) of the current line; to < 0 means to
// the end.
func (t *termScreen) erase(from, to int) {
	l := t.line()
	if to < 0 || to > len(l) {
		t.lines[t.row] = l[:min(from, len(l))]
		return
	}
	for i := from; i < to; i++ {
		l[i] = termCell{r: ' '}
	}
}

// write interprets s. An escape sequence or rune cut off at the end is
// dropped: the rest arrives with the next chunk, and s is re-interpreted
// whole every time.
func (t *termScreen) write(s string) {
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b:
			n := t.escape(s[i:])
			if n == 0 {
				return
			}
			i += n
			continue
		case c == '\n':
			t.row++
			t.col = 0
			t.line()
		case c == '\r':
			t.col = 0
		case c == '\b':
			t.col = max(t.col-1, 0)
		case c == '\t':
			t.col += tabWidth - t.col%tabWidth
		case c < 0x20 || c == 0x7f:
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size <= 1 {
				if !utf8.FullRuneInString(s[i:]) {
					return
				}
				r = '\uFFFD'
			}
			i += size
			w := runewidth.RuneWidth(r)
			if w == 0 {
				// Combining: attach it to the previous column.
				if l := t.line(); t.col > 0 && t.col <= len(l) {
					l[t.col-1].extra += string(r)
				}
				continue
			}
			t.put(r, min(w, 2))
			continue
		}
		i++
	}
}

// escape interprets the escape sequence starting s, returning its length,
// or 0 if it's incomplete.
func (t *termScreen) escape(s string) int {
	if len(s) < 2 {
		return 0
	}
	switch s[1] {
	case '[':
		j := 2
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
			j++
		}
		if j >= len(s) {
			return 0
		}
		t.csi(s[2:j], s[j])
		return j + 1
	case ']', 'P', '_', '^':
		// OSC and friends (titles, hyperlinks): skip to BEL or ST.
		for j := 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return 0
	case '7':
		t.saved = [2]int{t.row, t.col}
	case '8':
		t.row, t.col = t.saved[0], t.saved[1]
	case 'c':
		*t = *newTermScreen()
	case '(', ')', '#', '%':
		if len(s) < 3 {
			return 0
		}
		return 3
	}
	return 2
}

func (t *termScreen) csi(params string, final byte) {
	if final == 'm' {
		if !strings.HasPrefix(params, "?") && !strings.HasPrefix(params, ">") {
			st := t.styles[t.style]
			st.apply(params)
			t.setStyle(st)
		}
		return
	}
	if strings.ContainsAny(params, "?<=>") {
		return // private modes (cursor visibility, alt screen...)
	}
	ps := strings.Split(params, ";")
	arg := func(i, def int) int {
		if i < len(ps) {
			if n, err := strconv.Atoi(ps[i]); err == nil && n > 0 {
				return n
			}
		}
		return def
	}
	switch final {
	case 'A':
		t.row = max(t.row-arg(0, 1), 0)
	case 'B':
		t.row += arg(0, 1)
	case 'C':
		t.col += arg(0, 1)
	case 'D':
		t.col = max(t.col-arg(0, 1), 0)
	case 'E':
		t.row, t.col = t.row+arg(0, 1), 0
	case 'F':
		t.row, t.col = max(t.row-arg(0, 1), 0), 0
	case 'G', '`':
		t.col = arg(0, 1) - 1
	case 'd':
		t.row = arg(0, 1) - 1
	case 'H', 'f':
		t.row, t.col = arg(0, 1)-1, arg(1, 1)-1
	case 'K':
		switch arg(0, 0) {
		case 0:
			t.erase(t.col, -1)
		case 1:
			t.erase(0, t.col+1)
		case 2:
			t.erase(0, -1)
		}
	case 'J':
		switch arg(0, 0) {
		case 0:
			t.erase(t.col, -1)
			t.lines = t.lines[:min(t.row+1, len(t.lines))]
		case 1:
			for r := 0; r < t.row && r < len(t.lines); r++ {
				t.lines[r] = nil
			}
			t.erase(0, t.col+1)
		default:
			// Clearing the screen starts the output over. The cursor
			// goes home too: output drawn after a clear should come
			// first, even without an explicit "\x1b[H".
			t.lines, t.row, t.col = nil, 0, 0
		}
	}
}

// termLines renders program output as screen lines, wrapped at width,
// with the text in the default style drawn with base. Each line is
// self-contained: it opens and closes its own styles.
func termLines(text string, base lipgloss.Style, width int) []string {
	if !strings.ContainsAny(text, "\x1b\b") {
		// Plain output: no need to interpret it.
		var out []string
		for _, l := range wrapLines(normalizeOutput(text), width) {
			out = append(out, base.Render(l))
		}
		return out
	}
	if len(text) > maxTermInput {
		text = text[len(text)-maxTermInput:]
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	t := newTermScreen()
	t.write(text)
	lines := t.lines
	// Like the plain path, a final newline doesn't add an empty line.
	if n := len(lines); n > 0 && len(lines[n-1]) == 0 {
		lines = lines[:n-1]
	}
	var out []string
	for _, l := range lines {
		out = append(out, t.render(l, base, width)...)
	}
	return out
}

// render draws a screen line, wrapped at width.
func (t *termScreen) render(l []termCell, base lipgloss.Style, width int) []string {
	// Trailing blanks in the default style are invisible: drop them.
	for len(l) > 0 && l[len(l)-1].style == 0 && (l[len(l)-1].r == ' ' || l[len(l)-1].r == 0) && l[len(l)-1].extra == "" {
		l = l[:len(l)-1]
	}
	if len(l) == 0 {
		return []string{""}
	}
	width = max(width, 2)
	var out []string
	var sb, run strings.Builder
	runStyle := uint16(0)
	flush := func() {
		if run.Len() == 0 {
			return
		}
		if runStyle == 0 {
			sb.WriteString(base.Render(run.String()))
		} else {
			sb.WriteString(t.styles[runStyle].sgr())
			sb.WriteString(run.String())
			sb.WriteString("\x1b[m")
		}
		run.Reset()
	}
	x := 0
	for i, c := range l {
		if c.r == 0 {
			if i == 0 || l[i-1].r == 0 {
				c.r = ' ' // an orphaned half of a wide rune
			} else {
				continue
			}
		}
		w := 1
		if i+1 < len(l) && l[i+1].r == 0 && runewidth.RuneWidth(c.r) == 2 {
			w = 2
		}
		if x+w > width {
			flush()
			out = append(out, sb.String())
			sb.Reset()
			x = 0
		}
		if c.style != runStyle {
			flush()
			runStyle = c.style
		}
		run.WriteRune(c.r)
		run.WriteString(c.extra)
		x += w
	}
	flush()
	return append(out, sb.String())
}
