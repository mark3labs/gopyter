package markdown

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Heading is an ATX or setext heading in markdown source.
type Heading struct {
	Level int
	Text  string
}

// Headings returns headings in source order, flattening inline formatting.
func Headings(src string) []Heading {
	source := []byte(src)
	root := goldmark.DefaultParser().Parse(text.NewReader(source))
	var out []Heading
	// Walk callbacks never return errors.
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		var b strings.Builder
		_ = ast.Walk(h, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if entering {
				switch n := n.(type) {
				case *ast.Text:
					b.Write(n.Value(source))
				case *ast.String:
					b.Write(n.Value)
				}
			}
			return ast.WalkContinue, nil
		})
		out = append(out, Heading{h.Level, b.String()})
		return ast.WalkSkipChildren, nil
	})
	return out
}
