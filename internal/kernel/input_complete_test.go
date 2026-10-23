package kernel

import "testing"

func TestInputComplete(t *testing.T) {
	complete := []string{"", "fmt.Println(1)", "x := 1", "}", `"unterminated`, "for i := range 3 { fmt.Println(i) }", "%env A 1", "!echo hi", "%%bash\necho hi\n\n", "//gonb:%goflags", "!echo a \\\nb", "x++", "x--;"}
	incomplete := []string{"func f() {", "x := []int{1,\n2,", "s := `raw", "/* note", "a +", "x.", "f(a,", "x :=", "if x {\n} else", "for i := 0; i < 3; i++ {", `!echo a \`, "!echo a \\\n", "%%bash\necho hi"}
	for _, src := range complete {
		if !InputComplete(src) {
			t.Errorf("incomplete: %q", src)
		}
	}
	for _, src := range incomplete {
		if InputComplete(src) {
			t.Errorf("complete: %q", src)
		}
	}
}

func TestInputCompleteMatchesParse(t *testing.T) {
	for _, src := range []string{"fmt.Println(1)", "x := 1", "for i := range 3 { fmt.Println(i) }", "%env A 1", "!echo hi", "%%bash\necho hi\n\n", "//gonb:%goflags"} {
		if !InputComplete(src) {
			t.Fatalf("incomplete %q", src)
		}
		if _, err := parseCell("cell", "In[1]", src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}
