package runner

import (
	"fmt"
	"io"
	"strings"

	"github.com/mark3labs/gopyter/internal/notebook"
	"github.com/mark3labs/gopyter/internal/termimg"
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
	gfx         *termimg.Kitty
	savedImages []termimg.Key // immutable scrollback
	lastImages  []termimg.Key
	blockImages map[string][]termimg.Key
	w           io.Writer
	redraw      bool
	opts        Options

	lastKey   string // the block printed last, if nothing followed it
	lastLines int

	order   []string // keys of pending blocks, in order
	pending map[string]string
}

func newBlocks(w io.Writer, redraw bool) *blocks {
	return &blocks{w: w, redraw: redraw, opts: Options{Images: redraw}, pending: map[string]string{}}
}

// outputKey identifies output i of outs across updates.
func outputKey(outs []notebook.Output, i int) string {
	if id := outs[i].ID; id != "" {
		return "id:" + id
	}
	return fmt.Sprint("#", i)
}

// showOutput tracks each block's image references before it is printed.
func (b *blocks) showOutput(key string, o notebook.Output, updatable, event bool) {
	var d termimg.Drawer
	var keys []termimg.Key
	if b.gfx != nil {
		d = b.gfx.Track(&keys)
	}
	opts := b.opts
	if opts.Width <= 0 {
		opts.Width = 100
	}
	text := renderOutput(o, opts, d)
	if b.blockImages == nil {
		b.blockImages = make(map[string][]termimg.Key)
	}
	b.blockImages[key] = keys
	b.show(key, text, updatable, event)
}

// show prints (or defers) a block. updatable blocks have an id; event is
// false for changes made by DOM operations.
func (b *blocks) show(key, text string, updatable, event bool) {
	switch {
	case b.redraw && key == b.lastKey:
		if b.lastLines > 0 {
			// Raw: lipgloss would strip it when the terminal was only
			// detected through the environment (e.g. CLICOLOR_FORCE).
			_, _ = fmt.Fprintf(b.w, "\x1b[%dA\r\x1b[J", b.lastLines) // output is best-effort
		}
		b.print(key, text)
	case !event, !b.redraw && updatable:
		if _, ok := b.pending[key]; !ok {
			b.order = append(b.order, key)
		}
		b.pending[key] = text
	default:
		b.print(key, text)
	}
}

func (b *blocks) print(key, text string) {
	if b.gfx != nil {
		if key != b.lastKey {
			b.savedImages = append(b.savedImages, b.lastImages...)
		}
		b.lastImages = b.blockImages[key]
		delete(b.blockImages, key)
		keep := append([]termimg.Key{}, b.savedImages...)
		keep = append(keep, b.lastImages...)
		for _, keys := range b.blockImages {
			keep = append(keep, keys...)
		}
		b.gfx.Retain(keep)
		_, _ = io.WriteString(b.w, b.gfx.Flush()) // best-effort, bypass color conversion
	}
	b.lastKey, b.lastLines = key, 0
	delete(b.pending, key)
	if text != "" {
		writeln(b.w, text)
		b.lastLines = strings.Count(text, "\n") + 1
	}
}

// other is called before anything else is printed: the last block can't
// be redrawn anymore.
func (b *blocks) other() {
	b.savedImages = append(b.savedImages, b.lastImages...)
	b.lastImages, b.lastKey = nil, ""
}

// finish prints the pending blocks, at the end of the cell.
func (b *blocks) finish() {
	for _, key := range b.order {
		if text, ok := b.pending[key]; ok && text != "" {
			b.print(key, text)
		}
	}
	b.other()
	b.order, b.pending, b.lastKey = nil, map[string]string{}, ""
}

// renderOutput draws a rich output.
func renderOutput(o notebook.Output, opts Options, d termimg.Drawer) string {
	switch o.Kind {
	case notebook.Result:
		return paint(resultStyle, "=> "+o.Text)
	case notebook.MarkdownOut:
		return renderMarkdown(o.Text, opts.Width)
	case notebook.ImageOut:
		return renderImage(o.Text, opts.Images, opts.Width, d)
	case notebook.HTMLOut:
		return renderHTML(o.Text, opts.Images, opts.Width, d)
	}
	return o.Text
}
