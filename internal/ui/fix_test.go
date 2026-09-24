package ui

import (
	"cmp"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/ai"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// fakeFixer returns a canned outcome. With block set, Fix waits until its
// context is cancelled.
type fakeFixer struct {
	mu       sync.Mutex
	name     string // model; "fake/model" when empty
	fix      *ai.Fix
	err      error
	notReady error
	block    bool
	reqs     []ai.FixRequest
}

func (f *fakeFixer) Model() string { return cmp.Or(f.name, "fake/model") }
func (f *fakeFixer) Ready() error  { return f.notReady }

func (f *fakeFixer) Fix(ctx context.Context, req ai.FixRequest, _ ai.Checker, progress func(string)) (*ai.Fix, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	progress("thinking")
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.fix, f.err
}

// runFixCmds runs cmd synchronously, delivering fix results. Status timers
// are never run (they would sleep), which is why results' commands other
// than waitFix are dropped.
func runFixCmds(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runFixCmds(m, c)
		}
	case fixMsg:
		next := m.handleFixMsg(msg)
		if !msg.ev.done {
			runFixCmds(m, next)
		}
	}
}

// newFixModel returns a model whose second cell failed to compile.
func newFixModel(t *testing.T, f Fixer) *Model {
	t.Helper()
	nb := &notebook.Notebook{Cells: []*notebook.Cell{
		{ID: "a", Type: notebook.Code, Source: `name := "gopher"`, ExecutionCount: 1},
		{ID: "b", Type: notebook.Code, Source: "x := 1\nfmt.Println(nme)", ExecutionCount: 2,
			Outputs: []notebook.Output{{Kind: notebook.Error, Text: "In[2]:2:13: undefined: nme"}}},
	}}
	var cfg *AIConfig
	if f != nil {
		cfg = &AIConfig{Model: f.Model(), On: true, New: func(string) Fixer { return f }}
	}
	m := New(Options{Notebook: nb, Kernel: &kernel.Kernel{}, AI: cfg})
	m.width, m.height = 100, 30
	m.sel = 1
	m.cells[0].ran = true // as if it ran in this session
	return m
}

var keyF = tea.KeyPressMsg{Code: 'f', Text: "f"}

func TestFixApplyAndUndo(t *testing.T) {
	f := &fakeFixer{fix: &ai.Fix{Source: "x := 1\nfmt.Println(name)", Explanation: "Typo: nme is name."}}
	m := newFixModel(t, f)
	if !strings.Contains(screenText(m), "✦ fix") {
		t.Fatal("no fix button on the failed cell")
	}

	runFixCmds(m, m.handleKey(keyF))
	if len(f.reqs) != 1 {
		t.Fatalf("%d requests", len(f.reqs))
	}
	req := f.reqs[0]
	if req.Name != "In[2]" || !strings.Contains(req.Error, "undefined: nme") ||
		len(req.Before) != 1 || req.Before[0].Name != "In[1]" || req.Before[0].Source != `name := "gopher"` {
		t.Errorf("request = %+v", req)
	}
	if m.overlay != overlayFix {
		t.Fatalf("overlay = %v, want the fix review", m.overlay)
	}
	screen := screenText(m)
	for _, want := range []string{"Fix for In[2]", "Typo: nme is name.", "- fmt.Println(nme)", "+ fmt.Println(name)", "  x := 1", "Apply", "Discard"} {
		if !strings.Contains(screen, want) {
			t.Errorf("review lacks %q:\n%s", want, screen)
		}
	}

	// Enter applies (Apply has focus); the cell is changed, not run.
	m.handleKey(press(tea.KeyEnter, 0))
	c := m.cells[1]
	if m.overlay != overlayNone || c.ed.Value() != "x := 1\nfmt.Println(name)" {
		t.Fatalf("not applied: overlay=%v src=%q", m.overlay, c.ed.Value())
	}
	if m.busy() || !m.dirty {
		t.Errorf("busy=%v dirty=%v", m.busy(), m.dirty)
	}
	// One undo step restores the original.
	c.ed.Undo()
	if c.ed.Value() != "x := 1\nfmt.Println(nme)" {
		t.Errorf("undo gave %q", c.ed.Value())
	}
}

func TestFixDiscard(t *testing.T) {
	f := &fakeFixer{fix: &ai.Fix{Source: "fmt.Println(name)"}}
	m := newFixModel(t, f)
	runFixCmds(m, m.handleKey(keyF))
	m.handleKey(press(tea.KeyEscape, 0))
	if m.overlay != overlayNone || m.cells[1].ed.Value() != "x := 1\nfmt.Println(nme)" || m.fix.pending() {
		t.Fatalf("discard: overlay=%v src=%q", m.overlay, m.cells[1].ed.Value())
	}
}

func TestFixError(t *testing.T) {
	m := newFixModel(t, &fakeFixer{err: errors.New("no fix proposed: run In[1] first")})
	runFixCmds(m, m.handleKey(keyF))
	screen := screenText(m)
	if m.overlay != overlayFix || !strings.Contains(screen, "run In[1] first") || !strings.Contains(screen, "Close") || strings.Contains(screen, "Apply") {
		t.Fatalf("error review:\n%s", screen)
	}
	m.handleKey(press(tea.KeyEnter, 0))
	if m.overlay != overlayNone {
		t.Fatal("Close didn't close")
	}
}

func TestFixNotReady(t *testing.T) {
	f := &fakeFixer{notReady: errors.New("no API key for anthropic: set ANTHROPIC_API_KEY")}
	m := newFixModel(t, f)
	m.handleKey(keyF) // the status timer is not run
	if len(f.reqs) != 0 || m.fix.active || !strings.Contains(m.status, "ANTHROPIC_API_KEY") {
		t.Fatalf("reqs=%d active=%v status=%q", len(f.reqs), m.fix.active, m.status)
	}
}

func TestFixCancel(t *testing.T) {
	f := &fakeFixer{block: true}
	m := newFixModel(t, f)
	batch := m.handleKey(keyF)
	if !m.fix.active || !strings.Contains(screenText(m), "fixing In[2]") {
		t.Fatal("no progress shown")
	}
	m.handleKey(press(tea.KeyEscape, 0))
	if m.fix.active {
		t.Fatal("esc didn't cancel")
	}
	// The cancelled request still finishes; its outcome is dropped.
	runFixCmds(m, batch)
	if m.overlay != overlayNone || m.fix.pending() {
		t.Fatalf("cancelled fix surfaced: overlay=%v", m.overlay)
	}
}

func TestFixCellDeletedMeanwhile(t *testing.T) {
	m := newFixModel(t, &fakeFixer{fix: &ai.Fix{Source: "y"}})
	batch := m.handleKey(keyF)
	m.deleteCell(1)
	runFixCmds(m, batch)
	if m.overlay != overlayNone || m.fix.pending() {
		t.Fatalf("fix for a deleted cell: overlay=%v", m.overlay)
	}
}

func TestFixButtonClick(t *testing.T) {
	f := &fakeFixer{fix: &ai.Fix{Source: "fmt.Println(name)"}}
	m := newFixModel(t, f)
	screenText(m)
	for _, z := range m.zones {
		if z.act.kind == actFixCell {
			runFixCmds(m, m.doAction(z.act))
			if len(f.reqs) != 1 || m.overlay != overlayFix {
				t.Fatalf("click: reqs=%d overlay=%v", len(f.reqs), m.overlay)
			}
			return
		}
	}
	t.Fatal("no clickable fix button")
}

// TestAIOffIsInvisible: without a Fixer nothing AI-related is shown or
// bound.
func TestAIOffIsInvisible(t *testing.T) {
	m := newFixModel(t, nil)
	if s := screenText(m); strings.Contains(s, "fix") {
		t.Errorf("AI shown while off:\n%s", s)
	}
	m.handleKey(keyF)
	if m.fix.active || m.overlay != overlayNone {
		t.Error("f did something with AI off")
	}
	m.overlay = overlayHelp
	if s := screenText(m); strings.Contains(s, "AI") {
		t.Error("help lists AI while off")
	}
	m.overlay = overlayNone
	m.openContextMenu(10, m.layout[1].top+headerHeight+1)
	for _, it := range m.menu.items {
		if it.act.kind == actFixCell {
			t.Error("context menu offers a fix with AI off")
		}
	}
}

// TestFixMarksCellsNotRun: execution counts saved in the notebook don't
// mean a cell ran in this kernel session, and the model must know which
// earlier cells did.
func TestFixMarksCellsNotRun(t *testing.T) {
	f := &fakeFixer{fix: &ai.Fix{Source: "y"}}
	m := newFixModel(t, f)
	m.cells[0].ran = false // loaded from disk only
	runFixCmds(m, m.handleKey(keyF))
	if got := f.reqs[0].Before[0].Name; got != "In[ ] (not run in this session)" {
		t.Errorf("earlier cell labelled %q", got)
	}
}

func TestFixNotOfferedForOKCell(t *testing.T) {
	m := newFixModel(t, &fakeFixer{})
	m.sel = 0
	if m.fixable(m.cur()) {
		t.Fatal("a cell without error is fixable")
	}
}

func TestLineDiff(t *testing.T) {
	var got []string
	for _, d := range lineDiff("a\nb\nc", "a\nB\nc\nd") {
		got = append(got, string(d.op)+d.text)
	}
	want := []string{" a", "-b", "+B", " c", "+d"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v", got, want)
	}
}
