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

// fakeAssistant returns a canned outcome, for fixes and edits alike. With
// block set, requests wait until their context is cancelled.
type fakeAssistant struct {
	mu       sync.Mutex
	name     string // model; "fake/model" when empty
	prop     *ai.Proposal
	err      error
	notReady error
	block    bool
	reqs     []ai.Request
	edits    int // how many of reqs were edits
}

func (f *fakeAssistant) Model() string { return cmp.Or(f.name, "fake/model") }
func (f *fakeAssistant) Ready() error  { return f.notReady }

func (f *fakeAssistant) Fix(ctx context.Context, req ai.Request, _ ai.Checker, progress func(string)) (*ai.Proposal, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	progress("thinking")
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.prop, f.err
}

// requests counts the requests made, including one still blocked.
func (f *fakeAssistant) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeAssistant) Edit(ctx context.Context, req ai.Request, check ai.Checker, progress func(string)) (*ai.Proposal, error) {
	f.mu.Lock()
	f.edits++
	f.mu.Unlock()
	return f.Fix(ctx, req, check, progress)
}

// runTaskCmds runs cmd synchronously, delivering AI results. Status timers
// are never run (they would sleep), which is why results' commands other
// than waitTask are dropped.
func runTaskCmds(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runTaskCmds(m, c)
		}
	case taskMsg:
		next := m.handleTaskMsg(msg)
		if !msg.ev.done {
			runTaskCmds(m, next)
		}
	}
}

// newFixModel returns a model whose second cell failed to compile.
func newFixModel(t *testing.T, f Assistant) *Model {
	t.Helper()
	nb := &notebook.Notebook{Cells: []*notebook.Cell{
		{ID: "a", Type: notebook.Code, Source: `name := "gopher"`, ExecutionCount: 1},
		{ID: "b", Type: notebook.Code, Source: "x := 1\nfmt.Println(nme)", ExecutionCount: 2,
			Outputs: []notebook.Output{{Kind: notebook.Error, Text: "In[2]:2:13: undefined: nme"}}},
	}}
	var cfg *AIConfig
	if f != nil {
		cfg = &AIConfig{Model: f.Model(), On: true, New: func(string) Assistant { return f }}
	}
	m := New(Options{Notebook: nb, Kernel: &kernel.Kernel{}, AI: cfg})
	m.width, m.height = 100, 30
	m.sel = 1
	m.cells[0].ran = true // as if it ran in this session
	return m
}

var keyF = tea.KeyPressMsg{Code: 'f', Text: "f"}

func TestFixApplyAndUndo(t *testing.T) {
	f := &fakeAssistant{prop: &ai.Proposal{Source: "x := 1\nfmt.Println(name)", Explanation: "Typo: nme is name."}}
	m := newFixModel(t, f)
	if !strings.Contains(screenText(m), "✦ fix") {
		t.Fatal("no fix button on the failed cell")
	}

	runTaskCmds(m, m.handleKey(keyF))
	if len(f.reqs) != 1 {
		t.Fatalf("%d requests", len(f.reqs))
	}
	req := f.reqs[0]
	if req.Name != "In[2]" || !strings.Contains(req.Error, "undefined: nme") ||
		len(req.Before) != 1 || req.Before[0].Name != "In[1]" || req.Before[0].Source != `name := "gopher"` {
		t.Errorf("request = %+v", req)
	}
	if m.overlay != overlayReview {
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
	f := &fakeAssistant{prop: &ai.Proposal{Source: "fmt.Println(name)"}}
	m := newFixModel(t, f)
	runTaskCmds(m, m.handleKey(keyF))
	m.handleKey(press(tea.KeyEscape, 0))
	if m.overlay != overlayNone || m.cells[1].ed.Value() != "x := 1\nfmt.Println(nme)" || m.task.pending() {
		t.Fatalf("discard: overlay=%v src=%q", m.overlay, m.cells[1].ed.Value())
	}
}

func TestFixError(t *testing.T) {
	m := newFixModel(t, &fakeAssistant{err: errors.New("no fix proposed: run In[1] first")})
	runTaskCmds(m, m.handleKey(keyF))
	screen := screenText(m)
	if m.overlay != overlayReview || !strings.Contains(screen, "run In[1] first") || !strings.Contains(screen, "Close") || strings.Contains(screen, "Apply") {
		t.Fatalf("error review:\n%s", screen)
	}
	m.handleKey(press(tea.KeyEnter, 0))
	if m.overlay != overlayNone {
		t.Fatal("Close didn't close")
	}
}

func TestFixNotReady(t *testing.T) {
	f := &fakeAssistant{notReady: errors.New("no API key for anthropic: set ANTHROPIC_API_KEY")}
	m := newFixModel(t, f)
	m.handleKey(keyF) // the status timer is not run
	if len(f.reqs) != 0 || m.task.active || !strings.Contains(m.status, "ANTHROPIC_API_KEY") {
		t.Fatalf("reqs=%d active=%v status=%q", len(f.reqs), m.task.active, m.status)
	}
}

func TestFixCancel(t *testing.T) {
	f := &fakeAssistant{block: true}
	m := newFixModel(t, f)
	batch := m.handleKey(keyF)
	if !m.task.active || !strings.Contains(screenText(m), "fixing In[2]") {
		t.Fatal("no progress shown")
	}
	m.handleKey(press(tea.KeyEscape, 0))
	if m.task.active {
		t.Fatal("esc didn't cancel")
	}
	// The cancelled request still finishes; its outcome is dropped.
	runTaskCmds(m, batch)
	if m.overlay != overlayNone || m.task.pending() {
		t.Fatalf("cancelled fix surfaced: overlay=%v", m.overlay)
	}
}

func TestFixCellDeletedMeanwhile(t *testing.T) {
	m := newFixModel(t, &fakeAssistant{prop: &ai.Proposal{Source: "y"}})
	batch := m.handleKey(keyF)
	m.deleteCell(1)
	runTaskCmds(m, batch)
	if m.overlay != overlayNone || m.task.pending() {
		t.Fatalf("fix for a deleted cell: overlay=%v", m.overlay)
	}
}

func TestFixButtonClick(t *testing.T) {
	f := &fakeAssistant{prop: &ai.Proposal{Source: "fmt.Println(name)"}}
	m := newFixModel(t, f)
	screenText(m)
	for _, z := range m.zones {
		if z.act.kind == actFixCell {
			runTaskCmds(m, m.doAction(z.act))
			if len(f.reqs) != 1 || m.overlay != overlayReview {
				t.Fatalf("click: reqs=%d overlay=%v", len(f.reqs), m.overlay)
			}
			return
		}
	}
	t.Fatal("no clickable fix button")
}

// TestAIOffIsInvisible: without an Assistant nothing AI-related is shown or
// bound.
func TestAIOffIsInvisible(t *testing.T) {
	m := newFixModel(t, nil)
	if s := screenText(m); strings.Contains(s, "fix") {
		t.Errorf("AI shown while off:\n%s", s)
	}
	for _, k := range []tea.KeyPressMsg{keyF, keyE} {
		m.handleKey(k)
		if m.task.active || m.overlay != overlayNone {
			t.Errorf("%s did something with AI off", k.String())
		}
	}
	m.insertCell(2, newCell(notebook.Code, ""))
	m.mode = modeCommand
	if s := screenText(m); strings.Contains(s, "✦") || strings.Contains(s, "ask AI") {
		t.Errorf("AI shown while off:\n%s", s)
	}
	m.overlay = overlayHelp
	if s := screenText(m); strings.Contains(s, "AI") {
		t.Error("help lists AI while off")
	}
	m.overlay = overlayNone
	m.openContextMenu(10, m.layout[1].top+headerHeight+1)
	for _, it := range m.menu.items {
		if it.act.kind == actFixCell || it.act.kind == actAskCell {
			t.Error("context menu offers AI with AI off")
		}
	}
}

// TestFixMarksCellsNotRun: execution counts saved in the notebook don't
// mean a cell ran in this kernel session, and the model must know which
// earlier cells did.
func TestFixMarksCellsNotRun(t *testing.T) {
	f := &fakeAssistant{prop: &ai.Proposal{Source: "y"}}
	m := newFixModel(t, f)
	m.cells[0].ran = false // loaded from disk only
	runTaskCmds(m, m.handleKey(keyF))
	if got := f.reqs[0].Before[0].Name; got != "In[ ] (not run in this session)" {
		t.Errorf("earlier cell labelled %q", got)
	}
}

func TestFixNotOfferedForOKCell(t *testing.T) {
	m := newFixModel(t, &fakeAssistant{})
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

var keyE = tea.KeyPressMsg{Code: 'e', Text: "e"}

// typeInput types s into the focused dialog input. The input's own
// commands (cursor blinking) are dropped.
func typeInput(m *Model, s string) {
	for _, r := range s {
		m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// askFor opens the ask dialog on cell i, types instruction and sends it,
// delivering the outcome.
func askFor(t *testing.T, m *Model, i int, instruction string) {
	t.Helper()
	m.sel = i
	m.handleKey(keyE)
	if m.overlay != overlayAsk {
		t.Fatalf("overlay = %v, want the ask dialog (status %q)", m.overlay, m.status)
	}
	typeInput(m, instruction)
	runTaskCmds(m, m.handleKey(press(tea.KeyEnter, 0)))
}

func TestEditApplyAndUndo(t *testing.T) {
	f := &fakeAssistant{prop: &ai.Proposal{Source: "name := \"gopher\"\nname = strings.ToUpper(name)", Explanation: "Upper-cases the name."}}
	m := newFixModel(t, f)
	m.sel = 0
	m.handleKey(keyE)
	screen := screenText(m)
	for _, want := range []string{"Ask AI to change In[1]", "fake/model", "Ask", "Cancel"} {
		if !strings.Contains(screen, want) {
			t.Errorf("ask dialog lacks %q:\n%s", want, screen)
		}
	}
	typeInput(m, "shout it")
	runTaskCmds(m, m.handleKey(press(tea.KeyEnter, 0)))

	if len(f.reqs) != 1 || f.edits != 1 {
		t.Fatalf("reqs=%d edits=%d", len(f.reqs), f.edits)
	}
	if req := f.reqs[0]; req.Instruction != "shout it" || req.Name != "In[1]" || req.Error != "" || len(req.Before) != 0 {
		t.Errorf("request = %+v", req)
	}
	if m.overlay != overlayReview {
		t.Fatalf("overlay = %v, want the review", m.overlay)
	}
	screen = screenText(m)
	for _, want := range []string{"Edit for In[1]", "› shout it", "Upper-cases the name.", "+ name = strings.ToUpper(name)", "Apply"} {
		if !strings.Contains(screen, want) {
			t.Errorf("review lacks %q:\n%s", want, screen)
		}
	}

	m.handleKey(press(tea.KeyEnter, 0))
	c := m.cells[0]
	if m.overlay != overlayNone || c.ed.Value() != f.prop.Source || !m.dirty || m.busy() {
		t.Fatalf("not applied: overlay=%v src=%q", m.overlay, c.ed.Value())
	}
	if !strings.Contains(m.status, "edit applied to In[1]") {
		t.Errorf("status = %q", m.status)
	}
	c.ed.Undo()
	if c.ed.Value() != `name := "gopher"` {
		t.Errorf("undo gave %q", c.ed.Value())
	}
	// Applied: the next request starts with an empty input.
	m.handleKey(keyE)
	if m.input.Value() != "" {
		t.Errorf("input = %q after an applied edit", m.input.Value())
	}
}

// TestEditWritesEmptyCell: an empty cell that never ran is written from
// scratch, and the diff only adds lines.
func TestEditWritesEmptyCell(t *testing.T) {
	f := &fakeAssistant{prop: &ai.Proposal{Source: "fmt.Println(name)\nfmt.Println(len(name))"}}
	m := newFixModel(t, f)
	m.insertCell(2, newCell(notebook.Code, ""))
	m.mode = modeCommand
	if !strings.Contains(screenText(m), "e to ask AI") {
		t.Error("the empty cell doesn't mention e")
	}
	m.handleKey(keyE)
	if s := screenText(m); !strings.Contains(s, "Ask AI to write cell 3") {
		t.Errorf("ask dialog:\n%s", s)
	}
	typeInput(m, "print name and its length")
	runTaskCmds(m, m.handleKey(press(tea.KeyEnter, 0)))

	req := f.reqs[0]
	if req.Name != "In[ ]" || req.Source != "" || len(req.Before) != 2 {
		t.Errorf("request = %+v", req)
	}
	for _, d := range m.task.diff {
		if d.op != '+' {
			t.Errorf("diff of an empty cell has %q", string(d.op)+d.text)
		}
	}
	if len(m.task.diff) != 2 {
		t.Errorf("diff = %v", m.task.diff)
	}
	if s := screenText(m); !strings.Contains(s, "Edit for cell 3") {
		t.Errorf("review:\n%s", s)
	}
}

// TestEditOfFailedCellSendsError: the model gets the error of a failing
// cell with the instruction.
func TestEditOfFailedCellSendsError(t *testing.T) {
	f := &fakeAssistant{prop: &ai.Proposal{Source: "y"}}
	m := newFixModel(t, f)
	askFor(t, m, 1, "print the name")
	if req := f.reqs[0]; !strings.Contains(req.Error, "undefined: nme") || req.Instruction != "print the name" {
		t.Errorf("request = %+v", req)
	}
}

// TestEditDraftKept: a cancelled dialog, a discarded proposal or an error
// keeps the instruction for refining; another cell starts afresh.
func TestEditDraftKept(t *testing.T) {
	f := &fakeAssistant{prop: &ai.Proposal{Source: "y"}}
	m := newFixModel(t, f)
	m.sel = 0
	m.handleKey(keyE)
	typeInput(m, "draft")
	m.handleKey(press(tea.KeyEscape, 0))
	if m.overlay != overlayNone || len(f.reqs) != 0 {
		t.Fatalf("esc: overlay=%v reqs=%d", m.overlay, len(f.reqs))
	}
	m.handleKey(keyE)
	if m.input.Value() != "draft" {
		t.Fatalf("draft after esc = %q", m.input.Value())
	}
	typeInput(m, " more")
	runTaskCmds(m, m.handleKey(press(tea.KeyEnter, 0)))
	m.handleKey(press(tea.KeyEscape, 0)) // discard
	if m.cells[0].ed.Value() != `name := "gopher"` {
		t.Fatal("discard changed the cell")
	}
	m.handleKey(keyE)
	if m.input.Value() != "draft more" {
		t.Errorf("draft after discard = %q", m.input.Value())
	}
	m.handleKey(press(tea.KeyEscape, 0))

	m.sel = 1
	m.handleKey(keyE)
	if m.input.Value() != "" {
		t.Errorf("another cell got the draft %q", m.input.Value())
	}
}

func TestEditEmptyInstructionNotSent(t *testing.T) {
	f := &fakeAssistant{prop: &ai.Proposal{Source: "y"}}
	m := newFixModel(t, f)
	m.sel = 0
	m.handleKey(keyE)
	typeInput(m, "   ")
	runTaskCmds(m, m.handleKey(press(tea.KeyEnter, 0)))
	if len(f.reqs) != 0 || m.overlay != overlayAsk || m.task.active {
		t.Fatalf("reqs=%d overlay=%v", len(f.reqs), m.overlay)
	}
}

func TestEditNotOffered(t *testing.T) {
	f := &fakeAssistant{notReady: errors.New("no API key for anthropic: set ANTHROPIC_API_KEY")}
	m := newFixModel(t, f)
	m.sel = 0
	m.handleKey(keyE)
	if m.overlay != overlayNone || !strings.Contains(m.status, "ANTHROPIC_API_KEY") {
		t.Errorf("not ready: overlay=%v status=%q", m.overlay, m.status)
	}

	m = newFixModel(t, &fakeAssistant{})
	m.convertCell(0, false)
	m.sel = 0
	m.handleKey(keyE)
	if m.overlay != overlayNone || !strings.Contains(m.status, "code cells") {
		t.Errorf("markdown: overlay=%v status=%q", m.overlay, m.status)
	}
}

func TestEditCancel(t *testing.T) {
	f := &fakeAssistant{block: true}
	m := newFixModel(t, f)
	m.sel = 0
	m.handleKey(keyE)
	typeInput(m, "do it")
	batch := m.handleKey(press(tea.KeyEnter, 0))
	if !m.task.active || !strings.Contains(screenText(m), "editing In[1]") {
		t.Fatal("no progress shown")
	}
	// A second request waits for the first.
	m.handleKey(keyF)
	if f.requests() > 1 || !strings.Contains(m.status, "already editing In[1]") {
		t.Errorf("second request: status=%q", m.status)
	}
	m.handleKey(press(tea.KeyEscape, 0))
	if m.task.active || !strings.Contains(m.status, "edit cancelled") {
		t.Fatalf("esc: active=%v status=%q", m.task.active, m.status)
	}
	runTaskCmds(m, batch)
	if m.overlay != overlayNone || m.task.pending() {
		t.Fatalf("cancelled edit surfaced: overlay=%v", m.overlay)
	}
}

func TestEditButtonAndMenu(t *testing.T) {
	f := &fakeAssistant{prop: &ai.Proposal{Source: "y"}}
	m := newFixModel(t, f)
	m.sel = 0
	screenText(m)
	var clicked bool
	for _, z := range m.zones {
		if z.act.kind == actAskCell && z.act.cell == 0 {
			m.doAction(z.act)
			clicked = true
			break
		}
		if z.act.kind == actAskCell && z.act.cell == 1 {
			t.Error("the failed cell shows the ask button instead of fix")
		}
	}
	if !clicked || m.overlay != overlayAsk {
		t.Fatalf("ask button: clicked=%v overlay=%v", clicked, m.overlay)
	}
	typeInput(m, "go")
	// Clicking Ask sends, like enter.
	runTaskCmds(m, m.doAction(action{kind: actAskSend}))
	if f.edits != 1 {
		t.Fatalf("edits = %d", f.edits)
	}
	m.discardProposal()

	m.openContextMenu(10, m.layout[0].top+headerHeight+1)
	for _, it := range m.menu.items {
		if it.act.kind == actAskCell {
			return
		}
	}
	t.Error("context menu has no ask item")
}
