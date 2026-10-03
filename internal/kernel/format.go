package kernel

import (
	"fmt"
	"go/format"
	"strings"
)

// FormatCell formats notebook Go code without executing it or changing kernel
// state. Declarations and statements retain their source order, and notebook
// commands retain their exact text. On error it returns the original source.
func FormatCell(src string) (string, error) {
	if _, _, _, ok := cellMagic(src); ok {
		return src, nil
	}
	if strings.TrimSpace(src) == "" {
		return src, nil
	}

	// Commands become comments while formatting, so even commands inside a Go
	// block do not break its syntax. Choose a marker absent from user source.
	marker := "// gopyter_format_command_"
	for strings.Contains(src, marker) {
		marker += "_"
	}
	masked, commands := formatMaskCommands(src, marker)
	chunks := splitChunks(masked)
	var out strings.Builder
	start := 0
	for i, c := range chunks {
		if i+1 < len(chunks) && c.decl == chunks[i+1].decl {
			continue
		}
		end := len(masked)
		if i+1 < len(chunks) {
			end = formatChunkEnd(masked, c.end)
		}
		fragment := strings.TrimSpace(masked[start:end])
		// A complete file enables gofmt's import sorting. Declaration
		// fragments alone deliberately skip that part of go/format.
		const header = "package gopyterformat\n\n"
		if c.decl {
			fragment = header + fragment
		}
		text, err := format.Source([]byte(fragment))
		if err != nil {
			return src, fmt.Errorf("format cell: %w", err)
		}
		formatted := string(text)
		if c.decl {
			formatted = strings.TrimPrefix(formatted, header)
		}
		out.WriteString(strings.TrimSpace(formatted))
		out.WriteByte('\n')
		start = end
	}
	if len(chunks) == 0 {
		text, err := format.Source([]byte(strings.TrimSpace(masked)))
		if err != nil {
			return src, fmt.Errorf("format cell: %w", err)
		}
		out.WriteString(strings.TrimSpace(string(text)))
		out.WriteByte('\n')
	}
	lines := strings.SplitAfter(out.String(), "\n")
	for i, line := range lines {
		if original, ok := commands[strings.TrimSpace(line)]; ok {
			lines[i] = original
			// A final command without a newline must remain byte-for-byte intact.
		}
	}
	return strings.Join(lines, ""), nil
}

// formatChunkEnd keeps a same-line trailing comment with its declaration,
// including when an explicit semicolon precedes it.
func formatChunkEnd(src string, end int) int {
	for end < len(src) {
		switch {
		case src[end] == ' ' || src[end] == '\t' || src[end] == '\r':
			end++
		case strings.HasPrefix(src[end:], "//"):
			if n := strings.IndexByte(src[end:], '\n'); n >= 0 {
				return end + n + 1
			}
			return len(src)
		case strings.HasPrefix(src[end:], "/*"):
			n := strings.Index(src[end+2:], "*/")
			if n < 0 {
				return len(src) // go/format reports the unterminated comment.
			}
			end += n + 4
		default:
			return end
		}
	}
	return end
}

// formatMaskCommands tracks Go lexical context rather than treating every line
// beginning with ! or % as a command: raw strings and block comments can contain
// arbitrary notebook-looking text. Shell continuations are opaque too.
func formatMaskCommands(src, marker string) (string, map[string]string) {
	lines := strings.SplitAfter(src, "\n")
	commands := make(map[string]string)
	var out strings.Builder
	var state byte // zero, `, ", ', or / (block comment)
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if state == 0 && commandLine(line) != "" {
			var original strings.Builder
			original.WriteString(line)
			if strings.HasPrefix(commandLine(line), "!") {
				for strings.HasSuffix(strings.TrimSuffix(strings.TrimSuffix(lines[i], "\n"), "\r"), "\\") && i+1 < len(lines) {
					i++
					original.WriteString(lines[i])
				}
			}
			key := fmt.Sprintf("%s%d", marker, len(commands))
			commands[key] = original.String()
			out.WriteString(key)
			out.WriteByte('\n')
			continue
		}
		out.WriteString(line)
		for j := 0; j < len(line); j++ {
			b := line[j]
			switch state {
			case '`':
				if b == '`' {
					state = 0
				}
			case '"', '\'':
				switch b {
				case '\\':
					j++
				case state:
					state = 0
				}
			case '/':
				if b == '*' && j+1 < len(line) && line[j+1] == '/' {
					state = 0
					j++
				}
			default:
				switch {
				case b == '/' && j+1 < len(line) && line[j+1] == '/':
					j = len(line)
				case b == '/' && j+1 < len(line) && line[j+1] == '*':
					state = '/'
					j++
				case b == '`' || b == '"' || b == '\'':
					state = b
				}
			}
		}
	}
	return out.String(), commands
}
