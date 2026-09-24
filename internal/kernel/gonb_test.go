package kernel

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	for in, want := range map[string][]string{
		`a b  c`:            {"a", "b", "c"},
		`-x "a b" 'c d'e`:   {"-x", "a b", "c de"},
		`""`:                {""},
		`-ldflags="-X a=b"`: {"-ldflags=-X a=b"},
		``:                  nil,
	} {
		got, err := splitArgs(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("splitArgs(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := splitArgs(`"open`); err == nil {
		t.Error("expected an error for an unterminated quote")
	}
}

func TestGoNBMain(t *testing.T) {
	k := newTestKernel(t)
	out, err := run(t, k, "1", "var n = flag.Int(\"n\", 1, \"count\")")
	if err != nil {
		t.Fatalf("decl: %q %v", out, err)
	}
	// %% parses flags with its arguments, for this cell only.
	out, err = run(t, k, "2", "%% -n=3 extra\nfmt.Println(*n, flag.Args())")
	if err != nil || out != "3 [extra]\n" {
		t.Fatalf("%%%%: %q %v", out, err)
	}
	out, err = run(t, k, "3", "//gonb:%main\nfmt.Println(*n, flag.Args())")
	if err != nil || out != "1 []\n" {
		t.Fatalf("//gonb:%%main: %q %v", out, err)
	}
	out, err = run(t, k, "4", "%exec hello -n=5 world\nfunc hello() { fmt.Println(\"hello\", *n, flag.Arg(0)) }")
	if err != nil || out != "hello 5 world\n" {
		t.Fatalf("%%exec: %q %v", out, err)
	}
	out, err = run(t, k, "5", "%exec nosuch")
	if err == nil || !strings.Contains(out, "In[5]:1") {
		t.Fatalf("%%exec of an unknown function: %q %v", out, err)
	}
	out, err = run(t, k, "6", "%exec hello\nfmt.Println(1)")
	if err == nil || !strings.Contains(out, "only have declarations") {
		t.Fatalf("%%exec with statements: %q %v", out, err)
	}
}

func TestGoNBInitAndShell(t *testing.T) {
	k := newTestKernel(t)
	// init_xxx functions run like init(); errors keep their columns.
	out, err := run(t, k, "1", "func init_a() { fmt.Println(\"a\") }\nfunc init_b() { fmt.Println(\"b\") }\nfmt.Println(\"main\")")
	if err != nil || out != "a\nb\nmain\n" {
		t.Fatalf("init_: %q %v", out, err)
	}
	out, err = run(t, k, "2", "func init_c() { undefined() }")
	if err == nil || !strings.Contains(out, "In[2]:1:17") {
		t.Fatalf("init_ error position: %q %v", out, err)
	}
	// !* runs in the workspace too; a trailing backslash continues.
	out, err = run(t, k, "3", "!*echo one\n!echo two \\\nthree")
	if err != nil || out != "one\ntwo three\n" {
		t.Fatalf("shell: %q %v", out, err)
	}
	out, err = run(t, k, "4", "%%nope\nx")
	if err == nil || !strings.Contains(out, "unknown cell magic %%nope") {
		t.Fatalf("unknown cell magic: %q %v", out, err)
	}
}

func TestCellMagics(t *testing.T) {
	k := newTestKernel(t)
	k.RunDir = t.TempDir()
	out, err := run(t, k, "1", "%%writefile data/in.txt\nhello\nworld\n")
	if err != nil || !strings.Contains(out, "wrote 12 bytes") {
		t.Fatalf("writefile: %q %v", out, err)
	}
	if _, err := run(t, k, "2", "%%writefile -a data/in.txt\n!\n"); err != nil {
		t.Fatal(err)
	}
	// Programs run in RunDir, so they find the file.
	out, err = run(t, k, "3", "b, _ := os.ReadFile(\"data/in.txt\")\nfmt.Print(string(b))")
	if err != nil || out != "hello\nworld\n!\n" {
		t.Fatalf("read back: %q %v", out, err)
	}
	out, err = run(t, k, "4", "\n%%bash\nx=$(wc -l < data/in.txt)\necho lines $x")
	if err != nil || strings.TrimSpace(out) != "lines 3" {
		t.Fatalf("bash: %q %v", out, err)
	}
	out, err = run(t, k, "5", "%%script sh -s arg\necho \"$1\" from script")
	if err != nil || out != "arg from script\n" {
		t.Fatalf("script: %q %v", out, err)
	}
	out, err = run(t, k, "6", "%%sh\nexit 3")
	if err == nil {
		t.Fatalf("a failing script should fail the cell: %q", out)
	}
}

func TestTestMagic(t *testing.T) {
	k := newTestKernel(t)
	if _, err := run(t, k, "1", "func add(a, b int) int { return a + b }"); err != nil {
		t.Fatal(err)
	}
	src := "%test\nfunc TestAdd(t *testing.T) {\n\tif add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\nfunc BenchmarkAdd(b *testing.B) {\n\tfor b.Loop() {\n\t\tadd(1, 2)\n\t}\n}"
	out, err := run(t, k, "2", src)
	if err != nil || !strings.Contains(out, "--- PASS: TestAdd") || !strings.Contains(out, "BenchmarkAdd") {
		t.Fatalf("passing test: %q %v", out, err)
	}
	// Only the cell's own tests run by default.
	out, err = run(t, k, "3", "%test\nfunc TestFails(t *testing.T) { t.Error(\"boom\") }")
	if err == nil || !strings.Contains(out, "--- FAIL: TestFails") || strings.Contains(out, "TestAdd") {
		t.Fatalf("failing test: %q %v", out, err)
	}
	out, err = run(t, k, "4", "%test -test.run=TestAdd -test.v")
	if err != nil || !strings.Contains(out, "--- PASS: TestAdd") {
		t.Fatalf("explicit flags: %q %v", out, err)
	}
	out, err = run(t, k, "5", "%test\nfmt.Println(1)")
	if err == nil || !strings.Contains(out, "only have declarations") {
		t.Fatalf("%%test with statements: %q %v", out, err)
	}
	// Normal cells still build (main_test.go is gone).
	out, err = run(t, k, "6", "add(2, 2)")
	if err != nil || out != "=> 4\n" {
		t.Fatalf("after %%test: %q %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(k.Dir, "main_test.go")); !os.IsNotExist(err) {
		t.Fatalf("main_test.go left behind: %v", err)
	}
}

func TestGoflagsAutogetVersion(t *testing.T) {
	k := newTestKernel(t)
	out, err := run(t, k, "1", "%goflags \"-ldflags=-X main.who=flags\"\nvar who = \"default\"\nfmt.Println(who)")
	if err != nil || !strings.Contains(out, "go build flags: -ldflags=-X main.who=flags") || !strings.HasSuffix(out, "flags\n") {
		t.Fatalf("goflags: %q %v", out, err)
	}
	out, err = run(t, k, "2", "%goflags \"\"\nfmt.Println(who)")
	if err != nil || !strings.Contains(out, "(none)") || !strings.HasSuffix(out, "default\n") {
		t.Fatalf("goflags reset: %q %v", out, err)
	}
	// With %noautoget, missing modules aren't fetched (no network here).
	out, err = run(t, k, "3", "%noautoget\nimport \"example.com/nosuch/pkg\"\npkg.X()")
	if err == nil || strings.Contains(out, "go get example.com") {
		t.Fatalf("noautoget: %q %v", out, err)
	}
	out, err = run(t, k, "4", "%version")
	if err != nil || !strings.HasPrefix(out, "gopyter "+Version+", go") {
		t.Fatalf("version: %q %v", out, err)
	}
}

func TestCaptureAndResetGoMod(t *testing.T) {
	k := newTestKernel(t)
	k.RunDir = t.TempDir()
	if _, err := run(t, k, "1", "%capture out.txt\nfmt.Println(\"hi\")\n1+1"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, k, "2", "%capture -a out.txt\nfmt.Println(\"again\")"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(k.RunDir, "out.txt"))
	if err != nil || string(b) != "hi\n2\nagain\n" {
		t.Fatalf("capture: %q %v", b, err)
	}

	if err := os.WriteFile(filepath.Join(k.Dir, "go.mod"), []byte("module x\n\nrequire example.com/y v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, k, "3", "%reset go.mod"); err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(k.Dir, "go.mod"))
	if err != nil || strings.Contains(string(mod), "example.com/y") || !strings.Contains(string(mod), "gopyter.local/kernel") {
		t.Fatalf("go.mod not reset: %q %v", mod, err)
	}
}

func TestDisplayIDAndStdin(t *testing.T) {
	k := newTestKernel(t)
	var events []Event
	src := "sc := bufio.NewScanner(os.Stdin)\nfor i := 0; sc.Scan(); i++ {\n\tDisplayID(\"line\", i, sc.Text())\n}\nDisplayMarkdownID(\"md\", \"**done**\")"
	err := k.ExecuteInput(context.Background(), "1", "In[1]", src, strings.NewReader("a\nb\n"), func(e Event) {
		if e.Kind != Info { // notes about sc not being kept
			events = append(events, e)
		}
	})
	if err != nil {
		t.Fatalf("%v %+v", err, events)
	}
	want := []Event{
		{Kind: Result, Text: "0, a", ID: "line"},
		{Kind: Result, Text: "1, b", ID: "line"},
		{Kind: Markdown, Text: "**done**", ID: "md"},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events %+v", events)
	}
}
