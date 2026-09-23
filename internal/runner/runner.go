// Package runner executes notebooks headlessly, printing styled output.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
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

// Run executes all code cells of nb, storing outputs in the notebook and
// printing them to w. It returns the number of failed cells.
func Run(ctx context.Context, k *kernel.Kernel, nb *notebook.Notebook, w io.Writer, failFast bool) int {
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

		appendOut := func(kind notebook.OutputKind, text string) {
			if n := len(c.Outputs); n > 0 && c.Outputs[n-1].Kind == kind && (kind == notebook.Stdout || kind == notebook.Stderr) {
				c.Outputs[n-1].Text += text
				return
			}
			c.Outputs = append(c.Outputs, notebook.Output{Kind: kind, Text: text})
		}
		start := time.Now()
		err := k.Execute(ctx, c.ID, name, c.Source, func(e kernel.Event) {
			switch e.Kind {
			case kernel.Stdout:
				appendOut(notebook.Stdout, e.Text)
				write(w, paint(outStyle, e.Text))
			case kernel.Stderr:
				appendOut(notebook.Stderr, e.Text)
				write(w, paint(stderrStyle, e.Text))
			case kernel.Result:
				appendOut(notebook.Result, e.Text)
				writeln(w, paint(resultStyle, "=> "+e.Text))
			case kernel.Markdown:
				appendOut(notebook.MarkdownOut, e.Text)
				writeln(w, renderMarkdown(e.Text))
			case kernel.Error:
				appendOut(notebook.Error, e.Text)
				writeln(w, paint(errStyle, e.Text))
			case kernel.Info:
				writeln(w, codeStyle.Render("› "+e.Text))
			}
		})
		if err != nil {
			failed++
			if !errors.Is(err, kernel.ErrCompile) {
				appendOut(notebook.Error, err.Error())
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
