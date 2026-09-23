package ui

import (
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// highlighter maps source code to per-rune lipgloss styles using chroma.
type highlighter struct {
	style *chroma.Style
	mu    sync.Mutex
	cache map[chroma.TokenType]lipgloss.Style
}

func newHighlighter(name string) *highlighter {
	st := styles.Get(name)
	if st == nil {
		st = styles.Fallback
	}
	return &highlighter{style: st, cache: map[chroma.TokenType]lipgloss.Style{}}
}

func (h *highlighter) styleFor(t chroma.TokenType) lipgloss.Style {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.cache[t]; ok {
		return s
	}
	e := h.style.Get(t)
	s := lipgloss.NewStyle()
	if e.Colour.IsSet() {
		s = s.Foreground(lipgloss.Color(e.Colour.String()))
	} else {
		s = s.Foreground(colText)
	}
	if e.Bold == chroma.Yes {
		s = s.Bold(true)
	}
	if e.Italic == chroma.Yes {
		s = s.Italic(true)
	}
	if e.Underline == chroma.Yes {
		s = s.Underline(true)
	}
	h.cache[t] = s
	return s
}

// tokenize returns, for each line, the token type of each rune.
func tokenize(lang string, lines [][]rune) [][]chroma.TokenType {
	out := make([][]chroma.TokenType, len(lines))
	for i, l := range lines {
		out[i] = make([]chroma.TokenType, len(l))
		for j := range out[i] {
			out[i][j] = chroma.Text
		}
	}
	lexer := lexers.Get(lang)
	if lexer == nil {
		return out
	}
	lexer = chroma.Coalesce(lexer)
	var src []rune
	for i, l := range lines {
		if i > 0 {
			src = append(src, '\n')
		}
		src = append(src, l...)
	}
	it, err := lexer.Tokenise(&chroma.TokeniseOptions{State: "root", EnsureLF: false}, string(src))
	if err != nil {
		return out
	}
	row, col := 0, 0
	for tok := it(); tok != chroma.EOF; tok = it() {
		for _, r := range tok.Value {
			if r == '\n' {
				row++
				col = 0
				continue
			}
			if row < len(out) && col < len(out[row]) {
				out[row][col] = tok.Type
			}
			col++
		}
	}
	return out
}
