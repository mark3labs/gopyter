package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/htmlview"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// startCell starts running a single-cell notebook with a real kernel. pump
// processes run events until cond holds (or the run ends).
func startCell(t *testing.T, src string) (*Model, *Cell, func(cond func() bool)) {
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
	m.width, m.height = 100, 60
	m.mode = modeCommand
	c := m.cells[0]
	m.queue = []string{c.id}
	cmd := m.startNext()
	pump := func(cond func() bool) {
		t.Helper()
		for !cond() && m.running != nil {
			// Batches (e.g. with a cursor blink) are run concurrently;
			// only the run's events matter here.
			ch := make(chan runEventsMsg, 1)
			var run func(tea.Cmd)
			run = func(cmd tea.Cmd) {
				switch msg := cmd().(type) {
				case runEventsMsg:
					ch <- msg
				case tea.BatchMsg:
					for _, c := range msg {
						if c != nil {
							go run(c)
						}
					}
				}
			}
			go run(cmd)
			select {
			case rm := <-ch:
				cmd = m.handleRunEvents(rm)
			case <-time.After(20 * time.Second):
				t.Fatalf("timed out; output:\n%s", plainOutput(c))
			}
		}
		if !cond() {
			t.Fatalf("condition not met; output:\n%s", plainOutput(c))
		}
		_ = m.View() // lays out widgets and zones, as the program would
	}
	return m, c, pump
}

func has(c *Cell, s string) func() bool {
	return func() bool { return strings.Contains(plainOutput(c), s) }
}

func keyPress(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// zoneFor returns the zone of a widget action.
func zoneFor(t *testing.T, m *Model, kind actionKind, id string, n int) zone {
	t.Helper()
	for _, z := range m.zones {
		if z.act.kind == kind && z.act.id == id && (kind != actWidgetSet || z.act.n == n) {
			return z
		}
	}
	t.Fatalf("no zone for %v %q %d", kind, id, n)
	return zone{}
}

func TestWidgets(t *testing.T) {
	src := `import "github.com/janpfeifer/gonb/gonbui/widgets"
%%
s := widgets.Slider(0, 10, 5).WithAddress("s").Done()
b := widgets.Button("Go").WithAddress("b").Done()
sel := widgets.Select([]string{"red", "green", "blue"}).WithAddress("c").Done()
ch, bc, sc := s.Listen(), b.Listen(), sel.Listen()
fmt.Println("ready")
for {
	select {
	case v, ok := <-ch.C:
		if !ok {
			fmt.Println("finished", s.Value(), sel.Value())
			return
		}
		fmt.Println("slider", v)
	case n, ok := <-bc.C:
		if ok {
			fmt.Println("click", n)
		}
	case i, ok := <-sc.C:
		if ok {
			fmt.Println("select", i)
		}
	}
}`
	m, c, pump := startCell(t, src)
	// stdout and displays are separate pipes: wait for both.
	pump(func() bool { return has(c, "ready")() && len(htmlview.Widgets(c.outputs)) == 3 })
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"●", " Go ", " red ▾ ", "✓ done", "stdin ❯"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in view:\n%s", want, view)
		}
	}

	// Keyboard: enter focuses the first widget, tab the next ones.
	m.handleKey(keyPress(tea.KeyEnter))
	if m.in.focus != "s" {
		t.Fatalf("focus %q", m.in.focus)
	}
	m.handleKey(keyPress(tea.KeyRight))
	pump(has(c, "slider 6"))
	m.handleKey(keyPress(tea.KeyTab))
	m.handleKey(keyPress(tea.KeyEnter))
	pump(has(c, "click 1"))
	m.handleKey(keyPress(tea.KeyTab))
	m.handleKey(keyPress(tea.KeyRight))
	pump(has(c, "select 1"))
	if !strings.Contains(ansi.Strip(m.View().Content), " green ▾ ") {
		t.Fatal("select not redrawn")
	}
	m.handleKey(keyPress(tea.KeyTab))
	if m.in.focus != focusStdin {
		t.Fatalf("tab should reach the input line, focus %q", m.in.focus)
	}

	// Mouse: a click on the slider's track, on the button, and a choice
	// in the select's menu.
	z := zoneFor(t, m, actWidgetSet, "s", 10)
	m.handleMouseDown(tea.Mouse{X: z.rect.Min.X, Y: z.rect.Min.Y, Button: tea.MouseLeft})
	m.handleMouseRelease()
	pump(has(c, "slider 10"))
	z = zoneFor(t, m, actWidgetClick, "b", 0)
	m.handleMouseDown(tea.Mouse{X: z.rect.Min.X, Y: z.rect.Min.Y, Button: tea.MouseLeft})
	m.handleMouseRelease()
	pump(has(c, "click 2"))
	z = zoneFor(t, m, actWidgetMenu, "c", 0)
	m.handleMouseDown(tea.Mouse{X: z.rect.Min.X, Y: z.rect.Min.Y, Button: tea.MouseLeft})
	m.handleMouseRelease()
	if m.overlay != overlayMenu || len(m.menu.items) != 3 {
		t.Fatalf("select menu not open: overlay %v items %d", m.overlay, len(m.menu.items))
	}
	m.handleKey(keyPress(tea.KeyDown))
	m.handleKey(keyPress(tea.KeyEnter))
	pump(has(c, "select 2"))

	// Done (the button, as ctrl+d) ends the loop; the cell finishes.
	z = zoneFor(t, m, actInputDone, "", 0)
	m.handleMouseDown(tea.Mouse{X: z.rect.Min.X, Y: z.rect.Min.Y, Button: tea.MouseLeft})
	m.handleMouseRelease()
	pump(func() bool { return m.running == nil })
	if c.status != statusOK || !has(c, "finished 10 2")() {
		t.Fatalf("status %v, output:\n%s", c.status, plainOutput(c))
	}
	view = ansi.Strip(m.View().Content)
	if strings.Contains(view, "✓ done") || len(m.inputItems()) != 0 || len(c.outWidgets) != 0 {
		t.Fatalf("widgets still live after the run:\n%s", view)
	}
	// The final values are kept in the saved HTML.
	if ws := htmlview.Widgets(c.outputs); len(ws) != 3 || ws[0].Value != 10 || ws[2].Value != 2 {
		t.Fatalf("saved widgets %+v", ws)
	}
}

func TestRequestInputPassword(t *testing.T) {
	src := "%%\ngonbui.RequestInput(\"secret\", true)\nvar s string\nfmt.Scanln(&s)\nfmt.Println(len(s))"
	m, c, pump := startCell(t, src)
	pump(func() bool { return m.in.focus == focusStdin })
	if m.in.prompt != "secret" || !m.in.password || !strings.Contains(ansi.Strip(m.View().Content), "secret ❯") {
		t.Fatalf("prompt %q password %v", m.in.prompt, m.in.password)
	}
	for _, r := range "hunter2" {
		m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if strings.Contains(ansi.Strip(m.View().Content), "hunter2") {
		t.Fatal("password shown")
	}
	m.handleKey(keyPress(tea.KeyEnter))
	pump(func() bool { return m.running == nil })
	out := plainOutput(c)
	if strings.Contains(out, "hunter2") || !strings.Contains(out, "7") {
		t.Fatalf("output %q", out)
	}
}
