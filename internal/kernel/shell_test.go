package kernel

import (
	"context"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newKernel(t *testing.T) *Kernel {
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

// streams runs src, feeding it stdin, and returns what the command wrote
// to each output stream.
func streams(t *testing.T, k *Kernel, id, src, stdin string) (out, errOut string) {
	t.Helper()
	var o, e strings.Builder
	err := k.ExecuteInput(context.Background(), id, "In["+id+"]", src,
		Input{Stdin: strings.NewReader(stdin)}, func(ev Event) {
			switch ev.Kind {
			case Stdout:
				o.WriteString(ev.Text)
			case Stderr:
				e.WriteString(ev.Text)
			}
		})
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return o.String(), e.String()
}

func TestShellTerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pseudo-terminal on Windows: shell output stays on pipes")
	}
	k := newKernel(t)
	k.SetTerminal(78)

	// The command gets a terminal, so it colorizes and redraws.
	out, _ := streams(t, k, "1", `![ -t 1 ] && echo tty || echo pipe`, "")
	if out != "tty\n" {
		t.Fatalf("the command doesn't see a terminal: %q", out)
	}
	// Colors are asked for too, for the programs that only read the
	// environment.
	out, _ = streams(t, k, "2", `!echo "$FORCE_COLOR $CLICOLOR_FORCE"`, "")
	if out != "1 1\n" {
		t.Fatalf("no color requested: %q", out)
	}
	out, _ = streams(t, k, "3", `!echo "[$COLORTERM][$TERM]"`, "")
	if out == "[][]\n" {
		t.Fatal("no color terminal type for the command")
	}
	// The terminal ends its lines with "\r\n"; that must not reach the
	// output, which is saved with the notebook.
	out, _ = streams(t, k, "4", `!printf 'a\nb\n'`, "")
	if out != "a\nb\n" {
		t.Fatalf("line endings: %q", out)
	}
	// Both streams are the terminal's one stream, as in a terminal.
	out, errOut := streams(t, k, "5", "!sh -c 'echo out; echo err >&2'", "")
	if out != "out\nerr\n" || errOut != "" {
		t.Fatalf("streams: %q %q", out, errOut)
	}
	// Standard input is still the pipe the front end feeds.
	out, _ = streams(t, k, "6", "!read line; echo \"got $line\"", "typed\n")
	if out != "got typed\n" {
		t.Fatalf("stdin: %q", out)
	}
	// A redraw in place, as a progress bar writes it.
	out, _ = streams(t, k, "7", `!printf '10%%\r90%%\n'`, "")
	if out != "10%\r90%\n" {
		t.Fatalf("redraw: %q", out)
	}
}

func TestShellTerminalDrainsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pseudo-terminal on Windows")
	}
	// A slow consumer leaves output buffered in the terminal when the
	// shell exits. Waiting for the process must not discard those bytes.
	const lines = 10000
	cmd := exec.Command("sh", "-c", "i=0; while [ $i -lt 10000 ]; do echo buffered; i=$((i+1)); done")
	var out strings.Builder
	if err := runShellPTY(context.Background(), cmd, 78, func(ev Event) {
		time.Sleep(10 * time.Millisecond)
		out.WriteString(ev.Text)
	}); err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("buffered\n", lines); out.String() != want {
		t.Fatalf("received %d bytes, want %d", out.Len(), len(want))
	}
}

func TestShellTerminalWidth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pseudo-terminal on Windows")
	}
	if _, err := exec.LookPath("tput"); err != nil {
		t.Skip("tput is not installed")
	}
	k := newKernel(t)
	k.SetTerminal(78)
	out, _ := streams(t, k, "1", "!tput cols", "")
	if out != "78\n" {
		t.Fatalf("terminal width: %q", out)
	}
	// A width too small to be one turns the terminal off.
	k.SetTerminal(ptyMinWidth - 1)
	if k.terminalWidth() != 0 {
		t.Fatal("a narrow width should turn the terminal off")
	}
}

func TestShellWithoutTerminal(t *testing.T) {
	k := newKernel(t) // no SetTerminal: what `gopyter run` gets
	out, _ := streams(t, k, "1", `![ -t 1 ] && echo tty || echo pipe`, "")
	if out != "pipe\n" {
		t.Fatalf("expected pipes: %q", out)
	}
	// Each stream is its own, so the front end can tell them apart.
	out, errOut := streams(t, k, "2", "!sh -c 'echo out; echo err >&2'", "")
	if out != "out\n" || errOut != "err\n" {
		t.Fatalf("streams: %q %q", out, errOut)
	}
	// Without a terminal nothing asks for colors.
	out, _ = streams(t, k, "3", `!echo "[$FORCE_COLOR]"`, "")
	if out != "[]\n" {
		t.Fatalf("colors forced: %q", out)
	}
}

func TestShellTerminalKeepsUserColor(t *testing.T) {
	k := newKernel(t)
	k.SetTerminal(78)
	out, _ := streams(t, k, "1", "%env NO_COLOR=1\n!echo \"[$FORCE_COLOR]\"", "")
	if out != "[]\n" {
		t.Fatalf("colors forced despite NO_COLOR: %q", out)
	}
}

func TestShellBackgroundJob(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pseudo-terminal on Windows")
	}
	k := newKernel(t)
	k.SetTerminal(78)
	// A background job keeps the terminal open, so the cell waits for it,
	// as it did when the command's output came through pipes.
	out, _ := streams(t, k, "1", `!sleep 1 & echo started`, "")
	if out != "started\n" {
		t.Fatalf("output %q", out)
	}
}

func TestPumpTerminal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		in    string
		chunk int
		want  string
	}{
		{"lines", "a\r\nb\r\n", 32, "a\nb\n"},
		{"split pair", "a\r\nb\r\n", 1, "a\nb\n"},
		{"lone carriage return", "a\rb\n", 2, "a\rb\n"},
		{"trailing carriage return", "x\r", 32, "x\r"},
		{"pair only", "\r\n", 1, "\n"},
	} {
		var b strings.Builder
		pumpTerminal(&chunkedReader{s: tc.in, n: tc.chunk}, Stdout, func(e Event) { b.WriteString(e.Text) })
		if got := b.String(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// chunkedReader hands out its text in pieces of at most n bytes, so the
// handling of a sequence split across two reads is exercised.
type chunkedReader struct {
	s string
	n int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	n := min(min(len(p), r.n), len(r.s))
	copy(p, r.s[:n])
	r.s = r.s[n:]
	return n, nil
}
