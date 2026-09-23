package kernel

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strings"
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
	seq    int
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
}

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

// parseCell splits a cell into declarations, imports, statements and
// commands. name is used for //line directives (e.g. "In[3]").
func parseCell(cellID, name, src string) (*parsedCell, error) {
	pc := &parsedCell{}

	// Extract shell commands and magics, blanking them to preserve lines.
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "!"):
			pc.commands = append(pc.commands, Command{Line: i + 1, Text: strings.TrimSpace(t[1:])})
			lines[i] = ""
		case t == "%%" || strings.HasPrefix(t, "%%"):
			// GoNB compatibility: "%%" marks the start of main; we don't need it.
			lines[i] = ""
		case strings.HasPrefix(t, "%"):
			pc.commands = append(pc.commands, Command{Line: i + 1, Magic: true, Text: strings.TrimSpace(t[1:])})
			lines[i] = ""
		}
	}
	src = strings.Join(lines, "\n")

	chunks := splitChunks(src)
	var body, plain strings.Builder
	var stmts []chunk
	for _, c := range chunks {
		if c.decl {
			if err := pc.addDecl(cellID, name, src, c); err != nil {
				return nil, err
			}
		} else {
			stmts = append(stmts, c)
		}
	}
	for i, c := range stmts {
		text := src[c.start:c.end]
		stmt := lineDirective(name, c.line, c.col) + text + "\n"
		plain.WriteString(stmt)
		if i == len(stmts)-1 {
			if expr, ok := displayExpr(text); ok {
				body.WriteString("Display(\n")
				body.WriteString(lineDirective(name, c.line, c.col))
				body.WriteString(expr)
				body.WriteString(")\n")
				continue
			}
		}
		body.WriteString(stmt)
	}
	pc.body, pc.plain = body.String(), plain.String()
	pc.hasCode = len(chunks) > 0
	if pc.userMain != "" && len(stmts) > 0 {
		return nil, fmt.Errorf("%s: cell defines func main() and also has top-level statements", name)
	}
	return pc, nil
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
	"Display": true, "DisplayMarkdown": true,
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
