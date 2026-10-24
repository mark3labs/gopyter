package ui

import (
	"fmt"
	"image"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func outputCellModel() *Model {
	m := New(Options{Notebook: &notebook.Notebook{Cells: []*notebook.Cell{{
		ID: "a", Type: notebook.Code, Source: "42", ExecutionCount: 7,
	}}}})
	m.width, m.height, m.mode = 100, 60, modeCommand
	return m
}

// TestHelpColumnsKeepGap is a regression test: a description as long as its
// column ("insert / open line") ran straight into the next column's key.
func TestHelpColumnsKeepGap(t *testing.T) {
	m := outputCellModel()
	m.width, m.height = 110, 40
	m.vim.enabled = true
	m.overlay = overlayHelp
	text, _ := m.renderOverlay()
	found := 0
	for line := range strings.SplitSeq(ansi.Strip(text), "\n") {
		runes := []rune(line)
		for _, k := range []string{"i a o", "d c y"} {
			before, _, ok := strings.Cut(line, k)
			if !ok {
				continue
			}
			found++
			// Columns are 26 wide; the last one must stay blank.
			if end := len([]rune(before)) + 25; end < len(runes) && runes[end] != ' ' {
				t.Errorf("%q runs into the next column: %q", k, line)
			}
		}
	}
	if found == 0 {
		t.Fatal("vim help rows not rendered")
	}
	// Keys as wide as their column ran into their description too.
	plain := ansi.Strip(text)
	for _, want := range []string{"alt+k/F1 ", "alt+⇧+f ", "extend up ", "extend down "} {
		if !strings.Contains(plain, want) {
			t.Errorf("help is missing %q", want)
		}
	}
}

// TestHelpEntriesFit keeps every help entry, AI and vim ones included,
// within its column, so none is cut with "…" ("split cell (edit…").
func TestHelpEntriesFit(t *testing.T) {
	for _, sec := range newKeyMap().fullHelp(true, true, true) {
		// Mirrors renderOverlay: a key column of 8-12, 25 columns in all.
		keyWidth := 8
		for _, b := range sec.keys {
			if b.Enabled() {
				keyWidth = max(keyWidth, min(12, ansi.StringWidth(b.Help().Key)+1))
			}
		}
		for _, b := range sec.keys {
			if !b.Enabled() {
				continue
			}
			h := b.Help()
			if ansi.StringWidth(h.Key) > keyWidth-1 || ansi.StringWidth(h.Desc) > 25-keyWidth {
				t.Errorf("%s: %q %q doesn't fit (key column %d)", sec.title, h.Key, h.Desc, keyWidth)
			}
		}
	}
}

func numberedOutput(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func cellZone(t *testing.T, r cellRender, kind actionKind) zone {
	t.Helper()
	var found []zone
	for _, z := range r.zones {
		if z.act.kind == kind {
			found = append(found, z)
		}
	}
	if len(found) != 1 {
		t.Fatalf("got %d zones for action %v, want 1", len(found), kind)
	}
	return found[0]
}

func TestRenderCellFoldedOutput(t *testing.T) {
	for _, result := range []bool{false, true} {
		t.Run(fmt.Sprintf("result=%v", result), func(t *testing.T) {
			m := outputCellModel()
			c := m.cells[0]
			n := maxOutputLines + 5
			if result {
				n-- // Keep the total output length (and hidden count) the same.
			}
			c.appendOutput(notebook.Stdout, numberedOutput(n))
			if result {
				c.appendOutput(notebook.Result, "42")
			}
			r := m.renderCell(0, 100)
			top := r.edTop + len(r.ev.lines) + 1
			want := "⋯ 5 earlier lines hidden · click or o to expand"
			if got := strings.TrimSpace(strings.TrimPrefix(ansi.Strip(r.lines[top]), "▌")); got != want {
				t.Fatalf("first output line %q, want %q", got, want)
			}
			z := cellZone(t, r, actToggleOutput)
			if wantRect := image.Rect(gutterWidth+2, top, gutterWidth+2+lipgloss.Width(want), top+1); z.rect != wantRect || z.act.cell != 0 {
				t.Fatalf("fold zone %+v, want %v", z, wantRect)
			}
			if len(r.lines)-top != maxOutputLines+1 || !strings.HasSuffix(ansi.Strip(r.lines[top+1]), "line 5") {
				t.Fatalf("folded output %q", plainLines(r.lines[top:]))
			}
			if result && !strings.Contains(ansi.Strip(r.lines[len(r.lines)-1]), "Out[7]   42") {
				t.Fatalf("result label misplaced: %q", plainLines(r.lines[top:]))
			}
		})
	}
}

func TestRenderCellExpandedOutput(t *testing.T) {
	m := outputCellModel()
	c := m.cells[0]
	c.appendOutput(notebook.Stdout, numberedOutput(maxOutputLines+5))
	c.expanded = true
	r := m.renderCell(0, 100)
	last := len(r.lines) - 1
	want := "⋯ click or o to collapse"
	if got := strings.TrimSpace(strings.TrimPrefix(ansi.Strip(r.lines[last]), "▌")); got != want {
		t.Fatalf("last line %q, want %q", got, want)
	}
	z := cellZone(t, r, actToggleOutput)
	if wantRect := image.Rect(gutterWidth+2, last, gutterWidth+2+lipgloss.Width(want), last+1); z.rect != wantRect {
		t.Fatalf("collapse zone %v, want %v", z.rect, wantRect)
	}
	top := r.edTop + len(r.ev.lines) + 1
	if last-top != maxOutputLines+5 || !strings.HasSuffix(ansi.Strip(r.lines[top]), "line 0") {
		t.Fatalf("expanded output %q", plainLines(r.lines[top:]))
	}
}

func TestRenderCellFooterButtons(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		selected, hovered, buttons bool
		width                      int
	}{
		{"selected", true, false, true, 100},
		{"hovered", false, true, true, 100},
		{"idle", false, false, false, 100},
		{"narrow selected", true, false, false, 20},
		{"narrow hovered", false, true, false, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := outputCellModel()
			m.sel, m.hoverCell = -1, -1
			color, bar := m.theme.borderIdle, " "
			if tc.hovered {
				m.hoverCell, color = 0, colMuted
			}
			if tc.selected {
				m.sel, color = 0, m.theme.borderCmd
				bar = "▌"
			}
			r := m.renderCell(0, tc.width)
			last := len(r.lines) - 1
			var footerZones []zone
			for _, z := range r.zones {
				if z.rect.Min.Y == last {
					footerZones = append(footerZones, z)
					if z.rect.Max.Y != last+1 || z.act.cell != 0 {
						t.Fatalf("footer zone %+v", z)
					}
				}
			}
			if tc.buttons {
				if len(footerZones) == 0 || !strings.Contains(ansi.Strip(r.lines[last]), " fmt ") {
					t.Fatalf("missing footer buttons: %q, zones %+v", ansi.Strip(r.lines[last]), footerZones)
				}
				z := cellZone(t, r, actFormatCell)
				if z.rect.Min.Y != last {
					t.Fatalf("format zone on line %d, want %d", z.rect.Min.Y, last)
				}
			} else {
				want := bar + strings.Repeat(" ", gutterWidth-1) + boxBottom(tc.width-gutterWidth, lipgloss.NewStyle().Foreground(color))
				if ansi.Strip(r.lines[last]) != ansi.Strip(want) || len(footerZones) != 0 {
					t.Fatalf("footer %q, want %q without zones; got %+v", r.lines[last], want, footerZones)
				}
			}
		})
	}
}

func TestRenderCellWidgetZones(t *testing.T) {
	for _, n := range []int{0, maxOutputLines + 5} {
		for _, expanded := range []bool{false, true} {
			t.Run(fmt.Sprintf("lines=%d/expanded=%v", n, expanded), func(t *testing.T) {
				m := outputCellModel()
				c := m.cells[0]
				c.expanded = expanded
				if n > 0 {
					c.appendOutput(notebook.Stdout, numberedOutput(n))
				}
				c.appendOutput(notebook.HTMLOut, `<button data-address="/b">Go</button>`)
				m.running = &runState{cellID: c.id}
				m.in.events = &pipeQueue{} // Rendering only: no writer goroutine is needed.
				r := m.renderCell(0, 100)
				z := cellZone(t, r, actWidgetClick)
				top := r.edTop + len(r.ev.lines) + 1
				if z.rect.Min.Y != top+n || z.act.id != "/b" || z.act.cell != 0 {
					t.Fatalf("widget zone %+v, want line %d", z, top+n)
				}
				line := ansi.Strip(r.lines[z.rect.Min.Y])
				if !strings.Contains(line, " Go ") || z.rect.Min.X != gutterWidth+2 || z.rect.Max.Y != z.rect.Min.Y+1 {
					t.Fatalf("widget zone %v does not match line %q", z.rect, line)
				}
				// Live widgets never fold away; expanded long output gets a trailing collapse line.
				folds := 0
				for _, f := range r.zones {
					if f.act.kind == actToggleOutput {
						folds++
						if !expanded || n == 0 || f.rect.Min.Y != top+len(c.outLines) {
							t.Fatalf("unexpected fold zone %+v", f)
						}
					}
				}
				if (folds == 1) != (expanded && n > 0) {
					t.Fatalf("got %d fold zones", folds)
				}
				done := cellZone(t, r, actInputDone)
				if done.rect.Min.Y != len(r.lines)-1 {
					t.Fatalf("input zone %+v not on last line", done)
				}
			})
		}
	}
}
