package kernel

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func check(t *testing.T, k *Kernel, id, src string) CheckResult {
	t.Helper()
	r, err := k.Check(context.Background(), id, "In["+id+"]", src)
	if err != nil {
		t.Fatalf("Check(%q): %v", src, err)
	}
	return r
}

func TestCheckMatchesExecute(t *testing.T) {
	k := newTestKernel(t)
	if _, err := run(t, k, "seed", "var seed = 0"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		id, src string
		wantErr bool
	}{
		{"1", "x := 1", false},
		{"2", "x + 1", false},
		{"3", "func f() {}", false},
		{"4", "f()", false},
		{"5", "n := 2\nfunc() { n++ }()\nn", false},
		{"6", "y := undefined", true},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			assertUnchanged := snapshotCheckState(t, k)
			r := check(t, k, c.id, c.src)
			assertUnchanged()
			if len(r.Missing) > 0 {
				t.Fatalf("unexpected missing modules: %v", r.Missing)
			}
			out, err := run(t, k, c.id, c.src)
			if (r.Errors == "") != (err == nil) || (err != nil) != c.wantErr {
				t.Fatalf("Check: %+v; Execute: %q %v; wantErr: %v", r, out, err, c.wantErr)
			}
			if c.wantErr {
				position := "In[" + c.id + "]:1:"
				if !strings.Contains(r.Errors, position) || !strings.Contains(out, position) {
					t.Errorf("want position %q in Check %q and Execute %q", position, r.Errors, out)
				}
			}
		})
	}
}

func TestCheckTestCell(t *testing.T) {
	k := newTestKernel(t)
	if _, err := run(t, k, "1", "x := 1"); err != nil {
		t.Fatal(err)
	}
	assertUnchanged := snapshotCheckState(t, k)
	// Check uses go build on main.go even for %test: it compiles the
	// declaration but does not build or run a test binary.
	if r := check(t, k, "2", "%test\nfunc TestA(t *testing.T) {}"); !r.OK() {
		t.Fatalf("check test cell: %+v", r)
	}
	assertUnchanged()
	if _, err := os.Stat(filepath.Join(k.Dir, "main_test.go")); !os.IsNotExist(err) {
		t.Fatalf("Check created main_test.go or stat failed: %v", err)
	}
}

// snapshotCheckState guards both persisted declarations and Execute's program.
func snapshotCheckState(t *testing.T, k *Kernel) func() {
	t.Helper()
	decls := k.Declarations()
	file := filepath.Join(k.Dir, "main.go")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if got := k.Declarations(); !slices.Equal(got, decls) {
			t.Errorf("Check changed declarations: %v, want %v", got, decls)
		}
		after, err := os.ReadFile(file)
		if err != nil || string(after) != string(before) {
			t.Errorf("Check changed main.go content (%v)", err)
		}
		afterInfo, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if !afterInfo.ModTime().Equal(info.ModTime()) {
			t.Error("Check changed main.go mtime")
		}
	}
}

func TestCheck(t *testing.T) {
	k := newTestKernel(t)
	if _, err := run(t, k, "1", "type P struct{ X, Y int }\nn := 41"); err != nil {
		t.Fatal(err)
	}
	before := k.Declarations()

	cases := []struct {
		name, id, src string
		wantErr       string // substring of Errors; empty means the cell compiles
	}{
		{"uses earlier cells", "2", "p := P{1, 2}\np.X + n", ""},
		{"compile error in cell coordinates", "2", "x := 1\nundefinedThing(x)", "In[2]:2"},
		// Hoisting keeps a top-level := used nowhere from being an error,
		// as it is when the cell runs.
		{"unused define is fine", "2", "unused := strings.ToUpper(\"a\")", ""},
		{"void trailing call", "2", "fmt.Println(n)", ""},
		{"own main", "2", "func main() { fmt.Println(P{}) }", ""},
		{"syntax error", "2", "if {", "In[2]"},
		{"commands only", "2", "!echo not run", ""},
		// Re-checking the cell that defined n replaces it, like re-running.
		{"redefines own cell", "1", "type P struct{ X int }\nn := \"now a string\"\nn + \"!\"", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := check(t, k, c.id, c.src)
			if len(r.Missing) > 0 {
				t.Fatalf("unexpected missing modules %v", r.Missing)
			}
			if c.wantErr == "" && r.Errors != "" {
				t.Fatalf("want OK, got errors:\n%s", r.Errors)
			}
			if c.wantErr != "" && !strings.Contains(r.Errors, c.wantErr) {
				t.Fatalf("want errors containing %q, got:\n%s", c.wantErr, r.Errors)
			}
		})
	}

	// Checking never commits declarations, runs code or touches the
	// workspace's own program.
	if got := k.Declarations(); !slices.Equal(got, before) {
		t.Errorf("declarations changed by Check: %v, want %v", got, before)
	}
	if out, err := run(t, k, "3", "fmt.Println(n + 1)"); err != nil || out != "42\n" {
		t.Errorf("kernel state disturbed by Check: %q %v", out, err)
	}
}

// TestCheckIgnoresCommands makes sure a check never executes shell lines.
func TestCheckIgnoresCommands(t *testing.T) {
	k := newTestKernel(t)
	marker := filepath.Join(t.TempDir(), "ran")
	if r := check(t, k, "1", "!touch "+marker+"\nfmt.Println(1)"); !r.OK() {
		t.Fatalf("check failed: %+v", r)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("Check ran a !shell command")
	}
}

// TestCheckMissingModule: modules are only fetched by Execute, so Check
// reports them instead of a compile error, offline and without editing
// go.mod.
func TestCheckMissingModule(t *testing.T) {
	k := newTestKernel(t)
	goMod := filepath.Join(k.Dir, "go.mod")
	before, err := os.ReadFile(goMod)
	if err != nil {
		t.Fatal(err)
	}
	r := check(t, k, "1", "import \"example.com/gopyter/notreal\"\nnotreal.X()")
	if r.Errors != "" || !slices.Equal(r.Missing, []string{"example.com/gopyter/notreal"}) {
		t.Fatalf("got %+v", r)
	}
	if r.OK() {
		t.Error("OK() with missing modules")
	}
	if after, err := os.ReadFile(goMod); err != nil || string(after) != string(before) {
		t.Errorf("Check changed go.mod (%v):\n%s", err, after)
	}
}

// TestCheckWhileExecuting runs a check while a cell is executing: they
// build different packages, so neither may see the other's code.
func TestCheckWhileExecuting(t *testing.T) {
	k := newTestKernel(t)
	started := make(chan struct{})
	done := make(chan string)
	go func() {
		var out strings.Builder
		err := k.Execute(context.Background(), "1", "In[1]", "fmt.Println(\"start\")\ntime.Sleep(time.Second)\nfmt.Println(\"end\")", func(e Event) {
			if e.Text == "start\n" {
				close(started)
			}
			out.WriteString(e.Text)
		})
		if err != nil {
			out.WriteString(err.Error())
		}
		done <- out.String()
	}()
	<-started
	if r := check(t, k, "2", "s := \"checked\"\nfmt.Println(s)"); !r.OK() {
		t.Errorf("check during execution: %+v", r)
	}
	if out := <-done; out != "start\nend\n" {
		t.Errorf("execution disturbed by Check: %q", out)
	}
}
