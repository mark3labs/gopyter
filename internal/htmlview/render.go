// Package htmlview draws HTML output in the terminal and applies the
// front-end operations of a cell's program to it (DOM changes, widget
// values), for GoNB's gonbui, dom and widgets packages
// (https://github.com/janpfeifer/gonb), which draw these in a browser.
//
// HTML is drawn as text: inline formatting, headings, paragraphs, lists,
// tables, preformatted text and data-URL images. Styles and scripts are
// ignored. Buttons, sliders (<input type="range">) and selects with a
// data-address attribute are widgets: Render reports where they are drawn
// so the UI can make them interactive.
package htmlview

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mark3labs/gopyter/internal/termimg"
	"github.com/mattn/go-runewidth"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Kind is the kind of a widget.
type Kind int

// Widget kinds.
const (
	Button Kind = iota
	Slider
	Select
)

// Widget is an interactive element and where Render drew it.
type Widget struct {
	Address string
	Kind    Kind
	Label   string   // button
	Min     int      // slider
	Max     int      // slider
	Value   int      // slider value, or index of the selected option
	Options []string // select

	// Line is the index of the rendered line, and [X0, X1) its columns.
	Line, X0, X1 int
	// TrackX0 is the column of a slider's track (its value is Min there),
	// TrackW its width.
	TrackX0, TrackW int
}

// ValueAt returns the value of a slider at column x of its track.
func (w Widget) ValueAt(x int) int {
	if w.TrackW <= 1 || w.Max <= w.Min {
		return w.Min
	}
	f := float64(x-w.TrackX0) / float64(w.TrackW-1)
	f = math.Max(0, math.Min(1, f))
	return w.Min + int(math.Round(f*float64(w.Max-w.Min)))
}

// Styles are the styles HTML is drawn with.
type Styles struct {
	Text, Heading, Code, Link, Muted lipgloss.Style
	// Button is for buttons and selects; ButtonFocus when focused,
	// ButtonOff when the program no longer listens.
	Button, ButtonFocus, ButtonOff lipgloss.Style
	// Accent draws a slider's filled track and a focused knob.
	Accent lipgloss.Style
}

// Options configure Render.
type Options struct {
	Width  int
	Styles Styles
	// Live widgets are interactive; others are drawn as inactive.
	Live bool
	// Focus is the address of the focused widget.
	Focus string
	// MaxImageLines bounds the height of images.
	MaxImageLines int
	// NoImages describes images instead of drawing them, for plain text.
	NoImages bool
}

// Render draws HTML in lines at most opt.Width wide.
func Render(src string, opt Options) ([]string, []Widget) {
	nodes, err := parseFragment(src)
	if err != nil {
		return strings.Split(src, "\n"), nil
	}
	r := &renderer{opt: opt, l: layouter{width: max(opt.Width, 10)}}
	for _, n := range nodes {
		r.node(n, inline{})
	}
	return r.l.finish(r.widgets)
}

// Text draws HTML as plain text, for copying and non-terminal output.
func Text(src string) string {
	lines, _ := Render(src, Options{Width: 100, NoImages: true})
	return strings.Join(lines, "\n")
}

// parseFragment parses HTML as the contents of a <body>.
func parseFragment(src string) ([]*html.Node, error) {
	return html.ParseFragment(strings.NewReader(src), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
}

// inline is the inherited inline formatting.
type inline struct {
	bold, italic, underline, strike, code, link, heading, pre bool
}

func (in inline) style(st Styles) lipgloss.Style {
	s := st.Text
	switch {
	case in.code:
		s = st.Code
	case in.link:
		s = st.Link
	case in.heading:
		s = st.Heading
	}
	if in.bold {
		s = s.Bold(true)
	}
	if in.italic {
		s = s.Italic(true)
	}
	if in.underline {
		s = s.Underline(true)
	}
	if in.strike {
		s = s.Strikethrough(true)
	}
	return s
}

type renderer struct {
	opt     Options
	l       layouter
	widgets []Widget
}

func attr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

// textContent returns the text of n, with collapsed spaces.
func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

var blocks = map[atom.Atom]bool{
	atom.Div: true, atom.P: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true,
	atom.H5: true, atom.H6: true, atom.Ul: true, atom.Ol: true, atom.Li: true, atom.Pre: true,
	atom.Table: true, atom.Tr: true, atom.Blockquote: true, atom.Section: true, atom.Article: true,
	atom.Header: true, atom.Footer: true, atom.Form: true, atom.Figure: true, atom.Details: true,
	atom.Summary: true, atom.Dl: true, atom.Dt: true, atom.Dd: true, atom.Main: true, atom.Nav: true,
	atom.Center: true, atom.Fieldset: true,
}

// paragraphs are separated from what's around by a blank line.
var paragraphs = map[atom.Atom]bool{
	atom.P: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true,
	atom.H6: true, atom.Pre: true, atom.Table: true, atom.Blockquote: true,
}

func (r *renderer) children(n *html.Node, in inline) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.node(c, in)
	}
}

func (r *renderer) node(n *html.Node, in inline) {
	st := r.opt.Styles
	switch n.Type {
	case html.TextNode:
		if in.pre {
			r.l.preText(n.Data, in.style(st))
		} else {
			r.l.text(n.Data, in.style(st))
		}
		return
	case html.ElementNode:
	default:
		r.children(n, in)
		return
	}

	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Head, atom.Title, atom.Template, atom.Noscript:
		return
	case atom.Svg:
		r.l.word("[svg image]", st.Muted)
		return
	case atom.Canvas, atom.Iframe, atom.Video, atom.Audio, atom.Object, atom.Embed:
		r.l.word("["+n.Data+"]", st.Muted)
		return
	case atom.Br:
		r.l.newline()
		return
	case atom.Hr:
		r.l.block()
		r.l.rawLine(st.Muted.Render(strings.Repeat("─", r.l.width)))
		return
	case atom.Img:
		r.image(n)
		return
	case atom.Input:
		r.input(n)
		return
	case atom.Button:
		r.button(n)
		return
	case atom.Select:
		r.selectWidget(n)
		return
	case atom.Textarea:
		r.l.word("["+textContent(n)+"]", st.Muted)
		return
	case atom.B, atom.Strong:
		in.bold = true
	case atom.I, atom.Em, atom.Cite, atom.Var:
		in.italic = true
	case atom.U, atom.Ins:
		in.underline = true
	case atom.S, atom.Del, atom.Strike:
		in.strike = true
	case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
		in.code = true
	case atom.A:
		in.link = true
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Th, atom.Dt, atom.Summary:
		in.heading, in.bold = true, true
	case atom.Pre:
		in.pre, in.code = true, true
	}

	switch {
	case n.DataAtom == atom.Li:
		r.listItem(n, in)
		return
	case n.DataAtom == atom.Ul || n.DataAtom == atom.Ol || n.DataAtom == atom.Blockquote || n.DataAtom == atom.Dd:
		r.l.block()
		r.l.indent += 2
		r.children(n, in)
		r.l.block()
		r.l.indent -= 2
	case n.DataAtom == atom.Td || n.DataAtom == atom.Th:
		if n.PrevSibling != nil {
			r.l.word("│", st.Muted)
			r.l.space()
		}
		r.children(n, in)
		r.l.space()
	case paragraphs[n.DataAtom]:
		r.l.paragraph()
		r.children(n, in)
		r.l.paragraph()
	case blocks[n.DataAtom]:
		r.l.block()
		r.children(n, in)
		r.l.block()
	default:
		r.children(n, in)
	}
}

func (r *renderer) listItem(n *html.Node, in inline) {
	marker := "• "
	if p := n.Parent; p != nil && p.DataAtom == atom.Ol {
		i := 1
		for s := n.PrevSibling; s != nil; s = s.PrevSibling {
			if s.DataAtom == atom.Li {
				i++
			}
		}
		marker = strconv.Itoa(i) + ". "
	}
	r.l.block()
	r.l.marker = marker
	r.l.indent += runewidth.StringWidth(marker)
	r.children(n, in)
	r.l.block()
	r.l.indent -= runewidth.StringWidth(marker)
	r.l.marker = ""
}

func (r *renderer) image(n *html.Node) {
	src, _ := attr(n, "src")
	alt, _ := attr(n, "alt")
	if b64, ok := dataURL(src); ok {
		if img, err := termimg.Decode(b64); err == nil && r.opt.NoImages {
			r.l.word(termimg.Placeholder(img), r.opt.Styles.Muted)
			return
		} else if err == nil {
			lines := termimg.Render(img, r.l.width-r.l.indent, max(r.opt.MaxImageLines, 1))
			r.l.block()
			pad := strings.Repeat(" ", r.l.indent)
			for _, l := range lines {
				r.l.rawLine(pad + l)
			}
			return
		}
	}
	label := "[image]"
	if alt != "" {
		label = "[image: " + alt + "]"
	}
	r.l.word(label, r.opt.Styles.Muted)
}

// dataURL returns the base64 data of a data:image/...;base64, URL.
func dataURL(src string) (string, bool) {
	rest, ok := strings.CutPrefix(src, "data:image/")
	if !ok {
		return "", false
	}
	_, data, ok := strings.Cut(rest, ";base64,")
	return data, ok
}

func (r *renderer) input(n *html.Node) {
	typ, _ := attr(n, "type")
	st := r.opt.Styles
	switch strings.ToLower(typ) {
	case "range":
		w := Widget{Kind: Slider, Min: attrInt(n, "min", 0), Max: attrInt(n, "max", 100)}
		w.Value = attrInt(n, "value", (w.Min+w.Max)/2)
		w.Address, _ = attr(n, "data-address")
		r.widget(w)
	case "checkbox":
		mark := "☐"
		if _, ok := attr(n, "checked"); ok {
			mark = "☑"
		}
		r.l.word(mark, st.Text)
	case "hidden":
	default:
		v, _ := attr(n, "value")
		r.l.word("["+v+"]", st.Muted)
	}
}

func attrInt(n *html.Node, key string, def int) int {
	v, ok := attr(n, key)
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return def
	}
	return int(math.Round(f))
}

func (r *renderer) button(n *html.Node) {
	w := Widget{Kind: Button, Label: textContent(n)}
	w.Address, _ = attr(n, "data-address")
	r.widget(w)
}

func (r *renderer) selectWidget(n *html.Node) {
	w := Widget{Kind: Select}
	w.Address, _ = attr(n, "data-address")
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.DataAtom == atom.Option {
				if _, ok := attr(c, "selected"); ok {
					w.Value = len(w.Options)
				}
				w.Options = append(w.Options, textContent(c))
				continue
			}
			walk(c)
		}
	}
	walk(n)
	r.widget(w)
}

// widget draws a widget inline.
func (r *renderer) widget(w Widget) {
	st := r.opt.Styles
	live := r.opt.Live && w.Address != ""
	focused := live && w.Address == r.opt.Focus
	btn := st.ButtonOff
	switch {
	case focused:
		btn = st.ButtonFocus
	case live:
		btn = st.Button
	}
	var out string
	var width, trackOff int
	switch w.Kind {
	case Button:
		out = btn.Render(" " + w.Label + " ")
	case Select:
		label := ""
		if w.Value >= 0 && w.Value < len(w.Options) {
			label = w.Options[w.Value]
		}
		out = btn.Render(" " + label + " ▾ ")
	case Slider:
		w.TrackW = min(max(r.l.width/3, 10), 30)
		pos := 0
		if w.Max > w.Min {
			f := float64(w.Value-w.Min) / float64(w.Max-w.Min)
			pos = int(math.Round(math.Max(0, math.Min(1, f)) * float64(w.TrackW-1)))
		}
		fill, knob, rest := st.Accent, st.Text, st.Muted
		if !live {
			fill, knob = st.Muted, st.Muted
		}
		if focused {
			knob = st.Accent
		}
		value := strconv.Itoa(w.Value)
		if focused {
			value = st.Accent.Bold(true).Render(value)
		} else {
			value = knob.Render(value)
		}
		out = fill.Render(strings.Repeat("━", pos)) + knob.Render("●") +
			rest.Render(strings.Repeat("─", w.TrackW-1-pos)) + " " + value
		width = w.TrackW + 1 + len(strconv.Itoa(w.Value))
	}
	if width == 0 {
		width = lipgloss.Width(out)
	}
	idx := -1
	if live {
		idx = len(r.widgets)
		r.widgets = append(r.widgets, w)
	}
	r.l.atom(out, width, idx, trackOff)
}

// layouter flows words and widgets into lines.

type seg struct {
	out    string // styled
	w      int
	widget int // index of the widget, or -1
	off    int // offset of a slider's track in the segment
}

type line struct {
	segs []seg
	w    int
	raw  bool // prerendered (images, rules)
}

type layouter struct {
	width   int
	lines   []line
	cur     line
	started bool // cur has content (or is a forced empty line)
	indent  int
	marker  string // list marker for the next line
	sp      bool   // a space before the next word
}

func (l *layouter) startLine() {
	if l.started {
		return
	}
	l.started = true
	lead := l.indent
	if l.marker != "" {
		lead -= runewidth.StringWidth(l.marker)
	}
	lead = max(lead, 0)
	if lead > 0 {
		l.cur.segs = append(l.cur.segs, seg{out: strings.Repeat(" ", lead), w: lead, widget: -1})
		l.cur.w += lead
	}
	if l.marker != "" {
		l.cur.segs = append(l.cur.segs, seg{out: l.marker, w: runewidth.StringWidth(l.marker), widget: -1})
		l.cur.w += runewidth.StringWidth(l.marker)
		l.marker = ""
	}
}

// atLineStart reports whether nothing but the indent is on the line.
func (l *layouter) atLineStart() bool {
	for _, s := range l.cur.segs {
		if strings.TrimSpace(s.out) != "" || s.widget >= 0 {
			return false
		}
	}
	return true
}

func (l *layouter) push() {
	l.lines = append(l.lines, l.cur)
	l.cur, l.started, l.sp = line{}, false, false
}

// newline ends the line, even an empty one (<br>).
func (l *layouter) newline() {
	l.startLine()
	l.push()
}

// block ends the line if it has content.
func (l *layouter) block() {
	if l.started && !l.atLineStart() {
		l.push()
	}
	l.sp = false
}

// paragraph ends the line and separates what follows with a blank line.
func (l *layouter) paragraph() {
	l.block()
	if n := len(l.lines); n > 0 && !isBlank(l.lines[n-1]) {
		l.lines = append(l.lines, line{})
	}
}

func isBlank(ln line) bool {
	if ln.raw {
		return false
	}
	for _, s := range ln.segs {
		if strings.TrimSpace(s.out) != "" || s.widget >= 0 {
			return false
		}
	}
	return true
}

func (l *layouter) space() { l.sp = true }

// place adds a segment of width w, wrapping first if it doesn't fit.
func (l *layouter) place(s seg) {
	l.startLine()
	sp := l.sp && !l.atLineStart()
	need := s.w
	if sp {
		need++
	}
	if l.cur.w+need > l.width && !l.atLineStart() {
		l.push()
		l.startLine()
		sp = false
	}
	if sp {
		l.cur.segs = append(l.cur.segs, seg{out: " ", w: 1, widget: -1})
		l.cur.w++
	}
	l.cur.segs = append(l.cur.segs, s)
	l.cur.w += s.w
	l.sp = false
}

// word adds an unbreakable word, split only if longer than a line.
func (l *layouter) word(w string, st lipgloss.Style) {
	for w != "" {
		avail := l.width - l.indent
		if runewidth.StringWidth(w) <= avail {
			l.place(seg{out: st.Render(w), w: runewidth.StringWidth(w), widget: -1})
			return
		}
		head := runewidth.Truncate(w, avail, "")
		if head == "" {
			head = string([]rune(w)[:1])
		}
		l.place(seg{out: st.Render(head), w: runewidth.StringWidth(head), widget: -1})
		w = w[len(head):]
		l.sp = false
		l.push()
	}
}

// text adds flowing text: runs of spaces become one (breakable) space.
func (l *layouter) text(s string, st lipgloss.Style) {
	if s == "" {
		return
	}
	if strings.TrimSpace(s) == "" {
		l.sp = true
		return
	}
	if isSpace(s[0]) {
		l.sp = true
	}
	for w := range strings.FieldsSeq(s) {
		l.word(w, st)
		l.sp = true
	}
	l.sp = isSpace(s[len(s)-1])
}

func isSpace(b byte) bool { return b == ' ' || b == '\n' || b == '\t' || b == '\r' || b == '\f' }

// preText adds preformatted text, keeping spaces and newlines.
func (l *layouter) preText(s string, st lipgloss.Style) {
	s = strings.TrimPrefix(s, "\n")
	for i, part := range strings.Split(s, "\n") {
		if i > 0 {
			l.newline()
		}
		part = strings.ReplaceAll(part, "\t", "    ")
		for part != "" {
			l.startLine()
			avail := max(l.width-l.cur.w, 1)
			head := runewidth.Truncate(part, avail, "")
			if head == "" {
				head = string([]rune(part)[:1])
			}
			l.cur.segs = append(l.cur.segs, seg{out: st.Render(head), w: runewidth.StringWidth(head), widget: -1})
			l.cur.w += runewidth.StringWidth(head)
			part = part[len(head):]
			if part != "" {
				l.push()
			}
		}
	}
}

// atom adds a widget.
func (l *layouter) atom(out string, w, widget, trackOff int) {
	l.place(seg{out: out, w: w, widget: widget, off: trackOff})
}

// rawLine adds a prerendered line.
func (l *layouter) rawLine(s string) {
	l.block()
	l.lines = append(l.lines, line{segs: []seg{{out: s, widget: -1}}, raw: true})
}

// finish returns the lines, and the widgets (indexed by the segments)
// with their positions.
func (l *layouter) finish(all []Widget) ([]string, []Widget) {
	l.block()
	for len(l.lines) > 0 && isBlank(l.lines[0]) {
		l.lines = l.lines[1:]
	}
	for len(l.lines) > 0 && isBlank(l.lines[len(l.lines)-1]) {
		l.lines = l.lines[:len(l.lines)-1]
	}
	var widgets []Widget
	out := make([]string, len(l.lines))
	for i, ln := range l.lines {
		var b strings.Builder
		x := 0
		for _, s := range ln.segs {
			if s.widget >= 0 {
				w := all[s.widget]
				w.Line, w.X0, w.X1 = i, x, x+s.w
				w.TrackX0 = x + s.off
				widgets = append(widgets, w)
			}
			b.WriteString(s.out)
			x += s.w
		}
		out[i] = b.String()
	}
	return out, widgets
}

// String describes a widget, for plain text.
func (w Widget) String() string {
	switch w.Kind {
	case Button:
		return "[" + w.Label + "]"
	case Select:
		if w.Value >= 0 && w.Value < len(w.Options) {
			return "[" + w.Options[w.Value] + " ▾]"
		}
		return "[▾]"
	}
	return fmt.Sprintf("[%d..%d: %d]", w.Min, w.Max, w.Value)
}
