package kernel

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// VarPeek is a program's last reported type and one-line value preview.
type VarPeek struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Variable describes a persisted declaration for the variable inspector.
type Variable struct {
	Name string
	// Kind is var, const, type or func.
	Kind string
	// Type is the static type when explicit, otherwise the last dynamic
	// type, function signature, or declared type's underlying kind.
	Type string
	// Value is the last run's one-line preview, or a const's source expression.
	Value string
	// Hoisted indicates a variable carried over from a := statement.
	Hoisted bool
	// CellID identifies the declaring cell, which may no longer exist in the UI.
	CellID string
}

var mainQualifier = regexp.MustCompile(`\bmain\.`)

func printExpr(n ast.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), n); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Variables lists persisted declarations sorted by name, excluding methods
// and internal names. Reading it neither runs code nor type-checks a cell.
func (k *Kernel) Variables() []Variable {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []Variable
	for _, d := range k.decls {
		f, err := parser.ParseFile(token.NewFileSet(), "", "package main\n"+d.Raw, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, name := range d.Names {
			if strings.ContainsAny(name, "#.") {
				continue
			}
			v := Variable{Name: name, Kind: d.Tok.String(), CellID: d.CellID, Hoisted: d.Var != nil}
			for _, decl := range f.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					if decl.Name.Name == "init" || strings.HasPrefix(decl.Name.Name, "init_") {
						continue
					}
					v.Type = printExpr(decl.Type)
				case *ast.GenDecl:
					for _, spec := range decl.Specs {
						switch spec := spec.(type) {
						case *ast.TypeSpec:
							if spec.Name.Name != name {
								continue
							}
							switch spec.Type.(type) {
							case *ast.StructType:
								v.Type = "struct"
							case *ast.InterfaceType:
								v.Type = "interface"
							default:
								v.Type = printExpr(spec.Type)
							}
						case *ast.ValueSpec:
							for i, n := range spec.Names {
								if n.Name != name {
									continue
								}
								if spec.Type != nil {
									v.Type = printExpr(spec.Type)
								}
								if d.Tok == token.CONST && i < len(spec.Values) {
									v.Value = printExpr(spec.Values[i])
								}
							}
						}
					}
				}
			}
			if d.Tok == token.FUNC && (name == "init" || strings.HasPrefix(name, "init_")) {
				continue
			}
			if d.Tok == token.VAR {
				p := k.peeks[name]
				if v.Type == "" {
					v.Type = mainQualifier.ReplaceAllString(p.Type, "")
				}
				v.Value = p.Value
			}
			out = append(out, v)
		}
	}
	slices.SortFunc(out, func(a, b Variable) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// A shared deadline bounds the whole batch; parallel previews allow later
// variables to be reported even when an earlier String method blocks.
func writePeeks(b *strings.Builder, decls []*Decl) {
	var names []string
	for _, d := range decls {
		if d.Tok == token.VAR {
			for _, n := range d.Names {
				if !strings.Contains(n, "#") {
					names = append(names, n)
				}
			}
		}
	}
	if len(names) == 0 {
		return
	}
	b.WriteString("defer gopyterPeekVars(\n")
	for _, n := range names {
		b.WriteString("func() { gopyterPeekVar(")
		b.WriteString(strconv.Quote(n))
		b.WriteString(", ")
		b.WriteString(n)
		b.WriteString(") },\n")
	}
	b.WriteString(")\n")
}
