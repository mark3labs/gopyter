package complete

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/gopyter/internal/kernel"
)

func TestMapDiagnostics(t *testing.T) {
	k, err := kernel.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Close() })
	src := "var é = \"😀\"; var s string = 2\nx := 1\nif true {\n y := 1\n}\nx\nx"
	cs := k.CompletionSource("a", src, 0, 0, nil)
	mk := func(line, col, endLine, endCol int, msg string, sev Severity) lspDiagnostic {
		d := lspDiagnostic{Severity: sev, Message: msg}
		d.Range.Start = position{line, col}
		d.Range.End = position{endLine, endCol}
		return d
	}
	var ds []lspDiagnostic
	for _, s := range cs.Segments {
		switch s.CellLine {
		case 0:
			if strings.Contains(strings.Split(cs.Content, "\n")[s.Line], "var s") {
				ds = append(ds, mk(s.Line, 28, s.Line, 29, "unicode", SeverityError))
			}
		case 1:
			ds = append(ds, mk(s.Line, 0, s.Line, 1, "declared and not used: x", SeverityError))
		case 2:
			ds = append(ds, mk(s.Line+1, 1, s.Line+1, 2, "declared and not used: y", SeverityError))
			ds = append(ds, mk(s.Line, 0, s.Line+20, 100, "clamp", SeverityWarning))
		case 5:
			ds = append(ds, mk(s.Line, 0, s.Line, 1, "x is not used", SeverityError))
		case 6:
			ds = append(ds, mk(s.Line, 0, s.Line, 1, "x is not used", SeverityError))
		}
		for _, msg := range []string{`"os" imported and not used`, "x redeclared in this block", "other declaration of x"} {
			ds = append(ds, mk(s.Line, 0, s.Line, 1, msg, SeverityError))
		}
		ds = append(ds, mk(s.Line, 0, s.Line, 1, "hint", SeverityHint), mk(s.Line, 0, s.Line, 1, "info", SeverityInfo))
	}
	ds = append(ds, mk(0, 0, 0, 1, "wrapper", SeverityError), mk(len(strings.Split(cs.Content, "\n"))-2, 0, 99, 0, "closing", SeverityError))
	got := mapDiagnostics(cs, src, ds)
	if len(got) != 4 {
		t.Fatalf("got %+v\nmirror: %s\nsegments: %+v", got, cs.Content, cs.Segments)
	}
	for _, d := range got {
		switch d.Message {
		case "unicode":
			if d.Row != 0 || d.Col != 24 {
				t.Errorf("unicode: %+v", d)
			}
		case "clamp":
			if d.EndRow != 4 || d.EndCol != 1 {
				t.Errorf("clamp: %+v", d)
			}
		case "declared and not used: y":
			if d.Row != 3 {
				t.Error(d)
			}
		case "x is not used":
			if d.Row != 5 {
				t.Error(d)
			}
		default:
			t.Error(d)
		}
	}
}

func TestDiagnoseWithoutGopls(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	e := &Engine{ready: ready, startErr: errors.New("missing")}
	ds, err := e.Diagnose(context.Background(), Request{})
	if ds != nil || err != nil {
		t.Fatalf("%v %v", ds, err)
	}
}

func TestDiagnosticPublications(t *testing.T) {
	e := &Engine{file: "/mirror.go"}
	for _, p := range []string{`{"uri":"file:///other.go","version":9}`, `{"uri":"file:///mirror.go","version":3}`, `{"uri":"file:///mirror.go","version":2}`} {
		e.notifyDiagnostics("textDocument/publishDiagnostics", json.RawMessage(p))
	}
	if e.publication.Version != 3 {
		t.Fatal(e.publication)
	}
}

func TestGoplsDiagnostics(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	k, err := kernel.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Close() })
	e := New(k)
	t.Cleanup(func() { _ = e.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := k.Execute(ctx, "prior", "In[1]", "func f() {}", func(kernel.Event) {}); err != nil {
		t.Fatal(err)
	}
	ds, err := e.Diagnose(ctx, Request{CellID: "a", Src: "func f() {}"})
	if err != nil || len(ds) != 0 {
		t.Fatalf("redeclaration: %+v %v", ds, err)
	}
	for _, src := range []string{"x := 1\nvar s string = 2", "var s string = 3", "héllo := 1; fmt.Println(héllo + \"a\")"} {
		req := Request{CellID: "a", Src: src}
		result := make(chan struct {
			ds  []Diagnostic
			err error
		}, 1)
		go func() {
			ds, err := e.Diagnose(ctx, req)
			result <- struct {
				ds  []Diagnostic
				err error
			}{ds, err}
		}()
		if _, err := e.Complete(ctx, Request{CellID: "a", Src: src, Manual: true}); err != nil {
			t.Fatal(err)
		}
		r := <-result
		if r.err != nil || len(r.ds) != 1 || r.ds[0].Severity != SeverityError {
			t.Fatalf("%q: %+v %v", src, r.ds, r.err)
		}
		if strings.HasPrefix(src, "héllo") && r.ds[0].Col != 24 {
			t.Error(r.ds[0])
		}
	}
	// Regression test: analyzer warnings were never shown, and after a run
	// nb was reported undefined.
	ds, err = e.Diagnose(ctx, Request{CellID: "a", Src: "nb.Display(1)\nfmt.Printf(\"%d\\n\", \"s\")"})
	if err != nil || len(ds) != 1 || ds[0].Severity != SeverityWarning || ds[0].Source != "printf" || ds[0].Row != 1 {
		t.Fatalf("printf: %+v %v", ds, err)
	}
}

// TestDiagnosticsWaitForDiagnosis is a regression test: gopls may publish
// a version before diagnosing it (an empty list, clearing orphaned-file
// diagnostics after the previous change), and Diagnose returned that
// (TestGoplsDiagnostics failed on macOS CI).
func TestDiagnosticsWaitForDiagnosis(t *testing.T) {
	k, err := kernel.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Close() })
	src := "var s string = 3"
	cs := k.CompletionSource("a", src, 0, 0, nil)
	line := cs.Segments[0].Line
	e := &Engine{file: "/mirror.go", mirrors: map[int]mirror{2: {cellID: "a", src: src, cs: cs, sent: 2}}}
	notify := func(method, params string) { e.notifyDiagnostics(method, json.RawMessage(params)) }
	// The previous change (1) is still being diagnosed.
	notify("$/progress", `{"token":"1","value":{"kind":"begin","title":"diagnosing changed files"}}`)
	result := make(chan []Diagnostic, 1)
	go func() {
		ds, err := e.awaitDiagnostics(context.Background(), nil, Request{CellID: "a", Src: src}, 2, 2)
		if err != nil {
			t.Error(err)
		}
		result <- ds
	}()
	real := fmt.Sprintf(`{"uri":"file:///mirror.go","version":2,"diagnostics":[{"range":{"start":{"line":%d,"character":15},"end":{"line":%d,"character":16}},"severity":1,"message":"cannot use 3"}]}`, line, line)
	for _, m := range [][2]string{
		{"textDocument/publishDiagnostics", `{"uri":"file:///mirror.go","version":2,"diagnostics":[]}`},
		{"$/progress", `{"token":"1","value":{"kind":"end"}}`},
		{"$/progress", `{"token":"2","value":{"kind":"begin","title":"diagnosing changed files"}}`},
		{"textDocument/publishDiagnostics", real},
	} {
		notify(m[0], m[1])
		select {
		case ds := <-result:
			t.Fatalf("returned %+v before the diagnosis ended", ds)
		case <-time.After(20 * time.Millisecond):
		}
	}
	notify("$/progress", `{"token":"2","value":{"kind":"end"}}`)
	select {
	case ds := <-result:
		if len(ds) != 1 || ds[0].Message != "cannot use 3" {
			t.Fatalf("%+v", ds)
		}
	case <-time.After(time.Second):
		t.Fatal("still waiting")
	}
}
