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

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/mark3labs/gopyter/internal/htmlview"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
	"github.com/mark3labs/gopyter/internal/termimg"
)

var (
	labelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ADD8")).Bold(true)
	codeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#71717A"))
	outStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E4E4E7"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))
	stderrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FB923C"))
	resultStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#5DC9E2"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ADE80"))
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

func renderMarkdown(s string) string {
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(100))
	if err != nil {
		return s
	}
	out, err := r.Render(s)
	if err != nil {
		return s
	}
	return strings.Trim(out, "\n")
}

// renderImage draws a base64 image, or describes it when draw is false.
func renderImage(b64 string, draw bool) string {
	img, err := termimg.Decode(b64)
	if err != nil {
		return errStyle.Render("can't show image: " + err.Error())
	}
	if !draw {
		return resultStyle.Render("=> " + termimg.Placeholder(img))
	}
	return strings.Join(termimg.Render(img, 100, 40), "\n")
}

// htmlStyles draw HTML outputs; widgets are drawn inactive.
var htmlStyles = htmlview.Styles{
	Text:        outStyle,
	Heading:     labelStyle,
	Code:        resultStyle,
	Link:        resultStyle.Underline(true),
	Muted:       codeStyle,
	Button:      lipgloss.NewStyle().Foreground(lipgloss.Color("#E4E4E7")).Background(lipgloss.Color("#3F3F46")),
	ButtonFocus: lipgloss.NewStyle().Foreground(lipgloss.Color("#E4E4E7")).Background(lipgloss.Color("#3F3F46")),
	ButtonOff:   lipgloss.NewStyle().Foreground(lipgloss.Color("#A1A1AA")).Background(lipgloss.Color("#27272A")),
	Accent:      labelStyle,
}

func renderHTML(s string, tty bool) string {
	lines, _ := htmlview.Render(s, htmlview.Options{Width: 100, Styles: htmlStyles, MaxImageLines: 40, NoImages: !tty})
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
		c.ExecutionCount = count
		c.Outputs = nil
		name := fmt.Sprintf("In[%d]", count)

		writeln(w, labelStyle.Render(name))
		for l := range strings.SplitSeq(strings.TrimRight(c.Source, "\n"), "\n") {
			writeln(w, codeStyle.Render("  │ "+l))
		}

		appendOut := func(kind notebook.OutputKind, text, id string) {
			if id != "" {
				for i := range c.Outputs {
					if c.Outputs[i].ID == id {
						c.Outputs[i].Kind, c.Outputs[i].Text = kind, text
						return
					}
				}
			}
			if n := len(c.Outputs); n > 0 && c.Outputs[n-1].Kind == kind && (kind == notebook.Stdout || kind == notebook.Stderr) {
				c.Outputs[n-1].Text += text
				return
			}
			c.Outputs = append(c.Outputs, notebook.Output{Kind: kind, Text: text, ID: id})
		}
		// Program output that styles itself (escape codes) is passed
		// through: write adapts its colors to w.
		bl := newBlocks(w, tty)
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
		// rich stores a rich output and prints it.
		rich := func(kind notebook.OutputKind, text, id string) {
			appendOut(kind, text, id)
			i := len(c.Outputs) - 1
			for j := range c.Outputs {
				if id != "" && c.Outputs[j].ID == id {
					i = j
				}
			}
			bl.show(outputKey(c.Outputs, i), renderOutput(c.Outputs[i], tty), id != "", true)
		}
		ev := newEvents()
		start := time.Now()
		err := k.ExecuteInput(ctx, c.ID, name, c.Source, kernel.Input{Stdin: stdin, Events: ev.reader()}, func(e kernel.Event) {
			if e.Kind == kernel.Op {
				r := ev.session.Apply(c.Outputs, e.Text)
				if r.Changed {
					for i := range r.Outputs {
						if r.Outputs[i].Text != c.Outputs[i].Text {
							bl.show(outputKey(r.Outputs, i), renderOutput(r.Outputs[i], tty), r.Outputs[i].ID != "", false)
						}
					}
					c.Outputs = r.Outputs
				}
				ev.reply(r.Reply)
				return
			}
			switch e.Kind {
			case kernel.Stdout:
				appendOut(notebook.Stdout, e.Text, "")
				stream(outStyle, e.Text)
			case kernel.Stderr:
				appendOut(notebook.Stderr, e.Text, "")
				stream(stderrStyle, e.Text)
			case kernel.Result:
				rich(notebook.Result, e.Text, e.ID)
			case kernel.Markdown:
				rich(notebook.MarkdownOut, e.Text, e.ID)
			case kernel.Image:
				rich(notebook.ImageOut, e.Text, e.ID)
			case kernel.HTML:
				rich(notebook.HTMLOut, e.Text, e.ID)
			case kernel.Error:
				bl.other()
				appendOut(notebook.Error, e.Text, "")
				writeln(w, paint(errStyle, e.Text))
			case kernel.Info:
				bl.other()
				writeln(w, codeStyle.Render("› "+e.Text))
			}
		})
		ev.close()
		bl.finish()
		if err != nil {
			failed++
			if !errors.Is(err, kernel.ErrCompile) {
				appendOut(notebook.Error, err.Error(), "")
			}
			writeln(w, errStyle.Render("✗ "+err.Error()))
			if failFast || errors.Is(err, kernel.ErrInterrupted) {
				return failed
			}
		} else {
			writeln(w, okStyle.Render("✓ ")+codeStyle.Render(time.Since(start).Round(time.Millisecond).String()))
		}
		writeln(w)
	}
	return failed
}
