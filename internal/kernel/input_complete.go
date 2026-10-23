package kernel

import (
	"go/scanner"
	"go/token"
	"strings"
)

// InputComplete reports whether console input is ready to run. Malformed
// input is submitted too, so the compiler can report it rather than trap the
// user in a continuation prompt.
func InputComplete(src string) bool {
	if strings.TrimSpace(src) == "" {
		return true
	}
	if _, _, _, ok := cellMagic(src); ok {
		return strings.HasSuffix(src, "\n\n")
	}
	lines := strings.Split(strings.TrimSuffix(src, "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		cmd := commandLine(lines[i])
		if cmd == "" {
			continue
		}
		lines[i] = ""
		if strings.HasPrefix(cmd, "!") {
			for strings.HasSuffix(cmd, "\\") {
				if i+1 == len(lines) {
					return false
				}
				i++
				cmd = lines[i]
				lines[i] = ""
			}
		}
	}
	src = strings.Join(lines, "\n")
	fs := token.NewFileSet()
	var s scanner.Scanner
	open, malformed := false, false
	s.Init(fs.AddFile("input", -1, len(src)), []byte(src), func(_ token.Position, msg string) {
		if msg == "raw string literal not terminated" || msg == "comment not terminated" {
			open = true
		} else {
			malformed = true
		}
	}, 0)
	var stack []token.Token
	last := token.ILLEGAL
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		last = tok
		switch tok {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			stack = append(stack, tok)
		case token.RPAREN, token.RBRACK, token.RBRACE:
			if len(stack) == 0 {
				malformed = true
				continue
			}
			want := map[token.Token]token.Token{token.RPAREN: token.LPAREN, token.RBRACK: token.LBRACK, token.RBRACE: token.LBRACE}[tok]
			if stack[len(stack)-1] != want {
				malformed = true
			}
			stack = stack[:len(stack)-1]
		}
	}
	if open {
		return false
	}
	if malformed {
		return true
	}
	if len(stack) > 0 {
		return false
	}
	switch last {
	case token.RPAREN, token.RBRACK, token.RBRACE, token.INC, token.DEC, token.SEMICOLON:
		return true
	}
	return !last.IsOperator() && last != token.ELSE
}
