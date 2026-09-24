package htmlview

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func plain(src string, width int) []string {
	lines, _ := Render(src, Options{Width: width, MaxImageLines: 4})
	for i, l := range lines {
		lines[i] = ansi.Strip(l)
	}
	return lines
}

func TestRender(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		width    int
		want     []string
	}{
		{"inline", "Count: <b>3</b> and <i>more</i>", 40, []string{"Count: 3 and more"}},
		{"collapse spaces", "a \n\t  b", 40, []string{"a b"}},
		{"wrap", "one two three four", 12, []string{"one two", "three four"}},
		{"paragraphs", "<p>a</p><p>b</p>", 40, []string{"a", "", "b"}},
		{"br", "a<br>b<br/><br>c", 40, []string{"a", "b", "", "c"}},
		{"list", "<ul><li>x</li><li>y z</li></ul>", 40, []string{"  • x", "  • y z"}},
		{"ordered", "<ol><li>x</li><li>y</li></ol>", 40, []string{"  1. x", "  2. y"}},
		{"pre", "<pre>a  b\n  c</pre>", 40, []string{"a  b", "  c"}},
		{"table", "<table><tr><th>k</th><td>v</td></tr><tr><td>1</td><td>2</td></tr></table>", 40, []string{"k │ v", "1 │ 2"}},
		{"skipped", "<style>x{}</style><script>alert(1)</script>ok", 40, []string{"ok"}},
		{"svg", "<div><svg><rect/></svg></div>", 40, []string{"[svg image]"}},
		{"entities", "a&nbsp;&lt;b&gt;", 40, []string{"a <b>"}},
		{"image alt", `<img src="x.png" alt="cat">`, 40, []string{"[image: cat]"}},
		{"long word", "abcdefghijklmnopqrstuvw", 10, []string{"abcdefghij", "klmnopqrst", "uvw"}},
	} {
		got := plain(tc.in, tc.width)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRenderImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	px := base64.StdEncoding.EncodeToString(buf.Bytes())
	lines, _ := Render(`before<img src="data:image/png;base64,`+px+`">after`, Options{Width: 20, MaxImageLines: 4})
	if len(lines) != 3 || !strings.Contains(lines[1], "▀") {
		t.Fatalf("lines %q", lines)
	}
	if got := Text(`<img src="data:image/png;base64,` + px + `">`); got != "[image 2x2]" {
		t.Fatalf("text %q", got)
	}
}

func TestRenderWidgets(t *testing.T) {
	src := `<div id="d">Freq <input type="range" min="0" max="100" value="50" data-address="/s"/>` +
		`<button data-address="/b">Ok</button> <select data-address="/c"><option>a</option><option selected>bb</option></select>` +
		`<button>inert</button></div>`
	lines, ws := Render(src, Options{Width: 80, Live: true, Focus: "/b"})
	if len(lines) != 1 || len(ws) != 3 {
		t.Fatalf("lines %q widgets %+v", lines, ws)
	}
	s, b, c := ws[0], ws[1], ws[2]
	if s.Kind != Slider || s.Value != 50 || s.Min != 0 || s.Max != 100 || s.X0 != 5 {
		t.Fatalf("slider %+v", s)
	}
	text := ansi.Strip(lines[0])
	if got := text[len("Freq "):]; !strings.HasPrefix(got, "━━━") {
		t.Fatalf("slider drawn as %q", text)
	}
	if s.ValueAt(s.TrackX0) != 0 || s.ValueAt(s.TrackX0+s.TrackW-1) != 100 || s.ValueAt(s.TrackX0+s.TrackW/2) < 40 {
		t.Fatalf("ValueAt: %d %d", s.ValueAt(s.TrackX0), s.ValueAt(s.TrackX0+s.TrackW-1))
	}
	if b.Kind != Button || b.Label != "Ok" || c.Kind != Select || c.Value != 1 || strings.Join(c.Options, ",") != "a,bb" {
		t.Fatalf("button %+v select %+v", b, c)
	}
	for _, w := range ws {
		if got := []rune(text)[w.X0:w.X1]; len(got) != w.X1-w.X0 {
			t.Fatalf("bad columns for %+v", w)
		}
	}
	if !strings.Contains(text, " Ok ") || !strings.Contains(text, " bb ▾ ") || !strings.Contains(text, " inert ") {
		t.Fatalf("text %q", text)
	}
	// Not live: nothing is interactive.
	if _, ws := Render(src, Options{Width: 80}); len(ws) != 0 {
		t.Fatalf("inactive widgets reported: %+v", ws)
	}
}

func htmlOuts(texts ...string) []notebook.Output {
	var outs []notebook.Output
	for _, t := range texts {
		outs = append(outs, notebook.Output{Kind: notebook.HTMLOut, Text: t})
	}
	return outs
}

func apply(t *testing.T, s *Session, outs []notebook.Output, op map[string]any) ([]notebook.Output, Result) {
	t.Helper()
	b, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	r := s.Apply(outs, string(b))
	if r.Changed {
		outs = r.Outputs
	}
	return outs, r
}

func TestSessionDOM(t *testing.T) {
	s := NewSession()
	outs := htmlOuts(`<div id="root"></div>`)
	outs, r := apply(t, s, outs, map[string]any{"op": "dom", "action": "insert", "target": "root", "pos": "beforeend", "data": "a <span id=\"x\">1</span>"})
	if !r.Changed || outs[0].Text != `<div id="root">a <span id="x">1</span></div>` {
		t.Fatalf("append: %q", outs[0].Text)
	}
	outs, _ = apply(t, s, outs, map[string]any{"op": "dom", "action": "set_text", "target": "x", "data": "<2>"})
	outs, _ = apply(t, s, outs, map[string]any{"op": "dom", "action": "insert", "target": "root", "pos": "afterbegin", "data": "<i>p</i><i>q</i>"})
	if outs[0].Text != `<div id="root"><i>p</i><i>q</i>a <span id="x">&lt;2&gt;</span></div>` {
		t.Fatalf("set_text/afterbegin: %q", outs[0].Text)
	}
	_, r = apply(t, s, outs, map[string]any{"op": "dom", "action": "get", "target": "root", "reply": "#r"})
	var ev struct {
		Address string
		Value   string
	}
	if json.Unmarshal(r.Reply, &ev) != nil || ev.Address != "#r" || ev.Value != `<i>p</i><i>q</i>a <span id="x">&lt;2&gt;</span>` {
		t.Fatalf("get: %s", r.Reply)
	}
	outs, _ = apply(t, s, outs, map[string]any{"op": "dom", "action": "remove", "target": "x"})
	outs, _ = apply(t, s, outs, map[string]any{"op": "dom", "action": "set_html", "target": "root", "data": "<b>new</b>"})
	if outs[0].Text != `<div id="root"><b>new</b></div>` {
		t.Fatalf("remove/set_html: %q", outs[0].Text)
	}
	if _, r = apply(t, s, outs, map[string]any{"op": "dom", "action": "remove", "target": "nosuch"}); r.Changed {
		t.Fatal("unknown target changed outputs")
	}
}

func TestSessionValues(t *testing.T) {
	s := NewSession()
	outs := htmlOuts(`<input type="range" min="0" max="10" value="5" data-address="/s"/>`,
		`<select data-address="/c"><option selected>a</option><option>b</option></select><button data-address="/b">go</button>`)

	// The program sets values; reads see them.
	outs, r := apply(t, s, outs, map[string]any{"op": "value", "address": "/s", "value": 7})
	if !r.Changed || !strings.Contains(outs[0].Text, `value="7"`) {
		t.Fatalf("value: %q", outs[0].Text)
	}
	_, r = apply(t, s, outs, map[string]any{"op": "read", "address": "/s", "reply": "#r"})
	if string(r.Reply) != `{"address":"#r","value":7}`+"\n" {
		t.Fatalf("read: %s", r.Reply)
	}
	// The UI sets values and clicks.
	outs, ev := s.SetValue(outs, "/c", 1)
	if string(ev) != `{"address":"/c","value":1}`+"\n" || !strings.Contains(outs[1].Text, `<option selected="">b</option>`) {
		t.Fatalf("SetValue: %s %q", ev, outs[1].Text)
	}
	if ws := Widgets(outs); len(ws) != 3 || ws[1].Value != 1 {
		t.Fatalf("widgets %+v", ws)
	}
	s.Click("/b")
	if ev := s.Click("/b"); string(ev) != `{"address":"/b","value":2}`+"\n" {
		t.Fatalf("click: %s", ev)
	}
	_, r = apply(t, s, outs, map[string]any{"op": "read", "address": "/b"})
	if string(r.Reply) != `{"address":"/b","value":2}`+"\n" {
		t.Fatalf("read clicks: %s", r.Reply)
	}
	if _, r = apply(t, s, outs, map[string]any{"op": "input", "prompt": "pw", "password": true}); r.Input == nil || !r.Input.Password || r.Input.Prompt != "pw" {
		t.Fatalf("input: %+v", r)
	}
}
