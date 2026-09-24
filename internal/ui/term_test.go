package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func plainLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

func TestTermLines(t *testing.T) {
	base := lipgloss.NewStyle()
	for _, tc := range []struct {
		name, in string
		want     []string
	}{
		{"plain", "a\nb\n", []string{"a", "b"}},
		{"carriage return", "10%\r50%\n", []string{"50%"}},
		{"backspace", "ab\bc\n", []string{"ac"}},
		{"colors", "\x1b[31mred\x1b[0m ok\n", []string{"red ok"}},
		{"cursor up redraw", "a: 1\nb: 1\n\x1b[2A\x1b[Ka: 2\n\x1b[Kb: 2\n", []string{"a: 2", "b: 2"}},
		{"clear screen", "frame 1\n\x1b[2J\x1b[Hframe 2\n", []string{"frame 2"}},
		{"clear without home", "old\nold\n\x1b[2Jnew\n", []string{"new"}},
		{"erase to end", "hello world\r\x1b[5C\x1b[K\n", []string{"hello"}},
		{"position", "\x1b[2;3Hx\n", []string{"", "  x"}},
		{"osc title", "\x1b]0;title\x07hi\n", []string{"hi"}},
		{"incomplete tail", "done\n\x1b[3", []string{"done"}},
		{"wide", "\x1b[1m日本\x1b[m\n", []string{"日本"}},
	} {
		got := plainLines(termLines(tc.in, base, 80))
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTermLinesStyles(t *testing.T) {
	lines := termLines("\x1b[1;38;5;208mhot\x1b[22m \x1b[4;48;2;1;2;3mx\x1b[m\n", lipgloss.NewStyle(), 80)
	if len(lines) != 1 {
		t.Fatalf("lines %q", lines)
	}
	for _, want := range []string{"\x1b[1;38;5;208mhot", "\x1b[38;5;208m ", "\x1b[4;38;5;208;48;2;1;2;3mx"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("missing %q in %q", want, lines[0])
		}
	}
	// A wrapped styled line keeps its style on the next line, and every
	// line resets at its end.
	wrapped := termLines("\x1b[32m"+strings.Repeat("g", 10)+"\x1b[m\n", lipgloss.NewStyle(), 4)
	if len(wrapped) != 3 {
		t.Fatalf("wrapped %q", wrapped)
	}
	for _, l := range wrapped {
		if !strings.HasPrefix(l, "\x1b[32m") || !strings.HasSuffix(l, "\x1b[m") {
			t.Errorf("wrapped line not self-contained: %q", l)
		}
	}
}

func TestSetOutputReplaces(t *testing.T) {
	m := New(Options{Notebook: notebook.New(), Kernel: &kernel.Kernel{}})
	m.width, m.height = 80, 30
	c := m.cells[0]
	c.appendOutput(notebook.Stdout, "start\n")
	c.setOutput(notebook.Result, "1%", "p")
	c.appendOutput(notebook.Stdout, "more\n")
	lines, _, _ := m.renderOutputs(c, 60, false, "")
	c.outLines, c.outKey = lines, c.outputFingerprint(60, "", false)

	c.setOutput(notebook.Result, "99%", "p")
	if len(c.outputs) != 3 || c.outputs[1].Text != "99%" {
		t.Fatalf("outputs %+v", c.outputs)
	}
	// The render cache sees the change even though the last output didn't.
	if c.outputFingerprint(60, "", false) == c.outKey {
		t.Fatal("fingerprint unchanged after an update")
	}
	if got := plainLines(m.renderCell(0, 80).lines); !strings.Contains(strings.Join(got, "\n"), "99%") {
		t.Fatalf("render:\n%s", strings.Join(got, "\n"))
	}
}

// runCell runs the selected cell with a real kernel, feeding the program
// the given input lines through the UI, and returns the cell.
func runCell(t *testing.T, src string, input ...string) (*Model, *Cell) {
	t.Helper()
	k, err := kernel.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Close(); err != nil {
			t.Error(err)
		}
	})
	nb := &notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code, Source: src}}}
	m := New(Options{Notebook: nb, Kernel: k})
	m.width, m.height = 100, 40
	c := m.cells[0]
	m.mode = modeCommand
	m.queue = []string{c.id}
	cmd := m.startNext()
	if !m.stdinCell(c) {
		t.Fatal("no input line for the running cell")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "stdin ❯") {
		t.Fatalf("input line not shown:\n%s", view)
	}
	// enter on the running cell focuses its input.
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.in.focus != focusStdin || m.mode != modeCommand {
		t.Fatalf("enter should focus the input: focus=%q mode=%v", m.in.focus, m.mode)
	}
	for _, line := range input {
		for _, r := range line {
			m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	m.handleKey(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if m.in.stdin != nil || m.in.focus != "" || !m.in.done {
		t.Fatal("ctrl+d should end the input")
	}
	for m.running != nil {
		msg, ok := cmd().(runEventsMsg)
		if !ok {
			t.Fatal("unexpected message")
		}
		cmd = m.handleRunEvents(msg)
	}
	return m, c
}

func TestStdinInput(t *testing.T) {
	m, c := runCell(t, "sc := bufio.NewScanner(os.Stdin)\nfor sc.Scan() {\n\tfmt.Println(\"got\", sc.Text())\n}\nfmt.Println(\"eof\")", "one", "two")
	if c.status != statusOK {
		t.Fatalf("status %v: %+v", c.status, c.outputs)
	}
	out := plainOutput(c)
	// Typed lines are echoed like a terminal does; the program's output
	// may come before or after the echo of the next line.
	for _, want := range []string{"one\n", "two\n", "got one\n", "got two\n", "eof"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output %q", want, out)
		}
	}
	if m.stdinCell(c) || strings.Contains(ansi.Strip(m.View().Content), "stdin ❯") {
		t.Fatal("input line shown after the run")
	}
}
