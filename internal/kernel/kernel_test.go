package kernel

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func run(t *testing.T, k *Kernel, id, src string) (string, error) {
	t.Helper()
	var b strings.Builder
	err := k.Execute(context.Background(), id, "In["+id+"]", src, func(e Event) {
		switch e.Kind {
		case Result:
			fmt.Fprintf(&b, "=> %s\n", e.Text)
		case Error:
			fmt.Fprintf(&b, "ERR %s\n", e.Text)
		default:
			b.WriteString(e.Text)
		}
	})
	return b.String(), err
}

func TestKernel(t *testing.T) {
	k, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Close(); err != nil {
			t.Error(err)
		}
	})

	out, err := run(t, k, "1", "type P struct{ X, Y int }\nfunc (p P) Sum() int { return p.X + p.Y }\nvar base = 10\nfmt.Println(\"hello\")")
	if err != nil || out != "hello\n" {
		t.Fatalf("cell1: %q %v", out, err)
	}
	out, err = run(t, k, "2", "p := P{1, 2}\nfor i := 0; i < 2; i++ {\n\tfmt.Println(i)\n}\np.Sum() + base")
	if err != nil || !strings.Contains(out, "=> 13") {
		t.Fatalf("cell2: %q %v", out, err)
	}
	out, err = run(t, k, "3", "x := 1\ny")
	if err == nil || !strings.Contains(out, "In[3]:2") {
		t.Fatalf("cell3: %q %v", out, err)
	}
	out, err = run(t, k, "4", "func main() {\n\tfmt.Println(strings.ToUpper(\"main\"))\n}")
	if err != nil || out != "MAIN\n" {
		t.Fatalf("cell4: %q %v", out, err)
	}
	// Redefine base in a new cell; P remains.
	out, err = run(t, k, "5", "const base = 1\nP{2, 3}.Sum() * base")
	if err != nil || !strings.Contains(out, "=> 5") {
		t.Fatalf("cell5: %q %v", out, err)
	}
	// Explicit semicolons in control headers and one-liners.
	out, err = run(t, k, "7", "for i := 0; i < 2; i++ { fmt.Print(i) }; if v := 3; v > 2 { fmt.Print(\"!\") }\nswitch x := 1; x {\ncase 1:\n\tfmt.Println(\"one\")\n}")
	if err != nil || out != "01!one\n" {
		t.Fatalf("cell7: %q %v", out, err)
	}
	// Trailing calls: displayed when they return values.
	for src, want := range map[string]string{
		`strings.ToUpper("go")`:  "=> GO\n",
		`strconv.Atoi("42")`:     "=> 42, <nil>\n",
		`fmt.Println("plain")`:   "plain\n",
		"func noop() {}\nnoop()": "",
		"(len(\"abc\"))":         "=> 3\n",
	} {
		out, err = run(t, k, "8", src)
		if err != nil || out != want {
			t.Fatalf("%s: %q %v", src, out, err)
		}
	}
	out, err = run(t, k, "6", "%ls\n!echo shell")
	if err != nil || !strings.Contains(out, "P.Sum") || !strings.Contains(out, "shell") {
		t.Fatalf("cell6: %q %v", out, err)
	}
}
