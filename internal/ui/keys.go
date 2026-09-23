package ui

import (
	"charm.land/bubbles/v2/key"
	"github.com/mark3labs/gopyter/internal/notebook"
)

type keyMap struct {
	// Both modes
	RunAdvance key.Binding
	Run        key.Binding
	RunInsert  key.Binding
	Save       key.Binding
	Interrupt  key.Binding

	// Command mode
	Up, Down     key.Binding
	Top, Bottom  key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
	Edit         key.Binding
	InsertAbove  key.Binding
	InsertBelow  key.Binding
	Delete       key.Binding
	Undelete     key.Binding
	Cut          key.Binding
	Copy         key.Binding
	Paste        key.Binding
	MoveUp       key.Binding
	MoveDown     key.Binding
	ToMarkdown   key.Binding
	ToCode       key.Binding
	ToggleOutput key.Binding
	ClearOutput  key.Binding
	RunAll       key.Binding
	Restart      key.Binding
	Help         key.Binding
	Quit         key.Binding

	// Edit mode
	Escape key.Binding
	Undo   key.Binding
	Redo   key.Binding
}

func newKeyMap() keyMap {
	b := func(keys []string, k, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(k, desc))
	}
	return keyMap{
		RunAdvance: b([]string{"shift+enter", "ctrl+r"}, "⇧↵/^r", "run & next"),
		Run:        b([]string{"ctrl+enter", "ctrl+j"}, "^↵", "run"),
		RunInsert:  b([]string{"alt+enter"}, "alt+↵", "run & insert"),
		Save:       b([]string{"ctrl+s"}, "^s", "save"),
		Interrupt:  b([]string{"ctrl+c"}, "^c", "interrupt"),

		Up:           b([]string{"up", "k"}, "↑/k", "up"),
		Down:         b([]string{"down", "j"}, "↓/j", "down"),
		Top:          b([]string{"g", "home"}, "g", "first cell"),
		Bottom:       b([]string{"G", "end"}, "G", "last cell"),
		PageUp:       b([]string{"pgup", "ctrl+u"}, "pgup", "page up"),
		PageDown:     b([]string{"pgdown", "ctrl+d"}, "pgdn", "page down"),
		Edit:         b([]string{"enter", "i"}, "↵", "edit"),
		InsertAbove:  b([]string{"a"}, "a", "insert above"),
		InsertBelow:  b([]string{"b"}, "b", "insert below"),
		Delete:       b([]string{"d"}, "dd", "delete"),
		Undelete:     b([]string{"z", "u"}, "z", "undo delete"),
		Cut:          b([]string{"x"}, "x", "cut"),
		Copy:         b([]string{"c"}, "c", "copy"),
		Paste:        b([]string{"v", "p"}, "v", "paste below"),
		MoveUp:       b([]string{"K", "shift+up"}, "K", "move up"),
		MoveDown:     b([]string{"J", "shift+down"}, "J", "move down"),
		ToMarkdown:   b([]string{"m"}, "m", "to markdown"),
		ToCode:       b([]string{"y"}, "y", "to code"),
		ToggleOutput: b([]string{"o"}, "o", "fold output"),
		ClearOutput:  b([]string{"O"}, "O", "clear output"),
		RunAll:       b([]string{"A"}, "A", "run all"),
		Restart:      b([]string{"R"}, "R", "restart kernel"),
		Help:         b([]string{"?"}, "?", "help"),
		Quit:         b([]string{"q"}, "q", "quit"),

		Escape: b([]string{"esc"}, "esc", "command mode"),
		Undo:   b([]string{"ctrl+z"}, "^z", "undo"),
		Redo:   b([]string{"ctrl+y", "ctrl+shift+z"}, "^y", "redo"),
	}
}

// commandShort returns the footer hints for command mode. The conversion
// hint depends on the type of the selected cell.
func (k keyMap) commandShort(kind notebook.CellType) []key.Binding {
	convert := k.ToMarkdown
	if kind != notebook.Code {
		convert = k.ToCode
	}
	return []key.Binding{k.Edit, k.RunAdvance, convert, k.InsertBelow, k.Delete, k.Save, k.Help}
}

func (k keyMap) editShort() []key.Binding {
	return []key.Binding{k.Escape, k.RunAdvance, k.Run, k.RunInsert, k.Undo, k.Save}
}

type helpSection struct {
	title string
	keys  []key.Binding
}

func (k keyMap) fullHelp() []helpSection {
	return []helpSection{
		{"Running", []key.Binding{k.RunAdvance, k.Run, k.RunInsert, k.RunAll, k.Interrupt, k.Restart}},
		{"Navigation", []key.Binding{k.Up, k.Down, k.Top, k.Bottom, k.PageUp, k.PageDown, k.Edit, k.Escape}},
		{"Cells", []key.Binding{k.InsertAbove, k.InsertBelow, k.Delete, k.Undelete, k.Cut, k.Copy, k.Paste, k.MoveUp, k.MoveDown}},
		{"Misc", []key.Binding{k.ToMarkdown, k.ToCode, k.ToggleOutput, k.ClearOutput, k.Save, k.Help, k.Quit}},
		{"Editing", []key.Binding{
			k.Undo, k.Redo,
			key.NewBinding(key.WithKeys("shift+left"), key.WithHelp("⇧+move", "select")),
			key.NewBinding(key.WithKeys("alt+a"), key.WithHelp("alt+a", "select all")),
			key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("^c", "copy selection")),
			key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("^x", "cut selection")),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("⇥/⇧⇥", "indent/dedent")),
			key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "code ⇄ markdown")),
			key.NewBinding(key.WithKeys("ctrl+space"), key.WithHelp("⇥/^␣", "complete")),
		}},
	}
}
