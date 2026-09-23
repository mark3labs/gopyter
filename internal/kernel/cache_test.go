package kernel

import (
	"slices"
	"strings"
	"testing"
)

func newTestKernel(t *testing.T) *Kernel {
	t.Helper()
	k, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Close(); err != nil {
			t.Error(err)
		}
	})
	return k
}

// A cached global's initializer must run once, not in every later cell.
func TestCachePersistsAcrossCells(t *testing.T) {
	k := newTestKernel(t)
	out, err := run(t, k, "1", `type Resp struct{ N int; Tags map[string]string }
func expensive() (*Resp, error) {
	fmt.Println("computing")
	return &Resp{N: 42, Tags: map[string]string{"a": "b"}}, nil
}
var resp, err = CacheErr("resp", expensive)
var plain = Cache("plain", func() []int { fmt.Println("plain"); return []int{1, 2} })`)
	if err != nil || out != "computing\nplain\n" {
		t.Fatalf("cell1: %q %v", out, err)
	}
	out, err = run(t, k, "2", "fmt.Println(resp.N, resp.Tags[\"a\"], plain, err)")
	if err != nil || out != "42 b [1 2] <nil>\n" {
		t.Fatalf("cell2: %q %v", out, err)
	}
	keys, err := k.CacheKeys()
	if err != nil || !slices.Equal(keys, []string{"plain", "resp"}) {
		t.Fatalf("keys: %v %v", keys, err)
	}

	// Clearing a key recomputes it on the next run.
	out, err = run(t, k, "3", "%cache clear resp\nfmt.Println(resp.N)")
	if err != nil || !strings.Contains(out, "cleared resp") || !strings.Contains(out, "computing\n42\n") ||
		strings.Contains(out, "plain") {
		t.Fatalf("cell3: %q %v", out, err)
	}
}

// Failed computations are not cached, so they are retried.
func TestCacheErrNotStored(t *testing.T) {
	k := newTestKernel(t)
	src := `var v, verr = CacheErr("k/1", func() (int, error) { fmt.Println("try"); return 0, errors.New("boom") })`
	for i := range 2 {
		out, err := run(t, k, "1", src)
		if err != nil || out != "try\n" {
			t.Fatalf("run %d: %q %v", i, out, err)
		}
	}
	if keys, err := k.CacheKeys(); err != nil || len(keys) != 0 {
		t.Fatalf("keys: %v %v", keys, err)
	}
}

func TestRemoveMagic(t *testing.T) {
	k := newTestKernel(t)
	if _, err := run(t, k, "1", "import \"math\"\ntype T int\nfunc (T) M() {}\nvar a, b = 1, 2\nvar c = math.Pi"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, k, "2", "%rm T a")
	if err != nil || !strings.Contains(out, "removed T, T.M, a, b") {
		t.Fatalf("rm: %q %v", out, err)
	}
	if got := k.Declarations(); !slices.Equal(got, []string{"c"}) {
		t.Fatalf("declarations: %v", got)
	}
	out, err = run(t, k, "3", "a")
	if err == nil || !strings.Contains(out, "undefined: a") {
		t.Fatalf("a should be gone: %q %v", out, err)
	}
	out, err = run(t, k, "4", "%rm nope")
	if err != nil || !strings.Contains(out, "nothing to remove") {
		t.Fatalf("rm nope: %q %v", out, err)
	}
}
