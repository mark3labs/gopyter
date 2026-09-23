package complete

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/gopyter/internal/kernel"
)

func labels(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func find(items []Item, label string) (Item, bool) {
	for _, it := range items {
		if it.Label == label {
			return it, true
		}
	}
	return Item{}, false
}

func complete(t *testing.T, e *Engine, id, src string) Result {
	t.Helper()
	// The cursor goes where "|" is.
	i := strings.Index(src, "|")
	src = src[:i] + src[i+1:]
	before := src[:i]
	row := strings.Count(before, "\n")
	col := len([]rune(before[strings.LastIndex(before, "\n")+1:]))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := e.Complete(ctx, Request{CellID: id, Src: src, Row: row, Col: col, Manual: true})
	if err != nil {
		t.Fatalf("complete %q: %v", src, err)
	}
	return res
}

func TestGopls(t *testing.T) {
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

	// Package members, without an explicit import.
	res := complete(t, e, "a", "s := strings.ToU|")
	if res.Source != "gopls" {
		t.Fatalf("expected gopls, got %s", res.Source)
	}
	up, ok := find(res.Items, "ToUpper")
	if !ok {
		t.Fatalf("ToUpper missing from %v", labels(res.Items))
	}
	// Standard library packages are auto-imported for full type info.
	if up.Replace != 3 || up.Kind != KindFunc || !strings.HasPrefix(up.Detail, "func(s string) string") || up.Doc == "" {
		t.Fatalf("bad ToUpper item: %+v", up)
	}

	// Declarations persisted from an executed cell, and locals in main.
	err = k.Execute(context.Background(), "decl", "In[1]",
		"type Point struct{ X, Y float64 }\nfunc (p Point) Dist() float64 { return 0 }", func(kernel.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	res = complete(t, e, "b", "p := Point{1, 2}\nfmt.Println(p.|)")
	if !slices.Contains(labels(res.Items), "Dist") || !slices.Contains(labels(res.Items), "X") {
		t.Fatalf("Point members missing: %v", labels(res.Items))
	}

	// Once a build resolved an import, members get full signatures and docs.
	if err := k.Execute(context.Background(), "use", "In[2]", "_ = strings.ToUpper", func(kernel.Event) {}); err != nil {
		t.Fatal(err)
	}
	res = complete(t, e, "a", "s := strings.ToU|")
	if up, _ = find(res.Items, "ToUpper"); !strings.HasPrefix(up.Detail, "func(s string) string") || up.Doc == "" {
		t.Fatalf("expected signature and docs: %+v", up)
	}

	// Inside a function declared in the cell being edited (incomplete code).
	res = complete(t, e, "c", "func f(name string) {\n\tfmt.Println(na|")
	if !slices.Contains(labels(res.Items), "name") {
		t.Fatalf("parameter missing: %v", labels(res.Items))
	}

	// Multi-byte text before the cursor keeps columns right.
	res = complete(t, e, "d", "héllo := \"日本\"; x := héllo + strings.Rep|")
	if it, ok := find(res.Items, "Repeat"); !ok || it.Replace != 3 {
		t.Fatalf("Repeat: %+v in %v", it, labels(res.Items))
	}
}

func TestBasic(t *testing.T) {
	k, err := kernel.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Close() })
	e := &Engine{k: k}
	res := e.basic(Request{Src: "counter := 1\nco", Row: 1, Col: 2})
	got := labels(res.Items)
	for _, want := range []string{"counter", "const", "continue", "copy", "complex"} {
		if !slices.Contains(got, want) {
			t.Fatalf("%s missing from %v", want, got)
		}
	}
	if res.Replace != 2 {
		t.Fatalf("replace = %d", res.Replace)
	}
}

func hover(t *testing.T, e *Engine, id, src string) string {
	t.Helper()
	// The cursor goes where "|" is.
	i := strings.Index(src, "|")
	src = src[:i] + src[i+1:]
	before := src[:i]
	row := strings.Count(before, "\n")
	col := len([]rune(before[strings.LastIndex(before, "\n")+1:]))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	md, err := e.Hover(ctx, Request{CellID: id, Src: src, Row: row, Col: col})
	if err != nil {
		t.Fatalf("hover %q: %v", src, err)
	}
	return md
}

func TestHover(t *testing.T) {
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

	// A standard library function, without an explicit import, with the
	// cursor inside the name and just past it.
	for _, src := range []string{`x := strings.Sp|lit("a,b", ",")`, `x := strings.Split|`} {
		md := hover(t, e, "a", src)
		if !strings.Contains(md, "func strings.Split(s string, sep string) []string") || !strings.Contains(md, "Split slices s") {
			t.Fatalf("hover %q:\n%s", src, md)
		}
		// Rules and links to local source files are cleaned up.
		if strings.Contains(md, "---") || strings.Contains(md, "file://") {
			t.Fatalf("hover %q not cleaned:\n%s", src, md)
		}
	}

	// A struct persisted from an executed cell shows its fields.
	err = k.Execute(context.Background(), "decl", "In[1]",
		"type Point struct{ X, Y float64 }", func(kernel.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if md := hover(t, e, "b", "p := Poi|nt{1, 2}"); !strings.Contains(md, "type Point struct") ||
		!strings.Contains(md, "X, Y float64") {
		t.Fatalf("struct hover:\n%s", md)
	}

	// Among call arguments, the enclosing call's signature.
	if md := hover(t, e, "b", `x := strings.Split("a", |)`); !strings.Contains(md, "Split(s string, sep string) []string") {
		t.Fatalf("signature help:\n%s", md)
	}

	// Nothing to show on a literal or blank space.
	for _, src := range []string{"x := 1|", "|"} {
		if md := hover(t, e, "b", src); md != "" {
			t.Fatalf("hover %q: want nothing, got\n%s", src, md)
		}
	}
}

func TestCleanHover(t *testing.T) {
	in := "```go\nfunc F()\n```\n\n---\n\nSee [G](file:///x/y.go#1,2), [https://go.dev](https://go.dev) and [docs](https://pkg.go.dev)."
	want := "```go\nfunc F()\n```\n\nSee G, https://go.dev and [docs](https://pkg.go.dev)."
	if got := cleanHover(in); got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}
