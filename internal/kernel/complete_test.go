package kernel

import (
	"strings"
	"testing"
)

func TestCompletionSourceSegments(t *testing.T) {
	k := newTestKernel(t)
	src := "import \"os\"\nfunc f() {\n println(1)\n}\na := 1; b := 2\n!ls\né := \"日本\""
	cs := k.CompletionSource("a", src, 0, 0, nil)
	lines, cell := strings.Split(cs.Content, "\n"), strings.Split(src, "\n")
	if len(cs.Segments) != 5 {
		t.Fatalf("%+v", cs.Segments)
	}
	for _, s := range cs.Segments {
		if s.Stmt != (s.CellLine >= 4) {
			t.Errorf("statement flag: %+v", s)
		}
		for n := range s.Lines {
			// Chunks sharing a line preserve their original columns, with spaces
			// replacing the preceding chunk and no text from the following chunk.
			mirror, original := lines[s.Line+n], cell[s.CellLine+n]
			start := len(mirror) - len(strings.TrimLeft(mirror, " "))
			if len(mirror) > len(original) || mirror[start:] != original[start:len(mirror)] {
				t.Fatalf("%q vs %q", mirror, original)
			}
		}
	}
	if got := k.CompletionSource("a", "%%bash\necho hi", 0, 0, nil); len(got.Segments) != 0 {
		t.Fatal(got)
	}
	simple := k.CompletionSource("a", "a := 1", 0, 0, nil)
	if simple.Content != "package main\n\n\nfunc main() {\na := 1\n}\n" {
		t.Fatal(simple.Content)
	}
}
