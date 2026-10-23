package ui

import (
	"charm.land/bubbles/v2/key"
	"github.com/mark3labs/gopyter/internal/notebook"
)

type keyMap struct {
	RestartClear, SaveAs                                             key.Binding
	ToRaw, ToggleInput, ScrollLeft, ScrollRight, ExtEdit, CopyOutput key.Binding

	// Both modes
	RunAdvance key.Binding
	Run        key.Binding
	RunInsert  key.Binding
	Save       key.Binding
	Interrupt  key.Binding
	Input      key.Binding // focus the running program's input

	// Command mode
	Sidebar                 key.Binding
	ExtendUp, ExtendDown    key.Binding
	SelectAll, Merge, Split key.Binding
	Up, Down                key.Binding
	Top, Bottom             key.Binding
	PageUp                  key.Binding
	PageDown                key.Binding
	Edit                    key.Binding
	InsertAbove             key.Binding
	InsertBelow             key.Binding
	Delete                  key.Binding
	Undelete                key.Binding
	Cut                     key.Binding
	Copy                    key.Binding
	Paste                   key.Binding
	MoveUp                  key.Binding
	MoveDown                key.Binding
	ToMarkdown              key.Binding
	ToCode                  key.Binding
	ToggleOutput            key.Binding
	ClearOutput             key.Binding
	ClearAllOutput          key.Binding
	RunAll                  key.Binding
	Restart                 key.Binding
	Help                    key.Binding
	Theme                   key.Binding
	ToggleVim               key.Binding
	Fix                     key.Binding
	Ask                     key.Binding
	AIModel                 key.Binding
	Palette                 key.Binding
	Quit                    key.Binding

	// Edit mode
	Escape key.Binding
	Undo   key.Binding
	Redo   key.Binding
	Info   key.Binding
}

func newKeyMap() keyMap {
	b := func(keys []string, k, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(k, desc))
	}
	return keyMap{
		ToRaw:        b([]string{"r"}, "r", "to raw"),
		ToggleInput:  b([]string{"I"}, "I", "hide/show input"),
		ScrollLeft:   b([]string{"<"}, "<", "scroll output ←"),
		ScrollRight:  b([]string{">"}, ">", "scroll output →"),
		ExtEdit:      b([]string{"E"}, "E", "edit in $EDITOR"),
		CopyOutput:   b([]string{"C"}, "C", "copy output"),
		RestartClear: b([]string{"alt+r"}, "alt+r", "restart & clear"),
		SaveAs:       b([]string{"alt+s"}, "alt+s", "save as…"),
		RunAdvance:   b([]string{"shift+enter", "ctrl+r"}, "⇧↵/^r", "run & next"),
		Run:          b([]string{"ctrl+enter", "ctrl+j"}, "^↵", "run"),
		RunInsert:    b([]string{"alt+enter"}, "alt+↵", "run & insert"),
		Save:         b([]string{"ctrl+s"}, "^s", "save"),
		Interrupt:    b([]string{"ctrl+c"}, "^c", "interrupt"),
		Input:        b([]string{"alt+i"}, "alt+i", "input line"),
		// Widgets: enter on the running cell focuses them, ⇥ moves.

		ExtendUp:       b([]string{"alt+up"}, "alt+↑", "extend up"),
		ExtendDown:     b([]string{"alt+down"}, "alt+↓", "extend down"),
		SelectAll:      b([]string{"alt+a"}, "alt+a", "select all cells"),
		Merge:          b([]string{"alt+m"}, "alt+m", "merge cells"),
		Split:          b([]string{"alt+-"}, "alt+-", "split cell (editing)"),
		Up:             b([]string{"up", "k"}, "↑/k", "up"),
		Down:           b([]string{"down", "j"}, "↓/j", "down"),
		Top:            b([]string{"g", "home"}, "g", "first cell"),
		Bottom:         b([]string{"G", "end"}, "G", "last cell"),
		PageUp:         b([]string{"pgup", "ctrl+u"}, "pgup", "page up"),
		PageDown:       b([]string{"pgdown", "ctrl+d"}, "pgdn", "page down"),
		Edit:           b([]string{"enter", "i"}, "↵", "edit"),
		InsertAbove:    b([]string{"a"}, "a", "insert above"),
		InsertBelow:    b([]string{"b"}, "b", "insert below"),
		Delete:         b([]string{"d"}, "dd", "delete"),
		Undelete:       b([]string{"z", "u"}, "z", "undo delete/merge/split"),
		Cut:            b([]string{"x"}, "x", "cut"),
		Copy:           b([]string{"c"}, "c", "copy"),
		Paste:          b([]string{"v", "p"}, "v", "paste below"),
		MoveUp:         b([]string{"K", "shift+up"}, "K", "move up"),
		MoveDown:       b([]string{"J", "shift+down"}, "J", "move down"),
		ToMarkdown:     b([]string{"m"}, "m", "to markdown"),
		ToCode:         b([]string{"y"}, "y", "to code"),
		ToggleOutput:   b([]string{"o"}, "o", "fold output"),
		ClearOutput:    b([]string{"O"}, "O", "clear output"),
		ClearAllOutput: b([]string{"D"}, "D", "clear all outputs"),
		RunAll:         b([]string{"A"}, "A", "run all"),
		Restart:        b([]string{"R"}, "R", "restart kernel"),
		Help:           b([]string{"?"}, "?", "help"),
		Sidebar:        b([]string{"ctrl+b"}, "^b", "sidebar"),
		Theme:          b([]string{"T"}, "T", "color theme"),
		ToggleVim:      b([]string{"V"}, "V", "vim keys on/off"),
		Fix:            b([]string{"f"}, "f", "AI fix error"),
		Ask:            b([]string{"e"}, "e", "ask AI to edit"),
		AIModel:        b([]string{"M"}, "M", "AI model / on-off"),
		Palette:        b([]string{"ctrl+p", ":"}, "^p/:", "command palette"),
		Quit:           b([]string{"q"}, "q", "quit"),

		Escape: b([]string{"esc"}, "esc", "command mode"),
		Undo:   b([]string{"ctrl+z"}, "^z", "undo"),
		Redo:   b([]string{"ctrl+y", "ctrl+shift+z"}, "^y", "redo"),
		Info:   b([]string{"alt+k", "f1"}, "alt+k/F1", "symbol info"),
	}
}

// commandShort returns the footer hints for command mode. The conversion
// hint depends on the type of the selected cell. While AI is on, fixable
// adds the AI fix hint for a failed cell, and editable the AI edit hint
// for a code cell.
func (k keyMap) commandShort(kind notebook.CellType, fixable, editable bool) []key.Binding {
	convert := k.ToMarkdown
	if kind != notebook.Code {
		convert = k.ToCode
	}
	var keys []key.Binding
	switch {
	case fixable:
		keys = append(keys, k.Fix)
	case editable:
		keys = append(keys, k.Ask)
	}
	return append(keys, k.Edit, k.RunAdvance, convert, k.InsertBelow, k.Delete, k.Save, k.Help)
}

func (k keyMap) editShort() []key.Binding {
	return []key.Binding{k.Escape, k.RunAdvance, k.Run, k.RunInsert, k.Undo, k.Save}
}

func (k keyMap) sidebarShort() []key.Binding {
	return []key.Binding{hint("↑↓", "move"), hint("↵", "jump"), hint("⇥", "panel"), hint("esc", "notebook"), k.Sidebar}
}

type helpSection struct {
	title string
	keys  []key.Binding
}

// withKey appends a configured help key, omitting it for unbound commands.
func withKey(desc string, b key.Binding) string {
	if !b.Enabled() {
		return desc
	}
	return desc + " · " + b.Help().Key
}

// hint is a help-only binding.
func hint(keys, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys), key.WithHelp(keys, desc))
}

// inputShort returns the footer hints while the program's input has the
// focus: its input line, or a widget.
func (k keyMap) inputShort(line bool) []key.Binding {
	if line {
		return []key.Binding{hint("↵", "send line"), hint("⇥", "next"), hint("^d", "done"), hint("esc", "leave"), k.Interrupt}
	}
	return []key.Binding{hint("←/→", "change"), hint("↵", "press/choose"), hint("⇥", "next"), hint("^d", "done"), hint("esc", "leave")}
}

// vimNormalShort returns the footer hints for vim normal mode.
func (k keyMap) vimNormalShort() []key.Binding {
	return []key.Binding{
		hint("i", "insert"), hint("v", "visual"), hint("esc", "command mode"),
		hint("⇧↵", "run & next"), hint("u", "undo"), k.Save,
	}
}

// vimInsertShort returns the footer hints for vim insert mode.
func (k keyMap) vimInsertShort() []key.Binding {
	return []key.Binding{hint("esc", "normal"), k.RunAdvance, k.Run, k.RunInsert, k.Undo, k.Save}
}

// vimVisualShort returns the footer hints for vim visual mode.
func (k keyMap) vimVisualShort() []key.Binding {
	return []key.Binding{hint("d", "delete"), hint("y", "yank"), hint("c", "change"), hint("esc", "normal")}
}

// fullHelp returns the help overlay sections; vim adds its own. The AI
// section needs AI support (aiAvailable); its fix keys only show while AI
// is on. Help and the command palette may mention the model picker while AI is off.
func (k keyMap) fullHelp(vim, aiAvailable, aiOn bool) []helpSection {
	secs := k.baseHelp()
	if aiAvailable {
		keys := []key.Binding{k.AIModel}
		if aiOn {
			keys = append(keys, k.Fix, k.Ask, hint("esc", "cancel a request"))
		}
		secs = append(secs, helpSection{"AI", keys})
	}
	if vim {
		secs = append(secs, helpSection{"Vim (edit mode)", []key.Binding{
			hint("i a o", "insert / open line"),
			hint("esc", "normal / command"),
			hint("v V", "visual / line"),
			hint("d c y", "delete/change/yank"),
			hint("dd yy", "whole lines"),
			hint("x D C", "char / line end"),
			hint("p P", "put"),
			hint("u ^r", "undo / redo"),
			hint("w b $ G", "motions, 3w"),
			hint("K", "symbol info"),
		}})
	}
	for i := range secs {
		var enabled []key.Binding
		for _, b := range secs[i].keys {
			if b.Enabled() {
				enabled = append(enabled, b)
			}
		}
		secs[i].keys = enabled
	}
	return secs
}

func (k keyMap) baseHelp() []helpSection {
	widgets := k.Edit
	widgets.SetHelp(k.Edit.Help().Key, "use widgets")
	return []helpSection{
		// The widget keys (←/→, ⇥...) are in the footer while one has the
		// focus.
		{"Running", []key.Binding{
			k.RunAdvance, k.Run, k.RunInsert, k.RunAll, k.Interrupt, k.Restart, k.RestartClear,
			widgets, k.Input, hint("^d", "done (input)"),
		}},
		{"Navigation", []key.Binding{k.Up, k.Down, k.Top, k.Bottom, k.PageUp, k.PageDown, k.Edit, k.Escape}},
		{"Cells", []key.Binding{k.ExtendUp, k.ExtendDown, hint("⇧click", "extend selection"), k.SelectAll, k.Merge, k.Split, k.InsertAbove, k.InsertBelow, k.Delete, k.Undelete, k.Cut, k.Copy, k.Paste, k.MoveUp, k.MoveDown}},
		{"Misc", []key.Binding{k.ToMarkdown, k.ToCode, k.ToggleOutput, k.ClearOutput, k.ClearAllOutput, k.Save, k.SaveAs, k.Sidebar, k.Theme, k.ToggleVim, k.Palette, k.Help, k.Quit}},
		{"Cell", []key.Binding{k.ToRaw, k.ToggleInput, k.ScrollLeft, k.ScrollRight, k.ExtEdit, k.CopyOutput}},
		{"Editing", []key.Binding{
			k.Undo, k.Redo,
			key.NewBinding(key.WithKeys("alt+shift+f"), key.WithHelp("alt+⇧+f", "format Go code")),
			key.NewBinding(key.WithKeys("shift+left"), key.WithHelp("⇧+move", "select")),
			key.NewBinding(key.WithKeys("alt+a"), key.WithHelp("alt+a", "select all")),
			key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("^c", "copy selection")),
			key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("^x", "cut selection")),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("⇥/⇧⇥", "indent/dedent")),
			key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "code ⇄ markdown")),
			key.NewBinding(key.WithKeys("ctrl+space"), key.WithHelp("⇥/^␣", "complete")),
			k.Info,
		}},
	}
}
