package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/complete"
)

func TestDiagnosticMarksAndOverlap(t *testing.T) {
	m, f := diagModel()
	_ = m.diagnoseIfChanged()
	warning := f.items[0]
	warning.Severity = complete.SeverityWarning
	warning.Message = "warning"
	m.diag.items = append([]complete.Diagnostic{warning}, f.items...)
	m.cur().ed.SetCursor(0, 2)
	d, ok := m.diagAtCursor()
	if !ok || d.Severity != complete.SeverityError {
		t.Fatal(d, ok)
	}
	marks := m.diagMarks(m.cur())
	if len(marks) != 2 || marks[0].Color != colError {
		t.Fatal(marks)
	}
	// Check run boundaries independently of syntax highlighting.
	e := NewEditor("text", "abcd")
	v := e.Render(20, true, m.hl, m.theme, []Mark{{Row: 0, Start: 1, End: 3, Color: colError}})
	line := v.lines[0]
	at := strings.Index(line, "4:3")
	if at < 0 {
		t.Fatalf("no curly underline: %q", line)
	}
	// The underline begins after a, and its reset precedes d.
	if !strings.HasSuffix(ansi.Strip(line[:strings.LastIndex(line[:at], "\x1b")]), "a") {
		t.Fatalf("underline starts at wrong rune: %q", line)
	}
	if !strings.Contains(ansi.Strip(line[strings.LastIndex(line[:at], "\x1b"):]), "bcd") {
		t.Fatalf("wrong marked text: %q", line)
	}
}

func TestDiagnosticZeroWidthAndWrapping(t *testing.T) {
	m := newCompletionModel()
	e := NewEditor("text", "abcd")
	v := e.Render(7, true, m.hl, m.theme, []Mark{{Row: 0, Start: 4, End: 4, Color: colError}})
	if len(v.lines) != 2 || !strings.Contains(v.lines[1], "4:3") || !strings.HasSuffix(ansi.Strip(v.lines[1]), " ") {
		t.Fatalf("%q", v.lines)
	}
}
