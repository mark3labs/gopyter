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

// Fixer proposes corrections for failing cells. *ai.Assistant implements
// it. A nil Fixer means AI features are off, and none of them is shown.
type Fixer interface {
	// Model is the "provider/model" in use.
	Model() string
	// Ready reports whether the model can be used, e.g. that its API key
	// is set, without contacting the provider.
	Ready() error
	Fix(ctx context.Context, req ai.FixRequest, check ai.Checker, progress func(string)) (*ai.Fix, error)
}

// maxFixErrorText bounds the error output sent with a fix request; the end
// of it (where panics and the last compile errors are) is kept.
const maxFixErrorText = 8 << 10

// fixState is an AI fix: a request in flight, then its outcome, which is
// reviewed in overlayFix.
type fixState struct {
	seq      int
	active   bool // a request is in flight
	cellID   string
	name     string // "In[n]", as in the cell's error positions
	orig     string // cell source the fix was requested for
	progress string
	cancel   context.CancelFunc

	// The outcome, until it is applied or dismissed.
	fix  *ai.Fix
	err  string
	diff []diffLine
	top  int // first visible diff line
	rows int // visible diff lines at the last render
}

// pending reports whether an outcome is waiting to be reviewed.
func (f *fixState) pending() bool { return f.fix != nil || f.err != "" }

// fixEvent is sent by the goroutine running a fix request.
type fixEvent struct {
	progress string
	fix      *ai.Fix
	err      error
	done     bool
}

type fixMsg struct {
	seq int
	ev  fixEvent
	ch  chan fixEvent
}

func waitFix(seq int, ch chan fixEvent) tea.Cmd {
	return func() tea.Msg { return fixMsg{seq: seq, ev: <-ch, ch: ch} }
}

// fixable reports whether cell c can be sent for an AI fix.
func (m *Model) fixable(c *Cell) bool {
	return m.fixer != nil && c.kind == notebook.Code && c.status == statusFailed
}

// requestFix asks the model to fix cell i, or shows an outcome that is
// waiting for review. Keyboard (f), the cell's fix button and the context
// menu all end up here.
func (m *Model) requestFix(i int) tea.Cmd {
	if m.fixer == nil || i < 0 || i >= len(m.cells) {
		return nil
	}
	if m.fix.pending() {
		return m.openDialog(overlayFix)
	}
	if m.fix.active {
		return m.setStatus(statusInfo, "already fixing %s · esc to cancel", m.fix.name)
	}
	c := m.cells[i]
	if !m.fixable(c) {
		return m.setStatus(statusInfo, "nothing to fix: the cell has no error")
	}
	if err := m.fixer.Ready(); err != nil {
		return m.setStatus(statusError, "%v", err)
	}

	req := m.fixRequest(i)
	ctx, cancel := context.WithCancel(context.Background())
	m.fix = fixState{
		seq: m.fix.seq + 1, active: true, cellID: c.id, name: req.Name, orig: req.Source,
		progress: "asking " + m.fixer.Model(), cancel: cancel,
	}
	seq := m.fix.seq
	// Progress is best effort and may be dropped; the outcome is always
	// delivered because waitFix keeps reading until it arrives.
	ch := make(chan fixEvent, 16)
	fixer := m.fixer
	var check ai.Checker
	if m.k != nil {
		check = m.k
	}
	go func() {
		fix, err := fixer.Fix(ctx, req, check, func(s string) {
			select {
			case ch <- fixEvent{progress: s}:
			default:
			}
		})
		ch <- fixEvent{fix: fix, err: err, done: true}
	}()
	return tea.Batch(waitFix(seq, ch), m.startSpinner())
}

// fixRequest describes cell i and its context for the model.
func (m *Model) fixRequest(i int) ai.FixRequest {
	c := m.cells[i]
	req := ai.FixRequest{CellID: c.id, Name: "In[" + itoa(c.count) + "]", Source: c.ed.Value()}
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
	var errs strings.Builder
	for _, o := range c.outputs {
		if o.Kind == notebook.Error || o.Kind == notebook.Stderr {
			errs.WriteString(strings.TrimRight(o.Text, "\n"))
			errs.WriteByte('\n')
		}
	}
	req.Error = errs.String()
	if n := len(req.Error); n > maxFixErrorText {
		req.Error = "…" + req.Error[n-maxFixErrorText:]
	}
	if m.k != nil {
		req.Declarations = m.k.Declarations()
		req.GoVersion = m.k.GoVersion()
	}
	return req
}

func (m *Model) handleFixMsg(msg fixMsg) tea.Cmd {
	if !msg.ev.done {
		if msg.seq == m.fix.seq && m.fix.active {
			m.fix.progress = msg.ev.progress
		}
		return waitFix(msg.seq, msg.ch)
	}
	if msg.seq != m.fix.seq || !m.fix.active {
		return nil // cancelled or superseded
	}
	m.fix.active = false
	m.fix.cancel()
	_, c := m.cellByID(m.fix.cellID)
	switch {
	case c == nil:
		m.fix = fixState{seq: m.fix.seq}
		return m.setStatus(statusInfo, "fix discarded: the cell was deleted")
	case errors.Is(msg.ev.err, context.Canceled):
		return nil
	case msg.ev.err != nil:
		m.fix.err = msg.ev.err.Error()
	default:
		m.fix.fix = msg.ev.fix
		m.fix.diff = lineDiff(c.ed.Value(), msg.ev.fix.Source)
	}
	if m.overlay != overlayNone {
		// Don't pull another dialog or menu out from under the user.
		return m.setStatus(statusInfo, "fix for %s ready · press f to review", m.fix.name)
	}
	return m.openDialog(overlayFix)
}

// cancelFix aborts the request in flight.
func (m *Model) cancelFix() tea.Cmd {
	if !m.fix.active {
		return nil
	}
	m.fix.cancel()
	m.fix.active = false
	return m.setStatus(statusInfo, "fix cancelled")
}

// applyFix replaces the cell's source with the proposal, as one undo step.
// The cell isn't run: that's left to the user.
func (m *Model) applyFix() tea.Cmd {
	f, name := m.fix.fix, m.fix.name
	i, c := m.cellByID(m.fix.cellID)
	m.discardFix()
	if f == nil || c == nil {
		return nil
	}
	if i != m.sel {
		m.leaveEdit()
		m.sel = i
	}
	c.ed.SetValue(f.Source)
	c.ed.breakUndo()
	m.dirty, m.follow = true, true
	return m.setStatus(statusSuccess, "fix applied to %s · shift+enter to run it, ctrl+z while editing to undo", name)
}

// discardFix closes the review, dropping the outcome.
func (m *Model) discardFix() {
	m.overlay = overlayNone
	m.fix = fixState{seq: m.fix.seq}
}

// scrollFix scrolls the diff in the review dialog.
func (m *Model) scrollFix(d int) {
	m.fix.top = clamp(m.fix.top+d, 0, max(len(m.fix.diff)-m.fix.rows, 0))
}

// renderFix returns the rows of the review dialog, without its buttons.
func (m *Model) renderFix() []string {
	t := m.theme
	f := &m.fix
	// Border (2), padding and a little margin on each side.
	width := clamp(m.width-2-2*dlgPadX-4, 20, 100)
	rows := []string{
		lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render("✦ Fix for "+f.name) +
			t.muted.Render("  "+m.fixer.Model()),
		"",
	}
	wrap := func(style lipgloss.Style, s string) {
		for _, l := range wrapLines(normalizeOutput(s), width) {
			rows = append(rows, style.Render(l))
		}
	}
	if f.err != "" {
		wrap(t.errorText, f.err)
		return rows
	}
	if f.fix.Explanation != "" {
		wrap(t.text, f.fix.Explanation)
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
	if len(f.fix.Missing) > 0 {
		notes = append(notes, "Uses "+strings.Join(f.fix.Missing, ", ")+", downloaded when the cell runs; the fix is only checked up to its imports.")
	}
	if _, c := m.cellByID(f.cellID); c != nil && c.ed.Value() != f.orig {
		notes = append(notes, "The cell changed while the fix was prepared; applying replaces your changes.")
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
