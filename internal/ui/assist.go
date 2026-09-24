package ui

import (
	"context"
	"errors"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/ai"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// Assistant proposes new versions of cells: fixes for failing ones, and
// edits the user asks for. *ai.Assistant implements it. A nil Assistant
// means AI features are off, and none of them is shown.
type Assistant interface {
	// Model is the "provider/model" in use.
	Model() string
	// Ready reports whether the model can be used, e.g. that its API key
	// is set, without contacting the provider.
	Ready() error
	// Fix proposes a corrected version of a failing cell.
	Fix(ctx context.Context, req ai.Request, check ai.Checker, progress func(string)) (*ai.Proposal, error)
	// Edit changes (or writes) a cell as req.Instruction says.
	Edit(ctx context.Context, req ai.Request, check ai.Checker, progress func(string)) (*ai.Proposal, error)
}

// maxErrorText bounds the error output sent with a request; the end of it
// (where panics and the last compile errors are) is kept.
const maxErrorText = 8 << 10

// aiTask is an AI fix or edit: a request in flight, then its outcome,
// which is reviewed in overlayReview. Only one exists at a time.
type aiTask struct {
	seq         int
	active      bool // a request is in flight
	cellID      string
	name        string // "In[n]", as in the cell's error positions
	label       string // the cell as shown to the user, see cellLabel
	orig        string // cell source the request was made for
	instruction string // what the user asked for; empty for a fix
	progress    string
	cancel      context.CancelFunc

	// The outcome, until it is applied or dismissed.
	prop *ai.Proposal
	err  string
	diff []diffLine
	top  int // first visible diff line
	rows int // visible diff lines at the last render
}

// pending reports whether an outcome is waiting to be reviewed.
func (t *aiTask) pending() bool { return t.prop != nil || t.err != "" }

// isEdit reports whether the task is an edit rather than a fix.
func (t *aiTask) isEdit() bool { return t.instruction != "" }

// noun names the task in messages: "fix" or "edit".
func (t *aiTask) noun() string {
	if t.isEdit() {
		return "edit"
	}
	return "fix"
}

// doing describes the request in flight, for the footer.
func (t *aiTask) doing() string {
	switch {
	case !t.isEdit():
		return "fixing " + t.label
	case strings.TrimSpace(t.orig) == "":
		return "writing " + t.label
	}
	return "editing " + t.label
}

// reviewKey is the key that reopens the task's review.
func (t *aiTask) reviewKey() string {
	if t.isEdit() {
		return "e"
	}
	return "f"
}

// askState is the "ask AI" dialog: the cell it edits, and the instruction
// typed for it, kept until an edit is applied so a discarded or failed
// request can be refined instead of retyped.
type askState struct {
	cellID string
	draft  string
}

// taskEvent is sent by the goroutine running a request.
type taskEvent struct {
	progress string
	prop     *ai.Proposal
	err      error
	done     bool
}

type taskMsg struct {
	seq int
	ev  taskEvent
	ch  chan taskEvent
}

func waitTask(seq int, ch chan taskEvent) tea.Cmd {
	return func() tea.Msg { return taskMsg{seq: seq, ev: <-ch, ch: ch} }
}

// fixable reports whether cell c can be sent for an AI fix.
func (m *Model) fixable(c *Cell) bool {
	return m.assistant != nil && c.kind == notebook.Code && c.status == statusFailed
}

// editable reports whether cell c can be edited with AI.
func (m *Model) editable(c *Cell) bool {
	return m.assistant != nil && c.kind == notebook.Code
}

// taskBusy reports, as a status, why a new request can't start: an outcome
// is waiting for review (which is shown instead) or a request is running.
func (m *Model) taskBusy() (tea.Cmd, bool) {
	if m.task.pending() {
		return m.openDialog(overlayReview), true
	}
	if m.task.active {
		return m.setStatus(statusInfo, "already %s · esc to cancel", m.task.doing()), true
	}
	return nil, false
}

// requestFix asks the model to fix cell i, or shows an outcome that is
// waiting for review. Keyboard (f), the cell's fix button and the context
// menu all end up here.
func (m *Model) requestFix(i int) tea.Cmd {
	if m.assistant == nil || i < 0 || i >= len(m.cells) {
		return nil
	}
	if cmd, busy := m.taskBusy(); busy {
		return cmd
	}
	if !m.fixable(m.cells[i]) {
		return m.setStatus(statusInfo, "nothing to fix: the cell has no error")
	}
	if err := m.assistant.Ready(); err != nil {
		return m.setStatus(statusError, "%v", err)
	}
	return m.startTask(i, "")
}

// requestEdit opens the dialog asking what to do with cell i, or shows an
// outcome that is waiting for review. Keyboard (e), the cell's ask button
// and the context menu all end up here.
func (m *Model) requestEdit(i int) tea.Cmd {
	if m.assistant == nil || i < 0 || i >= len(m.cells) {
		return nil
	}
	if cmd, busy := m.taskBusy(); busy {
		return cmd
	}
	c := m.cells[i]
	if !m.editable(c) {
		return m.setStatus(statusInfo, "AI edits work on code cells")
	}
	// Check the key before the user types a request that can't be sent.
	if err := m.assistant.Ready(); err != nil {
		return m.setStatus(statusError, "%v", err)
	}
	if i != m.sel {
		m.leaveEdit()
		m.sel = i
	}
	if m.ask.cellID != c.id {
		m.ask = askState{cellID: c.id}
	}
	m.input.Placeholder = "what should this cell do?"
	m.input.SetWidth(clamp(m.width-2-2*dlgPadX-8, 20, 72))
	m.input.SetValue(m.ask.draft)
	m.input.CursorEnd()
	return m.openDialog(overlayAsk)
}

// askTarget returns the cell the ask dialog is for.
func (m *Model) askTarget() (int, *Cell) { return m.cellByID(m.ask.cellID) }

// sendAsk sends the instruction typed in the ask dialog.
func (m *Model) sendAsk() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m.setDialogFocus(focusInput)
	}
	m.ask.draft = text
	m.overlay = overlayNone
	m.input.Blur()
	i, c := m.askTarget()
	if c == nil {
		return nil
	}
	return m.startTask(i, text)
}

// startTask sends cell i to the model: an edit following instruction, or a
// fix when instruction is empty.
func (m *Model) startTask(i int, instruction string) tea.Cmd {
	c := m.cells[i]
	req := m.aiRequest(i)
	req.Instruction = instruction
	ctx, cancel := context.WithCancel(context.Background())
	m.task = aiTask{
		seq: m.task.seq + 1, active: true, cellID: c.id, name: req.Name, label: m.cellLabel(i), orig: req.Source,
		instruction: instruction, progress: "asking " + m.assistant.Model(), cancel: cancel,
	}
	seq := m.task.seq
	// Progress is best effort and may be dropped; the outcome is always
	// delivered because waitTask keeps reading until it arrives.
	ch := make(chan taskEvent, 16)
	run := m.assistant.Fix
	if instruction != "" {
		run = m.assistant.Edit
	}
	var check ai.Checker
	if m.k != nil {
		check = m.k
	}
	go func() {
		p, err := run(ctx, req, check, func(s string) {
			select {
			case ch <- taskEvent{progress: s}:
			default:
			}
		})
		ch <- taskEvent{prop: p, err: err, done: true}
	}()
	return tea.Batch(waitTask(seq, ch), m.startSpinner())
}

// cellLabel names cell i for the user: In[n] like its prompt, or its
// position when it hasn't run.
func (m *Model) cellLabel(i int) string {
	if n := m.cells[i].count; n > 0 {
		return "In[" + itoa(n) + "]"
	}
	return "cell " + itoa(i+1)
}

// aiRequest describes cell i and its context for the model.
func (m *Model) aiRequest(i int) ai.Request {
	c := m.cells[i]
	name := "In[ ]" // never run: its number isn't known yet
	if c.count > 0 {
		name = "In[" + itoa(c.count) + "]"
	}
	req := ai.Request{CellID: c.id, Name: name, Source: c.ed.Value()}
	for _, p := range m.cells[:i] {
		if p.kind != notebook.Code || strings.TrimSpace(p.ed.Value()) == "" {
			continue
		}
		name := "In[ ] (not run in this session)"
		if p.ran && p.count > 0 {
			name = "In[" + itoa(p.count) + "]"
		}
		req.Before = append(req.Before, ai.Cell{Name: name, Source: p.ed.Value()})
	}
	if c.status == statusFailed {
		var errs strings.Builder
		for _, o := range c.outputs {
			if o.Kind == notebook.Error || o.Kind == notebook.Stderr {
				errs.WriteString(strings.TrimRight(o.Text, "\n"))
				errs.WriteByte('\n')
			}
		}
		req.Error = errs.String()
		if n := len(req.Error); n > maxErrorText {
			req.Error = "…" + req.Error[n-maxErrorText:]
		}
	}
	if m.k != nil {
		req.Declarations = m.k.Declarations()
		req.GoVersion = m.k.GoVersion()
	}
	return req
}

func (m *Model) handleTaskMsg(msg taskMsg) tea.Cmd {
	if !msg.ev.done {
		if msg.seq == m.task.seq && m.task.active {
			m.task.progress = msg.ev.progress
		}
		return waitTask(msg.seq, msg.ch)
	}
	if msg.seq != m.task.seq || !m.task.active {
		return nil // cancelled or superseded
	}
	m.task.active = false
	m.task.cancel()
	_, c := m.cellByID(m.task.cellID)
	switch {
	case c == nil:
		noun := m.task.noun()
		m.task = aiTask{seq: m.task.seq}
		return m.setStatus(statusInfo, "%s discarded: the cell was deleted", noun)
	case errors.Is(msg.ev.err, context.Canceled):
		return nil
	case msg.ev.err != nil:
		m.task.err = msg.ev.err.Error()
	default:
		m.task.prop = msg.ev.prop
		m.task.diff = lineDiff(c.ed.Value(), msg.ev.prop.Source)
	}
	if m.overlay != overlayNone {
		// Don't pull another dialog or menu out from under the user.
		return m.setStatus(statusInfo, "%s for %s ready · press %s to review", m.task.noun(), m.task.label, m.task.reviewKey())
	}
	return m.openDialog(overlayReview)
}

// cancelTask aborts the request in flight.
func (m *Model) cancelTask() tea.Cmd {
	if !m.task.active {
		return nil
	}
	m.task.cancel()
	m.task.active = false
	return m.setStatus(statusInfo, "%s cancelled", m.task.noun())
}

// applyProposal replaces the cell's source with the proposal, as one undo
// step. The cell isn't run: that's left to the user.
func (m *Model) applyProposal() tea.Cmd {
	p, name, noun := m.task.prop, m.task.label, m.task.noun()
	i, c := m.cellByID(m.task.cellID)
	m.discardProposal()
	if p == nil || c == nil {
		return nil
	}
	if m.ask.cellID == c.id {
		m.ask = askState{} // done: the next request starts afresh
	}
	if i != m.sel {
		m.leaveEdit()
		m.sel = i
	}
	c.ed.SetValue(p.Source)
	c.ed.breakUndo()
	m.dirty, m.follow = true, true
	return m.setStatus(statusSuccess, "%s applied to %s · shift+enter to run it, ctrl+z while editing to undo", noun, name)
}

// discardProposal closes the review, dropping the outcome.
func (m *Model) discardProposal() {
	m.overlay = overlayNone
	m.task = aiTask{seq: m.task.seq}
}

// scrollReview scrolls the diff in the review dialog.
func (m *Model) scrollReview(d int) {
	m.task.top = clamp(m.task.top+d, 0, max(len(m.task.diff)-m.task.rows, 0))
}

// renderAsk returns the rows of the ask dialog, without its buttons.
func (m *Model) renderAsk() []string {
	t := m.theme
	heading := "✦ Ask AI to change this cell"
	if i, c := m.askTarget(); c != nil {
		heading = "✦ Ask AI to change " + m.cellLabel(i)
		if strings.TrimSpace(c.ed.Value()) == "" {
			heading = "✦ Ask AI to write " + m.cellLabel(i)
		}
	}
	return []string{
		lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render(heading) +
			t.muted.Render("  "+m.assistant.Model()),
		"",
		m.input.View(),
		"",
		t.muted.Render("Sends this cell and the code cells above it. You review the change first."),
	}
}

// renderReview returns the rows of the review dialog, without its buttons.
func (m *Model) renderReview() []string {
	t := m.theme
	f := &m.task
	// Border (2), padding and a little margin on each side.
	width := clamp(m.width-2-2*dlgPadX-4, 20, 100)
	title := "✦ Fix for " + f.label
	if f.isEdit() {
		title = "✦ Edit for " + f.label
	}
	rows := []string{
		lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render(title) +
			t.muted.Render("  "+m.assistant.Model()),
		"",
	}
	wrap := func(style lipgloss.Style, s string) {
		for _, l := range wrapLines(normalizeOutput(s), width) {
			rows = append(rows, style.Render(l))
		}
	}
	if f.isEdit() {
		wrap(t.muted, "› "+f.instruction)
		rows = append(rows, "")
	}
	if f.err != "" {
		wrap(t.errorText, f.err)
		return rows
	}
	if f.prop.Explanation != "" {
		wrap(t.text, f.prop.Explanation)
		rows = append(rows, "")
	}
	// Dialog rows around the diff: title, explanation, notes, buttons,
	// hint, spacers, border and padding.
	f.rows = clamp(m.height-len(rows)-12, 3, max(len(f.diff), 1))
	f.top = clamp(f.top, 0, max(len(f.diff)-f.rows, 0))
	del := lipgloss.NewStyle().Foreground(colError)
	add := lipgloss.NewStyle().Foreground(colSuccess)
	for _, d := range f.diff[f.top:min(f.top+f.rows, len(f.diff))] {
		line := ansi.Truncate(expandTabs(d.text), width-2, "…")
		switch d.op {
		case '-':
			rows = append(rows, del.Render("- "+line))
		case '+':
			rows = append(rows, add.Render("+ "+line))
		default:
			rows = append(rows, t.muted.Render("  "+line))
		}
	}
	if len(f.diff) > f.rows {
		rows = append(rows, t.subtle.Render("↑↓ scroll · lines "+itoa(f.top+1)+"–"+itoa(min(f.top+f.rows, len(f.diff)))+" of "+itoa(len(f.diff))))
	}

	var notes []string
	if len(f.prop.Missing) > 0 {
		notes = append(notes, "Uses "+strings.Join(f.prop.Missing, ", ")+", downloaded when the cell runs; the proposal is only checked up to its imports.")
	}
	if _, c := m.cellByID(f.cellID); c != nil && c.ed.Value() != f.orig {
		notes = append(notes, "The cell changed while the "+f.noun()+" was prepared; applying replaces your changes.")
	}
	if len(notes) > 0 {
		rows = append(rows, "")
		for _, n := range notes {
			wrap(lipgloss.NewStyle().Foreground(colWarning), n)
		}
	}
	return rows
}

// diffLine is a line of a line-based diff: op is '-', '+' or ' '.
type diffLine struct {
	op   byte
	text string
}

// lineDiff is a longest-common-subsequence diff of a and b by lines. Cells
// are small; for huge ones it falls back to "all removed, all added".
func lineDiff(a, b string) []diffLine {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	if a == "" {
		x = nil // an empty cell has no line to remove
	}
	var out []diffLine
	if len(x)*len(y) > 1<<20 {
		for _, l := range x {
			out = append(out, diffLine{'-', l})
		}
		for _, l := range y {
			out = append(out, diffLine{'+', l})
		}
		return out
	}
	// lcs[i][j] is the LCS length of x[i:] and y[j:].
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i, v := range slices.Backward(x) {
		for j := len(y) - 1; j >= 0; j-- {
			if v == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(x) || j < len(y) {
		switch {
		case i < len(x) && j < len(y) && x[i] == y[j]:
			out = append(out, diffLine{' ', x[i]})
			i++
			j++
		case j < len(y) && (i == len(x) || lcs[i][j+1] >= lcs[i+1][j]):
			out = append(out, diffLine{'+', y[j]})
			j++
		default:
			out = append(out, diffLine{'-', x[i]})
			i++
		}
	}
	// Show removals before additions within a changed block.
	for k := 0; k < len(out); {
		e := k
		for e < len(out) && out[e].op != ' ' {
			e++
		}
		slices.SortStableFunc(out[k:e], func(p, q diffLine) int { return int(q.op) - int(p.op) })
		k = max(e, k+1)
	}
	return out
}
