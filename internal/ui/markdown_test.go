package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func TestMarkdownCellPanel(t *testing.T) {
	m := New(Options{
		Notebook: &notebook.Notebook{Cells: []*notebook.Cell{
			{ID: "a", Type: notebook.Code, Source: "x := 1"},
			{ID: "b", Type: notebook.Markdown, Source: "## Title\n\nSome prose that is long enough to wrap in a narrow window, " +
				"and then some more of it."},
		}},
		Kernel: &kernel.Kernel{},
	})
	m.width, m.height = 60, 40
	t.Cleanup(func() { setPalette(defaultTheme().palette(true)) })

	w := m.width - 1
	r := m.renderCell(1, w)
	lines := r.lines[:len(r.lines)-1] // the last line is the spacer
	if !r.ownSpacer || len(lines) < 5 {
		t.Fatalf("unexpected render: %q", r.lines)
	}
	top, bottom := ansi.Strip(lines[0]), ansi.Strip(lines[len(lines)-1])
	if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(top), "▌")) != "" ||
		strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(bottom), "▌")) != "" {
		t.Errorf("the panel should be padded with blank rows: %q / %q", top, bottom)
	}
	for _, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Errorf("panel line %q is %d wide, want %d", ansi.Strip(l), got, w)
		}
		// The accent bar sits at the panel's left edge on every row.
		if p := ansi.Strip(l); !strings.HasPrefix(p[len(strings.Repeat(" ", gutterWidth)):], "▌") {
			t.Errorf("line %q lacks the accent bar", p)
		}
	}
	text := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"▌  Title", "▌  Some prose"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestMarkdownOutputHasNoPanel(t *testing.T) {
	m := New(Options{
		Notebook: &notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code, Source: "x"}}},
		Kernel:   &kernel.Kernel{},
	})
	m.width, m.height = 60, 40
	c := m.cells[0]
	c.outputs = []notebook.Output{{Kind: notebook.MarkdownOut, Text: "**bold** text"}}
	out, _, _ := m.renderOutputs(c, 40, false, "")
	if len(out) != 1 || strings.TrimSpace(ansi.Strip(out[0])) != "bold text" {
		t.Fatalf("got %q", out)
	}
}

func TestCellsKeepClearOfScrollbar(t *testing.T) {
	m := New(Options{
		Notebook: &notebook.Notebook{Cells: []*notebook.Cell{
			{ID: "a", Type: notebook.Code, Source: "x := 1"},
			{ID: "b", Type: notebook.Markdown, Source: "# Title"},
		}},
		Kernel: &kernel.Kernel{},
	})
	m.width, m.height = 60, 20
	t.Cleanup(func() { setPalette(defaultTheme().palette(true)) })
	body, _ := m.renderBody()
	for l := range strings.SplitSeq(body, "\n") {
		// Before the scrollbar column, the margin must be blank.
		r := []rune(ansi.Strip(l))
		if len(r) < m.width {
			continue
		}
		if margin := string(r[m.width-1-cellMarginR : m.width-1]); strings.TrimSpace(margin) != "" {
			t.Errorf("line %q draws into the margin: %q", string(r), margin)
		}
	}
}
