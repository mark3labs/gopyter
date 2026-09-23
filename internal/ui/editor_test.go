package ui

import "testing"

func TestEditorBasics(t *testing.T) {
	e := NewEditor("go", "")
	for _, r := range "func f() {" {
		e.InsertRune(r)
	}
	e.Newline()
	for _, r := range "return" {
		e.InsertRune(r)
	}
	e.Newline()
	e.InsertRune('}')
	if got, want := e.Value(), "func f() {\n\treturn\n}"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	// Enter between braces opens an indented line.
	e = NewEditor("go", "if x {}")
	e.SetCursor(0, 6)
	e.Newline()
	if got, want := e.Value(), "if x {\n\t\n}"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	// Undo restores the previous state.
	e.Undo()
	if got := e.Value(); got != "if x {}" {
		t.Fatalf("undo: got %q", got)
	}
	e.Redo()
	if got := e.Value(); got != "if x {\n\t\n}" {
		t.Fatalf("redo: got %q", got)
	}

	// Multi-line paste and backspace joining lines.
	e = NewEditor("go", "ab")
	e.SetCursor(0, 1)
	e.InsertText("1\n2")
	if got := e.Value(); got != "a1\n2b" {
		t.Fatalf("paste: got %q", got)
	}
	e.SetCursor(1, 0)
	e.Backspace()
	if got := e.Value(); got != "a12b" {
		t.Fatalf("join: got %q", got)
	}
	e.SetCursor(0, 4)
	e.DeleteWordBackward()
	if got := e.Value(); got != "" {
		t.Fatalf("delete word: got %q", got)
	}
}

func TestEditorRenderWrapAndClick(t *testing.T) {
	e := NewEditor("go", "abcdefghij\nx")
	e.SetCursor(0, 9)
	v := e.Render(3+6, true, newHighlighter("dracula"), newTheme())
	// gutter is 3 wide (1 digit + 2), text width 6 → first line wraps.
	if len(v.lines) != 3 {
		t.Fatalf("expected 3 visual lines, got %d", len(v.lines))
	}
	if v.curY != 1 || v.curX != 3+3 {
		t.Fatalf("cursor at %d,%d", v.curX, v.curY)
	}
	row, col := e.PositionAt(v, 3+1, 1)
	if row != 0 || col != 7 {
		t.Fatalf("click mapped to %d,%d", row, col)
	}
}

func TestEditorSelection(t *testing.T) {
	e := NewEditor("go", "hello world\nsecond line\nthird")

	e.SelectWordAt(0, 7)
	if got := e.SelectedText(); got != "world" {
		t.Fatalf("word: %q", got)
	}
	// Typing replaces the selection; one undo restores it.
	e.InsertRune('X')
	e.InsertRune('Y')
	if got := e.Value(); got != "hello XY\nsecond line\nthird" {
		t.Fatalf("replace: %q", got)
	}
	e.Undo()
	if got := e.Value(); got != "hello world\nsecond line\nthird" {
		t.Fatalf("undo replace: %q", got)
	}

	// Multi-line selection text and deletion.
	e.SelectRange(Pos{0, 6}, Pos{1, 6})
	if got := e.SelectedText(); got != "world\nsecond" {
		t.Fatalf("multi-line: %q", got)
	}
	e.Backspace()
	if got := e.Value(); got != "hello  line\nthird" {
		t.Fatalf("delete selection: %q", got)
	}

	// Line selection includes the newline; backwards selections work.
	e = NewEditor("go", "a\nb\nc")
	e.SelectLine(1)
	if got := e.SelectedText(); got != "b\n" {
		t.Fatalf("line: %q", got)
	}
	e.SelectRange(Pos{2, 1}, Pos{0, 0})
	if got := e.SelectedText(); got != "a\nb\nc" {
		t.Fatalf("backwards: %q", got)
	}

	// Block indent / dedent.
	e.IndentSelection(false)
	if got := e.Value(); got != "\ta\n\tb\n\tc" {
		t.Fatalf("indent: %q", got)
	}
	e.IndentSelection(true)
	if got := e.Value(); got != "a\nb\nc" {
		t.Fatalf("dedent: %q", got)
	}

	// Multi-line paste over a selection.
	e.SelectAll()
	e.InsertText("x\ny")
	if got := e.Value(); got != "x\ny" || e.HasSelection() {
		t.Fatalf("paste over selection: %q", got)
	}
}

func TestInCodeContext(t *testing.T) {
	for src, want := range map[string]bool{
		`fmt.Pr`:               true,
		`x := "Pr`:             false,
		`x := "done" + fm`:     true,
		`x := "esc \" still`:   false,
		`x := "ends\\" + fm`:   true,
		"x := `raw\nstill":     false,
		"x := `raw` + fm":      true,
		`c := 'a`:              false,
		`// comment fm`:        false,
		"// done\nfm":          true,
		`/* open fm`:           false,
		`/* closed */ fm`:      true,
		"s := strings.ToU":     true,
		"if x { // note\n\tfm": true,
	} {
		e := NewEditor("go", src)
		e.CursorEnd()
		if got := e.InCodeContext(); got != want {
			t.Errorf("%q: got %v want %v", src, got, want)
		}
	}
}
