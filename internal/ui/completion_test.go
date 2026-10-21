package ui

import (
	"context"
	"image/color"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/complete"
	"github.com/mark3labs/gopyter/internal/notebook"
)

type fakeCompleter struct{ items []complete.Item }

func (f fakeCompleter) Complete(_ context.Context, req complete.Request) (complete.Result, error) {
	word, _ := NewEditor("go", req.Src).lineWord(req.Row, req.Col)
	res := complete.Result{Source: "fake", Replace: len([]rune(word))}
	for _, it := range f.items {
		it.Replace = res.Replace
		res.Items = append(res.Items, it)
	}
	return res, nil
}

// lineWord returns the identifier before (row, col); test helper.
func (e *Editor) lineWord(row, col int) (string, rune) {
	e.SetCursor(row, col)
	return e.WordBeforeCursor()
}

// typeText sends key presses and runs the resulting completion commands
// synchronously (debounce ticks are fired immediately).
func typeText(m *Model, s string) {
	for _, r := range s {
		cmd := m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
		runCompletionCmds(m, cmd)
	}
}

func runCompletionCmds(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runCompletionCmds(m, c)
		}
	case completionTickMsg:
		if msg.seq == m.comp.seq {
			runCompletionCmds(m, m.requestCompletion(msg.trigger, false))
		}
	case completionResultMsg:
		runCompletionCmds(m, m.handleCompletionResult(msg))
	}
}

func newCompletionModel(items ...complete.Item) *Model {
	nb := &notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code}}}
	m := New(Options{Notebook: nb, Completer: fakeCompleter{items}})
	m.width, m.height = 100, 30
	return m
}

func TestCompletionFlow(t *testing.T) {
	m := newCompletionModel(
		complete.Item{Label: "Println", Insert: "Println", Kind: complete.KindFunc, Detail: "func(a ...any) (n int, err error)"},
		complete.Item{Label: "Printf", Insert: "Printf", Kind: complete.KindFunc, Detail: "func(format string, a ...any) (n int, err error)"},
		complete.Item{Label: "Sprint", Insert: "Sprint", Kind: complete.KindFunc, Detail: "func(a ...any) string"},
		complete.Item{Label: "Stringer", Insert: "Stringer", Kind: complete.KindInterface},
	)
	if m.mode != modeEdit {
		t.Fatal("expected edit mode")
	}

	typeText(m, "fmt.Pr")
	// Prefix matches come first, then fuzzy ones (sPRint).
	if !m.comp.open || len(m.comp.items) != 3 || m.comp.items[0].Label != "Println" ||
		m.comp.items[1].Label != "Printf" || m.comp.items[2].Label != "Sprint" {
		t.Fatalf("got open=%v %v", m.comp.open, m.comp.items)
	}

	// Typing narrows the list locally; a fuzzy match stays after prefixes.
	typeText(m, "intl")
	if len(m.comp.items) != 1 || m.comp.items[0].Label != "Println" {
		t.Fatalf("filter: %v", m.comp.items)
	}

	// Accept: the word is replaced and parentheses added, cursor inside.
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	ed := m.cur().ed
	if got := ed.Value(); got != "fmt.Println()" {
		t.Fatalf("accept: %q", got)
	}
	if _, col := ed.Cursor(); col != len("fmt.Println(") {
		t.Fatalf("cursor col %d", col)
	}
	if m.comp.open {
		t.Fatal("popup should close after accepting")
	}

	// Undo restores the typed text in one step.
	ed.Undo()
	if got := ed.Value(); got != "fmt.Printl" {
		t.Fatalf("undo: %q", got)
	}

	// Non-identifier characters close the popup; esc dismisses it without
	// leaving edit mode.
	ed.SetValue("")
	typeText(m, "S")
	if !m.comp.open {
		t.Fatal("expected popup")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.comp.open || m.mode != modeEdit {
		t.Fatalf("esc: open=%v mode=%v", m.comp.open, m.mode)
	}
	typeText(m, "t(")
	if m.comp.open {
		t.Fatal("'(' should close the popup")
	}

	// No popup inside string literals.
	ed.SetValue("")
	typeText(m, `x := "Pr`)
	if m.comp.open {
		t.Fatal("no completion inside strings")
	}
}

func TestCompletionIconsUsePalette(t *testing.T) {
	themeModel(t, Options{}) // restore the default palette after the test
	for _, name := range ThemeNames() {
		def, ok := lookupTheme(name)
		if !ok {
			t.Fatalf("missing theme %q", name)
		}
		for _, dark := range []bool{false, true} {
			setPalette(def.palette(dark))
			paletteColors := []color.Color{
				colPrimary, colInfo, colAccent, colSuccess, colWarning,
				colError, colOrange, colText, colDim, colMuted,
			}
			for k := complete.KindOther; k <= complete.KindKeyword; k++ {
				if got := kindColor(k); !slices.Contains(paletteColors, got) {
					t.Errorf("%s dark=%v: kind %d color %v is not in the palette", name, dark, k, got)
				}
			}
			if got := kindColor(complete.Kind(-1)); got != colDim {
				t.Errorf("%s dark=%v: unknown kind color = %v, want %v", name, dark, got, colDim)
			}
		}
	}
}

func TestCompletionIconsFollowTheme(t *testing.T) {
	themeModel(t, Options{})
	setPalette(defaultTheme().palette(true))
	dark := kindColor(complete.KindVar)
	setPalette(defaultTheme().palette(false))
	if light := kindColor(complete.KindVar); light == dark {
		t.Fatalf("variable icon color did not change between variants: %v", light)
	}
}

func TestCompletionIconsRenderAfterThemeChange(t *testing.T) {
	var items []complete.Item
	for k := complete.KindOther; k <= complete.KindKeyword; k++ {
		items = append(items, complete.Item{Label: "candidate", Insert: "candidate", Kind: k})
	}
	m := themeModel(t, Options{Completer: fakeCompleter{items}})
	m.cur().ed.SetValue("")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	typeText(m, "ca")
	if !m.comp.open || len(m.comp.items) != len(items) {
		t.Fatalf("expected all completion kinds, got open=%v items=%v", m.comp.open, m.comp.items)
	}
	m.renderCompletion(10, 5)
	m.themes.dark = false
	m.applyTheme("dracula")

	// Scroll through the popup because there are more kinds than visible rows.
	for i, it := range m.comp.items {
		m.comp.idx = i
		popup, _, _, _ := m.renderCompletion(10, 5)
		glyph := kindIcons[it.Kind]
		if !strings.Contains(ansi.Strip(popup), " "+glyph+" ") {
			t.Errorf("kind %d glyph %q missing from popup", it.Kind, glyph)
		}
		want := lipgloss.NewStyle().Background(colSelection).
			Foreground(kindColor(it.Kind)).Bold(true).Render(" " + glyph + " ")
		if !strings.Contains(popup, want) {
			t.Errorf("kind %d icon does not use the active palette", it.Kind)
		}
	}
}

func TestCompletionManualTab(t *testing.T) {
	m := newCompletionModel(complete.Item{Label: "counter", Insert: "counter", Kind: complete.KindVar})
	typeText(m, "cou")
	m.closeCompletion()
	// Tab after an identifier completes; a single match is inserted.
	runCompletionCmds(m, m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab}))
	if got := m.cur().ed.Value(); got != "counter" {
		t.Fatalf("manual tab: %q", got)
	}
	// Tab at the start of a line still indents.
	m.cur().ed.SetValue("")
	runCompletionCmds(m, m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab}))
	if got := m.cur().ed.Value(); got != "\t" {
		t.Fatalf("tab indent: %q", got)
	}
}
