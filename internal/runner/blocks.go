package runner

import (
	"fmt"
	"io"
	"strings"

	"github.com/mark3labs/gopyter/internal/notebook"
)

// blocks prints a cell's rich outputs (results, markdown, images, HTML),
// some of which change after they're shown: DisplayID/UpdateHTML replace
// an output, and DOM operations edit HTML.
//
// On a terminal, a change to the last block printed redraws it in place;
// other changes are printed when the cell ends. Without a terminal
// nothing can be redrawn, so updatable outputs and changed ones are only
// printed when the cell ends, in their final state, instead of every
// frame of an animation.
type blocks struct {
	w   io.Writer
	tty bool

	lastKey   string // the block printed last, if nothing followed it
	lastLines int

	order   []string // keys of pending blocks, in order
	pending map[string]string
}

func newBlocks(w io.Writer, tty bool) *blocks {
	return &blocks{w: w, tty: tty, pending: map[string]string{}}
}

// outputKey identifies output i of outs across updates.
func outputKey(outs []notebook.Output, i int) string {
	if id := outs[i].ID; id != "" {
		return "id:" + id
	}
	return fmt.Sprint("#", i)
}

// show prints (or defers) a block. updatable blocks have an id; event is
// false for changes made by DOM operations.
func (b *blocks) show(key, text string, updatable, event bool) {
	switch {
	case b.tty && key == b.lastKey:
		if b.lastLines > 0 {
			// Raw: lipgloss would strip it when the terminal was only
			// detected through the environment (e.g. CLICOLOR_FORCE).
			_, _ = fmt.Fprintf(b.w, "\x1b[%dA\r\x1b[J", b.lastLines) // output is best-effort
		}
		b.print(key, text)
	case !event, !b.tty && updatable:
		if _, ok := b.pending[key]; !ok {
			b.order = append(b.order, key)
		}
		b.pending[key] = text
	default:
		b.print(key, text)
	}
}

func (b *blocks) print(key, text string) {
	b.lastKey, b.lastLines = key, 0
	delete(b.pending, key)
	if text != "" {
		writeln(b.w, text)
		b.lastLines = strings.Count(text, "\n") + 1
	}
}

// other is called before anything else is printed: the last block can't
// be redrawn anymore.
func (b *blocks) other() { b.lastKey = "" }

// finish prints the pending blocks, at the end of the cell.
func (b *blocks) finish() {
	for _, key := range b.order {
		if text, ok := b.pending[key]; ok && text != "" {
			writeln(b.w, text)
		}
	}
	b.order, b.pending, b.lastKey = nil, map[string]string{}, ""
}

// renderOutput draws a rich output.
func renderOutput(o notebook.Output, tty bool) string {
	switch o.Kind {
	case notebook.Result:
		return paint(resultStyle, "=> "+o.Text)
	case notebook.MarkdownOut:
		return renderMarkdown(o.Text)
	case notebook.ImageOut:
		return renderImage(o.Text, tty)
	case notebook.HTMLOut:
		return renderHTML(o.Text, tty)
	}
	return o.Text
}
