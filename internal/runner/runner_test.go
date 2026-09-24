package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/gopyter/internal/htmlview"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func TestRunWidgetsAndDOM(t *testing.T) {
	k, err := kernel.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Close(); err != nil {
			t.Error(err)
		}
	})
	// As in GoNB's tutorial: widgets in a div, a loop until the button is
	// pressed. Nobody can press it here: the widgets are done at once.
	src := `import "github.com/janpfeifer/gonb/gonbui/widgets"
%%
divId := dom.CreateTransientDiv()
slider := widgets.Slider(0, 100, 50).AppendTo(divId).Done()
dom.Append(divId, "<span id=\"freq\">5.50</span>")
button := widgets.Button("Ok").AppendTo(divId).Done()
sliderChan := slider.Listen().LatestOnly()
buttonChan := button.Listen()
loop:
for {
	select {
	case <-buttonChan.C:
		break loop
	case v := <-sliderChan.C:
		dom.SetInnerText("freq", fmt.Sprint(v))
	}
}
dom.SetInnerText("freq", "final")
fmt.Println(dom.GetInnerHtml("freq"))`
	nb := &notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code, Source: src}}}
	var out bytes.Buffer
	if failed := Run(context.Background(), k, nb, &out, nil, false); failed != 0 {
		t.Fatalf("failed:\n%s", out.String())
	}
	outs := nb.Cells[0].Outputs
	if len(outs) != 2 || outs[0].Kind != notebook.HTMLOut || outs[1].Text != "final\n" {
		t.Fatalf("outputs %+v", outs)
	}
	if ws := htmlview.Widgets(outs); len(ws) != 2 {
		t.Fatalf("widgets %+v", ws)
	}
	if text := htmlview.Text(outs[0].Text); !strings.Contains(text, "final") || !strings.Contains(text, " Ok ") {
		t.Fatalf("html %q", text)
	}
}

func TestRunPrintsFinalStates(t *testing.T) {
	k, err := kernel.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Close(); err != nil {
			t.Error(err)
		}
	})
	// Without a terminal, updatable outputs are printed once, finished.
	src := `%%
for i := range 5 {
	nb.DisplayID("progress", fmt.Sprint("step ", i))
}
fmt.Println("log line")
div := dom.CreateTransientDiv()
dom.Append(div, "<b id=\"n\">0</b>")
dom.SetInnerText("n", "counted")`
	nb := &notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code, Source: src}}}
	var out bytes.Buffer
	if failed := Run(context.Background(), k, nb, &out, nil, false); failed != 0 {
		t.Fatalf("failed:\n%s", out.String())
	}
	// Only the output, not the echoed code.
	var lines []string
	for l := range strings.SplitSeq(out.String(), "\n") {
		if !strings.HasPrefix(l, "  │") {
			lines = append(lines, l)
		}
	}
	if got := strings.Join(lines[1:4], "|"); got != "log line|=> step 4|counted" {
		t.Fatalf("got %q in:\n%s", got, out.String())
	}
}

func TestBlocksRedrawOnTerminal(t *testing.T) {
	var out bytes.Buffer
	b := newBlocks(&out, true)
	b.show("id:p", "one\ntwo", true, true)
	b.show("id:p", "three", true, true) // the last block: redrawn
	b.other()
	b.show("id:p", "four", true, true) // after other output: printed again
	b.show("#0", "html", false, true)
	b.show("id:p", "five", true, false) // a DOM change to an earlier block: at the end
	b.finish()
	want := "one\ntwo\n\x1b[2A\r\x1b[Jthree\nfour\nhtml\nfive\n"
	if got := out.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
