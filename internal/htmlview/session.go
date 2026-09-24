package htmlview

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/mark3labs/gopyter/internal/notebook"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// DoneEvent tells the program the user is done with its input.
const DoneEvent = `{"done":true}` + "\n"

// Session holds the front-end state of a running cell's program: values of
// addresses and button clicks. Its methods change the cell's outputs.
type Session struct {
	values map[string]json.RawMessage
	clicks map[string]int
}

// NewSession returns a session for a new run.
func NewSession() *Session {
	return &Session{values: map[string]json.RawMessage{}, clicks: map[string]int{}}
}

// InputRequest asks the user for a line of input (gonbui.RequestInput).
type InputRequest struct {
	Prompt   string
	Password bool
}

// Result is the outcome of Apply.
type Result struct {
	// Outputs is the new outputs, when they changed.
	Outputs []notebook.Output
	Changed bool
	// Reply is an event to send to the program, or nil.
	Reply []byte
	// Input is set when the program asks for input.
	Input *InputRequest
}

type op struct {
	Op      string          `json:"op"`
	Action  string          `json:"action"`
	Target  string          `json:"target"`
	Pos     string          `json:"pos"`
	Data    string          `json:"data"`
	Address string          `json:"address"`
	Value   json.RawMessage `json:"value"`
	Reply   string          `json:"reply"`
	Prompt  string          `json:"prompt"`
	Pass    bool            `json:"password"`
}

// event encodes a value for the program.
func event(address string, v any) []byte {
	b, err := json.Marshal(map[string]any{"address": address, "value": v})
	if err != nil {
		return nil
	}
	return append(b, '\n')
}

// Apply applies an operation (a kernel Op event) to outs.
func (s *Session) Apply(outs []notebook.Output, raw string) Result {
	var o op
	if json.Unmarshal([]byte(raw), &o) != nil {
		return Result{}
	}
	switch o.Op {
	case "value":
		s.values[o.Address] = o.Value
		var v any
		if json.Unmarshal(o.Value, &v) != nil {
			return Result{}
		}
		outs, changed := setWidgetValue(outs, o.Address, v)
		return Result{Outputs: outs, Changed: changed}
	case "read":
		v := s.value(outs, o.Address)
		to := o.Reply
		if to == "" {
			to = o.Address
		}
		return Result{Reply: event(to, v)}
	case "sync":
		return Result{Reply: event(o.Reply, 1)}
	case "input":
		return Result{Input: &InputRequest{Prompt: o.Prompt, Password: o.Pass}}
	case "dom":
		return s.dom(outs, o)
	}
	return Result{}
}

// value is the current value at address: the last one set, or the value
// of the widget there.
func (s *Session) value(outs []notebook.Output, address string) any {
	if v, ok := s.values[address]; ok {
		return v
	}
	for _, w := range Widgets(outs) {
		if w.Address == address {
			if w.Kind == Button {
				return s.clicks[address]
			}
			return w.Value
		}
	}
	return nil
}

// SetValue sets the value of a slider or select from the UI, returning
// the new outputs and the event to send to the program.
func (s *Session) SetValue(outs []notebook.Output, address string, v int) ([]notebook.Output, []byte) {
	s.values[address] = json.RawMessage(strconv.Itoa(v))
	outs, _ = setWidgetValue(outs, address, float64(v))
	return outs, event(address, v)
}

// Click counts a click of a button, returning the event to send.
func (s *Session) Click(address string) []byte {
	s.clicks[address]++
	return event(address, s.clicks[address])
}

// Widgets returns the widgets of the HTML outputs (their positions aren't
// set).
func Widgets(outs []notebook.Output) []Widget {
	var ws []Widget
	for _, o := range outs {
		if o.Kind == notebook.HTMLOut && strings.Contains(o.Text, "data-address") {
			_, w := Render(o.Text, Options{Width: 200, Live: true})
			ws = append(ws, w...)
		}
	}
	return ws
}

// editHTML calls fn on the parsed HTML outputs, from the last one, until
// it returns true; the output it changed is serialized back. It returns
// the outputs (copied if changed) and whether one changed.
func editHTML(outs []notebook.Output, fn func(root *html.Node) bool) ([]notebook.Output, bool) {
	for i := len(outs) - 1; i >= 0; i-- {
		if outs[i].Kind != notebook.HTMLOut {
			continue
		}
		root, err := parseRoot(outs[i].Text)
		if err != nil || !fn(root) {
			continue
		}
		outs = append([]notebook.Output(nil), outs...)
		outs[i].Text = renderChildren(root)
		return outs, true
	}
	return outs, false
}

// parseRoot parses an HTML output under a synthetic <body>.
func parseRoot(src string) (*html.Node, error) {
	nodes, err := parseFragment(src)
	if err != nil {
		return nil, err
	}
	root := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	return root, nil
}

func renderChildren(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&b, c) // writing to a strings.Builder can't fail
	}
	return b.String()
}

func findID(n *html.Node, id string) *html.Node {
	if n.Type == html.ElementNode {
		if v, ok := attr(n, "id"); ok && v == id {
			return n
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := findID(c, id); f != nil {
			return f
		}
	}
	return nil
}

func setAttr(n *html.Node, key, val string) {
	for i := range n.Attr {
		if n.Attr[i].Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}

func delAttr(n *html.Node, key string) {
	for i := range n.Attr {
		if n.Attr[i].Key == key {
			n.Attr = append(n.Attr[:i], n.Attr[i+1:]...)
			return
		}
	}
}

// setWidgetValue sets the value of the sliders and selects at address in
// every HTML output.
func setWidgetValue(outs []notebook.Output, address string, v any) ([]notebook.Output, bool) {
	f, ok := v.(float64)
	if !ok {
		if str, isStr := v.(string); isStr {
			var err error
			if f, err = strconv.ParseFloat(str, 64); err != nil {
				return outs, false
			}
		} else {
			return outs, false
		}
	}
	n := strconv.Itoa(int(f))
	changed := false
	for i, o := range outs {
		if o.Kind != notebook.HTMLOut || !strings.Contains(o.Text, address) {
			continue
		}
		root, err := parseRoot(o.Text)
		if err != nil {
			continue
		}
		hit := false
		var walk func(*html.Node)
		walk = func(e *html.Node) {
			if e.Type == html.ElementNode {
				if a, ok := attr(e, "data-address"); ok && a == address {
					switch e.DataAtom {
					case atom.Input:
						setAttr(e, "value", n)
						hit = true
					case atom.Select:
						i := 0
						for c := e.FirstChild; c != nil; c = c.NextSibling {
							if c.DataAtom != atom.Option {
								continue
							}
							if strconv.Itoa(i) == n {
								setAttr(c, "selected", "")
							} else {
								delAttr(c, "selected")
							}
							i++
						}
						hit = true
					}
				}
			}
			for c := e.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(root)
		if hit {
			if !changed {
				outs = append([]notebook.Output(nil), outs...)
			}
			outs[i].Text = renderChildren(root)
			changed = true
		}
	}
	return outs, changed
}

// dom applies a gonbui/dom operation.
func (s *Session) dom(outs []notebook.Output, o op) Result {
	if o.Action == "get" {
		inner := ""
		editHTML(outs, func(root *html.Node) bool {
			if n := findID(root, o.Target); n != nil {
				inner = renderChildren(n)
				return true
			}
			return false
		})
		return Result{Reply: event(o.Reply, inner)}
	}
	outs, changed := editHTML(outs, func(root *html.Node) bool {
		n := findID(root, o.Target)
		if n == nil {
			return false
		}
		switch o.Action {
		case "insert":
			parent := n
			if o.Pos == "beforebegin" || o.Pos == "afterend" {
				parent = n.Parent
			}
			if parent == nil {
				return false
			}
			frag, err := html.ParseFragment(strings.NewReader(o.Data), parent)
			if err != nil {
				return false
			}
			first, next := n.FirstChild, n.NextSibling
			for _, c := range frag {
				switch o.Pos {
				case "beforebegin":
					n.Parent.InsertBefore(c, n)
				case "afterbegin":
					n.InsertBefore(c, first)
				case "afterend":
					n.Parent.InsertBefore(c, next)
				default: // beforeend
					n.AppendChild(c)
				}
			}
		case "set_html":
			frag, err := html.ParseFragment(strings.NewReader(o.Data), n)
			if err != nil {
				return false
			}
			for n.FirstChild != nil {
				n.RemoveChild(n.FirstChild)
			}
			for _, c := range frag {
				n.AppendChild(c)
			}
		case "set_text":
			for n.FirstChild != nil {
				n.RemoveChild(n.FirstChild)
			}
			n.AppendChild(&html.Node{Type: html.TextNode, Data: o.Data})
		case "remove":
			if n.Parent == nil {
				return false
			}
			n.Parent.RemoveChild(n)
		default:
			return false
		}
		return true
	})
	return Result{Outputs: outs, Changed: changed}
}
