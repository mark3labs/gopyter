package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// newVimModel returns a vim-enabled model in normal mode on the first cell,
// with the cursor at (row, col).
func newVimModel(t *testing.T, row, col int, sources ...string) *Model {
	t.Helper()
	nb := &notebook.Notebook{}
	for i, src := range sources {
		nb.Cells = append(nb.Cells, &notebook.Cell{ID: itoa(i), Type: notebook.Code, Source: src})
	}
	m := New(Options{Notebook: nb, Vim: true})
	m.width, m.height = 100, 30
	m.enterEdit()
	m.cur().ed.SetCursor(row, col)
	if m.vim.mode != vimNormal {
		t.Fatalf("want normal mode, got %v", m.vim.mode)
	}
	return m
}

// vimType sends keys: each rune is a key, and <name> sends a named key
// such as <esc>, <enter> or <c-r>.
func vimType(m *Model, keys string) {
	for keys != "" {
		var msg tea.KeyPressMsg
		if strings.HasPrefix(keys, "<") {
			end := strings.IndexByte(keys, '>')
			name := keys[1:end]
			keys = keys[end+1:]
			switch name {
			case "esc":
				msg = tea.KeyPressMsg{Code: tea.KeyEscape}
			case "enter":
				msg = tea.KeyPressMsg{Code: tea.KeyEnter}
			case "c-r":
				msg = tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
			default:
				panic("unknown key " + name)
			}
		} else {
			r := []rune(keys)[0]
			keys = keys[len(string(r)):]
			msg = tea.KeyPressMsg{Code: r, Text: string(r)}
		}
		m.handleKey(msg)
	}
}

func assertEditor(t *testing.T, m *Model, want string, row, col int) {
	t.Helper()
	ed := m.cur().ed
	if got := ed.Value(); got != want {
		t.Fatalf("text: got %q want %q", got, want)
	}
	if r, c := ed.Cursor(); r != row || c != col {
		t.Fatalf("cursor: got %d:%d want %d:%d", r, c, row, col)
	}
}

func TestVimEdits(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		row, col int
		keys     string
		want     string
		wr, wc   int
	}{
		{"x", "abc", 0, 1, "x", "ac", 0, 1},
		{"count x", "abcdef", 0, 1, "3x", "aef", 0, 1},
		{"x at end", "abc", 0, 2, "x", "ab", 0, 1},
		{"X", "abc", 0, 2, "X", "ac", 0, 1},
		{"dw", "foo bar baz", 0, 0, "dw", "bar baz", 0, 0},
		{"d2w", "foo bar baz", 0, 0, "d2w", "baz", 0, 0},
		{"2dw", "foo bar baz", 0, 0, "2dw", "baz", 0, 0},
		{"dw last word keeps newline", "a foo\nbar", 0, 2, "dw", "a \nbar", 0, 1},
		{"dw punctuation", "fmt.Println(x)", 0, 0, "dw", ".Println(x)", 0, 0},
		{"dW", "fmt.Println(x) y", 0, 0, "dW", "y", 0, 0},
		{"de", "foo bar", 0, 0, "de", " bar", 0, 0},
		{"db", "foo bar", 0, 4, "db", "bar", 0, 0},
		{"d$", "foo bar", 0, 3, "d$", "foo", 0, 2},
		{"D", "foo bar", 0, 3, "D", "foo", 0, 2},
		{"d0", "foo bar", 0, 4, "d0", "bar", 0, 0},
		{"dd", "a\nb\nc", 1, 0, "dd", "a\nc", 1, 0},
		{"2dd", "a\nb\nc", 0, 0, "2dd", "c", 0, 0},
		{"dd last", "a\nb", 1, 0, "dd", "a", 0, 0},
		{"dd only", "abc", 0, 1, "dd", "", 0, 0},
		{"dj", "a\nb\nc", 0, 0, "dj", "c", 0, 0},
		{"dk", "a\nb\nc", 2, 0, "dk", "a", 0, 0},
		{"dG", "a\nb\nc", 1, 0, "dG", "a", 0, 0},
		{"dgg", "a\nb\nc", 1, 0, "dgg", "c", 0, 0},
		{"dd keeps indent target", "x\n\ty\nz", 0, 0, "ddj", "\ty\nz", 1, 0},
		{"cw", "foo bar", 0, 0, "cwxy<esc>", "xy bar", 0, 1},
		{"cw on single char", "a bar", 0, 0, "cwz<esc>", "z bar", 0, 0},
		{"cc keeps indent", "\tfoo\nbar", 0, 2, "ccx<esc>", "\tx\nbar", 0, 1},
		{"C", "foo bar", 0, 4, "Cx<esc>", "foo x", 0, 4},
		{"s", "abc", 0, 1, "sX<esc>", "aXc", 0, 1},
		{"S", "a\n  bc", 1, 3, "Sx<esc>", "a\n  x", 1, 2},
		{"i", "ac", 0, 1, "ib<esc>", "abc", 0, 1},
		{"a", "ac", 0, 0, "ab<esc>", "abc", 0, 1},
		{"I", "  ab", 0, 3, "Ix<esc>", "  xab", 0, 2},
		{"A", "ab", 0, 0, "Ax<esc>", "abx", 0, 2},
		{"o", "func f() {\n}", 0, 0, "ox<esc>", "func f() {\n\tx\n}", 1, 1},
		{"O", "\ta\n", 0, 1, "Ox<esc>", "\tx\n\ta\n", 0, 1},
		{"J", "a\n   b", 0, 0, "J", "a b", 0, 1},
		{"yyp", "a\nb", 0, 0, "yyp", "a\na\nb", 1, 0},
		{"yyP", "a\nb", 1, 0, "yyP", "a\nb\nb", 1, 0},
		{"Y 2p", "a", 0, 0, "Y2p", "a\na\na", 1, 0},
		{"ddp swaps lines", "a\nb", 0, 0, "ddp", "b\na", 1, 0},
		{"yw P", "foo bar", 0, 4, "ywP", "foo barbar", 0, 6},
		{"xp swaps chars", "ab", 0, 0, "xp", "ba", 0, 1},
		{"u undoes a command", "foo bar", 0, 0, "dwu", "foo bar", 0, 0},
		{"u ctrl+r", "foo bar", 0, 0, "dwu<c-r>", "bar", 0, 0},
		{"u after change", "foo bar", 0, 0, "cwx<esc>uu", "foo bar", 0, 0},
		{"viwd", "foo bar", 0, 1, "vld", "f bar", 0, 1},
		{"v$y then P", "ab cd", 0, 3, "v$y0P", "cdab cd", 0, 1},
		{"Vjd", "a\nb\nc", 0, 0, "Vjd", "c", 0, 0},
		{"Vc", "\ta\nb", 0, 1, "Vcx<esc>", "\tx\nb", 0, 1},
		{"v o swaps", "abcd", 0, 1, "vlohd", "d", 0, 0},
		{"visual esc", "abc", 0, 0, "vl<esc>x", "ac", 0, 1},
		{"esc cancels op", "abc", 0, 0, "d<esc>x", "bc", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newVimModel(t, tc.row, tc.col, tc.src)
			vimType(m, tc.keys)
			assertEditor(t, m, tc.want, tc.wr, tc.wc)
			if m.mode != modeEdit || m.vim.mode != vimNormal {
				t.Fatalf("want edit/normal, got mode=%v vim=%v", m.mode, m.vim.mode)
			}
		})
	}
}

func TestVimMotions(t *testing.T) {
	src := "func main() {\n\tx := foo.Bar(1)\n\n\treturn\n}"
	cases := []struct {
		name     string
		row, col int
		keys     string
		wr, wc   int
	}{
		{"l", 0, 0, "l", 0, 1},
		{"l stops at last char", 0, 12, "l", 0, 12},
		{"3l", 0, 0, "3l", 0, 3},
		{"h at col 0", 0, 0, "h", 0, 0},
		{"j keeps display column", 0, 5, "j", 1, 2},
		{"j clamps to line", 1, 10, "j", 2, 0},
		{"w", 0, 0, "w", 0, 5},
		{"w punctuation", 0, 5, "w", 0, 9},
		{"w across lines", 0, 12, "w", 1, 1},
		{"w stops on empty line", 1, 15, "w", 2, 0},
		{"W", 1, 6, "W", 2, 0},
		{"b", 0, 5, "b", 0, 0},
		{"b across lines", 1, 1, "b", 0, 12},
		{"e", 0, 0, "e", 0, 3},
		{"e from end of word", 0, 3, "e", 0, 8},
		{"$", 0, 0, "$", 0, 12},
		{"0", 1, 5, "0", 1, 0},
		{"^", 1, 5, "^", 1, 1},
		{"G", 0, 0, "G", 4, 0},
		{"gg", 3, 3, "gg", 0, 0},
		{"2G", 0, 0, "2G", 1, 1},
		{"enter", 0, 0, "<enter>", 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newVimModel(t, tc.row, tc.col, src)
			vimType(m, tc.keys)
			assertEditor(t, m, src, tc.wr, tc.wc)
		})
	}
}

func TestVimModes(t *testing.T) {
	m := newVimModel(t, 0, 0, "a", "b")

	// j at the last line continues into the next cell, in normal mode.
	vimType(m, "j")
	if m.sel != 1 || m.mode != modeEdit || m.vim.mode != vimNormal {
		t.Fatalf("j across cells: sel=%d mode=%v vim=%v", m.sel, m.mode, m.vim.mode)
	}

	// esc from normal goes to command mode; enter comes back to normal.
	vimType(m, "<esc>")
	if m.mode != modeCommand {
		t.Fatalf("esc: mode=%v", m.mode)
	}
	vimType(m, "<enter>")
	if m.mode != modeEdit || m.vim.mode != vimNormal {
		t.Fatalf("enter: mode=%v vim=%v", m.mode, m.vim.mode)
	}

	// Typing in normal mode doesn't insert text.
	vimType(m, "qz")
	if got := m.cur().ed.Value(); got != "b" {
		t.Fatalf("normal mode inserted text: %q", got)
	}

	// Insert → esc → normal, with pending counts shown in the footer.
	vimType(m, "i")
	if m.vim.mode != vimInsert {
		t.Fatalf("i: vim=%v", m.vim.mode)
	}
	vimType(m, "<esc>2d")
	if m.vim.pending() != "2d" {
		t.Fatalf("pending: %q", m.vim.pending())
	}
	if !strings.Contains(m.renderFooter(), "2d…") {
		t.Fatalf("footer lacks pending command: %q", m.renderFooter())
	}
	vimType(m, "<esc><esc>")
	if m.mode != modeCommand {
		t.Fatalf("esc esc: mode=%v", m.mode)
	}

	// New cells start in insert mode.
	vimType(m, "b")
	if m.mode != modeEdit || m.vim.mode != vimInsert {
		t.Fatalf("new cell: mode=%v vim=%v", m.mode, m.vim.mode)
	}
}

func TestVimCtrlRRedoesInNormalMode(t *testing.T) {
	m := newVimModel(t, 0, 0, "foo bar")
	vimType(m, "dwu<c-r>")
	if m.busy() {
		t.Fatalf("ctrl+r ran the cell instead of redoing")
	}
	assertEditor(t, m, "bar", 0, 0)
}

func TestVimMouseSelectionBecomesVisual(t *testing.T) {
	m := newVimModel(t, 0, 0, "foo bar")
	ed := m.cur().ed
	// A selection made outside vim (the mouse, select all) is exclusive.
	ed.SelectRange(Pos{0, 0}, Pos{0, 3})
	m.vimSync()
	if m.vim.mode != vimVisual {
		t.Fatalf("vim=%v", m.vim.mode)
	}
	if got := ed.SelectedText(); got != "foo" {
		t.Fatalf("selection: %q", got)
	}
	vimType(m, "d")
	assertEditor(t, m, " bar", 0, 0)

	// Dropping the selection returns to normal mode.
	vimType(m, "v")
	ed.ClearSelection()
	m.vimSync()
	if m.vim.mode != vimNormal {
		t.Fatalf("vim=%v", m.vim.mode)
	}
}

func TestVimDisabledKeepsEditBehaviour(t *testing.T) {
	nb := &notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code, Source: "ab"}}}
	m := New(Options{Notebook: nb})
	m.enterEdit()
	m.cur().ed.SetCursor(0, 2)
	vimType(m, "x<esc>")
	if got := m.cur().ed.Value(); got != "abx" || m.mode != modeCommand {
		t.Fatalf("got %q mode=%v", got, m.mode)
	}
}
