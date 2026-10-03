package kernel

import (
	"strings"
	"testing"
)

func TestFormatCell(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"empty", "", ""},
		{"whitespace", " \n\t", " \n\t"},
		{"statements", "x:=1\nfmt.Println( x+2 )\nx+3", "x := 1\nfmt.Println(x + 2)\nx + 3\n"},
		{"declarations", "import (\"strings\";\"fmt\")\ntype T struct{ X int }\nfunc(f T) M()int{return f.X}", "import (\n\t\"fmt\"\n\t\"strings\"\n)\n\ntype T struct{ X int }\n\nfunc (f T) M() int { return f.X }\n"},
		{"mixed", "// intro\nvar x=1 // x\nx++ // increment\n// f docs\nfunc f()int{return x}\nf() // result\n// end", "// intro\nvar x = 1 // x\nx++ // increment\n// f docs\nfunc f() int { return x }\nf() // result\n// end\n"},
		{"same line", "var x=1; x++; var y=2; x+y", "var x = 1\nx++\nvar y = 2\nx + y\n"},
		{"trailing comments", "var x=1; /* keep */ x++", "var x = 1 /* keep */\nx++\n"},
		{"controls", "for i:=0;i<3;i++{fmt.Println(i)}\nif x:=1;x>0{fmt.Println(x)}else{fmt.Println(0)}", "for i := 0; i < 3; i++ {\n\tfmt.Println(i)\n}\nif x := 1; x > 0 {\n\tfmt.Println(x)\n} else {\n\tfmt.Println(0)\n}\n"},
		{"function literal", "f:=func(x int)int{return x+1}\nf(2)", "f := func(x int) int { return x + 1 }\nf(2)\n"},
		{"comments only", "// keep\n/* %not magic */", "// keep\n/* %not magic */\n"},
		{"commands", "  %args   a b\nvar x=1\n!*echo hi\nx++\n//gonb:%main arg\nx", "  %args   a b\nvar x = 1\n!*echo hi\nx++\n//gonb:%main arg\nx\n"},
		{"block command", "if true{\n  !echo hi\nfmt.Println( 1 )\n}", "if true {\n  !echo hi\n\tfmt.Println(1)\n}\n"},
		{"shell continuation", "!echo \\\n%literal \\\nnot Go at all\nx:=1", "!echo \\\n%literal \\\nnot Go at all\nx := 1\n"},
		{"commands only", "  !echo hi\n%help", "  !echo hi\n%help"},
		{"raw string", "s:=`start\n!not shell\n%%bash\n//gonb:%help\nend`\ns", "s := `start\n!not shell\n%%bash\n//gonb:%help\nend`\ns\n"},
		{"block comment", "/*\n!not shell\n%%bash\n//gonb:%help\n*/\nx:=1", "/*\n!not shell\n%%bash\n//gonb:%help\n*/\nx := 1\n"},
		{"quoted delimiters", "s:=\"\\\"/*`\" // ` /*\nx:= '\\''\nx", "s := \"\\\"/*`\" // ` /*\nx := '\\''\nx\n"},
		{"marker collision", "// gopyter_format_command_0\n!echo hi\nx:=1", "// gopyter_format_command_0\n!echo hi\nx := 1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FormatCell(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tt.want)
			}
			again, err := FormatCell(got)
			if err != nil || again != got {
				t.Fatalf("not idempotent: %q, %v", again, err)
			}
		})
	}
}

func TestFormatCellNonGoMagics(t *testing.T) {
	for _, magic := range []string{"bash", "sh", "script", "writefile"} {
		for _, prefix := range []string{"%%", "//gonb:%%"} {
			src := "\n  " + prefix + magic + " args\nthis is not Go {\n!shell\n%magic"
			got, err := FormatCell(src)
			if err != nil || got != src {
				t.Fatalf("magic %s: got %q, %v", magic, got, err)
			}
		}
	}
}

func TestFormatCellAtomicErrors(t *testing.T) {
	for _, src := range []string{
		"x:=1\nfunc broken( {", "var x=1\nx :=", "x:=1\n/* unterminated",
		"x:=`unterminated\n!echo hi", "if true {", "x := \"unterminated\n%help",
		"var x = 1\n!echo hi\nvar y =", "x:=1\n@", "var x = )",
	} {
		got, err := FormatCell(src)
		if err == nil || got != src {
			t.Errorf("source %q: got %q, %v; want original and error", src, got, err)
		}
	}
}

func TestFormatCellPreservesCommentOrder(t *testing.T) {
	src := "// first\nvar x=1 // second\n/* third */\nx++ // fourth\n// fifth\ntype T int\n// sixth\nx\n// seventh\n"
	got, err := FormatCell(src)
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for _, comment := range []string{"first", "second", "third", "fourth", "fifth", "sixth", "seventh"} {
		pos := strings.Index(got, comment)
		if pos <= last || strings.Count(got, comment) != 1 {
			t.Fatalf("lost or reordered %q in %q", comment, got)
		}
		last = pos
	}
}
