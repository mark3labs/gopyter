package complete

import (
	"go/scanner"
	"go/token"
	"sort"
	"strings"
)

var keywords = []string{
	"break", "case", "chan", "const", "continue", "default", "defer", "else",
	"fallthrough", "for", "func", "go", "goto", "if", "import", "interface",
	"map", "package", "range", "return", "select", "struct", "switch", "type", "var",
}

var builtins = map[string]Kind{
	"append": KindFunc, "cap": KindFunc, "clear": KindFunc, "close": KindFunc,
	"complex": KindFunc, "copy": KindFunc, "delete": KindFunc, "imag": KindFunc,
	"len": KindFunc, "make": KindFunc, "max": KindFunc, "min": KindFunc,
	"new": KindFunc, "panic": KindFunc, "print": KindFunc, "println": KindFunc,
	"real": KindFunc, "recover": KindFunc,
	"Display": KindFunc, "DisplayMarkdown": KindFunc, "DisplayPNG": KindFunc,
	"DisplayID": KindFunc, "DisplayMarkdownID": KindFunc,
	"bool": KindType, "byte": KindType, "complex64": KindType, "complex128": KindType,
	"error": KindInterface, "float32": KindType, "float64": KindType, "int": KindType,
	"int8": KindType, "int16": KindType, "int32": KindType, "int64": KindType,
	"rune": KindType, "string": KindType, "uint": KindType, "uint8": KindType,
	"uint16": KindType, "uint32": KindType, "uint64": KindType, "uintptr": KindType,
	"any": KindInterface, "comparable": KindInterface,
	"true": KindConst, "false": KindConst, "iota": KindConst, "nil": KindConst,
	"fmt": KindPackage, "strings": KindPackage, "strconv": KindPackage,
	"math": KindPackage, "sort": KindPackage, "slices": KindPackage, "maps": KindPackage,
	"os": KindPackage, "time": KindPackage, "errors": KindPackage, "bytes": KindPackage,
}

// basic completes keywords, builtins and identifiers seen in the notebook.
func (e *Engine) basic(req Request) Result {
	lines := strings.Split(req.Src, "\n")
	row := min(max(req.Row, 0), len(lines)-1)
	line := []rune(lines[row])
	col := min(max(req.Col, 0), len(line))
	prefix := string(line[col-wordLen(string(line[:col])) : col])
	res := Result{Source: "basic", Replace: len([]rune(prefix))}
	// Members after "." need type information.
	if n := col - len([]rune(prefix)); n > 0 && line[n-1] == '.' {
		return res
	}
	if prefix == "" && !req.Manual {
		return res
	}

	seen := map[string]bool{prefix: true}
	add := func(label string, kind Kind) {
		if seen[label] || !strings.HasPrefix(strings.ToLower(label), strings.ToLower(prefix)) {
			return
		}
		seen[label] = true
		res.Items = append(res.Items, Item{Label: label, Insert: label, Kind: kind, Replace: res.Replace})
	}

	for _, name := range e.k.Declarations() {
		if !strings.Contains(name, ".") {
			add(name, KindVar)
		}
	}
	var s scanner.Scanner
	fset := token.NewFileSet()
	src := []byte(req.Src)
	s.Init(fset.AddFile("", fset.Base(), len(src)), src, nil, 0)
	var idents []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.IDENT {
			idents = append(idents, lit)
		}
	}
	sort.Strings(idents)
	for _, id := range idents {
		add(id, KindVar)
	}
	names := make([]string, 0, len(builtins))
	for n := range builtins {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		add(n, builtins[n])
	}
	for _, kw := range keywords {
		add(kw, KindKeyword)
	}
	return res
}
