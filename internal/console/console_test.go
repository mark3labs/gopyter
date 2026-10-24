package console

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/complete"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func testKernel(t *testing.T) *kernel.Kernel {
	t.Helper()
	k, err := kernel.New("")
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
func testModel(t *testing.T) (*model, *[]string) {
	t.Helper()
	h, err := loadHistory(filepath.Join(t.TempDir(), "history"))
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(t.Context(), Options{Kernel: testKernel(t)}, &session{}, h)
	lines := []string{}
	m.print = func(s string) tea.Cmd { return func() tea.Msg { lines = append(lines, ansi.Strip(s)); return nil } }
	t.Cleanup(m.stop)
	return m, &lines
}
func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "alt+enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	return tea.KeyPressMsg{Code: rune(s[len(s)-1]), Mod: tea.ModCtrl}
}

// Commands are sequences/batches in production; tests unwrap them to verify
// printing and run events in the same deterministic order without a terminal.
func pumpCmd(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			pumpCmd(m, c)
		}
		return
	}
	// Bubble Tea's sequence message is an unexported []tea.Cmd type.
	v := reflect.ValueOf(msg)
	if v.IsValid() && v.Kind() == reflect.Slice {
		for i := 0; i < v.Len(); i++ {
			if c, ok := v.Index(i).Interface().(tea.Cmd); ok {
				pumpCmd(m, c)
			}
		}
		return
	}
	switch msg.(type) {
	case runMsg, completionMsg:
		_, next := m.Update(msg)
		pumpCmd(m, next)
	}
}
func runInput(t *testing.T, m *model, src string) {
	t.Helper()
	m.ta.SetValue(src)
	pumpCmd(m, m.key(key("ctrl+r")))
	if m.run != nil {
		t.Fatal("still running")
	}
}
func TestEnterRunsCompleteInput(t *testing.T) {
	m, lines := testModel(t)
	m.ta.SetValue("x := 21")
	pumpCmd(m, m.key(key("enter")))
	m.ta.SetValue("x*2")
	pumpCmd(m, m.key(key("enter")))
	got := strings.Join(*lines, "\n")
	if !strings.Contains(got, "In [2]: x*2\n=> 42") {
		t.Fatal(got)
	}
	if !strings.Contains(m.View().Content, "In [3]:") {
		t.Fatal(m.View().Content)
	}
}
func TestMultilineKeys(t *testing.T) {
	m, _ := testModel(t)
	m.ta.SetValue("func f() int {")
	m.key(key("enter"))
	if m.run != nil || m.ta.Value() != "func f() int {\n" {
		t.Fatal(m.ta.Value())
	}
	m.ta.InsertString("\treturn 1")
	m.key(key("enter"))
	if !strings.HasSuffix(m.ta.Value(), "\n    ") {
		t.Fatalf("indent: %q", m.ta.Value())
	}
	m.ta.InsertString("}")
	pumpCmd(m, m.key(key("enter")))
	if m.sess.count != 1 {
		t.Fatal(m.sess.count)
	}
	m.ta.SetValue("fmt.Println(1)")
	m.key(key("alt+enter"))
	if m.ta.Value() != "fmt.Println(1)\n" {
		t.Fatal(m.ta.Value())
	}
}
func TestCtrlRForcesRun(t *testing.T) {
	m, lines := testModel(t)
	runInput(t, m, "func f() {")
	if got := strings.Join(*lines, "\n"); !strings.Contains(got, "In[1]:") || !strings.Contains(got, "✗") {
		t.Fatal(got)
	}
}
func TestCtrlD(t *testing.T) {
	m, _ := testModel(t)
	if _, ok := m.key(key("ctrl+d"))().(tea.QuitMsg); !ok {
		t.Fatal("not quit")
	}
	m.ta.SetValue("abc")
	m.ta.MoveToBegin()
	m.key(key("ctrl+d"))
	if m.ta.Value() != "bc" {
		t.Fatal(m.ta.Value())
	}
	m.ta.SetValue("data, _ := io.ReadAll(os.Stdin)\nfmt.Println(len(data))")
	cmd := m.key(key("ctrl+r"))
	m.key(key("ctrl+d"))
	pumpCmd(m, cmd)
	if m.run != nil {
		t.Fatal("still running")
	}
}
func TestCtrlCInterrupts(t *testing.T) {
	m, lines := testModel(t)
	m.ta.SetValue("time.Sleep(time.Hour)")
	cmd := m.key(key("ctrl+r"))
	m.key(key("ctrl+c"))
	pumpCmd(m, cmd)
	if !strings.Contains(strings.Join(*lines, "\n"), "✗ interrupted") {
		t.Fatal(*lines)
	}
	m.ta.SetValue("draft")
	m.key(key("ctrl+c"))
	if m.ta.Value() != "" {
		t.Fatal("not cleared")
	}
}
func TestStdinLine(t *testing.T) {
	m, lines := testModel(t)
	m.ta.SetValue("fmt.Print(\"Name? \")\nname, _ := bufio.NewReader(os.Stdin).ReadString('\\n')\nfmt.Println(\"hi\", strings.TrimSpace(name))")
	cmd := m.key(key("ctrl+r"))
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(m.run.out.partial(), "Name? ") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(m.View().Content, "Name? ") {
		t.Fatal(m.View().Content)
	}
	m.in.SetValue("gopher")
	m.key(key("enter"))
	pumpCmd(m, cmd)
	if !strings.Contains(strings.Join(*lines, "\n"), "hi gopher") {
		t.Fatal(*lines)
	}
}
func TestHistory(t *testing.T) {
	m, _ := testModel(t)
	runInput(t, m, "a := 1")
	runInput(t, m, "b := 2\nb")
	m.ta.SetValue("draft")
	m.key(key("up"))
	if m.ta.Value() != "b := 2\nb" {
		t.Fatal(m.ta.Value())
	}
	m.key(key("up"))
	if m.ta.Value() != "a := 1" {
		t.Fatal(m.ta.Value())
	}
	m.key(key("down"))
	m.ta.MoveToEnd()
	m.key(key("down"))
	if m.ta.Value() != "draft" {
		t.Fatal(m.ta.Value())
	}
	h, err := loadHistory(m.hist.path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h.entries, m.hist.entries) {
		t.Fatal(h.entries)
	}
	m.ta.SetValue("one\ntwo")
	m.ta.MoveToEnd()
	m.key(key("up"))
	if m.ta.Value() != "one\ntwo" || m.ta.Line() != 0 {
		t.Fatal(m.ta.Value(), m.ta.Line())
	}
}
func TestHistoryCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	var b bytes.Buffer
	for i := range 1005 {
		if err := json.NewEncoder(&b).Encode(fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := loadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.entries) != 1000 || h.entries[0] != "5" {
		t.Fatal(len(h.entries), h.entries[0])
	}
	if err := h.add("1004"); err != nil {
		t.Fatal(err)
	}
	if err := h.add("multi\nline"); err != nil {
		t.Fatal(err)
	}
	again, err := loadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.entries) != 1000 || again.entries[999] != "multi\nline" {
		t.Fatal(again.entries)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatal(info.Mode())
	}
}
func TestSave(t *testing.T) {
	m, lines := testModel(t)
	runInput(t, m, "fmt.Println(7)")
	runInput(t, m, "2*3")
	path := filepath.Join(t.TempDir(), "out.ipynb")
	runInput(t, m, "%save "+path)
	nb, err := notebook.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(nb.Cells) != 2 || nb.Cells[0].ExecutionCount != 1 || nb.Cells[1].ExecutionCount != 2 || nb.Cells[0].Outputs[0].Kind != notebook.Stdout || nb.Cells[1].Outputs[0].Kind != notebook.Result {
		t.Fatalf("%+v", nb.Cells)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	runInput(t, m, "%save "+path)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !strings.Contains((*lines)[len(*lines)-1], "✗") {
		t.Fatal(*lines)
	}
	runInput(t, m, "%save -f "+path)
	if m.sess.count != 2 || len(m.hist.entries) != 2 {
		t.Fatal("save recorded")
	}
}
func TestLineMode(t *testing.T) {
	k := testKernel(t)
	var out bytes.Buffer
	path := filepath.Join(t.TempDir(), "exit.ipynb")
	opts := Options{Kernel: k, SavePath: path}
	err := Run(t.Context(), opts, strings.NewReader("x := 2\nfunc f(n int) int {\nreturn n*3\n}\nf(x)\n"), &out)
	if err != nil {
		t.Fatal(err, out.String())
	}
	if strings.Count(out.String(), "In [") != 3 || !strings.Contains(out.String(), "=> 6") || strings.Contains(out.String(), "✓") {
		t.Fatal(out.String())
	}
	nb, err := notebook.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(nb.Cells) != 3 {
		t.Fatal(len(nb.Cells))
	}
	if err := Run(t.Context(), Options{Kernel: k}, strings.NewReader("undefinedName\n"), &out); err == nil {
		t.Fatal("failure not reported")
	}
}

type fakeCompleter struct{ items []complete.Item }

func (f fakeCompleter) Complete(context.Context, complete.Request) (complete.Result, error) {
	return complete.Result{Items: f.items, Replace: 2}, nil
}
func TestCompletionTab(t *testing.T) {
	m, _ := testModel(t)
	m.opts.Completer = fakeCompleter{[]complete.Item{{Label: "Println", Insert: "Println"}}}
	m.ta.SetValue("fmt.Pr")
	pumpCmd(m, m.key(key("tab")))
	if m.ta.Value() != "fmt.Println" {
		t.Fatal(m.ta.Value())
	}
	m.clearCompletion()
	m.opts.Completer = fakeCompleter{[]complete.Item{{Label: "Println", Insert: "Println"}, {Label: "Printf", Insert: "Printf"}}}
	m.ta.SetValue("fmt.Pr")
	pumpCmd(m, m.key(key("tab")))
	if !strings.Contains(m.View().Content, "Printf") {
		t.Fatal(m.View().Content)
	}
	m.key(key("tab"))
	if m.ta.Value() != "fmt.Printf" {
		t.Fatal(m.ta.Value())
	}
	m.clearCompletion()
	m.ta.SetValue("  ")
	m.key(key("tab"))
	if m.ta.Value() != "      " {
		t.Fatalf("%q", m.ta.Value())
	}
	// Editing invalidates an in-flight result, even if it arrives after submit.
	seq := m.comp.seq
	m.key(tea.KeyPressMsg{Code: 'x', Text: "x"})
	_, _ = m.Update(completionMsg{seq: seq, result: complete.Result{Items: []complete.Item{{Insert: "stale"}}}})
	if strings.Contains(m.ta.Value(), "stale") {
		t.Fatal(m.ta.Value())
	}
}
func TestLineWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := make(chan string, 10)
	w := &lineWriter{ctx: ctx, ch: ch}
	_, _ = w.Write([]byte("one\ntwo\rthree"))
	if got := <-ch; got != "one" {
		t.Fatal(got)
	}
	if w.partial() != "three" {
		t.Fatal(w.partial())
	}
	w.flush()
	if got := <-ch; got != "three" {
		t.Fatal(got)
	}
}

// TestLineWriterCarriageReturn is a regression test: "\r" cleared the line
// at once, so CRLF lines came out blank and progress lines never showed.
func TestLineWriterCarriageReturn(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := make(chan string, 10)
	w := &lineWriter{ctx: ctx, ch: ch}
	_, _ = w.Write([]byte("abc\r\ndef\r\n"))
	if a, b := <-ch, <-ch; a != "abc" || b != "def" {
		t.Fatalf("%q %q", a, b)
	}
	_, _ = w.Write([]byte("10%\r"))
	if w.partial() != "10%" {
		t.Fatal(w.partial())
	}
	_, _ = w.Write([]byte("100%\r"))
	if w.partial() != "100%" {
		t.Fatal(w.partial())
	}
	w.flush()
	if got := <-ch; got != "100%" {
		t.Fatal(got)
	}
	// Styled as the runner paints it on a terminal: the escapes after \r
	// print nothing, so they don't overwrite the line.
	_, _ = w.Write([]byte("\x1b[31mabc\r\x1b[m\n\x1b]8;;x\x1b\\p1\r\x1b[1mp2\r\x1b[m"))
	if got := <-ch; got != "\x1b[31mabc\x1b[m" {
		t.Fatalf("%q", got)
	}
	if got := w.partial(); got != "\x1b[1mp2\x1b[m" {
		t.Fatalf("%q", got)
	}
}

func TestCellMagicEnter(t *testing.T) {
	m, lines := testModel(t)
	m.ta.SetValue("%%bash\necho magic")
	m.key(key("enter"))
	if m.run != nil {
		t.Fatal("ran before blank line")
	}
	pumpCmd(m, m.key(key("enter")))
	if !strings.Contains(strings.Join(*lines, "\n"), "magic") {
		t.Fatal(*lines)
	}
}

func TestOutputQueueDoesNotBlockView(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	ch := make(chan string, 1)
	w := &lineWriter{ctx: ctx, ch: ch}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = w.Write([]byte("first\nsecond\n")) }()
	// Leave a writer blocked on a full queue, then read the live tail.
	deadline := time.Now().Add(time.Second)
	for len(ch) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	viewed := make(chan struct{})
	go func() { _ = w.partial(); close(viewed) }()
	select {
	case <-viewed:
	case <-time.After(time.Second):
		t.Fatal("view blocked")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("writer didn't cancel")
	}
}
