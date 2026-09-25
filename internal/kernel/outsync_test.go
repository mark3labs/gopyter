package kernel

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

// chunkReader returns s in reads of at most n bytes.
type chunkReader struct {
	s string
	n int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	n := copy(p[:min(len(p), r.n)], r.s)
	r.s = r.s[n:]
	return n, nil
}

func TestSplitMarkers(t *testing.T) {
	marker := func(n int) string { return fmt.Sprintf("%s%d%s", syncPrefix, n, syncSuffix) }
	notMarker := syncPrefix + "x" + syncSuffix
	in := "a" + marker(1) + "b\n" + marker(2) + marker(3) + "\x1b[31mred\x1b[0m" + notMarker + "end\x1b"
	want := "a<1>b\n<2><3>\x1b[31mred\x1b[0m" + notMarker + "end\x1b"
	// Every chunking, down to one byte per read, gives the same result.
	for size := 1; size <= len(in); size++ {
		var got strings.Builder
		r := &chunkReader{s: in, n: size}
		buf := make([]byte, 64)
		var pending []byte
		for {
			n, err := r.Read(buf)
			pending = append(pending, buf[:n]...)
			pending = splitMarkers(pending,
				func(b []byte) { got.Write(b) },
				func(n uint64) { fmt.Fprintf(&got, "<%d>", n) })
			if err != nil {
				got.Write(pending)
				break
			}
		}
		if got.String() != want {
			t.Fatalf("size %d: got %q, want %q", size, got.String(), want)
		}
	}
}

func TestSyncPump(t *testing.T) {
	s := newOutSync()
	s.closeRich() // no messages will come: markers must not block
	var got strings.Builder
	in := "a" + syncPrefix + "1" + syncSuffix + "b"
	syncPump(&chunkReader{s: in, n: 3}, s, func(e Event) {
		if e.Kind != Stdout {
			t.Errorf("kind %v", e.Kind)
		}
		got.WriteString(e.Text)
	})
	if got.String() != "ab" || s.stdout != 1 || !s.stdoutDone {
		t.Fatalf("%q %+v", got.String(), s)
	}
}

// Regression test: stdout and rich output (separate pipes) used to be
// emitted in whichever order the reads happened.
func TestOutputOrder(t *testing.T) {
	k := newTestKernel(t)
	const n = 50
	src := fmt.Sprintf("for i := range %d {\n\tfmt.Println(\"out\", i)\n\tnb.Display(i)\n\tfmt.Print(\"x\")\n\tnb.DisplayMarkdown(\"md\")\n\tfmt.Println()\n}\n\"last\"", n)
	var want strings.Builder
	for i := range n {
		fmt.Fprintf(&want, "out %d\n=> %d\nxmd\n", i, i)
	}
	want.WriteString("=> last\n")
	// Several runs, since a wrong order only showed up sometimes.
	for i := range 3 {
		out, err := run(t, k, fmt.Sprint(i+1), src)
		if err != nil || out != want.String() {
			t.Fatalf("run %d: %q %v", i, out, err)
		}
	}
}

// A program that closed its stdout can't write markers: its messages must
// still arrive, and nothing may hang.
func TestOutputOrderWithoutStdout(t *testing.T) {
	k := newTestKernel(t)
	out, err := run(t, k, "1", "fmt.Println(\"a\")\nos.Stdout.Close()\nnb.Display(1)\nnb.Display(2)")
	if err != nil || out != "a\n=> 1\n=> 2\n" {
		t.Fatalf("%q %v", out, err)
	}
}
