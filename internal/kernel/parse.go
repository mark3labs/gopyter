package kernel

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strings"
	"unicode"
)

// Decl is a package-level declaration that persists across cell executions.
type Decl struct {
	// Names are the identifiers declared (used to replace re-declarations).
	Names []string
	// Src is the declaration source, prefixed with a //line directive so
	// compiler errors point back to the originating cell.
	Src string
	// Raw is the declaration source without the //line directive.
	Raw string
	// CellID is the id of the cell that contributed the declaration.
	CellID string
	// Var is set for a variable hoisted from a top-level := statement. It
	// is declared in gopyter_vars.go rather than main.go.
	Var *VarInfo
}

// Import is an import spec that persists across cell executions.
type Import struct {
	Name   string
	Path   string
	CellID string
}

// Command is a shell command ("!cmd") or magic ("%cmd") found in a cell.
type Command struct {
	Line  int
	Magic bool
	Text  string
	// Cell is set for a cell magic ("%%writefile", "%%bash", ...), which
	// takes the rest of the cell as Input.
	Cell  bool
	Input string
}

// parsedCell is the result of splitting a cell into its components.
type parsedCell struct {
	decls    []*Decl
	imports  []*Import
	body     string // main() body statements
	plain    string // body without the trailing Display wrapper
	userMain string // user-provided func main, if any
	commands []Command
	hasCode  bool

	// GoNB-style run settings: %% / %main [args], %exec fn [args] and
	// %test [flags]. Their arguments replace %args for this cell only.
	args       []string
	hasArgs    bool
	parseFlags bool     // call flag.Parse() first thing in main
	exec       string   // %exec: the function main calls
	execLine   int      // line of the %exec magic, for errors
	test       bool     // %test: build with go test
	tests      []string // Test/Example/Fuzz functions declared by the cell
	benchmarks []string // Benchmark functions declared by the cell

	name    string    // name used in //line directives
	src     string    // cell source with commands blanked
	stmts   []chunk   // statement chunks, in order
	defines []*define // top-level := statements
}

// define is a top-level "x, y := ..." statement of a cell. Its variables
// can be hoisted to package level so that later cells see them.
type define struct {
	stmt   int        // index in parsedCell.stmts
	tok    int        // offset of ":=" in the cell source
	idents []defIdent // left-hand side identifiers, except "_"
	// fn is set for "f := func(...) {...}": the literal's range in the
	// cell source. Evaluating a literal has no side effects, so it can be
	// hoisted as a regular declaration instead of a saved value.
	fn *span
}

// defIdent is an identifier on the left of a define, at a cell position.
type defIdent struct {
	name      string
	line, col int
}

// span is a range of the cell source starting at line:col.
type span struct {
	start, end int
	line, col  int
}

// hoist tells how a define is emitted.
type hoist int

const (
	hoistNone hoist = iota // keep the local variables
	hoistVar               // assign package-level variables (":=" becomes " =")
	hoistFunc              // the statement became a declaration: drop it
)

type chunk struct {
	decl       bool
	start, end int
	line, col  int
}

type tokInfo struct {
	off int
	tok token.Token
	lit string
}

// splitChunks splits Go source into top-level declaration and statement
// chunks, using the Go scanner to track nesting depth.
func splitChunks(src string) []chunk {
	fset := token.NewFileSet()
	file := fset.AddFile("cell", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, []byte(src), func(token.Position, string) {}, 0)

	var toks []tokInfo
	for {
		pos, tok, lit := s.Scan()
		toks = append(toks, tokInfo{off: file.Offset(pos), tok: tok, lit: lit})
		if tok == token.EOF {
			break
		}
	}

	isDecl := func(i int) bool {
		switch toks[i].tok {
		case token.IMPORT, token.TYPE, token.CONST, token.VAR:
			return true
		case token.FUNC:
			if i+1 < len(toks) && toks[i+1].tok == token.IDENT {
				return true
			}
			if i+1 < len(toks) && toks[i+1].tok == token.LPAREN {
				// Method declaration: func (r T) Name( ... or Name[ ...
				depth := 0
				for j := i + 1; j < len(toks); j++ {
					switch toks[j].tok {
					case token.LPAREN:
						depth++
					case token.RPAREN:
						depth--
					}
					if depth == 0 {
						return j+2 < len(toks) && toks[j+1].tok == token.IDENT &&
							(toks[j+2].tok == token.LPAREN || toks[j+2].tok == token.LBRACK)
					}
				}
			}
		}
		return false
	}

	var chunks []chunk
	depth, start := 0, -1
	var decl, inHeader bool
	for i, t := range toks {
		if start < 0 {
			if t.tok == token.SEMICOLON || t.tok == token.EOF {
				continue
			}
			start, decl = i, isDecl(i)
		}
		switch t.tok {
		case token.FOR, token.IF, token.SWITCH, token.SELECT:
			// Explicit semicolons in control clause headers
			// (e.g. "for i := 0; i < n; i++ {") don't end the statement.
			if depth == 0 {
				inHeader = true
			}
		case token.LBRACE:
			if depth == 0 {
				inHeader = false
			}
		}
		switch t.tok {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth--
		}
		if t.tok == token.SEMICOLON && depth <= 0 && inHeader && t.lit == ";" {
			continue
		}
		if (t.tok == token.SEMICOLON && depth <= 0) || t.tok == token.EOF {
			inHeader = false
			end := t.off
			if t.tok == token.SEMICOLON && t.lit == ";" {
				end++
			}
			if end > len(src) {
				end = len(src)
			}
			p := file.Position(file.Pos(toks[start].off))
			chunks = append(chunks, chunk{decl: decl, start: toks[start].off, end: end, line: p.Line, col: p.Column})
			start, depth = -1, 0
		}
	}
	return chunks
}

func lineDirective(name string, line, col int) string {
	return fmt.Sprintf("//line %s:%d:%d\n", name, line, max(col, 1))
}

// cellMagics are the "%%name" cell magics: the rest of the cell is their
// input rather than Go code.
var cellMagics = map[string]bool{"writefile": true, "script": true, "bash": true, "sh": true}

// commandLine returns the command of a cell line ("!..." or "%..."),
// trimmed, or "" if the line is Go code. GoNB also accepts commands
// written as "//gonb:%...", which keeps files valid Go for editors.
func commandLine(l string) string {
	t := strings.TrimSpace(l)
	if rest, ok := strings.CutPrefix(t, "//gonb:"); ok && (strings.HasPrefix(rest, "%") || strings.HasPrefix(rest, "!")) {
		t = rest
	}
	if strings.HasPrefix(t, "!") || strings.HasPrefix(t, "%") {
		return t
	}
	return ""
}

// cellMagic returns the cell magic starting src (its first non-blank line),
// if any: its name, arguments and the rest of the cell.
func cellMagic(src string) (name, args, body string, ok bool) {
	lines := strings.SplitAfter(src, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		t, found := strings.CutPrefix(commandLine(l), "%%")
		if !found {
			return "", "", "", false
		}
		name, args, _ = strings.Cut(strings.TrimSpace(t), " ")
		if !cellMagics[name] {
			return "", "", "", false
		}
		return name, strings.TrimSpace(args), strings.Join(lines[i+1:], ""), true
	}
	return "", "", "", false
}

// parseCell splits a cell into declarations, imports, statements and
// commands. name is used for //line directives (e.g. "In[3]").
func parseCell(cellID, name, src string) (*parsedCell, error) {
	pc := &parsedCell{}

	if magic, args, body, ok := cellMagic(src); ok {
		pc.commands = []Command{{Line: 1, Magic: true, Cell: true, Text: strings.TrimSpace(magic + " " + args), Input: body}}
		return pc, nil
	}

	// Extract shell commands and magics, blanking them to preserve lines.
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		t := commandLine(lines[i])
		switch {
		case strings.HasPrefix(t, "!"):
			line := i + 1
			text := strings.TrimPrefix(t[1:], "*") // GoNB's "!*": in the workspace, like "!"
			lines[i] = ""
			// A trailing backslash continues the command; sh -c joins
			// the lines itself.
			for strings.HasSuffix(text, "\\") && i+1 < len(lines) {
				i++
				text += "\n" + lines[i]
				lines[i] = ""
			}
			pc.commands = append(pc.commands, Command{Line: line, Text: strings.TrimSpace(text)})
		case strings.HasPrefix(t, "%"):
			lines[i] = ""
			if err := pc.addMagic(name, i+1, strings.TrimSpace(t[1:])); err != nil {
				return nil, err
			}
		}
	}
	src = strings.Join(lines, "\n")

	pc.name, pc.src = name, src
	chunks := splitChunks(src)
	for _, c := range chunks {
		if c.decl {
			if err := pc.addDecl(cellID, name, src, c); err != nil {
				return nil, err
			}
		} else {
			if d := parseDefine(src, c); d != nil {
				d.stmt = len(pc.stmts)
				pc.defines = append(pc.defines, d)
			}
			pc.stmts = append(pc.stmts, c)
		}
	}
	pc.render(nil)
	pc.hasCode = len(chunks) > 0 || pc.exec != "" || pc.test
	if pc.userMain != "" && len(pc.stmts) > 0 {
		return nil, fmt.Errorf("%s: cell defines func main() and also has top-level statements", name)
	}
	switch {
	case pc.test && pc.exec != "":
		return nil, fmt.Errorf("%s: %%test and %%exec can't be used together", name)
	case (pc.test || pc.exec != "") && pc.userMain != "":
		return nil, fmt.Errorf("%s: a %%test or %%exec cell can't define func main()", name)
	case pc.test && len(pc.stmts) > 0:
		return nil, fmt.Errorf("%s: a %%test cell can only have declarations (put statements in a test function)", name)
	case pc.exec != "" && len(pc.stmts) > 0:
		return nil, fmt.Errorf("%s: a %%exec cell can only have declarations (%%exec calls %s instead of running statements)", name, pc.exec)
	}
	return pc, nil
}

// addMagic records a %magic line. Magics that change how the cell is
// built are handled here; the others run as commands before the code.
func (pc *parsedCell) addMagic(name string, line int, text string) error {
	fields, err := splitArgs(text)
	if err != nil {
		return fmt.Errorf("%s:%d: %%%s: %w", name, line, text, err)
	}
	if len(fields) == 0 {
		return nil // a lone "%"
	}
	switch head := fields[0]; {
	case head == "%" || head == "main":
		// GoNB: "%%" starts func main(). Statements always go to main
		// here; what it keeps is flag parsing and per-cell arguments.
		pc.parseFlags = true
		if len(fields) > 1 {
			pc.args, pc.hasArgs = fields[1:], true
		}
	case strings.HasPrefix(head, "%"):
		if !cellMagics[head[1:]] {
			return fmt.Errorf("%s:%d: unknown cell magic %%%s (try %%help)", name, line, head)
		}
		return fmt.Errorf("%s:%d: %%%s must be the first line of the cell", name, line, head)
	case head == "exec":
		if len(fields) < 2 {
			return fmt.Errorf("%s:%d: %%exec needs the name of the function to run", name, line)
		}
		pc.exec, pc.execLine, pc.parseFlags = fields[1], line, true
		pc.args, pc.hasArgs = fields[2:], true
	case head == "test":
		pc.test = true
		if len(fields) > 1 {
			pc.args, pc.hasArgs = fields[1:], true
		}
	default:
		pc.commands = append(pc.commands, Command{Line: line, Magic: true, Text: text})
	}
	return nil
}

// splitArgs splits a command line into words like a shell would, without
// expansions: single and double quotes group words, and "" is an empty
// argument.
func splitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inWord := false
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				args = append(args, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if inWord {
		args = append(args, cur.String())
	}
	return args, nil
}

// render sets the main() body from the statements, emitting each define
// as chosen in how (missing ones stay local).
func (pc *parsedCell) render(how map[*define]hoist) {
	mode := map[int]*define{}
	for _, d := range pc.defines {
		mode[d.stmt] = d
	}
	var body, plain strings.Builder
	for i, c := range pc.stmts {
		text := pc.src[c.start:c.end]
		if d := mode[i]; d != nil {
			switch how[d] {
			case hoistFunc:
				continue
			case hoistVar:
				// Same length, so the columns of what follows are kept.
				rel := d.tok - c.start
				text = text[:rel] + " =" + text[rel+2:]
			}
		}
		stmt := lineDirective(pc.name, c.line, c.col) + text + "\n"
		plain.WriteString(stmt)
		if i == len(pc.stmts)-1 {
			if expr, ok := displayExpr(text); ok {
				body.WriteString("Display(\n")
				body.WriteString(lineDirective(pc.name, c.line, c.col))
				body.WriteString(expr)
				body.WriteString(")\n")
				continue
			}
		}
		body.WriteString(stmt)
	}
	pc.body, pc.plain = body.String(), plain.String()
}

// parseDefine returns the define in statement chunk c, if it is one.
func parseDefine(src string, c chunk) *define {
	text := src[c.start:c.end]
	if !strings.Contains(text, ":=") {
		return nil
	}
	const prefix = "package p\nfunc _() {\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", prefix+text+"\n}", parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	body := f.Decls[0].(*ast.FuncDecl).Body.List
	if len(body) != 1 {
		return nil
	}
	as, ok := body[0].(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE {
		return nil
	}
	off := func(p token.Pos) int { return c.start + fset.Position(p).Offset - len(prefix) }
	d := &define{tok: off(as.TokPos)}
	for _, e := range as.Lhs {
		id, ok := e.(*ast.Ident)
		if !ok {
			return nil
		}
		if id.Name == "_" {
			continue
		}
		line, col := cellPos(src, off(id.Pos()))
		d.idents = append(d.idents, defIdent{name: id.Name, line: line, col: col})
	}
	if len(d.idents) == 0 {
		return nil
	}
	if lit, ok := as.Rhs[0].(*ast.FuncLit); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 {
		start := off(lit.Pos())
		line, col := cellPos(src, start)
		d.fn = &span{start: start, end: off(lit.End()), line: line, col: col}
	}
	return d
}

// cellPos converts a byte offset in src to a 1-based line and column.
func cellPos(src string, off int) (line, col int) {
	before := src[:off]
	line = strings.Count(before, "\n") + 1
	return line, off - strings.LastIndexByte(before, '\n')
}

// uninteresting are function/method names whose results (byte counts,
// errors) shouldn't be displayed when they end a cell.
var uninteresting = map[string]bool{
	"Print": true, "Printf": true, "Println": true,
	"Fprint": true, "Fprintf": true, "Fprintln": true,
	"Scan": true, "Scanf": true, "Scanln": true,
	"Sscan": true, "Sscanf": true, "Sscanln": true,
	"Fscan": true, "Fscanf": true, "Fscanln": true,
	"Write": true, "WriteString": true, "WriteByte": true, "WriteRune": true, "WriteTo": true,
	"Copy": true, "CopyN": true, "ReadFrom": true, "Close": true,
	"Display": true, "DisplayMarkdown": true, "DisplayPNG": true,
	"DisplayID": true, "DisplayMarkdownID": true,
	"print": true, "println": true, "panic": true, "close": true, "delete": true, "clear": true,
}

// displayExpr reports whether a trailing statement is a bare expression
// whose value should be displayed as the cell result, like Jupyter does.
// Calls that return no value are handled by retrying the build without
// the wrapper.
func displayExpr(text string) (string, bool) {
	t := strings.TrimSuffix(strings.TrimSpace(text), ";")
	expr, err := parser.ParseExpr(t)
	if err != nil {
		return "", false
	}
	for {
		p, ok := expr.(*ast.ParenExpr)
		if !ok {
			break
		}
		expr = p.X
	}
	if call, ok := expr.(*ast.CallExpr); ok {
		var name string
		switch f := call.Fun.(type) {
		case *ast.Ident:
			name = f.Name
		case *ast.SelectorExpr:
			name = f.Sel.Name
		}
		if uninteresting[name] {
			return "", false
		}
	}
	return t, true
}

func (pc *parsedCell) addDecl(cellID, name, src string, c chunk) error {
	text := src[c.start:c.end]
	prefixed := lineDirective(name, c.line, c.col) + text
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", "package main\n"+prefixed, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "main" {
				pc.userMain = prefixed
				continue
			}
			if d.Recv == nil {
				pc.addTestFunc(d.Name.Name)
			}
			if fn := d.Name.Name; d.Recv == nil && strings.HasPrefix(fn, "init_") {
				// GoNB: every init_xxx() is an init(), so each cell can
				// have its own. The name is padded to keep the columns;
				// the key stays init_xxx so redefining it replaces it.
				off := fset.Position(d.Name.Pos()).Offset - len("package main\n") - (len(prefixed) - len(text))
				text = text[:off] + "init" + strings.Repeat(" ", len(fn)-len("init")) + text[off+len(fn):]
				prefixed = lineDirective(name, c.line, c.col) + text
			}
			key := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				key = recvTypeName(d.Recv.List[0].Type) + "." + key
			} else if key == "init" {
				key = fmt.Sprintf("init#%s#%d", cellID, c.start)
			}
			pc.decls = append(pc.decls, &Decl{Names: []string{key}, Src: prefixed, Raw: text, CellID: cellID})
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				for _, s := range d.Specs {
					is := s.(*ast.ImportSpec)
					imp := &Import{Path: strings.Trim(is.Path.Value, "\"`"), CellID: cellID}
					if is.Name != nil {
						imp.Name = is.Name.Name
					}
					pc.imports = append(pc.imports, imp)
				}
				continue
			}
			var names []string
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					names = append(names, s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name == "_" {
							names = append(names, fmt.Sprintf("_#%s#%d", cellID, c.start))
						} else {
							names = append(names, n.Name)
						}
					}
				}
			}
			pc.decls = append(pc.decls, &Decl{Names: names, Src: prefixed, Raw: text, CellID: cellID})
		}
	}
	return nil
}

// addTestFunc records the test functions a %test cell runs by default.
func (pc *parsedCell) addTestFunc(name string) {
	for _, prefix := range []string{"Test", "Example", "Fuzz", "Benchmark"} {
		rest, ok := strings.CutPrefix(name, prefix)
		// As go test: TestFoo and Test_foo, not Testing.
		if !ok || (rest != "" && !startsUpperOrDigitOrUnderscore(rest)) {
			continue
		}
		if prefix == "Benchmark" {
			pc.benchmarks = append(pc.benchmarks, name)
		} else {
			pc.tests = append(pc.tests, name)
		}
		return
	}
}

func startsUpperOrDigitOrUnderscore(s string) bool {
	r := []rune(s)[0]
	return r == '_' || unicode.IsUpper(r) || unicode.IsDigit(r)
}

// testArgs are the flags a %test cell runs with when none are given: the
// cell's own tests and benchmarks, verbosely.
func (pc *parsedCell) testArgs() []string {
	if pc.hasArgs {
		return pc.args
	}
	anchored := func(names []string) string {
		if len(names) == 0 {
			return "^$"
		}
		return "^(" + strings.Join(names, "|") + ")$"
	}
	args := []string{"-test.v", "-test.run=" + anchored(pc.tests)}
	if len(pc.benchmarks) > 0 {
		args = append(args, "-test.bench="+anchored(pc.benchmarks))
	}
	return args
}

func recvTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.IndexExpr:
		return recvTypeName(t.X)
	case *ast.IndexListExpr:
		return recvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.ParenExpr:
		return recvTypeName(t.X)
	}
	return "?"
}
