package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func press(code rune, mod tea.KeyMod) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Mod: mod} }

func TestDialogFocusRing(t *testing.T) {
	nb := &notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code, Source: "x := 1"}}}
	m := New(Options{Notebook: nb})
	m.dirty = true
	m.requestQuit()
	if m.overlay != overlayQuit || m.dlgFocus != 0 {
		t.Fatalf("quit dialog: overlay=%v focus=%d", m.overlay, m.dlgFocus)
	}

	steps := []struct {
		msg  tea.KeyPressMsg
		want int
	}{
		{press(tea.KeyTab, 0), 1},
		{press(tea.KeyTab, 0), 2},
		{press(tea.KeyTab, 0), 0}, // wraps
		{press(tea.KeyTab, tea.ModShift), 2},
		{press(tea.KeyRight, 0), 0},
		{press(tea.KeyLeft, 0), 2},
	}
	for i, s := range steps {
		m.handleKey(s.msg)
		if m.dlgFocus != s.want {
			t.Fatalf("step %d: focus=%d want %d", i, m.dlgFocus, s.want)
		}
	}
	// Enter activates the focused button (Cancel).
	m.handleKey(press(tea.KeyEnter, 0))
	if m.overlay != overlayNone || m.quitting {
		t.Fatalf("cancel: overlay=%v quitting=%v", m.overlay, m.quitting)
	}

	// Save-as: the input is part of the ring and starts focused.
	m.openSaveAs()
	if m.dlgFocus != focusInput || !m.input.Focused() {
		t.Fatalf("save-as should focus the input")
	}
	m.handleKey(press(tea.KeyTab, 0))
	if m.dlgFocus != 0 || m.input.Focused() {
		t.Fatalf("tab should focus Save and blur the input")
	}
	m.handleKey(press(tea.KeyTab, tea.ModShift))
	if m.dlgFocus != focusInput || !m.input.Focused() {
		t.Fatalf("shift+tab should return to the input")
	}
	m.handleKey(press(tea.KeyTab, tea.ModShift))
	if m.dlgFocus != 1 {
		t.Fatalf("shift+tab from the input should wrap to Cancel, got %d", m.dlgFocus)
	}
	m.handleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.dlgFocus != focusInput || m.input.Value() != "n" {
		t.Fatalf("typing should refocus the input: focus=%d value=%q", m.dlgFocus, m.input.Value())
	}
}
