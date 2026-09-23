package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/notebook"
)

type cellStatus int

const (
	statusIdle cellStatus = iota
	statusQueued
	statusRunning
	statusOK
	statusFailed
	statusInterrupted
)

const maxOutputLines = 40

// Cell is a notebook cell in the UI.
type Cell struct {
	id       string
	kind     notebook.CellType
	ed       *Editor
	outputs  []notebook.Output
	count    int
	status   cellStatus
	errMsg   string
	started  time.Time
	duration time.Duration
	expanded bool
	metadata map[string]any

	mdSrc, mdOut string
	mdWidth      int

	outKey      outputKey
	outLines    []string
	outResultAt int
}

// outputKey fingerprints the outputs; they are append-only between resets.
type outputKey struct {
	width, n, last int
	kind           notebook.OutputKind
}

func (c *Cell) outputFingerprint(width int) outputKey {
	k := outputKey{width: width, n: len(c.outputs)}
	if k.n > 0 {
		k.last = len(c.outputs[k.n-1].Text)
		k.kind = c.outputs[k.n-1].Kind
	}
	return k
}

func langFor(kind notebook.CellType) string {
	if kind == notebook.Markdown {
		return "markdown"
	}
	if kind == notebook.Raw {
		return "plaintext"
	}
	return "go"
}

func newCell(kind notebook.CellType, src string) *Cell {
	return &Cell{id: notebook.NewID(), kind: kind, ed: NewEditor(langFor(kind), src)}
}

func fromNotebook(c *notebook.Cell) *Cell {
	cell := &Cell{
		id: c.ID, kind: c.Type, ed: NewEditor(langFor(c.Type), c.Source),
		outputs: c.Outputs, count: c.ExecutionCount, metadata: c.Metadata,
		status: diskStatus(c),
	}
	return cell
}

// diskStatus derives a cell's run status from its saved outputs.
func diskStatus(c *notebook.Cell) cellStatus {
	if c.ExecutionCount == 0 {
		return statusIdle
	}
	for _, o := range c.Outputs {
		if o.Kind == notebook.Error {
			return statusFailed
		}
	}
	return statusOK
}

func (c *Cell) toNotebook() *notebook.Cell {
	return &notebook.Cell{ID: c.id, Type: c.kind, Source: c.ed.Value(), ExecutionCount: c.count, Outputs: c.outputs, Metadata: c.metadata}
}

func (c *Cell) clone() *Cell {
	n := newCell(c.kind, c.ed.Value())
	n.outputs = append([]notebook.Output(nil), c.outputs...)
	return n
}

func (c *Cell) setKind(kind notebook.CellType) {
	c.kind = kind
	c.ed.SetLang(langFor(kind))
	if kind != notebook.Code {
		c.outputs, c.count, c.status = nil, 0, statusIdle
	}
}

func (c *Cell) appendOutput(kind notebook.OutputKind, text string) {
	if n := len(c.outputs); n > 0 && (kind == notebook.Stdout || kind == notebook.Stderr) && c.outputs[n-1].Kind == kind {
		c.outputs[n-1].Text += text
		// Keep memory bounded for chatty programs.
		if len(c.outputs[n-1].Text) > 4<<20 {
			t := c.outputs[n-1].Text
			c.outputs[n-1].Text = t[len(t)-(2<<20):]
		}
		return
	}
	c.outputs = append(c.outputs, notebook.Output{Kind: kind, Text: text})
}

// cellRender is the rendered form of a cell plus layout metadata.
type cellRender struct {
	lines   []string
	edTop   int // first line of the editor within lines, -1 if none
	edLeft  int // column where the editor starts
	ev      editorView
	hasEdit bool
	zones   []zone // clickable regions; y is relative to the cell's first line
	// ownSpacer is set when the last line doubles as the inter-cell spacer.
	ownSpacer bool
}

// normalizeOutput turns raw program output into printable lines.
func normalizeOutput(s string) []string {
	s = ansi.Strip(s)
	s = strings.TrimSuffix(s, "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		if j := strings.LastIndex(l, "\r"); j >= 0 {
			l = l[j+1:]
		}
		l = expandTabs(l)
		l = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, l)
		lines[i] = l
	}
	return lines
}

func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	x := 0
	for _, r := range s {
		if r == '\t' {
			n := tabWidth - x%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			x += n
			continue
		}
		b.WriteRune(r)
		x += runeWidth(r, x)
	}
	return b.String()
}

func wrapLines(lines []string, width int) []string {
	var out []string
	for _, l := range lines {
		if lipgloss.Width(l) <= width {
			out = append(out, l)
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(l, width, ""), "\n")...)
	}
	return out
}

func fmtDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return "<1ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.2fs", d.Seconds())
	default:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}

// boxTop draws a rounded top border with a left title and a right badge.
// The title is inserted verbatim after "╭─" (callers include any padding).
func boxTop(width int, border lipgloss.Style, title, badge string) string {
	left := border.Render("╭─") + title
	right := border.Render("╮")
	if badge != "" {
		right = " " + badge + " " + border.Render("─╮")
	}
	fill := width - lipgloss.Width(left) - lipgloss.Width(right)
	if fill < 0 {
		return border.Render("╭" + strings.Repeat("─", max(width-2, 0)) + "╮")
	}
	return left + border.Render(strings.Repeat("─", fill)) + right
}

func boxBottom(width int, border lipgloss.Style) string {
	return border.Render("╰" + strings.Repeat("─", max(width-2, 0)) + "╯")
}

func padRight(s string, w int) string {
	if n := lipgloss.Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}
