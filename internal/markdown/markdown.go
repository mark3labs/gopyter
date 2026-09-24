// Package markdown renders Markdown for the terminal with herald-md, themed
// from gopyter's palette and wrapped to a width.
package markdown

import (
	"image/color"
	"slices"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/indaco/herald"
	heraldmd "github.com/indaco/herald-md"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Colors are the colors a Renderer draws with.
type Colors struct {
	Heading color.Color // H1, H2, table headers
	Info    color.Color // H3, links
	Accent  color.Color // H4, list bullets, "important" alerts
	Warning color.Color // H5, highlights, warning alerts
	Success color.Color // tip alerts
	Error   color.Color // caution alerts
	Code    color.Color // inline code text
	Text    color.Color
	Muted   color.Color // quotes, rules, H6
	Subtle  color.Color // table borders, kbd background
	CodeBg  color.Color // code block and inline code background
}

// Renderer renders Markdown with a fixed set of colors.
type Renderer struct {
	ty     *herald.Typography
	text   color.Color
	reflow bool
}

// Reflowing returns a copy of r that joins soft line breaks into spaces,
// reflowing paragraphs to the width.
func (r *Renderer) Reflowing() *Renderer {
	c := *r
	c.reflow = true
	return &c
}

// Parsers. md keeps the source's line breaks, the way the author laid out
// the text in the editor. reflowMD joins soft line breaks into spaces, for
// text whose breaks only fit some other width, like doc comments.
var (
	md       = heraldmd.NewRenderer()
	reflowMD = heraldmd.NewRenderer(goldmark.WithParserOptions(
		parser.WithASTTransformers(util.Prioritized(softBreaks{}, 1000)),
	))
)

// New returns a Renderer. code, when non-nil, highlights fenced code blocks
// that name a language.
func New(c Colors, code *chroma.Style) *Renderer {
	p := herald.ColorPalette{
		Primary: c.Heading, Secondary: c.Accent, Tertiary: c.Info,
		Accent: c.Warning, Highlight: c.Error, Muted: c.Muted,
		Text: c.Text, Surface: c.Subtle, Base: c.CodeBg,
	}
	th := herald.ThemeFromPalette(p)
	s := lipgloss.NewStyle
	// Blocks are separated by one blank line; margins would add another.
	th.H1 = s().Bold(true).Foreground(c.Heading)
	th.H2 = s().Bold(true).Foreground(c.Heading)
	th.H3 = s().Bold(true).Foreground(c.Info)
	th.H4 = s().Bold(true).Foreground(c.Accent)
	th.H5 = s().Bold(true).Foreground(c.Warning)
	th.H6 = s().Bold(true).Foreground(c.Muted)
	th.H3UnderlineChar = ""
	th.Paragraph = s().Foreground(c.Text)
	th.ListItem = s().Foreground(c.Text)
	th.ListBullet = s().Foreground(c.Accent)
	th.CodeInline = s().Foreground(c.Code).Background(c.CodeBg).Padding(0, 1)
	th.CodeBlock = s().Foreground(c.Text).Background(c.CodeBg).Padding(0, 1)
	th.Link = s().Foreground(c.Info).Underline(true)
	// Unstyled: inline styles inside a quote reset any quote-wide style
	// partway through the line, so the bar alone marks quotes.
	th.Blockquote = s()
	th.BlockquoteBarStyle = s().Foreground(c.Subtle)
	th.BlockquoteBar = "┃"
	th.HR = s().Foreground(c.Subtle)
	th.TableBorder = s().Foreground(c.Subtle)
	th.Alerts = herald.DefaultAlertConfigs(herald.AlertPalette{
		Note: c.Info, Tip: c.Success, Important: c.Accent, Warning: c.Warning, Caution: c.Error,
	})
	th.AlertBar = "┃"
	if code != nil {
		th.CodeFormatter = highlighter(code, c.Text, c.CodeBg)
	}
	// herald.New starts from its default theme, which queries the terminal
	// for its background color; that would race Bubble Tea for stdin. Apply
	// the theme to a zero Typography instead.
	ty := &herald.Typography{}
	herald.WithTheme(th)(ty)
	return &Renderer{ty: ty, text: c.Text}
}

// Render renders src and wraps it to width, returning its lines without
// leading or trailing blank lines. Unstyled text gets the text color, and
// when bg is non-nil every line is padded to width and filled with it.
func (r *Renderer) Render(src string, width int, bg color.Color) []string {
	// Tabs have no width in the terminal; indent struct fields and the like.
	src = strings.ReplaceAll(src, "\t", "    ")
	p := md
	if r.reflow {
		p = reflowMD
	}
	out := p.Render(r.ty, []byte(src))
	width = max(width, 8)

	var lines []string
	emit := func(l uv.Line) {
		l = trimRight(l)
		for x := range l {
			c := &l[x]
			if c.Width == 0 {
				continue
			}
			if c.Style.Fg == nil && c.Content != " " {
				c.Style.Fg = r.text
			}
			if bg != nil && c.Style.Bg == nil {
				c.Style.Bg = bg
			}
		}
		if bg != nil {
			for len(l) < width {
				c := uv.EmptyCell
				c.Style.Bg = bg
				l = append(l, c)
			}
		}
		lines = append(lines, l.Render())
	}
	blank := false
	// listIndent is the text column of the list item being rendered, or 0.
	listIndent := 0
	for s := range strings.SplitSeq(out, "\n") {
		l := trimRight(toCells(s))
		if len(l) == 0 {
			// Keep single blank lines between blocks, never runs of them.
			blank = len(lines) > 0
			listIndent = 0
			continue
		}
		if blank {
			emit(nil)
			blank = false
		}
		// herald starts the later lines of a multi-line list item at
		// column 0; line them up with the item's text instead.
		if pw, _, marker := hangingIndent(l); marker {
			listIndent = pw
		} else if listIndent > 0 && !plainSpace(l[0]) {
			l = append(slices.Repeat(uv.Line{uv.EmptyCell}, listIndent), l...)
		}
		for _, w := range wrapCells(l, width) {
			emit(w)
		}
	}
	return lines
}

// toCells parses a styled line into cells.
func toCells(s string) uv.Line {
	w := ansi.StringWidth(s)
	if w == 0 {
		return nil
	}
	buf := uv.NewScreenBuffer(w+2, 1)
	uv.NewStyledString(s).Draw(buf, buf.Bounds())
	return slices.Clone(buf.Line(0))
}

// plainSpace reports whether c is a space without a background, a place
// where lines may break. Padding of inline code has a background, so code
// spans stay in one piece.
func plainSpace(c uv.Cell) bool {
	return c.Content == " " && c.Style.Bg == nil &&
		c.Style.Underline == uv.UnderlineNone
}

// trimRight drops trailing unstyled spaces.
func trimRight(l uv.Line) uv.Line {
	for len(l) > 0 && (plainSpace(l[len(l)-1]) || l[len(l)-1].IsZero()) {
		l = l[:len(l)-1]
	}
	return l
}

// wrapCells word-wraps a line to width, keeping the indentation of list
// items and the bars of quotes and alerts on continuation lines. Lines that
// can't be wrapped sensibly (tables, rules, underlines) are cut.
func wrapCells(l uv.Line, width int) []uv.Line {
	if len(l) <= width {
		return []uv.Line{l}
	}
	cut := func() []uv.Line {
		out := slices.Clone(l[:width-1])
		// Don't leave half of a wide character behind.
		for len(out) > 0 && out[len(out)-1].Width != 1 && out[len(out)-1].Width != 0 {
			out = out[:len(out)-1]
		}
		ell := uv.EmptyCell
		ell.Content = "…"
		if len(out) > 0 {
			ell.Style = out[len(out)-1].Style
		}
		return []uv.Line{append(out, ell)}
	}
	if isTableLine(l) {
		return cut()
	}
	pw, bars, _ := hangingIndent(l)
	if pw > width/2 {
		pw, bars = 0, nil
	}
	words := splitWords(l[pw:])
	if len(words) <= 1 {
		// Nothing to break at: underlines, rules, long URLs.
		return cut()
	}
	cont := make(uv.Line, pw)
	for x := range cont {
		cont[x] = uv.EmptyCell
	}
	for _, b := range bars {
		cont[b] = l[b]
	}

	var out []uv.Line
	cur := slices.Clone(l[:pw])
	fresh := true // cur holds no word yet
	avail := width - pw
	for _, w := range words {
		// w.sep is the run of spaces before the word.
		word := w.word
		switch {
		case fresh:
		case len(cur)+len(w.sep)+len(word) <= width:
			cur = append(cur, w.sep...)
		default:
			out = append(out, cur)
			cur = slices.Clone(cont)
		}
		// Hard-break words longer than a whole line.
		for len(word) > avail {
			n := avail
			for n > 1 && word[n].Width == 0 {
				n-- // keep wide characters whole
			}
			out = append(out, append(cur, word[:n]...))
			cur, word = slices.Clone(cont), word[n:]
		}
		cur = append(cur, word...)
		fresh = false
	}
	return append(out, cur)
}

type wordCells struct{ sep, word uv.Line }

// splitWords splits a line into words at plain spaces.
func splitWords(l uv.Line) []wordCells {
	var ws []wordCells
	i := 0
	for i < len(l) {
		j := i
		for j < len(l) && plainSpace(l[j]) {
			j++
		}
		k := j
		for k < len(l) && !plainSpace(l[k]) {
			k++
		}
		if k > j {
			ws = append(ws, wordCells{sep: l[i:j], word: l[j:k]})
		}
		i = k
	}
	return ws
}

// hangingIndent returns the width of a line's leading marker (indentation,
// quote bars, a list bullet or number) and the columns of its bars, and
// whether it has a list marker.
func hangingIndent(l uv.Line) (int, []int, bool) {
	var bars []int
	x := 0
	for x < len(l) {
		if c := l[x].Content; c == "┃" || c == "│" || c == "▎" {
			bars = append(bars, x)
		} else if !plainSpace(l[x]) {
			break
		}
		x++
	}
	// A bullet or a list number, followed by a space.
	j := x
	if j < len(l) && strings.Contains("•◦▪▹-*", l[j].Content) && l[j].Content != "" {
		j++
	} else {
		for j < len(l) && len(l[j].Content) == 1 && (unicode.IsDigit(rune(l[j].Content[0])) || l[j].Content == ".") {
			j++
		}
		if j == x || l[j-1].Content != "." {
			j = x
		}
	}
	if j > x && j < len(l) && l[j].Content == " " {
		return j + 1, bars, true
	}
	return x, bars, false
}

// isTableLine reports whether a line is part of a box-drawn table.
func isTableLine(l uv.Line) bool {
	first, last := "", ""
	for _, c := range l {
		if c.Content != " " && c.Width > 0 {
			if first == "" {
				first = c.Content
			}
			last = c.Content
		}
	}
	switch first {
	case "┌", "├", "└", "╭", "╰", "┬", "┼":
		return true
	case "│":
		return last == "│"
	}
	return false
}

// highlighter returns a herald code formatter that colors code with style.
// Every token carries the code background, so it survives the resets
// between tokens.
func highlighter(style *chroma.Style, fg, bg color.Color) func(code, lang string) string {
	return func(code, lang string) string {
		lexer := lexers.Get(lang)
		if lexer == nil {
			return code
		}
		it, err := chroma.Coalesce(lexer).Tokenise(nil, code)
		if err != nil {
			return code
		}
		var sb strings.Builder
		for tok := it(); tok != chroma.EOF; tok = it() {
			e := style.Get(tok.Type)
			st := lipgloss.NewStyle().Foreground(fg).Background(bg)
			if e.Colour.IsSet() {
				st = st.Foreground(lipgloss.Color(e.Colour.String()))
			}
			if e.Bold == chroma.Yes {
				st = st.Bold(true)
			}
			if e.Italic == chroma.Yes {
				st = st.Italic(true)
			}
			// Style each line on its own so newlines stay unstyled.
			for i, part := range strings.Split(tok.Value, "\n") {
				if i > 0 {
					sb.WriteByte('\n')
				}
				if part != "" {
					sb.WriteString(st.Render(part))
				}
			}
		}
		return strings.TrimRight(sb.String(), "\n")
	}
}

// softBreaks turns soft line breaks into spaces.
type softBreaks struct{}

func (softBreaks) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	var soft []*ast.Text
	// Walk only collects: the tree can't change while it's walked.
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if t, ok := n.(*ast.Text); ok && entering && t.SoftLineBreak() {
			soft = append(soft, t)
		}
		return ast.WalkContinue, nil
	})
	for _, t := range soft {
		t.SetSoftLineBreak(false)
		t.Parent().InsertAfter(t.Parent(), t, ast.NewString([]byte(" ")))
	}
}
