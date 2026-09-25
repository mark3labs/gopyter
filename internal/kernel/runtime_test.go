package kernel

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/gopyter/internal/htmlview"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func TestNBPackage(t *testing.T) {
	k := newTestKernel(t)
	var events []Event
	collect := func(e Event) { events = append(events, e) }
	// nb works without an import; its Display is namespaced.
	if err := k.Execute(context.Background(), "1", "In[1]", "nb.DisplayMarkdown(\"**hi**\")\nnb.DisplayID(\"p\", 1)", collect); err != nil {
		t.Fatalf("%v %+v", err, events)
	}
	if len(events) != 2 || events[0].Kind != Markdown || events[1] != (Event{Kind: Result, Text: "1", ID: "p"}) {
		t.Fatalf("events %+v", events)
	}
	// A notebook can declare Display (and a variable called nb) itself;
	// trailing expressions are still displayed.
	out, err := run(t, k, "2", "func Display(s string) string { return \"mine: \" + s }\nnb := 2\ns := Display(\"x\")\ns")
	if err != nil || out != "=> mine: x\n" {
		t.Fatalf("own Display: %q %v", out, err)
	}
	// Notebooks from before nb still work.
	out, err = run(t, k, "3", "DisplayMarkdown(\"old\")\nv := Cache(\"k\", func() int { return 7 })\nv")
	if err != nil || !strings.Contains(out, "old") || !strings.Contains(out, "=> 7") {
		t.Fatalf("compat: %q %v", out, err)
	}
	// Explicit import.
	out, err = run(t, k, "4", "import n \"github.com/mark3labs/gopyter/nb\"\nn.Display(3)")
	if err != nil || out != "=> 3\n" {
		t.Fatalf("explicit import: %q %v", out, err)
	}
}

// liveRun runs src with an events pipe, applying the program's operations
// to its outputs like the UI does. onEvent can send events.
func liveRun(t *testing.T, k *Kernel, id, src string, onEvent func(e Event, send func(string))) ([]notebook.Output, string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	send := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = io.WriteString(w, s) // the program may be gone
	}
	session := htmlview.NewSession()
	var outs []notebook.Output
	var stdout strings.Builder
	err = k.ExecuteInput(context.Background(), id, "In["+id+"]", src, Input{Events: r}, func(e Event) {
		switch e.Kind {
		case Op:
			res := session.Apply(outs, e.Text)
			if res.Changed {
				outs = res.Outputs
			}
			if res.Reply != nil {
				send(string(res.Reply))
			}
		case HTML:
			found := false
			for i := range outs {
				if e.ID != "" && outs[i].ID == e.ID {
					outs[i].Text, found = e.Text, true
				}
			}
			if !found {
				outs = append(outs, notebook.Output{Kind: notebook.HTMLOut, Text: e.Text, ID: e.ID})
			}
		case Stdout, Error, Stderr:
			stdout.WriteString(e.Text)
		}
		if onEvent != nil {
			onEvent(e, send)
		}
	})
	_ = w.Close()
	_ = r.Close()
	return outs, stdout.String(), err
}

func TestGoNBWidgets(t *testing.T) {
	k := newTestKernel(t)
	src := `import "github.com/janpfeifer/gonb/gonbui/widgets"
%%
s := widgets.Slider(0, 100, 50).WithAddress("s").Done()
b := widgets.Button("go").WithAddress("b").Done()
ch, bc := s.Listen(), b.Listen()
fmt.Println("ready")
for {
	select {
	case v, ok := <-ch.C:
		if !ok {
			fmt.Println("done", s.Value())
			return
		}
		fmt.Println("slider", v)
	case n, ok := <-bc.C:
		if !ok {
			fmt.Println("done", s.Value())
			return
		}
		fmt.Println("click", n)
	}
}`
	outs, out, err := liveRun(t, k, "1", src, func(e Event, send func(string)) {
		switch {
		case e.Kind == Stdout && strings.Contains(e.Text, "ready"):
			send(`{"address":"s","value":63}` + "\n")
		case e.Kind == Stdout && strings.Contains(e.Text, "slider 63"):
			send(`{"address":"b","value":1}` + "\n")
		case e.Kind == Stdout && strings.Contains(e.Text, "click 1"):
			send(htmlview.DoneEvent)
		}
	})
	if err != nil || out != "ready\nslider 63\nclick 1\ndone 63\n" {
		t.Fatalf("%q %v", out, err)
	}
	if ws := htmlview.Widgets(outs); len(ws) != 2 || ws[0].Kind != htmlview.Slider || ws[1].Label != "go" {
		t.Fatalf("widgets %+v in %+v", ws, outs)
	}
	// Without a UI (no events) the widgets are done at once. Their HTML is
	// shown where they were created, before "ready".
	out, err = run(t, k, "2", src)
	if err != nil || !strings.HasPrefix(out, "<input") || !strings.HasSuffix(out, "</button>ready\ndone 50\n") || strings.Contains(out, "not kept") {
		t.Fatalf("no UI: %q %v", out, err)
	}
}

func TestGoNBDom(t *testing.T) {
	k := newTestKernel(t)
	// As GoNB's own dom test, plus SetValue and RequestInput.
	src := `%%
rootId := dom.CreateTransientDiv()
dom.SetInnerHtml(rootId, "This is a test!<br>\n")
dom.Append(rootId, "And a second test.<br>\n")
contents := dom.GetInnerHtml(rootId)
removeId := "dom_test_" + gonbui.UniqueID()
dom.Append(rootId, fmt.Sprintf(` + "`" + `<div id="%s">This shouldn't be here</div>` + "`" + `, removeId))
dom.Remove(removeId)
dom.Append(rootId, contents)
gonbui.Sync()
fmt.Println(comms.ReadValue[int]("nothing"))
gonbui.UpdateHtml("u", "<b>1</b>")
gonbui.UpdateHtml("u", "<b>2</b>")`
	outs, out, err := liveRun(t, k, "1", src, nil)
	if err != nil || out != "0\n" {
		t.Fatalf("%q %v", out, err)
	}
	if len(outs) != 2 {
		t.Fatalf("outputs %+v", outs)
	}
	text := htmlview.Text(outs[0].Text)
	if strings.Count(text, "This is a test!") != 2 || strings.Count(text, "And a second test.") != 2 || strings.Contains(text, "shouldn't") {
		t.Fatalf("dom: %q", text)
	}
	if outs[1].Text != "<b>2</b>" {
		t.Fatalf("update: %+v", outs[1])
	}
}
