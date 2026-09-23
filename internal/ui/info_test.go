package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/complete"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// fakeDocumenter is a completer that also serves hover documentation.
type fakeDocumenter struct {
	fakeCompleter
	md  string
	err error
	req *complete.Request // records the last request
}

func (f fakeDocumenter) Hover(_ context.Context, req complete.Request) (string, error) {
	if f.req != nil {
		*f.req = req
	}
	return f.md, f.err
}

// runInfoCmds runs cmd synchronously, delivering hover results.
// Spinner and status timers are skipped.
func runInfoCmds(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runInfoCmds(m, c)
		}
	case infoResultMsg:
		// The returned command only clears the status later; skip its wait.
		_ = m.handleInfoResult(msg)
	}
}

func newInfoModel(t *testing.T, d Documenter, vim bool, src string) *Model {
	t.Helper()
	nb := &notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code, Source: src}}}
	var c Completer
	if d != nil {
		c = d.(Completer)
	}
	m := New(Options{Notebook: nb, Kernel: &kernel.Kernel{}, Completer: c, Vim: vim})
	m.width, m.height = 100, 30
	m.enterEdit()
	return m
}

var altK = tea.KeyPressMsg{Code: 'k', Mod: tea.ModAlt}

func screenText(m *Model) string {
	s, _ := m.renderScreen()
	return ansi.Strip(s)
}

func TestInfoPopup(t *testing.T) {
	var req complete.Request
	d := fakeDocumenter{md: "```go\nfunc strings.Split(s, sep string) []string\n```\n\nSplit slices s.", req: &req}
	m := newInfoModel(t, d, false, "x := strings.Split(a, b)")
	if !key.Matches(altK, m.keys.Info) {
		t.Fatal("alt+k should be the info key")
	}
	m.cur().ed.SetCursor(0, 15)

	runInfoCmds(m, m.handleKey(altK))
	if req.CellID != "a" || req.Row != 0 || req.Col != 15 || req.Src != "x := strings.Split(a, b)" {
		t.Fatalf("bad request: %+v", req)
	}
	if !m.infoVisible() || m.info.loading {
		t.Fatalf("popup not shown: %+v", m.info)
	}
	scr := screenText(m)
	if !strings.Contains(scr, "func strings.Split(s, sep string) []string") || !strings.Contains(scr, "Split slices s.") {
		t.Fatalf("popup content missing:\n%s", scr)
	}

	// esc only dismisses the popup; it doesn't leave edit mode.
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.info.open || m.mode != modeEdit {
		t.Fatalf("esc: open=%v mode=%v", m.info.open, m.mode)
	}

	// Other keys close it and still do their job.
	runInfoCmds(m, m.handleKey(altK))
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.info.open {
		t.Fatal("popup should close on cursor movement")
	}
	if _, col := m.cur().ed.Cursor(); col != 16 {
		t.Fatalf("right arrow not applied, col %d", col)
	}

	// The info key toggles the popup.
	runInfoCmds(m, m.handleKey(altK))
	m.handleKey(altK)
	if m.info.open {
		t.Fatal("second info key should close the popup")
	}
}

func TestInfoStaleResultDropped(t *testing.T) {
	m := newInfoModel(t, fakeDocumenter{md: "doc"}, false, "strings.Split")
	cmd := m.handleKey(altK)
	// The cursor moves before gopls answers.
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	runInfoCmds(m, cmd)
	if m.info.open || strings.Contains(screenText(m), "doc") {
		t.Fatal("a stale result must not open the popup")
	}
}

func TestInfoEmptyAndError(t *testing.T) {
	m := newInfoModel(t, fakeDocumenter{}, false, "x := 1")
	runInfoCmds(m, m.handleKey(altK))
	if m.info.open || !strings.Contains(m.status, "no info") {
		t.Fatalf("empty: open=%v status=%q", m.info.open, m.status)
	}

	m = newInfoModel(t, fakeDocumenter{err: errors.New("gopls not found")}, false, "x")
	runInfoCmds(m, m.handleKey(altK))
	if m.info.open || !strings.Contains(m.status, "gopls not found") {
		t.Fatalf("error: open=%v status=%q", m.info.open, m.status)
	}

	// Without a documentation backend the key just explains why.
	m = newInfoModel(t, nil, false, "x")
	_ = m.handleKey(altK) // only a status-clearing timer

	if m.info.open || !strings.Contains(m.status, "gopls") {
		t.Fatalf("no backend: open=%v status=%q", m.info.open, m.status)
	}
}

func TestInfoMarkdownCellIgnored(t *testing.T) {
	m := newInfoModel(t, fakeDocumenter{md: "doc"}, false, "")
	m.cur().kind = notebook.Markdown
	runInfoCmds(m, m.handleKey(altK))
	if m.info.open {
		t.Fatal("markdown cells have no symbol info")
	}
}

func TestInfoScroll(t *testing.T) {
	var b strings.Builder
	for i := range 60 {
		b.WriteString("line ")
		b.WriteString(itoa(i))
		b.WriteString("\n\n")
	}
	m := newInfoModel(t, fakeDocumenter{md: b.String()}, false, "x")
	runInfoCmds(m, m.handleKey(altK))
	scr := screenText(m)
	if !strings.Contains(scr, "line 0") || strings.Contains(scr, "line 59") || !strings.Contains(scr, "pgup/pgdn") {
		t.Fatalf("expected a scrollable popup:\n%s", scr)
	}
	for range 20 {
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if !m.info.open {
		t.Fatal("paging should keep the popup open")
	}
	if scr := screenText(m); !strings.Contains(scr, "line 59") {
		t.Fatalf("expected the end after paging:\n%s", scr)
	}
}

func TestInfoVimK(t *testing.T) {
	d := fakeDocumenter{md: "type Point struct{ X, Y int }"}
	m := newInfoModel(t, d, true, "p := Point{}")
	if m.vim.mode != vimNormal {
		t.Fatalf("want normal mode, got %v", m.vim.mode)
	}
	m.cur().ed.SetCursor(0, 6)
	runInfoCmds(m, m.handleKey(tea.KeyPressMsg{Code: 'K', Text: "K"}))
	if !m.infoVisible() || !strings.Contains(screenText(m), "type Point struct") {
		t.Fatal("K should show symbol info in normal mode")
	}
	if m.cur().ed.Value() != "p := Point{}" {
		t.Fatal("K must not edit")
	}
	m.closeInfo()

	// In insert mode K is text; alt+k still works.
	vimType(m, "i")
	m.handleKey(tea.KeyPressMsg{Code: 'K', Text: "K"})
	if m.info.open || !strings.Contains(m.cur().ed.Value(), "K") {
		t.Fatal("K should insert text in insert mode")
	}
	runInfoCmds(m, m.handleKey(altK))
	if !m.infoVisible() {
		t.Fatal("alt+k should work in insert mode")
	}
}

func TestInfoThemeChangeRerenders(t *testing.T) {
	m := newInfoModel(t, fakeDocumenter{md: "doc"}, false, "x")
	runInfoCmds(m, m.handleKey(altK))
	_ = screenText(m)
	if m.info.lines == nil || m.infoMD == nil {
		t.Fatal("expected cached render")
	}
	m.applyTheme(m.themes.name)
	if m.info.lines != nil || m.infoMD != nil {
		t.Fatal("theme change must drop the cached render")
	}
}
