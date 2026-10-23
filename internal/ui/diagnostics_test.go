package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/complete"
	"github.com/mark3labs/gopyter/internal/notebook"
)

type fakeDiagnoser struct {
	fakeCompleter
	calls int
	items []complete.Diagnostic
}

func (f *fakeDiagnoser) Diagnose(_ context.Context, _ complete.Request) ([]complete.Diagnostic, error) {
	f.calls++
	return f.items, nil
}

func runDiagCmds(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runDiagCmds(m, c)
		}
	case diagTickMsg:
		_, cmd := m.Update(msg)
		runDiagCmds(m, cmd)
	case diagResultMsg:
		_, cmd := m.Update(msg)
		runDiagCmds(m, cmd)
	}
}
func diagModel() (*Model, *fakeDiagnoser) {
	m := newCompletionModel()
	f := &fakeDiagnoser{items: []complete.Diagnostic{{Row: 0, Col: 1, EndRow: 0, EndCol: 3, Severity: complete.SeverityError, Message: "bad token"}}}
	m.completer = f
	m.enterEdit()
	m.cur().ed.SetValue("abcd")
	return m, f
}

func TestDiagnosticsFlow(t *testing.T) {
	m, f := diagModel()
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	runDiagCmds(m, cmd)
	if f.calls != 1 || len(m.diag.items) != 1 {
		t.Fatalf("calls %d, items %+v", f.calls, m.diag.items)
	}
	v := m.cur().ed.Render(80, true, m.hl, m.theme, m.diagMarks(m.cur()))
	if !strings.Contains(v.lines[0], "4:3") || !strings.HasSuffix(ansi.Strip(v.lines[0]), "abcd") {
		t.Fatalf("%q", v.lines[0])
	}
	m.cur().ed.Right()
	_, cmd = m.Update(nil)
	runDiagCmds(m, cmd)
	if f.calls != 1 {
		t.Fatal("cursor move requested diagnostics")
	}
}

func TestDiagnosticsStaleResultDropped(t *testing.T) {
	m, _ := diagModel()
	_ = m.diagnoseIfChanged()
	for _, msg := range []diagResultMsg{
		{seq: m.diag.seq - 1, cellID: m.cur().id, version: m.cur().ed.version},
		{seq: m.diag.seq, cellID: m.cur().id, version: m.cur().ed.version - 1},
	} {
		msg.items = []complete.Diagnostic{{Message: "stale"}}
		_, _ = m.Update(msg)
		if len(m.diag.items) != 0 {
			t.Fatal(m.diag.items)
		}
	}
}

func TestDiagnosticsClearedOnEditRunAndLeave(t *testing.T) {
	m, f := diagModel()
	_, cmd := m.Update(nil)
	runDiagCmds(m, cmd)
	_, _ = m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if len(m.diag.items) != 0 {
		t.Fatal("edit retained marks")
	}
	m.diag.items = f.items
	m.leaveEdit()
	if len(m.diag.items) != 0 {
		t.Fatal("leave retained marks")
	}
	m.enterEdit()
	m.diag.items = f.items
	// Markdown executes through runSelected without needing a kernel.
	m.cur().kind = notebook.Markdown
	_ = m.runSelected(false, false)
	if len(m.diag.items) != 0 {
		t.Fatal("run retained marks")
	}
}

func TestDiagnosticFooterMessage(t *testing.T) {
	m, f := diagModel()
	_ = m.diagnoseIfChanged()
	m.diag.items = f.items
	for _, col := range []int{1, 2, 3} {
		m.cur().ed.SetCursor(0, col)
		if !strings.Contains(ansi.Strip(m.renderFooter()), "bad token") {
			t.Fatalf("footer at %d", col)
		}
	}
	m.cur().ed.SetCursor(0, 4)
	if strings.Contains(ansi.Strip(m.renderFooter()), "bad token") {
		t.Fatal("outside range")
	}
	m.cur().ed.SetCursor(0, 2)
	m.status = "priority"
	footer := ansi.Strip(m.renderFooter())
	if !strings.Contains(footer, "priority") || strings.Contains(footer, "bad token") {
		t.Fatal(footer)
	}
}

func TestNoDiagnoserNoRequests(t *testing.T) {
	m := newCompletionModel()
	m.enterEdit()
	if cmd := m.diagnoseIfChanged(); cmd != nil {
		t.Fatal("plain completer requested diagnostics")
	}
	m, f := diagModel()
	m.cur().kind = notebook.Markdown
	_, cmd := m.Update(nil)
	runDiagCmds(m, cmd)
	if f.calls != 0 || len(m.diagMarks(m.cur())) != 0 {
		t.Fatal("markdown diagnostics")
	}
}

func TestDiagnosticUnderlineFollowsTheme(t *testing.T) {
	m, f := diagModel()
	_ = m.diagnoseIfChanged()
	m.diag.items = f.items
	dark := m.cur().ed.Render(80, true, m.hl, m.theme, m.diagMarks(m.cur())).lines[0]
	m.themes.dark = false
	m.applyTheme(m.themes.name)
	t.Cleanup(func() { setPalette(defaultTheme().palette(true)) })
	light := m.cur().ed.Render(80, true, m.hl, m.theme, m.diagMarks(m.cur())).lines[0]
	if dark == light || !strings.Contains(light, "4:3") {
		t.Fatalf("%q vs %q", dark, light)
	}
}
