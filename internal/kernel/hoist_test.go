package kernel

import (
	"slices"
	"strings"
	"testing"
)

type cellCase struct {
	id, src, want string
	fail          bool // compile or run error expected
}

func runCells(t *testing.T, k *Kernel, cells []cellCase) {
	t.Helper()
	for _, c := range cells {
		out, err := run(t, k, c.id, c.src)
		if (err != nil) != c.fail || !strings.Contains(out, c.want) {
			t.Fatalf("cell %s %q: got %q, err %v", c.id, c.src, out, err)
		}
	}
}

// Variables declared with := at the top level carry over to later cells.
func TestHoistedVariables(t *testing.T) {
	t.Parallel()
	k := newTestKernel(t)
	runCells(t, k, []cellCase{
		// Not "declared and not used" anymore.
		{id: "1", src: "a := 1"},
		{id: "2", src: "fmt.Println(a)", want: "1\n"},
		// Updates persist, and the := sees the previous value.
		{id: "3", src: "a++"},
		{id: "4", src: "a", want: "=> 2"},
		{id: "5", src: "a := a * 10\nfmt.Println(a)", want: "20\n"},
		{id: "4", src: "a", want: "=> 20"},
		// Re-running the first cell resets it.
		{id: "1", src: "a := 1"},
		{id: "4", src: "a", want: "=> 1"},
		// Several variables, errors and trailing expressions.
		{id: "6", src: "n, err := strconv.Atoi(\"42\")\n_, perr := strconv.Atoi(\"x\")\nf := 1.5\nn + 1", want: "=> 43"},
		{id: "7", src: "fmt.Println(n, err, f)\nfmt.Println(perr)", want: "42 <nil> 1.5\nstrconv.Atoi: parsing \"x\": invalid syntax\n"},
		// Types of the notebook and of other packages, and names that
		// clash with the kernel's helpers.
		{id: "8", src: "type P struct{ X int }\np := &P{3}\nu, _ := url.Parse(\"http://h/path\")\nbytes := []byte(\"hi\")\nm := map[string][]P{\"k\": {{1}}}"},
		{id: "9", src: "fmt.Println(p.X, u.Path, len(bytes), m[\"k\"][0].X)", want: "3 /path 2 1\n"},
		// Package-level declarations can use them.
		{id: "10", src: "var twice = a * 2\ntwice", want: "=> 2"},
		// Re-running reads the value saved by the last run.
		{id: "13", src: "c := 1"},
		{id: "14", src: "c := c + 1\nc", want: "=> 2"},
		{id: "14", src: "c := c + 1\nc", want: "=> 3"},
		// Redefined with another type.
		{id: "13", src: "c := \"s\""},
		{id: "4", src: "c + \"!\"", want: "=> s!"},
		// := in nested blocks stays local.
		{id: "11", src: "for i := 0; i < 1; i++ {\n\tj := i\n\t_ = j\n}\nif v := 1; v > 0 {\n}"},
		{id: "12", src: "fmt.Println(i)", want: "In[12]:1:13: undefined: i", fail: true},
	})
	decls := k.Declarations()
	for _, n := range []string{"a", "n", "err", "perr", "f", "p", "u", "bytes", "m"} {
		if !slices.Contains(decls, n) {
			t.Errorf("%s not declared: %v", n, decls)
		}
	}
}

func TestHoistedFuncLiterals(t *testing.T) {
	t.Parallel()
	k := newTestKernel(t)
	runCells(t, k, []cellCase{
		{id: "1", src: "k := 3\ndouble := func(x int) int { return x * 2 }\nmul := func(x int) int { return double(x) * k }"},
		{id: "2", src: "mul(2)", want: "=> 12"},
		{id: "3", src: "k = 10"},
		{id: "2", src: "mul(2)", want: "=> 40"},
	})
}

func TestHoistUnsavable(t *testing.T) {
	t.Parallel()
	k := newTestKernel(t)
	runCells(t, k, []cellCase{
		{id: "1", src: "ch := make(chan int, 1)\nre := regexp.MustCompile(\"a+\")\nok := re.MatchString(\"aa\")\nch <- 1\nfmt.Println(<-ch)", want: "1\n"},
		{id: "2", src: "ok", want: "=> true"},
		{id: "3", src: "ch", want: "In[3]:1:1: undefined: ch", fail: true},
		{id: "4", src: "re", want: "undefined: re", fail: true},
	})
	out, _ := run(t, k, "1", "ch := make(chan int)\n_ = ch")
	if !strings.Contains(out, "ch is not kept for later cells: chan values can't be saved") {
		t.Fatalf("no note about ch: %q", out)
	}
}

// Values assigned before a panic are kept; a variable of a program that
// exits without saving is forgotten.
func TestHoistPanicAndExit(t *testing.T) {
	t.Parallel()
	k := newTestKernel(t)
	runCells(t, k, []cellCase{
		{id: "1", src: "z := 5\npanic(\"boom\")", want: "boom", fail: true},
		{id: "2", src: "z", want: "=> 5"},
		{id: "3", src: "y := 1\nos.Exit(0)"},
		{id: "4", src: "y", want: "undefined: y", fail: true},
	})
}

func TestHoistRemoveAndReset(t *testing.T) {
	t.Parallel()
	k := newTestKernel(t)
	runCells(t, k, []cellCase{
		{id: "1", src: "a := 7\nb := 8"},
		{id: "2", src: "%rm a"},
		{id: "3", src: "a", want: "undefined: a", fail: true},
		{id: "3", src: "b", want: "=> 8"},
		{id: "4", src: "%reset"},
		{id: "3", src: "b", want: "undefined: b", fail: true},
		// A new variable of the same name doesn't see the old value.
		{id: "5", src: "var b int\nb", want: "=> 0"},
	})
}

// Errors still point at the right cell positions, and a cell that doesn't
// compile keeps the previous variables.
func TestHoistErrors(t *testing.T) {
	t.Parallel()
	k := newTestKernel(t)
	runCells(t, k, []cellCase{
		{id: "1", src: "a := 1"},
		{id: "1", src: "a := 2\n  b := nope", want: "In[1]:2:8: undefined: nope", fail: true},
		{id: "2", src: "a", want: "=> 1"},
	})
}

func TestCompletionSeesHoisted(t *testing.T) {
	t.Parallel()
	k := newTestKernel(t)
	runCells(t, k, []cellCase{{id: "1", src: "u, _ := url.Parse(\"http://h\")"}})
	src := k.CompletionSource("2", "u.", 0, 2, nil)
	if !strings.Contains(src.Content, "var u *url.URL") {
		t.Fatalf("completion source lacks u:\n%s", src.Content)
	}
}
