// Package runner executes notebooks headlessly, printing styled output.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	"github.com/mark3labs/gopyter/internal/htmlview"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/markdown"
	"github.com/mark3labs/gopyter/internal/notebook"
	"github.com/mark3labs/gopyter/internal/palette"
	"github.com/mark3labs/gopyter/internal/termimg"
)

var (
	labelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Dark.Primary)).Bold(true)
	codeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Dark.Muted))
	outStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Dark.Text))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Dark.Error))
	stderrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Dark.Orange))
	resultStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Dark.Info))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Dark.Success))
)

// write and writeln write styled output. Write errors (e.g. a closed pipe)
// are intentionally ignored: output is best-effort and results are still
// recorded in the notebook.
func write(w io.Writer, a ...any)   { _, _ = lipgloss.Fprint(w, a...) }
func writeln(w io.Writer, a ...any) { _, _ = lipgloss.Fprintln(w, a...) }

// paint styles each line of s individually, preserving newlines, so that
// streamed chunks aren't padded into blocks.
func paint(st lipgloss.Style, s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = st.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

// md renders markdown in gopyter's default dark colors.
var md = markdown.New(markdown.Colors{
	Heading: lipgloss.Color(palette.Dark.Primary), Info: lipgloss.Color(palette.Dark.Info), Accent: lipgloss.Color(palette.Dark.Accent),
	Warning: lipgloss.Color(palette.Dark.Warning), Success: lipgloss.Color(palette.Dark.Success), Error: lipgloss.Color(palette.Dark.Error),
	Code: lipgloss.Color(palette.Dark.Orange), Text: lipgloss.Color(palette.Dark.Text), Muted: lipgloss.Color(palette.Dark.Muted),
	Subtle: lipgloss.Color(palette.Dark.Subtle), CodeBg: lipgloss.Color(palette.Dark.Faint),
}, styles.Get(palette.Dark.Syntax))

func renderMarkdown(s string, width int) string {
	return strings.Join(md.Render(s, width, nil), "\n")
}

// renderImage draws a base64 image, or describes it when draw is false.
func renderImage(b64 string, draw bool, width int, d termimg.Drawer) string {
	img, err := termimg.Decode(b64)
	if err != nil {
		return errStyle.Render("can't show image: " + err.Error())
	}
	if !draw {
		return resultStyle.Render("=> " + termimg.Description(img))
	}
	return strings.Join(termimg.Draw(d, img, b64, width, 40), "\n")
}

// htmlStyles draw HTML outputs; widgets are drawn inactive.
var htmlStyles = htmlview.Styles{
	Text:        outStyle,
	Heading:     labelStyle,
	Code:        resultStyle,
	Link:        resultStyle.Underline(true),
	Muted:       codeStyle,
	Button:      outStyle.Background(lipgloss.Color(palette.Dark.Subtle)),
	ButtonFocus: outStyle.Background(lipgloss.Color(palette.Dark.Subtle)),
	ButtonOff:   lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Dark.Dim)).Background(lipgloss.Color(palette.Dark.Faint)),
	Accent:      labelStyle,
}

func renderHTML(s string, tty bool, width int, d termimg.Drawer) string {
	lines, _ := htmlview.Render(s, htmlview.Options{Width: width, Styles: htmlStyles, MaxImageLines: 40, NoImages: !tty, Images: d})
	return strings.Join(lines, "\n")
}

// events feeds a program's widgets: nobody can use them here, so they
// are done at once, but requests (like dom.GetInnerHtml) are answered.
type events struct {
	r, w    *os.File
	session *htmlview.Session
}

func newEvents() *events {
	r, w, err := os.Pipe()
	if err != nil {
		return &events{session: htmlview.NewSession()}
	}
	_, _ = io.WriteString(w, htmlview.DoneEvent) // fits the pipe's buffer
	return &events{r: r, w: w, session: htmlview.NewSession()}
}

func (e *events) reader() io.Reader {
	if e.r == nil {
		return nil
	}
	return e.r
}

func (e *events) reply(b []byte) {
	if e.w != nil && b != nil {
		_, _ = e.w.Write(b) // the program may be gone
	}
}

func (e *events) close() {
	for _, f := range []*os.File{e.w, e.r} {
		if f != nil {
			_ = f.Close()
		}
	}
}

// Run executes all code cells of nb, storing outputs in the notebook and
// printing them to w. Programs read stdin (nil for no input). It returns
// the number of failed cells.
func Run(ctx context.Context, k *kernel.Kernel, nb *notebook.Notebook, w io.Writer, stdin io.Reader, failFast bool) int {
	return RunWith(ctx, k, nb, w, stdin, Options{FailFast: failFast})
}

// RunWith executes a notebook with optional full-resolution image drawing.
func RunWith(ctx context.Context, k *kernel.Kernel, nb *notebook.Notebook, w io.Writer, stdin io.Reader, opts Options) int {
	gfx := opts.Graphics
	if f, ok := w.(*os.File); !ok || !term.IsTerminal(f.Fd()) {
		gfx = nil
	} else if gfx != nil {
		cw, ch := termimg.CellSize(f)
		gfx.SetCellSize(cw, ch)
	}
	// Retain scrollback images across cells as well as within a cell.
	var scrollback []termimg.Key
	// Images are drawn with colored half blocks, which are noise without
	// colors (e.g. when the output is redirected to a file). The same goes
	// for redrawing DisplayID outputs in place.
	tty := colorprofile.Detect(w, os.Environ()) >= colorprofile.ANSI
	failed, count := 0, 0
	for _, c := range nb.Cells {
		if c.Type != notebook.Code || strings.TrimSpace(c.Source) == "" {
			continue
		}
		count++
		name := fmt.Sprintf("In[%d]", count)

		writeln(w, labelStyle.Render(name))
		for l := range strings.SplitSeq(strings.TrimRight(c.Source, "\n"), "\n") {
			writeln(w, codeStyle.Render("  │ "+l))
		}

		bl := newBlocks(w, tty)
		bl.gfx, bl.savedImages = gfx, scrollback
		cellOpts := Options{Images: tty, Redraw: tty, Width: 100}
		start := time.Now()
		err := runCell(ctx, k, c, count, w, stdin, cellOpts, bl)
		scrollback = bl.savedImages
		if err != nil {
			failed++
			writeln(w, errStyle.Render("✗ "+err.Error()))
			if opts.FailFast || errors.Is(err, kernel.ErrInterrupted) {
				return failed
			}
		} else {
			writeln(w, okStyle.Render("✓ ")+codeStyle.Render(time.Since(start).Round(time.Millisecond).String()))
		}
		writeln(w)
	}
	return failed
}

// Options control output rendering independently of in-place updates.
type Options struct {
	FailFast bool           // stop a notebook run after the first failure
	Graphics *termimg.Kitty // optional full-resolution images for notebook runs
	Images   bool           // draw images rather than placeholders
	Redraw   bool           // redraw the last updated output in place
	Width    int            // rendering width; zero defaults to 100
}

// RunCell executes one input, replacing its outputs and execution count.
// It prints outputs but neither source nor a success/error status line.
// Widgets are done at once; stdin may be nil for no input.
func RunCell(ctx context.Context, k *kernel.Kernel, c *notebook.Cell, count int, w io.Writer, stdin io.Reader, opts Options) error {
	if opts.Width <= 0 {
		opts.Width = 100
	}
	return runCell(ctx, k, c, count, w, stdin, opts, newBlocks(w, opts.Redraw))
}

// runCell shares rendering with notebook runs while retaining their image scrollback.
func runCell(ctx context.Context, k *kernel.Kernel, c *notebook.Cell, count int, w io.Writer, stdin io.Reader, opts Options, bl *blocks) error {
	bl.opts = opts
	c.ExecutionCount, c.Outputs = count, nil
	name := fmt.Sprintf("In[%d]", count)
	appendOut := func(kind notebook.OutputKind, text, id string) int {
		var i int
		c.Outputs, i = notebook.AddOutput(c.Outputs, kind, text, id)
		return i
	}
	// Program output that styles itself (escape codes) is passed
	// through: write adapts its colors to w.
	rawOut := false
	stream := func(st lipgloss.Style, text string) {
		bl.other()
		rawOut = rawOut || strings.Contains(text, "\x1b")
		if rawOut {
			write(w, text)
		} else {
			write(w, paint(st, text))
		}
	}
	ev := newEvents()
	err := k.ExecuteInput(ctx, c.ID, name, c.Source, kernel.Input{Stdin: stdin, Events: ev.reader()}, func(e kernel.Event) {
		if e.Kind == kernel.Op {
			r := ev.session.Apply(c.Outputs, e.Text)
			if r.Changed {
				for i := range r.Outputs {
					if r.Outputs[i].Text != c.Outputs[i].Text {
						bl.showOutput(outputKey(r.Outputs, i), r.Outputs[i], r.Outputs[i].ID != "", false)
					}
				}
				c.Outputs = r.Outputs
			}
			ev.reply(r.Reply)
			return
		}
		kind, ok := e.Kind.OutputKind()
		if !ok {
			return
		}
		i := appendOut(kind, e.Text, e.ID)
		switch e.Kind {
		case kernel.Stdout:
			stream(outStyle, e.Text)
		case kernel.Stderr:
			stream(stderrStyle, e.Text)
		case kernel.Result, kernel.Markdown, kernel.Image, kernel.HTML:
			bl.showOutput(outputKey(c.Outputs, i), c.Outputs[i], e.ID != "", true)
		case kernel.Error:
			bl.other()
			writeln(w, paint(errStyle, e.Text))
		case kernel.Info:
			bl.other()
			writeln(w, codeStyle.Render("› "+e.Text))
		}
	})
	ev.close()
	bl.finish()
	if err != nil && !errors.Is(err, kernel.ErrCompile) {
		appendOut(notebook.Error, err.Error(), "")
	}
	return err
}
