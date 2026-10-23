package kernel

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func variableMap(k *Kernel) map[string]Variable {
	out := map[string]Variable{}
	for _, v := range k.Variables() {
		out[v.Name] = v
	}
	return out
}

func TestVariablesKindsTypesValues(t *testing.T) {
	k := newTestKernel(t)
	runCells(t, k, []cellCase{{id: "1", src: `a := 1
s := "hi\nthere"
type P struct{ X, Y int }
var p = &P{3,4}
const Pi = 3.14
func Add(x, y int) int { return x+y }
func (P) M() {}
func init_xxx() {}
n, err := strconv.Atoi("4")`}})
	vs := variableMap(k)
	for name, want := range map[string]Variable{
		"a":   {Kind: "var", Type: "int", Value: "1", Hoisted: true},
		"s":   {Kind: "var", Type: "string", Value: `"hi\nthere"`, Hoisted: true},
		"p":   {Kind: "var", Type: "*P", Value: "&{3 4}"},
		"Pi":  {Kind: "const", Value: "3.14"},
		"Add": {Kind: "func", Type: "func(x, y int) int"},
		"P":   {Kind: "type", Type: "struct"},
		"err": {Kind: "var", Type: "error", Value: "<nil>", Hoisted: true},
	} {
		got := vs[name]
		if got.Kind != want.Kind || got.Type != want.Type || got.Value != want.Value || got.Hoisted != want.Hoisted || got.CellID != "1" {
			t.Errorf("%s = %+v, want %+v", name, got, want)
		}
	}
	if len(vs) != 8 {
		t.Errorf("unexpected entries: %+v", vs)
	}
	if !slices.IsSortedFunc(k.Variables(), func(a, b Variable) int { return strings.Compare(a.Name, b.Name) }) {
		t.Fatal("unsorted")
	}
}

func TestVariablesPreviewBounds(t *testing.T) {
	k := newTestKernel(t)
	runCells(t, k, []cellCase{{id: "1", src: `big := make([]int,1000)
a := [30]int{}
m := make(map[int]int)
for i:=0;i<100;i++ { m[i]=i }
f := func() {}
const huge = 1 << 100
s := strings.Repeat("界",200)`}})
	vs := variableMap(k)
	for _, n := range []string{"big", "a"} {
		if !strings.HasPrefix(vs[n].Value, "len ") {
			t.Errorf("%s: %+v", n, vs[n])
		}
	}
	if vs["m"].Value != "map len 100" || vs["f"].Type != "func()" || vs["f"].Value != "" || vs["huge"].Value != "1 << 100" {
		t.Fatalf("previews: %+v", vs)
	}
	for _, v := range vs {
		if utf8.RuneCountInString(v.Value) > 120 || strings.Contains(v.Value, "\n") {
			t.Errorf("unbounded: %+v", v)
		}
	}
}

func TestVariablesPreviewTimeout(t *testing.T) {
	k := newTestKernel(t)
	// run uses the test's bounded context: a blocked preview must not block save.
	runCells(t, k, []cellCase{{id: "1", src: `type Blocker struct{ X int }
func (Blocker) String() string { select {} }
v := Blocker{1}
w := 5`}})
	vs := variableMap(k)
	if vs["v"].Type != "Blocker" || vs["v"].Value != "" || vs["w"].Value != "5" {
		t.Fatalf("timeout previews: %+v", vs)
	}
	runCells(t, k, []cellCase{{id: "2", src: "w", want: "=> 5"}})
}

func TestVariablesPanickingStringer(t *testing.T) {
	k := newTestKernel(t)
	runCells(t, k, []cellCase{{id: "1", src: `type P struct{ X int }
func (P) String() string { panic("oops") }
p := P{3}`}})
	if !strings.Contains(variableMap(k)["p"].Value, "PANIC") {
		t.Fatal(k.Variables())
	}
	runCells(t, k, []cellCase{{id: "2", src: "p.X", want: "=> 3"}})
}

func TestVariablesLifecycle(t *testing.T) {
	k := newTestKernel(t)
	runCells(t, k, []cellCase{{id: "1", src: "a := 1"}, {id: "2", src: `func main() { fmt.Println("x") }`}})
	if variableMap(k)["a"].Value != "1" {
		t.Fatal(k.Variables())
	}
	runCells(t, k, []cellCase{{id: "3", src: "var x = 1"}, {id: "3", src: "var x = \"s\"\nos.Exit(0)"}})
	if v := variableMap(k)["x"]; v.Name != "x" || v.Value != "" {
		t.Fatalf("stale preview: %+v", v)
	}
	runCells(t, k, []cellCase{{id: "4", src: "ch := make(chan int)"}, {id: "5", src: "%rm a"}})
	vs := variableMap(k)
	if _, ok := vs["a"]; ok {
		t.Fatal("a not removed")
	}
	if _, ok := vs["ch"]; ok {
		t.Fatal("unsavable ch retained")
	}
	runCells(t, k, []cellCase{{id: "6", src: "%reset"}})
	if len(k.Variables()) != 0 {
		t.Fatal(k.Variables())
	}
}
