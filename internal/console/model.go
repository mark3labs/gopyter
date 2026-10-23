package console

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/quick"
	"github.com/charmbracelet/colorprofile"
	"github.com/mark3labs/gopyter/internal/complete"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
	"github.com/mark3labs/gopyter/internal/palette"
	"github.com/mark3labs/gopyter/internal/runner"
)

type running struct {
	cancel context.CancelFunc
	ch     chan string
	out    *lineWriter
	stdin  *stdinQueue
	start  time.Time
	done   chan struct{}
	err    error // published by closing done, then ch
	count  int
}
type runMsg struct {
	run  *running
	line string
	done bool
}
type tickMsg struct{ run *running }
type completionMsg struct {
	seq    int
	result complete.Result
	err    error
}
type completion struct {
	seq             int
	items           []complete.Item
	index, inserted int
	cancel          context.CancelFunc
}

type model struct {
	ctx      context.Context
	opts     Options
	sess     *session
	hist     *history
	ta       textarea.Model
	in       textinput.Model
	comp     completion
	run      *running
	width    int
	nextID   string
	profile  colorprofile.Profile
	outputFD uintptr
	print    func(string) tea.Cmd
}

func newModel(ctx context.Context, opts Options, sess *session, h *history) *model {
	m := &model{ctx: ctx, opts: opts, sess: sess, hist: h, width: 100, nextID: notebook.NewID(), print: func(s string) tea.Cmd { return tea.Println(s) }}
	m.ta = textarea.New()
	m.ta.ShowLineNumbers = false
	m.ta.DynamicHeight = true
	m.ta.MaxHeight = 0
	m.ta.MaxWidth = 0
	m.ta.SetHeight(1)
	m.ta.SetWidth(100)
	m.ta.SetVirtualCursor(false)
	m.ta.Placeholder = ""
	m.ta.SetPromptFunc(8, func(p textarea.PromptInfo) string {
		if p.LineNumber == 0 {
			return fmt.Sprintf("In [%d]: ", m.sess.count+1)
		}
		return "   ...: "
	})
	_ = m.ta.Focus() // real cursor needs no blink command
	m.in = textinput.New()
	m.in.Prompt = "stdin> "
	m.in.SetVirtualCursor(false)
	return m
}
func (m *model) Init() tea.Cmd { return m.print(banner()) }
func (m *model) stop() {
	m.clearCompletion()
	if m.run != nil {
		m.run.cancel()
		m.run.stdin.close()
		<-m.run.done
	}
}
func (m *model) clearCompletion() {
	if m.comp.cancel != nil {
		m.comp.cancel()
	}
	m.comp = completion{seq: m.comp.seq + 1}
}
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.ta.SetWidth(m.width)
		m.in.SetWidth(max(1, m.width-7))
	case runMsg:
		if m.run != msg.run {
			return m, nil
		}
		if msg.done {
			rs := m.run
			m.run = nil
			rs.stdin.close()
			rs.cancel()
			m.in.Reset()
			_ = m.ta.Focus()
			if rs.err != nil {
				return m, m.print("✗ " + rs.err.Error())
			}
			return m, nil
		}
		return m, tea.Sequence(m.print(msg.line), waitRun(msg.run))
	case tickMsg:
		if m.run == msg.run {
			return m, tick(msg.run)
		}
	case completionMsg:
		if msg.seq != m.comp.seq || m.run != nil {
			return m, nil
		}
		if msg.err != nil {
			return m, m.print("✗ completion: " + msg.err.Error())
		}
		m.comp.items = msg.result.Items
		m.comp.index = 0
		for i := range m.comp.items {
			if m.comp.items[i].Replace == 0 {
				m.comp.items[i].Replace = msg.result.Replace
			}
		}
		if len(m.comp.items) > 0 {
			m.applyCompletion(m.comp.items[0].Replace)
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	var cmd tea.Cmd
	if m.run != nil {
		m.in, cmd = m.in.Update(msg)
	} else {
		m.ta, cmd = m.ta.Update(msg)
	}
	return m, cmd
}
func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+l" {
		return tea.ClearScreen
	}
	if m.run != nil {
		switch key {
		case "ctrl+c":
			m.run.cancel()
			m.run.stdin.close()
			return nil
		case "ctrl+d":
			m.run.stdin.close()
			return nil
		case "enter":
			if !m.run.stdin.send(m.in.Value() + "\n") {
				return m.print("› the program isn't reading its input")
			}
			m.in.Reset()
			return nil
		}
		var cmd tea.Cmd
		m.in, cmd = m.in.Update(msg)
		return cmd
	}
	if key != "tab" {
		m.clearCompletion()
	}
	switch key {
	case "ctrl+c":
		if m.ta.Value() == "" {
			return m.print("› ctrl+d exits")
		}
		m.ta.Reset()
		return nil
	case "ctrl+d":
		if m.ta.Value() == "" {
			return tea.Quit
		}
		// textarea's default ctrl+d is delete-after-cursor on some keymaps;
		// use the explicit forward-delete key for consistent shell semantics.
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
		return cmd
	case "enter":
		complete := kernel.InputComplete(m.ta.Value())
		// The Enter being pressed terminates the blank line of a cell magic.
		if strings.HasSuffix(m.ta.Value(), "\n") {
			complete = kernel.InputComplete(m.ta.Value() + "\n")
		}
		if !complete {
			line := []rune(strings.Split(m.ta.Value(), "\n")[m.ta.Line()])
			prefix := string(line[:min(m.ta.Column(), len(line))])
			indent := prefix[:len(prefix)-len(strings.TrimLeft(prefix, " \t"))]
			m.ta.InsertString("\n" + indent)
			return nil
		}
		return m.submit()
	case "alt+enter":
		m.ta.InsertString("\n")
		return nil
	case "ctrl+r", "ctrl+j":
		return m.submit()
	case "up":
		if m.ta.Line() == 0 {
			if s, ok := m.hist.prev(m.ta.Value()); ok {
				m.ta.SetValue(s)
				m.ta.MoveToBegin()
			}
			return nil
		}
	case "down":
		if m.ta.Line() == strings.Count(m.ta.Value(), "\n") {
			if s, ok := m.hist.next(); ok {
				m.ta.SetValue(s)
				m.ta.MoveToEnd()
			}
			return nil
		}
	case "tab":
		return m.complete()
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return cmd
}
func (m *model) submit() tea.Cmd {
	src := m.ta.Value()
	if strings.TrimSpace(src) == "" {
		m.ta.Reset()
		return nil
	}
	m.ta.Reset()
	if text, ok, err := m.sess.command(src); ok {
		if err != nil {
			text = "✗ " + err.Error()
		}
		return m.print(text)
	}
	var notice tea.Cmd
	if err := m.hist.add(src); err != nil {
		notice = m.print("✗ history: " + err.Error())
	}
	c := m.sess.add(src)
	c.ID = m.nextID
	m.nextID = notebook.NewID()
	sr, sw, err := os.Pipe()
	if err != nil {
		return m.print("✗ stdin: " + err.Error())
	}
	ctx, cancel := context.WithCancel(m.ctx)
	rs := &running{cancel: cancel, ch: make(chan string, 256), stdin: newStdin(), start: time.Now(), done: make(chan struct{}), count: m.sess.count}
	rs.out = &lineWriter{ctx: ctx, ch: rs.ch}
	m.run = rs
	go func() {
		defer func() { _ = sw.Close() }() // deliver EOF after queued lines
		_, _ = io.Copy(sw, rs.stdin)      // program exit can close the pipe mid-write
	}()
	_ = m.in.Focus() // real cursor needs no blink command
	width := m.width
	go func() {
		defer close(rs.ch)
		defer close(rs.done)
		rs.err = runner.RunCell(ctx, m.opts.Kernel, c, rs.count, &terminalWriter{lineWriter: rs.out, fd: m.outputFD}, sr, runner.Options{Images: true, Width: width})
		_ = sr.Close() // program owns a copy; also unblocks the queue writer
		_ = sw.Close() // unblock a write even if the program never read stdin
		rs.stdin.close()
		rs.out.flush()
		cancel()
	}()
	return tea.Sequence(m.print(echo(src, rs.count, m.profile)), notice, tea.Batch(waitRun(rs), tick(rs)))
}
func waitRun(rs *running) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-rs.ch
		return runMsg{run: rs, line: line, done: !ok}
	}
}
func tick(rs *running) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{run: rs} })
}

func (m *model) View() tea.View {
	if m.run != nil {
		status := fmt.Sprintf("In [%d] running · %s · ctrl+c interrupts", m.run.count, time.Since(m.run.start).Round(time.Second))
		partial := m.run.out.partial()
		content := status + "\n"
		if partial != "" {
			content += partial + "\n"
		}
		content += m.in.View()
		v := tea.NewView(content)
		v.Cursor = m.in.Cursor()
		if v.Cursor != nil {
			v.Cursor.Y += strings.Count(content, "\n")
		}
		return v
	}
	content := m.ta.View()
	if len(m.comp.items) > 1 {
		labels := make([]string, len(m.comp.items))
		for i, it := range m.comp.items {
			labels[i] = it.Label
			if i == m.comp.index {
				labels[i] = "[" + labels[i] + "]"
			}
		}
		content += "\n" + strings.Join(labels, "  ")
	}
	v := tea.NewView(content)
	v.Cursor = m.ta.Cursor()
	return v
}

func echo(src string, count int, profile colorprofile.Profile) string {
	if profile >= colorprofile.ANSI {
		formatter := "terminal16"
		if profile >= colorprofile.TrueColor {
			formatter = "terminal16m"
		} else if profile >= colorprofile.ANSI256 {
			formatter = "terminal256"
		}
		var b strings.Builder
		if err := quick.Highlight(&b, src, "go", formatter, palette.Dark.Syntax); err == nil {
			src = b.String()
		}
	}
	lines := strings.Split(strings.TrimRight(src, "\n"), "\n")
	for i, line := range lines {
		label := "   ...: "
		if i == 0 {
			label = fmt.Sprintf("In [%d]: ", count)
		}
		if profile >= colorprofile.ANSI {
			label = lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Dark.Primary)).Bold(true).Render(label)
		}
		lines[i] = label + line
	}
	result := strings.Join(lines, "\n")
	var b strings.Builder
	w := &colorprofile.Writer{Forward: &b, Profile: profile}
	_, _ = w.WriteString(result) // strings.Builder cannot fail
	return b.String()
}
